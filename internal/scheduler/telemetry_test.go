package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"supadupa2026/internal/control"
)

type fakeTelemetryCollector struct {
	mu      sync.Mutex
	samples map[string]control.TelemetrySampleInput
	refs    []string
}

func (f *fakeTelemetryCollector) CollectProjectTelemetry(ctx context.Context, ref string) (control.TelemetrySampleInput, error) {
	f.mu.Lock()
	f.refs = append(f.refs, ref)
	f.mu.Unlock()
	return f.samples[ref], nil
}

func TestTelemetrySchedulerTickFromEnvDefaults(t *testing.T) {
	tick, err := TelemetrySchedulerTickFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tick != DefaultTelemetrySchedulerTick {
		t.Fatalf("tick = %s, want %s", tick, DefaultTelemetrySchedulerTick)
	}
}

func TestTelemetrySchedulerTickFromEnvParsesDuration(t *testing.T) {
	tick, err := TelemetrySchedulerTickFromEnv(func(key string) string {
		if key != TelemetrySchedulerTickEnv {
			t.Fatalf("key = %q, want %q", key, TelemetrySchedulerTickEnv)
		}
		return "45s"
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tick != 45*time.Second {
		t.Fatalf("tick = %s, want 45s", tick)
	}
}

func TestTelemetrySchedulerTickFromEnvRejectsInvalidDuration(t *testing.T) {
	tick, err := TelemetrySchedulerTickFromEnv(func(string) string { return "often" })
	if err == nil {
		t.Fatal("expected error")
	}
	if tick != DefaultTelemetrySchedulerTick {
		t.Fatalf("tick = %s, want %s", tick, DefaultTelemetrySchedulerTick)
	}
}

func TestTelemetrySchedulerRecordsEligibleProjectSamples(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := store.CreateProject(ctx, control.CreateProjectRequest{OrgID: org.ID, Ref: "healthy-stack", Name: "Healthy Stack"})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := store.CreateProject(ctx, control.CreateProjectRequest{OrgID: org.ID, Ref: "paused-stack", Name: "Paused Stack"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProjectStatus(ctx, healthy.Ref, control.ProjectHealthy, "ready"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProjectStatus(ctx, paused.Ref, control.ProjectPaused, "paused"); err != nil {
		t.Fatal(err)
	}

	sampledAt := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	collector := &fakeTelemetryCollector{samples: map[string]control.TelemetrySampleInput{
		healthy.Ref: {
			Source:           "compose",
			CPUPercent:       8.5,
			MemoryBytes:      256 * 1024 * 1024,
			MemoryLimitBytes: 1024 * 1024 * 1024,
			NetworkRxBytes:   100,
			NetworkTxBytes:   200,
			SampledAt:        sampledAt,
		},
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	NewTelemetryScheduler(store, collector, logger).runOnce(ctx)

	if len(collector.refs) != 1 || collector.refs[0] != healthy.Ref {
		t.Fatalf("collected refs = %#v, want only %q", collector.refs, healthy.Ref)
	}
	metrics, err := store.GetProjectMetrics(ctx, healthy.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Observed == nil || metrics.Observed.CPUPercent != 8.5 || metrics.Observed.SampledAt != sampledAt {
		t.Fatalf("unexpected observed metrics %#v", metrics.Observed)
	}
	pausedMetrics, err := store.GetProjectMetrics(ctx, paused.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if pausedMetrics.Observed != nil {
		t.Fatalf("paused project should not receive telemetry, got %#v", pausedMetrics.Observed)
	}
}

func TestTelemetrySchedulerCollectsEligibleProjectsConcurrently(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{"alpha-one", "alpha-two", "alpha-three", "alpha-four"}
	for _, ref := range refs {
		project, err := store.CreateProject(ctx, control.CreateProjectRequest{OrgID: org.ID, Ref: ref, Name: ref})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpdateProjectStatus(ctx, project.Ref, control.ProjectHealthy, "ready"); err != nil {
			t.Fatal(err)
		}
	}

	collector := newBlockingTelemetryCollector(len(refs))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	scheduler := NewTelemetryScheduler(store, collector, logger).WithMaxConcurrency(len(refs))

	done := make(chan struct{})
	go func() {
		scheduler.runOnce(ctx)
		close(done)
	}()

	for i := 0; i < len(refs); i++ {
		select {
		case <-collector.started:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for concurrent collector %d to start; max active=%d", i+1, collector.MaxActive())
		}
	}
	if maxActive := collector.MaxActive(); maxActive < 2 {
		t.Fatalf("expected telemetry collection to overlap, max active collectors=%d", maxActive)
	}
	close(collector.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for telemetry pass to finish")
	}
	for _, ref := range refs {
		metrics, err := store.GetProjectMetrics(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		if metrics.Observed == nil || metrics.Observed.Source != "blocking-test" {
			t.Fatalf("expected observed telemetry for %s, got %#v", ref, metrics.Observed)
		}
	}
}

type blockingTelemetryCollector struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	active  int
	max     int
}

func newBlockingTelemetryCollector(expected int) *blockingTelemetryCollector {
	return &blockingTelemetryCollector{
		started: make(chan struct{}, expected),
		release: make(chan struct{}),
	}
}

func (c *blockingTelemetryCollector) CollectProjectTelemetry(ctx context.Context, ref string) (control.TelemetrySampleInput, error) {
	c.mu.Lock()
	c.active++
	if c.active > c.max {
		c.max = c.active
	}
	c.mu.Unlock()
	c.started <- struct{}{}
	select {
	case <-ctx.Done():
		return control.TelemetrySampleInput{}, ctx.Err()
	case <-c.release:
	}
	c.mu.Lock()
	c.active--
	c.mu.Unlock()
	return control.TelemetrySampleInput{Source: "blocking-test", SampledAt: time.Now().UTC()}, nil
}

func (c *blockingTelemetryCollector) MaxActive() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.max
}
