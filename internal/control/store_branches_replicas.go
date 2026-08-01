package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *MemoryStore) CreateProjectBranch(ctx context.Context, sourceRef string, input ProjectBranchInput) (ProjectBranch, Project, error) {
	sourceRef = strings.ToLower(strings.TrimSpace(sourceRef))
	branchRef := strings.ToLower(strings.TrimSpace(input.Ref))
	if !projectRefPattern.MatchString(branchRef) {
		return ProjectBranch{}, Project{}, fmt.Errorf("branch ref must be 3-55 lowercase letters, numbers, or hyphens, and cannot start or end with a hyphen")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = branchRef
	}
	if input.TTLHours < 0 {
		return ProjectBranch{}, Project{}, fmt.Errorf("ttl_hours cannot be negative")
	}

	now := time.Now().UTC()
	var expiresAt *time.Time
	if input.TTLHours > 0 {
		expires := now.Add(time.Duration(input.TTLHours) * time.Hour)
		expiresAt = &expires
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.projects[sourceRef]
	if !ok {
		return ProjectBranch{}, Project{}, fmt.Errorf("%w: project %s", ErrNotFound, sourceRef)
	}
	if _, ok := s.projects[branchRef]; ok {
		return ProjectBranch{}, Project{}, fmt.Errorf("%w: project ref %s already exists", ErrConflict, branchRef)
	}

	environment := cloneStringMap(source.Spec.Environment)
	environment["SUPADUPA_BRANCH_SOURCE_REF"] = source.Ref
	req := CreateProjectRequest{
		OrgID:         source.OrgID,
		Ref:           branchRef,
		Name:          name,
		HostID:        source.Spec.HostID,
		Domain:        source.Spec.Domain,
		StackVersion:  source.Spec.StackVersion,
		Profile:       source.Spec.Profile,
		ResourceTier:  source.Spec.ResourceTier,
		CPU:           source.Spec.CPU,
		RAMMB:         source.Spec.RAMMB,
		DiskGB:        source.Spec.DiskGB,
		EnforceLimits: source.Spec.EnforceLimits,
		Services:      serviceEnabledMap(source.Spec.Services),
		Environment:   environment,
	}
	if err := validateCreateProject(req); err != nil {
		return ProjectBranch{}, Project{}, err
	}
	if err := s.validateGeneratedProjectHostReservationsLocked(req.Ref, req.Domain); err != nil {
		return ProjectBranch{}, Project{}, err
	}

	project := Project{
		ID:        newID(),
		Ref:       req.Ref,
		OrgID:     req.OrgID,
		Name:      req.Name,
		Status:    ProjectProvisioning,
		Message:   "branch accepted for provisioning",
		Spec:      req.toSpec(),
		CreatedAt: now,
		UpdatedAt: now,
	}
	reservation := resourceReservationForSpec(project.Spec)
	if err := s.validateOrgQuotaLocked(project.OrgID, reservation); err != nil {
		return ProjectBranch{}, Project{}, err
	}
	if project.Spec.HostID != "" {
		host, ok := s.hosts[project.Spec.HostID]
		if !ok {
			return ProjectBranch{}, Project{}, fmt.Errorf("%w: host %s", ErrNotFound, project.Spec.HostID)
		}
		if !hostHasCapacity(host, reservation) {
			return ProjectBranch{}, Project{}, fmt.Errorf("%w: host %s has insufficient capacity for %s tier", ErrConflict, project.Spec.HostID, project.Spec.ResourceTier)
		}
		host.Used = addHostCapacity(host.Used, reservation)
		s.hosts[host.ID] = host
	}

	branch := ProjectBranch{
		ID:               newID(),
		SourceProjectRef: source.Ref,
		ProjectRef:       project.Ref,
		Name:             name,
		WithData:         input.WithData,
		Status:           string(project.Status),
		CreatedAt:        now,
		ExpiresAt:        expiresAt,
	}
	s.projects[project.Ref] = project
	s.branches[source.Ref] = append(s.branches[source.Ref], branch)
	s.secrets[project.Ref] = generateProjectSecrets(project.Ref)
	s.policies[project.Ref] = defaultBackupPolicy(project.Ref)
	s.pitrPolicies[project.Ref] = defaultPITRPolicy(project.Ref)
	return branch, project, nil
}

func (s *MemoryStore) ListProjectBranches(ctx context.Context, sourceRef string) ([]ProjectBranch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sourceRef = strings.ToLower(strings.TrimSpace(sourceRef))
	if _, ok := s.projects[sourceRef]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, sourceRef)
	}
	branches := append([]ProjectBranch(nil), s.branches[sourceRef]...)
	for index := range branches {
		if project, ok := s.projects[branches[index].ProjectRef]; ok {
			branches[index].Status = string(project.Status)
		}
	}
	sort.Slice(branches, func(i, j int) bool {
		return branches[i].CreatedAt.Before(branches[j].CreatedAt)
	})
	if branches == nil {
		branches = []ProjectBranch{}
	}
	return branches, nil
}

