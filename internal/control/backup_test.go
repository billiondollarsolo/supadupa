package control

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
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

func TestRunDueBackupsCreatesPhysicalBackupForPhysicalPolicy(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "physical-scheduled")
	now := time.Now().UTC()
	policy, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{
		Enabled:  true,
		Schedule: "hourly",
		Kind:     "physical",
	})
	if err != nil {
		t.Fatal(err)
	}
	due := now.Add(-time.Minute)
	policy.NextRunAt = &due
	store.policies[project.Ref] = policy

	backups, err := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:         t.TempDir(),
		PhysicalCommand: "printf 'base backup for %s\\n' {{ref}}",
	}).RunDueBackups(ctx, store, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].Kind != "physical" {
		t.Fatalf("expected one physical backup, got %#v", backups)
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

func TestBackupPolicySupportsPhysicalKind(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "policy-physical")

	policy, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{
		Enabled:  true,
		Schedule: "daily",
		Kind:     "physical",
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy.Kind != "physical" {
		t.Fatalf("expected physical policy, got %#v", policy)
	}
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "wal"}); err == nil || !strings.Contains(err.Error(), "unsupported backup kind") {
		t.Fatalf("expected unsupported kind error, got %v", err)
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
	if backup.StartedAt.IsZero() || backup.FinishedAt == nil {
		t.Fatalf("expected backup run timestamps, got started_at=%v finished_at=%v", backup.StartedAt, backup.FinishedAt)
	}
	if backup.FinishedAt.Before(backup.StartedAt) {
		t.Fatalf("expected backup finished_at >= started_at, got started_at=%v finished_at=%v", backup.StartedAt, backup.FinishedAt)
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
	if body := string(payload); !strings.Contains(body, "dump for command on "+DefaultStackReleaseVersion) {
		t.Fatalf("expected command output in backup artifact, got:\n%s", body)
	}
	if backup.VerifiedAt == nil || len(backup.ChecksumSHA256) != 64 {
		t.Fatalf("expected verified command backup checksum, got verified_at=%v checksum=%q", backup.VerifiedAt, backup.ChecksumSHA256)
	}
}

func TestTriggerPhysicalBackupRunsConfiguredCommandAndUploads(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "physical-command")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Backups",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "auto",
		Bucket:          "supadupa-backups",
		Prefix:          "control",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		ForcePathStyle:  true,
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	uploader := &fakeBackupUploader{}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:         t.TempDir(),
		PhysicalCommand: "printf 'physical base for %s on %s\\n' {{ref}} {{stack_version}}",
		Uploader:        uploader,
	})

	backup, err := service.TriggerPhysicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(backup.Location)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Kind != "physical" || !strings.Contains(string(payload), "physical base for physical-command on "+DefaultStackReleaseVersion) {
		t.Fatalf("expected physical backup artifact, got backup=%#v body=\n%s", backup, string(payload))
	}
	if backup.StorageTargetID != target.ID || backup.RemoteLocation != "s3://supadupa-backups/control/projects/physical-command/backups/"+filepath.Base(backup.Location) {
		t.Fatalf("expected uploaded physical backup, got %#v", backup)
	}
	if uploader.project.Ref != project.Ref || uploader.localPath != backup.Location {
		t.Fatalf("expected uploader call for physical artifact, got %#v", uploader)
	}
}

func TestTriggerPhysicalBackupRequiresReadyTargetWhenGuardEnabled(t *testing.T) {
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "true")
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "physical-ready-target")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Untested",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "auto",
		Bucket:          "supadupa-backups",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:         t.TempDir(),
		PhysicalCommand: "printf 'should not run\\n'",
		Uploader:        &fakeBackupUploader{},
	})

	if _, err := service.TriggerPhysicalBackup(ctx, store, project); err == nil || !strings.Contains(err.Error(), "not recovery-ready") {
		t.Fatalf("expected recovery-ready target error, got %v", err)
	}
	markBackupTargetPassed(t, ctx, store, target.ID)
	if _, err := service.TriggerPhysicalBackup(ctx, store, project); err != nil {
		t.Fatalf("expected physical backup after target validation: %v", err)
	}
}

func TestTriggerPhysicalBackupRequiresTargetWhenGuardEnabled(t *testing.T) {
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "true")
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "physical-missing-target")
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:         t.TempDir(),
		PhysicalCommand: "printf 'should not run\\n'",
		Uploader:        &fakeBackupUploader{},
	})

	if _, err := service.TriggerPhysicalBackup(ctx, store, project); err == nil || !strings.Contains(err.Error(), "backup storage target is required") {
		t.Fatalf("expected missing recovery-ready target error, got %v", err)
	}
}

func TestTriggerPhysicalBackupRequiresConfiguredCommand(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "physical-missing-command")
	t.Setenv("SUPADUPA_COMPOSE_BACKUP_DEFAULTS", "false")
	t.Setenv("SUPADUPA_PHYSICAL_BACKUP_COMMAND", "")

	_, err := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir()}).TriggerPhysicalBackup(ctx, store, project)
	if err == nil || !strings.Contains(err.Error(), "physical backup command is not configured") {
		t.Fatalf("expected missing physical command error, got %v", err)
	}
}

func TestTriggerLogicalBackupUploadsToDefaultStorageTarget(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "s3-default")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Backups",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "auto",
		Bucket:          "supadupa-backups",
		Prefix:          "control",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		ForcePathStyle:  true,
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	uploader := &fakeBackupUploader{}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:  t.TempDir(),
		DryRun:   true,
		Uploader: uploader,
	})

	backup, err := service.TriggerLogicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	if backup.StorageTargetID != target.ID {
		t.Fatalf("expected backup storage target %s, got %#v", target.ID, backup)
	}
	if backup.RemoteLocation != "s3://supadupa-backups/control/projects/s3-default/backups/"+filepath.Base(backup.Location) {
		t.Fatalf("expected remote location, got %q", backup.RemoteLocation)
	}
	if uploader.target.SecretAccessKey != "secret" {
		t.Fatalf("expected uploader to receive unredacted target")
	}
	if uploader.project.Ref != project.Ref || uploader.localPath != backup.Location {
		t.Fatalf("expected uploader call for project artifact, got %#v", uploader)
	}
}

