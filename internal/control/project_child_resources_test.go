package control

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestProjectChildResourceRegistryClassifiesStoreMaps(t *testing.T) {
	registeredNames := map[string]struct{}{}
	classifiedStoreFields := map[string]struct{}{}
	for _, resource := range projectChildResourceRegistry {
		if resource.name == "" {
			t.Fatal("project child registry contains an unnamed resource")
		}
		if _, exists := registeredNames[resource.name]; exists {
			t.Fatalf("project child registry contains duplicate resource %q", resource.name)
		}
		registeredNames[resource.name] = struct{}{}
		if resource.cleanup == nil {
			t.Fatalf("project child registry resource %q has no cleanup function", resource.name)
		}

		fieldName := resource.inventory.memoryField
		if strings.TrimSpace(fieldName) == "" {
			t.Fatalf("project child registry resource %q must declare its MemoryStore field in inventory", resource.name)
		}
		classifiedStoreFields[fieldName] = struct{}{}
	}

	manualProjectChildFields := map[string]string{
		"branches": "branch cleanup must also remove branch projects and source-project branch references",
		"replicas": "replica cleanup must release host capacity before deleting the project entry",
	}
	for fieldName := range manualProjectChildFields {
		classifiedStoreFields[fieldName] = struct{}{}
	}

	nonProjectScopedFields := map[string]struct{}{
		"users":                {},
		"orgs":                 {},
		"orgQuotas":            {},
		"usageSnapshots":       {},
		"billingInvoices":      {},
		"memberships":          {},
		"teams":                {},
		"teamMembers":          {},
		"hosts":                {},
		"nodeTelemetry":        {},
		"projects":             {},
		"backupStorageTargets": {},
	}
	for fieldName := range nonProjectScopedFields {
		classifiedStoreFields[fieldName] = struct{}{}
	}

	storeType := reflect.TypeOf(MemoryStore{})
	for i := 0; i < storeType.NumField(); i++ {
		field := storeType.Field(i)
		if field.Type.Kind() != reflect.Map {
			continue
		}
		if _, ok := classifiedStoreFields[field.Name]; !ok {
			t.Fatalf("MemoryStore map field %q must be registered as a project child, documented as a manual project-child exception, or classified as non-project state", field.Name)
		}
	}
}

