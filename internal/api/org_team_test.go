package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"supadupa2026/internal/control"
)

func TestOrgMembershipsCreateListDeleteAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	token := extractString(t, bootstrap.Body.String(), "token")
	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, token)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("expected org status 201, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")

	membersResponse := performWithToken(server, http.MethodGet, "/v1/orgs/"+orgID+"/members", "", token)
	if membersResponse.Code != http.StatusOK || !strings.Contains(membersResponse.Body.String(), `"email":"admin@example.com"`) || !strings.Contains(membersResponse.Body.String(), `"role":"owner"`) {
		t.Fatalf("expected bootstrap admin owner membership: %d %s", membersResponse.Code, membersResponse.Body.String())
	}

	upsertResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/members", `{"email":"DEV@example.com","role":"developer"}`, token)
	if upsertResponse.Code != http.StatusOK {
		t.Fatalf("expected member upsert 200, got %d: %s", upsertResponse.Code, upsertResponse.Body.String())
	}
	if !strings.Contains(upsertResponse.Body.String(), `"email":"dev@example.com"`) || !strings.Contains(upsertResponse.Body.String(), `"role":"developer"`) {
		t.Fatalf("expected normalized developer member: %s", upsertResponse.Body.String())
	}

	updateResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/members", `{"email":"dev@example.com","role":"admin"}`, token)
	if updateResponse.Code != http.StatusOK || !strings.Contains(updateResponse.Body.String(), `"role":"admin"`) {
		t.Fatalf("expected member role update: %d %s", updateResponse.Code, updateResponse.Body.String())
	}

	invalidResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/members", `{"email":"bad@example.com","role":"root"}`, token)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid role 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	deleteResponse := performWithToken(server, http.MethodDelete, "/v1/orgs/"+orgID+"/members/dev@example.com", "", token)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected member delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}

	membersResponse = performWithToken(server, http.MethodGet, "/v1/orgs/"+orgID+"/members", "", token)
	if strings.Contains(membersResponse.Body.String(), `"email":"dev@example.com"`) {
		t.Fatalf("expected dev member removed: %s", membersResponse.Body.String())
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	for _, action := range []string{"org.member_upsert", "org.member_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestOrgProjectRBACEnforced(t *testing.T) {
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

	for _, user := range []struct {
		email string
		role  string
	}{
		{email: "dev@example.com", role: "developer"},
		{email: "viewer@example.com", role: "viewer"},
	} {
		createUserResponse := performWithToken(server, http.MethodPost, "/v1/users", `{"email":"`+user.email+`","password":"super-secure","role":"admin"}`, ownerToken)
		if createUserResponse.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", user.email, createUserResponse.Code, createUserResponse.Body.String())
		}
		memberResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/members", `{"email":"`+user.email+`","role":"`+user.role+`"}`, ownerToken)
		if memberResponse.Code != http.StatusOK {
			t.Fatalf("expected member upsert for %s, got %d: %s", user.email, memberResponse.Code, memberResponse.Body.String())
		}
	}
	createOutsiderResponse := performWithToken(server, http.MethodPost, "/v1/users", `{"email":"outsider@example.com","password":"super-secure","role":"admin"}`, ownerToken)
	if createOutsiderResponse.Code != http.StatusCreated {
		t.Fatalf("create outsider: %d %s", createOutsiderResponse.Code, createOutsiderResponse.Body.String())
	}

	devLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"dev@example.com","password":"super-secure"}`)
	devToken := extractString(t, devLogin.Body.String(), "token")
	viewerLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"viewer@example.com","password":"super-secure"}`)
	viewerToken := extractString(t, viewerLogin.Body.String(), "token")
	outsiderLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"outsider@example.com","password":"super-secure"}`)
	outsiderToken := extractString(t, outsiderLogin.Body.String(), "token")

	projectBody := `{"ref":"rbac-proj","name":"RBAC","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody, ownerToken)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected owner project create 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	viewProject := performWithToken(server, http.MethodGet, "/v1/projects/rbac-proj", "", viewerToken)
	if viewProject.Code != http.StatusOK {
		t.Fatalf("expected viewer project read 200, got %d: %s", viewProject.Code, viewProject.Body.String())
	}
	viewerConfigUpdate := performWithToken(server, http.MethodPut, "/v1/projects/rbac-proj/config/auth", `{"config":{"email_enabled":"false"}}`, viewerToken)
	if viewerConfigUpdate.Code != http.StatusForbidden {
		t.Fatalf("expected viewer config update forbidden, got %d: %s", viewerConfigUpdate.Code, viewerConfigUpdate.Body.String())
	}
	viewerSecrets := performWithToken(server, http.MethodGet, "/v1/projects/rbac-proj/secrets", "", viewerToken)
	if viewerSecrets.Code != http.StatusForbidden {
		t.Fatalf("expected viewer secrets forbidden, got %d: %s", viewerSecrets.Code, viewerSecrets.Body.String())
	}

	pauseResponse := performWithToken(server, http.MethodPost, "/v1/projects/rbac-proj/pause", "", devToken)
	if pauseResponse.Code != http.StatusOK {
		t.Fatalf("expected developer pause 200, got %d: %s", pauseResponse.Code, pauseResponse.Body.String())
	}
	devDomain := performWithToken(server, http.MethodPost, "/v1/projects/rbac-proj/domains", `{"fqdn":"rbac.example.com"}`, devToken)
	if devDomain.Code != http.StatusForbidden {
		t.Fatalf("expected developer domain create forbidden, got %d: %s", devDomain.Code, devDomain.Body.String())
	}
	devDelete := performWithToken(server, http.MethodDelete, "/v1/projects/rbac-proj", "", devToken)
	if devDelete.Code != http.StatusForbidden {
		t.Fatalf("expected developer delete forbidden, got %d: %s", devDelete.Code, devDelete.Body.String())
	}

	outsiderProjects := performWithToken(server, http.MethodGet, "/v1/orgs/"+orgID+"/projects", "", outsiderToken)
	if outsiderProjects.Code != http.StatusForbidden {
		t.Fatalf("expected outsider org projects forbidden, got %d: %s", outsiderProjects.Code, outsiderProjects.Body.String())
	}

	deleteResponse := performWithToken(server, http.MethodDelete, "/v1/projects/rbac-proj", "", ownerToken)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected owner delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
}