func TestTriggerPlatformBackupWritesArtifactAndUploadsDefaultTarget(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Backups",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "auto",
		Bucket:          "supadupa-backups",
		Prefix:          "control",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		ForcePathStyle:  true,
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	uploader := &fakeBackupUploader{}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:  t.TempDir(),
		Uploader: uploader,
	})

	backup, err := service.TriggerPlatformBackup(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Kind != "control-plane" || backup.Status != "completed" {
		t.Fatalf("expected completed control-plane backup, got %#v", backup)
	}
	if backup.StorageTargetID != target.ID {
		t.Fatalf("expected target %s, got %#v", target.ID, backup)
	}
	if backup.RemoteLocation != "s3://supadupa-backups/control/platform/backups/"+filepath.Base(backup.Location) {
		t.Fatalf("expected platform remote location, got %q", backup.RemoteLocation)
	}
	payload, err := os.ReadFile(backup.Location)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	if !strings.Contains(body, `"format": "supadupa-control-plane-backup/v1"`) || !strings.Contains(body, `"mode": "dry-run"`) {
		t.Fatalf("expected platform backup manifest, got:\n%s", body)
	}
	if uploader.platformPath != backup.Location {
		t.Fatalf("expected platform uploader path %s, got %#v", backup.Location, uploader)
	}
}

func TestRestorePlatformBackupImportsPersistentCheckpoint(t *testing.T) {
	ctx := context.Background()
	db := openCheckpointDB(t)
	store, err := NewPersistentStore(ctx, db)
	if err != nil {
		t.Fatalf("new persistent store: %v", err)
	}
	org, err := store.CreateOrg(ctx, "Before")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "before", Name: "Before"})
	if err != nil {
		t.Fatal(err)
	}
	service := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir()})
	backup, err := service.TriggerPlatformBackup(ctx, store)
	if err != nil {
		t.Fatalf("trigger platform backup: %v", err)
	}
	if _, err := store.CreateOrg(ctx, "After"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "after", Name: "After"}); err != nil {
		t.Fatal(err)
	}

	restoredBackup, restore, err := service.RestorePlatformBackup(ctx, store, backup.ID)
	if err != nil {
		t.Fatalf("restore platform backup: %v", err)
	}
	if restoredBackup.ID != backup.ID || restore.State != "metadata-restored" || restore.Path != backup.Location {
		t.Fatalf("unexpected platform restore result backup=%#v restore=%#v", restoredBackup, restore)
	}
	platformBackups, err := store.ListPlatformBackups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(platformBackups) != 1 || platformBackups[0].ID != backup.ID {
		t.Fatalf("expected restored checkpoint to preserve source platform backup, got %#v", platformBackups)
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Ref != project.Ref {
		t.Fatalf("expected restored checkpoint project %s only, got %#v", project.Ref, projects)
	}
	reopened, err := NewPersistentStore(ctx, db)
	if err != nil {
		t.Fatalf("reopen persistent store: %v", err)
	}
	reopenedProjects, err := reopened.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopenedProjects) != 1 || reopenedProjects[0].Ref != project.Ref {
		t.Fatalf("expected reopened checkpoint project %s only, got %#v", project.Ref, reopenedProjects)
	}
	reopenedPlatformBackups, err := reopened.ListPlatformBackups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopenedPlatformBackups) != 1 || reopenedPlatformBackups[0].ID != backup.ID {
		t.Fatalf("expected reopened checkpoint to preserve source platform backup, got %#v", reopenedPlatformBackups)
	}
}

func TestRestorePlatformBackupRejectsDryRunArtifact(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir()})
	backup, err := service.TriggerPlatformBackup(ctx, store)
	if err != nil {
		t.Fatalf("trigger platform backup: %v", err)
	}

	_, _, err = service.RestorePlatformBackup(ctx, store, backup.ID)
	if err == nil || !strings.Contains(err.Error(), "persistent meta DB") {
		t.Fatalf("expected persistent store requirement, got %v", err)
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

func TestRestoreBackupDownloadsMissingRemoteArtifact(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "restore-remote")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Backups",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "auto",
		Bucket:          "supadupa-backups",
		Prefix:          "control",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		ForcePathStyle:  true,
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	uploader := &fakeBackupUploader{}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:        t.TempDir(),
		Command:        "printf 'remote restore dump for %s\\n' {{ref}}",
		RestoreCommand: "test -s {{backup_path}} && printf 'restored %s from %s\\n' {{ref}} {{backup_path}}",
		Uploader:       uploader,
	})
	backup, err := service.TriggerLogicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(backup.Location)
	if err != nil {
		t.Fatal(err)
	}
	uploader.downloadPayload = payload
	if err := os.Remove(backup.Location); err != nil {
		t.Fatal(err)
	}

	_, result, err := service.RestoreBackup(ctx, store, project.Ref, backup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "completed" {
		t.Fatalf("expected completed restore, got %#v", result)
	}
	if uploader.downloadTarget.ID != target.ID || uploader.downloadRemote != backup.RemoteLocation || uploader.downloadPath != backup.Location {
		t.Fatalf("expected remote download of backup artifact, got %#v", uploader)
	}
	downloaded, err := os.ReadFile(backup.Location)
	if err != nil {
		t.Fatal(err)
	}
	if string(downloaded) != string(payload) {
		t.Fatalf("expected downloaded artifact to match original")
	}
}

func TestRestoreBackupRejectsDownloadedChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "restore-bad-remote")
	if _, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Backups",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "auto",
		Bucket:          "supadupa-backups",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		Default:         true,
	}); err != nil {
		t.Fatal(err)
	}
	uploader := &fakeBackupUploader{downloadPayload: []byte("evil artifact\n")}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:        t.TempDir(),
		Command:        "printf 'good artifact\\n'",
		RestoreCommand: "printf 'should not run\\n'",
		Uploader:       uploader,
	})
	backup, err := service.TriggerLogicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(backup.Location); err != nil {
		t.Fatal(err)
	}

	_, _, err = service.RestoreBackup(ctx, store, project.Ref, backup.ID)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestBackupServiceUsesComposeApplyDefaultCommands(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "compose-defaults")
	root := t.TempDir()
	projectRoot := t.TempDir()
	projectDir := filepath.Join(projectRoot, project.Ref)
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("POSTGRES_PASSWORD=test-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	logPath := root + "/docker-args.log"
	dockerPath := fakeBin + "/docker"
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n" +
		"case \"$*\" in\n" +
		"  *pg_dump*) printf 'CREATE TABLE compat_restore_default(id int);\\n' ;;\n" +
		"  *pg_basebackup*) printf 'physical base backup tar\\n' ;;\n" +
		"  *pg_switch_wal*) printf '00000001000000000000000A\\n' ;;\n" +
		"  *pg_wal*) printf 'real wal bytes\\n' ;;\n" +
		"  *psql*) cat >/dev/null; printf 'restore ok\\n' ;;\n" +
		"  *) echo unexpected docker invocation >&2; exit 2 ;;\n" +
		"esac\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SUPADUPA_COMPOSE_APPLY", "true")
	t.Setenv("SUPADUPA_LOGICAL_BACKUP_COMMAND", "")
	t.Setenv("SUPADUPA_PHYSICAL_BACKUP_COMMAND", "")
	t.Setenv("SUPADUPA_LOGICAL_RESTORE_COMMAND", "")

	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:     root,
		ProjectRoot: projectRoot,
	})
	backup, err := service.TriggerLogicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	backupPayload, err := os.ReadFile(backup.Location)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(backupPayload), "compat_restore_default") {
		t.Fatalf("expected fake pg_dump output in backup, got:\n%s", string(backupPayload))
	}
	physicalBackup, err := service.TriggerPhysicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	physicalPayload, err := os.ReadFile(physicalBackup.Location)
	if err != nil {
		t.Fatal(err)
	}
	if physicalBackup.Kind != "physical" || !strings.Contains(string(physicalPayload), "physical base backup tar") {
		t.Fatalf("expected fake pg_basebackup output in physical backup, got backup=%#v body=\n%s", physicalBackup, string(physicalPayload))
	}
	if _, err := store.UpdatePITRPolicy(ctx, project.Ref, PITRPolicyInput{Enabled: true, ArchiveBucket: "s3://archive/compose-defaults", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	walArchive, err := service.ArchiveWALSegment(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	walPayload, err := os.ReadFile(walArchive.Location)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(walPayload), "real wal bytes") || strings.Contains(string(walPayload), "mode: dry-run") {
		t.Fatalf("expected real compose WAL command output, got archive=%#v body=\n%s", walArchive, string(walPayload))
	}
	if walArchive.Segment != "00000001000000000000000A" || walArchive.SegmentSource != "postgres" {
		t.Fatalf("expected compose WAL command to record real Postgres segment, got %#v", walArchive)
	}
	_, result, err := service.RestoreBackup(ctx, store, project.Ref, backup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "completed" {
		t.Fatalf("expected completed restore, got %#v", result)
	}
	logPayload, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logBody := string(logPayload)
	for _, expected := range []string{
		"compose -p compose-defaults",
		"pg_dump --clean --if-exists --quote-all-identifiers",
		"pg_basebackup -D",
		"-U supabase_replication_admin",
		"-Ft -X stream -c fast",
		"pg_switch_wal()",
		"--schema=public --schema=auth --schema=storage --schema=supabase_migrations",
		"psql -v ON_ERROR_STOP=1 -U supabase_admin",
	} {
		if !strings.Contains(logBody, expected) {
			t.Fatalf("expected docker invocation to contain %q, got:\n%s", expected, logBody)
		}
	}
}

func TestBackupServiceRequiresWALArchiveCommandOutsideDryRun(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "wal-command-required")
	if _, err := store.UpdatePITRPolicy(ctx, project.Ref, PITRPolicyInput{Enabled: true, ArchiveBucket: "s3://archive/wal-command-required", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUPADUPA_COMPOSE_APPLY", "")
	t.Setenv("SUPADUPA_WAL_ARCHIVE_COMMAND", "")
	t.Setenv("SUPADUPA_WAL_ARCHIVE_DRY_RUN", "")

	service := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir()})
	_, err := service.ArchiveWALSegment(ctx, store, project)
	if err == nil || !strings.Contains(err.Error(), "WAL archive command is not configured") {
		t.Fatalf("expected WAL command error, got %v", err)
	}
	archives, err := store.ListWALArchives(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("expected no WAL archive records, got %#v", archives)
	}
}

func TestBackupServiceComposeApplyDefaultsCanBeDisabled(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "compose-defaults-off")
	t.Setenv("SUPADUPA_COMPOSE_APPLY", "true")
	t.Setenv("SUPADUPA_COMPOSE_BACKUP_DEFAULTS", "false")
	t.Setenv("SUPADUPA_LOGICAL_BACKUP_COMMAND", "")
	t.Setenv("SUPADUPA_LOGICAL_RESTORE_COMMAND", "")

	service := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir()})
	backup, err := service.TriggerLogicalBackup(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(backup.Location)
	if err != nil {
		t.Fatal(err)
	}
	if body := string(payload); !strings.Contains(body, "-- mode: dry-run") {
		t.Fatalf("expected disabled compose defaults to use dry-run backup, got:\n%s", body)
	}

	_, result, err := service.RestoreBackup(ctx, store, project.Ref, backup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "dry-run" || !strings.HasSuffix(result.Path, ".sql") {
		t.Fatalf("expected disabled compose defaults to use dry-run restore, got %#v", result)
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

func TestArchiveWALSegmentUploadsToConfiguredTarget(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "wal-upload")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	enablePITRPolicy(t, ctx, store, project.Ref)
	uploader := &fakeBackupUploader{}
	service := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir(), WALDryRun: true, Uploader: uploader})

	archive, err := service.ArchiveWALSegment(ctx, store, project)
	if err != nil {
		t.Fatal(err)
	}
	if uploader.walPath != archive.Location || uploader.walFilename == "" {
		t.Fatalf("expected WAL artifact upload, uploader=%#v archive=%#v", uploader, archive)
	}
	if archive.StorageTargetID != target.ID || !strings.HasPrefix(archive.RemoteLocation, "s3://backups/supadupa/projects/wal-upload/wal/") {
		t.Fatalf("expected remote WAL metadata, got %#v", archive)
	}
}

func TestArchiveWALSegmentRequiresReadyTargetWhenGuardEnabled(t *testing.T) {
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "true")
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "wal-ready-target")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Untested",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "auto",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePITRPolicy(ctx, project.Ref, PITRPolicyInput{
		Enabled:       true,
		ArchiveBucket: "s3://backups/supadupa/projects/wal-ready-target/wal",
		RetentionDays: 7,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:   t.TempDir(),
		WALDryRun: true,
		Uploader:  &fakeBackupUploader{},
	})

	if _, err := service.ArchiveWALSegment(ctx, store, project); err == nil || !strings.Contains(err.Error(), "not recovery-ready") {
		t.Fatalf("expected recovery-ready target error, got %v", err)
	}
	markBackupTargetPassed(t, ctx, store, target.ID)
	if _, err := service.ArchiveWALSegment(ctx, store, project); err != nil {
		t.Fatalf("expected WAL archive after target validation: %v", err)
	}
}

