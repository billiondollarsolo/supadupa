package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"supadupa2026/internal/env"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

type BackupService struct {
	rootDir            string
	projectRoot        string
	command            string
	physicalCommand    string
	restoreCommand     string
	walCommand         string
	pitrRestoreCommand string
	dryRun             bool
	restoreDryRun      bool
	walDryRun          bool
	uploader           BackupArtifactUploader
}

type BackupServiceOptions struct {
	RootDir            string
	ProjectRoot        string
	Command            string
	PhysicalCommand    string
	RestoreCommand     string
	WALCommand         string
	PITRRestoreCommand string
	DryRun             bool
	RestoreDryRun      bool
	WALDryRun          bool
	Uploader           BackupArtifactUploader
}

type RestoreResult struct {
	Path  string `json:"path"`
	State string `json:"state"`
}

type RestoreToTimeResult struct {
	Path                   string    `json:"path"`
	State                  string    `json:"state"`
	RecoveryTimeTargetUnix int64     `json:"recovery_time_target_unix"`
	RecoveryTimeTarget     time.Time `json:"recovery_time_target"`
}

type PlatformRestoreResult struct {
	Path  string `json:"path"`
	State string `json:"state"`
}

type BackupArtifactUploader interface {
	UploadBackupArtifact(ctx context.Context, target BackupStorageTarget, project Project, localPath string, filename string) (string, error)
	UploadWALArchiveArtifact(ctx context.Context, target BackupStorageTarget, project Project, localPath string, filename string) (string, error)
	UploadPlatformBackupArtifact(ctx context.Context, target BackupStorageTarget, localPath string, filename string) (string, error)
	DownloadBackupArtifact(ctx context.Context, target BackupStorageTarget, remoteLocation string, localPath string) error
	DownloadWALArchiveArtifact(ctx context.Context, target BackupStorageTarget, remoteLocation string, localPath string) error
}

type controlPlaneCheckpointExporter interface {
	ExportControlPlaneCheckpoint(ctx context.Context) ([]byte, error)
}

type controlPlaneCheckpointImporter interface {
	ImportControlPlaneCheckpoint(ctx context.Context, payload []byte, preservedPlatformBackups ...PlatformBackup) error
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
	if command == "" && composeBackupDefaultsEnabled() {
		command = "docker compose -p {{ref}} -f {{compose_file}} exec -T db pg_dump --clean --if-exists --quote-all-identifiers --schema=public --schema=auth --schema=storage --schema=supabase_migrations -U postgres postgres"
	}
	physicalCommand := opts.PhysicalCommand
	if physicalCommand == "" {
		physicalCommand = os.Getenv("SUPADUPA_PHYSICAL_BACKUP_COMMAND")
	}
	if physicalCommand == "" && composeBackupDefaultsEnabled() {
		physicalCommand = "docker compose -p {{ref}} -f {{compose_file}} exec -T db sh -c 'tmp=\"$(mktemp -d /tmp/supadupa-basebackup.XXXXXX)\" && trap \"rm -rf \\\"$tmp\\\"\" EXIT && PGPASSWORD=\"$POSTGRES_PASSWORD\" pg_basebackup -D \"$tmp\" -Ft -X stream -c fast -U supabase_replication_admin -h 127.0.0.1 -p 5432 && tar -C \"$tmp\" -cf - .'"
	}
	restoreCommand := opts.RestoreCommand
	if restoreCommand == "" {
		restoreCommand = os.Getenv("SUPADUPA_LOGICAL_RESTORE_COMMAND")
	}
	if restoreCommand == "" && composeBackupDefaultsEnabled() {
		restoreCommand = "db_password=\"$(sed -n 's/^POSTGRES_PASSWORD=//p' {{project_dir}}/.env | tail -n 1)\" && docker compose -p {{ref}} -f {{compose_file}} exec -T -e PGPASSWORD=\"$db_password\" db psql -v ON_ERROR_STOP=1 -U supabase_admin postgres < {{backup_path}}"
	}
	walCommand := opts.WALCommand
	if walCommand == "" {
		walCommand = os.Getenv("SUPADUPA_WAL_ARCHIVE_COMMAND")
	}
	if walCommand == "" && composeBackupDefaultsEnabled() {
		walCommand = "segment=\"$(docker compose -p {{ref}} -f {{compose_file}} exec -T db psql -At -U supabase_admin -d postgres -c 'select pg_walfile_name(pg_switch_wal())')\" && segment=\"$(printf '%s' \"$segment\" | tr -d '\\r')\" && test -n \"$segment\" && docker compose -p {{ref}} -f {{compose_file}} exec -T db sh -c 'cat \"$PGDATA/pg_wal/$1\"' _ \"$segment\" > {{wal_path}} && printf '%s\\n' \"$segment\""
	}
	pitrRestoreCommand := opts.PITRRestoreCommand
	if pitrRestoreCommand == "" {
		pitrRestoreCommand = os.Getenv("SUPADUPA_PITR_RESTORE_COMMAND")
	}
	if pitrRestoreCommand == "" && composeBackupDefaultsEnabled() {
		pitrRestoreCommand = "set -eu; ref={{ref}}; backup={{backup_path}}; target={{recovery_time_target}}; tmp=\"$(mktemp -d)\"; trap 'rm -rf \"$tmp\"' EXIT; echo \"PITR restore starting ref=$ref target=$target\"; docker compose -p \"$ref\" {{compose_file_args}} stop; volume_path=\"$(docker volume inspect \"${ref}_db-data\" --format '{{ .Mountpoint }}')\"; test -n \"$volume_path\"; tar -C \"$tmp\" -xf \"$backup\"; test -f \"$tmp/base.tar\"; test -f \"$tmp/pg_wal.tar\"; rm -rf \"$volume_path\"/* \"$volume_path\"/.[!.]* \"$volume_path\"/..?* 2>/dev/null || true; tar -C \"$volume_path\" -xf \"$tmp/base.tar\"; mkdir -p \"$volume_path/pg_wal\"; tar -C \"$volume_path/pg_wal\" -xf \"$tmp/pg_wal.tar\"; for wal_path in {{wal_path_args}}; do cp \"$wal_path\" \"$volume_path/pg_wal/\"; done; rm -f \"$volume_path/postmaster.pid\"; touch \"$volume_path/recovery.signal\"; { printf '\\nrecovery_target_time = '\\''%s'\\''\\n' \"$target\"; printf 'recovery_target_action = '\\''promote'\\''\\n'; } >> \"$volume_path/postgresql.auto.conf\"; docker compose -p \"$ref\" {{compose_file_args}} up -d; echo \"PITR restore completed ref=$ref target=$target\""
	}
	dryRun := opts.DryRun || command == "" || env.Bool("SUPADUPA_BACKUP_DRY_RUN")
	restoreDryRun := opts.RestoreDryRun || restoreCommand == "" || env.Bool("SUPADUPA_RESTORE_DRY_RUN")
	walDryRun := opts.WALDryRun || env.Bool("SUPADUPA_WAL_ARCHIVE_DRY_RUN")
	uploader := opts.Uploader
	if uploader == nil {
		uploader = s3BackupArtifactUploader{}
	}
	return &BackupService{
		rootDir:            rootDir,
		projectRoot:        projectRoot,
		command:            command,
		physicalCommand:    physicalCommand,
		restoreCommand:     restoreCommand,
		walCommand:         walCommand,
		pitrRestoreCommand: pitrRestoreCommand,
		dryRun:             dryRun,
		restoreDryRun:      restoreDryRun,
		walDryRun:          walDryRun,
		uploader:           uploader,
	}
}

func composeBackupDefaultsEnabled() bool {
	if !env.Bool("SUPADUPA_COMPOSE_APPLY") {
		return false
	}
	value := strings.ToLower(strings.TrimSpace(os.Getenv("SUPADUPA_COMPOSE_BACKUP_DEFAULTS")))
	return value != "false" && value != "0" && value != "off"
}

