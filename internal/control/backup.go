package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type BackupService struct {
	rootDir        string
	projectRoot    string
	command        string
	restoreCommand string
	walCommand     string
	dryRun         bool
	restoreDryRun  bool
	walDryRun      bool
}

type BackupServiceOptions struct {
	RootDir        string
	ProjectRoot    string
	Command        string
	RestoreCommand string
	WALCommand     string
	DryRun         bool
	RestoreDryRun  bool
	WALDryRun      bool
}

type RestoreResult struct {
	Path  string `json:"path"`
	State string `json:"state"`
}

func NewBackupService(rootDir string) *BackupService {
	return NewBackupServiceWithOptions(BackupServiceOptions{RootDir: rootDir})
}

func NewBackupServiceWithOptions(opts BackupServiceOptions) *BackupService {
	rootDir := opts.RootDir
	if rootDir == "" {
		rootDir = os.Getenv("SUPADUPA_BACKUP_ROOT")
	}
	if rootDir == "" {
		rootDir = "./runtime/backups"
	}
	projectRoot := opts.ProjectRoot
	if projectRoot == "" {
		projectRoot = os.Getenv("SUPADUPA_PROJECT_ROOT")
	}
	if projectRoot == "" {
		projectRoot = "./runtime/projects"
	}
	command := opts.Command
	if command == "" {
		command = os.Getenv("SUPADUPA_LOGICAL_BACKUP_COMMAND")
	}
	restoreCommand := opts.RestoreCommand
	if restoreCommand == "" {
		restoreCommand = os.Getenv("SUPADUPA_LOGICAL_RESTORE_COMMAND")
	}
	walCommand := opts.WALCommand
	if walCommand == "" {
		walCommand = os.Getenv("SUPADUPA_WAL_ARCHIVE_COMMAND")
	}
	dryRun := opts.DryRun || command == "" || envBool("SUPADUPA_BACKUP_DRY_RUN")
	restoreDryRun := opts.RestoreDryRun || restoreCommand == "" || envBool("SUPADUPA_RESTORE_DRY_RUN")
	walDryRun := opts.WALDryRun || walCommand == "" || envBool("SUPADUPA_WAL_ARCHIVE_DRY_RUN")
	return &BackupService{
		rootDir:        rootDir,
		projectRoot:    projectRoot,
		command:        command,
		restoreCommand: restoreCommand,
		walCommand:     walCommand,
		dryRun:         dryRun,
		restoreDryRun:  restoreDryRun,
		walDryRun:      walDryRun,
	}
}

func (s *BackupService) TriggerLogicalBackup(ctx context.Context, store Store, project Project) (Backup, error) {
	now := time.Now().UTC()
	dir := filepath.Join(s.rootDir, project.Ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Backup{}, err
	}

	filename := fmt.Sprintf("%s-logical.sql", now.Format("20060102T150405Z"))
	path := filepath.Join(dir, filename)
	if s.dryRun {
		if err := s.writeDryRunLogicalBackup(path, project, now); err != nil {
			return Backup{}, err
		}
	} else {
		if err := s.runLogicalBackupCommand(ctx, path, project); err != nil {
			return Backup{}, err
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		return Backup{}, err
	}
	checksum, err := checksumFileSHA256(path)
	if err != nil {
		return Backup{}, err
	}
	return store.CreateBackup(ctx, BackupInput{
		ProjectRef:     project.Ref,
		Kind:           "logical",
		Location:       path,
		SizeBytes:      info.Size(),
		ChecksumSHA256: checksum,
		Status:         "completed",
		VerifiedAt:     &now,
	})
}

func (s *BackupService) writeDryRunLogicalBackup(path string, project Project, now time.Time) error {
	body := fmt.Sprintf(`-- supadupa logical backup
-- mode: dry-run
-- project_ref: %s
-- project_id: %s
-- stack_version: %s
-- created_at: %s
--
-- Configure SUPADUPA_LOGICAL_BACKUP_COMMAND to execute pg_dump for live data-plane backups.
-- The command stdout is written to this .sql artifact.

BEGIN;
-- no-op dry-run backup marker for %s
ROLLBACK;
`, project.Ref, project.ID, project.Spec.StackVersion, now.Format(time.RFC3339Nano), project.Ref)
	return os.WriteFile(path, []byte(body), 0o600)
}

func (s *BackupService) runLogicalBackupCommand(ctx context.Context, path string, project Project) error {
	command := renderBackupCommand(s.command, project, s.projectRoot, s.rootDir, path)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("logical backup command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if len(output) == 0 {
		return fmt.Errorf("logical backup command produced no output")
	}
	if err := os.WriteFile(path, output, 0o600); err != nil {
		return err
	}
	return nil
}

func renderBackupCommand(template string, project Project, projectRoot string, backupRoot string, backupPath string) string {
	projectDir := filepath.Join(projectRoot, project.Ref)
	replacements := map[string]string{
		"{{backup_path}}":   shellQuote(backupPath),
		"{{backup_root}}":   shellQuote(backupRoot),
		"{{compose_file}}":  shellQuote(filepath.Join(projectDir, "compose.yaml")),
		"{{domain}}":        shellQuote(project.Spec.Domain),
		"{{project_dir}}":   shellQuote(projectDir),
		"{{project_id}}":    shellQuote(project.ID),
		"{{project_ref}}":   shellQuote(project.Ref),
		"{{ref}}":           shellQuote(project.Ref),
		"{{stack_version}}": shellQuote(project.Spec.StackVersion),
	}
	out := template
	for token, value := range replacements {
		out = strings.ReplaceAll(out, token, value)
	}
	return out
}

func (s *BackupService) ArchiveWALSegment(ctx context.Context, store Store, project Project) (WALArchive, error) {
	policy, err := store.GetPITRPolicy(ctx, project.Ref)
	if err != nil {
		return WALArchive{}, err
	}
	if !policy.Enabled {
		return WALArchive{}, fmt.Errorf("PITR is disabled for project %s", project.Ref)
	}

	now := time.Now().UTC()
	segment := fmt.Sprintf("%024X", now.UnixNano())
	dir := filepath.Join(s.rootDir, project.Ref, "wal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return WALArchive{}, err
	}
	filename := fmt.Sprintf("%s-%s.wal", now.Format("20060102T150405Z"), segment)
	path := filepath.Join(dir, filename)
	if s.walDryRun {
		if err := s.writeDryRunWALArchive(path, project, policy, segment, now); err != nil {
			return WALArchive{}, err
		}
	} else {
		if err := s.runWALArchiveCommand(ctx, path, project, policy, segment); err != nil {
			return WALArchive{}, err
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return WALArchive{}, err
	}
	checksum, err := checksumFileSHA256(path)
	if err != nil {
		return WALArchive{}, err
	}
	return store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:     project.Ref,
		Segment:        segment,
		Location:       path,
		SizeBytes:      info.Size(),
		ChecksumSHA256: checksum,
		Status:         "archived",
		VerifiedAt:     &now,
	})
}

