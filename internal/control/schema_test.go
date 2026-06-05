package control

import (
	"os"
	"path/filepath"
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