func (s *BackupService) TriggerLogicalBackup(ctx context.Context, store Store, project Project) (Backup, error) {
	startedAt := time.Now().UTC()
	dir := filepath.Join(s.rootDir, project.Ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Backup{}, err
	}

	filename := fmt.Sprintf("%s-logical.sql", startedAt.Format("20060102T150405Z"))
	path := filepath.Join(dir, filename)
	if s.dryRun {
		if err := s.writeDryRunLogicalBackup(path, project, startedAt); err != nil {
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
	var remoteLocation string
	var storageTargetID string
	target, ok, err := s.resolveBackupTarget(ctx, store, project.Ref)
	if err != nil {
		return Backup{}, err
	}
	if ok {
		remoteLocation, err = s.uploader.UploadBackupArtifact(ctx, target, project, path, filename)
		if err != nil {
			return Backup{}, err
		}
		storageTargetID = target.ID
	}
	finishedAt := time.Now().UTC()
	return store.CreateBackup(ctx, BackupInput{
		ProjectRef:      project.Ref,
		Kind:            "logical",
		Location:        path,
		RemoteLocation:  remoteLocation,
		StorageTargetID: storageTargetID,
		SizeBytes:       info.Size(),
		ChecksumSHA256:  checksum,
		Status:          "completed",
		StartedAt:       startedAt,
		FinishedAt:      &finishedAt,
		VerifiedAt:      &finishedAt,
	})
}

func (s *BackupService) TriggerPhysicalBackup(ctx context.Context, store Store, project Project) (Backup, error) {
	if strings.TrimSpace(s.physicalCommand) == "" {
		return Backup{}, fmt.Errorf("physical backup command is not configured")
	}
	target, hasTarget, err := s.resolveBackupTarget(ctx, store, project.Ref)
	if err != nil {
		return Backup{}, err
	}
	if err := ensureRecoveryReadyBackupTarget(target, hasTarget, "physical backup upload"); err != nil {
		return Backup{}, err
	}
	startedAt := time.Now().UTC()
	dir := filepath.Join(s.rootDir, project.Ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Backup{}, err
	}

	filename := fmt.Sprintf("%s-physical.base", startedAt.Format("20060102T150405Z"))
	path := filepath.Join(dir, filename)
	if err := s.runPhysicalBackupCommand(ctx, path, project); err != nil {
		return Backup{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return Backup{}, err
	}
	checksum, err := checksumFileSHA256(path)
	if err != nil {
		return Backup{}, err
	}
	var remoteLocation string
	var storageTargetID string
	if hasTarget {
		remoteLocation, err = s.uploader.UploadBackupArtifact(ctx, target, project, path, filename)
		if err != nil {
			return Backup{}, err
		}
		storageTargetID = target.ID
	}
	finishedAt := time.Now().UTC()
	return store.CreateBackup(ctx, BackupInput{
		ProjectRef:      project.Ref,
		Kind:            "physical",
		Location:        path,
		RemoteLocation:  remoteLocation,
		StorageTargetID: storageTargetID,
		SizeBytes:       info.Size(),
		ChecksumSHA256:  checksum,
		Status:          "completed",
		StartedAt:       startedAt,
		FinishedAt:      &finishedAt,
		VerifiedAt:      &finishedAt,
	})
}

func (s *BackupService) TriggerPlatformBackup(ctx context.Context, store Store) (PlatformBackup, error) {
	startedAt := time.Now().UTC()
	dir := filepath.Join(s.rootDir, "_platform")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return PlatformBackup{}, err
	}
	filename := fmt.Sprintf("%s-control-plane.json", startedAt.Format("20060102T150405Z"))
	path := filepath.Join(dir, filename)
	if err := s.writePlatformBackupArtifact(ctx, store, path, startedAt); err != nil {
		return PlatformBackup{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return PlatformBackup{}, err
	}
	checksum, err := checksumFileSHA256(path)
	if err != nil {
		return PlatformBackup{}, err
	}
	var remoteLocation string
	var storageTargetID string
	target, ok, err := s.resolveDefaultBackupTarget(ctx, store)
	if err != nil {
		return PlatformBackup{}, err
	}
	if ok {
		remoteLocation, err = s.uploader.UploadPlatformBackupArtifact(ctx, target, path, filename)
		if err != nil {
			return PlatformBackup{}, err
		}
		storageTargetID = target.ID
	}
	finishedAt := time.Now().UTC()
	return store.CreatePlatformBackup(ctx, PlatformBackupInput{
		Kind:            "control-plane",
		Location:        path,
		RemoteLocation:  remoteLocation,
		StorageTargetID: storageTargetID,
		SizeBytes:       info.Size(),
		ChecksumSHA256:  checksum,
		Status:          "completed",
		StartedAt:       startedAt,
		FinishedAt:      &finishedAt,
		VerifiedAt:      &finishedAt,
	})
}

func (s *BackupService) writePlatformBackupArtifact(ctx context.Context, store Store, path string, now time.Time) error {
	payload := map[string]any{
		"format":     "supadupa-control-plane-backup/v1",
		"created_at": now.Format(time.RFC3339Nano),
	}
	if exporter, ok := store.(controlPlaneCheckpointExporter); ok {
		checkpoint, err := exporter.ExportControlPlaneCheckpoint(ctx)
		if err != nil {
			return err
		}
		payload["mode"] = "encrypted-checkpoint"
		payload["checkpoint_id"] = "default"
		payload["checkpoint_encoding"] = "base64"
		payload["encrypted_checkpoint"] = base64.StdEncoding.EncodeToString(checkpoint)
	} else {
		payload["mode"] = "dry-run"
		payload["note"] = "SUPADUPA_META_DSN is not configured; no encrypted control-plane checkpoint is available."
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o600)
}

func (s *BackupService) RestorePlatformBackup(ctx context.Context, store Store, backupID string) (PlatformBackup, PlatformRestoreResult, error) {
	importer, ok := store.(controlPlaneCheckpointImporter)
	if !ok {
		return PlatformBackup{}, PlatformRestoreResult{}, fmt.Errorf("control-plane restore requires persistent meta DB checkpoint storage")
	}
	backup, err := store.GetPlatformBackup(ctx, backupID)
	if err != nil {
		return PlatformBackup{}, PlatformRestoreResult{}, err
	}
	if backup.Status != "completed" {
		return PlatformBackup{}, PlatformRestoreResult{}, fmt.Errorf("platform backup %s is not completed", backup.ID)
	}
	if backup.Kind != "control-plane" {
		return PlatformBackup{}, PlatformRestoreResult{}, fmt.Errorf("platform backup %s kind %q cannot be restored as control-plane state", backup.ID, backup.Kind)
	}
	if err := s.ensurePlatformBackupArtifact(ctx, store, &backup); err != nil {
		return PlatformBackup{}, PlatformRestoreResult{}, err
	}
	checkpoint, err := readControlPlaneCheckpointBackupArtifact(backup.Location)
	if err != nil {
		return PlatformBackup{}, PlatformRestoreResult{}, err
	}
	if err := importer.ImportControlPlaneCheckpoint(ctx, checkpoint, backup); err != nil {
		return PlatformBackup{}, PlatformRestoreResult{}, err
	}
	return backup, PlatformRestoreResult{Path: backup.Location, State: "metadata-restored"}, nil
}

func (s *BackupService) ensurePlatformBackupArtifact(ctx context.Context, store Store, backup *PlatformBackup) error {
	if backup == nil {
		return fmt.Errorf("platform backup is required")
	}
	if strings.TrimSpace(backup.Location) == "" {
		if strings.TrimSpace(backup.RemoteLocation) == "" {
			return fmt.Errorf("platform backup %s does not include an artifact location", backup.ID)
		}
		backup.Location = filepath.Join(s.rootDir, "_platform", filenameFromS3RemoteLocation(backup.RemoteLocation))
	}
	if _, err := os.Stat(backup.Location); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("platform backup artifact is not readable: %w", err)
		}
		if err := s.downloadPlatformBackupArtifact(ctx, store, backup); err != nil {
			return err
		}
	}
	info, err := os.Stat(backup.Location)
	if err != nil {
		return fmt.Errorf("platform backup artifact is not readable: %w", err)
	}
	if backup.SizeBytes > 0 && info.Size() != backup.SizeBytes {
		return fmt.Errorf("platform backup %s size mismatch", backup.ID)
	}
	if strings.TrimSpace(backup.ChecksumSHA256) == "" {
		return fmt.Errorf("platform backup %s does not include a checksum", backup.ID)
	}
	actual, err := checksumFileSHA256(backup.Location)
	if err != nil {
		return fmt.Errorf("platform backup artifact is not readable: %w", err)
	}
	if !strings.EqualFold(actual, strings.TrimSpace(backup.ChecksumSHA256)) {
		return fmt.Errorf("platform backup %s checksum mismatch", backup.ID)
	}
	return nil
}

func (s *BackupService) downloadPlatformBackupArtifact(ctx context.Context, store Store, backup *PlatformBackup) error {
	if strings.TrimSpace(backup.RemoteLocation) == "" {
		return fmt.Errorf("platform backup artifact is not readable: local file is missing and backup %s has no remote location", backup.ID)
	}
	if strings.TrimSpace(backup.StorageTargetID) == "" {
		return fmt.Errorf("platform backup artifact is not readable: backup %s has no storage target for remote location", backup.ID)
	}
	target, err := store.GetBackupStorageTarget(ctx, backup.StorageTargetID)
	if err != nil {
		return fmt.Errorf("platform backup artifact is not readable: storage target %s is unavailable: %w", backup.StorageTargetID, err)
	}
	if err := s.uploader.DownloadBackupArtifact(ctx, target, backup.RemoteLocation, backup.Location); err != nil {
		return fmt.Errorf("platform backup artifact is not readable: %w", err)
	}
	return nil
}

type controlPlaneBackupArtifact struct {
	Format              string `json:"format"`
	Mode                string `json:"mode"`
	CheckpointEncoding  string `json:"checkpoint_encoding"`
	EncryptedCheckpoint string `json:"encrypted_checkpoint"`
}

func readControlPlaneCheckpointBackupArtifact(path string) ([]byte, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var artifact controlPlaneBackupArtifact
	if err := json.Unmarshal(payload, &artifact); err != nil {
		return nil, fmt.Errorf("decode control-plane backup artifact: %w", err)
	}
	if artifact.Format != "supadupa-control-plane-backup/v1" {
		return nil, fmt.Errorf("unsupported control-plane backup format %q", artifact.Format)
	}
	if artifact.Mode != "encrypted-checkpoint" {
		return nil, fmt.Errorf("control-plane backup mode %q is not restorable", artifact.Mode)
	}
	if artifact.CheckpointEncoding != "base64" {
		return nil, fmt.Errorf("unsupported control-plane checkpoint encoding %q", artifact.CheckpointEncoding)
	}
	checkpoint, err := base64.StdEncoding.DecodeString(artifact.EncryptedCheckpoint)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted control-plane checkpoint: %w", err)
	}
	if len(checkpoint) == 0 {
		return nil, fmt.Errorf("control-plane backup checkpoint is empty")
	}
	return checkpoint, nil
}

