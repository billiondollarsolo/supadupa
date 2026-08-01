package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"supadupa2026/internal/env"

	"supadupa2026/internal/control"
)

func listProjectStorageBucketsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		buckets, err := store.ListProjectStorageBuckets(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskStorageBuckets(buckets))
	}
}

func createProjectStorageBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectStorageBucketInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		bucket, err := store.CreateProjectStorageBucket(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectStorageBucketCreate(r.Context(), store, project, bucket); err != nil {
			logRollbackError(r.Context(), "delete project storage bucket after apply failure", store.DeleteProjectStorageBucket(r.Context(), ref, bucket.Name))
			control.LogProject(r.Context(), store, ref, "error", "Storage bucket data-plane create failed", map[string]string{"name": bucket.Name, "error": err.Error()})
			control.Audit(r.Context(), store, "project.storage_bucket_create_failed", "project:"+ref, map[string]string{"name": bucket.Name, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":            bucket.Name,
			"public":          fmt.Sprintf("%t", bucket.Public),
			"file_size_limit": fmt.Sprintf("%d", bucket.FileSizeLimit),
		}
		control.LogProject(r.Context(), store, ref, "info", "Storage bucket configured", metadata)
		control.Audit(r.Context(), store, "project.storage_bucket_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskStorageBucket(bucket))
	}
}

func deleteProjectStorageBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		if err := applyProjectStorageBucketDelete(r.Context(), store, project, name); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Storage bucket data-plane delete failed", map[string]string{"name": strings.ToLower(strings.TrimSpace(name)), "error": err.Error()})
			control.Audit(r.Context(), store, "project.storage_bucket_delete_failed", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name)), "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectStorageBucket(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Storage bucket deleted", map[string]string{"name": name})
		control.Audit(r.Context(), store, "project.storage_bucket_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func applyProjectStorageBucketCreate(ctx context.Context, store control.Store, project control.Project, bucket control.ProjectStorageBucket) error {
	if !storageDataPlaneApplyEnabled() {
		return nil
	}
	payload := map[string]any{
		"id":                 bucket.Name,
		"name":               bucket.Name,
		"public":             bucket.Public,
		"file_size_limit":    bucket.FileSizeLimit,
		"allowed_mime_types": bucket.AllowedMimeTypes,
		"avif_autodetection": bucket.AvifAutodetection,
	}
	if strings.TrimSpace(bucket.CacheControl) != "" {
		payload["cache_control"] = bucket.CacheControl
	}
	return projectStorageDataPlaneRequest(ctx, store, project, http.MethodPost, "/storage/v1/bucket", payload, false)
}

func applyProjectStorageBucketDelete(ctx context.Context, store control.Store, project control.Project, name string) error {
	if !storageDataPlaneApplyEnabled() {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return fmt.Errorf("storage bucket name is required")
	}
	return projectStorageDataPlaneRequest(ctx, store, project, http.MethodDelete, "/storage/v1/bucket/"+url.PathEscape(normalized), nil, true)
}

func storageDataPlaneApplyEnabled() bool {
	if value := strings.TrimSpace(os.Getenv("SUPADUPA_STORAGE_APPLY")); value != "" {
		return env.BoolValue(value)
	}
	return env.BoolValue(os.Getenv("SUPADUPA_COMPOSE_APPLY")) || env.BoolValue(os.Getenv("SUPADUPA_K8S_APPLY"))
}

func envFalseValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "n", "off":
		return true
	default:
		return false
	}
}

func projectStorageDataPlaneRequest(ctx context.Context, store control.Store, project control.Project, method string, path string, payload any, allowNotFound bool) error {
	serviceRole, err := projectServiceRoleKey(ctx, store, project.Ref)
	if err != nil {
		return err
	}
	var body io.Reader = http.NoBody
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, projectStorageDataPlaneBaseURL(project)+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("apikey", serviceRole)
	request.Header.Set("Authorization", "Bearer "+serviceRole)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("storage data-plane request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	detail, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if allowNotFound && storageDataPlaneNotFound(response.StatusCode, detail) {
		return nil
	}
	return fmt.Errorf("storage data-plane %s %s returned HTTP %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(detail)))
}

func storageDataPlaneNotFound(status int, detail []byte) bool {
	if status == http.StatusNotFound {
		return true
	}
	if status != http.StatusBadRequest {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(detail, &payload); err != nil {
		return false
	}
	statusCode := strings.TrimSpace(fmt.Sprint(payload["statusCode"]))
	message := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["message"])))
	return statusCode == "404" || strings.Contains(message, "bucket not found")
}