func TestOrgFeatureFlagsAPIInheritsOverridesAndAudits(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	token := extractString(t, bootstrap.Body.String(), "token")
	defaults := performWithToken(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.example.com","stack_version":"latest","profile":"full","resource_tier":"custom","backup_schedule":"daily","feature_flags":{"billing":true,"read_replicas":true}}`, token)
	if defaults.Code != http.StatusOK {
		t.Fatalf("expected defaults update 200, got %d: %s", defaults.Code, defaults.Body.String())
	}
	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, token)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("expected org status 201, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")

	getResponse := performWithToken(server, http.MethodGet, "/v1/orgs/"+orgID+"/features", "", token)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"billing":true`) || !strings.Contains(getResponse.Body.String(), `"overrides":{}`) {
		t.Fatalf("expected inherited org flags: %d %s", getResponse.Code, getResponse.Body.String())
	}

	updateResponse := performWithToken(server, http.MethodPut, "/v1/orgs/"+orgID+"/features", `{"overrides":{"billing":false,"custom_domains":true}}`, token)
	if updateResponse.Code != http.StatusOK || !strings.Contains(updateResponse.Body.String(), `"billing":false`) || !strings.Contains(updateResponse.Body.String(), `"custom_domains":true`) {
		t.Fatalf("expected org override response: %d %s", updateResponse.Code, updateResponse.Body.String())
	}

	invalidResponse := performWithToken(server, http.MethodPut, "/v1/orgs/"+orgID+"/features", `{"overrides":{"root_mode":true}}`, token)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid feature flag 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	if !strings.Contains(auditResponse.Body.String(), `"action":"org.features_update"`) {
		t.Fatalf("expected org feature update audit event: %s", auditResponse.Body.String())
	}
}

func TestOrgFeatureFlagsGateAdvancedMutations(t *testing.T) {
	t.Setenv("SUPADUPA_CERT_ROOT", t.TempDir())
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"feature-proj","name":"Feature Project","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	disabledDomain := perform(server, http.MethodPost, "/v1/projects/feature-proj/domains", `{"fqdn":"feature.example.com"}`)
	if disabledDomain.Code != http.StatusForbidden || !strings.Contains(disabledDomain.Body.String(), "custom_domains") {
		t.Fatalf("expected custom domain feature gate 403, got %d: %s", disabledDomain.Code, disabledDomain.Body.String())
	}
	disabledSnapshot := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/usage/snapshots", "")
	if disabledSnapshot.Code != http.StatusForbidden || !strings.Contains(disabledSnapshot.Body.String(), "usage_metering") {
		t.Fatalf("expected usage metering feature gate 403, got %d: %s", disabledSnapshot.Code, disabledSnapshot.Body.String())
	}
	disabledInvoices := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/billing/invoices", "")
	if disabledInvoices.Code != http.StatusForbidden || !strings.Contains(disabledInvoices.Body.String(), "billing") {
		t.Fatalf("expected billing feature gate 403, got %d: %s", disabledInvoices.Code, disabledInvoices.Body.String())
	}

	enableOrgFeaturesForTest(t, store, orgID, "custom_domains", "usage_metering", "billing")
	enabledDomain := perform(server, http.MethodPost, "/v1/projects/feature-proj/domains", `{"fqdn":"feature.example.com"}`)
	if enabledDomain.Code != http.StatusCreated {
		t.Fatalf("expected enabled custom domain create 201, got %d: %s", enabledDomain.Code, enabledDomain.Body.String())
	}
	enabledSnapshot := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/usage/snapshots", "")
	if enabledSnapshot.Code != http.StatusCreated {
		t.Fatalf("expected enabled usage snapshot 201, got %d: %s", enabledSnapshot.Code, enabledSnapshot.Body.String())
	}
	enabledInvoices := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/billing/invoices", "")
	if enabledInvoices.Code != http.StatusOK {
		t.Fatalf("expected enabled invoice list 200, got %d: %s", enabledInvoices.Code, enabledInvoices.Body.String())
	}
}

func TestTeamProjectAccessAuthorizesProjectAdminRoute(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	token := extractString(t, bootstrap.Body.String(), "token")
	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, token)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"team-api","name":"Team API"}`, token)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project create: %d %s", projectResponse.Code, projectResponse.Body.String())
	}
	createUser := performWithToken(server, http.MethodPost, "/v1/users", `{"email":"dev@example.com","password":"super-secure","role":"member"}`, token)
	if createUser.Code != http.StatusCreated {
		t.Fatalf("expected user create: %d %s", createUser.Code, createUser.Body.String())
	}
	teamResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/teams", `{"name":"Developers","slug":"developers"}`, token)
	if teamResponse.Code != http.StatusCreated {
		t.Fatalf("expected team create: %d %s", teamResponse.Code, teamResponse.Body.String())
	}
	memberResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/teams/developers/members", `{"email":"dev@example.com"}`, token)
	if memberResponse.Code != http.StatusOK {
		t.Fatalf("expected team member add: %d %s", memberResponse.Code, memberResponse.Body.String())
	}
	login := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"dev@example.com","password":"super-secure"}`)
	devToken := extractString(t, login.Body.String(), "token")

	denied := performWithToken(server, http.MethodPut, "/v1/projects/team-api/config/auth", `{"config":{"site_url":"https://app.example.com"}}`, devToken)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("expected viewer to be denied admin config update, got %d: %s", denied.Code, denied.Body.String())
	}

	grantResponse := performWithToken(server, http.MethodPut, "/v1/projects/team-api/access", `{"subject_type":"team","subject_id":"developers","role":"admin"}`, token)
	if grantResponse.Code != http.StatusOK || !strings.Contains(grantResponse.Body.String(), `"role":"admin"`) {
		t.Fatalf("expected project access grant: %d %s", grantResponse.Code, grantResponse.Body.String())
	}
	reviewResponse := performWithToken(server, http.MethodGet, "/v1/orgs/"+orgID+"/access-review", "", token)
	if reviewResponse.Code != http.StatusOK || !strings.Contains(reviewResponse.Body.String(), `"project_ref":"team-api"`) || !strings.Contains(reviewResponse.Body.String(), `"email":"dev@example.com"`) || !strings.Contains(reviewResponse.Body.String(), `"role":"admin"`) {
		t.Fatalf("expected access review with effective admin role: %d %s", reviewResponse.Code, reviewResponse.Body.String())
	}
	allowed := performWithToken(server, http.MethodPut, "/v1/projects/team-api/config/auth", `{"config":{"site_url":"https://app.example.com"}}`, devToken)
	if allowed.Code != http.StatusOK {
		t.Fatalf("expected team grant to authorize config update, got %d: %s", allowed.Code, allowed.Body.String())
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	for _, action := range []string{"org.team_create", "org.team_member_upsert", "project.access_upsert"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectOwnerIsNotPlatformAdmin(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	adminToken := extractString(t, bootstrap.Body.String(), "token")
	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, adminToken)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"owner-api","name":"Owner API"}`, adminToken)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project create: %d %s", projectResponse.Code, projectResponse.Body.String())
	}
	createUser := performWithToken(server, http.MethodPost, "/v1/users", `{"email":"owner@example.com","password":"super-secure","role":"developer"}`, adminToken)
	if createUser.Code != http.StatusCreated {
		t.Fatalf("expected owner user create: %d %s", createUser.Code, createUser.Body.String())
	}
	memberResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/members", `{"email":"owner@example.com","role":"viewer"}`, adminToken)
	if memberResponse.Code != http.StatusOK {
		t.Fatalf("expected owner org membership: %d %s", memberResponse.Code, memberResponse.Body.String())
	}
	grantResponse := performWithToken(server, http.MethodPut, "/v1/projects/owner-api/access", `{"subject_type":"user","subject_id":"owner@example.com","role":"owner"}`, adminToken)
	if grantResponse.Code != http.StatusOK || !strings.Contains(grantResponse.Body.String(), `"role":"owner"`) {
		t.Fatalf("expected owner project grant: %d %s", grantResponse.Code, grantResponse.Body.String())
	}
	login := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"owner@example.com","password":"super-secure"}`)
	ownerToken := extractString(t, login.Body.String(), "token")

	projectAdminRoute := performWithToken(server, http.MethodPut, "/v1/projects/owner-api/config/auth", `{"config":{"site_url":"https://owner.example.com"}}`, ownerToken)
	if projectAdminRoute.Code != http.StatusOK {
		t.Fatalf("expected project owner to administer project config, got %d: %s", projectAdminRoute.Code, projectAdminRoute.Body.String())
	}
	globalAdminRoute := performWithToken(server, http.MethodGet, "/v1/users", "", ownerToken)
	if globalAdminRoute.Code != http.StatusForbidden {
		t.Fatalf("expected project owner to be denied platform admin users route, got %d: %s", globalAdminRoute.Code, globalAdminRoute.Body.String())
	}
}

func TestFleetProjectListFiltersByMembership(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	adminToken := extractString(t, bootstrap.Body.String(), "token")

	orgOne := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"One"}`, adminToken)
	orgOneID := extractString(t, orgOne.Body.String(), "id")
	orgTwo := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Two"}`, adminToken)
	orgTwoID := extractString(t, orgTwo.Body.String(), "id")

	for _, input := range []struct {
		orgID string
		ref   string
		name  string
	}{
		{orgID: orgOneID, ref: "fleet-one", name: "Fleet One"},
		{orgID: orgTwoID, ref: "fleet-two", name: "Fleet Two"},
	} {
		body := `{"ref":"` + input.ref + `","name":"` + input.name + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
		response := performWithToken(server, http.MethodPost, "/v1/orgs/"+input.orgID+"/projects", body, adminToken)
		if response.Code != http.StatusAccepted {
			t.Fatalf("create %s: %d %s", input.ref, response.Code, response.Body.String())
		}
	}

	createDev := performWithToken(server, http.MethodPost, "/v1/users", `{"email":"dev@example.com","password":"super-secure","role":"developer"}`, adminToken)
	if createDev.Code != http.StatusCreated {
		t.Fatalf("create dev: %d %s", createDev.Code, createDev.Body.String())
	}
	member := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgOneID+"/members", `{"email":"dev@example.com","role":"viewer"}`, adminToken)
	if member.Code != http.StatusOK {
		t.Fatalf("add dev member: %d %s", member.Code, member.Body.String())
	}
	devLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"dev@example.com","password":"super-secure"}`)
	devToken := extractString(t, devLogin.Body.String(), "token")

	adminProjects := performWithToken(server, http.MethodGet, "/v1/projects", "", adminToken)
	if adminProjects.Code != http.StatusOK || !strings.Contains(adminProjects.Body.String(), `"ref":"fleet-one"`) || !strings.Contains(adminProjects.Body.String(), `"ref":"fleet-two"`) {
		t.Fatalf("expected admin fleet projects: %d %s", adminProjects.Code, adminProjects.Body.String())
	}

	devProjects := performWithToken(server, http.MethodGet, "/v1/projects", "", devToken)
	if devProjects.Code != http.StatusOK {
		t.Fatalf("expected dev fleet list 200, got %d: %s", devProjects.Code, devProjects.Body.String())
	}
	if !strings.Contains(devProjects.Body.String(), `"ref":"fleet-one"`) || strings.Contains(devProjects.Body.String(), `"ref":"fleet-two"`) {
		t.Fatalf("expected membership-filtered project list: %s", devProjects.Body.String())
	}
}

func TestCreateOrgProjectAndConnect(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("expected org status 201, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")

	projectBody := `{"ref":"alpha-proj","name":"Alpha","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if !strings.Contains(projectResponse.Body.String(), `"status":"provisioning"`) {
		t.Fatalf("expected provisioning project on create: %s", projectResponse.Body.String())
	}

	poolerUpdateResponse := perform(server, http.MethodPut, "/v1/projects/alpha-proj/config/pooler", `{"config":{"dedicated_pooler_enabled":"true","dedicated_pooler_tier":"medium","pool_mode":"both"}}`)
	if poolerUpdateResponse.Code != http.StatusOK {
		t.Fatalf("expected pooler config update 200, got %d: %s", poolerUpdateResponse.Code, poolerUpdateResponse.Body.String())
	}

	connectResponse := perform(server, http.MethodGet, "/v1/projects/alpha-proj/connect", "")
	if connectResponse.Code != http.StatusOK {
		t.Fatalf("expected connect status 200, got %d: %s", connectResponse.Code, connectResponse.Body.String())
	}
	if !strings.Contains(connectResponse.Body.String(), "https://alpha-proj.supadupa.test") {
		t.Fatalf("expected project URL in connect payload: %s", connectResponse.Body.String())
	}
	for _, expected := range []string{
		`"publishable":"secret://projects/alpha-proj/publishable_key"`,
		`"service_role":"secret://projects/alpha-proj/service_role"`,
		`"signing_key_current":"secret://projects/alpha-proj/jwt_signing_key_current"`,
		`"jwt_signing_keys"`,
		`"kind":"jwt_signing_key_current"`,
		`"status":"current"`,
		`"alg":"EdDSA"`,
		`"direct":"postgres://postgres:${DB_PASSWORD}@db.alpha-proj.internal:5432/postgres?sslmode=require"`,
		`"transaction":"postgres://postgres.alpha-proj:${DB_PASSWORD}@pooler.alpha-proj.internal:6543/postgres?sslmode=require"`,
		`"session":"postgres://postgres.alpha-proj:${DB_PASSWORD}@pooler.alpha-proj.internal:5432/postgres?sslmode=require"`,
		`"pooler":{"dedicated":"true","dedicated_tier":"medium","default_pool_size":"20","max_client_connections":"200","pool_mode":"both","session_port":"5432","transaction_port":"6543"}`,
		`"password_handle":"secret://projects/alpha-proj/db_password"`,
		`"sslmode":"require"`,
		`"public_direct":{"database":"postgres","host":"db-alpha-proj.supadupa.test","password_handle":"secret://projects/alpha-proj/db_password","port":"5432","sslmode":"require","user":"postgres"}`,
		`"public_transaction":{"database":"postgres","host":"pooler-alpha-proj.supadupa.test","password_handle":"secret://projects/alpha-proj/db_password","port":"6543","sslmode":"require","user":"postgres.alpha-proj"}`,
		`"public_session":{"database":"postgres","host":"pooler-alpha-proj.supadupa.test","password_handle":"secret://projects/alpha-proj/db_password","port":"5432","sslmode":"require","user":"postgres.alpha-proj"}`,
		`"auth_url":"https://alpha-proj.supadupa.test/auth/v1"`,
		`"storage_url":"https://alpha-proj.supadupa.test/storage/v1"`,
		`"s3_endpoint":"https://storage-alpha-proj.supadupa.test/storage/v1/s3"`,
		`"access_key_handle":"secret://projects/alpha-proj/s3_access_key"`,
		`"api":"https://alpha-proj.supadupa.test"`,
		`"studio_url":"https://studio-alpha-proj.supadupa.test"`,
		`"studio":"https://studio-alpha-proj.supadupa.test"`,
		`"rest_docs":"https://studio-alpha-proj.supadupa.test/project/alpha-proj/api"`,
		`"graphql_explorer":"https://studio-alpha-proj.supadupa.test/project/alpha-proj/api?panel=graphql"`,
		`"storage_s3":"https://storage-alpha-proj.supadupa.test/storage/v1/s3"`,
		`"functions_service":"https://alpha-proj.supadupa.test/functions/v1"`,
		`"realtime_service":"https://alpha-proj.supadupa.test/realtime/v1"`,
		`"connection_snippets"`,
		`"psql_direct":"psql postgres://postgres:${DB_PASSWORD}@db-alpha-proj.supadupa.test:5432/postgres?sslmode=require"`,
		`"psql_pool_transaction":"psql postgres://postgres.alpha-proj:${DB_PASSWORD}@pooler-alpha-proj.supadupa.test:6543/postgres?sslmode=require"`,
		`"psql_internal_pool_transaction":"psql postgres://postgres.alpha-proj:${DB_PASSWORD}@pooler.alpha-proj.internal:6543/postgres?sslmode=require"`,
		`"env_publishable_key":"SUPABASE_PUBLISHABLE_KEY=secret://projects/alpha-proj/publishable_key"`,
		`"flutter":"Supabase.initialize`,
		`"swift":"SupabaseClient`,
	} {
		if !strings.Contains(connectResponse.Body.String(), expected) {
			t.Fatalf("expected connect payload value %s: %s", expected, connectResponse.Body.String())
		}
	}

	databaseSSLResponse := perform(server, http.MethodPut, "/v1/projects/alpha-proj/config/database", `{"config":{"ssl_enforced":"false"}}`)
	if databaseSSLResponse.Code != http.StatusOK {
		t.Fatalf("expected database ssl config update 200, got %d: %s", databaseSSLResponse.Code, databaseSSLResponse.Body.String())
	}
	connectWithoutSSLResponse := perform(server, http.MethodGet, "/v1/projects/alpha-proj/connect", "")
	if connectWithoutSSLResponse.Code != http.StatusOK {
		t.Fatalf("expected connect without enforced ssl status 200, got %d: %s", connectWithoutSSLResponse.Code, connectWithoutSSLResponse.Body.String())
	}
	if !strings.Contains(connectWithoutSSLResponse.Body.String(), `"direct":"postgres://postgres:${DB_PASSWORD}@db.alpha-proj.internal:5432/postgres?sslmode=prefer"`) || !strings.Contains(connectWithoutSSLResponse.Body.String(), `"sslmode":"prefer"`) {
		t.Fatalf("expected preferred sslmode when DB SSL is optional: %s", connectWithoutSSLResponse.Body.String())
	}

	cliProfileResponse := perform(server, http.MethodGet, "/v1/projects/alpha-proj/connect/cli", "")
	if cliProfileResponse.Code != http.StatusOK {
		t.Fatalf("expected cli profile status 200, got %d: %s", cliProfileResponse.Code, cliProfileResponse.Body.String())
	}
	for _, expected := range []string{
		`"project_ref":"alpha-proj"`,
		`"SUPABASE_URL":"https://alpha-proj.supadupa.test"`,
		`"SUPABASE_SERVICE_ROLE_KEY":"secret://projects/alpha-proj/service_role"`,
		`"database_url":"postgres://postgres:${DB_PASSWORD}@db-alpha-proj.supadupa.test:5432/postgres?sslmode=require"`,
		`"internal_database_url":"postgres://postgres:${DB_PASSWORD}@db.alpha-proj.internal:5432/postgres?sslmode=prefer"`,
		`"pooler_transaction_url":"postgres://postgres.alpha-proj:${DB_PASSWORD}@pooler-alpha-proj.supadupa.test:6543/postgres?sslmode=require"`,
		`"supadupa_gen_types":"supadupa-cli projects gen-types --ref alpha-proj --out database.types.ts"`,
		`"supabase_config_toml"`,
		`"control_plane":"Use supadupa Management API`,
		`"typegen":"Use supadupa-cli projects gen-types`,
	} {
		if !strings.Contains(cliProfileResponse.Body.String(), expected) {
			t.Fatalf("expected cli profile value %s: %s", expected, cliProfileResponse.Body.String())
		}
	}

	routesResponse := perform(server, http.MethodGet, "/v1/projects/alpha-proj/routes", "")
	if routesResponse.Code != http.StatusOK {
		t.Fatalf("expected routes status 200, got %d: %s", routesResponse.Code, routesResponse.Body.String())
	}
	if !strings.Contains(routesResponse.Body.String(), `"fqdn":"alpha-proj.supadupa.test"`) {
		t.Fatalf("expected api route in response: %s", routesResponse.Body.String())
	}
	if !strings.Contains(routesResponse.Body.String(), `"fqdn":"studio-alpha-proj.supadupa.test"`) {
		t.Fatalf("expected studio route in response: %s", routesResponse.Body.String())
	}
	if !strings.Contains(routesResponse.Body.String(), `"fqdn":"storage-alpha-proj.supadupa.test"`) {
		t.Fatalf("expected storage route in response: %s", routesResponse.Body.String())
	}

	enableDatabaseExposure(t, server, "alpha-proj", "public", "")
	routeManifestResponse := perform(server, http.MethodGet, "/v1/projects/alpha-proj/route-manifest", "")
	if routeManifestResponse.Code != http.StatusOK {
		t.Fatalf("expected route manifest status 200, got %d: %s", routeManifestResponse.Code, routeManifestResponse.Body.String())
	}
	for _, expected := range []string{
		`"project_ref":"alpha-proj"`,
		`"http_routes"`,
		`"fqdn":"storage-alpha-proj.supadupa.test"`,
		`"tcp_routes"`,
		`"name":"db"`,
		`"fqdn":"db-alpha-proj.supadupa.test"`,
		`"entrypoint":"postgres"`,
		`"public_port":5432`,
		`"upstream_address":"alpha-proj-db:5432"`,
		`"name":"pooler-transaction"`,
		`"fqdn":"pooler-alpha-proj.supadupa.test"`,
		`"entrypoint":"pooler"`,
		`"public_port":6543`,
		`"upstream_address":"alpha-proj-pooler:6543"`,
		`"name":"pooler-session"`,
		`"upstream_address":"alpha-proj-pooler:5432"`,
	} {
		if !strings.Contains(routeManifestResponse.Body.String(), expected) {
			t.Fatalf("expected route manifest value %s: %s", expected, routeManifestResponse.Body.String())
		}
	}
}