func (s *MemoryStore) DeleteProjectBranch(ctx context.Context, sourceRef string, branchRef string) error {
	sourceRef = strings.ToLower(strings.TrimSpace(sourceRef))
	branchRef = strings.ToLower(strings.TrimSpace(branchRef))

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[sourceRef]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, sourceRef)
	}
	found := false
	for _, branch := range s.branches[sourceRef] {
		if branch.ProjectRef == branchRef {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: branch %s", ErrNotFound, branchRef)
	}
	return s.deleteProjectLocked(branchRef)
}

func (s *MemoryStore) CreateProjectReplica(ctx context.Context, ref string, input ProjectReplicaInput) (ProjectReplica, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name := strings.ToLower(strings.TrimSpace(input.Name))
	tier := ResourceTier(strings.TrimSpace(string(input.Tier)))
	region := strings.TrimSpace(input.Region)
	hostID := strings.TrimSpace(input.HostID)
	readWeight := input.ReadWeight
	if readWeight <= 0 {
		readWeight = 100
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectReplica{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if tier == "" {
		tier = defaultReplicaTierForProject(project.Spec)
	}
	if err := validateReplicaResourceTier(tier); err != nil {
		return ProjectReplica{}, err
	}
	if name == "" {
		name = fmt.Sprintf("replica-%d", len(s.replicas[ref])+1)
	}
	failoverPriority := input.FailoverPriority
	if failoverPriority <= 0 {
		failoverPriority = len(s.replicas[ref]) + 1
	}
	normalizedName, err := normalizeReplicaName(name)
	if err != nil {
		return ProjectReplica{}, err
	}
	if err := validateReplicaPublicDNSHost(ref, normalizedName, project.Spec.Domain); err != nil {
		return ProjectReplica{}, err
	}
	for _, replica := range s.replicas[ref] {
		if replica.Name == normalizedName {
			return ProjectReplica{}, fmt.Errorf("%w: replica %s for project %s already exists", ErrConflict, normalizedName, ref)
		}
	}
	reservation := replicaReservationForTier(tier)
	if err := s.validateOrgReplicaQuotaLocked(project.OrgID, reservation); err != nil {
		return ProjectReplica{}, err
	}
	if hostID != "" {
		host, ok := s.hosts[hostID]
		if !ok {
			return ProjectReplica{}, fmt.Errorf("%w: host %s", ErrNotFound, hostID)
		}
		if !hostHasCapacity(host, reservation) {
			return ProjectReplica{}, fmt.Errorf("%w: host %s has insufficient capacity for %s replica tier", ErrConflict, hostID, tier)
		}
		host.Used = addHostCapacity(host.Used, reservation)
		s.hosts[host.ID] = host
	}
	now := time.Now().UTC()
	internalReadURI := fmt.Sprintf("postgres://postgres:${DB_PASSWORD}@%s.%s.replica.internal:5432/postgres", normalizedName, ref)
	publicReadURI := postgresURIWithSSLMode(fmt.Sprintf("postgres://postgres:${DB_PASSWORD}@%s:5432/postgres", replicaDatabaseHost(ref, normalizedName, project.Spec.Domain)), "require")
	replica := ProjectReplica{
		ID:               newID(),
		ProjectRef:       ref,
		Name:             normalizedName,
		HostID:           hostID,
		Region:           region,
		Tier:             tier,
		Status:           "provisioning",
		Role:             "read",
		Message:          "replica accepted for provisioning",
		ReadURI:          publicReadURI,
		PublicReadURI:    publicReadURI,
		InternalReadURI:  internalReadURI,
		ReadWeight:       readWeight,
		FailoverPriority: failoverPriority,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	s.replicas[ref] = append(s.replicas[ref], replica)
	return replica, nil
}

func (s *MemoryStore) ListProjectReplicas(ctx context.Context, ref string) ([]ProjectReplica, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ref = strings.ToLower(strings.TrimSpace(ref))
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	replicas := append([]ProjectReplica(nil), s.replicas[ref]...)
	sort.Slice(replicas, func(i, j int) bool {
		return replicas[i].CreatedAt.Before(replicas[j].CreatedAt)
	})
	if replicas == nil {
		replicas = []ProjectReplica{}
	}
	return replicas, nil
}

func (s *MemoryStore) UpdateProjectReplicaStatus(ctx context.Context, ref string, replicaID string, status string, message string) (ProjectReplica, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	replicaID = strings.TrimSpace(replicaID)
	status = strings.ToLower(strings.TrimSpace(status))
	if replicaID == "" {
		return ProjectReplica{}, fmt.Errorf("replica id is required")
	}
	if status == "" {
		return ProjectReplica{}, fmt.Errorf("replica status is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	replicas := s.replicas[ref]
	for index, replica := range replicas {
		if replica.ID == replicaID {
			replica.Status = status
			if replica.Role == "" {
				replica.Role = "read"
			}
			if replica.ReadWeight <= 0 {
				replica.ReadWeight = 100
			}
			if replica.FailoverPriority <= 0 {
				replica.FailoverPriority = index + 1
			}
			replica.Message = strings.TrimSpace(message)
			replica.UpdatedAt = time.Now().UTC()
			replicas[index] = replica
			s.replicas[ref] = replicas
			return replica, nil
		}
	}
	return ProjectReplica{}, fmt.Errorf("%w: replica %s for project %s", ErrNotFound, replicaID, ref)
}

func (s *MemoryStore) DeleteProjectReplica(ctx context.Context, ref string, replicaID string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	replicaID = strings.TrimSpace(replicaID)
	if replicaID == "" {
		return fmt.Errorf("replica id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	replicas := s.replicas[ref]
	filtered := replicas[:0]
	removed := false
	var removedReplica ProjectReplica
	for _, replica := range replicas {
		if replica.ID != replicaID {
			filtered = append(filtered, replica)
			continue
		}
		if replica.Role == "primary" {
			return fmt.Errorf("%w: promoted primary replica %s cannot be deleted", ErrConflict, replicaID)
		}
		removed = true
		removedReplica = replica
	}
	if !removed {
		return fmt.Errorf("%w: replica %s for project %s", ErrNotFound, replicaID, ref)
	}
	if removedReplica.HostID != "" {
		if host, ok := s.hosts[removedReplica.HostID]; ok {
			host.Used = subtractHostCapacity(host.Used, replicaReservationForTier(removedReplica.Tier))
			s.hosts[host.ID] = host
		}
	}
	if len(filtered) == 0 {
		delete(s.replicas, ref)
	} else {
		s.replicas[ref] = append([]ProjectReplica(nil), filtered...)
	}
	return nil
}

func (s *MemoryStore) GetProjectReplicaRouting(ctx context.Context, ref string) (ProjectReplicaRouting, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ref = strings.ToLower(strings.TrimSpace(ref))
	project, ok := s.projects[ref]
	if !ok {
		return ProjectReplicaRouting{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return s.projectReplicaRoutingLocked(project, s.replicas[ref]), nil
}

func (s *MemoryStore) PromoteProjectReplica(ctx context.Context, ref string, replicaID string, reason string) (ProjectReplica, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	replicaID = strings.TrimSpace(replicaID)
	if replicaID == "" {
		return ProjectReplica{}, fmt.Errorf("replica id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectReplica{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	now := time.Now().UTC()
	replicas := s.replicas[ref]
	for index, replica := range replicas {
		if replica.ID != replicaID {
			continue
		}
		if replica.Status != "healthy" {
			return ProjectReplica{}, fmt.Errorf("%w: replica %s for project %s is not healthy", ErrConflict, replicaID, ref)
		}
		for otherIndex := range replicas {
			if replicas[otherIndex].Role == "primary" {
				replicas[otherIndex].Role = "read"
				replicas[otherIndex].UpdatedAt = now
			}
		}
		replica.Role = "primary"
		replica.Message = defaultReplicaMessage(strings.TrimSpace(reason), "replica promoted for failover")
		replica.PromotedAt = &now
		replica.UpdatedAt = now
		replicas[index] = replica
		project.Status = ProjectHealthy
		project.Message = "replica " + replica.Name + " promoted"
		project.UpdatedAt = now
		s.projects[ref] = project
		s.replicas[ref] = replicas
		return replica, nil
	}
	return ProjectReplica{}, fmt.Errorf("%w: replica %s for project %s", ErrNotFound, replicaID, ref)
}

func (s *MemoryStore) FailoverProjectReplica(ctx context.Context, ref string, reason string) (ProjectReplica, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectReplica{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	replicas := s.replicas[ref]
	candidateIndex := -1
	for index, replica := range replicas {
		if replica.Status != "healthy" {
			continue
		}
		if replica.Role == "primary" {
			continue
		}
		if candidateIndex == -1 || compareReplicaFailoverCandidate(replica, replicas[candidateIndex]) < 0 {
			candidateIndex = index
		}
	}
	if candidateIndex == -1 {
		return ProjectReplica{}, fmt.Errorf("%w: project %s has no healthy failover candidate", ErrConflict, ref)
	}
	now := time.Now().UTC()
	for index := range replicas {
		if replicas[index].Role == "primary" {
			replicas[index].Role = "read"
			replicas[index].UpdatedAt = now
		}
	}
	candidate := replicas[candidateIndex]
	candidate.Role = "primary"
	candidate.Message = defaultReplicaMessage(strings.TrimSpace(reason), "automatic failover candidate promoted")
	candidate.PromotedAt = &now
	candidate.UpdatedAt = now
	replicas[candidateIndex] = candidate
	project.Status = ProjectHealthy
	project.Message = "replica " + candidate.Name + " promoted"
	project.UpdatedAt = now
	s.projects[ref] = project
	s.replicas[ref] = replicas
	return candidate, nil
}
