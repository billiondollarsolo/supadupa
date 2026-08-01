package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *MemoryStore) CreateProject(ctx context.Context, req CreateProjectRequest) (Project, error) {
	req = s.createProjectRequestWithDefaults(req)
	if err := validateCreateProject(req); err != nil {
		return Project{}, err
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[req.OrgID]; !ok {
		return Project{}, fmt.Errorf("%w: org %s", ErrNotFound, req.OrgID)
	}
	if _, ok := s.projects[req.Ref]; ok {
		return Project{}, fmt.Errorf("%w: project ref %s already exists", ErrConflict, req.Ref)
	}
	if err := s.validateGeneratedProjectHostReservationsLocked(req.Ref, req.Domain); err != nil {
		return Project{}, err
	}
	spec := req.toSpec()
	reservation := resourceReservationForSpec(spec)
	if err := s.validateOrgQuotaLocked(req.OrgID, reservation); err != nil {
		return Project{}, err
	}
	if req.HostID == "" {
		req.HostID = s.defaultHostForReservationLocked(reservation)
		spec.HostID = req.HostID
	}
	if req.HostID != "" {
		host, ok := s.hosts[req.HostID]
		if !ok {
			return Project{}, fmt.Errorf("%w: host %s", ErrNotFound, req.HostID)
		}
		if !hostHasCapacity(host, reservation) {
			return Project{}, fmt.Errorf("%w: host %s has insufficient capacity", ErrConflict, req.HostID)
		}
		host.Used = addHostCapacity(host.Used, reservation)
		s.hosts[host.ID] = host
	}
	project := Project{
		ID:        newID(),
		Ref:       req.Ref,
		OrgID:     req.OrgID,
		Name:      req.Name,
		Status:    ProjectProvisioning,
		Message:   "project accepted for provisioning",
		Spec:      spec,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.projects[project.Ref] = project
	if project.Spec.Profile == StackProfileOrioleDB {
		if s.configs[project.Ref] == nil {
			s.configs[project.Ref] = map[string]ProjectConfig{}
		}
		config := defaultProjectConfig(project.Ref, "database")
		config.Config["orioledb_profile"] = "preview"
		config.UpdatedAt = now
		s.configs[project.Ref]["database"] = config
	}
	s.secrets[project.Ref] = generateProjectSecrets(project.Ref)
	s.policies[project.Ref] = defaultBackupPolicyForSchedule(project.Ref, s.platformDefaults.BackupSchedule)
	s.pitrPolicies[project.Ref] = defaultPITRPolicy(project.Ref)
	return project, nil
}

func (s *MemoryStore) defaultHostForReservationLocked(reservation HostCapacity) string {
	type candidate struct {
		id       string
		projects int
		cpu      int
	}
	candidates := make([]candidate, 0, len(s.hosts))
	for _, host := range s.hosts {
		if hostHasCapacity(host, reservation) {
			candidates = append(candidates, candidate{id: host.ID, projects: host.Used.Project, cpu: host.Used.CPU})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].projects != candidates[j].projects {
			return candidates[i].projects < candidates[j].projects
		}
		if candidates[i].cpu != candidates[j].cpu {
			return candidates[i].cpu < candidates[j].cpu
		}
		return candidates[i].id < candidates[j].id
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].id
}

func (s *MemoryStore) createProjectRequestWithDefaults(req CreateProjectRequest) CreateProjectRequest {
	s.mu.RLock()
	defaults := normalizedPlatformDefaults(s.platformDefaults)
	s.mu.RUnlock()
	if strings.TrimSpace(req.Domain) == "" {
		req.Domain = defaults.Domain
	}
	if strings.TrimSpace(req.StackVersion) == "" {
		req.StackVersion = defaults.StackVersion
	}
	req.StackVersion = NormalizeStackReleaseVersion(req.StackVersion)
	if req.Profile == "" {
		req.Profile = defaults.Profile
	}
	if req.ResourceTier == "" {
		req.ResourceTier = ResourceTierCustom
	}
	if req.ResourceTier == ResourceTierCustom {
		spec := req.toSpec()
		recommended := recommendedReservationForSpec(spec)
		if req.CPU == 0 {
			req.CPU = recommended.CPU
		}
		if req.RAMMB == 0 {
			req.RAMMB = recommended.RAMMB
		}
		if req.DiskGB == 0 {
			req.DiskGB = recommended.DiskGB
		}
	}
	return req
}

func (s *MemoryStore) ListProjectsByOrg(ctx context.Context, orgID string) ([]Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}

	projects := make([]Project, 0)
	for _, project := range s.projects {
		if project.OrgID == orgID {
			projects = append(projects, project)
		}
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].CreatedAt.Before(projects[j].CreatedAt)
	})
	return projects, nil
}