func TestListOrgs(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	_ = perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)

	response := perform(server, http.MethodGet, "/v1/orgs", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"name":"Platform"`) {
		t.Fatalf("expected org in response: %s", response.Body.String())
	}
}

func TestOrgLifecycleAndDeleteProtection(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")

	getResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID, "")
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"name":"Platform"`) {
		t.Fatalf("expected org get response: %d %s", getResponse.Code, getResponse.Body.String())
	}
	updateResponse := perform(server, http.MethodPut, "/v1/orgs/"+orgID, `{"name":"Renamed"}`)
	if updateResponse.Code != http.StatusOK || !strings.Contains(updateResponse.Body.String(), `"name":"Renamed"`) {
		t.Fatalf("expected org update response: %d %s", updateResponse.Code, updateResponse.Body.String())
	}

	projectBody := `{"ref":"org-owned","name":"Org Owned","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	blockedDelete := perform(server, http.MethodDelete, "/v1/orgs/"+orgID, "")
	if blockedDelete.Code != http.StatusConflict {
		t.Fatalf("expected org delete conflict, got %d: %s", blockedDelete.Code, blockedDelete.Body.String())
	}
	deleteProject := perform(server, http.MethodDelete, "/v1/projects/org-owned", "")
	if deleteProject.Code != http.StatusNoContent {
		t.Fatalf("expected project delete 204, got %d: %s", deleteProject.Code, deleteProject.Body.String())
	}
	deleteOrg := perform(server, http.MethodDelete, "/v1/orgs/"+orgID, "")
	if deleteOrg.Code != http.StatusNoContent {
		t.Fatalf("expected org delete 204, got %d: %s", deleteOrg.Code, deleteOrg.Body.String())
	}
	missingOrg := perform(server, http.MethodGet, "/v1/orgs/"+orgID, "")
	if missingOrg.Code != http.StatusNotFound {
		t.Fatalf("expected deleted org 404, got %d: %s", missingOrg.Code, missingOrg.Body.String())
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"org.update", "org.delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestOrgQuotasTrackUsageAndRejectProjectOverLimit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")

	defaultQuotaResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/quotas", "")
	if defaultQuotaResponse.Code != http.StatusOK || !strings.Contains(defaultQuotaResponse.Body.String(), `"max_projects":0`) {
		t.Fatalf("expected default unlimited quota: %d %s", defaultQuotaResponse.Code, defaultQuotaResponse.Body.String())
	}

	updateQuotaResponse := perform(server, http.MethodPut, "/v1/orgs/"+orgID+"/quotas", `{"max_projects":1,"max_cpu":2,"max_ram_mb":4096,"max_disk_gb":40}`)
	if updateQuotaResponse.Code != http.StatusOK {
		t.Fatalf("expected quota update 200, got %d: %s", updateQuotaResponse.Code, updateQuotaResponse.Body.String())
	}

	firstProject := `{"ref":"quota-one","name":"Quota One","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	firstResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", firstProject)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("expected first project status 202, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}

	quotaResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/quotas", "")
	for _, expected := range []string{`"max_projects":1`, `"cpu":2`, `"ram_mb":4096`, `"disk_gb":40`, `"projects":1`} {
		if !strings.Contains(quotaResponse.Body.String(), expected) {
			t.Fatalf("expected quota usage %s: %s", expected, quotaResponse.Body.String())
		}
	}

	secondProject := `{"ref":"quota-two","name":"Quota Two","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	secondResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", secondProject)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("expected quota conflict, got %d: %s", secondResponse.Code, secondResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/quota-one", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	quotaResponse = perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/quotas", "")
	if !strings.Contains(quotaResponse.Body.String(), `"projects":0`) {
		t.Fatalf("expected quota usage decrement: %s", quotaResponse.Body.String())
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"org.quota_update"`) {
		t.Fatalf("expected quota update audit event: %s", auditResponse.Body.String())
	}
}

