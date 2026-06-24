package control

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProjectMetricsTracksProjectScopedCounters(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "metrics-one", Name: "Metrics One"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertProjectRoutes(ctx, project.Ref, []ProjectRoute{{Name: "api", FQDN: "metrics-one.supadupa.test", UpstreamURL: "http://metrics-one-kong:8000", TLS: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddProjectDomain(ctx, project.Ref, ProjectDomainInput{FQDN: "metrics.example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeployProjectFunction(ctx, project.Ref, ProjectFunctionInput{Name: "hello", Source: "Deno.serve(() => new Response('ok'))"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateBackup(ctx, BackupInput{ProjectRef: project.Ref, Kind: "logical", Location: "memory://backup", SizeBytes: 2048, Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWALArchive(ctx, WALArchiveInput{ProjectRef: project.Ref, Segment: "000000010000000000000001", SegmentSource: "postgres", Location: "memory://wal", SizeBytes: 1024, Status: "archived"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordProjectLog(ctx, ProjectLogInput{ProjectRef: project.Ref, Level: "info", Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	Audit(ctx, store, "project.metrics_test", "project:"+project.Ref, nil)

	metrics, err := store.GetProjectMetrics(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.ProjectRef != project.Ref || metrics.Routes != 1 || metrics.CustomDomains != 1 || metrics.FunctionDeployments != 1 || metrics.Backups != 1 || metrics.WALArchives != 1 || metrics.ProjectLogEvents != 1 || metrics.ActivityEvents != 1 {
		t.Fatalf("unexpected project metrics %#v", metrics)
	}
	if metrics.BackupStorageBytes != 2048 || metrics.WALArchiveBytes != 1024 || metrics.StorageBytes != 3072 {
		t.Fatalf("unexpected storage metrics %#v", metrics)
	}
}

func TestProjectMetricsIncludesObservedTelemetry(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "metrics-observed", Name: "Metrics Observed"})
	if err != nil {
		t.Fatal(err)
	}
	sampledAt := time.Now().UTC().Add(-1 * time.Minute)
	if _, err := store.RecordProjectTelemetry(ctx, project.Ref, TelemetrySampleInput{
		Source:           "compose",
		CPUPercent:       12.5,
		MemoryBytes:      512 * 1024 * 1024,
		MemoryLimitBytes: 2 * 1024 * 1024 * 1024,
		DiskUsedBytes:    7 * 1024 * 1024 * 1024,
		DiskLimitBytes:   20 * 1024 * 1024 * 1024,
		NetworkRxBytes:   42,
		NetworkTxBytes:   84,
		SampledAt:        sampledAt,
	}); err != nil {
		t.Fatal(err)
	}

	metrics, err := store.GetProjectMetrics(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Observed == nil {
		t.Fatalf("expected observed telemetry")
	}
	if metrics.Observed.Source != "compose" || metrics.Observed.CPUPercent != 12.5 || metrics.Observed.SampledAt != sampledAt {
		t.Fatalf("unexpected observed telemetry %#v", metrics.Observed)
	}

	fleet, err := store.GetFleetMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fleet.Observed.ProjectsSampled != 1 || fleet.Observed.CPUPercent != 12.5 || fleet.Observed.MemoryBytes != 512*1024*1024 {
		t.Fatalf("unexpected fleet observed rollup %#v", fleet.Observed)
	}
}

func TestProjectTelemetryHistoryTracksReservationRelativeUsage(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "history-observed", Name: "History Observed", CPU: 4, RAMMB: 4096, DiskGB: 50})
	if err != nil {
		t.Fatal(err)
	}
	sampledAt := time.Now().UTC().Add(-1 * time.Minute)
	if _, err := store.RecordProjectTelemetry(ctx, project.Ref, TelemetrySampleInput{
		Source:           "compose",
		CPUPercent:       200,
		MemoryBytes:      1024 * 1024 * 1024,
		MemoryLimitBytes: 2 * 1024 * 1024 * 1024,
		DiskUsedBytes:    5 * 1024 * 1024 * 1024,
		DiskLimitBytes:   50 * 1024 * 1024 * 1024,
		SampledAt:        sampledAt,
	}); err != nil {
		t.Fatal(err)
	}

	history, err := store.GetProjectTelemetryHistory(ctx, project.Ref, TelemetryHistoryQuery{
		From: sampledAt.Add(-time.Minute),
		To:   sampledAt.Add(time.Minute),
		Step: 15 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Points) != 1 {
		t.Fatalf("expected one history point, got %#v", history.Points)
	}
	point := history.Points[0]
	if point.ReservedCPU != 4 || point.ReservedRAMMB != 4096 || point.ReservedDiskGB != 50 {
		t.Fatalf("expected reservation snapshot, got %#v", point)
	}
	if point.CPUReservationPercent != 50 || point.MemoryReservationPercent != 25 || point.DiskReservationPercent != 10 {
		t.Fatalf("unexpected reservation-relative percentages %#v", point)
	}
}

func TestProjectTelemetryHistoryCompactsOlderRawSamples(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "history-compact", Name: "History Compact", CPU: 4, RAMMB: 4096, DiskGB: 50})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Hour)
	for _, sample := range []struct {
		at  time.Time
		cpu float64
	}{
		{at: now.Add(-25 * time.Hour), cpu: 100},
		{at: now.Add(-25*time.Hour + time.Minute), cpu: 300},
		{at: now, cpu: 25},
	} {
		if _, err := store.RecordProjectTelemetry(ctx, project.Ref, TelemetrySampleInput{
			Source:      "compose",
			CPUPercent:  sample.cpu,
			MemoryBytes: 1024 * 1024 * 1024,
			SampledAt:   sample.at,
		}); err != nil {
			t.Fatal(err)
		}
	}

	history, err := store.GetProjectTelemetryHistory(ctx, project.Ref, TelemetryHistoryQuery{
		From: now.Add(-26 * time.Hour),
		To:   now.Add(-24 * time.Hour),
		Step: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Points) != 1 {
		t.Fatalf("expected one compacted point, got %#v", history.Points)
	}
	point := history.Points[0]
	if point.Samples != 2 {
		t.Fatalf("expected compacted point to represent two samples, got %#v", point)
	}
	if point.CPUPercent != 200 || point.CPUReservationPercent != 50 {
		t.Fatalf("unexpected compacted CPU values %#v", point)
	}
}

func TestProjectTelemetryHistoryUsesWallClockRetentionAndRejectsFutureSkew(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "history-retention", Name: "History Retention", CPU: 4, RAMMB: 4096, DiskGB: 50})
	if err != nil {
		t.Fatal(err)
	}

	oldAt := time.Now().UTC().Add(-telemetryHistoryRetention - time.Hour)
	if _, err := store.RecordProjectTelemetry(ctx, project.Ref, TelemetrySampleInput{
		Source:      "compose",
		CPUPercent:  100,
		MemoryBytes: 1024,
		SampledAt:   oldAt,
	}); err != nil {
		t.Fatal(err)
	}
	oldHistory, err := store.GetProjectTelemetryHistory(ctx, project.Ref, TelemetryHistoryQuery{
		From: oldAt.Add(-time.Minute),
		To:   oldAt.Add(time.Minute),
		Step: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(oldHistory.Points) != 0 {
		t.Fatalf("expected old backfilled sample to be outside retained history, got %#v", oldHistory.Points)
	}

	recentAt := time.Now().UTC().Add(-time.Minute)
	if _, err := store.RecordProjectTelemetry(ctx, project.Ref, TelemetrySampleInput{
		Source:      "compose",
		CPUPercent:  200,
		MemoryBytes: 2048,
		SampledAt:   recentAt,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordProjectTelemetry(ctx, project.Ref, TelemetrySampleInput{
		Source:      "compose",
		CPUPercent:  300,
		MemoryBytes: 4096,
		SampledAt:   time.Now().UTC().Add(telemetryMaxFutureSkew + time.Minute),
	}); err == nil || !strings.Contains(err.Error(), "sampled_at cannot be more than") {
		t.Fatalf("expected future skew rejection, got %v", err)
	}
	history, err := store.GetProjectTelemetryHistory(ctx, project.Ref, TelemetryHistoryQuery{
		From: recentAt.Add(-time.Minute),
		To:   recentAt.Add(time.Minute),
		Step: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Points) != 1 || history.Points[0].CPUPercent != 200 {
		t.Fatalf("expected retained recent sample only, got %#v", history.Points)
	}
}

func TestFleetMetricsExcludesStaleAndDeletedProjectTelemetry(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "fresh-metrics", Name: "Fresh Metrics"})
	if err != nil {
		t.Fatal(err)
	}
	stale, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "stale-metrics", Name: "Stale Metrics"})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "deleted-metrics", Name: "Deleted Metrics"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordProjectTelemetry(ctx, fresh.Ref, TelemetrySampleInput{
		Source:           "compose",
		CPUPercent:       10,
		MemoryBytes:      128,
		MemoryLimitBytes: 1024,
		SampledAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordProjectTelemetry(ctx, stale.Ref, TelemetrySampleInput{
		Source:           "compose",
		CPUPercent:       90,
		MemoryBytes:      512,
		MemoryLimitBytes: 1024,
		SampledAt:        time.Now().UTC().Add(-10 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordProjectTelemetry(ctx, deleted.Ref, TelemetrySampleInput{
		Source:           "compose",
		CPUPercent:       80,
		MemoryBytes:      256,
		MemoryLimitBytes: 1024,
		SampledAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProject(ctx, deleted.Ref); err != nil {
		t.Fatal(err)
	}

	fleet, err := store.GetFleetMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fleet.Observed.ProjectsSampled != 1 || fleet.Observed.StaleProjects != 1 {
		t.Fatalf("expected one fresh and one stale active telemetry sample, got %#v", fleet.Observed)
	}
	if fleet.Observed.CPUPercent != 10 || fleet.Observed.MemoryBytes != 128 {
		t.Fatalf("expected stale/deleted telemetry excluded from totals, got %#v", fleet.Observed)
	}
}

func TestFleetMetricsIncludesNodeTelemetrySeparatelyFromProjectTelemetry(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	host, err := store.CreateHost(ctx, CreateHostRequest{
		Name:     "local-docker",
		Address:  "localhost",
		Capacity: HostCapacity{CPU: 16, RAMMB: 32768, DiskGB: 600, Project: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	sampledAt := time.Now().UTC().Add(-30 * time.Second)
	if _, err := store.RecordNodeTelemetry(ctx, host.ID, NodeTelemetrySampleInput{
		Source:             "compose-local-node",
		CPUPercent:         12.5,
		CPUUsedCores:       2,
		CPUCapacityCores:   16,
		MemoryUsedBytes:    4 * 1024 * 1024 * 1024,
		MemoryTotalBytes:   32 * 1024 * 1024 * 1024,
		DiskUsedBytes:      80 * 1024 * 1024 * 1024,
		DiskTotalBytes:     600 * 1024 * 1024 * 1024,
		DiskAvailableBytes: 520 * 1024 * 1024 * 1024,
		NetworkSampled:     true,
		NetworkRxBytes:     1234,
		NetworkTxBytes:     5678,
		SampledAt:          sampledAt,
	}); err != nil {
		t.Fatal(err)
	}

	metrics, err := store.GetFleetMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.NodeObserved) != 1 {
		t.Fatalf("expected one node sample, got %#v", metrics.NodeObserved)
	}
	node := metrics.NodeObserved[0]
	if node.HostID != host.ID || node.Source != "compose-local-node" || node.CPUPercent != 12.5 || node.SampledAt != sampledAt {
		t.Fatalf("unexpected node telemetry %#v", node)
	}
	if !node.NetworkSampled || node.NetworkRxBytes != 1234 || node.NetworkTxBytes != 5678 {
		t.Fatalf("unexpected node network telemetry %#v", node)
	}
	if metrics.Observed.ProjectsSampled != 0 || metrics.Observed.CPUPercent != 0 {
		t.Fatalf("node telemetry should not change project telemetry rollup: %#v", metrics.Observed)
	}
}

func TestCreateProjectDefaultsToAvailableHostCapacity(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	host, err := store.CreateHost(ctx, CreateHostRequest{
		Name:     "local-docker",
		Address:  "127.0.0.1",
		Capacity: HostCapacity{CPU: 16, RAMMB: 32768, DiskGB: 600, Project: 20},
	})
	if err != nil {
		t.Fatal(err)
	}

	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "auto-hosted", Name: "Auto Hosted", ResourceTier: ResourceTierSmall})
	if err != nil {
		t.Fatal(err)
	}
	if project.Spec.HostID != host.ID {
		t.Fatalf("expected project host_id %q, got %q", host.ID, project.Spec.HostID)
	}

	metrics, err := store.GetFleetMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.HostUsed.Project != 1 || metrics.HostUsed.CPU != 2 || metrics.HostUsed.RAMMB != 4096 || metrics.HostUsed.DiskGB != 40 {
		t.Fatalf("expected small tier reservation in host usage, got %#v", metrics.HostUsed)
	}
}