func TestProjectChildResourceInventoryMatchesManualSurfaces(t *testing.T) {
	completeInventoryResources := map[string]struct{}{
		"auth_clients":            {},
		"auth_hooks":              {},
		"domains":                 {},
		"configs":                 {},
		"functions":               {},
		"function_regions":        {},
		"function_storage_mounts": {},
		"replication_pipelines":   {},
		"embedding_jobs":          {},
		"database_extensions":     {},
		"database_cron_jobs":      {},
		"database_queues":         {},
		"database_webhooks":       {},
		"database_schemas":        {},
		"database_roles":          {},
		"storage_buckets":         {},
		"vector_buckets":          {},
		"analytics_buckets":       {},
		"cdn_policies":            {},
		"log_drains":              {},
		"network_connections":     {},
		"backup_policies":         {},
		"pitr_policies":           {},
		"project_access":          {},
	}
	partialInventoryResources := map[string]map[projectChildResourceSurface]string{
		"routes": {
			projectChildSurfaceTerraform: "routes are generated runtime routing state, not a declarative Terraform resource",
		},
		"cdn_invalidations": {
			projectChildSurfaceTerraform: "invalidation events are imperative operations, not desired-state Terraform resources",
		},
		"secrets": {
			projectChildSurfaceTerraform: "secret reveal/copy/rotate surfaces are intentionally not Terraform-managed",
		},
		"telemetry": {
			projectChildSurfaceTable:     "telemetry samples are checkpointed and treated as transient scheduler observations rather than normalized durable configuration",
			projectChildSurfaceCLI:       "telemetry is posted by collectors and surfaced through metrics commands",
			projectChildSurfaceMCP:       "telemetry is exposed through project metrics tools rather than a telemetry mutation tool",
			projectChildSurfaceTerraform: "telemetry is observed runtime state, not Terraform-managed desired state",
		},
		"telemetry_history": {
			projectChildSurfaceTable:     "telemetry history is checkpointed and compacted as transient runtime observation state",
			projectChildSurfaceTerraform: "telemetry history is observed runtime state, not Terraform-managed desired state",
		},
	}

	apiRoutes := readProjectChildSource(t, "..", "api", "routes.go")
	cli := readProjectChildSource(t, "..", "cli", "cli.go")
	cliDispatch := projectChildSourceSection(t, cli, "func (r Runner) dispatch", "\n}\n\nfunc ")
	apiRoutePatterns := extractAPIHandleFuncRoutePatterns(apiRoutes)
	cliDispatchCommands := extractCLIDispatchCommands(cliDispatch)
	cliUsageCommands := extractCLIUsageCommands(t, cli)
	mcp := readProjectChildSource(t, "..", "mcp", "server.go")
	terraformProvider := readProjectChildSource(t, "..", "terraform", "provider.go")
	persistentStore := readProjectChildSource(t, "persistent_store.go")
	normalizedLoad := projectChildSourceSection(t, persistentStore, "func (s *PersistentStore) loadNormalizedProjectChildren", "\nfunc (s *PersistentStore) save")
	normalizedSync := projectChildSourceSection(t, persistentStore, "func (s *PersistentStore) syncNormalizedTablesTx", "\nfunc jsonBytes")
	normalizedProjectChildSync := projectChildSourceSection(t, normalizedSync, "for ref, grants := range snapshot.ProjectAccess", "\n\tfor _, event := range snapshot.AuditEvents")
	if !strings.Contains(normalizedSync, "projectChildNormalizedDeleteStatements()") {
		t.Fatal("normalized project-child table cleanup must be registry-driven")
	}
	normalizedDeleteTables := projectChildNormalizedDeleteTables(t)
	assertProjectChildNormalizedTableParity(t, normalizedLoad, normalizedProjectChildSync, normalizedDeleteTables)
	storeType := reflect.TypeOf(MemoryStore{})
	snapshotType := reflect.TypeOf(memoryStoreSnapshot{})

	for _, resource := range projectChildResourceRegistry {
		inventory := resource.inventory
		if _, ok := completeInventoryResources[resource.name]; ok {
			if !projectChildInventoryComplete(inventory) {
				t.Fatalf("%s inventory must include memory field, snapshot field, normalized table, API route, CLI command, MCP tool, and Terraform resource: %#v", resource.name, inventory)
			}
			if len(inventory.omittedSurfaces) != 0 {
				t.Fatalf("%s complete inventory must not declare omitted surfaces: %#v", resource.name, inventory.omittedSurfaces)
			}
			assertProjectChildInventorySurfaces(t, resource.name, inventory, storeType, snapshotType, persistentStore, normalizedLoad, normalizedSync, normalizedDeleteTables, apiRoutePatterns, cliDispatchCommands, cliUsageCommands, mcp, terraformProvider)
			delete(completeInventoryResources, resource.name)
			continue
		}

		expectedOmissions, ok := partialInventoryResources[resource.name]
		if !ok {
			t.Fatalf("%s must be listed as a complete or partial inventory resource", resource.name)
		}
		if !projectChildInventoryPresent(inventory) {
			t.Fatalf("%s partial inventory must declare the surfaces it does have", resource.name)
		}
		if !reflect.DeepEqual(inventory.omittedSurfaces, expectedOmissions) {
			t.Fatalf("%s partial inventory omitted surfaces mismatch:\n got %#v\nwant %#v", resource.name, inventory.omittedSurfaces, expectedOmissions)
		}
		assertProjectChildInventoryOmissions(t, resource.name, inventory)
		assertProjectChildInventorySurfaces(t, resource.name, inventory, storeType, snapshotType, persistentStore, normalizedLoad, normalizedSync, normalizedDeleteTables, apiRoutePatterns, cliDispatchCommands, cliUsageCommands, mcp, terraformProvider)
		delete(partialInventoryResources, resource.name)
	}

	for name := range completeInventoryResources {
		t.Fatalf("missing project child inventory resource %q", name)
	}
	for name := range partialInventoryResources {
		t.Fatalf("missing partial project child inventory resource %q", name)
	}
}

