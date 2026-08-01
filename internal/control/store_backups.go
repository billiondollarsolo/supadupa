package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *MemoryStore) CreateBackup(ctx context.Context, input BackupInput) (Backup, error) {
	if strings.TrimSpace(input.ProjectRef) == "" {
		return Backup{}, fmt.Errorf("project ref is required")
	}
	if strings.TrimSpace(input.Location) == "" {
		return Backup{}, fmt.Errorf("backup location is required")
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = "logical"
	}
	status := input.Status
	if status == "" {
		status = "completed"
	}
	now := time.Now().UTC()
	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = now
	}
	finishedAt := input.FinishedAt
	if finishedAt == nil && input.VerifiedAt != nil {
		verifiedAt := *input.VerifiedAt
		finishedAt = &verifiedAt
	}
	if finishedAt == nil && status == "completed" {
		completedAt := now
		finishedAt = &completedAt
	}
	backup := Backup{
		ID:              newID(),
		ProjectRef:      input.ProjectRef,
		Kind:            kind,
		Location:        input.Location,
		RemoteLocation:  strings.TrimSpace(input.RemoteLocation),
		StorageTargetID: strings.TrimSpace(input.StorageTargetID),
		SizeBytes:       input.SizeBytes,
		ChecksumSHA256:  strings.TrimSpace(input.ChecksumSHA256),
		Status:          status,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
		CreatedAt:       now,
		VerifiedAt:      input.VerifiedAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[input.ProjectRef]; !ok {
		return Backup{}, fmt.Errorf("%w: project %s", ErrNotFound, input.ProjectRef)
	}
	if backup.StorageTargetID != "" {
		if _, ok := s.backupStorageTargets[backup.StorageTargetID]; !ok {
			return Backup{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, backup.StorageTargetID)
		}
	}
	s.backups = append(s.backups, backup)
	return backup, nil
}

func (s *MemoryStore) ListBackups(ctx context.Context, ref string) ([]Backup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	backups := make([]Backup, 0)
	for _, backup := range s.backups {
		if backup.ProjectRef == ref {
			backups = append(backups, backup)
		}
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})
	return backups, nil
}

func (s *MemoryStore) GetBackup(ctx context.Context, ref string, backupID string) (Backup, error) {
	backupID = strings.TrimSpace(backupID)
	if backupID == "" {
		return Backup{}, fmt.Errorf("backup id is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return Backup{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, backup := range s.backups {
		if backup.ProjectRef == ref && backup.ID == backupID {
			return backup, nil
		}
	}
	return Backup{}, fmt.Errorf("%w: backup %s for project %s", ErrNotFound, backupID, ref)
}

func (s *MemoryStore) CreatePlatformBackup(ctx context.Context, input PlatformBackupInput) (PlatformBackup, error) {
	if strings.TrimSpace(input.Location) == "" {
		return PlatformBackup{}, fmt.Errorf("platform backup location is required")
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = "control-plane"
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "completed"
	}
	now := time.Now().UTC()
	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = now
	}
	finishedAt := input.FinishedAt
	if finishedAt == nil && input.VerifiedAt != nil {
		verifiedAt := *input.VerifiedAt
		finishedAt = &verifiedAt
	}
	if finishedAt == nil && status == "completed" {
		completedAt := now
		finishedAt = &completedAt
	}
	backup := PlatformBackup{
		ID:              newID(),
		Kind:            kind,
		Location:        strings.TrimSpace(input.Location),
		RemoteLocation:  strings.TrimSpace(input.RemoteLocation),
		StorageTargetID: strings.TrimSpace(input.StorageTargetID),
		SizeBytes:       input.SizeBytes,
		ChecksumSHA256:  strings.TrimSpace(input.ChecksumSHA256),
		Status:          status,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
		CreatedAt:       now,
		VerifiedAt:      input.VerifiedAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if backup.StorageTargetID != "" {
		if _, ok := s.backupStorageTargets[backup.StorageTargetID]; !ok {
			return PlatformBackup{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, backup.StorageTargetID)
		}
	}
	s.platformBackups = append(s.platformBackups, backup)
	return backup, nil
}

func (s *MemoryStore) ListPlatformBackups(ctx context.Context) ([]PlatformBackup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	backups := append([]PlatformBackup(nil), s.platformBackups...)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})
	return backups, nil
}

