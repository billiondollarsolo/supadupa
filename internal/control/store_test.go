package control

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCreateProjectRejectsRefsThatBreakPublicDNSLabels(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}

	valid55 := strings.Repeat("a", 55)
	if _, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: valid55, Name: "Valid"}); err != nil {
		t.Fatalf("expected 55-character ref to be valid: %v", err)
	}
	longAppsDomain := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63)
	if _, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: strings.Repeat("b", 55), Name: "Long Domain", Domain: longAppsDomain}); err == nil || !strings.Contains(err.Error(), "253-character DNS name limit") {
		t.Fatalf("expected generated project host length rejection, got %v", err)
	}

	for _, ref := range []string{
		"-bad",
		"bad-",
		strings.Repeat("a", 56),
		strings.Repeat("a", 57),
		strings.Repeat("a", 64),
	} {
		if _, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: ref, Name: "Invalid"}); err == nil || !strings.Contains(err.Error(), "ref must be 3-55") {
			t.Fatalf("expected DNS-safe ref rejection for %q, got %v", ref, err)
		}
	}
}

func TestCreateProjectAppliesExactSizingOverridesAndAccounting(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	host, err := store.CreateHost(ctx, CreateHostRequest{Name: "big", Address: "127.0.0.1", Capacity: HostCapacity{CPU: 64, RAMMB: 262144, DiskGB: 16384, Project: 50}})
	if err != nil {
		t.Fatal(err)
	}

	// Medium tier preset is 4 CPU / 8192 MB / 80 GB; override CPU and RAM only,
	// leaving disk to fall back to the preset.
	project, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID: org.ID, Ref: "sized-one", Name: "Sized", HostID: host.ID,
		ResourceTier: ResourceTierMedium, CPU: 6, RAMMB: 12288, EnforceLimits: true,
	})
	if err != nil {
		t.Fatalf("create with overrides failed: %v", err)
	}
	if project.Spec.CPU != 6 || project.Spec.RAMMB != 12288 || !project.Spec.EnforceLimits {
		t.Fatalf("spec did not carry overrides: %+v", project.Spec)
	}
	cpu, ramMB, diskGB := EffectiveResourceSizing(project.Spec)
	if cpu != 6 || ramMB != 12288 || diskGB != 80 {
		t.Fatalf("effective sizing = %d/%d/%d, want 6/12288/80", cpu, ramMB, diskGB)
	}
	hosts, err := store.ListHosts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hosts[0].Used.CPU != 6 || hosts[0].Used.RAMMB != 12288 || hosts[0].Used.DiskGB != 80 {
		t.Fatalf("host reservation = %+v, want CPU 6 / RAM 12288 / Disk 80", hosts[0].Used)
	}

	// Out-of-bounds overrides are rejected.
	if _, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "too-big", Name: "Too Big", HostID: host.ID, CPU: maxProjectCPU + 1}); err == nil || !strings.Contains(err.Error(), "cpu cannot exceed") {
		t.Fatalf("expected cpu bound rejection, got %v", err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "too-small", Name: "Too Small", HostID: host.ID, RAMMB: 64}); err == nil || !strings.Contains(err.Error(), "ram cannot be below") {
		t.Fatalf("expected ram floor rejection, got %v", err)
	}
}

func TestCreateProjectBranchRejectsRefsThatBreakPublicDNSLabels(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "source", Name: "Source"})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.CreateProjectBranch(ctx, source.Ref, ProjectBranchInput{Ref: strings.Repeat("b", 56), Name: "Too Long"}); err == nil || !strings.Contains(err.Error(), "branch ref must be 3-55") {
		t.Fatalf("expected DNS-safe branch ref rejection, got %v", err)
	}
}