func (s *BackupService) writeDryRunWALArchive(path string, project Project, policy PITRPolicy, segment string, now time.Time) error {
	body := fmt.Sprintf(`-- supadupa WAL archive
-- mode: dry-run
-- project_ref: %s
-- project_id: %s
-- segment: %s
-- archive_bucket: %s
-- retention_days: %d
-- created_at: %s
--
-- Configure SUPADUPA_WAL_ARCHIVE_COMMAND to execute WAL-G, pgBackRest, or an operator wrapper.
-- The command stdout is written to this WAL archive artifact.

WAL-SEGMENT %s
`, project.Ref, project.ID, segment, policy.ArchiveBucket, policy.RetentionDays, now.Format(time.RFC3339Nano), segment)
	return os.WriteFile(path, []byte(body), 0o600)
}

func (s *BackupService) runWALArchiveCommand(ctx context.Context, path string, project Project, policy PITRPolicy, segment string) error {
	command := renderWALArchiveCommand(s.walCommand, project, policy, segment, s.projectRoot, s.rootDir, path)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("WAL archive command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if len(output) == 0 {
		return fmt.Errorf("WAL archive command produced no output")
	}
	if err := os.WriteFile(path, output, 0o600); err != nil {
		return err
	}
	return nil
}

func renderWALArchiveCommand(template string, project Project, policy PITRPolicy, segment string, projectRoot string, backupRoot string, archivePath string) string {
	projectDir := filepath.Join(projectRoot, project.Ref)
	replacements := map[string]string{
		"{{archive_bucket}}": shellQuote(policy.ArchiveBucket),
		"{{backup_root}}":    shellQuote(backupRoot),
		"{{compose_file}}":   shellQuote(filepath.Join(projectDir, "compose.yaml")),
		"{{domain}}":         shellQuote(project.Spec.Domain),
		"{{project_dir}}":    shellQuote(projectDir),
		"{{project_id}}":     shellQuote(project.ID),
		"{{project_ref}}":    shellQuote(project.Ref),
		"{{ref}}":            shellQuote(project.Ref),
		"{{retention_days}}": shellQuote(fmt.Sprintf("%d", policy.RetentionDays)),
		"{{segment}}":        shellQuote(segment),
		"{{wal_path}}":       shellQuote(archivePath),
		"{{wal_root}}":       shellQuote(filepath.Dir(archivePath)),
	}
	out := template
	for token, value := range replacements {
		out = strings.ReplaceAll(out, token, value)
	}
	return out
}