func TestArchiveWALSegmentRequiresTargetWhenGuardEnabled(t *testing.T) {
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "true")
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "wal-missing-target")
	if _, err := store.UpdatePITRPolicy(ctx, project.Ref, PITRPolicyInput{
		Enabled:       true,
		ArchiveBucket: "s3://archive/wal-missing-target",
		RetentionDays: 7,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:   t.TempDir(),
		WALDryRun: true,
		Uploader:  &fakeBackupUploader{},
	})

	if _, err := service.ArchiveWALSegment(ctx, store, project); err == nil || !strings.Contains(err.Error(), "backup storage target is required") {
		t.Fatalf("expected missing recovery-ready target error, got %v", err)
	}
}

func TestCreateWALArchiveRejectsUnknownStorageTarget(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "wal-invalid-target")

	_, err := store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:      project.Ref,
		Segment:         "000000010000000000000001",
		SegmentSource:   "postgres",
		Location:        "/tmp/wal",
		RemoteLocation:  "s3://backups/supadupa/projects/wal-invalid-target/wal/000000010000000000000001.wal",
		StorageTargetID: "missing-target",
		SizeBytes:       512,
		ChecksumSHA256:  strings.Repeat("b", 64),
		Status:          "archived",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing storage target error, got %v", err)
	}

	archives, err := store.ListWALArchives(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("invalid WAL archive should not be recorded, got %#v", archives)
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
	if !strings.Contains(body, "wal "+archive.Segment+" to s3://archive/wal-command at ") {
		t.Fatalf("expected WAL command output, got:\n%s", body)
	}
	if archive.SegmentSource != "legacy-command" {
		t.Fatalf("expected streamed WAL command to be marked legacy-command, got %#v", archive)
	}
}

func TestRunDueWALArchivesCreatesArchiveAndHonorsInterval(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "wal-scheduled")
	enablePITRPolicy(t, ctx, store, project.Ref)
	if _, err := store.UpdateProjectStatus(ctx, project.Ref, ProjectHealthy, "healthy"); err != nil {
		t.Fatal(err)
	}
	service := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir(), WALDryRun: true})
	now := time.Now().UTC()

	archives, err := service.RunDueWALArchives(ctx, store, now, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("expected one scheduled WAL archive, got %#v", archives)
	}
	policy, err := store.GetPITRPolicy(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if policy.LastArchiveAt == nil {
		t.Fatalf("expected last archive timestamp after scheduled WAL archive")
	}

	archives, err = service.RunDueWALArchives(ctx, store, now.Add(time.Minute), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("expected no archive inside interval, got %#v", archives)
	}

	archives, err = service.RunDueWALArchives(ctx, store, now.Add(10*time.Minute), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("expected archive after interval, got %#v", archives)
	}
}

func TestRunDueWALArchivesSkipsDisabledAndPausedProjects(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	disabled := createBackupTestProject(t, ctx, store, "wal-disabled")
	paused := createBackupTestProject(t, ctx, store, "wal-paused")
	enablePITRPolicy(t, ctx, store, paused.Ref)
	if _, err := store.UpdateProjectStatus(ctx, paused.Ref, ProjectPaused, "paused"); err != nil {
		t.Fatal(err)
	}

	archives, err := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir(), WALDryRun: true}).RunDueWALArchives(ctx, store, time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("expected no scheduled WAL archives for disabled=%s paused=%s, got %#v", disabled.Ref, paused.Ref, archives)
	}
}

func TestProjectRecoverabilityReportsLocalOnlyAndRestoreToTimeGaps(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "recoverability-local")
	service := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: t.TempDir(), DryRun: true})
	if _, err := service.TriggerLogicalBackup(ctx, store, project); err != nil {
		t.Fatal(err)
	}

	status, err := ProjectRecoverability(ctx, store, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "local-backup-only" || status.LatestVerifiedBackup == nil || status.OffHostBackupVerified || status.RestoreToTimeAvailable {
		t.Fatalf("unexpected local-only recoverability status: %#v", status)
	}
	for _, expected := range []string{"no S3-compatible backup target", "PITR is disabled", "no verified physical base backup"} {
		if !containsString(status.Warnings, expected) {
			t.Fatalf("expected warning containing %q, got %#v", expected, status.Warnings)
		}
	}
}

func TestProjectRecoverabilityRejectsLoopbackBackupTargetAsOffHost(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "recoverability-loopback")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Local MinIO",
		Type:            "s3",
		Endpoint:        "http://127.0.0.1:9000",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
		ForcePathStyle:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "physical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.CreateBackup(ctx, BackupInput{
		ProjectRef:      project.Ref,
		Kind:            "physical",
		Location:        "/tmp/base.tar",
		RemoteLocation:  "s3://backups/supadupa/projects/recoverability-loopback/backups/base.tar",
		StorageTargetID: target.ID,
		SizeBytes:       1024,
		ChecksumSHA256:  strings.Repeat("a", 64),
		Status:          "completed",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}
	enablePITRPolicy(t, ctx, store, project.Ref)
	if _, err := store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:      project.Ref,
		Segment:         "000000010000000000000001",
		SegmentSource:   "postgres",
		Location:        "/tmp/wal",
		RemoteLocation:  "s3://backups/supadupa/projects/recoverability-loopback/wal/000000010000000000000001.wal",
		StorageTargetID: target.ID,
		SizeBytes:       512,
		ChecksumSHA256:  strings.Repeat("b", 64),
		Status:          "archived",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}

	status, err := ProjectRecoverabilityWithOptions(ctx, store, project.Ref, ProjectRecoverabilityOptions{RestoreToTimeConfigured: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "local-backup-only" || status.OffHostBackupConfigured || status.OffHostBackupVerified || status.PhysicalBackupAvailable || status.WALArchiveOffHostVerified || status.RestoreToTimeAvailable {
		t.Fatalf("loopback target should not satisfy off-host recoverability: %#v", status)
	}
	if status.LatestVerifiedBackup == nil || status.LatestWALArchive == nil {
		t.Fatalf("expected local verified backup and WAL to remain visible: %#v", status)
	}
	if !containsString(status.Warnings, "local or loopback") {
		t.Fatalf("expected loopback warning, got %#v", status.Warnings)
	}
}

func TestProjectRecoverabilityRequiresBackupTargetValidation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "recoverability-untested")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	status, err := ProjectRecoverability(ctx, store, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if status.OffHostBackupConfigured {
		t.Fatalf("untested target should not satisfy off-host configuration: %#v", status)
	}
	if !containsString(status.Warnings, "has not passed validation") {
		t.Fatalf("expected validation warning, got %#v", status.Warnings)
	}
}

