package reconciler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"supadupa2026/internal/control"
)

func TestReconcileUpdatesProjectStatus(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = New(store, fakeProvisioner{status: control.ProjectStatus{
		Ref:     project.Ref,
		Phase:   control.ProjectHealthy,
		Message: "runtime healthy",
	}}, nil).Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetProject(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != control.ProjectHealthy {
		t.Fatalf("expected %s, got %s", control.ProjectHealthy, updated.Status)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "project.reconciled" {
		t.Fatalf("expected reconciled audit event, got %#v", events)
	}
	logs, err := store.ListProjectLogs(ctx, project.Ref, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Message != "Project reconciled" || logs[0].Metadata["to"] != string(control.ProjectHealthy) {
		t.Fatalf("expected project reconcile log, got %#v", logs)
	}
}

func TestReconcileMarksDegradedDrift(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpdateProjectStatus(ctx, project.Ref, control.ProjectHealthy, "project provisioned")
	if err != nil {
		t.Fatal(err)
	}

	err = New(store, fakeProvisioner{status: control.ProjectStatus{
		Ref:     project.Ref,
		Phase:   control.ProjectDegraded,
		Message: "compose render drift: missing vector.yml",
	}}, nil).Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetProject(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != control.ProjectDegraded || !strings.Contains(updated.Message, "missing vector.yml") {
		t.Fatalf("expected degraded project with drift message, got %#v", updated)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "project.reconciled" || events[0].Metadata["to"] != string(control.ProjectDegraded) {
		t.Fatalf("expected degraded reconciled audit event, got %#v", events)
	}
	logs, err := store.ListProjectLogs(ctx, project.Ref, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Level != "warning" || !strings.Contains(logs[0].Metadata["message"], "missing vector.yml") {
		t.Fatalf("expected warning drift log, got %#v", logs)
	}
}

func TestReconcileUpdatesSamePhaseMessageDrift(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpdateProjectStatus(ctx, project.Ref, control.ProjectDegraded, "compose render drift: compose missing ./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro")
	if err != nil {
		t.Fatal(err)
	}

	err = New(store, fakeProvisioner{status: control.ProjectStatus{
		Ref:     project.Ref,
		Phase:   control.ProjectDegraded,
		Message: "compose live drift: missing live services realtime",
	}}, nil).Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetProject(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != control.ProjectDegraded || updated.Message != "compose live drift: missing live services realtime" {
		t.Fatalf("expected same-phase message update, got %#v", updated)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "project.reconciled" || events[0].Metadata["from"] != string(control.ProjectDegraded) || events[0].Metadata["to"] != string(control.ProjectDegraded) {
		t.Fatalf("expected same-phase reconciled audit event, got %#v", events)
	}
}

func TestReconcileLogsProvisionerErrors(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = New(store, fakeProvisioner{err: errors.New("compose file missing")}, nil).Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetProject(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != control.ProjectError {
		t.Fatalf("expected project error, got %#v", updated)
	}
	logs, err := store.ListProjectLogs(ctx, project.Ref, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Level != "error" || logs[0].Message != "Reconcile failed" || logs[0].Metadata["error"] != "compose file missing" {
		t.Fatalf("expected reconcile error project log, got %#v", logs)
	}
}

func TestReconcilePreservesPausedProject(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpdateProjectStatus(ctx, project.Ref, control.ProjectPaused, "compose project paused")
	if err != nil {
		t.Fatal(err)
	}

	err = New(store, fakeProvisioner{status: control.ProjectStatus{
		Ref:     project.Ref,
		Phase:   control.ProjectPaused,
		Message: "compose project paused",
	}}, nil).Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetProject(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != control.ProjectPaused {
		t.Fatalf("expected paused project to remain paused, got %#v", updated)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no reconcile audit event for unchanged paused state, got %#v", events)
	}
}

type fakeProvisioner struct {
	status control.ProjectStatus
	err    error
}

func (p fakeProvisioner) Name() string { return "fake" }

func (p fakeProvisioner) Create(ctx context.Context, spec control.ProjectSpec) error { return nil }

func (p fakeProvisioner) SyncSecrets(ctx context.Context, ref string, spec control.ProjectSpec) error {
	return nil
}

func (p fakeProvisioner) Destroy(ctx context.Context, ref string) error { return nil }

func (p fakeProvisioner) Status(ctx context.Context, ref string) (control.ProjectStatus, error) {
	if p.err != nil {
		return control.ProjectStatus{}, p.err
	}
	if p.status.Ref == "" {
		return control.ProjectStatus{}, errors.New("missing status")
	}
	return p.status, nil
}

func (p fakeProvisioner) Upgrade(ctx context.Context, ref string, version string) error { return nil }

func (p fakeProvisioner) Pause(ctx context.Context, ref string) error { return nil }

func (p fakeProvisioner) Resume(ctx context.Context, ref string) error { return nil }

func (p fakeProvisioner) Scale(ctx context.Context, ref string, tier control.ResourceTier) error {
	return nil
}

func (p fakeProvisioner) AddReplica(ctx context.Context, ref string, opts control.ReplicaOpts) error {
	return nil
}
