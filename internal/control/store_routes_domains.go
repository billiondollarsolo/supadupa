package control

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

func (s *MemoryStore) UpsertProjectRoutes(ctx context.Context, ref string, routes []ProjectRoute) ([]ProjectRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	now := time.Now().UTC()
	normalized := make([]ProjectRoute, 0, len(routes))
	for _, route := range routes {
		if strings.TrimSpace(route.Name) == "" {
			return nil, fmt.Errorf("route name is required")
		}
		if strings.TrimSpace(route.FQDN) == "" {
			return nil, fmt.Errorf("route fqdn is required")
		}
		if strings.TrimSpace(route.UpstreamURL) == "" {
			return nil, fmt.Errorf("route upstream url is required")
		}
		if route.ID == "" {
			route.ID = newID()
		}
		route.ProjectRef = ref
		if route.CreatedAt.IsZero() {
			route.CreatedAt = now
		}
		route.IPAllowlist = append([]string(nil), route.IPAllowlist...)
		normalized = append(normalized, route)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Name < normalized[j].Name
	})
	s.routes[ref] = cloneProjectRoutes(normalized)
	return cloneProjectRoutes(normalized), nil
}

func (s *MemoryStore) ListProjectRoutes(ctx context.Context, ref string) ([]ProjectRoute, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneProjectRoutes(s.routes[ref]), nil
}

func (s *MemoryStore) DeleteProjectRoutes(ctx context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	delete(s.routes, ref)
	return nil
}

func (s *MemoryStore) ListProjectDomains(ctx context.Context, ref string) ([]ProjectDomain, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	domains := cloneProjectDomains(s.domains[ref])
	sort.Slice(domains, func(i, j int) bool {
		return domains[i].CreatedAt.Before(domains[j].CreatedAt)
	})
	return domains, nil
}

