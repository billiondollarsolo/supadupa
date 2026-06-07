package reconciler

import (
	"context"
	"log/slog"
	"time"

	"supadupa2026/internal/control"
)

type Reconciler struct {
	store       control.Store
	provisioner control.Provisioner
	interval    time.Duration
	logger      *slog.Logger
}

func New(store control.Store, provisioner control.Provisioner, logger *slog.Logger) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		store:       store,
		provisioner: provisioner,
		interval:    30 * time.Second,
		logger:      logger,
	}
}

func (r *Reconciler) WithInterval(interval time.Duration) *Reconciler {
	if interval > 0 {
		r.interval = interval
	}
	return r
}

func (r *Reconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if err := r.Reconcile(ctx); err != nil {
			r.logger.Warn("reconcile failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Reconciler) Reconcile(ctx context.Context) error {
	if r.store == nil || r.provisioner == nil {
		return nil
	}
	projects, err := r.store.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, project := range projects {
		if project.Status == control.ProjectDestroying {
			continue
		}
		status, err := r.provisioner.Status(ctx, project.Ref)
		if err != nil {
			if project.Status != control.ProjectError {
				_, _ = r.store.UpdateProjectStatus(ctx, project.Ref, control.ProjectError, err.Error())
				control.LogProject(ctx, r.store, project.Ref, "error", "Reconcile failed", map[string]string{"error": err.Error()})
				control.Audit(ctx, r.store, "project.reconcile_error", "project:"+project.Ref, map[string]string{"error": err.Error()})
			}
			continue
		}
		if status.Phase != "" && (status.Phase != project.Status || status.Message != project.Message) {
			_, _ = r.store.UpdateProjectStatus(ctx, project.Ref, status.Phase, status.Message)
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
	return nil
}