func projectServiceRoleKey(ctx context.Context, store control.Store, ref string) (string, error) {
	secrets, err := store.EnsureProjectSecrets(ctx, ref)
	if err != nil {
		return "", err
	}
	for _, secret := range secrets {
		if secret.Kind == "service_role" && strings.TrimSpace(secret.Value) != "" {
			return secret.Value, nil
		}
	}
	return "", fmt.Errorf("project %s service_role key is not available", ref)
}

func projectStorageDataPlaneBaseURL(project control.Project) string {
	if configured := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPADUPA_STORAGE_APPLY_BASE_URL")), "/"); configured != "" {
		configured = strings.ReplaceAll(configured, "{{ref}}", project.Ref)
		configured = strings.ReplaceAll(configured, "{{project_ref}}", project.Ref)
		configured = strings.ReplaceAll(configured, "{{domain}}", project.Spec.Domain)
		return configured
	}
	return fmt.Sprintf("https://%s.%s", project.Ref, project.Spec.Domain)
}

func maskStorageBuckets(buckets []control.ProjectStorageBucket) []control.ProjectStorageBucket {
	out := make([]control.ProjectStorageBucket, len(buckets))
	copy(out, buckets)
	for index := range out {
		out[index] = maskStorageBucket(out[index])
	}
	return out
}

func maskStorageBucket(bucket control.ProjectStorageBucket) control.ProjectStorageBucket {
	bucket.Metadata = maskSensitiveStringMap(bucket.Metadata, isSensitiveMetadataKey)
	return bucket
}

func listProjectVectorBucketsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		buckets, err := store.ListProjectVectorBuckets(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskVectorBuckets(buckets))
	}
}

func createProjectVectorBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectVectorBucketInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		bucket, err := store.CreateProjectVectorBucket(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"name":            bucket.Name,
			"dimension":       fmt.Sprintf("%d", bucket.Dimension),
			"distance":        bucket.Distance,
			"storage_backend": bucket.StorageBackend,
		}
		control.LogProject(r.Context(), store, ref, "info", "Vector bucket configured", metadata)
		control.Audit(r.Context(), store, "project.vector_bucket_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskVectorBucket(bucket))
	}
}

func deleteProjectVectorBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		name := r.PathValue("name")
		if err := store.DeleteProjectVectorBucket(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Vector bucket deleted", map[string]string{"name": name})
		control.Audit(r.Context(), store, "project.vector_bucket_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskVectorBuckets(buckets []control.ProjectVectorBucket) []control.ProjectVectorBucket {
	out := make([]control.ProjectVectorBucket, len(buckets))
	copy(out, buckets)
	for index := range out {
		out[index] = maskVectorBucket(out[index])
	}
	return out
}

func maskVectorBucket(bucket control.ProjectVectorBucket) control.ProjectVectorBucket {
	bucket.Metadata = maskSensitiveStringMap(bucket.Metadata, isSensitiveMetadataKey)
	return bucket
}

func listProjectAnalyticsBucketsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		buckets, err := store.ListProjectAnalyticsBuckets(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskAnalyticsBuckets(buckets))
	}
}

func createProjectAnalyticsBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectAnalyticsBucketInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		bucket, err := store.CreateProjectAnalyticsBucket(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"name":           bucket.Name,
			"storage_uri":    bucket.StorageURI,
			"format_version": fmt.Sprintf("%d", bucket.FormatVersion),
			"warehouse":      bucket.Warehouse,
		}
		control.LogProject(r.Context(), store, ref, "info", "Analytics bucket configured", metadata)
		control.Audit(r.Context(), store, "project.analytics_bucket_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskAnalyticsBucket(bucket))
	}
}

func deleteProjectAnalyticsBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		name := r.PathValue("name")
		if err := store.DeleteProjectAnalyticsBucket(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Analytics bucket deleted", map[string]string{"name": name})
		control.Audit(r.Context(), store, "project.analytics_bucket_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskAnalyticsBuckets(buckets []control.ProjectAnalyticsBucket) []control.ProjectAnalyticsBucket {
	out := make([]control.ProjectAnalyticsBucket, len(buckets))
	copy(out, buckets)
	for index := range out {
		out[index] = maskAnalyticsBucket(out[index])
	}
	return out
}

func maskAnalyticsBucket(bucket control.ProjectAnalyticsBucket) control.ProjectAnalyticsBucket {
	if strings.TrimSpace(bucket.CredentialHandle) != "" {
		bucket.CredentialHandle = maskedSensitiveValue
	}
	bucket.Metadata = maskSensitiveStringMap(bucket.Metadata, isSensitiveMetadataKey)
	return bucket
}