func TestCreateProjectRejectsGeneratedHostReservations(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "admin",
		Name:   "Admin Collision",
		Domain: "example.com",
	}); err == nil || !strings.Contains(err.Error(), "platform host topology") {
		t.Fatalf("expected generated platform host collision, got %v", err)
	}

	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "apps.example.com",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddProjectDomain(ctx, "alpha", ProjectDomainInput{FQDN: "beta.apps.example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "beta",
		Name:   "Beta",
		Domain: "apps.example.com",
	}); err == nil || !strings.Contains(err.Error(), "project custom domain alpha") {
		t.Fatalf("expected generated host collision with existing custom domain, got %v", err)
	}
}

func TestCreateProjectBranchRejectsGeneratedHostReservations(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "source",
		Name:   "Source",
		Domain: "example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.CreateProjectBranch(ctx, source.Ref, ProjectBranchInput{Ref: "api", Name: "API Collision"}); err == nil || !strings.Contains(err.Error(), "platform host") {
		t.Fatalf("expected branch generated platform host collision, got %v", err)
	}
}

func TestProjectChildListsCloneSortAndReturnEmptySlices(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "child-lists", Name: "Child Lists"})
	if err != nil {
		t.Fatal(err)
	}

	emptyAnalytics, err := store.ListProjectAnalyticsBuckets(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if emptyAnalytics == nil || len(emptyAnalytics) != 0 {
		t.Fatalf("expected non-nil empty analytics bucket list, got %#v", emptyAnalytics)
	}
	emptyDrains, err := store.ListProjectLogDrains(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if emptyDrains == nil || len(emptyDrains) != 0 {
		t.Fatalf("expected non-nil empty log drain list, got %#v", emptyDrains)
	}

	older := time.Date(2026, 6, 8, 1, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	store.analyticsBuckets[project.Ref] = []ProjectAnalyticsBucket{
		{ID: "bucket-zeta", ProjectRef: project.Ref, Name: "zeta", Metadata: map[string]string{"tier": "hot"}, CreatedAt: newer, UpdatedAt: newer},
		{ID: "bucket-alpha", ProjectRef: project.Ref, Name: "alpha", Metadata: map[string]string{"tier": "cold"}, CreatedAt: older, UpdatedAt: older},
	}
	store.logDrains[project.Ref] = []LogDrain{
		{ID: "drain-new", ProjectRef: project.Ref, Target: "https", Config: map[string]string{"url": "https://new.example.com"}, CreatedAt: newer},
		{ID: "drain-old", ProjectRef: project.Ref, Target: "https", Config: map[string]string{"url": "https://old.example.com"}, CreatedAt: older},
	}

	analytics, err := store.ListProjectAnalyticsBuckets(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(analytics) != 2 || analytics[0].Name != "alpha" || analytics[1].Name != "zeta" {
		t.Fatalf("expected analytics buckets sorted by name, got %#v", analytics)
	}
	analytics[0].Metadata["tier"] = "mutated"
	analyticsAgain, err := store.ListProjectAnalyticsBuckets(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if analyticsAgain[0].Metadata["tier"] != "cold" {
		t.Fatalf("expected analytics bucket metadata to be cloned, got %#v", analyticsAgain[0].Metadata)
	}

	drains, err := store.ListProjectLogDrains(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(drains) != 2 || drains[0].ID != "drain-old" || drains[1].ID != "drain-new" {
		t.Fatalf("expected log drains sorted by creation time, got %#v", drains)
	}
	drains[0].Config["url"] = "https://mutated.example.com"
	drainsAgain, err := store.ListProjectLogDrains(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if drainsAgain[0].Config["url"] != "https://old.example.com" {
		t.Fatalf("expected log drain config to be cloned, got %#v", drainsAgain[0].Config)
	}
}

func TestCreateProjectReplicaRejectsPublicDNSLabelOverflow(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}

	validName := strings.Repeat("r", 46)
	if _, err := store.CreateProjectReplica(ctx, "alpha", ProjectReplicaInput{Name: validName}); err != nil {
		t.Fatalf("expected 63-character replica DNS label to be valid: %v", err)
	}
	for _, invalidName := range []string{"-east", "east-"} {
		if _, err := store.CreateProjectReplica(ctx, "alpha", ProjectReplicaInput{Name: invalidName}); err == nil || !strings.Contains(err.Error(), "cannot start or end with a dash") {
			t.Fatalf("expected replica DNS label rejection for %q, got %v", invalidName, err)
		}
	}
	tooLongName := strings.Repeat("r", 47)
	if _, err := store.CreateProjectReplica(ctx, "alpha", ProjectReplicaInput{Name: tooLongName}); err == nil || !strings.Contains(err.Error(), "63-character DNS label limit") {
		t.Fatalf("expected replica DNS label overflow rejection, got %v", err)
	}

	longRef := strings.Repeat("b", 49)
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    longRef,
		Name:   "Long Ref",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProjectReplica(ctx, longRef, ProjectReplicaInput{Name: "abc"}); err == nil || !strings.Contains(err.Error(), "too long for public read-replica DNS labels") {
		t.Fatalf("expected long project ref replica rejection, got %v", err)
	}

	replicaLongDomain := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 47)
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "short",
		Name:   "Long Replica Domain",
		Domain: replicaLongDomain,
	}); err != nil {
		t.Fatalf("expected long domain to still fit base generated project hosts: %v", err)
	}
	if _, err := store.CreateProjectReplica(ctx, "short", ProjectReplicaInput{Name: "east"}); err == nil || !strings.Contains(err.Error(), "253-character DNS name limit") {
		t.Fatalf("expected replica full FQDN length rejection, got %v", err)
	}
}

func TestAddProjectDomainRejectsGeneratedStorageHost(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "beta",
		Name:   "Beta",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}

	_, err = store.AddProjectDomain(ctx, "beta", ProjectDomainInput{FQDN: "storage-alpha.apps.supadupa.test"})
	if err == nil || !strings.Contains(err.Error(), "project storage alpha") {
		t.Fatalf("expected generated storage host reservation conflict, got %v", err)
	}
}

func TestAddProjectDomainRejectsGeneratedReplicaDatabaseHost(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "beta",
		Name:   "Beta",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProjectReplica(ctx, "alpha", ProjectReplicaInput{Name: "east"}); err != nil {
		t.Fatal(err)
	}

	_, err = store.AddProjectDomain(ctx, "beta", ProjectDomainInput{FQDN: "db-replica-east-alpha.apps.supadupa.test"})
	if err == nil || !strings.Contains(err.Error(), "project replica alpha/east") {
		t.Fatalf("expected generated replica host reservation conflict, got %v", err)
	}
}

func TestProjectChildResourceRegistryCountsAndCleansRegisteredResources(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "registry", Name: "Registry"})
	if err != nil {
		t.Fatal(err)
	}
	seedRegisteredProjectChildren(store, project.Ref)
	expectedDatabaseExtensions := registeredProjectDatabaseExtensionCount(store, project.Ref)

	projectMetrics, err := store.GetProjectMetrics(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	assertRegisteredProjectChildMetrics(t, projectMetrics, expectedDatabaseExtensions)
	if !projectMetrics.CDNEnabled {
		t.Fatalf("expected project CDN metric to be true")
	}

	fleetMetrics, err := store.GetFleetMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertRegisteredProjectChildFleetMetrics(t, fleetMetrics, expectedDatabaseExtensions)
	if fleetMetrics.CDNEnabledProjects != 1 {
		t.Fatalf("expected one CDN-enabled project, got %d", fleetMetrics.CDNEnabledProjects)
	}

	usage, err := store.GetOrgUsage(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertRegisteredProjectChildOrgUsage(t, usage, expectedDatabaseExtensions)
	if usage.CDNEnabledProjects != 1 {
		t.Fatalf("expected one CDN-enabled usage project, got %d", usage.CDNEnabledProjects)
	}

	if err := store.DeleteProject(ctx, project.Ref); err != nil {
		t.Fatal(err)
	}
	fleetMetrics, err = store.GetFleetMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fleetMetrics.Projects != 0 {
		t.Fatalf("expected deleted project to be removed from fleet metrics, got %d projects", fleetMetrics.Projects)
	}
	assertNoRegisteredProjectChildFleetMetrics(t, fleetMetrics)
	assertRegisteredProjectChildrenCleaned(t, store, project.Ref)
}

func seedRegisteredProjectChildren(store *MemoryStore, ref string) {
	now := time.Now().UTC()
	store.mu.Lock()
	defer store.mu.Unlock()

	store.routes[ref] = []ProjectRoute{{ID: "route", ProjectRef: ref, Name: "route", CreatedAt: now}}
	store.domains[ref] = []ProjectDomain{{ProjectRef: ref, FQDN: "registry.example.com", CreatedAt: now, UpdatedAt: now}}
	store.configs[ref] = map[string]ProjectConfig{"database": {ProjectRef: ref, Area: "database", Config: map[string]string{"ssl_enforced": "true"}, UpdatedAt: now}}
	store.authClients[ref] = []ProjectAuthClient{{ID: "client", ProjectRef: ref, Name: "client", CreatedAt: now, UpdatedAt: now}}
	store.authHooks[ref] = []ProjectAuthHook{{ID: "hook", ProjectRef: ref, HookType: "signup", CreatedAt: now, UpdatedAt: now}}
	store.functions[ref] = []ProjectFunction{{ID: "function", ProjectRef: ref, Name: "function", CreatedAt: now, UpdatedAt: now}}
	store.functionRegions[ref] = []ProjectFunctionRegion{{ID: "region", ProjectRef: ref, FunctionName: "function", CreatedAt: now, UpdatedAt: now}}
	store.functionStorageMounts[ref] = []ProjectFunctionStorageMount{{ID: "mount", ProjectRef: ref, FunctionName: "function", BucketName: "bucket", CreatedAt: now, UpdatedAt: now}}
	store.replicationPipelines[ref] = []ProjectReplicationPipeline{{ID: "pipeline", ProjectRef: ref, Name: "pipeline", CreatedAt: now, UpdatedAt: now}}
	store.embeddingJobs[ref] = []ProjectEmbeddingJob{{ID: "embedding", ProjectRef: ref, Name: "embedding", CreatedAt: now, UpdatedAt: now}}
	store.databaseExtensions[ref] = []ProjectDatabaseExtension{{ID: "extension", ProjectRef: ref, Name: "pg_stat_statements", Enabled: true, CreatedAt: now, UpdatedAt: now}}
	store.databaseCronJobs[ref] = []ProjectDatabaseCronJob{{ID: "cron", ProjectRef: ref, Name: "cron", CreatedAt: now, UpdatedAt: now}}
	store.databaseQueues[ref] = []ProjectDatabaseQueue{{ID: "queue", ProjectRef: ref, Name: "queue", CreatedAt: now, UpdatedAt: now}}
	store.databaseWebhooks[ref] = []ProjectDatabaseWebhook{{ID: "webhook", ProjectRef: ref, Name: "webhook", CreatedAt: now, UpdatedAt: now}}
	store.databaseSchemas[ref] = []ProjectDatabaseSchema{{ID: "schema", ProjectRef: ref, Name: "schema", CreatedAt: now, UpdatedAt: now}}
	store.databaseRoles[ref] = []ProjectDatabaseRole{{ID: "role", ProjectRef: ref, Name: "role", CreatedAt: now, UpdatedAt: now}}
	store.storageBuckets[ref] = []ProjectStorageBucket{{ID: "storage", ProjectRef: ref, Name: "storage", CreatedAt: now, UpdatedAt: now}}
	store.vectorBuckets[ref] = []ProjectVectorBucket{{ID: "vector", ProjectRef: ref, Name: "vector", CreatedAt: now, UpdatedAt: now}}
	store.analyticsBuckets[ref] = []ProjectAnalyticsBucket{{ID: "analytics", ProjectRef: ref, Name: "analytics", CreatedAt: now, UpdatedAt: now}}
	store.cdnPolicies[ref] = ProjectCDNPolicy{ProjectRef: ref, Enabled: true, UpdatedAt: now}
	store.cdnInvalidations[ref] = []CDNInvalidation{{ID: "cdn", ProjectRef: ref, Paths: []string{"/*"}, CreatedAt: now}}
	store.networkConnections[ref] = []ProjectNetworkConnection{{ID: "network", ProjectRef: ref, Name: "network", CreatedAt: now, UpdatedAt: now}}
	store.logDrains[ref] = []LogDrain{{ID: "drain", ProjectRef: ref, Target: "https", Config: map[string]string{"url": "https://logs.example.com"}, CreatedAt: now}}
	store.secrets[ref] = map[string]ProjectSecret{"custom": {ID: "secret", ProjectRef: ref, Kind: "custom", Value: "secret", Masked: "********", CreatedAt: now}}
	store.policies[ref] = BackupPolicy{ProjectRef: ref, Enabled: true, UpdatedAt: now}
	store.pitrPolicies[ref] = PITRPolicy{ProjectRef: ref, Enabled: true, UpdatedAt: now}
	store.projectAccess[ref] = []ProjectAccessGrant{{ID: "access", ProjectRef: ref, SubjectType: "user", SubjectID: "user", Role: "viewer", CreatedAt: now}}
	store.telemetry[ref] = TelemetrySample{ProjectRef: ref, Source: "test", SampledAt: now}
}

func registeredProjectDatabaseExtensionCount(store *MemoryStore, ref string) int {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return countEnabledDatabaseExtensions(ref, store.databaseExtensions[ref])
}

func assertRegisteredProjectChildMetrics(t *testing.T, metrics ProjectMetrics, expectedDatabaseExtensions int) {
	t.Helper()
	if metrics.Routes != 1 || metrics.CustomDomains != 1 || metrics.LogDrains != 1 || metrics.FunctionDeployments != 1 ||
		metrics.FunctionRegions != 1 || metrics.FunctionStorageMounts != 1 || metrics.ReplicationPipelines != 1 ||
		metrics.EmbeddingJobs != 1 || metrics.AuthClients != 1 || metrics.AuthHooks != 1 || metrics.DatabaseExtensions != expectedDatabaseExtensions ||
		metrics.DatabaseCronJobs != 1 || metrics.DatabaseQueues != 1 || metrics.DatabaseWebhooks != 1 ||
		metrics.DatabaseSchemas != 1 || metrics.DatabaseRoles != 1 || metrics.StorageBuckets != 1 ||
		metrics.VectorBuckets != 1 || metrics.AnalyticsBuckets != 1 || metrics.CDNInvalidations != 1 ||
		metrics.NetworkConnections != 1 || metrics.Secrets != 1 {
		t.Fatalf("expected registered project child metric counts to be 1, got %#v", metrics)
	}
}

func assertRegisteredProjectChildFleetMetrics(t *testing.T, metrics FleetMetrics, expectedDatabaseExtensions int) {
	t.Helper()
	if metrics.Routes != 1 || metrics.CustomDomains != 1 || metrics.LogDrains != 1 || metrics.FunctionDeployments != 1 ||
		metrics.FunctionRegions != 1 || metrics.FunctionStorageMounts != 1 || metrics.ReplicationPipelines != 1 ||
		metrics.EmbeddingJobs != 1 || metrics.AuthClients != 1 || metrics.AuthHooks != 1 || metrics.DatabaseExtensions != expectedDatabaseExtensions ||
		metrics.DatabaseCronJobs != 1 || metrics.DatabaseQueues != 1 || metrics.DatabaseWebhooks != 1 ||
		metrics.DatabaseSchemas != 1 || metrics.DatabaseRoles != 1 || metrics.StorageBuckets != 1 ||
		metrics.VectorBuckets != 1 || metrics.AnalyticsBuckets != 1 || metrics.CDNInvalidations != 1 ||
		metrics.NetworkConnections != 1 {
		t.Fatalf("expected registered fleet child metric counts to be 1, got %#v", metrics)
	}
}

func assertRegisteredProjectChildOrgUsage(t *testing.T, usage OrgUsage, expectedDatabaseExtensions int) {
	t.Helper()
	if usage.CustomDomains != 1 || usage.LogDrains != 1 || usage.FunctionDeployments != 1 ||
		usage.FunctionRegions != 1 || usage.FunctionStorageMounts != 1 || usage.ReplicationPipelines != 1 ||
		usage.EmbeddingJobs != 1 || usage.AuthClients != 1 || usage.AuthHooks != 1 || usage.DatabaseExtensions != expectedDatabaseExtensions ||
		usage.DatabaseCronJobs != 1 || usage.DatabaseQueues != 1 || usage.DatabaseWebhooks != 1 ||
		usage.DatabaseSchemas != 1 || usage.DatabaseRoles != 1 || usage.StorageBuckets != 1 ||
		usage.VectorBuckets != 1 || usage.AnalyticsBuckets != 1 || usage.CDNInvalidations != 1 ||
		usage.NetworkConnections != 1 || usage.Secrets != 1 {
		t.Fatalf("expected registered org usage counts to be 1, got %#v", usage)
	}
}

func assertNoRegisteredProjectChildFleetMetrics(t *testing.T, metrics FleetMetrics) {
	t.Helper()
	if metrics.Routes != 0 || metrics.CustomDomains != 0 || metrics.LogDrains != 0 || metrics.FunctionDeployments != 0 ||
		metrics.FunctionRegions != 0 || metrics.FunctionStorageMounts != 0 || metrics.ReplicationPipelines != 0 ||
		metrics.EmbeddingJobs != 0 || metrics.AuthClients != 0 || metrics.AuthHooks != 0 || metrics.DatabaseExtensions != 0 ||
		metrics.DatabaseCronJobs != 0 || metrics.DatabaseQueues != 0 || metrics.DatabaseWebhooks != 0 ||
		metrics.DatabaseSchemas != 0 || metrics.DatabaseRoles != 0 || metrics.StorageBuckets != 0 ||
		metrics.VectorBuckets != 0 || metrics.AnalyticsBuckets != 0 || metrics.CDNEnabledProjects != 0 ||
		metrics.CDNInvalidations != 0 || metrics.NetworkConnections != 0 {
		t.Fatalf("expected registered fleet child metric counts to be 0 after delete, got %#v", metrics)
	}
}

func assertRegisteredProjectChildrenCleaned(t *testing.T, store *MemoryStore, ref string) {
	t.Helper()
	store.mu.RLock()
	defer store.mu.RUnlock()

	if _, ok := store.configs[ref]; ok {
		t.Fatalf("expected configs for %s to be cleaned", ref)
	}
	if _, ok := store.databaseExtensions[ref]; ok {
		t.Fatalf("expected database extensions for %s to be cleaned", ref)
	}
	if _, ok := store.cdnPolicies[ref]; ok {
		t.Fatalf("expected CDN policy for %s to be cleaned", ref)
	}
	if _, ok := store.secrets[ref]; ok {
		t.Fatalf("expected secrets for %s to be cleaned", ref)
	}
	if _, ok := store.policies[ref]; ok {
		t.Fatalf("expected backup policy for %s to be cleaned", ref)
	}
	if _, ok := store.pitrPolicies[ref]; ok {
		t.Fatalf("expected PITR policy for %s to be cleaned", ref)
	}
	if _, ok := store.projectAccess[ref]; ok {
		t.Fatalf("expected project access grants for %s to be cleaned", ref)
	}
	if _, ok := store.telemetry[ref]; ok {
		t.Fatalf("expected telemetry for %s to be cleaned", ref)
	}
}