func (s *BackupService) resolveBackupTarget(ctx context.Context, store Store, ref string) (BackupStorageTarget, bool, error) {
	policy, err := store.GetBackupPolicy(ctx, ref)
	if err != nil {
		return BackupStorageTarget{}, false, err
	}
	if strings.TrimSpace(policy.StorageTargetID) != "" {
		target, err := store.GetBackupStorageTarget(ctx, policy.StorageTargetID)
		return target, err == nil, err
	}
	return s.resolveDefaultBackupTarget(ctx, store)
}

func (s *BackupService) resolveDefaultBackupTarget(ctx context.Context, store Store) (BackupStorageTarget, bool, error) {
	targets, err := store.ListBackupStorageTargets(ctx)
	if err != nil {
		return BackupStorageTarget{}, false, err
	}
	for _, target := range targets {
		if target.Default {
			fullTarget, err := store.GetBackupStorageTarget(ctx, target.ID)
			return fullTarget, err == nil, err
		}
	}
	return BackupStorageTarget{}, false, nil
}

type s3BackupArtifactUploader struct{}

func (s3BackupArtifactUploader) UploadBackupArtifact(ctx context.Context, target BackupStorageTarget, project Project, localPath string, filename string) (string, error) {
	key := backupObjectKey(target, project, filename)
	return uploadS3BackupArtifact(ctx, target, localPath, key)
}

func (s3BackupArtifactUploader) UploadWALArchiveArtifact(ctx context.Context, target BackupStorageTarget, project Project, localPath string, filename string) (string, error) {
	key := walArchiveObjectKey(target, project, filename)
	return uploadS3BackupArtifact(ctx, target, localPath, key)
}

func (s3BackupArtifactUploader) UploadPlatformBackupArtifact(ctx context.Context, target BackupStorageTarget, localPath string, filename string) (string, error) {
	key := platformBackupObjectKey(target, filename)
	return uploadS3BackupArtifact(ctx, target, localPath, key)
}

func (s3BackupArtifactUploader) DownloadBackupArtifact(ctx context.Context, target BackupStorageTarget, remoteLocation string, localPath string) error {
	bucket, key, err := parseS3RemoteLocation(remoteLocation)
	if err != nil {
		return err
	}
	if bucket != target.Bucket {
		return fmt.Errorf("backup remote bucket %q does not match storage target bucket %q", bucket, target.Bucket)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		return err
	}

	client, err := s3ClientForBackupTarget(ctx, target)
	if err != nil {
		return err
	}
	object, err := client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(target.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("download backup artifact from %s: %w", remoteLocation, err)
	}
	defer object.Body.Close()

	temp, err := os.CreateTemp(filepath.Dir(localPath), ".backup-download-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(temp, object.Body); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tempPath, localPath)
}

func (u s3BackupArtifactUploader) DownloadWALArchiveArtifact(ctx context.Context, target BackupStorageTarget, remoteLocation string, localPath string) error {
	return u.DownloadBackupArtifact(ctx, target, remoteLocation, localPath)
}

func TestBackupStorageTarget(ctx context.Context, target BackupStorageTarget) error {
	client, err := s3ClientForBackupTarget(ctx, target)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	key := strings.Trim(strings.TrimSpace(target.Prefix), "/")
	if key != "" {
		key += "/"
	}
	key += "_supadupa-checks/" + now.Format("20060102T150405Z") + "-" + newID() + ".txt"
	body := []byte("supadupa backup target probe " + now.Format(time.RFC3339Nano) + "\n")
	remoteLocation := backupRemoteLocation(target, key)
	if _, err := client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: aws.String(target.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	}); err != nil {
		return fmt.Errorf("write backup target probe to %s: %w", remoteLocation, err)
	}
	object, err := client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(target.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("read backup target probe from %s: %w", remoteLocation, err)
	}
	defer object.Body.Close()
	readBody, err := io.ReadAll(object.Body)
	if err != nil {
		return fmt.Errorf("read backup target probe body from %s: %w", remoteLocation, err)
	}
	if !bytes.Equal(readBody, body) {
		return fmt.Errorf("read backup target probe from %s returned unexpected content", remoteLocation)
	}
	_, _ = client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(target.Bucket),
		Key:    aws.String(key),
	})
	return nil
}

func uploadS3BackupArtifact(ctx context.Context, target BackupStorageTarget, localPath string, key string) (string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	client, err := s3ClientForBackupTarget(ctx, target)
	if err != nil {
		return "", err
	}
	if _, err := client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: aws.String(target.Bucket),
		Key:    aws.String(key),
		Body:   file,
	}); err != nil {
		return "", fmt.Errorf("upload backup artifact to %s: %w", backupRemoteLocation(target, key), err)
	}
	return backupRemoteLocation(target, key), nil
}

func s3ClientForBackupTarget(ctx context.Context, target BackupStorageTarget) (*awss3.Client, error) {
	region := strings.TrimSpace(target.Region)
	if region == "" || region == "auto" {
		region = "us-east-1"
	}
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(target.AccessKeyID, target.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load S3 config: %w", err)
	}
	client := awss3.NewFromConfig(cfg, func(opts *awss3.Options) {
		opts.UsePathStyle = target.ForcePathStyle
		if target.Endpoint != "" {
			opts.BaseEndpoint = aws.String(target.Endpoint)
		}
	})
	return client, nil
}

