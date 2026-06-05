package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"supadupa2026/internal/control"
)

type fakeTelemetryCollector struct {
	samples map[string]control.TelemetrySampleInput
	refs    []string
}

func (f *fakeTelemetryCollector) CollectProjectTelemetry(ctx context.Context, ref string) (control.TelemetrySampleInput, error) {
	f.refs = append(f.refs, ref)
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