func TestOrgUsageMeteringTracksControlPlaneResources(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "custom_domains", "log_drains")

	firstProject := `{"ref":"meter-one","name":"Meter One","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	firstResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", firstProject)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("expected first project status 202, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
	secondProject := `{"ref":"meter-two","name":"Meter Two","domain":"supadupa.test","profile":"full","resource_tier":"medium"}`
	secondResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", secondProject)
	if secondResponse.Code != http.StatusAccepted {
		t.Fatalf("expected second project status 202, got %d: %s", secondResponse.Code, secondResponse.Body.String())
	}

	if _, err := store.CreateBackup(context.Background(), control.BackupInput{ProjectRef: "meter-one", Kind: "logical", Location: "memory://meter-one", SizeBytes: 1048576, Status: "completed"}); err != nil {
		t.Fatalf("create metered backup: %v", err)
	}
	domainResponse := perform(server, http.MethodPost, "/v1/projects/meter-one/domains", `{"fqdn":"meter.example.com"}`)
	if domainResponse.Code != http.StatusCreated {
		t.Fatalf("expected domain status 201, got %d: %s", domainResponse.Code, domainResponse.Body.String())
	}
	drainResponse := perform(server, http.MethodPost, "/v1/projects/meter-one/log-drains", `{"target":"https","config":{"url":"https://logs.example.com/ingest"}}`)
	if drainResponse.Code != http.StatusCreated {
		t.Fatalf("expected drain status 201, got %d: %s", drainResponse.Code, drainResponse.Body.String())
	}

	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if usageResponse.Code != http.StatusOK {
		t.Fatalf("expected usage status 200, got %d: %s", usageResponse.Code, usageResponse.Body.String())
	}
	for _, expected := range []string{
		`"cpu":6`,
		`"ram_mb":12288`,
		`"disk_gb":120`,
		`"projects":2`,
		`"healthy":2`,
		`"backup_count":1`,
		`"backup_storage_bytes":1048576`,
		`"custom_domains":1`,
		`"log_drains":1`,
		`"secrets":20`,
		`"db_allocated_bytes":128849018880`,
	} {
		if !strings.Contains(usageResponse.Body.String(), expected) {
			t.Fatalf("expected metering value %s: %s", expected, usageResponse.Body.String())
		}
	}
}

func TestOrgUsageSnapshotsCaptureMeteringLedger(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "usage_metering")
	project := `{"ref":"snap-one","name":"Snap One","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", project)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	firstResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/usage/snapshots", "")
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("expected first snapshot 201, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
	if !strings.Contains(firstResponse.Body.String(), `"metrics"`) || !strings.Contains(firstResponse.Body.String(), `"projects":1`) {
		t.Fatalf("expected captured usage metrics: %s", firstResponse.Body.String())
	}
	secondResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/usage/snapshots", "")
	if secondResponse.Code != http.StatusCreated {
		t.Fatalf("expected second snapshot 201, got %d: %s", secondResponse.Code, secondResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage/snapshots?limit=1", "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected snapshot list 200, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	if strings.Count(listResponse.Body.String(), `"metrics"`) != 1 {
		t.Fatalf("expected limited snapshot list: %s", listResponse.Body.String())
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"org.usage_snapshot_create"`) {
		t.Fatalf("expected usage snapshot audit event: %s", auditResponse.Body.String())
	}
}

func TestBillingInvoicesGenerateFromUsageSnapshots(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "usage_metering", "billing")
	project := `{"ref":"bill-one","name":"Bill One","domain":"supadupa.test","profile":"full","resource_tier":"medium"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", project)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	snapshotResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/usage/snapshots", "")
	if snapshotResponse.Code != http.StatusCreated {
		t.Fatalf("expected snapshot status 201, got %d: %s", snapshotResponse.Code, snapshotResponse.Body.String())
	}
	snapshotID := extractString(t, snapshotResponse.Body.String(), "id")

	createResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/billing/invoices", `{"usage_snapshot_id":"`+snapshotID+`","currency":"USD","status":"draft","due_days":14}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected invoice create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"number":"SDP-`, `"usage_snapshot_id":"` + snapshotID + `"`, `"currency":"USD"`, `"total_cents":`, `"line_items":[`, `"key":"projects"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected invoice value %s: %s", expected, createResponse.Body.String())
		}
	}
	invoiceID := extractString(t, createResponse.Body.String(), "id")
	getResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/billing/invoices/"+invoiceID, "")
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"id":"`+invoiceID+`"`) {
		t.Fatalf("expected invoice get 200: %d %s", getResponse.Code, getResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/billing/invoices?limit=1", "")
	if listResponse.Code != http.StatusOK || strings.Count(listResponse.Body.String(), `"number"`) != 1 {
		t.Fatalf("expected limited invoice list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"org.billing_invoice_create"`) {
		t.Fatalf("expected billing invoice audit event: %s", auditResponse.Body.String())
	}
}