func TestProjectChildListMethodsUseSharedCloneSortHelper(t *testing.T) {
	store := readProjectChildSource(t, "store.go")
	sharedListMethods := map[string]string{
		"ListProjectAuthClients":           "authClients",
		"ListProjectAuthHooks":             "authHooks",
		"ListProjectAccess":                "projectAccess",
		"ListProjectFunctions":             "functions",
		"ListProjectFunctionRegions":       "functionRegions",
		"ListProjectFunctionStorageMounts": "functionStorageMounts",
		"ListProjectReplicationPipelines":  "replicationPipelines",
		"ListProjectEmbeddingJobs":         "embeddingJobs",
		"ListProjectDatabaseCronJobs":      "databaseCronJobs",
		"ListProjectDatabaseQueues":        "databaseQueues",
		"ListProjectDatabaseWebhooks":      "databaseWebhooks",
		"ListProjectDatabaseSchemas":       "databaseSchemas",
		"ListProjectDatabaseRoles":         "databaseRoles",
		"ListProjectStorageBuckets":        "storageBuckets",
		"ListProjectVectorBuckets":         "vectorBuckets",
		"ListProjectAnalyticsBuckets":      "analyticsBuckets",
		"ListProjectCDNInvalidations":      "cdnInvalidations",
		"ListProjectNetworkConnections":    "networkConnections",
		"ListProjectLogDrains":             "logDrains",
	}
	for method, field := range sharedListMethods {
		body := projectChildSourceSection(t, store, "func (s *MemoryStore) "+method, "\n}\n\nfunc ")
		expected := "cloneAndSortProjectChildList(s." + field + "[ref]"
		if !strings.Contains(body, expected) {
			t.Fatalf("%s must use shared project-child clone/sort helper for s.%s", method, field)
		}
		if strings.Contains(body, "sort.Slice(") {
			t.Fatalf("%s must not reintroduce bespoke sort.Slice logic", method)
		}
	}
}

func TestProjectChildFieldPreservationCoverageTracksInventory(t *testing.T) {
	covered := projectChildFieldPreservationFixtureResources(t)
	explicitlyExcluded := map[string]string{}
	for _, resource := range projectChildResourceRegistry {
		if !projectChildInventoryComplete(resource.inventory) {
			continue
		}
		if _, ok := covered[resource.name]; ok {
			continue
		}
		if reason := strings.TrimSpace(explicitlyExcluded[resource.name]); reason != "" {
			continue
		}
		t.Fatalf("%s is a complete project-child resource and must be covered by field-preservation restore assertions or explicitly excluded", resource.name)
	}
}

func projectChildFieldPreservationFixtureResources(t *testing.T) map[string]struct{} {
	t.Helper()
	resourceNamesByValueType := projectChildResourceNamesByValueType(t)
	covered := map[string]struct{}{}
	directFixtureTypes := []reflect.Type{
		reflect.TypeOf(ProjectAuthClient{}),
		reflect.TypeOf(ProjectAuthHook{}),
		reflect.TypeOf(ProjectDatabaseWebhook{}),
		reflect.TypeOf(ProjectNetworkConnection{}),
	}
	for _, fixtureType := range directFixtureTypes {
		addProjectChildFixtureResource(t, covered, resourceNamesByValueType, fixtureType)
	}

	additionalFixtures := reflect.TypeOf(additionalProjectChildFieldFixtures{})
	for index := 0; index < additionalFixtures.NumField(); index++ {
		addProjectChildFixtureResource(t, covered, resourceNamesByValueType, additionalFixtures.Field(index).Type)
	}
	return covered
}

func projectChildResourceNamesByValueType(t *testing.T) map[reflect.Type]string {
	t.Helper()
	storeType := reflect.TypeOf(MemoryStore{})
	resourceNamesByValueType := map[reflect.Type]string{}
	for _, resource := range projectChildResourceRegistry {
		if !projectChildInventoryComplete(resource.inventory) {
			continue
		}
		field, ok := storeType.FieldByName(resource.inventory.memoryField)
		if !ok {
			t.Fatalf("%s inventory references missing MemoryStore field %q", resource.name, resource.inventory.memoryField)
		}
		valueType, ok := projectChildResourceValueType(field.Type)
		if !ok {
			t.Fatalf("%s inventory MemoryStore field %q has unsupported type %s", resource.name, resource.inventory.memoryField, field.Type)
		}
		if existing, exists := resourceNamesByValueType[valueType]; exists {
			t.Fatalf("project child resources %s and %s both use value type %s; field-preservation fixture lookup needs an explicit disambiguation", existing, resource.name, valueType)
		}
		resourceNamesByValueType[valueType] = resource.name
	}
	return resourceNamesByValueType
}

