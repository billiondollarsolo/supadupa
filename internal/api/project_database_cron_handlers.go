package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"supadupa2026/internal/control"
)

func listProjectDatabaseCronJobsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		jobs, err := store.ListProjectDatabaseCronJobs(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskDatabaseCronJobs(jobs))
	}
}

func createProjectDatabaseCronJobHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectDatabaseCronJobInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		job, err := store.CreateProjectDatabaseCronJob(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseCronJobCreate(r.Context(), project, job); err != nil {
			logRollbackError(r.Context(), "delete project database cron job after apply failure", store.DeleteProjectDatabaseCronJob(r.Context(), ref, job.Name))
			metadata := map[string]string{"name": job.Name, "schedule": job.Schedule, "active": fmt.Sprintf("%t", job.Active), "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database cron job apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_cron_create_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":     job.Name,
			"schedule": job.Schedule,
			"active":   fmt.Sprintf("%t", job.Active),
		}
		control.LogProject(r.Context(), store, ref, "info", "Database cron job configured", metadata)
		control.Audit(r.Context(), store, "project.database_cron_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskDatabaseCronJob(job))
	}
}

func deleteProjectDatabaseCronJobHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		job, ok, err := getCurrentProjectDatabaseCronJob(r.Context(), store, ref, name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok {
			if err := store.DeleteProjectDatabaseCronJob(r.Context(), ref, name); err != nil {
				writeStoreError(w, err)
			}
			return
		}
		if err := applyProjectDatabaseCronJobDelete(r.Context(), project, job); err != nil {
			metadata := map[string]string{"name": job.Name, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database cron job delete apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_cron_delete_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectDatabaseCronJob(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Database cron job deleted", map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		control.Audit(r.Context(), store, "project.database_cron_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func getCurrentProjectDatabaseCronJob(ctx context.Context, store control.Store, ref string, name string) (control.ProjectDatabaseCronJob, bool, error) {
	jobs, err := store.ListProjectDatabaseCronJobs(ctx, ref)
	if err != nil {
		return control.ProjectDatabaseCronJob{}, false, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, job := range jobs {
		if job.Name == normalized {
			return job, true, nil
		}
	}
	return control.ProjectDatabaseCronJob{}, false, nil
}

func applyProjectDatabaseCronJobCreate(ctx context.Context, project control.Project, job control.ProjectDatabaseCronJob) error {
	if !databaseRuntimeApplyEnabled() || !job.Active {
		return nil
	}
	sql := "CREATE EXTENSION IF NOT EXISTS pg_cron WITH SCHEMA extensions;\n" +
		"SELECT cron.schedule_in_database(" +
		quoteDatabaseLiteral(job.Name) + ", " +
		quoteDatabaseLiteral(job.Schedule) + ", " +
		quoteDatabaseLiteral(job.Command) + ", " +
		quoteDatabaseLiteral(job.Database) + ", " +
		quoteDatabaseLiteral(job.Username) + ", true);\n"
	return execProjectDatabaseSQL(ctx, project, sql)
}

func applyProjectDatabaseCronJobDelete(ctx context.Context, project control.Project, job control.ProjectDatabaseCronJob) error {
	if !databaseRuntimeApplyEnabled() || !job.Active {
		return nil
	}
	return execProjectDatabaseSQL(ctx, project, "SELECT cron.unschedule(jobid) FROM cron.job WHERE jobname = "+quoteDatabaseLiteral(job.Name)+";\n")
}

func maskDatabaseCronJobs(jobs []control.ProjectDatabaseCronJob) []control.ProjectDatabaseCronJob {
	out := make([]control.ProjectDatabaseCronJob, len(jobs))
	copy(out, jobs)
	for index := range out {
		out[index] = maskDatabaseCronJob(out[index])
	}
	return out
}

func maskDatabaseCronJob(job control.ProjectDatabaseCronJob) control.ProjectDatabaseCronJob {
	job.Metadata = maskSensitiveStringMap(job.Metadata, isSensitiveMetadataKey)
	return job
}
