package api

import (
	"net/http"
	"strings"
	"testing"

	"supadupa2026/internal/control"
)

func TestPlatformAdminTokenInvalidatedAfterDemotionAndDeletion(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"owner@example.com","password":"super-secure"}`)
	ownerToken := extractString(t, bootstrap.Body.String(), "token")
	demotedAdmin := performWithToken(server, http.MethodPost, "/v1/users", `{"email":"demote-admin@example.com","password":"super-secure","role":"admin"}`, ownerToken)
	if demotedAdmin.Code != http.StatusCreated {
		t.Fatalf("expected demoted admin create 201, got %d: %s", demotedAdmin.Code, demotedAdmin.Body.String())
	}
	demotedAdminID := extractString(t, demotedAdmin.Body.String(), "id")
	deletedAdmin := performWithToken(server, http.MethodPost, "/v1/users", `{"email":"delete-admin@example.com","password":"super-secure","role":"admin"}`, ownerToken)
	if deletedAdmin.Code != http.StatusCreated {
		t.Fatalf("expected deleted admin create 201, got %d: %s", deletedAdmin.Code, deletedAdmin.Body.String())
	}
	deletedAdminID := extractString(t, deletedAdmin.Body.String(), "id")

	demotedLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"demote-admin@example.com","password":"super-secure"}`)
	demotedToken := extractString(t, demotedLogin.Body.String(), "token")
	deletedLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"delete-admin@example.com","password":"super-secure"}`)
	deletedToken := extractString(t, deletedLogin.Body.String(), "token")
	beforeDemotion := performWithToken(server, http.MethodGet, "/v1/users", "", demotedToken)
	if beforeDemotion.Code != http.StatusOK {
		t.Fatalf("expected admin token to work before demotion, got %d: %s", beforeDemotion.Code, beforeDemotion.Body.String())
	}

	demotion := performWithToken(server, http.MethodPut, "/v1/users/"+demotedAdminID, `{"email":"demote-admin@example.com","role":"member"}`, ownerToken)
	if demotion.Code != http.StatusOK {
		t.Fatalf("expected demotion 200, got %d: %s", demotion.Code, demotion.Body.String())
	}
	staleAfterDemotion := performWithToken(server, http.MethodGet, "/v1/users", "", demotedToken)
	if staleAfterDemotion.Code != http.StatusForbidden {
		t.Fatalf("expected stale demoted admin token forbidden, got %d: %s", staleAfterDemotion.Code, staleAfterDemotion.Body.String())
	}

	deletion := performWithToken(server, http.MethodDelete, "/v1/users/"+deletedAdminID, "", ownerToken)
	if deletion.Code != http.StatusNoContent {
		t.Fatalf("expected deletion 204, got %d: %s", deletion.Code, deletion.Body.String())
	}
	staleAfterDeletion := performWithToken(server, http.MethodGet, "/v1/users", "", deletedToken)
	if staleAfterDeletion.Code != http.StatusUnauthorized {
		t.Fatalf("expected stale deleted admin token unauthorized, got %d: %s", staleAfterDeletion.Code, staleAfterDeletion.Body.String())
	}
}

func TestProjectRestoreRequiresAdminAndConfirmation(t *testing.T) {
	t.Setenv("SUPADUPA_BACKUP_DRY_RUN", "true")
	t.Setenv("SUPADUPA_RESTORE_DRY_RUN", "true")
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"owner@example.com","password":"super-secure"}`)
	ownerToken := extractString(t, bootstrap.Body.String(), "token")
	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, ownerToken)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("expected org status 201, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"restore-rbac","name":"Restore RBAC","domain":"supadupa.test","profile":"full","resource_tier":"small"}`, ownerToken)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	for _, user := range []struct {
		email string
		role  string
	}{
		{email: "restore-dev@example.com", role: "developer"},
		{email: "restore-admin@example.com", role: "admin"},
	} {
		createUserResponse := performWithToken(server, http.MethodPost, "/v1/users", `{"email":"`+user.email+`","password":"super-secure","role":"member"}`, ownerToken)
		if createUserResponse.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", user.email, createUserResponse.Code, createUserResponse.Body.String())
		}
		memberResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/members", `{"email":"`+user.email+`","role":"`+user.role+`"}`, ownerToken)
		if memberResponse.Code != http.StatusOK {
			t.Fatalf("member %s: %d %s", user.email, memberResponse.Code, memberResponse.Body.String())
		}
	}

	adminLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"restore-admin@example.com","password":"super-secure"}`)
	adminToken := extractString(t, adminLogin.Body.String(), "token")
	devLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"restore-dev@example.com","password":"super-secure"}`)
	devToken := extractString(t, devLogin.Body.String(), "token")
	backupResponse := performWithToken(server, http.MethodPost, "/v1/projects/restore-rbac/backups", "", adminToken)
	if backupResponse.Code != http.StatusCreated {
		t.Fatalf("expected admin backup create 201, got %d: %s", backupResponse.Code, backupResponse.Body.String())
	}
	backupID := extractString(t, backupResponse.Body.String(), "id")

	devRestore := performWithToken(server, http.MethodPost, "/v1/projects/restore-rbac/restore", `{"backup_id":"`+backupID+`","confirmation":"restore project restore-rbac"}`, devToken)
	if devRestore.Code != http.StatusForbidden {
		t.Fatalf("expected developer restore forbidden, got %d: %s", devRestore.Code, devRestore.Body.String())
	}
	devPITRRestore := performWithToken(server, http.MethodPost, "/v1/projects/restore-rbac/database/backups/restore-pitr", `{"recovery_time_target_unix":"1735689600","confirmation":"restore pitr project restore-rbac"}`, devToken)
	if devPITRRestore.Code != http.StatusForbidden {
		t.Fatalf("expected developer PITR restore forbidden, got %d: %s", devPITRRestore.Code, devPITRRestore.Body.String())
	}
	missingConfirmation := performWithToken(server, http.MethodPost, "/v1/projects/restore-rbac/restore", `{"backup_id":"`+backupID+`"}`, adminToken)
	if missingConfirmation.Code != http.StatusBadRequest {
		t.Fatalf("expected missing confirmation 400, got %d: %s", missingConfirmation.Code, missingConfirmation.Body.String())
	}
	wrongConfirmation := performWithToken(server, http.MethodPost, "/v1/projects/restore-rbac/database/backups/restore-pitr", `{"recovery_time_target_unix":"1735689600","confirmation":"restore project restore-rbac"}`, adminToken)
	if wrongConfirmation.Code != http.StatusBadRequest {
		t.Fatalf("expected wrong PITR confirmation 400, got %d: %s", wrongConfirmation.Code, wrongConfirmation.Body.String())
	}
	adminRestore := performWithToken(server, http.MethodPost, "/v1/projects/restore-rbac/restore", `{"backup_id":"`+backupID+`","confirmation":"restore project restore-rbac"}`, adminToken)
	if adminRestore.Code != http.StatusAccepted {
		t.Fatalf("expected admin restore accepted, got %d: %s", adminRestore.Code, adminRestore.Body.String())
	}
	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", ownerToken)
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.restore"`) || !strings.Contains(auditResponse.Body.String(), `"restore_type":"logical"`) || !strings.Contains(auditResponse.Body.String(), `"confirmation":"present"`) {
		t.Fatalf("expected restore audit metadata: %s", auditResponse.Body.String())
	}
}