func projectChildResourceValueType(fieldType reflect.Type) (reflect.Type, bool) {
	if fieldType.Kind() != reflect.Map || fieldType.Key().Kind() != reflect.String {
		return nil, false
	}
	valueType := fieldType.Elem()
	if valueType.Kind() == reflect.Slice {
		return valueType.Elem(), true
	}
	if valueType.Kind() == reflect.Map && valueType.Key().Kind() == reflect.String {
		return valueType.Elem(), true
	}
	return valueType, true
}

func addProjectChildFixtureResource(t *testing.T, covered map[string]struct{}, resourceNamesByValueType map[reflect.Type]string, fixtureType reflect.Type) {
	t.Helper()
	resourceName, ok := resourceNamesByValueType[fixtureType]
	if !ok {
		t.Fatalf("field-preservation fixture type %s is not mapped to a complete project-child registry resource", fixtureType)
	}
	covered[resourceName] = struct{}{}
}

func projectChildInventoryPresent(inventory projectChildResourceInventory) bool {
	return inventory.memoryField != "" ||
		inventory.snapshotField != "" ||
		inventory.normalizedTable != "" ||
		inventory.apiRoutePrefix != "" ||
		inventory.cliCommand != "" ||
		inventory.mcpTool != "" ||
		inventory.terraformResource != ""
}

func projectChildInventoryComplete(inventory projectChildResourceInventory) bool {
	return inventory.memoryField != "" &&
		inventory.snapshotField != "" &&
		inventory.normalizedTable != "" &&
		inventory.apiRoutePrefix != "" &&
		inventory.cliCommand != "" &&
		inventory.mcpTool != "" &&
		inventory.terraformResource != ""
}

func assertProjectChildInventoryOmissions(t *testing.T, resourceName string, inventory projectChildResourceInventory) {
	t.Helper()
	for _, surface := range []projectChildResourceSurface{
		projectChildSurfaceMemory,
		projectChildSurfaceSnapshot,
		projectChildSurfaceTable,
		projectChildSurfaceAPI,
		projectChildSurfaceCLI,
		projectChildSurfaceMCP,
		projectChildSurfaceTerraform,
	} {
		present := projectChildInventorySurfacePresent(inventory, surface)
		reason := strings.TrimSpace(inventory.omittedSurfaces[surface])
		if present && reason != "" {
			t.Fatalf("%s inventory declares %s surface and also marks it omitted: %q", resourceName, surface, reason)
		}
		if !present && reason == "" {
			t.Fatalf("%s partial inventory must explain omitted %s surface", resourceName, surface)
		}
	}
}

func projectChildInventorySurfacePresent(inventory projectChildResourceInventory, surface projectChildResourceSurface) bool {
	switch surface {
	case projectChildSurfaceMemory:
		return inventory.memoryField != ""
	case projectChildSurfaceSnapshot:
		return inventory.snapshotField != ""
	case projectChildSurfaceTable:
		return inventory.normalizedTable != ""
	case projectChildSurfaceAPI:
		return inventory.apiRoutePrefix != ""
	case projectChildSurfaceCLI:
		return inventory.cliCommand != ""
	case projectChildSurfaceMCP:
		return inventory.mcpTool != ""
	case projectChildSurfaceTerraform:
		return inventory.terraformResource != ""
	default:
		return false
	}
}

