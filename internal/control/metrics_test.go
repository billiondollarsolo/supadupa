package control

import (
	"context"
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
	if _, err := store.CreateWALArchive(ctx, WALArchiveInput{ProjectRef: project.Ref, Segment: "0001", Location: "memory://wal", SizeBytes: 1024, Status: "archived"}); err != nil {
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
	sampledAt := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
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
