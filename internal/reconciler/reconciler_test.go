package reconciler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	// Hand the project off out of "provisioning" — the reconciler only acts on
	// projects the create goroutine has already released.
	if _, err := store.UpdateProjectStatus(ctx, project.Ref, control.ProjectStarting, "starting"); err != nil {
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
	if len(events) != 0 {
		t.Fatalf("reconcile must not write audit events, got %#v", events)
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
	if len(events) != 0 {
		t.Fatalf("reconcile must not write audit events, got %#v", events)
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
	if len(events) != 0 {
		t.Fatalf("reconcile must not write audit events, got %#v", events)
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
	if _, err := store.UpdateProjectStatus(ctx, project.Ref, control.ProjectStarting, "starting"); err != nil {
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

func TestReconcileReturnsStatusUpdateErrors(t *testing.T) {
	ctx := context.Background()
	base := control.NewMemoryStore()
	org, err := base.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := base.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.UpdateProjectStatus(ctx, project.Ref, control.ProjectStarting, "starting"); err != nil {
		t.Fatal(err)
	}
	store := failingStatusStore{Store: base, err: errors.New("checkpoint failed")}

	err = New(store, fakeProvisioner{status: control.ProjectStatus{
		Ref:     project.Ref,
		Phase:   control.ProjectHealthy,
		Message: "runtime healthy",
	}}, nil).Reconcile(ctx)
	if err == nil || !strings.Contains(err.Error(), "checkpoint failed") || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("expected status update error with project ref, got %v", err)
	}
}

func TestReconcileReturnsStatusUpdateErrorsAfterProvisionerError(t *testing.T) {
	ctx := context.Background()
	base := control.NewMemoryStore()
	org, err := base.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := base.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.UpdateProjectStatus(ctx, project.Ref, control.ProjectStarting, "starting"); err != nil {
		t.Fatal(err)
	}
	store := failingStatusStore{Store: base, err: errors.New("checkpoint failed")}

	err = New(store, fakeProvisioner{err: errors.New("compose file missing")}, nil).Reconcile(ctx)
	if err == nil || !strings.Contains(err.Error(), "checkpoint failed") || !strings.Contains(err.Error(), "provisioner error") {
		t.Fatalf("expected provisioner status update error, got %v", err)
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

func TestReconcileSkipsActiveProvisioning(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	// A freshly created project sits in "provisioning", owned by the create
	// goroutine. The provisioner here reports an error (the runtime is only
	// half-up), but the reconciler must not clobber the provisioning status.
	project, err := store.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = New(store, fakeProvisioner{err: errors.New("compose not up yet")}, nil).Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetProject(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != control.ProjectProvisioning {
		t.Fatalf("expected project to remain provisioning, got %s", updated.Status)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no reconcile events for active provisioning, got %#v", events)
	}
}

func TestReconcileTakesOverStaleProvisioning(t *testing.T) {
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

	// Pretend the create goroutine died and the project has been stuck in
	// provisioning past the grace window; the reconciler should now converge it.
	future := func() time.Time { return project.UpdatedAt.Add(provisionGracePeriod + time.Minute) }
	err = New(store, fakeProvisioner{status: control.ProjectStatus{
		Ref:     project.Ref,
		Phase:   control.ProjectError,
		Message: "compose project not found",
	}}, nil).WithNow(future).Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetProject(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != control.ProjectError {
		t.Fatalf("expected stale provisioning project to be recovered to error, got %s", updated.Status)
	}
}

type failingStatusStore struct {
	control.Store
	err error
}

func (s failingStatusStore) UpdateProjectStatus(ctx context.Context, ref string, status control.ProjectPhase, message string) (control.Project, error) {
	return control.Project{}, s.err
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

func (p fakeProvisioner) Scale(ctx context.Context, ref string, spec control.ProjectSpec) error {
	return nil
}

func (p fakeProvisioner) AddReplica(ctx context.Context, ref string, opts control.ReplicaOpts) error {
	return nil
}