func (s *BackupService) RunDueBackups(ctx context.Context, store Store, now time.Time) ([]Backup, error) {
	projects, err := store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	completed := make([]Backup, 0)
	for _, project := range projects {
		policy, err := store.GetBackupPolicy(ctx, project.Ref)
		if err != nil {
			return completed, err
		}
		if !policy.Enabled || policy.NextRunAt == nil || policy.NextRunAt.After(now) {
			continue
		}
		backup, err := s.TriggerLogicalBackup(ctx, store, project)
		if err != nil {
			LogProject(ctx, store, project.Ref, "error", "Scheduled backup failed", map[string]string{"error": err.Error()})
			Audit(ctx, store, "project.backup_scheduled_failed", "project:"+project.Ref, map[string]string{"error": err.Error()})
			continue
		}
		_, _ = store.MarkBackupPolicyRun(ctx, project.Ref, now)
		LogProject(ctx, store, project.Ref, "info", "Scheduled backup completed", map[string]string{"backup_id": backup.ID})
		Audit(ctx, store, "project.backup_scheduled", "project:"+project.Ref, map[string]string{"backup_id": backup.ID})
		completed = append(completed, backup)
	}
	return completed, nil
}

func (s *BackupService) RestoreBackup(ctx context.Context, store Store, ref string, backupID string) (Backup, RestoreResult, error) {
	backup, err := store.GetBackup(ctx, ref, backupID)
	if err != nil {
		return Backup{}, RestoreResult{}, err
	}
	if backup.Status != "completed" {
		return Backup{}, RestoreResult{}, fmt.Errorf("backup %s is not completed", backup.ID)
	}
	if backup.Kind != "logical" {
		return Backup{}, RestoreResult{}, fmt.Errorf("backup %s kind %q cannot be restored by logical restore", backup.ID, backup.Kind)
	}
	if _, err := os.Stat(backup.Location); err != nil {
		return Backup{}, RestoreResult{}, fmt.Errorf("backup artifact is not readable: %w", err)
	}
	now := time.Now().UTC()
	dir := filepath.Join(s.rootDir, ref, "restores")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Backup{}, RestoreResult{}, err
	}

	if s.restoreDryRun {
		path := filepath.Join(dir, fmt.Sprintf("%s-restore-%s.sql", now.Format("20060102T150405Z"), backup.ID))
		if err := s.writeDryRunRestorePlan(path, ref, backup, now); err != nil {
			return Backup{}, RestoreResult{}, err
		}
		return backup, RestoreResult{Path: path, State: "dry-run"}, nil
	}

	path := filepath.Join(dir, fmt.Sprintf("%s-restore-%s.log", now.Format("20060102T150405Z"), backup.ID))
	if err := s.runLogicalRestoreCommand(ctx, path, ref, backup); err != nil {
		return Backup{}, RestoreResult{}, err
	}
	return backup, RestoreResult{Path: path, State: "completed"}, nil
}

func (s *BackupService) writeDryRunRestorePlan(path string, ref string, backup Backup, now time.Time) error {
	body := fmt.Sprintf(`-- supadupa logical restore
-- mode: dry-run
-- project_ref: %s
-- backup_id: %s
-- backup_kind: %s
-- backup_location: %s
-- created_at: %s
--
-- Configure SUPADUPA_LOGICAL_RESTORE_COMMAND to execute psql/pg_restore against the target data plane.
-- The command output is written to a restore transcript.

BEGIN;
-- dry-run restore marker for %s from %s
ROLLBACK;
`, ref, backup.ID, backup.Kind, backup.Location, now.Format(time.RFC3339Nano), ref, backup.Location)
	return os.WriteFile(path, []byte(body), 0o600)
}

func (s *BackupService) runLogicalRestoreCommand(ctx context.Context, path string, ref string, backup Backup) error {
	command := renderRestoreCommand(s.restoreCommand, ref, backup, s.projectRoot, s.rootDir, path)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		if writeErr := os.WriteFile(path, output, 0o600); writeErr != nil {
			return writeErr
		}
	}
	if err != nil {
		return fmt.Errorf("logical restore command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if len(output) == 0 {
		return fmt.Errorf("logical restore command produced no transcript")
	}
	return nil
}

func checksumFileSHA256(path string) (string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func renderRestoreCommand(template string, ref string, backup Backup, projectRoot string, backupRoot string, restorePath string) string {
	projectDir := filepath.Join(projectRoot, ref)
	replacements := map[string]string{
		"{{backup_id}}":    shellQuote(backup.ID),
		"{{backup_kind}}":  shellQuote(backup.Kind),
		"{{backup_path}}":  shellQuote(backup.Location),
		"{{backup_root}}":  shellQuote(backupRoot),
		"{{compose_file}}": shellQuote(filepath.Join(projectDir, "compose.yaml")),
		"{{project_dir}}":  shellQuote(projectDir),
		"{{project_ref}}":  shellQuote(ref),
		"{{ref}}":          shellQuote(ref),
		"{{restore_path}}": shellQuote(restorePath),
		"{{restore_root}}": shellQuote(filepath.Dir(restorePath)),
	}
	out := template
	for token, value := range replacements {
		out = strings.ReplaceAll(out, token, value)
	}
	return out
}

func envBool(key string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
