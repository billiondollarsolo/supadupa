package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"supadupa2026/internal/control"
	"supadupa2026/internal/scheduler"
)

// edgeNetworkEnsurer is implemented by provisioners that attach the shared
// edge-router to each project's isolated edge network. The reconciler re-runs it
// every cycle so routing self-heals if the edge-router restarts independently of
// the control plane (which would otherwise leave it detached and 502 all
// projects until the next control-plane restart). The call is idempotent.
type edgeNetworkEnsurer interface {
	EnsureEdgeNetworking(ctx context.Context, ref string) error
}

// provisionGracePeriod is how long the reconciler leaves a project in the
// "provisioning" phase untouched. While a project is provisioning it is owned by
// the in-flight create goroutine (api.provisionNewProject), which runs compose up
// plus the edge-router attach and writes the terminal healthy/error status itself
// under a 20-minute ceiling. This grace is that ceiling plus margin: once it
// elapses and the project is still provisioning, the goroutine is gone (e.g. the
// control plane restarted mid-provision) and the reconciler takes over to recover
// it. See the skip in Reconcile.
const provisionGracePeriod = 25 * time.Minute

type Reconciler struct {
	store          control.Store
	provisioner    control.Provisioner
	interval       time.Duration
	logger         *slog.Logger
	runner         *scheduler.PeriodicRunner
	now            func() time.Time
	provisionGrace time.Duration
}

func New(store control.Store, provisioner control.Provisioner, logger *slog.Logger) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		store:          store,
		provisioner:    provisioner,
		interval:       30 * time.Second,
		logger:         logger,
		runner:         scheduler.NewPeriodicRunner("reconciler", logger),
		now:            time.Now,
		provisionGrace: provisionGracePeriod,
	}
}

// WithNow overrides the reconciler's clock. Used by tests to age a project's
// provisioning window deterministically.
func (r *Reconciler) WithNow(now func() time.Time) *Reconciler {
	if now != nil {
		r.now = now
	}
	return r
}

func (r *Reconciler) WithInterval(interval time.Duration) *Reconciler {
	if interval > 0 {
		r.interval = interval
	}
	return r
}

func (r *Reconciler) Run(ctx context.Context) {
	if r.runner == nil {
		r.runner = scheduler.NewPeriodicRunner("reconciler", r.logger)
	}
	r.runner.Run(ctx, r.interval, func(ctx context.Context) {
		if err := r.Reconcile(ctx); err != nil {
			r.logger.Warn("reconcile failed", "error", err)
		}
	})
}

func (r *Reconciler) Reconcile(ctx context.Context) error {
	if r.store == nil || r.provisioner == nil {
		return nil
	}
	projects, err := r.store.ListProjects(ctx)
	if err != nil {
		return err
	}
	now := r.now
	if now == nil {
		now = time.Now
	}
	grace := r.provisionGrace
	if grace <= 0 {
		grace = provisionGracePeriod
	}
	var reconcileErr error
	for _, project := range projects {
		if project.Status == control.ProjectDestroying {
			continue
		}
		// A freshly-created project in the "provisioning" phase is owned by the
		// in-flight create goroutine, which writes its terminal status itself. If
		// we polled the half-up runtime here we would see missing containers and
		// overwrite "provisioning" with a spurious "error" that then flaps back to
		// healthy once the goroutine finishes. Leave it alone until the goroutine's
		// ceiling has well and truly elapsed (provisionGracePeriod), after which a
		// still-provisioning project means the goroutine died and we take over.
		if project.Status == control.ProjectProvisioning && now().Sub(project.UpdatedAt) < grace {
			continue
		}
		// Re-attach the edge-router to this project's edge network. Idempotent and
		// best-effort: a no-op when already connected, but it transparently heals
		// routing if the edge-router container restarted on its own.
		if ensurer, ok := r.provisioner.(edgeNetworkEnsurer); ok {
			if err := ensurer.EnsureEdgeNetworking(ctx, project.Ref); err != nil {
				r.logger.Debug("ensure edge networking failed", "project", project.Ref, "error", err)
			}
		}
		status, err := r.provisioner.Status(ctx, project.Ref)
		if err != nil {
			if project.Status != control.ProjectError {
				if _, updateErr := r.store.UpdateProjectStatus(ctx, project.Ref, control.ProjectError, err.Error()); updateErr != nil {
					reconcileErr = errors.Join(reconcileErr, fmt.Errorf("update project %s status after provisioner error: %w", project.Ref, updateErr))
				}
				control.LogProject(ctx, r.store, project.Ref, "error", "Reconcile failed", map[string]string{"error": err.Error()})
				control.Audit(ctx, r.store, "project.reconcile_error", "project:"+project.Ref, map[string]string{"error": err.Error()})
			}
			continue
		}
		if status.Phase != "" && (status.Phase != project.Status || status.Message != project.Message) {
			if _, err := r.store.UpdateProjectStatus(ctx, project.Ref, status.Phase, status.Message); err != nil {
				reconcileErr = errors.Join(reconcileErr, fmt.Errorf("update project %s reconciled status: %w", project.Ref, err))
				continue
			}
			level := "info"
			if status.Phase == control.ProjectDegraded || status.Phase == control.ProjectError {
				level = "warning"
			}
			control.LogProject(ctx, r.store, project.Ref, level, "Project reconciled", map[string]string{
				"from":    string(project.Status),
				"to":      string(status.Phase),
				"message": status.Message,
			})
			control.Audit(ctx, r.store, "project.reconciled", "project:"+project.Ref, map[string]string{
				"from": string(project.Status),
				"to":   string(status.Phase),
			})
		}
	}
	return reconcileErr
}