func backupObjectKey(target BackupStorageTarget, project Project, filename string) string {
	parts := []string{}
	if prefix := strings.Trim(strings.TrimSpace(target.Prefix), "/"); prefix != "" {
		parts = append(parts, prefix)
	}
	parts = append(parts, "projects", project.Ref, "backups", filename)
	return strings.Join(parts, "/")
}

func walArchiveObjectKey(target BackupStorageTarget, project Project, filename string) string {
	parts := []string{}
	if prefix := strings.Trim(strings.TrimSpace(target.Prefix), "/"); prefix != "" {
		parts = append(parts, prefix)
	}
	parts = append(parts, "projects", project.Ref, "wal", filename)
	return strings.Join(parts, "/")
}

func platformBackupObjectKey(target BackupStorageTarget, filename string) string {
	parts := []string{}
	if prefix := strings.Trim(strings.TrimSpace(target.Prefix), "/"); prefix != "" {
		parts = append(parts, prefix)
	}
	parts = append(parts, "platform", "backups", filename)
	return strings.Join(parts, "/")
}

func backupRemoteLocation(target BackupStorageTarget, key string) string {
	return "s3://" + target.Bucket + "/" + strings.TrimLeft(key, "/")
}

func parseS3RemoteLocation(remoteLocation string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(remoteLocation))
	if err != nil {
		return "", "", fmt.Errorf("invalid backup remote location: %w", err)
	}
	if parsed.Scheme != "s3" || parsed.Host == "" {
		return "", "", fmt.Errorf("backup remote location must be an s3:// URI")
	}
	key := strings.TrimLeft(parsed.Path, "/")
	if key == "" {
		return "", "", fmt.Errorf("backup remote location does not include an object key")
	}
	return parsed.Host, key, nil
}

