package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

const (
	BackupSchedulerTickEnv     = "SUPADUPA_BACKUP_SCHEDULER_TICK"
	WALArchiveIntervalEnv      = "SUPADUPA_WAL_ARCHIVE_INTERVAL"
	DefaultBackupSchedulerTick = time.Minute
	DefaultWALArchiveInterval  = 5 * time.Minute
)

type BackupScheduler struct {
	store              control.Store
	service            *control.BackupService
	logger             *slog.Logger
	tick               time.Duration
	walArchiveInterval time.Duration
}

func NewBackupScheduler(store control.Store, service *control.BackupService, logger *slog.Logger) *BackupScheduler {
	if service == nil {
		service = control.NewBackupService("")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &BackupScheduler{
		store:              store,
		service:            service,
		logger:             logger,
		tick:               DefaultBackupSchedulerTick,
		walArchiveInterval: DefaultWALArchiveInterval,
	}
}

func BackupSchedulerTickFromEnv(getenv func(string) string) (time.Duration, error) {
	if getenv == nil {
		return DefaultBackupSchedulerTick, nil
	}
	raw := strings.TrimSpace(getenv(BackupSchedulerTickEnv))
	if raw == "" {
		return DefaultBackupSchedulerTick, nil
	}
	tick, err := time.ParseDuration(raw)
	if err != nil {
		return DefaultBackupSchedulerTick, fmt.Errorf("%s must be a Go duration such as 30s or 5m: %w", BackupSchedulerTickEnv, err)
	}
	if tick <= 0 {
		return DefaultBackupSchedulerTick, fmt.Errorf("%s must be positive", BackupSchedulerTickEnv)
	}
	return tick, nil
}

func WALArchiveIntervalFromEnv(getenv func(string) string) (time.Duration, error) {
	if getenv == nil {
		return DefaultWALArchiveInterval, nil
	}
	raw := strings.TrimSpace(getenv(WALArchiveIntervalEnv))
	if raw == "" {
		return DefaultWALArchiveInterval, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil {
		return DefaultWALArchiveInterval, fmt.Errorf("%s must be a Go duration such as 5m or 15m: %w", WALArchiveIntervalEnv, err)
	}
	if interval <= 0 {
		return DefaultWALArchiveInterval, fmt.Errorf("%s must be positive", WALArchiveIntervalEnv)
	}
	return interval, nil
}

func (s *BackupScheduler) WithTick(tick time.Duration) *BackupScheduler {
	if tick > 0 {
		s.tick = tick
	}
	return s
}

func (s *BackupScheduler) WithWALArchiveInterval(interval time.Duration) *BackupScheduler {
	if interval > 0 {
		s.walArchiveInterval = interval
	}
	return s
}

func (s *BackupScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		s.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *BackupScheduler) runOnce(ctx context.Context) {
	if s.store == nil {
		return
	}
	backups, err := s.service.RunDueBackups(ctx, s.store, time.Now().UTC())
	if err != nil {
		s.logger.Warn("scheduled backup pass failed", "error", err)
		return
	}
	if len(backups) > 0 {
		s.logger.Info("scheduled backups completed", "count", len(backups))
	}
	archives, err := s.service.RunDueWALArchives(ctx, s.store, time.Now().UTC(), s.walArchiveInterval)
	if err != nil {
		s.logger.Warn("scheduled WAL archive pass failed", "error", err)
		return
	}
	if len(archives) > 0 {
		s.logger.Info("scheduled WAL archives completed", "count", len(archives))
	}
}
