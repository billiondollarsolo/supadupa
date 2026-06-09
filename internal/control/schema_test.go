package control

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMetaDBMigrationsCoverPlatformResources(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	files, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var schema strings.Builder
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(root, file.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", file.Name(), err)
		}
		schema.Write(payload)
		schema.WriteByte('\n')
	}
	normalized := strings.ToLower(schema.String())
	for _, expected := range []string{
		"create table org_quotas",
		"create table billing_invoices",
		"create table project_routes",
		"create table project_branches",
		"create table project_replicas",
		"create table pitr_policies",
		"create table wal_archives",
		"create table control_state_checkpoints",
		"create table platform_defaults",
		"create table teams",
		"create table team_members",
		"create table project_access_grants",
		"create table auth_clients",
		"create table auth_hooks",
		"create table database_extensions",
		"create table database_cron_jobs",
		"create table database_queues",
		"create table database_webhooks",
		"create table database_schemas",
		"create table database_roles",
		"create table storage_buckets",
		"create table analytics_buckets",
		"add column status",
		"ip_allowlist",
		"retention_days",
		"last_archive_at",
		"read_uri",
		"checksum_sha256",
		"backup_schedule",
		"smtp_password_handle",
		"subject_type",
	} {
		if !strings.Contains(normalized, expected) {
			t.Fatalf("expected migration schema to contain %q", expected)
		}
	}
}

func TestPersistentStoreInsertColumnsExistInMigrations(t *testing.T) {
	schema := readMetaDBMigrationSchema(t)
	persistentStore, err := os.ReadFile("persistent_store.go")
	if err != nil {
		t.Fatalf("read persistent store: %v", err)
	}

	insertPattern := regexp.MustCompile(`(?is)INSERT\s+INTO\s+([a-z_]+)\s*\(([^)]*)\)`)
	matches := insertPattern.FindAllStringSubmatch(string(persistentStore), -1)
	if len(matches) == 0 {
		t.Fatal("expected persistent store to contain normalized INSERT statements")
	}

	for _, match := range matches {
		table := strings.ToLower(strings.TrimSpace(match[1]))
		for _, rawColumn := range strings.Split(match[2], ",") {
			column := strings.ToLower(strings.TrimSpace(rawColumn))
			if column == "" {
				continue
			}
			if !migrationSchemaDeclaresColumn(schema, table, column) {
				t.Fatalf("persistent_store.go inserts %s.%s, but migrations do not declare that column", table, column)
			}
		}
	}
}

func readMetaDBMigrationSchema(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "migrations")
	files, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var schema strings.Builder
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(root, file.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", file.Name(), err)
		}
		schema.Write(payload)
		schema.WriteByte('\n')
	}
	return strings.ToLower(schema.String())
}

func migrationSchemaDeclaresColumn(schema string, table string, column string) bool {
	tablePattern := regexp.MustCompile(`(?is)create\s+table\s+(?:if\s+not\s+exists\s+)?` + regexp.QuoteMeta(table) + `\s*\((.*?)\);`)
	if match := tablePattern.FindStringSubmatch(schema); len(match) == 2 {
		columnPattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(column) + `\b`)
		if columnPattern.MatchString(match[1]) {
			return true
		}
	}

	alterPattern := regexp.MustCompile(`(?is)alter\s+table\s+` + regexp.QuoteMeta(table) + `\b[^;]*\badd\s+column\s+(?:if\s+not\s+exists\s+)?` + regexp.QuoteMeta(column) + `\b`)
	return alterPattern.MatchString(schema)
}