func filenameFromS3RemoteLocation(remoteLocation string) string {
	_, key, err := parseS3RemoteLocation(remoteLocation)
	if err != nil {
		return "remote-backup.sql"
	}
	filename := filepath.Base(key)
	if filename == "." || filename == "/" || filename == "" {
		return "remote-backup.sql"
	}
	return filename
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

func (s *BackupService) runPhysicalBackupCommand(ctx context.Context, path string, project Project) error {
	command := renderBackupCommand(s.physicalCommand, project, s.projectRoot, s.rootDir, path)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("physical backup command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if len(output) == 0 {
		return fmt.Errorf("physical backup command produced no output")
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
	target, hasTarget, err := s.resolveBackupTarget(ctx, store, project.Ref)
	if err != nil {
		return WALArchive{}, err
	}
	if err := ensureRecoveryReadyBackupTarget(target, hasTarget, "WAL archive upload"); err != nil {
		return WALArchive{}, err
	}

	now := time.Now().UTC()
	segment := fallbackWALSegment(now)
	segmentSource := "dry-run"
	dir := filepath.Join(s.rootDir, project.Ref, "wal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return WALArchive{}, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-pending.wal", now.Format("20060102T150405Z")))
	if s.walDryRun {
		if err := s.writeDryRunWALArchive(path, project, policy, segment, now); err != nil {
			return WALArchive{}, err
		}
	} else {
		if strings.TrimSpace(s.walCommand) == "" {
			return WALArchive{}, fmt.Errorf("WAL archive command is not configured")
		}
		archivedSegment, archivedSegmentSource, err := s.runWALArchiveCommand(ctx, path, project, policy, segment)
		if err != nil {
			return WALArchive{}, err
		}
		segment = archivedSegment
		segmentSource = archivedSegmentSource
	}
	if !isPostgresWALSegment(segment) {
		return WALArchive{}, fmt.Errorf("WAL archive command returned invalid segment %q", segment)
	}
	filename := fmt.Sprintf("%s-%s.wal", now.Format("20060102T150405Z"), segment)
	finalPath := filepath.Join(dir, filename)
	if path != finalPath {
		if err := os.Rename(path, finalPath); err != nil {
			return WALArchive{}, err
		}
		path = finalPath
	}
	info, err := os.Stat(path)
	if err != nil {
		return WALArchive{}, err
	}
	checksum, err := checksumFileSHA256(path)
	if err != nil {
		return WALArchive{}, err
	}
	var remoteLocation string
	var storageTargetID string
	if hasTarget {
		remoteLocation, err = s.uploader.UploadWALArchiveArtifact(ctx, target, project, path, filename)
		if err != nil {
			return WALArchive{}, err
		}
		storageTargetID = target.ID
	}
	return store.CreateWALArchive(ctx, WALArchiveInput{
		ProjectRef:      project.Ref,
		Segment:         segment,
		SegmentSource:   segmentSource,
		Location:        path,
		RemoteLocation:  remoteLocation,
		StorageTargetID: storageTargetID,
		SizeBytes:       info.Size(),
		ChecksumSHA256:  checksum,
		Status:          "archived",
		VerifiedAt:      &now,
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

func (s *BackupService) runWALArchiveCommand(ctx context.Context, path string, project Project, policy PITRPolicy, segment string) (string, string, error) {
	command := renderWALArchiveCommand(s.walCommand, project, policy, segment, s.projectRoot, s.rootDir, path)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("WAL archive command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if len(output) == 0 {
		return "", "", fmt.Errorf("WAL archive command produced no output")
	}
	outputText := strings.TrimSpace(string(output))
	if isPostgresWALSegment(outputText) {
		if info, statErr := os.Stat(path); statErr == nil && info.Size() > 0 {
			return strings.ToUpper(outputText), "postgres", nil
		}
		return "", "", fmt.Errorf("WAL archive command returned segment %s but did not write {{wal_path}}", outputText)
	}
	if err := os.WriteFile(path, output, 0o600); err != nil {
		return "", "", err
	}
	return segment, "legacy-command", nil
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
		backup, err := s.TriggerBackupForKind(ctx, store, project, policy.Kind)
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

func (s *BackupService) TriggerBackupForKind(ctx context.Context, store Store, project Project, kind string) (Backup, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "logical":
		return s.TriggerLogicalBackup(ctx, store, project)
	case "physical":
		return s.TriggerPhysicalBackup(ctx, store, project)
	default:
		return Backup{}, fmt.Errorf("unsupported backup kind %q", kind)
	}
}

func (s *BackupService) RunDueWALArchives(ctx context.Context, store Store, now time.Time, interval time.Duration) ([]WALArchive, error) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	completed := make([]WALArchive, 0)
	for _, project := range projects {
		if !pitrArchiveEligible(project.Status) {
			continue
		}
		policy, err := store.GetPITRPolicy(ctx, project.Ref)
		if err != nil {
			return completed, err
		}
		if !pitrArchiveDue(policy, now, interval) {
			continue
		}
		archive, err := s.ArchiveWALSegment(ctx, store, project)
		if err != nil {
			LogProject(ctx, store, project.Ref, "error", "Scheduled WAL archive failed", map[string]string{"error": err.Error()})
			Audit(ctx, store, "project.wal_archive_scheduled_failed", "project:"+project.Ref, map[string]string{"error": err.Error()})
			continue
		}
		LogProject(ctx, store, project.Ref, "info", "Scheduled WAL archive completed", map[string]string{"wal_archive_id": archive.ID, "segment": archive.Segment})
		Audit(ctx, store, "project.wal_archive_scheduled", "project:"+project.Ref, map[string]string{"wal_archive_id": archive.ID, "segment": archive.Segment})
		completed = append(completed, archive)
	}
	return completed, nil
}

func ProjectRecoverability(ctx context.Context, store Store, ref string) (ProjectRecoverabilityStatus, error) {
	return ProjectRecoverabilityWithOptions(ctx, store, ref, ProjectRecoverabilityOptions{RestoreToTimeConfigured: pitrRestoreConfiguredFromEnv()})
}

type ProjectRecoverabilityOptions struct {
	RestoreToTimeConfigured bool
}

func ProjectRecoverabilityWithOptions(ctx context.Context, store Store, ref string, opts ProjectRecoverabilityOptions) (ProjectRecoverabilityStatus, error) {
	policy, err := store.GetBackupPolicy(ctx, ref)
	if err != nil {
		return ProjectRecoverabilityStatus{}, err
	}
	backups, err := store.ListBackups(ctx, ref)
	if err != nil {
		return ProjectRecoverabilityStatus{}, err
	}
	targets, err := store.ListBackupStorageTargets(ctx)
	if err != nil {
		return ProjectRecoverabilityStatus{}, err
	}
	pitrPolicy, err := store.GetPITRPolicy(ctx, ref)
	if err != nil {
		return ProjectRecoverabilityStatus{}, err
	}
	walArchives, err := store.ListWALArchives(ctx, ref)
	if err != nil {
		return ProjectRecoverabilityStatus{}, err
	}

	status := ProjectRecoverabilityStatus{
		ProjectRef:               ref,
		Status:                   "unprotected",
		BackupPolicyEnabled:      policy.Enabled,
		OffHostBackupConfigured:  backupPolicyHasOffHostTarget(policy, targets),
		PITREnabled:              pitrPolicy.Enabled,
		RestoreToTimeConfigured:  opts.RestoreToTimeConfigured,
		Warnings:                 []string{},
		Recommendations:          []string{},
		RestoreToTimeUnavailable: "physical base backup plus WAL replay is not configured",
	}
	if len(backups) > 0 {
		latest := backups[0]
		status.LatestBackup = &latest
	}
	if backup, ok := latestVerifiedBackup(backups, ""); ok {
		status.LatestVerifiedBackup = &backup
	}
	targetsByID := backupStorageTargetsByID(targets)
	if backup, ok := latestVerifiedOffHostBackup(backups, targetsByID); ok {
		status.OffHostBackupVerified = true
		if status.LatestVerifiedBackup == nil {
			status.LatestVerifiedBackup = &backup
		}
	}
	if _, ok := latestVerifiedOffHostBackup(backups, targetsByID, "physical"); ok {
		status.PhysicalBackupAvailable = true
	}
	if archive, ok := latestVerifiedWALArchive(walArchives); ok {
		status.LatestWALArchive = &archive
	}
	if archive, ok := latestVerifiedOffHostWALArchive(walArchives, targetsByID); ok {
		status.LatestWALArchive = &archive
		status.WALArchiveOffHostVerified = true
		if base, ok := latestVerifiedOffHostBackupAtOrBefore(backups, targetsByID, archive.CreatedAt, "physical"); ok {
			windowEnd := archive.CreatedAt
			windowStart := windowEnd.Add(-time.Duration(pitrPolicy.RetentionDays) * 24 * time.Hour)
			if base.CreatedAt.After(windowStart) {
				windowStart = base.CreatedAt
			}
			if !windowStart.After(windowEnd) {
				status.RecoveryWindowStart = &windowStart
				status.RecoveryWindowEnd = &windowEnd
			}
		}
	}
	status.RestoreToTimeAvailable = status.PITREnabled && status.PhysicalBackupAvailable && status.WALArchiveOffHostVerified && status.RestoreToTimeConfigured && status.RecoveryWindowStart != nil && status.RecoveryWindowEnd != nil
	if status.RestoreToTimeAvailable {
		status.RestoreToTimeUnavailable = ""
	}

	if !status.BackupPolicyEnabled {
		status.Warnings = append(status.Warnings, "scheduled logical backups are disabled")
		status.Recommendations = append(status.Recommendations, "enable scheduled backups for baseline recovery")
	}
	if !status.OffHostBackupConfigured {
		if reason := backupPolicyTargetReadinessIssue(policy, targets); reason != "" {
			status.Warnings = append(status.Warnings, reason)
			switch {
			case strings.Contains(reason, "local or loopback"):
				status.Recommendations = append(status.Recommendations, "configure a durable S3-compatible target on another host or provider so backups survive host loss")
			case strings.Contains(reason, "has not passed validation"):
				status.Recommendations = append(status.Recommendations, "test the selected backup target from Settings > Backups or the CLI before relying on off-host recovery")
			default:
				status.Recommendations = append(status.Recommendations, "configure a default or project backup target so backups survive host loss")
			}
		} else if backupPolicyHasAnyTarget(policy, targets) {
			status.Warnings = append(status.Warnings, "configured backup target is local or loopback and does not satisfy off-host recovery")
			status.Recommendations = append(status.Recommendations, "configure a durable S3-compatible target on another host or provider so backups survive host loss")
		} else {
			status.Warnings = append(status.Warnings, "no S3-compatible backup target is selected or configured as platform default")
			status.Recommendations = append(status.Recommendations, "configure a default or project backup target so backups survive host loss")
		}
	}
	if status.OffHostBackupConfigured && !status.OffHostBackupVerified {
		status.Warnings = append(status.Warnings, "no completed verified backup has been copied off-host yet")
		status.Recommendations = append(status.Recommendations, "run a backup after configuring the off-host target and verify remote_location is present")
	}
	if !status.PITREnabled {
		status.Warnings = append(status.Warnings, "PITR is disabled")
		status.Recommendations = append(status.Recommendations, "enable PITR and configure WAL archival for restore-to-time workflows")
	}
	if status.PITREnabled && status.LatestWALArchive == nil {
		status.Warnings = append(status.Warnings, "PITR is enabled but no verified WAL archive exists yet")
		status.Recommendations = append(status.Recommendations, "wait for the WAL scheduler or archive a segment manually")
	}
	if status.PITREnabled && status.LatestWALArchive != nil && !walArchiveHasPostgresSegment(*status.LatestWALArchive) {
		status.Warnings = append(status.Warnings, "latest WAL archive does not include a verified Postgres WAL segment identity")
		status.Recommendations = append(status.Recommendations, "archive WAL with a command that writes {{wal_path}} and returns the Postgres WAL filename on stdout")
	}
	if status.PITREnabled && status.LatestWALArchive != nil && !status.WALArchiveOffHostVerified {
		status.Warnings = append(status.Warnings, "verified WAL archives exist only on local disk")
		status.Recommendations = append(status.Recommendations, "configure an S3-compatible target and archive WAL again so PITR survives host loss")
	}
	if !status.PhysicalBackupAvailable {
		status.Warnings = append(status.Warnings, "no verified physical base backup is available for PITR restore")
		status.Recommendations = append(status.Recommendations, "configure physical base backups with WAL-G or pgBackRest before relying on restore-to-time")
	}
	if status.PITREnabled && status.PhysicalBackupAvailable && status.WALArchiveOffHostVerified && !status.RestoreToTimeConfigured {
		status.Warnings = append(status.Warnings, "no PITR restore command is configured")
		status.Recommendations = append(status.Recommendations, "configure SUPADUPA_PITR_RESTORE_COMMAND to replay the selected physical backup and WAL range")
	}
	if status.PITREnabled && status.PhysicalBackupAvailable && status.WALArchiveOffHostVerified && status.RecoveryWindowStart == nil {
		status.Warnings = append(status.Warnings, "no verified physical base backup is available at or before the latest WAL archive")
		status.Recommendations = append(status.Recommendations, "run a WAL archive after the latest physical base backup before relying on restore-to-time")
	}

	switch {
	case status.RestoreToTimeAvailable:
		status.Status = "restore-to-time-ready"
	case status.OffHostBackupVerified:
		status.Status = "off-host-backup-ready"
	case status.LatestVerifiedBackup != nil:
		status.Status = "local-backup-only"
	case status.BackupPolicyEnabled:
		status.Status = "scheduled-pending"
	}
	return status, nil
}

func pitrRestoreConfiguredFromEnv() bool {
	return strings.TrimSpace(os.Getenv("SUPADUPA_PITR_RESTORE_COMMAND")) != "" || composeBackupDefaultsEnabled()
}

func backupPolicyHasOffHostTarget(policy BackupPolicy, targets []BackupStorageTarget) bool {
	for _, target := range targets {
		if strings.TrimSpace(policy.StorageTargetID) != "" {
			if target.ID == policy.StorageTargetID && backupStorageTargetIsReadyOffHost(target) {
				return true
			}
			continue
		}
		if target.Default && backupStorageTargetIsReadyOffHost(target) {
			return true
		}
	}
	return false
}

func backupPolicyHasAnyTarget(policy BackupPolicy, targets []BackupStorageTarget) bool {
	policyTargetID := strings.TrimSpace(policy.StorageTargetID)
	for _, target := range targets {
		if policyTargetID != "" {
			if target.ID == policyTargetID && target.SecretConfigured {
				return true
			}
			continue
		}
		if target.Default && target.SecretConfigured {
			return true
		}
	}
	return false
}

func backupPolicyTargetReadinessIssue(policy BackupPolicy, targets []BackupStorageTarget) string {
	target, ok := backupPolicySelectedTarget(policy, targets)
	if !ok {
		return ""
	}
	if !target.SecretConfigured {
		return "configured backup target is missing secret credentials"
	}
	if !backupStorageTargetIsDurableOffHost(target) {
		return "configured backup target is local or loopback and does not satisfy off-host recovery"
	}
	if !strings.EqualFold(strings.TrimSpace(target.LastTestStatus), "passed") {
		return "configured backup target has not passed validation"
	}
	return ""
}

func backupPolicySelectedTarget(policy BackupPolicy, targets []BackupStorageTarget) (BackupStorageTarget, bool) {
	policyTargetID := strings.TrimSpace(policy.StorageTargetID)
	for _, target := range targets {
		if policyTargetID != "" {
			if target.ID == policyTargetID {
				return target, true
			}
			continue
		}
		if target.Default {
			return target, true
		}
	}
	return BackupStorageTarget{}, false
}

func backupStorageTargetsByID(targets []BackupStorageTarget) map[string]BackupStorageTarget {
	byID := make(map[string]BackupStorageTarget, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	return byID
}

func backupStorageTargetIsDurableOffHost(target BackupStorageTarget) bool {
	endpoint := strings.TrimSpace(target.Endpoint)
	if endpoint == "" {
		return true
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := strings.Trim(strings.ToLower(parsed.Hostname()), "[]")
	if host == "" {
		return false
	}
	switch host {
	case "localhost", "localhost.localdomain", "host.docker.internal", "host.containers.internal":
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback() && !ip.IsUnspecified() && !ipAssignedToLocalInterface(ip)
	}
	for _, ip := range resolveBackupTargetHostIPs(host) {
		if ip.IsLoopback() || ip.IsUnspecified() || ipAssignedToLocalInterface(ip) {
			return false
		}
	}
	return true
}

var resolveBackupTargetHostIPs = func(host string) []net.IP {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IP != nil {
			ips = append(ips, addr.IP)
		}
	}
	return ips
}

func ipAssignedToLocalInterface(ip net.IP) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		var candidate net.IP
		switch value := addr.(type) {
		case *net.IPNet:
			candidate = value.IP
		case *net.IPAddr:
			candidate = value.IP
		}
		if candidate != nil && candidate.Equal(ip) {
			return true
		}
	}
	return false
}

func backupStorageTargetIsReadyOffHost(target BackupStorageTarget) bool {
	return target.SecretConfigured && strings.EqualFold(strings.TrimSpace(target.LastTestStatus), "passed") && backupStorageTargetIsDurableOffHost(target)
}

func requireRecoveryReadyBackupTargets() bool {
	return env.Bool("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS")
}

func ensureRecoveryReadyBackupTarget(target BackupStorageTarget, hasTarget bool, purpose string) error {
	if !requireRecoveryReadyBackupTargets() {
		return nil
	}
	if !hasTarget {
		return fmt.Errorf("backup storage target is required for %s when SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true", purpose)
	}
	if backupStorageTargetIsReadyOffHost(target) {
		return nil
	}
	_, _, status, message := backupStorageTargetReadiness(target)
	if strings.TrimSpace(message) == "" {
		message = "target must pass validation and be durable off-host"
	}
	return fmt.Errorf("backup storage target %s is not recovery-ready for %s: %s (%s)", target.ID, purpose, status, message)
}

func backupStorageTargetReadiness(target BackupStorageTarget) (durableOffHost bool, recoveryReady bool, status string, message string) {
	durableOffHost = backupStorageTargetIsDurableOffHost(target)
	if !target.SecretConfigured {
		return durableOffHost, false, "missing-secret", "secret access key is not configured"
	}
	if !durableOffHost {
		return false, false, "local-or-loopback", "endpoint resolves to this host or a loopback target and cannot satisfy off-host recovery"
	}
	switch strings.ToLower(strings.TrimSpace(target.LastTestStatus)) {
	case "passed":
		return true, true, "off-host-ready", "target passed validation and is eligible for off-host recovery artifacts"
	case "failed":
		message := strings.TrimSpace(target.LastTestError)
		if message == "" {
			message = "target validation failed"
		}
		return true, false, "validation-failed", message
	default:
		return true, false, "validation-pending", "target must pass validation before it can satisfy off-host recovery"
	}
}

func walArchiveBucketForTarget(target BackupStorageTarget, ref string) string {
	parts := []string{}
	if prefix := strings.Trim(strings.TrimSpace(target.Prefix), "/"); prefix != "" {
		parts = append(parts, prefix)
	}
	parts = append(parts, "projects", ref, "wal")
	if len(parts) == 0 {
		return "s3://" + target.Bucket
	}
	return "s3://" + target.Bucket + "/" + strings.Join(parts, "/")
}

func latestVerifiedBackup(backups []Backup, kind string) (Backup, bool) {
	kind = strings.TrimSpace(kind)
	for _, backup := range backups {
		if backup.Status != "completed" || backup.VerifiedAt == nil {
			continue
		}
		if kind != "" && backup.Kind != kind {
			continue
		}
		return backup, true
	}
	return Backup{}, false
}

func latestVerifiedOffHostBackup(backups []Backup, targetsByID map[string]BackupStorageTarget, kinds ...string) (Backup, bool) {
	kind := ""
	if len(kinds) > 0 {
		kind = strings.TrimSpace(kinds[0])
	}
	for _, backup := range backups {
		if backup.Status != "completed" || backup.VerifiedAt == nil || strings.TrimSpace(backup.RemoteLocation) == "" || strings.TrimSpace(backup.StorageTargetID) == "" {
			continue
		}
		if kind != "" && backup.Kind != kind {
			continue
		}
		target, ok := targetsByID[backup.StorageTargetID]
		if !ok || !backupStorageTargetIsReadyOffHost(target) {
			continue
		}
		return backup, true
	}
	return Backup{}, false
}

func latestVerifiedOffHostBackupAtOrBefore(backups []Backup, targetsByID map[string]BackupStorageTarget, latest time.Time, kinds ...string) (Backup, bool) {
	kind := ""
	if len(kinds) > 0 {
		kind = strings.TrimSpace(kinds[0])
	}
	for _, backup := range backups {
		if backupCreatedAfterRecoveryTarget(backup.CreatedAt, latest) {
			continue
		}
		if backup.Status != "completed" || backup.VerifiedAt == nil || strings.TrimSpace(backup.RemoteLocation) == "" || strings.TrimSpace(backup.StorageTargetID) == "" {
			continue
		}
		if kind != "" && backup.Kind != kind {
			continue
		}
		target, ok := targetsByID[backup.StorageTargetID]
		if !ok || !backupStorageTargetIsReadyOffHost(target) {
			continue
		}
		return backup, true
	}
	return Backup{}, false
}

func backupCreatedAfterRecoveryTarget(createdAt time.Time, target time.Time) bool {
	if target.Nanosecond() == 0 {
		return createdAt.Unix() > target.Unix()
	}
	return createdAt.After(target)
}

func latestVerifiedWALArchive(archives []WALArchive) (WALArchive, bool) {
	for _, archive := range archives {
		if archive.Status == "archived" && archive.VerifiedAt != nil {
			return archive, true
		}
	}
	return WALArchive{}, false
}

func latestVerifiedOffHostWALArchive(archives []WALArchive, targetsByID map[string]BackupStorageTarget) (WALArchive, bool) {
	for _, archive := range archives {
		if archive.Status == "archived" && archive.VerifiedAt != nil && walArchiveHasPostgresSegment(archive) && strings.TrimSpace(archive.RemoteLocation) != "" && strings.TrimSpace(archive.StorageTargetID) != "" {
			target, ok := targetsByID[archive.StorageTargetID]
			if !ok || !backupStorageTargetIsReadyOffHost(target) {
				continue
			}
			return archive, true
		}
	}
	return WALArchive{}, false
}

func verifiedOffHostWALArchivesInRange(archives []WALArchive, targetsByID map[string]BackupStorageTarget, start time.Time, end time.Time) []WALArchive {
	selected := make([]WALArchive, 0)
	for _, archive := range archives {
		if archive.Status != "archived" || archive.VerifiedAt == nil || !walArchiveHasPostgresSegment(archive) || strings.TrimSpace(archive.RemoteLocation) == "" || strings.TrimSpace(archive.StorageTargetID) == "" {
			continue
		}
		target, ok := targetsByID[archive.StorageTargetID]
		if !ok || !backupStorageTargetIsReadyOffHost(target) {
			continue
		}
		if archive.CreatedAt.Before(start) || timestampAfterRecoveryTarget(archive.CreatedAt, end) {
			continue
		}
		selected = append(selected, archive)
	}
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].CreatedAt.Before(selected[j].CreatedAt)
	})
	return selected
}

