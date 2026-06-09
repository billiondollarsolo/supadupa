package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"supadupa2026/internal/control"
)

func listProjectAccessHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		grants, err := store.ListProjectAccess(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, grants)
	}
}

func upsertProjectAccessHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectAccessInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		grant, err := store.UpsertProjectAccess(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "project.access_upsert", "project:"+ref, map[string]string{"subject_type": grant.SubjectType, "subject_id": grant.SubjectID, "subject_name": grant.SubjectName, "role": grant.Role})
		writeJSON(w, http.StatusOK, grant)
	}
}

func deleteProjectAccessHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		subjectType := r.PathValue("subjectType")
		subjectID := r.PathValue("subjectID")
		if err := store.DeleteProjectAccess(r.Context(), ref, subjectType, subjectID); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "project.access_delete", "project:"+ref, map[string]string{"subject_type": subjectType, "subject_id": subjectID})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listProjectRoutesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		routes, err := store.ListProjectRoutes(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, routes)
	}
}

func getProjectRouteManifestHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleViewer)
		if !ok {
			return
		}
		routes, err := store.ListProjectRoutes(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		networkConfig, err := store.GetProjectConfig(r.Context(), ref, "network")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		replicas, err := store.ListProjectReplicas(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		defaults, err := store.GetPlatformDefaults(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		gatedNetwork := control.ApplyDatabaseExternalAccessGate(networkConfig, defaults)
		manifest := control.RouteManifestForProjectWithNetworkReplicasAndDatabaseIngress(project, routes, gatedNetwork, replicas, defaults.DatabaseIngressAllowedCIDRs)
		manifest.DatabaseIngressPublished = databaseIngressStatusFromEnvAndDefaults(os.Getenv, defaults).Public
		manifest.DatabaseExternalAccessEnabled = defaults.FeatureFlags[control.DatabaseExternalAccessFlag]
		writeJSON(w, http.StatusOK, manifest)
	}
}

func listProjectDomainsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		domains, err := store.ListProjectDomains(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, domains)
	}
}

func addProjectDomainHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "custom_domains") {
			return
		}
		var payload control.ProjectDomainInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		domain, err := store.AddProjectDomain(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		cert, certErr := control.NewCertificateService().Provision(r.Context(), domain)
		if certErr != nil {
			updated, updateErr := store.UpdateProjectDomainCertificate(r.Context(), ref, domain.FQDN, control.ProjectDomainCertificateMetadata{Status: "failed", Mode: certModeFromState(cert.State)})
			if updateErr == nil {
				domain = updated
			}
			control.LogProject(r.Context(), store, ref, "error", "Custom domain certificate failed", map[string]string{"fqdn": domain.FQDN, "error": certErr.Error(), "cert_path": cert.Path})
			control.Audit(r.Context(), store, "project.domain_cert_failed", "project:"+ref, map[string]string{"fqdn": domain.FQDN, "error": certErr.Error(), "cert_path": cert.Path})
		} else {
			updated, updateErr := store.UpdateProjectDomainCertificate(r.Context(), ref, domain.FQDN, control.ProjectDomainCertificateMetadata{Status: cert.Status, Mode: certModeFromState(cert.State)})
			if updateErr != nil {
				writeStoreError(w, updateErr)
				return
			}
			domain = updated
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		certMetadata := map[string]string{"fqdn": domain.FQDN, "route_path": routePath, "cert_status": domain.CertStatus, "cert_path": cert.Path, "cert_state": cert.State}
		control.LogProject(r.Context(), store, ref, "info", "Custom domain added", certMetadata)
		control.Audit(r.Context(), store, "project.domain_create", "project:"+ref, certMetadata)
		writeJSON(w, http.StatusCreated, domain)
	}
}

func certModeFromState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "manual":
		return "manual"
	case "completed":
		return "command"
	case "byo":
		return "byo"
	default:
		return "acme"
	}
}

func deleteProjectDomainHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		fqdn := r.PathValue("fqdn")
		if err := control.NewCertificateService().Remove(r.Context(), ref, fqdn); err != nil && !errors.Is(err, os.ErrNotExist) {
			control.LogProject(r.Context(), store, ref, "error", "Custom domain certificate cleanup failed", map[string]string{"fqdn": fqdn, "error": err.Error()})
			control.Audit(r.Context(), store, "project.domain_cert_cleanup_failed", "project:"+ref, map[string]string{"fqdn": fqdn, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectDomain(r.Context(), ref, fqdn); err != nil {
			writeStoreError(w, err)
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Custom domain removed", map[string]string{"fqdn": fqdn, "route_path": routePath})
		control.Audit(r.Context(), store, "project.domain_delete", "project:"+ref, map[string]string{"fqdn": fqdn})
		w.WriteHeader(http.StatusNoContent)
	}
}

func uploadProjectDomainCertificateHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "custom_domains") {
			return
		}
		domain, err := findProjectDomain(r.Context(), store, ref, r.PathValue("fqdn"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var payload control.ProjectDomainCertificateInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		cert, err := control.NewCertificateService().Upload(r.Context(), domain, payload.CertificatePEM, payload.PrivateKeyPEM)
		if err != nil {
			writeDecodeError(w, err)
			return
		}
		updated, err := store.UpdateProjectDomainCertificate(r.Context(), ref, domain.FQDN, control.ProjectDomainCertificateMetadata{
			Status:      cert.Status,
			Mode:        "byo",
			Fingerprint: cert.Fingerprint,
			NotAfter:    cert.NotAfter,
		})
		if err != nil {
			_ = control.NewCertificateService().Remove(r.Context(), ref, domain.FQDN)
			writeStoreError(w, err)
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"fqdn":             updated.FQDN,
			"route_path":       routePath,
			"cert_status":      updated.CertStatus,
			"cert_mode":        updated.CertMode,
			"cert_fingerprint": updated.CertFingerprint,
		}
		control.LogProject(r.Context(), store, ref, "info", "Custom domain certificate uploaded", metadata)
		control.Audit(r.Context(), store, "project.domain_cert_upload", "project:"+ref, metadata)
		writeJSON(w, http.StatusOK, updated)
	}
}

func resetProjectDomainCertificateHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "custom_domains") {
			return
		}
		domain, err := findProjectDomain(r.Context(), store, ref, r.PathValue("fqdn"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		certService := control.NewCertificateService()
		if err := certService.Remove(r.Context(), ref, domain.FQDN); err != nil && !errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		cert, certErr := certService.Provision(r.Context(), domain)
		status := cert.Status
		mode := "acme"
		if cert.State == "manual" {
			mode = "manual"
		}
		if certErr != nil {
			status = "failed"
		}
		updated, err := store.UpdateProjectDomainCertificate(r.Context(), ref, domain.FQDN, control.ProjectDomainCertificateMetadata{
			Status: status,
			Mode:   mode,
		})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{"fqdn": updated.FQDN, "route_path": routePath, "cert_status": updated.CertStatus, "cert_mode": updated.CertMode, "cert_path": cert.Path, "cert_state": cert.State}
		if certErr != nil {
			metadata["error"] = certErr.Error()
			control.LogProject(r.Context(), store, ref, "error", "Custom domain certificate reset failed", metadata)
			control.Audit(r.Context(), store, "project.domain_cert_reset_failed", "project:"+ref, metadata)
		} else {
			control.LogProject(r.Context(), store, ref, "info", "Custom domain certificate reset", metadata)
			control.Audit(r.Context(), store, "project.domain_cert_reset", "project:"+ref, metadata)
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

func findProjectDomain(ctx context.Context, store control.Store, ref string, fqdn string) (control.ProjectDomain, error) {
	needle := strings.Trim(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	domains, err := store.ListProjectDomains(ctx, ref)
	if err != nil {
		return control.ProjectDomain{}, err
	}
	for _, domain := range domains {
		if domain.FQDN == needle {
			return domain, nil
		}
	}
	return control.ProjectDomain{}, fmt.Errorf("%w: domain %s for project %s", control.ErrNotFound, fqdn, ref)
}