func (s *MemoryStore) GetPlatformBackup(ctx context.Context, backupID string) (PlatformBackup, error) {
	backupID = strings.TrimSpace(backupID)
	if backupID == "" {
		return PlatformBackup{}, fmt.Errorf("platform backup id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, backup := range s.platformBackups {
		if backup.ID == backupID {
			return backup, nil
		}
	}
	return PlatformBackup{}, fmt.Errorf("%w: platform backup %s", ErrNotFound, backupID)
}

func (s *MemoryStore) ListBackupStorageTargets(ctx context.Context) ([]BackupStorageTarget, error) {
	s.mu.RLock()
	rawTargets := make([]BackupStorageTarget, 0, len(s.backupStorageTargets))
	for _, target := range s.backupStorageTargets {
		rawTargets = append(rawTargets, target)
	}
	s.mu.RUnlock()

	targets := make([]BackupStorageTarget, 0, len(rawTargets))
	for _, target := range rawTargets {
		targets = append(targets, redactBackupStorageTarget(target))
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Default != targets[j].Default {
			return targets[i].Default
		}
		if targets[i].Name != targets[j].Name {
			return targets[i].Name < targets[j].Name
		}
		return targets[i].ID < targets[j].ID
	})
	return targets, nil
}

func (s *MemoryStore) GetBackupStorageTarget(ctx context.Context, id string) (BackupStorageTarget, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	target, ok := s.backupStorageTargets[id]
	if !ok {
		return BackupStorageTarget{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, id)
	}
	return target, nil
}

func (s *MemoryStore) CreateBackupStorageTarget(ctx context.Context, input BackupStorageTargetInput) (BackupStorageTarget, error) {
	target, err := normalizeBackupStorageTargetInput("", BackupStorageTarget{}, input, true)
	if err != nil {
		return BackupStorageTarget{}, err
	}
	now := time.Now().UTC()
	target.ID = newID()
	target.CreatedAt = now
	target.UpdatedAt = now

	s.mu.Lock()
	if target.Default {
		s.clearDefaultBackupStorageTargetLocked("")
	}
	s.backupStorageTargets[target.ID] = target
	s.mu.Unlock()
	return redactBackupStorageTarget(target), nil
}

func (s *MemoryStore) UpdateBackupStorageTarget(ctx context.Context, id string, input BackupStorageTargetInput) (BackupStorageTarget, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target id is required")
	}
	s.mu.Lock()
	existing, ok := s.backupStorageTargets[id]
	if !ok {
		s.mu.Unlock()
		return BackupStorageTarget{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, id)
	}
	target, err := normalizeBackupStorageTargetInput(id, existing, input, false)
	if err != nil {
		s.mu.Unlock()
		return BackupStorageTarget{}, err
	}
	if target.Default {
		s.clearDefaultBackupStorageTargetLocked(id)
	}
	s.backupStorageTargets[id] = target
	s.mu.Unlock()
	return redactBackupStorageTarget(target), nil
}

func (s *MemoryStore) UpdateBackupStorageTargetTestResult(ctx context.Context, id string, testedAt time.Time, status string, message string) (BackupStorageTarget, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target id is required")
	}
	status = strings.TrimSpace(status)
	if status != "passed" && status != "failed" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target test status is invalid")
	}
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	if testedAt.IsZero() {
		testedAt = time.Now().UTC()
	}
	s.mu.Lock()
	target, ok := s.backupStorageTargets[id]
	if !ok {
		s.mu.Unlock()
		return BackupStorageTarget{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, id)
	}
	target.LastTestedAt = &testedAt
	target.LastTestStatus = status
	target.LastTestError = message
	target.UpdatedAt = time.Now().UTC()
	s.backupStorageTargets[id] = target
	s.mu.Unlock()
	return redactBackupStorageTarget(target), nil
}

