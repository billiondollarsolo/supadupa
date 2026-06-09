package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"supadupa2026/internal/control"
)

const (
	TelemetrySchedulerTickEnv     = "SUPADUPA_TELEMETRY_SCHEDULER_TICK"
	DefaultTelemetrySchedulerTick = 15 * time.Second
	DefaultTelemetryConcurrency   = 4
)

type TelemetryScheduler struct {
	store          control.Store
	collector      control.TelemetryCollector
	nodeCollector  control.NodeTelemetryCollector
	logger         *slog.Logger
	runner         *PeriodicRunner
	tick           time.Duration
	maxConcurrency int
}

func NewTelemetryScheduler(store control.Store, collector control.TelemetryCollector, logger *slog.Logger) *TelemetryScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TelemetryScheduler{
		store:          store,
		collector:      collector,
		logger:         logger,
		runner:         NewPeriodicRunner("telemetry", logger),
		tick:           DefaultTelemetrySchedulerTick,
		maxConcurrency: DefaultTelemetryConcurrency,
	}
}

func TelemetrySchedulerTickFromEnv(getenv func(string) string) (time.Duration, error) {
	return durationFromEnv(getenv, TelemetrySchedulerTickEnv, DefaultTelemetrySchedulerTick, "15s or 1m")
}

func (s *TelemetryScheduler) WithTick(tick time.Duration) *TelemetryScheduler {
	if tick > 0 {
		s.tick = tick
	}
	return s
}

func (s *TelemetryScheduler) WithMaxConcurrency(maxConcurrency int) *TelemetryScheduler {
	if maxConcurrency > 0 {
		s.maxConcurrency = maxConcurrency
	}
	return s
}

func (s *TelemetryScheduler) WithNodeCollector(collector control.NodeTelemetryCollector) *TelemetryScheduler {
	s.nodeCollector = collector
	return s
}

func (s *TelemetryScheduler) Run(ctx context.Context) {
	if s.runner == nil {
		s.runner = NewPeriodicRunner("telemetry", s.logger)
	}
	s.runner.Run(ctx, s.tick, s.runOnce)
}

func (s *TelemetryScheduler) runOnce(ctx context.Context) {
	if s.store == nil {
		return
	}
	if s.nodeCollector != nil {
		s.collectLocalNode(ctx)
	}
	if s.collector == nil {
		return
	}
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		s.logger.Warn("telemetry project list failed", "error", err)
		return
	}
	eligible := make([]control.Project, 0, len(projects))
	for _, project := range projects {
		if !telemetryEligible(project.Status) {
			continue
		}
		eligible = append(eligible, project)
	}
	collected := s.collectEligibleProjects(ctx, eligible)
	if collected > 0 {
		s.logger.Debug("project telemetry collected", "count", collected)
	}
}

func (s *TelemetryScheduler) collectLocalNode(ctx context.Context) {
	hosts, err := s.store.ListHosts(ctx)
	if err != nil {
		s.logger.Warn("node telemetry host list failed", "error", err)
		return
	}
	if len(hosts) == 0 {
		return
	}
	host := hosts[0]
	sample, err := s.nodeCollector.CollectNodeTelemetry(ctx, host)
	if err != nil {
		s.logger.Debug("node telemetry collection failed", "host_id", host.ID, "error", err)
		return
	}
	if _, err := s.store.RecordNodeTelemetry(ctx, host.ID, sample); err != nil {
		s.logger.Warn("node telemetry record failed", "host_id", host.ID, "error", err)
	}
}

func (s *TelemetryScheduler) collectEligibleProjects(ctx context.Context, projects []control.Project) int {
	if len(projects) == 0 {
		return 0
	}
	workers := s.maxConcurrency
	if workers <= 0 {
		workers = DefaultTelemetryConcurrency
	}
	if workers > len(projects) {
		workers = len(projects)
	}
	jobs := make(chan control.Project)
	var wg sync.WaitGroup
	var mu sync.Mutex
	collected := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for project := range jobs {
				if s.collectProject(ctx, project) {
					mu.Lock()
					collected++
					mu.Unlock()
				}
			}
		}()
	}
	for _, project := range projects {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return collected
		case jobs <- project:
		}
	}
	close(jobs)
	wg.Wait()
	return collected
}

func (s *TelemetryScheduler) collectProject(ctx context.Context, project control.Project) bool {
	sample, err := s.collector.CollectProjectTelemetry(ctx, project.Ref)
	if err != nil {
		s.logger.Debug("project telemetry collection failed", "project_ref", project.Ref, "error", err)
		return false
	}
	if _, err := s.store.RecordProjectTelemetry(ctx, project.Ref, sample); err != nil {
		s.logger.Warn("project telemetry record failed", "project_ref", project.Ref, "error", err)
		return false
	}
	return true
}

func telemetryEligible(status control.ProjectPhase) bool {
	switch status {
	case control.ProjectDestroying, control.ProjectPaused, control.ProjectError:
		return false
	default:
		return true
	}
}
