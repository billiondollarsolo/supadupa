package scheduler

import (
	"context"
	"log/slog"
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
	runner             *PeriodicRunner
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
		runner:             NewPeriodicRunner("backup", logger),
		tick:               DefaultBackupSchedulerTick,
		walArchiveInterval: DefaultWALArchiveInterval,
	}
}

func BackupSchedulerTickFromEnv(getenv func(string) string) (time.Duration, error) {
	return durationFromEnv(getenv, BackupSchedulerTickEnv, DefaultBackupSchedulerTick, "30s or 5m")
}

func WALArchiveIntervalFromEnv(getenv func(string) string) (time.Duration, error) {
	return durationFromEnv(getenv, WALArchiveIntervalEnv, DefaultWALArchiveInterval, "5m or 15m")
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
	if s.runner == nil {
		s.runner = NewPeriodicRunner("backup", s.logger)
	}
	s.runner.Run(ctx, s.tick, s.runOnce)
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