func timestampAfterRecoveryTarget(createdAt time.Time, target time.Time) bool {
	if target.Nanosecond() == 0 {
		return createdAt.Unix() > target.Unix()
	}
	return createdAt.After(target)
}

func fallbackWALSegment(now time.Time) string {
	return fmt.Sprintf("%024X", now.UnixNano())
}

func isPostgresWALSegment(segment string) bool {
	segment = strings.TrimSpace(segment)
	if len(segment) != 24 {
		return false
	}
	for _, r := range segment {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func walArchiveHasPostgresSegment(archive WALArchive) bool {
	return strings.TrimSpace(archive.SegmentSource) == "postgres" && isPostgresWALSegment(archive.Segment)
}

func pitrArchiveDue(policy PITRPolicy, now time.Time, interval time.Duration) bool {
	if !policy.Enabled {
		return false
	}
	if policy.LastArchiveAt == nil {
		return true
	}
	return !policy.LastArchiveAt.Add(interval).After(now.UTC())
}

func pitrArchiveEligible(status ProjectPhase) bool {
	switch status {
	case ProjectHealthy, ProjectDegraded:
		return true
	default:
		return false
	}
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
	if err := s.EnsureLogicalBackupArtifact(ctx, store, ref, &backup); err != nil {
		return Backup{}, RestoreResult{}, err
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

func (s *BackupService) EnsureLogicalBackupArtifact(ctx context.Context, store Store, ref string, backup *Backup) error {
	if backup == nil {
		return fmt.Errorf("backup is required")
	}
	if backup.Kind != "logical" {
		return fmt.Errorf("backup %s kind %q cannot be used as a logical backup artifact", backup.ID, backup.Kind)
	}
	return s.EnsureBackupArtifact(ctx, store, ref, backup)
}

func (s *BackupService) EnsureBackupArtifact(ctx context.Context, store Store, ref string, backup *Backup) error {
	if backup == nil {
		return fmt.Errorf("backup is required")
	}
	if strings.TrimSpace(backup.Location) == "" {
		if strings.TrimSpace(backup.RemoteLocation) == "" {
			return fmt.Errorf("backup %s does not include an artifact location", backup.ID)
		}
		backup.Location = filepath.Join(s.rootDir, ref, filenameFromS3RemoteLocation(backup.RemoteLocation))
	}
	if _, err := os.Stat(backup.Location); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("backup artifact is not readable: %w", err)
		}
		if err := s.downloadBackupArtifact(ctx, store, backup); err != nil {
			return err
		}
	}
	info, err := os.Stat(backup.Location)
	if err != nil {
		return fmt.Errorf("backup artifact is not readable: %w", err)
	}
	if backup.SizeBytes > 0 && info.Size() != backup.SizeBytes {
		return fmt.Errorf("backup %s size mismatch", backup.ID)
	}
	if strings.TrimSpace(backup.ChecksumSHA256) == "" {
		return fmt.Errorf("backup %s does not include a checksum", backup.ID)
	}
	actual, err := checksumFileSHA256(backup.Location)
	if err != nil {
		return fmt.Errorf("backup artifact is not readable: %w", err)
	}
	if !strings.EqualFold(actual, strings.TrimSpace(backup.ChecksumSHA256)) {
		return fmt.Errorf("backup %s checksum mismatch", backup.ID)
	}
	return nil
}

func (s *BackupService) EnsureWALArchiveArtifact(ctx context.Context, store Store, archive *WALArchive) error {
	if archive == nil {
		return fmt.Errorf("WAL archive is required")
	}
	if strings.TrimSpace(archive.Location) == "" {
		if strings.TrimSpace(archive.RemoteLocation) == "" {
			return fmt.Errorf("WAL archive %s does not include an artifact location", archive.ID)
		}
		archive.Location = filepath.Join(s.rootDir, archive.ProjectRef, "wal", filenameFromS3RemoteLocation(archive.RemoteLocation))
	}
	info, err := os.Stat(archive.Location)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("WAL archive artifact is not readable: %w", err)
		}
		if err := s.downloadWALArchiveArtifact(ctx, store, archive); err != nil {
			return err
		}
	}
	info, err = os.Stat(archive.Location)
	if err != nil {
		return fmt.Errorf("WAL archive artifact is not readable: %w", err)
	}
	if archive.SizeBytes > 0 && info.Size() != archive.SizeBytes {
		return fmt.Errorf("WAL archive %s size mismatch", archive.ID)
	}
	if strings.TrimSpace(archive.ChecksumSHA256) == "" {
		return fmt.Errorf("WAL archive %s does not include a checksum", archive.ID)
	}
	actual, err := checksumFileSHA256(archive.Location)
	if err != nil {
		return fmt.Errorf("WAL archive artifact is not readable: %w", err)
	}
	if !strings.EqualFold(actual, strings.TrimSpace(archive.ChecksumSHA256)) {
		return fmt.Errorf("WAL archive %s checksum mismatch", archive.ID)
	}
	return nil
}