func (s *MemoryStore) DeleteBackupStorageTarget(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("backup storage target id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.backupStorageTargets[id]; !ok {
		return fmt.Errorf("%w: backup storage target %s", ErrNotFound, id)
	}
	for ref, policy := range s.policies {
		if policy.StorageTargetID == id {
			policy.StorageTargetID = ""
			policy.UpdatedAt = time.Now().UTC()
			s.policies[ref] = policy
		}
	}
	delete(s.backupStorageTargets, id)
	return nil
}

func (s *MemoryStore) clearDefaultBackupStorageTargetLocked(exceptID string) {
	for id, target := range s.backupStorageTargets {
		if id == exceptID || !target.Default {
			continue
		}
		target.Default = false
		target.UpdatedAt = time.Now().UTC()
		s.backupStorageTargets[id] = target
	}
}

func (s *MemoryStore) GetBackupPolicy(ctx context.Context, ref string) (BackupPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return BackupPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	policy, ok := s.policies[ref]
	if !ok {
		policy = defaultBackupPolicy(ref)
	}
	return policy, nil
}

func (s *MemoryStore) UpdateBackupPolicy(ctx context.Context, ref string, input BackupPolicyInput) (BackupPolicy, error) {
	schedule := strings.TrimSpace(input.Schedule)
	if schedule == "" {
		schedule = "daily"
	}
	if err := validateBackupSchedule(schedule); err != nil {
		return BackupPolicy{}, err
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = "logical"
	}
	if kind != "logical" && kind != "physical" {
		return BackupPolicy{}, fmt.Errorf("unsupported backup kind %q", kind)
	}
	targetID := strings.TrimSpace(input.StorageTargetID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return BackupPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if targetID != "" {
		if _, ok := s.backupStorageTargets[targetID]; !ok {
			return BackupPolicy{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, targetID)
		}
	}
	now := time.Now().UTC()
	policy := s.policies[ref]
	policy.ProjectRef = ref
	policy.Enabled = input.Enabled
	policy.Schedule = schedule
	policy.Kind = kind
	policy.StorageTargetID = targetID
	policy.UpdatedAt = now
	if input.Enabled {
		next := nextBackupRun(now, schedule)
		policy.NextRunAt = &next
	} else {
		policy.NextRunAt = nil
	}
	s.policies[ref] = policy
	return policy, nil
}

func (s *MemoryStore) MarkBackupPolicyRun(ctx context.Context, ref string, runAt time.Time) (BackupPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return BackupPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	policy := s.policies[ref]
	if policy.ProjectRef == "" {
		policy = defaultBackupPolicy(ref)
	}
	runAt = runAt.UTC()
	policy.LastRunAt = &runAt
	next := nextBackupRun(runAt, policy.Schedule)
	policy.NextRunAt = &next
	policy.UpdatedAt = time.Now().UTC()
	s.policies[ref] = policy
	return policy, nil
}

func (s *MemoryStore) GetPITRPolicy(ctx context.Context, ref string) (PITRPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return PITRPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	policy, ok := s.pitrPolicies[ref]
	if !ok {
		policy = defaultPITRPolicy(ref)
	}
	return policy, nil
}

func (s *MemoryStore) UpdatePITRPolicy(ctx context.Context, ref string, input PITRPolicyInput) (PITRPolicy, error) {
	bucket := strings.TrimSpace(input.ArchiveBucket)
	retentionDays := input.RetentionDays
	if retentionDays == 0 {
		retentionDays = 7
	}
	if retentionDays < 1 || retentionDays > 35 {
		return PITRPolicy{}, fmt.Errorf("retention_days must be between 1 and 35")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return PITRPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if input.Enabled && bucket == "" {
		var ok bool
		bucket, ok = s.defaultWALArchiveBucketLocked(ref)
		if !ok {
			return PITRPolicy{}, fmt.Errorf("archive_bucket is required when PITR is enabled and no backup storage target is selected or configured as platform default")
		}
	}
	now := time.Now().UTC()
	policy := s.pitrPolicies[ref]
	policy.ProjectRef = ref
	policy.Enabled = input.Enabled
	policy.ArchiveBucket = bucket
	policy.RetentionDays = retentionDays
	policy.UpdatedAt = now
	s.pitrPolicies[ref] = policy
	return policy, nil
}

func (s *MemoryStore) defaultWALArchiveBucketLocked(ref string) (string, bool) {
	targetID := strings.TrimSpace(s.policies[ref].StorageTargetID)
	for _, target := range s.backupStorageTargets {
		if targetID != "" {
			if target.ID != targetID {
				continue
			}
		} else if !target.Default {
			continue
		}
		if !target.SecretConfigured {
			continue
		}
		if requireRecoveryReadyBackupTargets() && !backupStorageTargetIsReadyOffHost(target) {
			continue
		}
		return walArchiveBucketForTarget(target, ref), true
	}
	return "", false
}

func (s *MemoryStore) CreateWALArchive(ctx context.Context, input WALArchiveInput) (WALArchive, error) {
	if strings.TrimSpace(input.ProjectRef) == "" {
		return WALArchive{}, fmt.Errorf("project ref is required")
	}
	if strings.TrimSpace(input.Segment) == "" {
		return WALArchive{}, fmt.Errorf("wal segment is required")
	}
	if !isPostgresWALSegment(input.Segment) {
		return WALArchive{}, fmt.Errorf("wal segment must be a 24-character Postgres WAL filename")
	}
	if strings.TrimSpace(input.Location) == "" {
		return WALArchive{}, fmt.Errorf("wal archive location is required")
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "archived"
	}
	segmentSource := strings.TrimSpace(input.SegmentSource)
	if segmentSource == "" {
		segmentSource = "unknown"
	}
	archive := WALArchive{
		ID:              newID(),
		ProjectRef:      input.ProjectRef,
		Segment:         strings.ToUpper(strings.TrimSpace(input.Segment)),
		SegmentSource:   segmentSource,
		Location:        input.Location,
		RemoteLocation:  strings.TrimSpace(input.RemoteLocation),
		StorageTargetID: strings.TrimSpace(input.StorageTargetID),
		SizeBytes:       input.SizeBytes,
		ChecksumSHA256:  strings.TrimSpace(input.ChecksumSHA256),
		Status:          status,
		CreatedAt:       time.Now().UTC(),
		VerifiedAt:      input.VerifiedAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[input.ProjectRef]; !ok {
		return WALArchive{}, fmt.Errorf("%w: project %s", ErrNotFound, input.ProjectRef)
	}
	if archive.StorageTargetID != "" {
		if _, ok := s.backupStorageTargets[archive.StorageTargetID]; !ok {
			return WALArchive{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, archive.StorageTargetID)
		}
	}
	s.walArchives = append(s.walArchives, archive)
	policy := s.pitrPolicies[input.ProjectRef]
	if policy.ProjectRef == "" {
		policy = defaultPITRPolicy(input.ProjectRef)
	}
	archivedAt := archive.CreatedAt
	policy.LastArchiveAt = &archivedAt
	policy.UpdatedAt = time.Now().UTC()
	s.pitrPolicies[input.ProjectRef] = policy
	return archive, nil
}

func (s *MemoryStore) ListWALArchives(ctx context.Context, ref string) ([]WALArchive, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	archives := make([]WALArchive, 0)
	for _, archive := range s.walArchives {
		if archive.ProjectRef == ref {
			archives = append(archives, archive)
		}
	}
	sort.Slice(archives, func(i, j int) bool {
		return archives[i].CreatedAt.After(archives[j].CreatedAt)
	})
	return archives, nil
}
