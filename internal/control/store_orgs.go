package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *MemoryStore) GetPlatformDefaults(ctx context.Context) (PlatformDefaults, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizedPlatformDefaults(s.platformDefaults), nil
}

func (s *MemoryStore) UpdatePlatformDefaults(ctx context.Context, input PlatformDefaultsInput) (PlatformDefaults, error) {
	defaults, err := normalizePlatformDefaults(input)
	if err != nil {
		return PlatformDefaults{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.platformDefaults = defaults
	return defaults, nil
}

func (s *MemoryStore) GetPlatformSSOConfig(ctx context.Context) (PlatformSSOConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizedPlatformSSOConfig(s.platformSSO), nil
}

func (s *MemoryStore) UpdatePlatformSSOConfig(ctx context.Context, input PlatformSSOConfigInput) (PlatformSSOConfig, error) {
	config, err := normalizePlatformSSOInput(input)
	if err != nil {
		return PlatformSSOConfig{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(input.SCIMToken) == "" {
		config.SCIMTokenHash = s.platformSSO.SCIMTokenHash
		config.SCIMTokenConfigured = config.SCIMTokenHash != ""
	}
	s.platformSSO = config
	return normalizedPlatformSSOConfig(config), nil
}

func (s *MemoryStore) CreateOrg(ctx context.Context, name string) (Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Org{}, fmt.Errorf("name is required")
	}

	now := time.Now().UTC()
	org := Org{ID: newID(), Name: name, FeatureFlagOverrides: map[string]bool{}, CreatedAt: now}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.orgs[org.ID] = org
	return orgWithFeatureFlags(org, normalizedPlatformDefaults(s.platformDefaults)), nil
}

func (s *MemoryStore) UpdateOrg(ctx context.Context, id string, name string) (Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Org{}, fmt.Errorf("name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	org, ok := s.orgs[id]
	if !ok {
		return Org{}, fmt.Errorf("%w: org %s", ErrNotFound, id)
	}
	org.Name = name
	s.orgs[id] = org
	return orgWithFeatureFlags(org, normalizedPlatformDefaults(s.platformDefaults)), nil
}

func (s *MemoryStore) DeleteOrg(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[id]; !ok {
		return fmt.Errorf("%w: org %s", ErrNotFound, id)
	}
	for _, project := range s.projects {
		if project.OrgID == id {
			return fmt.Errorf("%w: org %s still has projects", ErrConflict, id)
		}
	}
	for _, team := range s.teams[id] {
		delete(s.teamMembers, team.ID)
	}
	delete(s.orgs, id)
	delete(s.orgQuotas, id)
	delete(s.usageSnapshots, id)
	delete(s.billingInvoices, id)
	delete(s.memberships, id)
	delete(s.teams, id)
	return nil
}

func (s *MemoryStore) GetOrg(ctx context.Context, id string) (Org, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	org, ok := s.orgs[id]
	if !ok {
		return Org{}, fmt.Errorf("%w: org %s", ErrNotFound, id)
	}
	return orgWithFeatureFlags(org, normalizedPlatformDefaults(s.platformDefaults)), nil
}

func (s *MemoryStore) ListOrgs(ctx context.Context) ([]Org, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	orgs := make([]Org, 0, len(s.orgs))
	defaults := normalizedPlatformDefaults(s.platformDefaults)
	for _, org := range s.orgs {
		orgs = append(orgs, orgWithFeatureFlags(org, defaults))
	}
	sort.Slice(orgs, func(i, j int) bool {
		return orgs[i].CreatedAt.Before(orgs[j].CreatedAt)
	})
	return orgs, nil
}

func (s *MemoryStore) GetOrgFeatureFlags(ctx context.Context, orgID string) (OrgFeatureFlags, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	org, ok := s.orgs[orgID]
	if !ok {
		return OrgFeatureFlags{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	return orgFeatureFlags(orgID, org.FeatureFlagOverrides, normalizedPlatformDefaults(s.platformDefaults)), nil
}

func (s *MemoryStore) UpdateOrgFeatureFlags(ctx context.Context, orgID string, input OrgFeatureFlagsInput) (OrgFeatureFlags, error) {
	overrides, err := normalizeOrgFeatureOverrides(input.Overrides)
	if err != nil {
		return OrgFeatureFlags{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	org, ok := s.orgs[orgID]
	if !ok {
		return OrgFeatureFlags{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	org.FeatureFlagOverrides = overrides
	s.orgs[orgID] = org
	return orgFeatureFlags(orgID, overrides, normalizedPlatformDefaults(s.platformDefaults)), nil
}

func (s *MemoryStore) GetOrgQuota(ctx context.Context, orgID string) (OrgQuota, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return OrgQuota{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	return s.orgQuotaLocked(orgID), nil
}

func (s *MemoryStore) UpdateOrgQuota(ctx context.Context, orgID string, input OrgQuotaInput) (OrgQuota, error) {
	if input.MaxProjects < 0 || input.MaxCPU < 0 || input.MaxRAMMB < 0 || input.MaxDiskGB < 0 {
		return OrgQuota{}, fmt.Errorf("quota limits cannot be negative")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return OrgQuota{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	quota := OrgQuota{
		OrgID:       orgID,
		MaxProjects: input.MaxProjects,
		MaxCPU:      input.MaxCPU,
		MaxRAMMB:    input.MaxRAMMB,
		MaxDiskGB:   input.MaxDiskGB,
		UpdatedAt:   time.Now().UTC(),
	}
	s.orgQuotas[orgID] = quota
	return s.orgQuotaLocked(orgID), nil
}

func (s *MemoryStore) GetOrgUsage(ctx context.Context, orgID string) (OrgUsage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return OrgUsage{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	return s.orgMeteringLocked(orgID, time.Now().UTC()), nil
}

func (s *MemoryStore) ListOrgUsageSnapshots(ctx context.Context, orgID string, limit int) ([]UsageSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	snapshots := cloneUsageSnapshots(s.usageSnapshots[orgID])
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].SampledAt.After(snapshots[j].SampledAt)
	})
	if len(snapshots) > limit {
		snapshots = snapshots[:limit]
	}
	return snapshots, nil
}

func (s *MemoryStore) CreateOrgUsageSnapshot(ctx context.Context, orgID string) (UsageSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return UsageSnapshot{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	sampledAt := time.Now().UTC()
	usage := s.orgMeteringLocked(orgID, sampledAt)
	snapshot := UsageSnapshot{
		ID:        newID(),
		OrgID:     orgID,
		Metrics:   cloneOrgUsage(usage),
		SampledAt: sampledAt,
	}
	s.usageSnapshots[orgID] = append(s.usageSnapshots[orgID], snapshot)
	return cloneUsageSnapshot(snapshot), nil
}

func (s *MemoryStore) ListBillingInvoices(ctx context.Context, orgID string, limit int) ([]BillingInvoice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	invoices := cloneBillingInvoices(s.billingInvoices[orgID])
	sort.Slice(invoices, func(i, j int) bool {
		return invoices[i].CreatedAt.After(invoices[j].CreatedAt)
	})
	if len(invoices) > limit {
		invoices = invoices[:limit]
	}
	return invoices, nil
}

func (s *MemoryStore) GetBillingInvoice(ctx context.Context, orgID string, invoiceID string) (BillingInvoice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return BillingInvoice{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	for _, invoice := range s.billingInvoices[orgID] {
		if invoice.ID == invoiceID {
			return cloneBillingInvoice(invoice), nil
		}
	}
	return BillingInvoice{}, fmt.Errorf("%w: billing invoice %s", ErrNotFound, invoiceID)
}

func (s *MemoryStore) CreateBillingInvoice(ctx context.Context, orgID string, input BillingInvoiceInput) (BillingInvoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return BillingInvoice{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "draft"
	}
	if status != "draft" && status != "open" && status != "void" && status != "paid" {
		return BillingInvoice{}, fmt.Errorf("billing invoice status must be draft, open, void, or paid")
	}
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return BillingInvoice{}, fmt.Errorf("billing invoice currency must be a three-letter code")
	}
	dueDays := input.DueDays
	if dueDays <= 0 {
		dueDays = 30
	}
	if dueDays > 365 {
		return BillingInvoice{}, fmt.Errorf("billing invoice due_days cannot exceed 365")
	}

	snapshot, ok := s.usageSnapshotForInvoiceLocked(orgID, input.UsageSnapshotID)
	if !ok {
		return BillingInvoice{}, fmt.Errorf("%w: usage snapshot %s", ErrNotFound, input.UsageSnapshotID)
	}
	if snapshot.ID == "" {
		sampledAt := time.Now().UTC()
		snapshot = UsageSnapshot{
			ID:        newID(),
			OrgID:     orgID,
			Metrics:   cloneOrgUsage(s.orgMeteringLocked(orgID, sampledAt)),
			SampledAt: sampledAt,
		}
		s.usageSnapshots[orgID] = append(s.usageSnapshots[orgID], snapshot)
	}

	lineItems := billingLineItemsForUsage(snapshot.Metrics)
	total := int64(0)
	for _, item := range lineItems {
		total += item.AmountCents
	}
	periodStart := time.Date(snapshot.SampledAt.Year(), snapshot.SampledAt.Month(), 1, 0, 0, 0, 0, time.UTC)
	now := time.Now().UTC()
	invoice := BillingInvoice{
		ID:              newID(),
		OrgID:           orgID,
		UsageSnapshotID: snapshot.ID,
		Number:          fmt.Sprintf("SDP-%s-%04d", snapshot.SampledAt.Format("200601"), len(s.billingInvoices[orgID])+1),
		Status:          status,
		Currency:        currency,
		PeriodStart:     periodStart,
		PeriodEnd:       snapshot.SampledAt,
		DueAt:           now.AddDate(0, 0, dueDays),
		SubtotalCents:   total,
		TotalCents:      total,
		LineItems:       lineItems,
		Metrics:         cloneOrgUsage(snapshot.Metrics),
		CreatedAt:       now,
	}
	s.billingInvoices[orgID] = append(s.billingInvoices[orgID], invoice)
	return cloneBillingInvoice(invoice), nil
}

func (s *MemoryStore) usageSnapshotForInvoiceLocked(orgID string, snapshotID string) (UsageSnapshot, bool) {
	snapshotID = strings.TrimSpace(snapshotID)
	snapshots := s.usageSnapshots[orgID]
	if snapshotID == "" {
		if len(snapshots) == 0 {
			return UsageSnapshot{}, true
		}
		latest := snapshots[0]
		for _, snapshot := range snapshots[1:] {
			if snapshot.SampledAt.After(latest.SampledAt) {
				latest = snapshot
			}
		}
		return cloneUsageSnapshot(latest), true
	}
	for _, snapshot := range snapshots {
		if snapshot.ID == snapshotID {
			return cloneUsageSnapshot(snapshot), true
		}
	}
	return UsageSnapshot{}, false
}

func (s *MemoryStore) orgMeteringLocked(orgID string, sampledAt time.Time) OrgUsage {
	projectRefs := map[string]struct{}{}
	usage := OrgUsage{
		OrgID:            orgID,
		ProjectsByStatus: map[string]int{},
		SampledAt:        sampledAt,
	}
	for _, project := range s.projects {
		if project.OrgID != orgID {
			continue
		}
		projectRefs[project.Ref] = struct{}{}
		usage.Resources = addHostCapacity(usage.Resources, resourceReservationForSpec(project.Spec))
		usage.ProjectsByStatus[string(project.Status)]++
		s.addRegisteredProjectChildOrgUsageLocked(project.Ref, &usage)
		usage.DatabaseExtensions += countEnabledDatabaseExtensions(project.Ref, s.databaseExtensions[project.Ref])
		if policy, ok := s.cdnPolicies[project.Ref]; ok && policy.Enabled {
			usage.CDNEnabledProjects++
		}
	}
	for ref, replicas := range s.replicas {
		project, ok := s.projects[ref]
		if !ok || project.OrgID != orgID {
			continue
		}
		usage.ReadReplicas += len(replicas)
		for _, replica := range replicas {
			usage.Resources = addHostCapacity(usage.Resources, replicaReservationForTier(replica.Tier))
		}
	}
	for _, backup := range s.backups {
		if _, ok := projectRefs[backup.ProjectRef]; !ok {
			continue
		}
		usage.BackupCount++
		usage.BackupStorageBytes += backup.SizeBytes
	}
	for _, archive := range s.walArchives {
		if _, ok := projectRefs[archive.ProjectRef]; !ok {
			continue
		}
		usage.WALArchives++
		usage.WALArchiveBytes += archive.SizeBytes
	}
	for _, log := range s.projectLogs {
		if _, ok := projectRefs[log.ProjectRef]; ok {
			usage.ProjectLogEvents++
		}
	}
	usage.DBAllocatedBytes = int64(usage.Resources.DiskGB) * 1024 * 1024 * 1024
	usage.StorageBytes = usage.BackupStorageBytes + usage.WALArchiveBytes
	return usage
}

func billingLineItemsForUsage(usage OrgUsage) []BillingLineItem {
	items := []BillingLineItem{
		billingLineItem("projects", "Dedicated Supabase projects", int64(usage.Resources.Project), "project", 2000),
		billingLineItem("cpu", "Allocated vCPU", int64(usage.Resources.CPU), "vCPU", 500),
		billingLineItem("ram", "Allocated RAM", int64(usage.Resources.RAMMB+1023)/1024, "GB", 100),
		billingLineItem("disk", "Allocated database disk", int64(usage.Resources.DiskGB), "GB", 10),
		billingLineItem("storage", "Object storage", bytesToBillableGiB(usage.StorageBytes), "GB", 2),
		billingLineItem("egress", "Network egress", bytesToBillableGiB(usage.EgressBytes), "GB", 9),
		billingLineItem("function_invocations", "Edge Function invocations", (usage.FunctionInvocations+99999)/100000, "100k calls", 20),
		billingLineItem("auth_maus", "Auth monthly active users", int64(usage.AuthMAUs), "MAU", 1),
	}
	out := make([]BillingLineItem, 0, len(items))
	for _, item := range items {
		if item.Quantity > 0 {
			out = append(out, item)
		}
	}
	return out
}

func billingLineItem(key string, description string, quantity int64, unit string, unitPriceCents int64) BillingLineItem {
	if quantity < 0 {
		quantity = 0
	}
	return BillingLineItem{
		Key:            key,
		Description:    description,
		Quantity:       quantity,
		Unit:           unit,
		UnitPriceCents: unitPriceCents,
		AmountCents:    quantity * unitPriceCents,
	}
}

func bytesToBillableGiB(value int64) int64 {
	if value <= 0 {
		return 0
	}
	const gib = int64(1024 * 1024 * 1024)
	return (value + gib - 1) / gib
}

func (s *MemoryStore) GetOrgAccessReview(ctx context.Context, orgID string) (OrgAccessReview, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return OrgAccessReview{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}

	members := make([]Membership, 0, len(s.memberships[orgID]))
	membersByEmail := map[string]Membership{}
	for _, member := range s.memberships[orgID] {
		members = append(members, member)
		membersByEmail[member.Email] = member
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].Email < members[j].Email
	})

	teams := make([]TeamAccessReview, 0, len(s.teams[orgID]))
	for _, team := range s.teams[orgID] {
		teamMembers := make([]TeamMember, 0, len(s.teamMembers[team.ID]))
		for _, member := range s.teamMembers[team.ID] {
			teamMembers = append(teamMembers, member)
		}
		sort.Slice(teamMembers, func(i, j int) bool {
			return teamMembers[i].Email < teamMembers[j].Email
		})
		teams = append(teams, TeamAccessReview{Team: team, Members: teamMembers})
	}
	sort.Slice(teams, func(i, j int) bool {
		return teams[i].Team.Name < teams[j].Team.Name
	})

	projects := make([]ProjectAccessReview, 0)
	for _, project := range s.projects {
		if project.OrgID != orgID {
			continue
		}
		effective := map[string]EffectiveProjectRole{}
		for _, member := range members {
			effective[member.Email] = EffectiveProjectRole{
				UserID:  member.UserID,
				Email:   member.Email,
				Role:    member.Role,
				Sources: []string{"org:" + member.Role},
			}
		}
		grants := append([]ProjectAccessGrant(nil), s.projectAccess[project.Ref]...)
		sort.Slice(grants, func(i, j int) bool {
			if grants[i].SubjectType == grants[j].SubjectType {
				return grants[i].SubjectName < grants[j].SubjectName
			}
			return grants[i].SubjectType < grants[j].SubjectType
		})
		for _, grant := range grants {
			switch grant.SubjectType {
			case "user":
				for _, member := range members {
					if member.UserID == grant.SubjectID {
						mergeEffectiveRole(effective, member.UserID, member.Email, grant.Role, "project:user:"+grant.Role)
						break
					}
				}
			case "team":
				for _, member := range s.teamMembers[grant.SubjectID] {
					if member.OrgID == orgID {
						mergeEffectiveRole(effective, member.UserID, member.Email, grant.Role, "project:team:"+grant.SubjectName+":"+grant.Role)
					}
				}
			}
		}
		effectiveRoles := make([]EffectiveProjectRole, 0, len(effective))
		for _, role := range effective {
			effectiveRoles = append(effectiveRoles, role)
		}
		sort.Slice(effectiveRoles, func(i, j int) bool {
			return effectiveRoles[i].Email < effectiveRoles[j].Email
		})
		projects = append(projects, ProjectAccessReview{
			ProjectRef:  project.Ref,
			ProjectName: project.Name,
			Grants:      grants,
			Effective:   effectiveRoles,
		})
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].ProjectRef < projects[j].ProjectRef
	})

	return OrgAccessReview{
		OrgID:       orgID,
		Members:     members,
		Teams:       teams,
		Projects:    projects,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func (s *MemoryStore) ListOrgMembers(ctx context.Context, orgID string) ([]Membership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	members := make([]Membership, 0, len(s.memberships[orgID]))
	for _, member := range s.memberships[orgID] {
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].CreatedAt.Before(members[j].CreatedAt)
	})
	return members, nil
}

func (s *MemoryStore) UpsertOrgMember(ctx context.Context, orgID string, input MembershipInput) (Membership, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" {
		return Membership{}, fmt.Errorf("member email is required")
	}
	role, err := normalizeMembershipRole(input.Role)
	if err != nil {
		return Membership{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return Membership{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	user, ok := s.users[email]
	if !ok {
		user = User{
			ID:           newID(),
			Email:        email,
			PasswordHash: hashPassword(randomToken("invite", 24)),
			Role:         "member",
			CreatedAt:    time.Now().UTC(),
		}
		s.users[email] = user
	}
	if s.memberships[orgID] == nil {
		s.memberships[orgID] = map[string]Membership{}
	}
	member := s.memberships[orgID][email]
	if member.CreatedAt.IsZero() {
		member.CreatedAt = time.Now().UTC()
	}
	member.UserID = user.ID
	member.OrgID = orgID
	member.Email = email
	member.Role = role
	s.memberships[orgID][email] = member
	return member, nil
}

func (s *MemoryStore) DeleteOrgMember(ctx context.Context, orgID string, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return fmt.Errorf("member email is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	if _, ok := s.memberships[orgID][email]; !ok {
		return fmt.Errorf("%w: member %s for org %s", ErrNotFound, email, orgID)
	}
	userID := s.memberships[orgID][email].UserID
	delete(s.memberships[orgID], email)
	for teamID, members := range s.teamMembers {
		if member, ok := members[email]; ok && member.OrgID == orgID {
			delete(members, email)
			s.teamMembers[teamID] = members
		}
	}
	for ref, grants := range s.projectAccess {
		project, ok := s.projects[ref]
		if !ok || project.OrgID != orgID {
			continue
		}
		filtered := grants[:0]
		for _, grant := range grants {
			if grant.SubjectType == "user" && grant.SubjectID == userID {
				continue
			}
			filtered = append(filtered, grant)
		}
		s.projectAccess[ref] = append([]ProjectAccessGrant(nil), filtered...)
	}
	return nil
}

func (s *MemoryStore) ListOrgTeams(ctx context.Context, orgID string) ([]Team, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	teams := make([]Team, 0, len(s.teams[orgID]))
	for _, team := range s.teams[orgID] {
		teams = append(teams, team)
	}
	sort.Slice(teams, func(i, j int) bool {
		return teams[i].Name < teams[j].Name
	})
	return teams, nil
}

func (s *MemoryStore) CreateOrgTeam(ctx context.Context, orgID string, input TeamInput) (Team, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Team{}, fmt.Errorf("team name is required")
	}
	slug := normalizeTeamSlug(input.Slug)
	if slug == "" {
		slug = normalizeTeamSlug(name)
	}
	if !teamSlugPattern.MatchString(slug) {
		return Team{}, fmt.Errorf("team slug must be 2-64 lowercase letters, numbers, or hyphens")
	}

	team := Team{
		ID:        newID(),
		OrgID:     orgID,
		Name:      name,
		Slug:      slug,
		CreatedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return Team{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	if s.teams[orgID] == nil {
		s.teams[orgID] = map[string]Team{}
	}
	if _, ok := s.teams[orgID][slug]; ok {
		return Team{}, fmt.Errorf("%w: team %s already exists", ErrConflict, slug)
	}
	s.teams[orgID][slug] = team
	return team, nil
}

func (s *MemoryStore) DeleteOrgTeam(ctx context.Context, orgID string, slug string) error {
	slug = normalizeTeamSlug(slug)
	s.mu.Lock()
	defer s.mu.Unlock()
	team, ok := s.teams[orgID][slug]
	if !ok {
		return fmt.Errorf("%w: team %s for org %s", ErrNotFound, slug, orgID)
	}
	delete(s.teams[orgID], slug)
	delete(s.teamMembers, team.ID)
	for ref, grants := range s.projectAccess {
		project, ok := s.projects[ref]
		if !ok || project.OrgID != orgID {
			continue
		}
		filtered := grants[:0]
		for _, grant := range grants {
			if grant.SubjectType == "team" && grant.SubjectID == team.ID {
				continue
			}
			filtered = append(filtered, grant)
		}
		s.projectAccess[ref] = append([]ProjectAccessGrant(nil), filtered...)
	}
	return nil
}

func (s *MemoryStore) ListTeamMembers(ctx context.Context, orgID string, slug string) ([]TeamMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	team, ok := s.teams[orgID][normalizeTeamSlug(slug)]
	if !ok {
		return nil, fmt.Errorf("%w: team %s for org %s", ErrNotFound, slug, orgID)
	}
	members := make([]TeamMember, 0, len(s.teamMembers[team.ID]))
	for _, member := range s.teamMembers[team.ID] {
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].Email < members[j].Email
	})
	return members, nil
}

func (s *MemoryStore) UpsertTeamMember(ctx context.Context, orgID string, slug string, input TeamMemberInput) (TeamMember, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" {
		return TeamMember{}, fmt.Errorf("team member email is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	team, ok := s.teams[orgID][normalizeTeamSlug(slug)]
	if !ok {
		return TeamMember{}, fmt.Errorf("%w: team %s for org %s", ErrNotFound, slug, orgID)
	}
	user, ok := s.users[email]
	if !ok {
		user = User{
			ID:           newID(),
			Email:        email,
			PasswordHash: hashPassword(randomToken("invite", 24)),
			Role:         "member",
			CreatedAt:    time.Now().UTC(),
		}
		s.users[email] = user
	}
	if s.memberships[orgID] == nil {
		s.memberships[orgID] = map[string]Membership{}
	}
	if _, ok := s.memberships[orgID][email]; !ok {
		s.memberships[orgID][email] = Membership{
			UserID:    user.ID,
			OrgID:     orgID,
			Email:     email,
			Role:      "viewer",
			CreatedAt: time.Now().UTC(),
		}
	}
	if s.teamMembers[team.ID] == nil {
		s.teamMembers[team.ID] = map[string]TeamMember{}
	}
	member := s.teamMembers[team.ID][email]
	if member.CreatedAt.IsZero() {
		member.CreatedAt = time.Now().UTC()
	}
	member.TeamID = team.ID
	member.OrgID = orgID
	member.TeamSlug = team.Slug
	member.UserID = user.ID
	member.Email = email
	s.teamMembers[team.ID][email] = member
	return member, nil
}

func (s *MemoryStore) DeleteTeamMember(ctx context.Context, orgID string, slug string, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return fmt.Errorf("team member email is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	team, ok := s.teams[orgID][normalizeTeamSlug(slug)]
	if !ok {
		return fmt.Errorf("%w: team %s for org %s", ErrNotFound, slug, orgID)
	}
	if _, ok := s.teamMembers[team.ID][email]; !ok {
		return fmt.Errorf("%w: team member %s", ErrNotFound, email)
	}
	delete(s.teamMembers[team.ID], email)
	return nil
}

func (s *MemoryStore) ListProjectAccess(ctx context.Context, ref string) ([]ProjectAccessGrant, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.projectAccess[ref], func(grants []ProjectAccessGrant) []ProjectAccessGrant {
		return append([]ProjectAccessGrant(nil), grants...)
	}, func(left, right ProjectAccessGrant) bool {
		if left.SubjectType == right.SubjectType {
			return left.SubjectName < right.SubjectName
		}
		return left.SubjectType < right.SubjectType
	}), nil
}

func (s *MemoryStore) UpsertProjectAccess(ctx context.Context, ref string, input ProjectAccessInput) (ProjectAccessGrant, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	role, err := normalizeMembershipRole(input.Role)
	if err != nil {
		return ProjectAccessGrant{}, err
	}
	subjectType := strings.ToLower(strings.TrimSpace(input.SubjectType))
	subjectID := strings.TrimSpace(input.SubjectID)
	if subjectID == "" {
		return ProjectAccessGrant{}, fmt.Errorf("subject id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectAccessGrant{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	subjectName := subjectID
	switch subjectType {
	case "user":
		email := strings.ToLower(subjectID)
		user, ok := s.users[email]
		if !ok {
			return ProjectAccessGrant{}, fmt.Errorf("%w: user %s", ErrNotFound, email)
		}
		member, ok := s.memberships[project.OrgID][email]
		if !ok {
			return ProjectAccessGrant{}, fmt.Errorf("%w: member %s for org %s", ErrNotFound, email, project.OrgID)
		}
		subjectID = user.ID
		subjectName = member.Email
	case "team":
		team, ok := s.teams[project.OrgID][normalizeTeamSlug(subjectID)]
		if !ok {
			return ProjectAccessGrant{}, fmt.Errorf("%w: team %s for org %s", ErrNotFound, subjectID, project.OrgID)
		}
		subjectID = team.ID
		subjectName = team.Name
	default:
		return ProjectAccessGrant{}, fmt.Errorf("subject type must be user or team")
	}
	grants := s.projectAccess[ref]
	for i, grant := range grants {
		if grant.SubjectType == subjectType && grant.SubjectID == subjectID {
			grant.Role = role
			grant.SubjectName = subjectName
			grants[i] = grant
			s.projectAccess[ref] = grants
			return grant, nil
		}
	}
	grant := ProjectAccessGrant{
		ID:          newID(),
		ProjectRef:  ref,
		OrgID:       project.OrgID,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		SubjectName: subjectName,
		Role:        role,
		CreatedAt:   time.Now().UTC(),
	}
	s.projectAccess[ref] = append(grants, grant)
	return grant, nil
}

func (s *MemoryStore) DeleteProjectAccess(ctx context.Context, ref string, subjectType string, subjectID string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	subjectType = strings.ToLower(strings.TrimSpace(subjectType))
	subjectID = strings.TrimSpace(subjectID)
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	normalizedSubjectID, err := s.resolveAccessSubjectIDLocked(project.OrgID, subjectType, subjectID)
	if err != nil {
		return err
	}
	grants := s.projectAccess[ref]
	filtered := grants[:0]
	removed := false
	for _, grant := range grants {
		if grant.SubjectType == subjectType && grant.SubjectID == normalizedSubjectID {
			removed = true
			continue
		}
		filtered = append(filtered, grant)
	}
	if !removed {
		return fmt.Errorf("%w: project access grant", ErrNotFound)
	}
	s.projectAccess[ref] = append([]ProjectAccessGrant(nil), filtered...)
	return nil
}

func (s *MemoryStore) ResolveProjectRole(ctx context.Context, ref string, email string) (string, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[ref]
	if !ok {
		return "", fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	user, ok := s.users[email]
	if !ok {
		return "", fmt.Errorf("%w: user %s", ErrNotFound, email)
	}
	best := ""
	for _, grant := range s.projectAccess[ref] {
		if grant.SubjectType == "user" && grant.SubjectID == user.ID {
			best = higherRole(best, grant.Role)
			continue
		}
		if grant.SubjectType != "team" {
			continue
		}
		for _, member := range s.teamMembers[grant.SubjectID] {
			if member.OrgID == project.OrgID && member.Email == email {
				best = higherRole(best, grant.Role)
				break
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("%w: project access for %s", ErrNotFound, email)
	}
	return best, nil
}
