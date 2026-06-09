package api

import (
	"net/http"

	"supadupa2026/internal/control"
)

func listProjectReplicationPipelinesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		pipelines, err := store.ListProjectReplicationPipelines(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskReplicationPipelineConfigs(pipelines))
	}
}

func createProjectReplicationPipelineHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectReplicationPipelineInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		pipeline, err := store.CreateProjectReplicationPipeline(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Replication pipeline configured", map[string]string{
			"pipeline_id": pipeline.ID,
			"name":        pipeline.Name,
			"type":        pipeline.Type,
			"destination": pipeline.Destination,
		})
		control.Audit(r.Context(), store, "project.replication_create", "project:"+ref, map[string]string{
			"pipeline_id": pipeline.ID,
			"name":        pipeline.Name,
			"type":        pipeline.Type,
			"destination": pipeline.Destination,
		})
		writeJSON(w, http.StatusCreated, maskReplicationPipelineConfig(pipeline))
	}
}

func deleteProjectReplicationPipelineHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		id := r.PathValue("id")
		if err := store.DeleteProjectReplicationPipeline(r.Context(), ref, id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Replication pipeline deleted", map[string]string{"pipeline_id": id})
		control.Audit(r.Context(), store, "project.replication_delete", "project:"+ref, map[string]string{"pipeline_id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskReplicationPipelineConfigs(pipelines []control.ProjectReplicationPipeline) []control.ProjectReplicationPipeline {
	out := make([]control.ProjectReplicationPipeline, len(pipelines))
	copy(out, pipelines)
	for index := range out {
		out[index] = maskReplicationPipelineConfig(out[index])
	}
	return out
}

func maskReplicationPipelineConfig(pipeline control.ProjectReplicationPipeline) control.ProjectReplicationPipeline {
	pipeline.Config = maskSensitiveStringMap(pipeline.Config, isSensitiveMetadataKey)
	return pipeline
}

func listProjectEmbeddingJobsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		jobs, err := store.ListProjectEmbeddingJobs(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, jobs)
	}
}

func createProjectEmbeddingJobHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectEmbeddingJobInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		job, err := store.CreateProjectEmbeddingJob(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"job_id":      job.ID,
			"name":        job.Name,
			"source":      job.SourceSchema + "." + job.SourceTable + "." + job.SourceColumn,
			"provider":    job.Provider,
			"model":       job.Model,
			"destination": job.DestinationTable + "." + job.DestinationColumn,
		}
		control.LogProject(r.Context(), store, ref, "info", "Embedding job configured", metadata)
		control.Audit(r.Context(), store, "project.embedding_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, job)
	}
}

func deleteProjectEmbeddingJobHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		id := r.PathValue("id")
		if err := store.DeleteProjectEmbeddingJob(r.Context(), ref, id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Embedding job deleted", map[string]string{"job_id": id})
		control.Audit(r.Context(), store, "project.embedding_delete", "project:"+ref, map[string]string{"job_id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}