func TestBackupStorageTargetDurabilityRejectsLocalInterfaceIP(t *testing.T) {
	previousResolver := resolveBackupTargetHostIPs
	t.Cleanup(func() {
		resolveBackupTargetHostIPs = previousResolver
	})
	resolveBackupTargetHostIPs = func(host string) []net.IP {
		switch host {
		case "loopback.example.test":
			return []net.IP{net.ParseIP("127.0.0.1")}
		case "remote.example.test":
			return []net.IP{net.ParseIP("203.0.113.10")}
		default:
			return nil
		}
	}

	for _, endpoint := range []string{
		"http://127.0.0.1:9000",
		"http://0.0.0.0:9000",
		"http://host.docker.internal:9000",
		"http://loopback.example.test:9000",
	} {
		if backupStorageTargetIsDurableOffHost(BackupStorageTarget{Endpoint: endpoint}) {
			t.Fatalf("expected %s to be rejected as local", endpoint)
		}
	}
	localIP := firstConcreteLocalInterfaceIP()
	if localIP != "" && backupStorageTargetIsDurableOffHost(BackupStorageTarget{Endpoint: "http://" + localIP + ":9000"}) {
		t.Fatalf("expected local interface IP %s to be rejected as local", localIP)
	}
	if !backupStorageTargetIsDurableOffHost(BackupStorageTarget{Endpoint: "https://s3.example.test"}) {
		t.Fatalf("expected remote hostname to be accepted")
	}
	if !backupStorageTargetIsDurableOffHost(BackupStorageTarget{Endpoint: "https://remote.example.test"}) {
		t.Fatalf("expected resolved remote hostname to be accepted")
	}
}

func TestBackupStorageTargetReadinessStates(t *testing.T) {
	previousResolver := resolveBackupTargetHostIPs
	t.Cleanup(func() {
		resolveBackupTargetHostIPs = previousResolver
	})
	resolveBackupTargetHostIPs = func(host string) []net.IP {
		switch host {
		case "loopback.example.test":
			return []net.IP{net.ParseIP("127.0.0.1")}
		case "remote.example.test":
			return []net.IP{net.ParseIP("203.0.113.10")}
		default:
			return nil
		}
	}

	for _, tc := range []struct {
		name        string
		target      BackupStorageTarget
		wantDurable bool
		wantReady   bool
		wantStatus  string
		wantMessage string
	}{
		{
			name:        "missing secret",
			target:      BackupStorageTarget{Endpoint: "https://remote.example.test", LastTestStatus: "passed"},
			wantDurable: true,
			wantStatus:  "missing-secret",
			wantMessage: "secret access key",
		},
		{
			name:        "loopback",
			target:      BackupStorageTarget{Endpoint: "http://loopback.example.test:9000", SecretConfigured: true, LastTestStatus: "passed"},
			wantStatus:  "local-or-loopback",
			wantMessage: "loopback",
		},
		{
			name:        "pending",
			target:      BackupStorageTarget{Endpoint: "https://remote.example.test", SecretConfigured: true},
			wantDurable: true,
			wantStatus:  "validation-pending",
			wantMessage: "pass validation",
		},
		{
			name:        "failed",
			target:      BackupStorageTarget{Endpoint: "https://remote.example.test", SecretConfigured: true, LastTestStatus: "failed", LastTestError: "bucket denied"},
			wantDurable: true,
			wantStatus:  "validation-failed",
			wantMessage: "bucket denied",
		},
		{
			name:        "ready",
			target:      BackupStorageTarget{Endpoint: "https://remote.example.test", SecretConfigured: true, LastTestStatus: "passed"},
			wantDurable: true,
			wantReady:   true,
			wantStatus:  "off-host-ready",
			wantMessage: "eligible",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			durable, ready, status, message := backupStorageTargetReadiness(tc.target)
			if durable != tc.wantDurable || ready != tc.wantReady || status != tc.wantStatus || !strings.Contains(message, tc.wantMessage) {
				t.Fatalf("unexpected readiness durable=%t ready=%t status=%s message=%q", durable, ready, status, message)
			}
		})
	}
}

func TestPITRPolicyDerivesArchiveBucketFromSelectedBackupTarget(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "pitr-derived-bucket")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	policy, err := store.UpdatePITRPolicy(ctx, project.Ref, PITRPolicyInput{Enabled: true, RetentionDays: 14})
	if err != nil {
		t.Fatal(err)
	}
	if policy.ArchiveBucket != "s3://backups/supadupa/projects/pitr-derived-bucket/wal" {
		t.Fatalf("expected archive bucket derived from backup target, got %#v", policy)
	}
}

func TestPITRPolicyRequiresReadyDerivedTargetWhenGuardEnabled(t *testing.T) {
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "true")
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "pitr-ready-derived")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Untested",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "auto",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePITRPolicy(ctx, project.Ref, PITRPolicyInput{Enabled: true, RetentionDays: 7}); err == nil || !strings.Contains(err.Error(), "archive_bucket is required") {
		t.Fatalf("expected archive bucket requirement until target is recovery-ready, got %v", err)
	}
	markBackupTargetPassed(t, ctx, store, target.ID)
	policy, err := store.UpdatePITRPolicy(ctx, project.Ref, PITRPolicyInput{Enabled: true, RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	if policy.ArchiveBucket != "s3://backups/supadupa/projects/pitr-ready-derived/wal" {
		t.Fatalf("expected archive bucket derived from ready backup target, got %#v", policy)
	}
}

func TestPITRPolicyStillRequiresArchiveBucketWithoutBackupTarget(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "pitr-no-target")
	_, err := store.UpdatePITRPolicy(ctx, project.Ref, PITRPolicyInput{Enabled: true, RetentionDays: 7})
	if err == nil || !strings.Contains(err.Error(), "archive_bucket is required") {
		t.Fatalf("expected archive bucket or target requirement, got %v", err)
	}
}