func assertProjectChildInventorySurfaces(t *testing.T, resourceName string, inventory projectChildResourceInventory, storeType reflect.Type, snapshotType reflect.Type, persistentStore string, normalizedLoad string, normalizedSync string, normalizedDeleteTables map[string]struct{}, apiRoutePatterns map[string]struct{}, cliDispatchCommands map[string]struct{}, cliUsageCommands map[string]struct{}, mcp string, terraformProvider string) {
	t.Helper()
	var memoryField reflect.StructField
	var hasMemoryField bool
	if inventory.memoryField != "" {
		var ok bool
		memoryField, ok = storeType.FieldByName(inventory.memoryField)
		if !ok {
			t.Fatalf("%s inventory references missing MemoryStore field %q", resourceName, inventory.memoryField)
		}
		hasMemoryField = true
	}
	var snapshotField reflect.StructField
	var hasSnapshotField bool
	if inventory.snapshotField != "" {
		var ok bool
		snapshotField, ok = snapshotType.FieldByName(inventory.snapshotField)
		if !ok {
			t.Fatalf("%s inventory references missing memoryStoreSnapshot field %q", resourceName, inventory.snapshotField)
		}
		hasSnapshotField = true
	}
	if hasMemoryField && hasSnapshotField {
		if memoryField.Type != snapshotField.Type {
			t.Fatalf("%s inventory MemoryStore field %q type %s does not match snapshot field %q type %s", resourceName, inventory.memoryField, memoryField.Type, inventory.snapshotField, snapshotField.Type)
		}
		assertProjectChildSnapshotWiring(t, resourceName, inventory, memoryField.Type, persistentStore)
	}
	if inventory.normalizedTable != "" {
		if !strings.Contains(normalizedLoad, "FROM "+inventory.normalizedTable) {
			t.Fatalf("%s inventory table %q is not loaded from normalized persistence", resourceName, inventory.normalizedTable)
		}
		if !strings.Contains(normalizedSync, "INSERT INTO "+inventory.normalizedTable) {
			t.Fatalf("%s inventory table %q is not synced into normalized persistence", resourceName, inventory.normalizedTable)
		}
		if _, ok := normalizedDeleteTables[inventory.normalizedTable]; !ok {
			t.Fatalf("%s inventory table %q is not cleared before normalized persistence resync", resourceName, inventory.normalizedTable)
		}
	}
	if inventory.apiRoutePrefix != "" {
		if !apiRoutePrefixRegistered(apiRoutePatterns, inventory.apiRoutePrefix) {
			t.Fatalf("%s inventory API prefix %q is not registered in an API HandleFunc route", resourceName, inventory.apiRoutePrefix)
		}
	}
	if inventory.cliCommand != "" {
		if _, ok := cliDispatchCommands[inventory.cliCommand]; !ok {
			t.Fatalf("%s inventory CLI command %q is not dispatched by the top-level CLI switch", resourceName, inventory.cliCommand)
		}
		if _, ok := cliUsageCommands[inventory.cliCommand]; !ok {
			t.Fatalf("%s inventory CLI command %q is not present in top-level CLI usage", resourceName, inventory.cliCommand)
		}
	}
	if inventory.mcpTool != "" {
		if !strings.Contains(mcp, `case "`+inventory.mcpTool+`":`) {
			t.Fatalf("%s inventory MCP tool %q is not dispatched", resourceName, inventory.mcpTool)
		}
		if !strings.Contains(mcp, `{"name": "`+inventory.mcpTool+`"`) {
			t.Fatalf("%s inventory MCP tool %q is not present in the MCP tool list", resourceName, inventory.mcpTool)
		}
	}
	if inventory.terraformResource != "" {
		if !strings.Contains(terraformProvider, inventory.terraformResource) {
			t.Fatalf("%s inventory Terraform resource %q is not registered", resourceName, inventory.terraformResource)
		}
	}
}

func extractAPIHandleFuncRoutePatterns(source string) map[string]struct{} {
	routes := map[string]struct{}{}
	pattern := regexp.MustCompile(`HandleFunc\("[A-Z]+ ([^"]+)"`)
	for _, match := range pattern.FindAllStringSubmatch(source, -1) {
		if len(match) == 2 {
			routes[match[1]] = struct{}{}
		}
	}
	return routes
}

func apiRoutePrefixRegistered(routes map[string]struct{}, prefix string) bool {
	if _, ok := routes[prefix]; ok {
		return true
	}
	for route := range routes {
		if strings.HasPrefix(route, prefix+"/") {
			return true
		}
	}
	return false
}

func extractCLIDispatchCommands(source string) map[string]struct{} {
	commands := map[string]struct{}{}
	pattern := regexp.MustCompile(`case "([^"]+)":`)
	for _, match := range pattern.FindAllStringSubmatch(source, -1) {
		if len(match) == 2 {
			commands[match[1]] = struct{}{}
		}
	}
	return commands
}

