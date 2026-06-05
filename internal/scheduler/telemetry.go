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
	TelemetrySchedulerTickEnv     = "SUPADUPA_TELEMETRY_SCHEDULER_TICK"
	DefaultTelemetrySchedulerTick = 15 * time.Second
)

type TelemetryScheduler struct {
	store     control.Store
	collector control.TelemetryCollector
	logger    *slog.Logger
	tick      time.Duration
}

func NewTelemetryScheduler(store control.Store, collector control.TelemetryCollector, logger *slog.Logger) *TelemetryScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TelemetryScheduler{
		store:     store,
		collector: collector,
		logger:    logger,
		tick:      DefaultTelemetrySchedulerTick,
	}
}

func TelemetrySchedulerTickFromEnv(getenv func(string) string) (time.Duration, error) {
	if getenv == nil {
		return DefaultTelemetrySchedulerTick, nil
	}
	raw := strings.TrimSpace(getenv(TelemetrySchedulerTickEnv))
	if raw == "" {
		return DefaultTelemetrySchedulerTick, nil
	}
	tick, err := time.ParseDuration(raw)
	if err != nil {
		return DefaultTelemetrySchedulerTick, fmt.Errorf("%s must be a Go duration such as 15s or 1m: %w", TelemetrySchedulerTickEnv, err)
	}
	if tick <= 0 {
		return DefaultTelemetrySchedulerTick, fmt.Errorf("%s must be positive", TelemetrySchedulerTickEnv)
	}
	return tick, nil
}

func (s *TelemetryScheduler) WithTick(tick time.Duration) *TelemetryScheduler {
	if tick > 0 {
		s.tick = tick
	}
	return s
}

func (s *TelemetryScheduler) Run(ctx context.Context) {
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

func (s *TelemetryScheduler) runOnce(ctx context.Context) {
	if s.store == nil || s.collector == nil {
		return
	}
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		s.logger.Warn("telemetry project list failed", "error", err)
		return
	}
	collected := 0
	for _, project := range projects {
		if !telemetryEligible(project.Status) {
			continue
		}
		sample, err := s.collector.CollectProjectTelemetry(ctx, project.Ref)
		if err != nil {
			s.logger.Debug("project telemetry collection failed", "project_ref", project.Ref, "error", err)
			continue
		}
		if _, err := s.store.RecordProjectTelemetry(ctx, project.Ref, sample); err != nil {
			s.logger.Warn("project telemetry record failed", "project_ref", project.Ref, "error", err)
			continue
		}
		collected++
	}
	if collected > 0 {
		s.logger.Debug("project telemetry collected", "count", collected)
	}
}

func telemetryEligible(status control.ProjectPhase) bool {
	switch status {
	case control.ProjectDestroying, control.ProjectPaused, control.ProjectError:
		return false
	default:
		return true
	}
}