func TestProjectRecoverabilityReportsRestoreToTimeReady(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	project := createBackupTestProject(t, ctx, store, "recoverability-pitr")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	markBackupTargetPassed(t, ctx, store, target.ID)
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.CreateBackup(ctx, BackupInput{
		ProjectRef:      project.Ref,
		Kind:            "physical",
		Location:        "/tmp/base.tar",
		RemoteLocation:  "s3://backups/supadupa/projects/recoverability-pitr/backups/base.tar",
		StorageTargetID: target.ID,
		SizeBytes:       1024,
		ChecksumSHA256:  strings.Repeat("a", 64),
		Status:          "completed",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}
	enablePITRPolicy(t, ctx, store, project.Ref)
	if _, err := store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:      project.Ref,
		Segment:         "000000010000000000000001",
		SegmentSource:   "postgres",
		Location:        "/tmp/wal",
		RemoteLocation:  "s3://backups/supadupa/projects/recoverability-pitr/wal/000000010000000000000001.wal",
		StorageTargetID: target.ID,
		SizeBytes:       512,
		ChecksumSHA256:  strings.Repeat("b", 64),
		Status:          "archived",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}

	status, err := ProjectRecoverability(ctx, store, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "off-host-backup-ready" || !status.OffHostBackupConfigured || !status.OffHostBackupVerified || !status.PhysicalBackupAvailable || status.RestoreToTimeAvailable || status.RestoreToTimeConfigured {
		t.Fatalf("unexpected restore-to-time status without restore command: %#v", status)
	}
	if !containsString(status.Warnings, "no PITR restore command") {
		t.Fatalf("expected restore command warning, got %#v", status.Warnings)
	}

	t.Setenv("SUPADUPA_COMPOSE_APPLY", "true")
	t.Setenv("SUPADUPA_COMPOSE_BACKUP_DEFAULTS", "")
	status, err = ProjectRecoverability(ctx, store, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "restore-to-time-ready" || !status.RestoreToTimeAvailable || !status.RestoreToTimeConfigured {
		t.Fatalf("expected compose default restore command to make restore-to-time ready: %#v", status)
	}

	t.Setenv("SUPADUPA_COMPOSE_APPLY", "")
	status, err = ProjectRecoverabilityWithOptions(ctx, store, project.Ref, ProjectRecoverabilityOptions{RestoreToTimeConfigured: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "restore-to-time-ready" || !status.OffHostBackupConfigured || !status.OffHostBackupVerified || !status.PhysicalBackupAvailable || !status.RestoreToTimeAvailable || !status.RestoreToTimeConfigured {
		t.Fatalf("unexpected restore-to-time status: %#v", status)
	}
	if status.RecoveryWindowStart == nil || status.RecoveryWindowEnd == nil || status.RestoreToTimeUnavailable != "" {
		t.Fatalf("expected recovery window and no unavailable reason: %#v", status)
	}
}

func TestRestoreToTimeRunsConfiguredCommand(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	root := t.TempDir()
	project := createBackupTestProject(t, ctx, store, "restore-pitr")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	markBackupTargetPassed(t, ctx, store, target.ID)
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(root, "base.tar")
	if err := os.WriteFile(basePath, []byte("physical base backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	baseChecksum, err := checksumFileSHA256(basePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.CreateBackup(ctx, BackupInput{
		ProjectRef:      project.Ref,
		Kind:            "physical",
		Location:        basePath,
		RemoteLocation:  "s3://backups/supadupa/projects/restore-pitr/backups/base.tar",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len("physical base backup")),
		ChecksumSHA256:  baseChecksum,
		Status:          "completed",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}
	enablePITRPolicy(t, ctx, store, project.Ref)
	walPath := filepath.Join(root, "wal")
	if err := os.WriteFile(walPath, []byte("wal archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	walChecksum, err := checksumFileSHA256(walPath)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:      project.Ref,
		Segment:         "000000010000000000000001",
		SegmentSource:   "postgres",
		Location:        walPath,
		RemoteLocation:  "s3://backups/supadupa/projects/restore-pitr/wal/000000010000000000000001.wal",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len("wal archive")),
		ChecksumSHA256:  walChecksum,
		Status:          "archived",
		VerifiedAt:      &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	walPath2 := filepath.Join(root, "wal2")
	if err := os.WriteFile(walPath2, []byte("wal archive 2"), 0o600); err != nil {
		t.Fatal(err)
	}
	walChecksum2, err := checksumFileSHA256(walPath2)
	if err != nil {
		t.Fatal(err)
	}
	archive2, err := store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:      project.Ref,
		Segment:         "000000010000000000000002",
		SegmentSource:   "postgres",
		Location:        walPath2,
		RemoteLocation:  "s3://backups/supadupa/projects/restore-pitr/wal/000000010000000000000002.wal",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len("wal archive 2")),
		ChecksumSHA256:  walChecksum2,
		Status:          "archived",
		VerifiedAt:      &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	archive3, err := store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:      project.Ref,
		Segment:         "000000010000000000000003",
		SegmentSource:   "postgres",
		Location:        filepath.Join(root, "missing-later.wal"),
		RemoteLocation:  "s3://backups/supadupa/projects/restore-pitr/wal/000000010000000000000003.wal",
		StorageTargetID: target.ID,
		SizeBytes:       99,
		ChecksumSHA256:  strings.Repeat("c", 64),
		Status:          "archived",
		VerifiedAt:      &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:            root,
		PITRRestoreCommand: "printf 'restore %s from %s via %s range %s paths ' {{recovery_time_target_unix}} {{backup_path}} {{wal_segment}} {{wal_segments}}; printf '%s|' {{wal_path_args}}; printf '\\n'",
	})

	result, recoverability, err := service.RestoreToTime(ctx, store, project.Ref, archive2.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "completed" || result.RecoveryTimeTargetUnix != archive2.CreatedAt.Unix() || !recoverability.RestoreToTimeAvailable {
		t.Fatalf("unexpected PITR restore result=%#v recoverability=%#v", result, recoverability)
	}
	transcript, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "restore "+fmt.Sprintf("%d", result.RecoveryTimeTargetUnix)) ||
		!strings.Contains(string(transcript), basePath) ||
		!strings.Contains(string(transcript), archive2.Segment) ||
		!strings.Contains(string(transcript), archive.Segment+","+archive2.Segment) ||
		strings.Contains(string(transcript), archive3.Segment) ||
		!strings.Contains(string(transcript), walPath+"|") ||
		!strings.Contains(string(transcript), walPath2+"|") {
		t.Fatalf("expected PITR restore transcript with target, backup, and WAL, got:\n%s", string(transcript))
	}
}

func TestRestoreToTimeUsesComposeApplyDefaultCommand(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	root := t.TempDir()
	projectRoot := t.TempDir()
	project := createBackupTestProject(t, ctx, store, "compose-pitr-default")
	projectDir := filepath.Join(projectRoot, project.Ref)
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replicaDir := filepath.Join(projectDir, "replicas")
	if err := os.MkdirAll(replicaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replicaDir, "compose.yaml"), []byte("services:\n  db-replica-east: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	volumePath := filepath.Join(root, "volume")
	if err := os.MkdirAll(filepath.Join(volumePath, "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(volumePath, "old", "stale"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	logPath := filepath.Join(root, "docker.log")
	dockerPath := filepath.Join(fakeBin, "docker")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n" +
		"case \"$*\" in\n" +
		"  *'volume inspect'* ) printf '%s\\n' \"$SUPADUPA_TEST_VOLUME_PATH\" ;;\n" +
		"  *'compose -p compose-pitr-default'* ) exit 0 ;;\n" +
		"  * ) echo unexpected docker invocation >&2; exit 2 ;;\n" +
		"esac\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SUPADUPA_TEST_VOLUME_PATH", volumePath)
	t.Setenv("SUPADUPA_COMPOSE_APPLY", "true")
	t.Setenv("SUPADUPA_COMPOSE_BACKUP_DEFAULTS", "")
	t.Setenv("SUPADUPA_PITR_RESTORE_COMMAND", "")

	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	markBackupTargetPassed(t, ctx, store, target.ID)
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(root, "physical.base")
	if err := writePhysicalBaseArchive(basePath, "000000010000000000000001"); err != nil {
		t.Fatal(err)
	}
	baseChecksum, err := checksumFileSHA256(basePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.CreateBackup(ctx, BackupInput{
		ProjectRef:      project.Ref,
		Kind:            "physical",
		Location:        basePath,
		RemoteLocation:  "s3://backups/supadupa/projects/compose-pitr-default/backups/physical.base",
		StorageTargetID: target.ID,
		SizeBytes:       fileSize(t, basePath),
		ChecksumSHA256:  baseChecksum,
		Status:          "completed",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}
	enablePITRPolicy(t, ctx, store, project.Ref)
	walPath := filepath.Join(root, "000000010000000000000002")
	if err := os.WriteFile(walPath, []byte("later wal segment"), 0o600); err != nil {
		t.Fatal(err)
	}
	walChecksum, err := checksumFileSHA256(walPath)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:      project.Ref,
		Segment:         "000000010000000000000002",
		SegmentSource:   "postgres",
		Location:        walPath,
		RemoteLocation:  "s3://backups/supadupa/projects/compose-pitr-default/wal/000000010000000000000002.wal",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len("later wal segment")),
		ChecksumSHA256:  walChecksum,
		Status:          "archived",
		VerifiedAt:      &now,
	})
	if err != nil {
		t.Fatal(err)
	}

	service := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: root, ProjectRoot: projectRoot})
	result, recoverability, err := service.RestoreToTime(ctx, store, project.Ref, archive.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "completed" || !recoverability.RestoreToTimeAvailable {
		t.Fatalf("unexpected restore result=%#v recoverability=%#v", result, recoverability)
	}
	assertFileContains(t, filepath.Join(volumePath, "PG_VERSION"), "15")
	assertFileContains(t, filepath.Join(volumePath, "pg_wal", "000000010000000000000001"), "base wal segment")
	assertFileContains(t, filepath.Join(volumePath, "pg_wal", "000000010000000000000002"), "later wal segment")
	if _, err := os.Stat(filepath.Join(volumePath, "old", "stale")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale volume contents removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(volumePath, "recovery.signal")); err != nil {
		t.Fatalf("expected recovery.signal: %v", err)
	}
	assertFileContains(t, filepath.Join(volumePath, "postgresql.auto.conf"), "recovery_target_time")
	transcript, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "PITR restore completed ref=compose-pitr-default") {
		t.Fatalf("expected default restore transcript, got:\n%s", string(transcript))
	}
	dockerLog, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"compose -p compose-pitr-default -f " + projectDir + "/compose.yaml -f " + projectDir + "/replicas/compose.yaml stop",
		"compose -p compose-pitr-default -f " + projectDir + "/compose.yaml -f " + projectDir + "/replicas/compose.yaml up -d",
		"volume inspect compose-pitr-default_db-data",
	} {
		if !strings.Contains(string(dockerLog), expected) {
			t.Fatalf("expected docker log to contain %q, got:\n%s", expected, string(dockerLog))
		}
	}
}

