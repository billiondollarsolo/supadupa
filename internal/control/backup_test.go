package control

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunDueBackupsCreatesBackupAndAdvancesPolicy(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	policy, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{
		Enabled:  true,
		Schedule: "hourly",
		Kind:     "logical",
	})
	if err != nil {
		t.Fatal(err)
	}
	due := now.Add(-time.Minute)
	policy.NextRunAt = &due
	store.policies[project.Ref] = policy

	backups, err := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir(), DryRun: true}).RunDueBackups(ctx, store, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup, got %d", len(backups))
	}
	updated, err := store.GetBackupPolicy(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastRunAt == nil {
		t.Fatalf("expected last run timestamp")
	}
	if updated.NextRunAt == nil || !updated.NextRunAt.After(now) {
		t.Fatalf("expected next run after now, got %#v", updated.NextRunAt)
	}
}

func TestBackupPolicyDisableClearsNextRunAndReenableSchedules(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "policy-toggle")

	enabled, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{
		Enabled:  true,
		Schedule: "daily",
		Kind:     "logical",
	})
	if err != nil {
		t.Fatal(err)
	}
	if enabled.NextRunAt == nil {
		t.Fatalf("expected enabled policy to schedule next run: %#v", enabled)
	}

	disabled, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{
		Enabled:  false,
		Schedule: "daily",
		Kind:     "logical",
	})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.NextRunAt != nil {
		t.Fatalf("expected disabled policy to clear next run, got %#v", disabled.NextRunAt)
	}

	reenabled, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{
		Enabled:  true,
		Schedule: "hourly",
		Kind:     "logical",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reenabled.NextRunAt == nil || reenabled.Schedule != "hourly" {
		t.Fatalf("expected re-enabled policy to schedule hourly next run, got %#v", reenabled)
	}
}

func TestRunDueBackupsAuditsFailures(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "scheduled-fail")
	now := time.Now().UTC()
	policy, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{
		Enabled:  true,
		Schedule: "hourly",
		Kind:     "logical",
	})
	if err != nil {
		t.Fatal(err)
	}
	due := now.Add(-time.Minute)
	policy.NextRunAt = &due
	store.policies[project.Ref] = policy

	backups, err := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir: t.TempDir(),
		Command: "printf boom >&2; exit 42",
	}).RunDueBackups(ctx, store, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected no completed backups, got %d", len(backups))
	}
	stored, err := store.ListBackups(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatalf("expected no stored backups, got %#v", stored)
	}
	logs, err := store.ListProjectLogs(ctx, project.Ref, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Message != "Scheduled backup failed" {
		t.Fatalf("expected scheduled backup failure log, got %#v", logs)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "project.backup_scheduled_failed" {
		t.Fatalf("expected scheduled backup failure audit event, got %#v", events)
	}
	if events[0].Metadata["error"] == "" {
		t.Fatalf("expected failure audit error metadata, got %#v", events[0].Metadata)
	}
}

func TestTriggerLogicalBackupWritesDryRunSQLArtifact(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "dry-run")

	backup, err := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir(), DryRun: true}).TriggerLogicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Kind != "logical" || backup.Status != "completed" {
		t.Fatalf("expected completed logical backup, got %#v", backup)
	}
	if backup.VerifiedAt == nil || len(backup.ChecksumSHA256) != 64 {
		t.Fatalf("expected verified backup checksum, got verified_at=%v checksum=%q", backup.VerifiedAt, backup.ChecksumSHA256)
	}
	if !strings.HasSuffix(backup.Location, ".sql") {
		t.Fatalf("expected sql artifact, got %s", backup.Location)
	}
	payload, err := os.ReadFile(backup.Location)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	if !strings.Contains(body, "-- mode: dry-run") || !strings.Contains(body, "-- project_ref: dry-run") {
		t.Fatalf("expected dry-run SQL metadata, got:\n%s", body)
	}
	if strings.Contains(body, "placeholder") {
		t.Fatalf("backup artifact should not be a placeholder manifest, got:\n%s", body)
	}
}

func TestBackupServiceUsesEnvRoot(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "env-backup-root")
	root := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", root)

	backup, err := NewBackupService("").TriggerLogicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(backup.Location, root) {
		t.Fatalf("expected backup under env root %s, got %s", root, backup.Location)
	}
}