func (s *MemoryStore) AddProjectDomain(ctx context.Context, ref string, input ProjectDomainInput) (ProjectDomain, error) {
	fqdn, err := normalizeDomain(input.FQDN)
	if err != nil {
		return ProjectDomain{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectDomain{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if owner := s.reservedCustomDomainHostLocked(fqdn); owner != "" {
		return ProjectDomain{}, fmt.Errorf("%w: domain %s is reserved by %s", ErrConflict, fqdn, owner)
	}
	candidateRouteName := routeName(fqdn)
	for projectRef, domains := range s.domains {
		for _, existing := range domains {
			if existing.FQDN == fqdn {
				return ProjectDomain{}, fmt.Errorf("%w: domain %s already exists for project %s", ErrConflict, fqdn, projectRef)
			}
			if projectRef == ref && routeName(existing.FQDN) == candidateRouteName {
				return ProjectDomain{}, fmt.Errorf("%w: domain %s conflicts with route name for existing domain %s in project %s", ErrConflict, fqdn, existing.FQDN, ref)
			}
		}
	}
	now := time.Now().UTC()
	domain := ProjectDomain{
		ProjectRef: project.Ref,
		FQDN:       fqdn,
		CertStatus: "pending",
		CertMode:   "acme",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	s.domains[ref] = append(s.domains[ref], domain)
	return domain, nil
}

func (s *MemoryStore) reservedCustomDomainHostLocked(fqdn string) string {
	reserved := map[string]string{}
	for _, project := range s.projects {
		domain := strings.TrimSpace(project.Spec.Domain)
		addReservedDomainHost(reserved, domain, "project domain "+domain)
		addReservedDomainHost(reserved, projectHost(project.Ref, domain), "project API "+project.Ref)
		addReservedDomainHost(reserved, studioHost(project.Ref, domain), "project Studio "+project.Ref)
		addReservedDomainHost(reserved, storageHost(project.Ref, domain), "project storage "+project.Ref)
		addReservedDomainHost(reserved, databaseHost(project.Ref, domain), "project database "+project.Ref)
		addReservedDomainHost(reserved, poolerHost(project.Ref, domain), "project pooler "+project.Ref)
		for _, replica := range s.replicas[project.Ref] {
			addReservedDomainHost(reserved, replicaDatabaseHost(project.Ref, replica.Name, domain), "project replica "+project.Ref+"/"+replica.Name)
		}
		for _, host := range inferredPlatformHostsForProjectDomain(domain) {
			addReservedDomainHost(reserved, host, "platform host")
		}
	}
	for projectRef, domains := range s.domains {
		for _, domain := range domains {
			addReservedDomainHost(reserved, domain.FQDN, "project custom domain "+projectRef)
		}
	}
	addReservedDomainHost(reserved, os.Getenv("SUPADUPA_ADMIN_HOST"), "platform admin")
	addReservedDomainHost(reserved, os.Getenv("SUPADUPA_API_HOST"), "platform API")
	addReservedDomainHost(reserved, os.Getenv("SUPADUPA_ADMIN_URL"), "platform admin")
	addReservedDomainHost(reserved, os.Getenv("SUPADUPA_API_URL"), "platform API")
	return reserved[fqdn]
}

func (s *MemoryStore) validateGeneratedProjectHostReservationsLocked(ref string, domain string) error {
	domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
	for label, host := range generatedProjectHosts(ref, domain) {
		if owner := s.reservedCustomDomainHostLocked(host); owner != "" {
			return fmt.Errorf("%w: generated %s host %s is reserved by %s", ErrConflict, label, host, owner)
		}
	}
	inferredPlatformHosts := map[string]struct{}{}
	for _, host := range inferredPlatformHostsForProjectDomain(domain) {
		normalized, ok := normalizedHostForDomainReservation(host)
		if ok {
			inferredPlatformHosts[normalized] = struct{}{}
		}
	}
	for label, host := range generatedProjectHosts(ref, domain) {
		normalized, ok := normalizedHostForDomainReservation(host)
		if !ok {
			continue
		}
		if _, reserved := inferredPlatformHosts[normalized]; reserved {
			return fmt.Errorf("%w: generated %s host %s is reserved by platform host topology for %s", ErrConflict, label, host, domain)
		}
	}
	return nil
}

func addReservedDomainHost(out map[string]string, value string, owner string) {
	host, ok := normalizedHostForDomainReservation(value)
	if !ok {
		return
	}
	if _, exists := out[host]; !exists {
		out[host] = owner
	}
}

func inferredPlatformHostsForProjectDomain(domain string) []string {
	domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" {
		return nil
	}
	suffix := domain
	if strings.HasPrefix(domain, "apps.") {
		suffix = strings.TrimPrefix(domain, "apps.")
	}
	return []string{"admin." + suffix, "api." + suffix}
}

func normalizedHostForDomainReservation(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
		value = parsed.Hostname()
	} else {
		value = strings.Split(strings.TrimPrefix(strings.TrimPrefix(value, "https://"), "http://"), "/")[0]
		value = strings.Split(value, ":")[0]
	}
	host, err := normalizeDomain(value)
	if err != nil {
		return "", false
	}
	return host, true
}

func (s *MemoryStore) UpdateProjectDomainCertStatus(ctx context.Context, ref string, fqdn string, status string) (ProjectDomain, error) {
	normalized, err := normalizeDomain(fqdn)
	if err != nil {
		return ProjectDomain{}, err
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return ProjectDomain{}, fmt.Errorf("cert status is required")
	}
	switch status {
	case "pending", "issued", "failed", "uploaded":
	default:
		return ProjectDomain{}, fmt.Errorf("unsupported cert status %q", status)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDomain{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	domains := s.domains[ref]
	for index, domain := range domains {
		if domain.FQDN == normalized {
			domain.CertStatus = status
			domain.UpdatedAt = time.Now().UTC()
			domains[index] = domain
			s.domains[ref] = domains
			return domain, nil
		}
	}
	return ProjectDomain{}, fmt.Errorf("%w: domain %s for project %s", ErrNotFound, normalized, ref)
}

func (s *MemoryStore) UpdateProjectDomainCertificate(ctx context.Context, ref string, fqdn string, metadata ProjectDomainCertificateMetadata) (ProjectDomain, error) {
	normalized, err := normalizeDomain(fqdn)
	if err != nil {
		return ProjectDomain{}, err
	}
	status := strings.ToLower(strings.TrimSpace(metadata.Status))
	if status == "" {
		status = "uploaded"
	}
	switch status {
	case "pending", "issued", "failed", "uploaded":
	default:
		return ProjectDomain{}, fmt.Errorf("unsupported cert status %q", status)
	}
	mode := strings.ToLower(strings.TrimSpace(metadata.Mode))
	if mode == "" {
		mode = "byo"
	}
	switch mode {
	case "acme", "manual", "command", "byo":
	default:
		return ProjectDomain{}, fmt.Errorf("unsupported cert mode %q", mode)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDomain{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	domains := s.domains[ref]
	for index, domain := range domains {
		if domain.FQDN == normalized {
			domain.CertStatus = status
			domain.CertMode = mode
			domain.CertFingerprint = strings.TrimSpace(metadata.Fingerprint)
			domain.CertNotAfter = cloneTimePtr(metadata.NotAfter)
			domain.UpdatedAt = time.Now().UTC()
			domains[index] = domain
			s.domains[ref] = domains
			return domain, nil
		}
	}
	return ProjectDomain{}, fmt.Errorf("%w: domain %s for project %s", ErrNotFound, normalized, ref)
}

func (s *MemoryStore) DeleteProjectDomain(ctx context.Context, ref string, fqdn string) error {
	normalized, err := normalizeDomain(fqdn)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	domains := s.domains[ref]
	for index, domain := range domains {
		if domain.FQDN == normalized {
			s.domains[ref] = append(domains[:index], domains[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: domain %s for project %s", ErrNotFound, normalized, ref)
}