func (s *MemoryStore) ListProjects(ctx context.Context) ([]Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	projects := make([]Project, 0, len(s.projects))
	for _, project := range s.projects {
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].CreatedAt.Before(projects[j].CreatedAt)
	})
	return projects, nil
}

func (s *MemoryStore) GetProject(ctx context.Context, ref string) (Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return project, nil
}

func (s *MemoryStore) UpdateProjectStatus(ctx context.Context, ref string, status ProjectPhase, message string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	project.Status = status
	project.Message = message
	project.UpdatedAt = time.Now().UTC()
	s.projects[ref] = project
	return project, nil
}

func (s *MemoryStore) UpdateProjectStackVersion(ctx context.Context, ref string, version string) (Project, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return Project{}, fmt.Errorf("stack version is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	project.Spec.StackVersion = version
	project.Message = "stack version updated"
	project.UpdatedAt = time.Now().UTC()
	s.projects[ref] = project
	return project, nil
}

func (s *MemoryStore) UpdateProjectResourceTier(ctx context.Context, ref string, tier ResourceTier) (Project, error) {
	if tier == "" {
		return Project{}, fmt.Errorf("resource tier is required")
	}
	if _, ok := resourceTierReservations[tier]; !ok {
		return Project{}, fmt.Errorf("unsupported resource tier %q", tier)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	oldReservation := resourceReservationForSpec(project.Spec)
	newReservation := resourceReservationForTier(tier)
	if err := s.validateProjectScaleQuotaLocked(project.OrgID, oldReservation, newReservation); err != nil {
		return Project{}, err
	}
	if project.Spec.HostID != "" {
		host, ok := s.hosts[project.Spec.HostID]
		if !ok {
			return Project{}, fmt.Errorf("%w: host %s", ErrNotFound, project.Spec.HostID)
		}
		nextUsed := addHostCapacity(subtractHostCapacity(host.Used, oldReservation), newReservation)
		if !capacityWithinLimit(nextUsed.CPU, host.Capacity.CPU) ||
			!capacityWithinLimit(nextUsed.RAMMB, host.Capacity.RAMMB) ||
			!capacityWithinLimit(nextUsed.DiskGB, host.Capacity.DiskGB) ||
			!capacityWithinLimit(nextUsed.Project, host.Capacity.Project) {
			return Project{}, fmt.Errorf("%w: host %s has insufficient capacity for %s tier", ErrConflict, project.Spec.HostID, tier)
		}
		host.Used = nextUsed
		s.hosts[host.ID] = host
	}
	project.Spec.ResourceTier = tier
	// Scaling by preset resets any exact per-dimension overrides so the new
	// tier's defaults take effect cleanly.
	project.Spec.CPU = 0
	project.Spec.RAMMB = 0
	project.Spec.DiskGB = 0
	project.Message = "resource tier updated"
	project.UpdatedAt = time.Now().UTC()
	s.projects[ref] = project
	return project, nil
}

func (s *MemoryStore) UpdateProjectResources(ctx context.Context, ref string, input ProjectResourcesInput) (Project, error) {
	if input.CPU < 1 {
		return Project{}, fmt.Errorf("cpu must be at least 1 core")
	}
	if input.RAMMB < minProjectRAMMB {
		return Project{}, fmt.Errorf("ram cannot be below %d MB", minProjectRAMMB)
	}
	if input.DiskGB < 1 {
		return Project{}, fmt.Errorf("disk must be at least 1 GB")
	}
	if err := validateResourceSizing(input.CPU, input.RAMMB, input.DiskGB); err != nil {
		return Project{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	oldReservation := resourceReservationForSpec(project.Spec)
	nextSpec := project.Spec
	nextSpec.ResourceTier = ResourceTierCustom
	nextSpec.CPU = input.CPU
	nextSpec.RAMMB = input.RAMMB
	nextSpec.DiskGB = input.DiskGB
	nextSpec.EnforceLimits = input.EnforceLimits
	if err := validateEnforcedResourceMinimum(nextSpec); err != nil {
		return Project{}, err
	}
	newReservation := resourceReservationForSpec(nextSpec)
	if err := s.validateProjectScaleQuotaLocked(project.OrgID, oldReservation, newReservation); err != nil {
		return Project{}, err
	}
	if project.Spec.HostID != "" {
		host, ok := s.hosts[project.Spec.HostID]
		if !ok {
			return Project{}, fmt.Errorf("%w: host %s", ErrNotFound, project.Spec.HostID)
		}
		nextUsed := addHostCapacity(subtractHostCapacity(host.Used, oldReservation), newReservation)
		if !capacityWithinLimit(nextUsed.CPU, host.Capacity.CPU) ||
			!capacityWithinLimit(nextUsed.RAMMB, host.Capacity.RAMMB) ||
			!capacityWithinLimit(nextUsed.DiskGB, host.Capacity.DiskGB) ||
			!capacityWithinLimit(nextUsed.Project, host.Capacity.Project) {
			return Project{}, fmt.Errorf("%w: host %s has insufficient capacity for requested resources", ErrConflict, project.Spec.HostID)
		}
		host.Used = nextUsed
		s.hosts[host.ID] = host
	}
	project.Spec = nextSpec
	project.Message = "resource sizing updated"
	project.UpdatedAt = time.Now().UTC()
	s.projects[ref] = project
	return project, nil
}

func (s *MemoryStore) GetProjectServices(ctx context.Context, ref string) (ProjectServices, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectServices{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return ProjectServices{
		ProjectRef: project.Ref,
		Services:   ProjectServiceStates(project.Spec.Services),
		UpdatedAt:  project.UpdatedAt,
	}, nil
}

func (s *MemoryStore) UpdateProjectServices(ctx context.Context, ref string, input ProjectServicesInput) (Project, error) {
	services, err := normalizeProjectServices(input.Services)
	if err != nil {
		return Project{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	project.Spec.Services = services
	project.Message = "enabled services updated"
	project.UpdatedAt = time.Now().UTC()
	s.projects[ref] = project
	return project, nil
}

func (s *MemoryStore) DeleteProject(ctx context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteProjectLocked(ref)
}

func (s *MemoryStore) deleteProjectLocked(ref string) error {
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	project := s.projects[ref]
	if project.Spec.HostID != "" {
		if host, ok := s.hosts[project.Spec.HostID]; ok {
			host.Used = subtractHostCapacity(host.Used, resourceReservationForSpec(project.Spec))
			s.hosts[host.ID] = host
		}
	}
	delete(s.projects, ref)
	delete(s.branches, ref)
	for _, replica := range s.replicas[ref] {
		if replica.HostID != "" {
			if host, ok := s.hosts[replica.HostID]; ok {
				host.Used = subtractHostCapacity(host.Used, replicaReservationForTier(replica.Tier))
				s.hosts[host.ID] = host
			}
		}
	}
	delete(s.replicas, ref)
	for sourceRef, branches := range s.branches {
		filtered := branches[:0]
		for _, branch := range branches {
			if branch.ProjectRef != ref {
				filtered = append(filtered, branch)
			}
		}
		if len(filtered) == 0 {
			delete(s.branches, sourceRef)
		} else {
			s.branches[sourceRef] = filtered
		}
	}
	s.cleanupRegisteredProjectChildrenLocked(ref)
	return nil
}