func (s *BackupService) RestoreToTime(ctx context.Context, store Store, ref string, target time.Time) (RestoreToTimeResult, ProjectRecoverabilityStatus, error) {
	if target.IsZero() {
		return RestoreToTimeResult{}, ProjectRecoverabilityStatus{}, fmt.Errorf("recovery_time_target_unix is required")
	}
	target = target.UTC()
	status, err := ProjectRecoverabilityWithOptions(ctx, store, ref, ProjectRecoverabilityOptions{RestoreToTimeConfigured: strings.TrimSpace(s.pitrRestoreCommand) != ""})
	if err != nil {
		return RestoreToTimeResult{}, ProjectRecoverabilityStatus{}, err
	}
	if !status.RestoreToTimeAvailable {
		return RestoreToTimeResult{}, status, fmt.Errorf("restore-to-time is unavailable: %s", status.RestoreToTimeUnavailable)
	}
	if status.RecoveryWindowStart == nil || status.RecoveryWindowEnd == nil {
		return RestoreToTimeResult{}, status, fmt.Errorf("restore-to-time recovery window is unavailable")
	}
	if target.Unix() < status.RecoveryWindowStart.Unix() || target.Unix() > status.RecoveryWindowEnd.Unix() {
		return RestoreToTimeResult{}, status, fmt.Errorf("recovery_time_target_unix is outside the available recovery window")
	}
	project, err := store.GetProject(ctx, ref)
	if err != nil {
		return RestoreToTimeResult{}, status, err
	}
	backups, err := store.ListBackups(ctx, ref)
	if err != nil {
		return RestoreToTimeResult{}, status, err
	}
	targets, err := store.ListBackupStorageTargets(ctx)
	if err != nil {
		return RestoreToTimeResult{}, status, err
	}
	walArchives, err := store.ListWALArchives(ctx, ref)
	if err != nil {
		return RestoreToTimeResult{}, status, err
	}
	backup, ok := latestVerifiedOffHostBackupAtOrBefore(backups, backupStorageTargetsByID(targets), target, "physical")
	if !ok {
		return RestoreToTimeResult{}, status, fmt.Errorf("no verified physical base backup is available at or before the recovery target")
	}
	if err := s.EnsureBackupArtifact(ctx, store, ref, &backup); err != nil {
		return RestoreToTimeResult{}, status, err
	}
	walRange := verifiedOffHostWALArchivesInRange(walArchives, backupStorageTargetsByID(targets), backup.CreatedAt, target)
	if len(walRange) == 0 {
		return RestoreToTimeResult{}, status, fmt.Errorf("no verified WAL archive range is available for PITR restore")
	}
	for i := range walRange {
		if err := s.EnsureWALArchiveArtifact(ctx, store, &walRange[i]); err != nil {
			return RestoreToTimeResult{}, status, err
		}
	}
	now := time.Now().UTC()
	dir := filepath.Join(s.rootDir, ref, "restores")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return RestoreToTimeResult{}, status, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-pitr-restore-%d.log", now.Format("20060102T150405Z"), target.Unix()))
	if err := s.runPITRRestoreCommand(ctx, path, project, backup, walRange, target); err != nil {
		return RestoreToTimeResult{}, status, err
	}
	return RestoreToTimeResult{Path: path, State: "completed", RecoveryTimeTargetUnix: target.Unix(), RecoveryTimeTarget: target}, status, nil
}

