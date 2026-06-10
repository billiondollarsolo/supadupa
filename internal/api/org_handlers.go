package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"supadupa2026/internal/control"
)

type createOrgRequest struct {
	Name string `json:"name"`
}

func createOrgHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload createOrgRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		org, err := store.CreateOrg(r.Context(), payload.Name)
		if err != nil {
			writeDecodeError(w, err)
			return
		}
		if claims, ok := claimsFromRequest(r); ok {
			member, err := store.UpsertOrgMember(r.Context(), org.ID, control.MembershipInput{Email: claims.Email, Role: "owner"})
			if err != nil {
				writeStoreError(w, err)
				return
			}
			control.Audit(r.Context(), store, "org.member_upsert", "org:"+org.ID, map[string]string{"email": member.Email, "role": member.Role})
		}
		control.Audit(r.Context(), store, "org.create", "org:"+org.ID, map[string]string{"name": org.Name})
		writeJSON(w, http.StatusCreated, org)
	}
}

func getOrgHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		org, err := store.GetOrg(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, org)
	}
}

func updateOrgHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload createOrgRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		org, err := store.UpdateOrg(r.Context(), orgID, payload.Name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.update", "org:"+org.ID, map[string]string{"name": org.Name})
		writeJSON(w, http.StatusOK, org)
	}
}

func deleteOrgHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		if err := store.DeleteOrg(r.Context(), orgID); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.delete", "org:"+orgID, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func getOrgQuotaHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		quota, err := store.GetOrgQuota(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, quota)
	}
}

func updateOrgQuotaHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload control.OrgQuotaInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		quota, err := store.UpdateOrgQuota(r.Context(), orgID, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.quota_update", "org:"+orgID, map[string]string{
			"max_projects": fmt.Sprintf("%d", quota.MaxProjects),
			"max_cpu":      fmt.Sprintf("%d", quota.MaxCPU),
			"max_ram_mb":   fmt.Sprintf("%d", quota.MaxRAMMB),
			"max_disk_gb":  fmt.Sprintf("%d", quota.MaxDiskGB),
		})
		writeJSON(w, http.StatusOK, quota)
	}
}

func getOrgFeatureFlagsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		flags, err := store.GetOrgFeatureFlags(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, flags)
	}
}

func updateOrgFeatureFlagsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload control.OrgFeatureFlagsInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		flags, err := store.UpdateOrgFeatureFlags(r.Context(), orgID, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.features_update", "org:"+orgID, map[string]string{
			"overrides": fmt.Sprintf("%d", len(flags.Overrides)),
			"enabled":   fmt.Sprintf("%d", countEnabledFlags(flags.Effective)),
		})
		writeJSON(w, http.StatusOK, flags)
	}
}

func getOrgUsageHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		usage, err := store.GetOrgUsage(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, usage)
	}
}

func listOrgUsageSnapshotsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 {
				writeError(w, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
			limit = parsed
		}
		snapshots, err := store.ListOrgUsageSnapshots(r.Context(), orgID, limit)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snapshots)
	}
}

func createOrgUsageSnapshotHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		if !requireOrgFeature(w, r, store, orgID, "usage_metering") {
			return
		}
		snapshot, err := store.CreateOrgUsageSnapshot(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.usage_snapshot_create", "org:"+orgID, map[string]string{"snapshot_id": snapshot.ID})
		writeJSON(w, http.StatusCreated, snapshot)
	}
}

func listBillingInvoicesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		if !requireOrgFeature(w, r, store, orgID, "billing") {
			return
		}
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 {
				writeError(w, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
			limit = parsed
		}
		invoices, err := store.ListBillingInvoices(r.Context(), orgID, limit)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, invoices)
	}
}

func createBillingInvoiceHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		if !requireOrgFeature(w, r, store, orgID, "billing") {
			return
		}
		var payload control.BillingInvoiceInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		invoice, err := store.CreateBillingInvoice(r.Context(), orgID, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.billing_invoice_create", "org:"+orgID, map[string]string{
			"invoice_id": invoice.ID,
			"number":     invoice.Number,
			"status":     invoice.Status,
			"total":      fmt.Sprintf("%d", invoice.TotalCents),
		})
		writeJSON(w, http.StatusCreated, invoice)
	}
}

func getBillingInvoiceHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		if !requireOrgFeature(w, r, store, orgID, "billing") {
			return
		}
		invoice, err := store.GetBillingInvoice(r.Context(), orgID, r.PathValue("invoice_id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, invoice)
	}
}

func getOrgAccessReviewHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		review, err := store.GetOrgAccessReview(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, review)
	}
}

func listOrgMembersHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		members, err := store.ListOrgMembers(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, members)
	}
}

func upsertOrgMemberHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload control.MembershipInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		member, err := store.UpsertOrgMember(r.Context(), orgID, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.member_upsert", "org:"+orgID, map[string]string{"email": member.Email, "role": member.Role})
		writeJSON(w, http.StatusOK, member)
	}
}

func deleteOrgMemberHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		email := r.PathValue("email")
		if err := store.DeleteOrgMember(r.Context(), orgID, email); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.member_delete", "org:"+orgID, map[string]string{"email": strings.ToLower(strings.TrimSpace(email))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listOrgTeamsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		teams, err := store.ListOrgTeams(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, teams)
	}
}

func createOrgTeamHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload control.TeamInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		team, err := store.CreateOrgTeam(r.Context(), orgID, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.team_create", "org:"+orgID, map[string]string{"team": team.Slug, "name": team.Name})
		writeJSON(w, http.StatusCreated, team)
	}
}

func deleteOrgTeamHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		slug := r.PathValue("slug")
		if err := store.DeleteOrgTeam(r.Context(), orgID, slug); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.team_delete", "org:"+orgID, map[string]string{"team": slug})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listTeamMembersHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		members, err := store.ListTeamMembers(r.Context(), orgID, r.PathValue("slug"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, members)
	}
}

func upsertTeamMemberHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		slug := r.PathValue("slug")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload control.TeamMemberInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		member, err := store.UpsertTeamMember(r.Context(), orgID, slug, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.team_member_upsert", "org:"+orgID, map[string]string{"team": member.TeamSlug, "email": member.Email})
		writeJSON(w, http.StatusOK, member)
	}
}

func deleteTeamMemberHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		slug := r.PathValue("slug")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		email := r.PathValue("email")
		if err := store.DeleteTeamMember(r.Context(), orgID, slug, email); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.team_member_delete", "org:"+orgID, map[string]string{"team": slug, "email": strings.ToLower(strings.TrimSpace(email))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listOrgsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgs, err := orgsVisibleToRequest(r, store)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, orgs)
	}
}

func listOrgProjectsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		projects, err := store.ListProjectsByOrg(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sanitizeProjectsForResponse(projects))
	}
}

func listProjectsHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projects, err := projectsVisibleToRequest(r, store)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		hydrateRuntimeStatus(r.Context(), provisioner, projects)
		hydrateDBIngressMode(r.Context(), store, projects)
		writeJSON(w, http.StatusOK, sanitizeProjectsForResponse(projects))
	}
}

// dbIngressModeFor returns a project's configured database exposure mode
// ("private" when unset), read from its network config.
func dbIngressModeFor(ctx context.Context, store control.Store, ref string) string {
	cfg, err := store.GetProjectConfig(ctx, ref, "network")
	if err != nil {
		return "private"
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.Config["db_ingress_mode"]))
	if mode == "" {
		return "private"
	}
	return mode
}

// hydrateDBIngressMode fills each project's configured DB exposure mode so list
// views can flag publicly-reachable databases. Reads are in-memory map lookups.
func hydrateDBIngressMode(ctx context.Context, store control.Store, projects []control.Project) {
	for i := range projects {
		projects[i].DBIngressMode = dbIngressModeFor(ctx, store, projects[i].Ref)
	}
}

// hydrateRuntimeStatus fills each project's live RuntimeStatus by querying the
// provisioner concurrently. Each status call can shell out to the backend (e.g.
// `docker compose ps`), so we cap concurrency and bound the whole pass with a
// short deadline — the list must stay responsive even if one project hangs, and
// projects we couldn't reach simply keep a nil runtime_status.
func hydrateRuntimeStatus(ctx context.Context, provisioner control.Provisioner, projects []control.Project) {
	if provisioner == nil || len(projects) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	const maxConcurrent = 8
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i := range projects {
		wg.Add(1)
		go func(p *control.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			status, err := provisioner.Status(ctx, p.Ref)
			if err == nil {
				p.RuntimeStatus = &status
			}
		}(&projects[i])
	}
	wg.Wait()
}