func TestRestoreToTimeSelectsPhysicalBackupBeforeTarget(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	root := t.TempDir()
	project := createBackupTestProject(t, ctx, store, "restore-pitr-base-order")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	markBackupTargetPassed(t, ctx, store, target.ID)
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	oldBasePath := filepath.Join(root, "old-base.tar")
	if err := os.WriteFile(oldBasePath, []byte("old physical base backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldBaseChecksum, err := checksumFileSHA256(oldBasePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.CreateBackup(ctx, BackupInput{
		ProjectRef:      project.Ref,
		Kind:            "physical",
		Location:        oldBasePath,
		RemoteLocation:  "s3://backups/supadupa/projects/restore-pitr-base-order/backups/old-base.tar",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len("old physical base backup")),
		ChecksumSHA256:  oldBaseChecksum,
		Status:          "completed",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}
	enablePITRPolicy(t, ctx, store, project.Ref)
	time.Sleep(time.Millisecond)
	walPath := filepath.Join(root, "wal")
	if err := os.WriteFile(walPath, []byte("wal archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	walChecksum, err := checksumFileSHA256(walPath)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:      project.Ref,
		Segment:         "000000010000000000000003",
		SegmentSource:   "postgres",
		Location:        walPath,
		RemoteLocation:  "s3://backups/supadupa/projects/restore-pitr-base-order/wal/000000010000000000000003.wal",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len("wal archive")),
		ChecksumSHA256:  walChecksum,
		Status:          "archived",
		VerifiedAt:      &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	newBasePath := filepath.Join(root, "new-base.tar")
	if err := os.WriteFile(newBasePath, []byte("new physical base backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	newBaseChecksum, err := checksumFileSHA256(newBasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateBackup(ctx, BackupInput{
		ProjectRef:      project.Ref,
		Kind:            "physical",
		Location:        newBasePath,
		RemoteLocation:  "s3://backups/supadupa/projects/restore-pitr-base-order/backups/new-base.tar",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len("new physical base backup")),
		ChecksumSHA256:  newBaseChecksum,
		Status:          "completed",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:            root,
		PITRRestoreCommand: "printf 'restore from %s via %s\\n' {{backup_path}} {{wal_segment}}",
	})

	result, recoverability, err := service.RestoreToTime(ctx, store, project.Ref, archive.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !recoverability.RestoreToTimeAvailable {
		t.Fatalf("expected restore-to-time available with old base and WAL: %#v", recoverability)
	}
	transcript, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), oldBasePath) || strings.Contains(string(transcript), newBasePath) {
		t.Fatalf("expected PITR restore to use base before target, got:\n%s", string(transcript))
	}
}