func TestTriggerLogicalBackupRunsConfiguredCommand(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "command")
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir: t.TempDir(),
		Command: "printf 'dump for %s on %s\\n' {{ref}} {{stack_version}}",
	})

	backup, err := service.TriggerLogicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(backup.Location)
	if err != nil {
		t.Fatal(err)
	}
	if body := string(payload); !strings.Contains(body, "dump for command on latest") {
		t.Fatalf("expected command output in backup artifact, got:\n%s", body)
	}
	if backup.VerifiedAt == nil || len(backup.ChecksumSHA256) != 64 {
		t.Fatalf("expected verified command backup checksum, got verified_at=%v checksum=%q", backup.VerifiedAt, backup.ChecksumSHA256)
	}
}

func TestRestoreBackupWritesDryRunSQLPlan(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "restore-dry-run")
	service := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir(), DryRun: true, RestoreDryRun: true})
	backup, err := service.TriggerLogicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}

	restored, result, err := service.RestoreBackup(ctx, store, project.Ref, backup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != backup.ID {
		t.Fatalf("expected restored backup %s, got %s", backup.ID, restored.ID)
	}
	if result.State != "dry-run" || !strings.HasSuffix(result.Path, ".sql") {
		t.Fatalf("expected dry-run sql restore result, got %#v", result)
	}
	payload, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, expected := range []string{
		"-- supadupa logical restore",
		"-- mode: dry-run",
		"-- project_ref: restore-dry-run",
		"-- backup_id: " + backup.ID,
		"-- backup_location: " + backup.Location,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected restore plan to contain %q, got:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "placeholder") {
		t.Fatalf("restore plan should not be a placeholder manifest, got:\n%s", body)
	}
}

func TestRestoreBackupRunsConfiguredCommand(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "restore-command")
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:        t.TempDir(),
		Command:        "printf 'dump for %s\\n' {{ref}}",
		RestoreCommand: "printf 'restored %s from %s\\n' {{ref}} {{backup_path}}",
	})
	backup, err := service.TriggerLogicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}

	_, result, err := service.RestoreBackup(ctx, store, project.Ref, backup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "completed" || !strings.HasSuffix(result.Path, ".log") {
		t.Fatalf("expected completed restore transcript, got %#v", result)
	}
	payload, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if body := string(payload); !strings.Contains(body, "restored restore-command from "+backup.Location) {
		t.Fatalf("expected restore transcript command output, got:\n%s", body)
	}
}

func TestArchiveWALSegmentWritesDryRunArtifact(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "wal-dry-run")
	enablePITRPolicy(t, ctx, store, project.Ref)
	service := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir(), WALDryRun: true})

	archive, err := service.ArchiveWALSegment(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Status != "archived" || !strings.HasSuffix(archive.Location, ".wal") {
		t.Fatalf("expected archived wal artifact, got %#v", archive)
	}
	if archive.VerifiedAt == nil || len(archive.ChecksumSHA256) != 64 {
		t.Fatalf("expected verified WAL checksum, got verified_at=%v checksum=%q", archive.VerifiedAt, archive.ChecksumSHA256)
	}
	payload, err := os.ReadFile(archive.Location)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, expected := range []string{
		"-- supadupa WAL archive",
		"-- mode: dry-run",
		"-- project_ref: wal-dry-run",
		"-- archive_bucket: s3://archive/wal-dry-run",
		"WAL-SEGMENT " + archive.Segment,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected WAL archive to contain %q, got:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "placeholder") {
		t.Fatalf("WAL archive should not be a placeholder manifest, got:\n%s", body)
	}
}

func TestArchiveWALSegmentRunsConfiguredCommand(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "wal-command")
	enablePITRPolicy(t, ctx, store, project.Ref)
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:    t.TempDir(),
		WALCommand: "printf 'wal %s to %s at %s\\n' {{segment}} {{archive_bucket}} {{wal_path}}",
	})

	archive, err := service.ArchiveWALSegment(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(archive.Location)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	if !strings.Contains(body, "wal "+archive.Segment+" to s3://archive/wal-command at "+archive.Location) {
		t.Fatalf("expected WAL command output, got:\n%s", body)
	}
}

func createBackupTestProject(t *testing.T, ctx context.Context, store *MemoryStore, ref string) Project {
	t.Helper()
	org, err := store.CreateOrg(ctx, "Platform "+ref)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    ref,
		Name:   ref,
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return project
}

func enablePITRPolicy(t *testing.T, ctx context.Context, store *MemoryStore, ref string) {
	t.Helper()
	_, err := store.UpdatePITRPolicy(ctx, ref, PITRPolicyInput{
		Enabled:       true,
		ArchiveBucket: "s3://archive/" + ref,
		RetentionDays: 14,
	})
	if err != nil {
		t.Fatal(err)
	}
}
