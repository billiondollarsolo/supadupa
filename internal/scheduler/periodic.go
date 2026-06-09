package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// PeriodicRunner runs a scheduled task immediately and then on each tick.
// The runner bounds a task instance to one active pass, so overlapping Run
// calls skip ticks instead of starting duplicate work.
type PeriodicRunner struct {
	name   string
	logger *slog.Logger
	mu     sync.Mutex
}

func NewPeriodicRunner(name string, logger *slog.Logger) *PeriodicRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &PeriodicRunner{name: name, logger: logger}
}

func (r *PeriodicRunner) Run(ctx context.Context, interval time.Duration, runOnce func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		r.RunOnce(ctx, runOnce)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *PeriodicRunner) RunOnce(ctx context.Context, runOnce func(context.Context)) bool {
	if runOnce == nil {
		return true
	}
	if !r.mu.TryLock() {
		r.logger.Debug("scheduled pass skipped because previous pass is still running", "scheduler", r.name)
		return false
	}
	defer r.mu.Unlock()
	runOnce(ctx)
	return true
}

func durationFromEnv(getenv func(string) string, envName string, defaultValue time.Duration, example string) (time.Duration, error) {
	if getenv == nil {
		return defaultValue, nil
	}
	raw := strings.TrimSpace(getenv(envName))
	if raw == "" {
		return defaultValue, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return defaultValue, fmt.Errorf("%s must be a Go duration such as %s: %w", envName, example, err)
	}
	if duration <= 0 {
		return defaultValue, fmt.Errorf("%s must be positive", envName)
	}
	return duration, nil
}