func extractCLIUsageCommands(t *testing.T, source string) map[string]struct{} {
	t.Helper()
	commands := map[string]struct{}{}
	pattern := regexp.MustCompile(`commands: ([^"]+)`)
	matches := pattern.FindStringSubmatch(source)
	if len(matches) != 2 {
		t.Fatal("top-level CLI usage command list is missing")
	}
	for _, command := range strings.Split(matches[1], ",") {
		command = strings.TrimSpace(command)
		if command != "" {
			commands[command] = struct{}{}
		}
	}
	return commands
}

func projectChildNormalizedDeleteTables(t *testing.T) map[string]struct{} {
	t.Helper()
	tableNamePattern := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	tables := map[string]struct{}{}
	registeredTables := projectChildNormalizedTableSet()
	for _, statement := range projectChildNormalizedDeleteStatements() {
		table := strings.TrimPrefix(statement, "DELETE FROM ")
		if table == statement || table == "" {
			t.Fatalf("project child normalized delete statement must have DELETE FROM prefix, got %q", statement)
		}
		if !tableNamePattern.MatchString(table) {
			t.Fatalf("project child normalized table %q must be a static lowercase identifier", table)
		}
		if _, ok := tables[table]; ok {
			t.Fatalf("project child normalized table %q appears more than once in delete statements", table)
		}
		tables[table] = struct{}{}
	}
	assertStringSetEqual(t, "project child normalized delete statements", tables, registeredTables)
	return tables
}

func assertProjectChildNormalizedTableParity(t *testing.T, normalizedLoad string, normalizedSync string, normalizedDeleteTables map[string]struct{}) {
	t.Helper()
	registeredTables := projectChildNormalizedTableSet()
	assertStringSetEqual(t, "project child normalized delete tables", normalizedDeleteTables, registeredTables)

	allowedOperationalTables := map[string]string{
		"project_branches": "branches need source and branch project references and remain a manual project-child exception",
		"project_replicas": "replicas need host-capacity bookkeeping and remain a manual project-child exception",
		"backups":          "backup history is operational recoverability state rather than declarative project-child config",
		"wal_archives":     "WAL archive history is operational recoverability state rather than declarative project-child config",
		"project_logs":     "project logs are append-only activity history rather than declarative project-child config",
		"audit_events":     "audit events are global append-only control-plane history rather than declarative project-child config",
	}
	assertProjectChildNormalizedSourceTables(t, "normalized load", extractNormalizedFromTables(normalizedLoad), registeredTables, allowedOperationalTables)
	assertProjectChildNormalizedSourceTables(t, "normalized sync", extractNormalizedInsertTables(normalizedSync), registeredTables, allowedOperationalTables)
}

func projectChildNormalizedTableSet() map[string]struct{} {
	tables := map[string]struct{}{}
	for _, table := range projectChildNormalizedTables() {
		tables[table] = struct{}{}
	}
	return tables
}

func assertProjectChildNormalizedSourceTables(t *testing.T, label string, sourceTables map[string]struct{}, registeredTables map[string]struct{}, allowedExtra map[string]string) {
	t.Helper()
	for table := range registeredTables {
		if _, ok := sourceTables[table]; !ok {
			t.Fatalf("%s is missing registered project-child normalized table %q", label, table)
		}
	}
	for table := range sourceTables {
		if _, ok := registeredTables[table]; ok {
			continue
		}
		if reason := strings.TrimSpace(allowedExtra[table]); reason != "" {
			continue
		}
		t.Fatalf("%s references normalized project-scoped table %q without project-child registry inventory or an explicit exception", label, table)
	}
}

func extractNormalizedFromTables(source string) map[string]struct{} {
	return extractNormalizedTables(source, regexp.MustCompile(`(?m)\bFROM\s+([a-z][a-z0-9_]*)\b`))
}

func extractNormalizedInsertTables(source string) map[string]struct{} {
	return extractNormalizedTables(source, regexp.MustCompile(`(?m)\bINSERT INTO\s+([a-z][a-z0-9_]*)\b`))
}

func extractNormalizedTables(source string, pattern *regexp.Regexp) map[string]struct{} {
	tables := map[string]struct{}{}
	for _, match := range pattern.FindAllStringSubmatch(source, -1) {
		if len(match) == 2 {
			tables[match[1]] = struct{}{}
		}
	}
	return tables
}