func TestRestoreToTimeRejectsMissingWALArtifact(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	root := t.TempDir()
	project := createBackupTestProject(t, ctx, store, "restore-pitr-missing-wal")
	target, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	markBackupTargetPassed(t, ctx, store, target.ID)
	if _, err := store.UpdateBackupPolicy(ctx, project.Ref, BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(root, "base.tar")
	if err := os.WriteFile(basePath, []byte("physical base backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	baseChecksum, err := checksumFileSHA256(basePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.CreateBackup(ctx, BackupInput{
		ProjectRef:      project.Ref,
		Kind:            "physical",
		Location:        basePath,
		RemoteLocation:  "s3://backups/supadupa/projects/restore-pitr-missing-wal/backups/base.tar",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len("physical base backup")),
		ChecksumSHA256:  baseChecksum,
		Status:          "completed",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}
	enablePITRPolicy(t, ctx, store, project.Ref)
	archive, err := store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:      project.Ref,
		Segment:         "000000010000000000000002",
		SegmentSource:   "postgres",
		Location:        filepath.Join(root, "missing.wal"),
		RemoteLocation:  "s3://backups/supadupa/projects/restore-pitr-missing-wal/wal/000000010000000000000002.wal",
		StorageTargetID: target.ID,
		SizeBytes:       11,
		ChecksumSHA256:  strings.Repeat("b", 64),
		Status:          "archived",
		VerifiedAt:      &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewBackupServiceWithOptions(BackupServiceOptions{
		RootDir:            root,
		PITRRestoreCommand: "printf 'restore should not run\\n'",
		Uploader:           &fakeBackupUploader{walDownloadPayload: []byte("bad")},
	})

	_, _, err = service.RestoreToTime(ctx, store, project.Ref, archive.CreatedAt)
	if err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("expected WAL artifact validation error, got %v", err)
	}
}

func TestEnsureWALArchiveArtifactRejectsChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	walPath := filepath.Join(root, "segment.wal")
	if err := os.WriteFile(walPath, []byte("actual wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store := NewMemoryStore()
	archive := WALArchive{
		ID:             "wal_1",
		ProjectRef:     "checksum-wal",
		Segment:        "000000010000000000000003",
		SegmentSource:  "postgres",
		Location:       walPath,
		SizeBytes:      int64(len("actual wal")),
		ChecksumSHA256: strings.Repeat("c", 64),
		Status:         "archived",
	}
	err := NewBackupServiceWithOptions(BackupServiceOptions{RootDir: root}).EnsureWALArchiveArtifact(ctx, store, &archive)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected WAL checksum mismatch, got %v", err)
	}
}

func writePhysicalBaseArchive(path string, baseWALSegment string) error {
	baseTar, err := tarBytes(map[string][]byte{
		"PG_VERSION":           []byte("15\n"),
		"postgresql.auto.conf": []byte("# base config\n"),
	})
	if err != nil {
		return err
	}
	walTar, err := tarBytes(map[string][]byte{
		baseWALSegment: []byte("base wal segment"),
	})
	if err != nil {
		return err
	}
	outer, err := os.Create(path)
	if err != nil {
		return err
	}
	defer outer.Close()
	writer := tar.NewWriter(outer)
	for name, payload := range map[string][]byte{
		"./base.tar":        baseTar,
		"./pg_wal.tar":      walTar,
		"./backup_manifest": []byte("{}\n"),
	} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(payload))}); err != nil {
			_ = writer.Close()
			return err
		}
		if _, err := writer.Write(payload); err != nil {
			_ = writer.Close()
			return err
		}
	}
	return writer.Close()
}

func tarBytes(files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for name, payload := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(payload))}); err != nil {
			_ = writer.Close()
			return nil, err
		}
		if _, err := writer.Write(payload); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func assertFileContains(t *testing.T, path string, expected string) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), expected) {
		t.Fatalf("expected %s to contain %q, got:\n%s", path, expected, string(payload))
	}
}

func firstConcreteLocalInterfaceIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP == nil || ipNet.IP.IsLoopback() || ipNet.IP.IsUnspecified() {
			continue
		}
		if ipv4 := ipNet.IP.To4(); ipv4 != nil {
			return ipv4.String()
		}
	}
	return ""
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

type fakeBackupUploader struct {
	target             BackupStorageTarget
	project            Project
	localPath          string
	filename           string
	walPath            string
	walFilename        string
	platformPath       string
	platformFilename   string
	downloadTarget     BackupStorageTarget
	downloadRemote     string
	downloadPath       string
	walDownloadTarget  BackupStorageTarget
	walDownloadRemote  string
	walDownloadPath    string
	downloadPayload    []byte
	walDownloadPayload []byte
}

func (u *fakeBackupUploader) UploadBackupArtifact(ctx context.Context, target BackupStorageTarget, project Project, localPath string, filename string) (string, error) {
	u.target = target
	u.project = project
	u.localPath = localPath
	u.filename = filename
	return backupRemoteLocation(target, backupObjectKey(target, project, filename)), nil
}

func (u *fakeBackupUploader) UploadWALArchiveArtifact(ctx context.Context, target BackupStorageTarget, project Project, localPath string, filename string) (string, error) {
	u.target = target
	u.project = project
	u.walPath = localPath
	u.walFilename = filename
	return backupRemoteLocation(target, walArchiveObjectKey(target, project, filename)), nil
}

func (u *fakeBackupUploader) UploadPlatformBackupArtifact(ctx context.Context, target BackupStorageTarget, localPath string, filename string) (string, error) {
	u.target = target
	u.platformPath = localPath
	u.platformFilename = filename
	return backupRemoteLocation(target, platformBackupObjectKey(target, filename)), nil
}

func (u *fakeBackupUploader) DownloadBackupArtifact(ctx context.Context, target BackupStorageTarget, remoteLocation string, localPath string) error {
	u.downloadTarget = target
	u.downloadRemote = remoteLocation
	u.downloadPath = localPath
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(localPath, u.downloadPayload, 0o600)
}

func (u *fakeBackupUploader) DownloadWALArchiveArtifact(ctx context.Context, target BackupStorageTarget, remoteLocation string, localPath string) error {
	u.walDownloadTarget = target
	u.walDownloadRemote = remoteLocation
	u.walDownloadPath = localPath
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(localPath, u.walDownloadPayload, 0o600)
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

func markBackupTargetPassed(t *testing.T, ctx context.Context, store *MemoryStore, id string) {
	t.Helper()
	if _, err := store.UpdateBackupStorageTargetTestResult(ctx, id, time.Now().UTC(), "passed", ""); err != nil {
		t.Fatal(err)
	}
}
