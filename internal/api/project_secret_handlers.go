package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

func listProjectSecretsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		secrets, err := store.ListProjectSecrets(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, secrets)
	}
}

func revealProjectSecretHandler(store control.Store, limiter *fixedWindowLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		kind := r.PathValue("kind")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		if allowed, retryAfter := limiter.TakeAll(secretAccessKeys(r, "secret-reveal", ref, kind), time.Now().UTC()); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeError(w, http.StatusTooManyRequests, "too many secret access attempts")
			return
		}
		secret, err := store.RevealProjectSecret(r.Context(), ref, kind)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Secret revealed", map[string]string{"kind": kind})
		control.Audit(r.Context(), store, "project.secret_reveal", "project:"+ref, map[string]string{"kind": kind})
		writeJSON(w, http.StatusOK, control.SecretRevealFor(secret))
	}
}

func upsertProjectSecretHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		kind := r.PathValue("kind")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectSecretInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		secret, err := store.UpsertProjectSecret(r.Context(), ref, kind, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Secret updated", map[string]string{"kind": secret.Kind})
		control.Audit(r.Context(), store, "project.secret_upsert", "project:"+ref, map[string]string{"kind": secret.Kind})
		writeJSON(w, http.StatusOK, secret)
	}
}

func deleteProjectSecretHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		kind := r.PathValue("kind")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		if err := store.DeleteProjectSecret(r.Context(), ref, kind); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Secret deleted", map[string]string{"kind": strings.ToLower(strings.TrimSpace(kind))})
		control.Audit(r.Context(), store, "project.secret_delete", "project:"+ref, map[string]string{"kind": strings.ToLower(strings.TrimSpace(kind))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func auditProjectSecretCopyHandler(store control.Store, limiter *fixedWindowLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		kind := r.PathValue("kind")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		if allowed, retryAfter := limiter.TakeAll(secretAccessKeys(r, "secret-copy", ref, kind), time.Now().UTC()); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeError(w, http.StatusTooManyRequests, "too many secret access attempts")
			return
		}
		if _, err := store.RevealProjectSecret(r.Context(), ref, kind); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Secret copied", map[string]string{"kind": kind})
		control.Audit(r.Context(), store, "project.secret_copy", "project:"+ref, map[string]string{"kind": kind})
		w.WriteHeader(http.StatusNoContent)
	}
}

func rotateProjectSecretHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.RotateProjectSecretRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		secret, err := store.RotateProjectSecret(r.Context(), ref, payload.Kind)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if provisioner != nil {
			project, err := store.GetProject(r.Context(), ref)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			secrets, err := store.ListProjectSecrets(r.Context(), ref)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			spec := control.ProjectSpecWithSecrets(project.Spec, secrets)
			if err := provisioner.SyncSecrets(r.Context(), ref, spec); err != nil {
				_, _ = store.UpdateProjectStatus(r.Context(), ref, control.ProjectDegraded, err.Error())
				control.LogProject(r.Context(), store, ref, "error", "Secret sync failed", map[string]string{"kind": secret.Kind, "error": err.Error()})
				control.Audit(r.Context(), store, "project.secret_sync_failed", "project:"+ref, map[string]string{"kind": secret.Kind, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
		}
		control.LogProject(r.Context(), store, ref, "warning", "Secret rotated", map[string]string{"kind": secret.Kind})
		control.Audit(r.Context(), store, "project.secret_rotate", "project:"+ref, map[string]string{"kind": secret.Kind})
		writeJSON(w, http.StatusOK, secret)
	}
}