func (s *BackupService) downloadBackupArtifact(ctx context.Context, store Store, backup *Backup) error {
	if strings.TrimSpace(backup.RemoteLocation) == "" {
		return fmt.Errorf("backup artifact is not readable: local file is missing and backup %s has no remote location", backup.ID)
	}
	if strings.TrimSpace(backup.StorageTargetID) == "" {
		return fmt.Errorf("backup artifact is not readable: backup %s has no storage target for remote location", backup.ID)
	}
	target, err := store.GetBackupStorageTarget(ctx, backup.StorageTargetID)
	if err != nil {
		return fmt.Errorf("backup artifact is not readable: storage target %s is unavailable: %w", backup.StorageTargetID, err)
	}
	if err := s.uploader.DownloadBackupArtifact(ctx, target, backup.RemoteLocation, backup.Location); err != nil {
		return fmt.Errorf("backup artifact is not readable: %w", err)
	}
	return nil
}

func (s *BackupService) downloadWALArchiveArtifact(ctx context.Context, store Store, archive *WALArchive) error {
	if strings.TrimSpace(archive.RemoteLocation) == "" {
		return fmt.Errorf("WAL archive artifact is not readable: local file is missing and WAL archive %s has no remote location", archive.ID)
	}
	if strings.TrimSpace(archive.StorageTargetID) == "" {
		return fmt.Errorf("WAL archive artifact is not readable: WAL archive %s has no storage target for remote location", archive.ID)
	}
	target, err := store.GetBackupStorageTarget(ctx, archive.StorageTargetID)
	if err != nil {
		return fmt.Errorf("WAL archive artifact is not readable: storage target %s is unavailable: %w", archive.StorageTargetID, err)
	}
	if err := s.uploader.DownloadWALArchiveArtifact(ctx, target, archive.RemoteLocation, archive.Location); err != nil {
		return fmt.Errorf("WAL archive artifact is not readable: %w", err)
	}
	return nil
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

func (s *BackupService) runPITRRestoreCommand(ctx context.Context, path string, project Project, backup Backup, walArchives []WALArchive, target time.Time) error {
	if len(walArchives) == 0 {
		return fmt.Errorf("PITR restore requires at least one WAL archive")
	}
	command := renderPITRRestoreCommand(s.pitrRestoreCommand, project, backup, walArchives, target, s.projectRoot, s.rootDir, path)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		if writeErr := os.WriteFile(path, output, 0o600); writeErr != nil {
			return writeErr
		}
	}
	if err != nil {
		return fmt.Errorf("PITR restore command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if len(output) == 0 {
		return fmt.Errorf("PITR restore command produced no transcript")
	}
	return nil
}

func checksumFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func renderRestoreCommand(template string, ref string, backup Backup, projectRoot string, backupRoot string, restorePath string) string {
	projectDir := filepath.Join(projectRoot, ref)
	composeFiles := projectComposeFiles(projectDir)
	replacements := map[string]string{
		"{{backup_id}}":         shellQuote(backup.ID),
		"{{backup_kind}}":       shellQuote(backup.Kind),
		"{{backup_path}}":       shellQuote(backup.Location),
		"{{backup_root}}":       shellQuote(backupRoot),
		"{{compose_file}}":      shellQuote(filepath.Join(projectDir, "compose.yaml")),
		"{{compose_file_args}}": composeFileArgs(composeFiles),
		"{{compose_files}}":     shellQuote(strings.Join(composeFiles, "\n")),
		"{{project_dir}}":       shellQuote(projectDir),
		"{{project_ref}}":       shellQuote(ref),
		"{{ref}}":               shellQuote(ref),
		"{{restore_path}}":      shellQuote(restorePath),
		"{{restore_root}}":      shellQuote(filepath.Dir(restorePath)),
	}
	out := template
	for token, value := range replacements {
		out = strings.ReplaceAll(out, token, value)
	}
	return out
}

func renderPITRRestoreCommand(template string, project Project, backup Backup, walArchives []WALArchive, target time.Time, projectRoot string, backupRoot string, restorePath string) string {
	projectDir := filepath.Join(projectRoot, project.Ref)
	composeFiles := projectComposeFiles(projectDir)
	latestWALArchive := walArchives[len(walArchives)-1]
	walArchiveIDs := make([]string, 0, len(walArchives))
	walPaths := make([]string, 0, len(walArchives))
	walPathArgs := make([]string, 0, len(walArchives))
	walSegments := make([]string, 0, len(walArchives))
	for _, archive := range walArchives {
		walArchiveIDs = append(walArchiveIDs, archive.ID)
		walPaths = append(walPaths, archive.Location)
		walPathArgs = append(walPathArgs, shellQuote(archive.Location))
		walSegments = append(walSegments, archive.Segment)
	}
	replacements := map[string]string{
		"{{backup_id}}":                 shellQuote(backup.ID),
		"{{backup_kind}}":               shellQuote(backup.Kind),
		"{{backup_path}}":               shellQuote(backup.Location),
		"{{backup_remote_location}}":    shellQuote(backup.RemoteLocation),
		"{{backup_root}}":               shellQuote(backupRoot),
		"{{compose_file}}":              shellQuote(filepath.Join(projectDir, "compose.yaml")),
		"{{compose_file_args}}":         composeFileArgs(composeFiles),
		"{{compose_files}}":             shellQuote(strings.Join(composeFiles, "\n")),
		"{{domain}}":                    shellQuote(project.Spec.Domain),
		"{{project_dir}}":               shellQuote(projectDir),
		"{{project_id}}":                shellQuote(project.ID),
		"{{project_ref}}":               shellQuote(project.Ref),
		"{{recovery_time_target}}":      shellQuote(target.Format(time.RFC3339)),
		"{{recovery_time_target_unix}}": shellQuote(fmt.Sprintf("%d", target.Unix())),
		"{{ref}}":                       shellQuote(project.Ref),
		"{{restore_path}}":              shellQuote(restorePath),
		"{{restore_root}}":              shellQuote(filepath.Dir(restorePath)),
		"{{wal_archive_id}}":            shellQuote(latestWALArchive.ID),
		"{{wal_archive_ids}}":           shellQuote(strings.Join(walArchiveIDs, ",")),
		"{{wal_path}}":                  shellQuote(latestWALArchive.Location),
		"{{wal_path_args}}":             strings.Join(walPathArgs, " "),
		"{{wal_paths}}":                 shellQuote(strings.Join(walPaths, "\n")),
		"{{wal_segment}}":               shellQuote(latestWALArchive.Segment),
		"{{wal_segments}}":              shellQuote(strings.Join(walSegments, ",")),
	}
	out := template
	for token, value := range replacements {
		out = strings.ReplaceAll(out, token, value)
	}
	return out
}

func projectComposeFiles(projectDir string) []string {
	files := []string{filepath.Join(projectDir, "compose.yaml")}
	replicaOverlay := filepath.Join(projectDir, "replicas", "compose.yaml")
	if _, err := os.Stat(replicaOverlay); err == nil {
		files = append(files, replicaOverlay)
	}
	return files
}

func composeFileArgs(files []string) string {
	args := make([]string, 0, len(files)*2)
	for _, file := range files {
		args = append(args, "-f", shellQuote(file))
	}
	return strings.Join(args, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