func assertStringSetEqual(t *testing.T, label string, got map[string]struct{}, want map[string]struct{}) {
	t.Helper()
	for value := range want {
		if _, ok := got[value]; !ok {
			t.Fatalf("%s missing %q", label, value)
		}
	}
	for value := range got {
		if _, ok := want[value]; !ok {
			t.Fatalf("%s has unexpected %q", label, value)
		}
	}
}

type projectChildStoreShape string

const (
	projectChildStoreShapeMap       projectChildStoreShape = "map"
	projectChildStoreShapeNestedMap projectChildStoreShape = "nested map"
	projectChildStoreShapeSliceMap  projectChildStoreShape = "slice map"
)

func assertProjectChildSnapshotWiring(t *testing.T, resourceName string, inventory projectChildResourceInventory, fieldType reflect.Type, persistentStore string) {
	t.Helper()
	shape, ok := projectChildStoreShapeForType(fieldType)
	if !ok {
		t.Fatalf("%s inventory field %q has unsupported project child store type %s", resourceName, inventory.memoryField, fieldType)
	}
	applyHelper := projectChildSnapshotApplyHelper(shape)
	applySnippet := "s." + inventory.memoryField + " = " + applyHelper + "(snapshot." + inventory.snapshotField + ")"
	if !strings.Contains(persistentStore, applySnippet) {
		t.Fatalf("%s inventory snapshot restore must use %s for %s -> %s", resourceName, applyHelper, inventory.snapshotField, inventory.memoryField)
	}

	cloneHelper, ok := projectChildSnapshotCloneHelper(inventory, persistentStore)
	if !ok {
		t.Fatalf("%s inventory snapshot must clone %s from s.%s", resourceName, inventory.snapshotField, inventory.memoryField)
	}
	if !projectChildSnapshotCloneHelperAllowed(shape, cloneHelper) {
		t.Fatalf("%s inventory snapshot uses %s for %s-shaped field %s", resourceName, cloneHelper, shape, inventory.memoryField)
	}
}

func projectChildStoreShapeForType(fieldType reflect.Type) (projectChildStoreShape, bool) {
	if fieldType.Kind() != reflect.Map || fieldType.Key().Kind() != reflect.String {
		return "", false
	}
	switch elemType := fieldType.Elem(); elemType.Kind() {
	case reflect.Map:
		return projectChildStoreShapeNestedMap, elemType.Key().Kind() == reflect.String
	case reflect.Slice:
		return projectChildStoreShapeSliceMap, true
	default:
		return projectChildStoreShapeMap, true
	}
}

func projectChildSnapshotApplyHelper(shape projectChildStoreShape) string {
	switch shape {
	case projectChildStoreShapeNestedMap:
		return "nonNilNestedMap"
	case projectChildStoreShapeSliceMap:
		return "nonNilSliceMap"
	default:
		return "nonNilMap"
	}
}

func projectChildSnapshotCloneHelper(inventory projectChildResourceInventory, persistentStore string) (string, bool) {
	pattern := regexp.MustCompile(regexp.QuoteMeta(inventory.snapshotField) + `:\s+([A-Za-z0-9_]+)\(s\.` + regexp.QuoteMeta(inventory.memoryField) + `\)`)
	match := pattern.FindStringSubmatch(persistentStore)
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

func projectChildSnapshotCloneHelperAllowed(shape projectChildStoreShape, helper string) bool {
	switch shape {
	case projectChildStoreShapeNestedMap:
		return helper == "cloneNestedMap"
	case projectChildStoreShapeSliceMap:
		return helper == "cloneSliceMap" || (strings.HasPrefix(helper, "clone") && strings.HasSuffix(helper, "Map"))
	default:
		return helper == "cloneMap"
	}
}

func readProjectChildSource(t *testing.T, elements ...string) string {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(elements...))
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func projectChildSourceSection(t *testing.T, source string, startMarker string, endMarker string) string {
	t.Helper()
	start := strings.Index(source, startMarker)
	if start < 0 {
		t.Fatalf("source section start marker %q not found", startMarker)
	}
	afterStart := source[start:]
	end := strings.Index(afterStart, endMarker)
	if end < 0 {
		t.Fatalf("source section end marker %q not found after %q", endMarker, startMarker)
	}
	return afterStart[:end]
}
