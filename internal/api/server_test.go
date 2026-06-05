package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"supadupa2026/internal/control"
	composeprovisioner "supadupa2026/internal/provisioner/compose"
)

func TestHealth(t *testing.T) {
	server := NewServer(Config{Provisioner: composeprovisioner.New()})
	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
}

func TestCORSOrigins(t *testing.T) {
	server := NewServer(Config{Provisioner: composeprovisioner.New()})
	for _, origin := range []string{"http://127.0.0.1:3001", "http://127.0.0.1:5174"} {
		request := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()

		server.Handler.ServeHTTP(response, request)

		if response.Code != http.StatusNoContent {
			t.Fatalf("expected preflight status %d, got %d", http.StatusNoContent, response.Code)
		}
		if got := response.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("expected default local admin origin %s to be allowed, got %q", origin, got)
		}
		if got := response.Header().Get("Vary"); got != "Origin" {
			t.Fatalf("expected Vary Origin header, got %q", got)
		}
	}
}

func TestConfiguredCORSOriginsOverrideDefaults(t *testing.T) {
	server := NewServer(Config{
		Provisioner: composeprovisioner.New(),
		CORSOrigins: []string{"https://admin.example.com"},
	})

	allowedRequest := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
	allowedRequest.Header.Set("Origin", "https://admin.example.com")
	allowedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(allowedResponse, allowedRequest)
	if got := allowedResponse.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("expected configured origin to be allowed, got %q", got)
	}

	defaultRequest := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
	defaultRequest.Header.Set("Origin", "http://localhost:3000")
	defaultResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(defaultResponse, defaultRequest)
	if got := defaultResponse.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected configured origins to override defaults, got %q", got)
	}
}

func TestAuthBootstrapLoginAndProtectedAPI(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	unauthorized := perform(server, http.MethodGet, "/v1/orgs", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	initialState := perform(server, http.MethodGet, "/v1/auth/state", "")
	if initialState.Code != http.StatusOK || !strings.Contains(initialState.Body.String(), `"bootstrapped":false`) {
		t.Fatalf("expected public unbootstrapped auth state, got %d: %s", initialState.Code, initialState.Body.String())
	}

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	token := extractString(t, bootstrap.Body.String(), "token")

	bootstrappedState := perform(server, http.MethodGet, "/v1/auth/state", "")
	if bootstrappedState.Code != http.StatusOK || !strings.Contains(bootstrappedState.Body.String(), `"bootstrapped":true`) {
		t.Fatalf("expected public bootstrapped auth state, got %d: %s", bootstrappedState.Code, bootstrappedState.Body.String())
	}

	authorized := performWithToken(server, http.MethodGet, "/v1/orgs", "", token)
	if authorized.Code != http.StatusOK {
		t.Fatalf("expected authorized status 200, got %d: %s", authorized.Code, authorized.Body.String())
	}

	login := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`)
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `"token"`) {
		t.Fatalf("expected login token, got %d: %s", login.Code, login.Body.String())
	}
}

func TestAuthUsesPlatformSecretKeyFallback(t *testing.T) {
	t.Setenv(control.AuthSecretEnv, "")
	t.Setenv(control.PlatformSecretKeyEnv, "stable-platform-secret")
	server := NewServer(Config{
		Store:        control.NewMemoryStore(),
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	token := extractString(t, bootstrap.Body.String(), "token")
	if _, err := control.NewAuthService("stable-platform-secret").Verify(token); err != nil {
		t.Fatalf("expected token to verify with platform secret fallback: %v", err)
	}
	if _, err := control.NewAuthService("dev-only-change-me").Verify(token); err == nil {
		t.Fatal("expected token not to verify with development default")
	}
}

func TestPlatformSAMLSSOSettingsAndCallback(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	token := extractString(t, bootstrap.Body.String(), "token")

	startDisabled := perform(server, http.MethodGet, "/v1/auth/sso/saml/start", "")
	if startDisabled.Code != http.StatusNotFound {
		t.Fatalf("expected disabled sso start 404, got %d: %s", startDisabled.Code, startDisabled.Body.String())
	}

	privateKey, certificate := testSAMLSigningCertificate(t)
	configBody := mustJSON(t, map[string]any{
		"enabled":         true,
		"idp_entity_id":   "https://idp.example.com/saml",
		"sso_url":         "https://idp.example.com/login",
		"certificate_pem": certificate,
		"acs_url":         "https://supadupa.example.com/v1/auth/sso/saml/callback",
		"metadata_url":    "https://idp.example.com/metadata",
		"email_domain":    "example.com",
		"auto_provision":  true,
		"default_role":    "developer",
	})
	update := performWithToken(server, http.MethodPut, "/v1/settings/sso", configBody, token)
	if update.Code != http.StatusOK {
		t.Fatalf("expected sso settings update 200, got %d: %s", update.Code, update.Body.String())
	}
	for _, expected := range []string{`"enabled":true`, `"provider":"saml"`, `"idp_entity_id":"https://idp.example.com/saml"`, `"auto_provision":true`} {
		if !strings.Contains(update.Body.String(), expected) {
			t.Fatalf("expected sso config value %s: %s", expected, update.Body.String())
		}
	}

	start := perform(server, http.MethodGet, "/v1/auth/sso/saml/start", "")
	if start.Code != http.StatusOK || !strings.Contains(start.Body.String(), `"login_url":"https://idp.example.com/login"`) || !strings.Contains(start.Body.String(), `"acs_url":"https://supadupa.example.com/v1/auth/sso/saml/callback"`) {
		t.Fatalf("expected sso initiation metadata: %d %s", start.Code, start.Body.String())
	}

	assertion := control.PlatformSSOAssertion{
		Issuer:       "https://idp.example.com/saml",
		Audience:     "https://supadupa.example.com/v1/auth/sso/saml/callback",
		Email:        "Engineer@Example.com",
		NameID:       "idp-user-123",
		Role:         "viewer",
		NotOnOrAfter: time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second),
	}
	assertion.Signature = signSAMLAssertion(t, privateKey, assertion)
	callback := perform(server, http.MethodPost, "/v1/auth/sso/saml/callback", mustJSON(t, assertion))
	if callback.Code != http.StatusOK || !strings.Contains(callback.Body.String(), `"token"`) || !strings.Contains(callback.Body.String(), `"email":"engineer@example.com"`) || !strings.Contains(callback.Body.String(), `"role":"viewer"`) {
		t.Fatalf("expected sso callback token and provisioned user: %d %s", callback.Code, callback.Body.String())
	}

	badAssertion := assertion
	badAssertion.Email = "engineer@other.test"
	badAssertion.Signature = signSAMLAssertion(t, privateKey, badAssertion)
	badCallback := perform(server, http.MethodPost, "/v1/auth/sso/saml/callback", mustJSON(t, badAssertion))
	if badCallback.Code != http.StatusUnauthorized || !strings.Contains(badCallback.Body.String(), "outside the allowed domain") {
		t.Fatalf("expected bad domain rejection: %d %s", badCallback.Code, badCallback.Body.String())
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	for _, action := range []string{"settings.sso_update", "user.sso_login"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestPlatformMFAEnrollmentLoginAndDisable(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	token := extractString(t, bootstrap.Body.String(), "token")

	status := performWithToken(server, http.MethodGet, "/v1/account/mfa", "", token)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"enabled":false`) {
		t.Fatalf("expected disabled mfa status: %d %s", status.Code, status.Body.String())
	}

	enroll := performWithToken(server, http.MethodPost, "/v1/account/mfa/enroll", "", token)
	if enroll.Code != http.StatusCreated || !strings.Contains(enroll.Body.String(), `"pending":true`) || !strings.Contains(enroll.Body.String(), `otpauth://totp/`) {
		t.Fatalf("expected mfa enrollment: %d %s", enroll.Code, enroll.Body.String())
	}
	secret := extractString(t, enroll.Body.String(), "secret")
	code, err := control.TOTPCode(secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("expected totp code: %v", err)
	}

	badVerify := performWithToken(server, http.MethodPost, "/v1/account/mfa/verify", `{"code":"000000"}`, token)
	if badVerify.Code != http.StatusBadRequest {
		t.Fatalf("expected bad mfa verify 400, got %d: %s", badVerify.Code, badVerify.Body.String())
	}

	verify := performWithToken(server, http.MethodPost, "/v1/account/mfa/verify", `{"code":"`+code+`"}`, token)
	if verify.Code != http.StatusOK || !strings.Contains(verify.Body.String(), `"enabled":true`) || !strings.Contains(verify.Body.String(), `"pending":false`) {
		t.Fatalf("expected mfa enabled: %d %s", verify.Code, verify.Body.String())
	}

	challenge := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`)
	if challenge.Code != http.StatusAccepted || !strings.Contains(challenge.Body.String(), `"mfa_required":true`) || strings.Contains(challenge.Body.String(), `"token"`) {
		t.Fatalf("expected mfa login challenge without token: %d %s", challenge.Code, challenge.Body.String())
	}

	loginCode, err := control.TOTPCode(secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("expected login totp code: %v", err)
	}
	login := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure","totp_code":"`+loginCode+`"}`)
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `"token"`) {
		t.Fatalf("expected login with mfa token: %d %s", login.Code, login.Body.String())
	}

	disable := performWithToken(server, http.MethodDelete, "/v1/account/mfa", `{"code":"`+loginCode+`"}`, token)
	if disable.Code != http.StatusOK || !strings.Contains(disable.Body.String(), `"enabled":false`) {
		t.Fatalf("expected mfa disabled: %d %s", disable.Code, disable.Body.String())
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	for _, action := range []string{"user.mfa_enroll", "user.mfa_verify", "user.mfa_disable"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestListUsersProtectedAndSanitized(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	token := extractString(t, bootstrap.Body.String(), "token")

	unauthorized := perform(server, http.MethodGet, "/v1/users", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	createUser := performWithToken(server, http.MethodPost, "/v1/users", `{"email":"dev@example.com","password":"super-secure","role":"developer"}`, token)
	if createUser.Code != http.StatusCreated {
		t.Fatalf("expected user create status 201, got %d: %s", createUser.Code, createUser.Body.String())
	}

	users := performWithToken(server, http.MethodGet, "/v1/users", "", token)
	if users.Code != http.StatusOK {
		t.Fatalf("expected users status 200, got %d: %s", users.Code, users.Body.String())
	}
	body := users.Body.String()
	if !strings.Contains(body, `"email":"admin@example.com"`) || !strings.Contains(body, `"email":"dev@example.com"`) {
		t.Fatalf("expected listed users: %s", body)
	}
	if strings.Contains(body, "password") || strings.Contains(body, "PasswordHash") {
		t.Fatalf("expected sanitized user payload: %s", body)
	}
	if strings.Index(body, "admin@example.com") > strings.Index(body, "dev@example.com") {
		t.Fatalf("expected users sorted by email: %s", body)
	}
}

func TestSCIMUsersAndGroupsProvisioning(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	unauthorized := perform(server, http.MethodGet, "/v1/scim/v2/Users", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized SCIM status, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	token := extractString(t, bootstrap.Body.String(), "token")
	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, token)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("expected org status 201, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")

	configResponse := performWithToken(server, http.MethodGet, "/v1/scim/v2/ServiceProviderConfig", "", token)
	if configResponse.Code != http.StatusOK || !strings.Contains(configResponse.Body.String(), `"patch":{"supported":true}`) {
		t.Fatalf("expected SCIM service provider config: %d %s", configResponse.Code, configResponse.Body.String())
	}

	createUserBody := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User","urn:supadupa:params:scim:schemas:extension:User"],"userName":"Dev@Example.com","active":true,"urn:supadupa:params:scim:schemas:extension:User":{"role":"developer"}}`
	createUser := performWithToken(server, http.MethodPost, "/v1/scim/v2/Users", createUserBody, token)
	if createUser.Code != http.StatusCreated {
		t.Fatalf("expected SCIM user create 201, got %d: %s", createUser.Code, createUser.Body.String())
	}
	userID := extractString(t, createUser.Body.String(), "id")
	for _, expected := range []string{`"userName":"dev@example.com"`, `"active":true`, `"role":"developer"`, `"resourceType":"User"`} {
		if !strings.Contains(createUser.Body.String(), expected) {
			t.Fatalf("expected SCIM user value %s: %s", expected, createUser.Body.String())
		}
	}

	replaceUserBody := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User","urn:supadupa:params:scim:schemas:extension:User"],"userName":"engineer@example.com","active":true,"urn:supadupa:params:scim:schemas:extension:User":{"role":"admin"}}`
	replaceUser := performWithToken(server, http.MethodPut, "/v1/scim/v2/Users/"+userID, replaceUserBody, token)
	if replaceUser.Code != http.StatusOK || !strings.Contains(replaceUser.Body.String(), `"userName":"engineer@example.com"`) || !strings.Contains(replaceUser.Body.String(), `"role":"admin"`) {
		t.Fatalf("expected SCIM user replace: %d %s", replaceUser.Code, replaceUser.Body.String())
	}

	listUsers := performWithToken(server, http.MethodGet, "/v1/scim/v2/Users", "", token)
	if listUsers.Code != http.StatusOK || !strings.Contains(listUsers.Body.String(), `"totalResults":2`) || !strings.Contains(listUsers.Body.String(), `"userName":"engineer@example.com"`) {
		t.Fatalf("expected SCIM user list: %d %s", listUsers.Code, listUsers.Body.String())
	}

	groupBody := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group","urn:supadupa:params:scim:schemas:extension:Group"],"externalId":"` + orgID + `","displayName":"Platform Engineers","members":[{"value":"` + userID + `"}]}`
	createGroup := performWithToken(server, http.MethodPost, "/v1/scim/v2/Groups", groupBody, token)
	if createGroup.Code != http.StatusCreated {
		t.Fatalf("expected SCIM group create 201, got %d: %s", createGroup.Code, createGroup.Body.String())
	}
	groupID := extractString(t, createGroup.Body.String(), "id")
	for _, expected := range []string{`"displayName":"Platform Engineers"`, `"display":"engineer@example.com"`, `"org_id":"` + orgID + `"`} {
		if !strings.Contains(createGroup.Body.String(), expected) {
			t.Fatalf("expected SCIM group value %s: %s", expected, createGroup.Body.String())
		}
	}

	listGroups := performWithToken(server, http.MethodGet, "/v1/scim/v2/Groups?org_id="+orgID, "", token)
	if listGroups.Code != http.StatusOK || !strings.Contains(listGroups.Body.String(), `"totalResults":1`) || !strings.Contains(listGroups.Body.String(), `"displayName":"Platform Engineers"`) {
		t.Fatalf("expected SCIM group list: %d %s", listGroups.Code, listGroups.Body.String())
	}

	patchInactive := performWithToken(server, http.MethodPatch, "/v1/scim/v2/Users/"+userID, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"active","value":false}]}`, token)
	if patchInactive.Code != http.StatusNoContent {
		t.Fatalf("expected SCIM user deprovision 204, got %d: %s", patchInactive.Code, patchInactive.Body.String())
	}
	getDeletedUser := performWithToken(server, http.MethodGet, "/v1/scim/v2/Users/"+userID, "", token)
	if getDeletedUser.Code != http.StatusNotFound {
		t.Fatalf("expected deleted SCIM user 404, got %d: %s", getDeletedUser.Code, getDeletedUser.Body.String())
	}
	getGroup := performWithToken(server, http.MethodGet, "/v1/scim/v2/Groups/"+groupID, "", token)
	if getGroup.Code != http.StatusOK || strings.Contains(getGroup.Body.String(), "engineer@example.com") {
		t.Fatalf("expected SCIM deprovision to remove team membership: %d %s", getGroup.Code, getGroup.Body.String())
	}

	deleteGroup := performWithToken(server, http.MethodDelete, "/v1/scim/v2/Groups/"+groupID, "", token)
	if deleteGroup.Code != http.StatusNoContent {
		t.Fatalf("expected SCIM group delete 204, got %d: %s", deleteGroup.Code, deleteGroup.Body.String())
	}
	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	for _, action := range []string{"scim.user_create", "scim.user_replace", "scim.user_deprovision", "scim.group_create", "scim.group_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected SCIM audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestPlatformDefaultsAPIUpdatesAndAppliesToProjectCreate(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{
		Store:        store,
		Provisioner:  provisioner,
		AuthRequired: true,
	})

	unauthorized := perform(server, http.MethodGet, "/v1/settings/defaults", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized settings status, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	token := extractString(t, bootstrap.Body.String(), "token")

	update := performWithToken(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.example.com","stack_version":"2026.06.05","profile":"essential","resource_tier":"medium","backup_schedule":"hourly","feature_flags":{"single_org_mode":false,"read_replicas":true,"kubernetes_operator":true},"smtp":{"enabled":true,"host":"smtp.example.com","port":2525,"sender_name":"supadupa","sender_email":"noreply@example.com","username":"apikey","password_handle":"secret://platform/smtp-password","tls_mode":"implicit"}}`, token)
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"domain":"apps.example.com"`) || !strings.Contains(update.Body.String(), `"backup_schedule":"hourly"`) || !strings.Contains(update.Body.String(), `"host":"smtp.example.com"`) || !strings.Contains(update.Body.String(), `"password_handle":"secret://platform/smtp-password"`) || !strings.Contains(update.Body.String(), `"single_org_mode":false`) || !strings.Contains(update.Body.String(), `"read_replicas":true`) || !strings.Contains(update.Body.String(), `"kubernetes_operator":true`) || !strings.Contains(update.Body.String(), `"supabase_cli_compat":true`) {
		t.Fatalf("expected updated defaults: %d %s", update.Code, update.Body.String())
	}
	invalidSMTP := performWithToken(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.example.com","stack_version":"2026.06.05","profile":"essential","resource_tier":"medium","backup_schedule":"hourly","smtp":{"enabled":true,"host":"smtp.example.com","port":587,"password_handle":"raw","tls_mode":"starttls"}}`, token)
	if invalidSMTP.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid smtp defaults 400, got %d: %s", invalidSMTP.Code, invalidSMTP.Body.String())
	}

	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, token)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("expected org status 201, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"defaults-api","name":"Defaults API"}`, token)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if provisioner.spec.Domain != "apps.example.com" || provisioner.spec.StackVersion != "2026.06.05" || provisioner.spec.Profile != control.StackProfileEssential || provisioner.spec.ResourceTier != control.ResourceTierMedium {
		t.Fatalf("expected provisioner spec from defaults, got %#v", provisioner.spec)
	}
	policy, err := store.GetBackupPolicy(context.Background(), "defaults-api")
	if err != nil {
		t.Fatal(err)
	}
	if policy.Schedule != "hourly" {
		t.Fatalf("expected hourly backup policy, got %#v", policy)
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	if !strings.Contains(auditResponse.Body.String(), `"action":"settings.defaults_update"`) {
		t.Fatalf("expected settings audit event: %s", auditResponse.Body.String())
	}
}

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
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected owner project create 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	defaults := performWithToken(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.example.com","stack_version":"latest","profile":"full","resource_tier":"small","backup_schedule":"daily","feature_flags":{"billing":true,"read_replicas":true}}`, token)
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
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusCreated {
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
	if projectResponse.Code != http.StatusCreated {
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
		if response.Code != http.StatusCreated {
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
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if !strings.Contains(projectResponse.Body.String(), `"status":"healthy"`) {
		t.Fatalf("expected healthy project: %s", projectResponse.Body.String())
	}

	poolerUpdateResponse := perform(server, http.MethodPut, "/v1/projects/alpha-proj/config/pooler", `{"config":{"dedicated_pooler_enabled":"true","dedicated_pooler_tier":"medium","pool_mode":"both","transaction_port":"7654","session_port":"55432"}}`)
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
		`"transaction":"postgres://postgres.alpha-proj:${DB_PASSWORD}@pooler.alpha-proj.internal:7654/postgres?sslmode=require"`,
		`"session":"postgres://postgres.alpha-proj:${DB_PASSWORD}@pooler.alpha-proj.internal:55432/postgres?sslmode=require"`,
		`"pooler":{"dedicated":"true","dedicated_tier":"medium","default_pool_size":"20","max_client_connections":"200","pool_mode":"both","session_port":"55432","transaction_port":"7654"}`,
		`"password_handle":"secret://projects/alpha-proj/db_password"`,
		`"sslmode":"require"`,
		`"auth_url":"https://alpha-proj.supadupa.test/auth/v1"`,
		`"storage_url":"https://alpha-proj.supadupa.test/storage/v1"`,
		`"s3_endpoint":"https://alpha-proj.supadupa.test/storage/v1/s3"`,
		`"access_key_handle":"secret://projects/alpha-proj/s3_access_key"`,
		`"api":"https://alpha-proj.supadupa.test"`,
		`"studio_url":"https://studio.alpha-proj.supadupa.test"`,
		`"studio":"https://studio.alpha-proj.supadupa.test"`,
		`"studio_via_api":"https://alpha-proj.supadupa.test/studio"`,
		`"rest_docs":"https://studio.alpha-proj.supadupa.test/project/default/api"`,
		`"graphql_explorer":"https://studio.alpha-proj.supadupa.test/project/default/api?panel=graphql"`,
		`"storage_s3":"https://alpha-proj.supadupa.test/storage/v1/s3"`,
		`"functions_service":"https://alpha-proj.supadupa.test/functions/v1"`,
		`"realtime_service":"https://alpha-proj.supadupa.test/realtime/v1"`,
		`"connection_snippets"`,
		`"psql_pool_transaction":"psql postgres://postgres.alpha-proj:${DB_PASSWORD}@pooler.alpha-proj.internal:7654/postgres?sslmode=require"`,
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
		`"database_url":"postgres://postgres:${DB_PASSWORD}@db.alpha-proj.internal:5432/postgres?sslmode=prefer"`,
		`"pooler_transaction_url":"postgres://postgres.alpha-proj:${DB_PASSWORD}@pooler.alpha-proj.internal:7654/postgres?sslmode=prefer"`,
		`"supabase_config_toml"`,
		`[supadupa]`,
		`"control_plane":"Use supadupa Management API`,
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
	if !strings.Contains(routesResponse.Body.String(), `"fqdn":"studio.alpha-proj.supadupa.test"`) {
		t.Fatalf("expected studio route in response: %s", routesResponse.Body.String())
	}
}

func TestCreateProjectWithOrioleDBProfile(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"oriole-proj","name":"Oriole","domain":"supadupa.test","profile":"orioledb","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected orioledb project create 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if !strings.Contains(projectResponse.Body.String(), `"profile":"orioledb"`) {
		t.Fatalf("expected orioledb profile in project response: %s", projectResponse.Body.String())
	}
	configResponse := perform(server, http.MethodGet, "/v1/projects/oriole-proj/config/database", "")
	if configResponse.Code != http.StatusOK || !strings.Contains(configResponse.Body.String(), `"orioledb_profile":"preview"`) {
		t.Fatalf("expected orioledb database config: %d %s", configResponse.Code, configResponse.Body.String())
	}
}

func TestProjectBranchesCreateListRoutesAndCleanup(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "preview_branches")
	projectBody := `{"ref":"branch-source","name":"Branch Source","domain":"supadupa.test","profile":"full","resource_tier":"small","services":{"storage":true},"environment":{"CUSTOM":"value"}}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/branch-source/branches", `{"ref":"Bad Ref","name":"Bad"}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid branch ref 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/branch-source/branches", `{"ref":"branch-preview","name":"Preview","ttl_hours":24}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected branch create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{
		`"source_project_ref":"branch-source"`,
		`"project_ref":"branch-preview"`,
		`"status":"healthy"`,
		`"ref":"branch-preview"`,
		`"name":"Preview"`,
	} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected %s in branch create response: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "SUPADUPA_BRANCH_SOURCE_REF") || strings.Contains(createResponse.Body.String(), `"CUSTOM":"value"`) {
		t.Fatalf("branch create response leaked internal environment: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/branch-source/branches", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"project_ref":"branch-preview"`) {
		t.Fatalf("expected branch in list: %d %s", listResponse.Code, listResponse.Body.String())
	}

	connectResponse := perform(server, http.MethodGet, "/v1/projects/branch-preview/connect", "")
	if connectResponse.Code != http.StatusOK || !strings.Contains(connectResponse.Body.String(), "https://branch-preview.supadupa.test") {
		t.Fatalf("expected branch connect payload: %d %s", connectResponse.Code, connectResponse.Body.String())
	}
	routesResponse := perform(server, http.MethodGet, "/v1/projects/branch-preview/routes", "")
	if routesResponse.Code != http.StatusOK || !strings.Contains(routesResponse.Body.String(), `"fqdn":"branch-preview.supadupa.test"`) {
		t.Fatalf("expected branch routes: %d %s", routesResponse.Code, routesResponse.Body.String())
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.branch_create"`) {
		t.Fatalf("expected branch create audit event: %s", auditResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/branch-source/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Branch created") {
		t.Fatalf("expected source branch log: %s", logsResponse.Body.String())
	}

	deleteMissingResponse := perform(server, http.MethodDelete, "/v1/projects/branch-source/branches/missing-preview", "")
	if deleteMissingResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing branch delete 404, got %d: %s", deleteMissingResponse.Code, deleteMissingResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/branch-source/branches/branch-preview", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected branch project delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	listResponse = perform(server, http.MethodGet, "/v1/projects/branch-source/branches", "")
	if strings.Contains(listResponse.Body.String(), `"project_ref":"branch-preview"`) {
		t.Fatalf("expected branch metadata removed after branch delete: %s", listResponse.Body.String())
	}
	getBranchResponse := perform(server, http.MethodGet, "/v1/projects/branch-preview", "")
	if getBranchResponse.Code != http.StatusNotFound {
		t.Fatalf("expected branch project removed after branch delete, got %d: %s", getBranchResponse.Code, getBranchResponse.Body.String())
	}
	auditResponse = perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.branch_delete"`) {
		t.Fatalf("expected branch delete audit event: %s", auditResponse.Body.String())
	}
}

func TestProjectBranchCreatePassesGeneratedSecretsToProvisioner(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "preview_branches")
	projectBody := `{"ref":"branch-source","name":"Branch Source","domain":"supadupa.test","profile":"full","resource_tier":"small","environment":{"CUSTOM":"source-value","POSTGRES_PASSWORD":"source-should-not-win"}}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/branch-source/branches", `{"ref":"branch-secret-preview","name":"Preview","ttl_hours":24}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected branch create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	if strings.Contains(createResponse.Body.String(), "source-value") || strings.Contains(createResponse.Body.String(), "source-should-not-win") {
		t.Fatalf("branch response leaked internal environment: %s", createResponse.Body.String())
	}
	if provisioner.spec.Ref != "branch-secret-preview" {
		t.Fatalf("expected branch provisioner create call, got %#v", provisioner.spec)
	}
	if provisioner.spec.Environment["CUSTOM"] != "source-value" || provisioner.spec.Environment["SUPADUPA_BRANCH_SOURCE_REF"] != "branch-source" {
		t.Fatalf("expected branch to inherit source environment markers, got %#v", provisioner.spec.Environment)
	}
	for key, prefix := range map[string]string{
		"JWT_SECRET":                       "jwt_",
		"SUPADUPA_JWT_SIGNING_KEY_CURRENT": "{",
		"SUPADUPA_JWT_SIGNING_KEY_NEXT":    "{",
		"ANON_KEY":                         "anon_",
		"SERVICE_ROLE_KEY":                 "svc_",
		"SUPABASE_PUBLISHABLE_KEY":         "pub_",
		"SUPABASE_SECRET_KEY":              "sec_",
		"POSTGRES_PASSWORD":                "db_",
		"S3_ACCESS_KEY":                    "s3ak_",
		"S3_SECRET_KEY":                    "s3sk_",
	} {
		value := provisioner.spec.Environment[key]
		if !strings.HasPrefix(value, prefix) {
			t.Fatalf("expected branch provisioner env %s to have prefix %q, got %q in %#v", key, prefix, value, provisioner.spec.Environment)
		}
	}
	if provisioner.spec.Environment["POSTGRES_PASSWORD"] == "source-should-not-win" {
		t.Fatalf("source db password won over branch managed secret")
	}
	if provisioner.clonedBranch.SourceRef != "branch-source" || provisioner.clonedBranch.BranchRef != "branch-secret-preview" || provisioner.clonedBranch.BranchID == "" {
		t.Fatalf("expected branch clone call, got %#v", provisioner.clonedBranch)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"clone_state":"dry-run"`) || !strings.Contains(auditResponse.Body.String(), `"clone_path":"branch-clone.sql"`) {
		t.Fatalf("expected branch clone metadata in audit: %s", auditResponse.Body.String())
	}
}

func TestProjectReplicasCreateListUsageAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"disk_iops":24000,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host create 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "read_replicas")
	projectBody := `{"ref":"replica-proj","name":"Replica","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"bad name","tier":"small"}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid replica name 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"east","host_id":"`+hostID+`","region":"us-east","tier":"small","read_weight":75,"failover_priority":2}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected replica create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	eastID := extractString(t, createResponse.Body.String(), "id")
	for _, expected := range []string{
		`"project_ref":"replica-proj"`,
		`"name":"east"`,
		`"region":"us-east"`,
		`"tier":"small"`,
		`"status":"healthy"`,
		`"role":"read"`,
		`"read_weight":75`,
		`"failover_priority":2`,
		`"read_uri":"postgres://postgres:${DB_PASSWORD}@east.replica-proj.replica.internal:5432/postgres"`,
	} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected %s in replica create response: %s", expected, createResponse.Body.String())
		}
	}
	westResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"west","host_id":"`+hostID+`","region":"us-west","tier":"small","read_weight":125,"failover_priority":1}`)
	if westResponse.Code != http.StatusCreated {
		t.Fatalf("expected west replica create 201, got %d: %s", westResponse.Code, westResponse.Body.String())
	}
	westID := extractString(t, westResponse.Body.String(), "id")

	listResponse := perform(server, http.MethodGet, "/v1/projects/replica-proj/replicas", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"east"`) || !strings.Contains(listResponse.Body.String(), `"name":"west"`) {
		t.Fatalf("expected replica in list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	routingResponse := perform(server, http.MethodGet, "/v1/projects/replica-proj/replicas/routing", "")
	for _, expected := range []string{`"read_strategy":"weighted-healthy"`, `"auto_failover":true`, `"name":"west"`, `"weight":125`, `"failover_priority":1`} {
		if !strings.Contains(routingResponse.Body.String(), expected) {
			t.Fatalf("expected routing value %s: %s", expected, routingResponse.Body.String())
		}
	}
	promoteResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas/"+eastID+"/promote", `{"reason":"planned maintenance"}`)
	if promoteResponse.Code != http.StatusOK || !strings.Contains(promoteResponse.Body.String(), `"role":"primary"`) || !strings.Contains(promoteResponse.Body.String(), `"message":"planned maintenance"`) {
		t.Fatalf("expected promoted east replica: %d %s", promoteResponse.Code, promoteResponse.Body.String())
	}
	failoverResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas/failover", `{"reason":"primary degraded"}`)
	if failoverResponse.Code != http.StatusOK || !strings.Contains(failoverResponse.Body.String(), `"id":"`+westID+`"`) || !strings.Contains(failoverResponse.Body.String(), `"role":"primary"`) {
		t.Fatalf("expected automatic failover to west replica: %d %s", failoverResponse.Code, failoverResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	for _, expected := range []string{`"read_replicas":2`, `"cpu":3`, `"ram_mb":6144`, `"disk_gb":60`, `"disk_iops":9000`, `"projects":1`} {
		if !strings.Contains(usageResponse.Body.String(), expected) {
			t.Fatalf("expected usage value %s: %s", expected, usageResponse.Body.String())
		}
	}
	metricsResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(metricsResponse.Body.String(), `"read_replicas":2`) {
		t.Fatalf("expected fleet replica metric: %s", metricsResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_read_replicas_total 2") {
		t.Fatalf("expected prometheus replica metric: %s", prometheusResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/replica-proj/logs", "")
	for _, expected := range []string{"Read replica provisioned", "Read replica promoted", "Read replica failover completed"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected replica project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{`"action":"project.replica_create"`, `"action":"project.replica_promote"`, `"action":"project.replica_failover"`} {
		if !strings.Contains(auditResponse.Body.String(), action) {
			t.Fatalf("expected replica audit action %s: %s", action, auditResponse.Body.String())
		}
	}
	routingResponse = perform(server, http.MethodGet, "/v1/projects/replica-proj/replicas/routing", "")
	if !strings.Contains(routingResponse.Body.String(), `"primary_replica_id":"`+westID+`"`) {
		t.Fatalf("expected west as routing primary: %s", routingResponse.Body.String())
	}
	if strings.Contains(routingResponse.Body.String(), `"replica_id":"`+westID+`","name":"west","uri"`) && strings.Contains(routingResponse.Body.String(), `"healthy_read_targets":[{"replica_id":"`+westID) {
		t.Fatalf("promoted primary should not remain in healthy read targets: %s", routingResponse.Body.String())
	}
	if !strings.Contains(routingResponse.Body.String(), `"replica_id":"`+eastID+`"`) {
		t.Fatalf("expected east read target after failover: %s", routingResponse.Body.String())
	}
	deletePrimaryResponse := perform(server, http.MethodDelete, "/v1/projects/replica-proj/replicas/"+westID, "")
	if deletePrimaryResponse.Code != http.StatusConflict {
		t.Fatalf("expected deleting promoted primary to be rejected, got %d: %s", deletePrimaryResponse.Code, deletePrimaryResponse.Body.String())
	}
	deleteReadResponse := perform(server, http.MethodDelete, "/v1/projects/replica-proj/replicas/"+eastID, "")
	if deleteReadResponse.Code != http.StatusNoContent {
		t.Fatalf("expected read replica delete 204, got %d: %s", deleteReadResponse.Code, deleteReadResponse.Body.String())
	}
	listResponse = perform(server, http.MethodGet, "/v1/projects/replica-proj/replicas", "")
	if strings.Contains(listResponse.Body.String(), `"id":"`+eastID+`"`) || !strings.Contains(listResponse.Body.String(), `"id":"`+westID+`"`) {
		t.Fatalf("expected only promoted west replica after delete: %d %s", listResponse.Code, listResponse.Body.String())
	}
	usageResponse = perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"read_replicas":1`) || !strings.Contains(usageResponse.Body.String(), `"cpu":2`) {
		t.Fatalf("expected usage to reflect deleted read replica: %s", usageResponse.Body.String())
	}
	auditResponse = perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.replica_delete"`) {
		t.Fatalf("expected replica delete audit action: %s", auditResponse.Body.String())
	}
	if logsResponse.Code != http.StatusOK {
		t.Fatalf("expected replica project log: %s", logsResponse.Body.String())
	}
}

func TestProjectScaleUpdatesTierCapacityAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"disk_iops":24000,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host create 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"scale-proj","name":"Scale","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/scale-proj/scale", `{"resource_tier":"huge"}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid scale tier 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	scaleResponse := perform(server, http.MethodPost, "/v1/projects/scale-proj/scale", `{"resource_tier":"large"}`)
	if scaleResponse.Code != http.StatusOK {
		t.Fatalf("expected scale status 200, got %d: %s", scaleResponse.Code, scaleResponse.Body.String())
	}
	if !strings.Contains(scaleResponse.Body.String(), `"resource_tier":"large"`) || !strings.Contains(scaleResponse.Body.String(), `"message":"resource tier updated"`) {
		t.Fatalf("expected large scaled project response: %s", scaleResponse.Body.String())
	}

	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	for _, expected := range []string{`"cpu":4`, `"ram_mb":8192`, `"disk_gb":100`, `"disk_iops":12000`, `"projects":1`} {
		if !strings.Contains(usageResponse.Body.String(), expected) {
			t.Fatalf("expected scaled usage %s: %s", expected, usageResponse.Body.String())
		}
	}
	hostsResponse := perform(server, http.MethodGet, "/v1/hosts", "")
	for _, expected := range []string{`"used":{"cpu":4`, `"ram_mb":8192`, `"disk_gb":100`, `"disk_iops":12000`, `"projects":1`} {
		if !strings.Contains(hostsResponse.Body.String(), expected) {
			t.Fatalf("expected scaled host usage %s: %s", expected, hostsResponse.Body.String())
		}
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/scale-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Resource tier scaled") {
		t.Fatalf("expected scale project log: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.scale"`) {
		t.Fatalf("expected scale audit event: %s", auditResponse.Body.String())
	}
}

func TestProjectCustomDomainsUpdateRoutes(t *testing.T) {
	certRoot := t.TempDir()
	t.Setenv("SUPADUPA_CERT_ROOT", certRoot)
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "custom_domains")
	projectBody := `{"ref":"domain-proj","name":"Domain","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	addResponse := perform(server, http.MethodPost, "/v1/projects/domain-proj/domains", `{"fqdn":"API.Example.COM."}`)
	if addResponse.Code != http.StatusCreated {
		t.Fatalf("expected domain create 201, got %d: %s", addResponse.Code, addResponse.Body.String())
	}
	if !strings.Contains(addResponse.Body.String(), `"fqdn":"api.example.com"`) || !strings.Contains(addResponse.Body.String(), `"cert_status":"pending"`) {
		t.Fatalf("expected normalized pending domain: %s", addResponse.Body.String())
	}
	certPath := filepath.Join(certRoot, "domain-proj", "api.example.com.json")
	certPlan, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("expected certificate plan artifact: %v", err)
	}
	if !strings.Contains(string(certPlan), `"fqdn": "api.example.com"`) {
		t.Fatalf("expected normalized cert plan, got:\n%s", certPlan)
	}

	domainsResponse := perform(server, http.MethodGet, "/v1/projects/domain-proj/domains", "")
	if domainsResponse.Code != http.StatusOK || !strings.Contains(domainsResponse.Body.String(), `"fqdn":"api.example.com"`) {
		t.Fatalf("expected custom domain in list: %d %s", domainsResponse.Code, domainsResponse.Body.String())
	}

	routesResponse := perform(server, http.MethodGet, "/v1/projects/domain-proj/routes", "")
	if routesResponse.Code != http.StatusOK || !strings.Contains(routesResponse.Body.String(), `"fqdn":"api.example.com"`) {
		t.Fatalf("expected custom domain route: %d %s", routesResponse.Code, routesResponse.Body.String())
	}

	duplicateResponse := perform(server, http.MethodPost, "/v1/projects/domain-proj/domains", `{"fqdn":"api.example.com"}`)
	if duplicateResponse.Code != http.StatusConflict {
		t.Fatalf("expected duplicate domain conflict, got %d: %s", duplicateResponse.Code, duplicateResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/domain-proj/domains/api.example.com", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected domain delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, err := os.Stat(certPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected certificate artifact removed, got err=%v", err)
	}

	routesResponse = perform(server, http.MethodGet, "/v1/projects/domain-proj/routes", "")
	if strings.Contains(routesResponse.Body.String(), `"fqdn":"api.example.com"`) {
		t.Fatalf("expected custom domain route removed: %s", routesResponse.Body.String())
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.domain_create", "project.domain_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/domain-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Custom domain added") || !strings.Contains(logsResponse.Body.String(), "Custom domain removed") {
		t.Fatalf("expected custom domain project logs: %s", logsResponse.Body.String())
	}
}

func TestProjectConfigDefaultsUpdateAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"config-proj","name":"Config","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	defaultResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/auth", "")
	if defaultResponse.Code != http.StatusOK || !strings.Contains(defaultResponse.Body.String(), `"email_enabled":"true"`) || !strings.Contains(defaultResponse.Body.String(), `"mfa_totp_enroll_enabled":"true"`) || !strings.Contains(defaultResponse.Body.String(), `"mfa_phone_otp_length":"6"`) || !strings.Contains(defaultResponse.Body.String(), `"captcha_secret_handle":""`) {
		t.Fatalf("expected default auth config: %d %s", defaultResponse.Code, defaultResponse.Body.String())
	}

	invalidMFAOTPResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth", `{"config":{"mfa_phone_otp_length":"3"}}`)
	if invalidMFAOTPResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid phone mfa otp length 400, got %d: %s", invalidMFAOTPResponse.Code, invalidMFAOTPResponse.Body.String())
	}

	invalidMFAFrequencyResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth", `{"config":{"mfa_phone_max_frequency":"often"}}`)
	if invalidMFAFrequencyResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid phone mfa frequency 400, got %d: %s", invalidMFAFrequencyResponse.Code, invalidMFAFrequencyResponse.Body.String())
	}

	invalidCaptchaProviderResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth", `{"config":{"captcha_provider":"recaptcha"}}`)
	if invalidCaptchaProviderResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid captcha provider 400, got %d: %s", invalidCaptchaProviderResponse.Code, invalidCaptchaProviderResponse.Body.String())
	}

	invalidCaptchaSecretResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth", `{"config":{"captcha_secret_handle":"raw-secret"}}`)
	if invalidCaptchaSecretResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid captcha secret handle 400, got %d: %s", invalidCaptchaSecretResponse.Code, invalidCaptchaSecretResponse.Body.String())
	}

	updateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth", `{"config":{"email_enabled":"false","mfa_totp_enabled":"true","mfa_totp_enroll_enabled":"true","mfa_totp_verify_enabled":"true","mfa_phone_enabled":"true","mfa_phone_enroll_enabled":"true","mfa_phone_verify_enabled":"true","mfa_phone_otp_length":"8","mfa_phone_max_frequency":"20s","captcha_provider":"turnstile","captcha_site_key":"site-key","captcha_secret_handle":"secret://projects/config-proj/captcha-secret","site_url":"https://app.example.com"}}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected config update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	for _, expected := range []string{`"email_enabled":"false"`, `"mfa_totp_enabled":"true"`, `"mfa_totp_enroll_enabled":"true"`, `"mfa_totp_verify_enabled":"true"`, `"mfa_phone_enabled":"true"`, `"mfa_phone_enroll_enabled":"true"`, `"mfa_phone_verify_enabled":"true"`, `"mfa_phone_otp_length":"8"`, `"mfa_phone_max_frequency":"20s"`, `"captcha_provider":"turnstile"`, `"captcha_site_key":"site-key"`, `"captcha_secret_handle":"secret://projects/config-proj/captcha-secret"`, `"site_url":"https://app.example.com"`, `"magic_link_enabled":"true"`} {
		if !strings.Contains(updateResponse.Body.String(), expected) {
			t.Fatalf("expected %s in config update response: %s", expected, updateResponse.Body.String())
		}
	}

	getResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/auth", "")
	if !strings.Contains(getResponse.Body.String(), `"captcha_provider":"turnstile"`) || !strings.Contains(getResponse.Body.String(), `"captcha_secret_handle":"secret://projects/config-proj/captcha-secret"`) {
		t.Fatalf("expected persisted config: %s", getResponse.Body.String())
	}

	providersDefaultResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/auth_providers", "")
	if providersDefaultResponse.Code != http.StatusOK || !strings.Contains(providersDefaultResponse.Body.String(), `"oauth_google_enabled":"false"`) || !strings.Contains(providersDefaultResponse.Body.String(), `"oauth_discord_enabled":"false"`) || !strings.Contains(providersDefaultResponse.Body.String(), `"saml_enabled":"false"`) {
		t.Fatalf("expected default auth provider config: %d %s", providersDefaultResponse.Code, providersDefaultResponse.Body.String())
	}
	invalidAuthProviderSecretResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"oauth_google_client_secret_handle":"raw-secret"}}`)
	if invalidAuthProviderSecretResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid auth provider secret handle 400, got %d: %s", invalidAuthProviderSecretResponse.Code, invalidAuthProviderSecretResponse.Body.String())
	}
	invalidSMSProviderResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"sms_provider":"raw-sms"}}`)
	if invalidSMSProviderResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid sms provider 400, got %d: %s", invalidSMSProviderResponse.Code, invalidSMSProviderResponse.Body.String())
	}
	invalidSMSKeyResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"sms_messagebird_access_key_handle":"raw-key"}}`)
	if invalidSMSKeyResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid sms key handle 400, got %d: %s", invalidSMSKeyResponse.Code, invalidSMSKeyResponse.Body.String())
	}
	invalidOIDCIssuerResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"oauth_oidc_issuer_url":"http://issuer.example.com"}}`)
	if invalidOIDCIssuerResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid oidc issuer 400, got %d: %s", invalidOIDCIssuerResponse.Code, invalidOIDCIssuerResponse.Body.String())
	}
	providersUpdateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"oauth_google_enabled":"true","oauth_google_client_id":"google-client","oauth_google_client_secret_handle":"secret://projects/config-proj/google-oauth-secret","oauth_discord_enabled":"true","oauth_discord_client_id":"discord-client","oauth_discord_client_secret_handle":"secret://projects/config-proj/discord-secret","oauth_gitlab_enabled":"true","oauth_gitlab_url":"https://gitlab.example.com","oauth_gitlab_redirect_uri":"https://app.example.com/auth/callback","oauth_gitlab_skip_nonce_check":"true","oauth_oidc_enabled":"true","oauth_oidc_issuer_url":"https://issuer.example.com","oauth_oidc_client_id":"oidc-client","oauth_oidc_client_secret_handle":"secret://projects/config-proj/oidc-secret","oauth_oidc_scopes":"openid email profile","phone_enabled":"true","sms_provider":"messagebird","sms_messagebird_originator":"Supadupa","sms_messagebird_access_key_handle":"secret://projects/config-proj/messagebird-key","sms_twilio_auth_token_handle":"secret://projects/config-proj/twilio-token","saml_enabled":"true","saml_metadata_url":"https://idp.example.com/metadata","third_party_jwt_issuer":"https://issuer.example.com","web3_ethereum_enabled":"true"}}`)
	if providersUpdateResponse.Code != http.StatusOK {
		t.Fatalf("expected auth providers update 200, got %d: %s", providersUpdateResponse.Code, providersUpdateResponse.Body.String())
	}
	for _, expected := range []string{`"oauth_google_enabled":"true"`, `"oauth_google_client_id":"google-client"`, `"oauth_google_client_secret_handle":"secret://projects/config-proj/google-oauth-secret"`, `"oauth_discord_client_secret_handle":"secret://projects/config-proj/discord-secret"`, `"oauth_gitlab_url":"https://gitlab.example.com"`, `"oauth_gitlab_skip_nonce_check":"true"`, `"oauth_oidc_enabled":"true"`, `"oauth_oidc_issuer_url":"https://issuer.example.com"`, `"oauth_oidc_client_secret_handle":"secret://projects/config-proj/oidc-secret"`, `"phone_enabled":"true"`, `"sms_provider":"messagebird"`, `"sms_messagebird_access_key_handle":"secret://projects/config-proj/messagebird-key"`, `"sms_twilio_auth_token_handle":"secret://projects/config-proj/twilio-token"`, `"saml_metadata_url":"https://idp.example.com/metadata"`, `"web3_ethereum_enabled":"true"`} {
		if !strings.Contains(providersUpdateResponse.Body.String(), expected) {
			t.Fatalf("expected %s in auth providers response: %s", expected, providersUpdateResponse.Body.String())
		}
	}

	templatesDefaultResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/email_templates", "")
	if templatesDefaultResponse.Code != http.StatusOK || !strings.Contains(templatesDefaultResponse.Body.String(), `"confirmation_subject":""`) || !strings.Contains(templatesDefaultResponse.Body.String(), `"notification_password_changed_enabled":"false"`) || !strings.Contains(templatesDefaultResponse.Body.String(), `"notification_password_changed_subject":""`) {
		t.Fatalf("expected default email template config: %d %s", templatesDefaultResponse.Code, templatesDefaultResponse.Body.String())
	}
	templatesUpdateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/email_templates", `{"config":{"confirmation_subject":"Confirm your account","confirmation_body":"Hello {{ .Email }}","magic_link_subject":"Your magic link","sms_otp_message":"Code: {{ .Token }}","notification_password_changed_enabled":"true","notification_password_changed_subject":"Password changed","notification_identity_linked_enabled":"true","notification_identity_linked_body":"Identity {{ .IdentityID }} linked"}}`)
	if templatesUpdateResponse.Code != http.StatusOK {
		t.Fatalf("expected email templates update 200, got %d: %s", templatesUpdateResponse.Code, templatesUpdateResponse.Body.String())
	}
	for _, expected := range []string{`"confirmation_subject":"Confirm your account"`, `"confirmation_body":"Hello {{ .Email }}"`, `"magic_link_subject":"Your magic link"`, `"sms_otp_message":"Code: {{ .Token }}"`, `"notification_password_changed_enabled":"true"`, `"notification_password_changed_subject":"Password changed"`, `"notification_identity_linked_enabled":"true"`, `"notification_identity_linked_body":"Identity {{ .IdentityID }} linked"`} {
		if !strings.Contains(templatesUpdateResponse.Body.String(), expected) {
			t.Fatalf("expected %s in email templates response: %s", expected, templatesUpdateResponse.Body.String())
		}
	}

	databaseDefaultResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/database", "")
	if databaseDefaultResponse.Code != http.StatusOK || !strings.Contains(databaseDefaultResponse.Body.String(), `"pg_graphql_enabled":"true"`) || !strings.Contains(databaseDefaultResponse.Body.String(), `"supavisor_enabled":"true"`) {
		t.Fatalf("expected default database config: %d %s", databaseDefaultResponse.Code, databaseDefaultResponse.Body.String())
	}

	databaseUpdateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/database", `{"config":{"extension_toggle_ui":"true","performance_advisor_mode":"fleet","orioledb_profile":"preview"}}`)
	if databaseUpdateResponse.Code != http.StatusOK {
		t.Fatalf("expected database config update 200, got %d: %s", databaseUpdateResponse.Code, databaseUpdateResponse.Body.String())
	}
	for _, expected := range []string{`"extension_toggle_ui":"true"`, `"performance_advisor_mode":"fleet"`, `"orioledb_profile":"preview"`, `"pg_cron_enabled":"true"`} {
		if !strings.Contains(databaseUpdateResponse.Body.String(), expected) {
			t.Fatalf("expected %s in database config update response: %s", expected, databaseUpdateResponse.Body.String())
		}
	}

	realtimeDefaultResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/realtime", "")
	if realtimeDefaultResponse.Code != http.StatusOK || !strings.Contains(realtimeDefaultResponse.Body.String(), `"broadcast_replay":"false"`) || !strings.Contains(realtimeDefaultResponse.Body.String(), `"broadcast_from_database":"false"`) {
		t.Fatalf("expected default realtime config: %d %s", realtimeDefaultResponse.Code, realtimeDefaultResponse.Body.String())
	}
	realtimeUpdateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/realtime", `{"config":{"broadcast_replay":"true","broadcast_from_database":"true"}}`)
	if realtimeUpdateResponse.Code != http.StatusOK {
		t.Fatalf("expected realtime config update 200, got %d: %s", realtimeUpdateResponse.Code, realtimeUpdateResponse.Body.String())
	}
	for _, expected := range []string{`"broadcast_replay":"true"`, `"broadcast_from_database":"true"`, `"postgres_changes_enabled":"true"`} {
		if !strings.Contains(realtimeUpdateResponse.Body.String(), expected) {
			t.Fatalf("expected %s in realtime config response: %s", expected, realtimeUpdateResponse.Body.String())
		}
	}

	poolerDefaultResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/pooler", "")
	if poolerDefaultResponse.Code != http.StatusOK || !strings.Contains(poolerDefaultResponse.Body.String(), `"dedicated_pooler_enabled":"false"`) || !strings.Contains(poolerDefaultResponse.Body.String(), `"transaction_port":"6543"`) {
		t.Fatalf("expected default pooler config: %d %s", poolerDefaultResponse.Code, poolerDefaultResponse.Body.String())
	}
	invalidPoolerResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/pooler", `{"config":{"pool_mode":"invalid"}}`)
	if invalidPoolerResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid pooler config 400, got %d: %s", invalidPoolerResponse.Code, invalidPoolerResponse.Body.String())
	}
	poolerUpdateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/pooler", `{"config":{"dedicated_pooler_enabled":"true","dedicated_pooler_tier":"large","pool_mode":"both","default_pool_size":"50","max_client_connections":"500","transaction_port":"7654","session_port":"55432"}}`)
	if poolerUpdateResponse.Code != http.StatusOK {
		t.Fatalf("expected pooler config update 200, got %d: %s", poolerUpdateResponse.Code, poolerUpdateResponse.Body.String())
	}
	for _, expected := range []string{`"dedicated_pooler_enabled":"true"`, `"dedicated_pooler_tier":"large"`, `"pool_mode":"both"`, `"transaction_port":"7654"`, `"session_port":"55432"`} {
		if !strings.Contains(poolerUpdateResponse.Body.String(), expected) {
			t.Fatalf("expected %s in pooler config response: %s", expected, poolerUpdateResponse.Body.String())
		}
	}

	smtpDefaultResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/smtp", "")
	if smtpDefaultResponse.Code != http.StatusOK || !strings.Contains(smtpDefaultResponse.Body.String(), `"port":"587"`) || !strings.Contains(smtpDefaultResponse.Body.String(), `"tls_mode":"starttls"`) || !strings.Contains(smtpDefaultResponse.Body.String(), `"password_handle":""`) {
		t.Fatalf("expected default smtp config: %d %s", smtpDefaultResponse.Code, smtpDefaultResponse.Body.String())
	}
	invalidSMTPSecretResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/smtp", `{"config":{"password_handle":"raw-password"}}`)
	if invalidSMTPSecretResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid smtp password handle 400, got %d: %s", invalidSMTPSecretResponse.Code, invalidSMTPSecretResponse.Body.String())
	}
	invalidSMTPTLSResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/smtp", `{"config":{"tls_mode":"opportunistic"}}`)
	if invalidSMTPTLSResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid smtp tls mode 400, got %d: %s", invalidSMTPTLSResponse.Code, invalidSMTPTLSResponse.Body.String())
	}
	smtpUpdateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/smtp", `{"config":{"enabled":"true","host":"smtp.example.com","port":"2525","sender_name":"Supadupa","sender_email":"noreply@example.com","username":"apikey","password_handle":"secret://projects/config-proj/smtp-password","tls_mode":"implicit"}}`)
	if smtpUpdateResponse.Code != http.StatusOK {
		t.Fatalf("expected smtp config update 200, got %d: %s", smtpUpdateResponse.Code, smtpUpdateResponse.Body.String())
	}
	for _, expected := range []string{`"enabled":"true"`, `"username":"apikey"`, `"password_handle":"secret://projects/config-proj/smtp-password"`, `"tls_mode":"implicit"`} {
		if !strings.Contains(smtpUpdateResponse.Body.String(), expected) {
			t.Fatalf("expected %s in smtp config response: %s", expected, smtpUpdateResponse.Body.String())
		}
	}

	aiDefaultResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/ai", "")
	if aiDefaultResponse.Code != http.StatusOK || !strings.Contains(aiDefaultResponse.Body.String(), `"openai_enabled":"false"`) || !strings.Contains(aiDefaultResponse.Body.String(), `"default_embedding_model":"text-embedding-3-small"`) || !strings.Contains(aiDefaultResponse.Body.String(), `"studio_assistant_enabled":"false"`) {
		t.Fatalf("expected default ai config: %d %s", aiDefaultResponse.Code, aiDefaultResponse.Body.String())
	}
	invalidAIResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/ai", `{"config":{"openai_api_key_handle":"raw-key"}}`)
	if invalidAIResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid ai config 400, got %d: %s", invalidAIResponse.Code, invalidAIResponse.Body.String())
	}
	invalidAssistantKeyResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/ai", `{"config":{"studio_assistant_key_handle":"raw-key"}}`)
	if invalidAssistantKeyResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid studio assistant key 400, got %d: %s", invalidAssistantKeyResponse.Code, invalidAssistantKeyResponse.Body.String())
	}
	aiUpdateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/ai", `{"config":{"openai_enabled":"true","openai_api_key_handle":"secret://projects/config-proj/openai","default_embedding_provider":"openai","default_embedding_model":"text-embedding-3-large","default_embedding_dimension":"3072","studio_assistant_enabled":"true","studio_assistant_provider":"openai","studio_assistant_model":"assistant-default","studio_assistant_key_handle":"secret://projects/config-proj/studio-ai"}}`)
	if aiUpdateResponse.Code != http.StatusOK {
		t.Fatalf("expected ai config update 200, got %d: %s", aiUpdateResponse.Code, aiUpdateResponse.Body.String())
	}
	for _, expected := range []string{`"openai_enabled":"true"`, `"openai_api_key_handle":"secret://projects/config-proj/openai"`, `"default_embedding_model":"text-embedding-3-large"`, `"default_embedding_dimension":"3072"`, `"studio_assistant_enabled":"true"`, `"studio_assistant_key_handle":"secret://projects/config-proj/studio-ai"`} {
		if !strings.Contains(aiUpdateResponse.Body.String(), expected) {
			t.Fatalf("expected %s in ai config response: %s", expected, aiUpdateResponse.Body.String())
		}
	}

	invalidResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/billing", "")
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported area 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.config_update"`) {
		t.Fatalf("expected config update audit event: %s", auditResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Project config updated") {
		t.Fatalf("expected config update project log: %s", logsResponse.Body.String())
	}
}

func TestProjectConfigUpdateSyncsRuntimeProvisioner(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"runtime-config-proj","name":"Runtime Config","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	updateResponse := perform(server, http.MethodPut, "/v1/projects/runtime-config-proj/config/storage", `{"config":{"file_size_limit_mb":"100","image_transform_enabled":"false"}}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected config update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	if provisioner.syncedConfigRef != "runtime-config-proj" || provisioner.syncedConfig.Area != "storage" {
		t.Fatalf("expected runtime config sync, ref=%q config=%#v", provisioner.syncedConfigRef, provisioner.syncedConfig)
	}
	if provisioner.syncedConfig.Config["file_size_limit_mb"] != "100" || provisioner.syncedConfig.Config["image_transform_enabled"] != "false" {
		t.Fatalf("expected merged config in sync call, got %#v", provisioner.syncedConfig.Config)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"runtime_synced":"true"`) {
		t.Fatalf("expected runtime sync audit metadata: %s", auditResponse.Body.String())
	}
}

func TestProjectServicesUpdateSyncsRuntimeProvisioner(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"runtime-services-proj","name":"Runtime Services","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	updateResponse := perform(server, http.MethodPut, "/v1/projects/runtime-services-proj/services", `{"services":{"storage":false,"functions":false}}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected services update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	for _, expected := range []string{`"storage":false`, `"functions":false`, `"auth":true`} {
		if !strings.Contains(updateResponse.Body.String(), expected) {
			t.Fatalf("expected service state %s: %s", expected, updateResponse.Body.String())
		}
	}
	if provisioner.syncedServicesRef != "runtime-services-proj" {
		t.Fatalf("expected runtime services sync, got ref=%q", provisioner.syncedServicesRef)
	}
	states := control.ProjectServiceStates(provisioner.syncedServicesSpec.Services)
	if states["storage"] || states["functions"] || !states["auth"] {
		t.Fatalf("expected synced service state, got %#v", states)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.services_update"`) || !strings.Contains(auditResponse.Body.String(), `"runtime_synced":"true"`) {
		t.Fatalf("expected services audit metadata: %s", auditResponse.Body.String())
	}
}

func TestNetworkConfigReconcilesRoutePolicy(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"network-proj","name":"Network","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPut, "/v1/projects/network-proj/config/network", `{"config":{"ip_allowlist":"bad-cidr"}}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid network config 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	updateResponse := perform(server, http.MethodPut, "/v1/projects/network-proj/config/network", `{"config":{"ip_allowlist":"10.0.0.0/8, 203.0.113.10","ssl_enforced":"true"}}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected network config update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	routesResponse := perform(server, http.MethodGet, "/v1/projects/network-proj/routes", "")
	if routesResponse.Code != http.StatusOK {
		t.Fatalf("expected routes status 200, got %d: %s", routesResponse.Code, routesResponse.Body.String())
	}
	for _, expected := range []string{`"ssl_enforced":true`, `"ip_allowlist":["10.0.0.0/8","203.0.113.10"]`} {
		if !strings.Contains(routesResponse.Body.String(), expected) {
			t.Fatalf("expected route policy %s: %s", expected, routesResponse.Body.String())
		}
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/network-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Project config updated") {
		t.Fatalf("expected network config project log: %s", logsResponse.Body.String())
	}
}

func TestProjectFunctionsDeployListDeleteAndAudit(t *testing.T) {
	projectRoot := t.TempDir()
	t.Setenv("SUPADUPA_PROJECT_ROOT", projectRoot)
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"fn-proj","name":"Functions","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"east","address":"10.0.0.12","capacity":{"cpu":4,"ram_mb":16384,"disk_gb":200,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host create 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/fn-proj/functions", `{"name":"bad name","source":"Deno.serve(() => new Response('ok'))"}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid function name 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	createBody := `{"name":"hello-api","entrypoint":"index.ts","verify_jwt":true,"source":"Deno.serve(() => new Response('ok'))","secrets":{"API_KEY":"super-secret"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/fn-proj/functions", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected function deploy 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"hello-api"`, `"version":1`, `"status":"deployed"`, `"source_hash":"`, `"source_bytes":36`, `"api_key":"super-************cret"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected function deploy value %s: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "super-secret") {
		t.Fatalf("function deploy leaked secret: %s", createResponse.Body.String())
	}
	sourcePath := filepath.Join(projectRoot, "fn-proj", "functions", "hello-api", "index.ts")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("expected function source artifact: %v", err)
	}
	if string(source) != "Deno.serve(() => new Response('ok'))" {
		t.Fatalf("unexpected function source artifact:\n%s", source)
	}
	secretEnv, err := os.ReadFile(filepath.Join(projectRoot, "fn-proj", "functions", "hello-api", ".env"))
	if err != nil {
		t.Fatalf("expected function secret artifact: %v", err)
	}
	for _, expected := range []string{"SUPABASE_FUNCTION_VERSION=1", "VERIFY_JWT=true", "api_key=super-secret"} {
		if !strings.Contains(string(secretEnv), expected) {
			t.Fatalf("expected function env %q, got:\n%s", expected, secretEnv)
		}
	}
	metadata, err := os.ReadFile(filepath.Join(projectRoot, "fn-proj", "functions", "hello-api", "metadata.json"))
	if err != nil {
		t.Fatalf("expected function metadata artifact: %v", err)
	}
	if !strings.Contains(string(metadata), `"source_hash":`) || !strings.Contains(string(metadata), `"version": 1`) {
		t.Fatalf("expected function metadata, got:\n%s", metadata)
	}

	redeployResponse := perform(server, http.MethodPost, "/v1/projects/fn-proj/functions", `{"name":"hello-api","entrypoint":"index.ts","verify_jwt":false,"source":"Deno.serve(() => new Response('v2'))"}`)
	if redeployResponse.Code != http.StatusCreated || !strings.Contains(redeployResponse.Body.String(), `"version":2`) || !strings.Contains(redeployResponse.Body.String(), `"verify_jwt":false`) {
		t.Fatalf("expected function redeploy v2: %d %s", redeployResponse.Code, redeployResponse.Body.String())
	}
	source, err = os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("expected redeployed function source artifact: %v", err)
	}
	if string(source) != "Deno.serve(() => new Response('v2'))" {
		t.Fatalf("unexpected redeployed function source artifact:\n%s", source)
	}
	secretEnv, err = os.ReadFile(filepath.Join(projectRoot, "fn-proj", "functions", "hello-api", ".env"))
	if err != nil {
		t.Fatalf("expected redeployed function env artifact: %v", err)
	}
	if !strings.Contains(string(secretEnv), "SUPABASE_FUNCTION_VERSION=2") || strings.Contains(string(secretEnv), "api_key=super-secret") {
		t.Fatalf("expected redeploy env to update version and clear omitted secret, got:\n%s", secretEnv)
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/fn-proj/functions", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"version":2`) {
		t.Fatalf("expected function list v2: %d %s", listResponse.Code, listResponse.Body.String())
	}

	invalidRegionResponse := perform(server, http.MethodPost, "/v1/projects/fn-proj/functions/regions", `{"function_name":"missing-api","region":"us-east-1"}`)
	if invalidRegionResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing function region 404, got %d: %s", invalidRegionResponse.Code, invalidRegionResponse.Body.String())
	}
	regionResponse := perform(server, http.MethodPost, "/v1/projects/fn-proj/functions/regions", `{"function_name":"hello-api","host_id":"`+hostID+`","region":"us-east-1","routing_policy":"nearest"}`)
	if regionResponse.Code != http.StatusCreated {
		t.Fatalf("expected function region create 201, got %d: %s", regionResponse.Code, regionResponse.Body.String())
	}
	regionID := extractString(t, regionResponse.Body.String(), "id")
	for _, expected := range []string{`"function_name":"hello-api"`, `"host_id":"` + hostID + `"`, `"region":"us-east-1"`, `"routing_policy":"nearest"`, `"invocation_url":"https://hello-api.us-east-1.fn-proj.functions.internal"`, `"status":"configured"`} {
		if !strings.Contains(regionResponse.Body.String(), expected) {
			t.Fatalf("expected function region value %s: %s", expected, regionResponse.Body.String())
		}
	}
	regionListResponse := perform(server, http.MethodGet, "/v1/projects/fn-proj/functions/regions", "")
	if regionListResponse.Code != http.StatusOK || !strings.Contains(regionListResponse.Body.String(), `"id":"`+regionID+`"`) {
		t.Fatalf("expected function region list: %d %s", regionListResponse.Code, regionListResponse.Body.String())
	}
	regionManifest, err := os.ReadFile(filepath.Join(projectRoot, "fn-proj", "functions", "hello-api", "regions.json"))
	if err != nil {
		t.Fatalf("expected function region manifest: %v", err)
	}
	if !strings.Contains(string(regionManifest), `"region": "us-east-1"`) || !strings.Contains(string(regionManifest), `"routing_policy": "nearest"`) {
		t.Fatalf("expected function region manifest values, got:\n%s", regionManifest)
	}

	bucketResponse := perform(server, http.MethodPost, "/v1/projects/fn-proj/storage/buckets", `{"name":"assets","public":false,"file_size_limit":52428800}`)
	if bucketResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage bucket create 201, got %d: %s", bucketResponse.Code, bucketResponse.Body.String())
	}
	invalidMountResponse := perform(server, http.MethodPost, "/v1/projects/fn-proj/functions/storage-mounts", `{"function_name":"missing-api","bucket_name":"assets","mount_path":"/mnt/assets"}`)
	if invalidMountResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing function mount 404, got %d: %s", invalidMountResponse.Code, invalidMountResponse.Body.String())
	}
	mountResponse := perform(server, http.MethodPost, "/v1/projects/fn-proj/functions/storage-mounts", `{"function_name":"hello-api","bucket_name":"assets","mount_path":"/mnt/assets","read_only":true,"prefix":"public","env_alias":"ASSETS_MOUNT"}`)
	if mountResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage mount create 201, got %d: %s", mountResponse.Code, mountResponse.Body.String())
	}
	mountID := extractString(t, mountResponse.Body.String(), "id")
	for _, expected := range []string{`"function_name":"hello-api"`, `"bucket_name":"assets"`, `"mount_path":"/mnt/assets"`, `"read_only":true`, `"prefix":"public"`, `"env_alias":"ASSETS_MOUNT"`, `"status":"configured"`} {
		if !strings.Contains(mountResponse.Body.String(), expected) {
			t.Fatalf("expected function storage mount value %s: %s", expected, mountResponse.Body.String())
		}
	}
	mountListResponse := perform(server, http.MethodGet, "/v1/projects/fn-proj/functions/storage-mounts", "")
	if mountListResponse.Code != http.StatusOK || !strings.Contains(mountListResponse.Body.String(), `"id":"`+mountID+`"`) {
		t.Fatalf("expected function storage mount list: %d %s", mountListResponse.Code, mountListResponse.Body.String())
	}
	mountManifest, err := os.ReadFile(filepath.Join(projectRoot, "fn-proj", "functions", "hello-api", "storage-mounts.json"))
	if err != nil {
		t.Fatalf("expected function storage mount manifest: %v", err)
	}
	if !strings.Contains(string(mountManifest), `"bucket_name": "assets"`) || !strings.Contains(string(mountManifest), `"mount_path": "/mnt/assets"`) {
		t.Fatalf("expected function storage mount manifest values, got:\n%s", mountManifest)
	}
	metricsResponse := perform(server, http.MethodGet, "/v1/projects/fn-proj/metrics", "")
	if !strings.Contains(metricsResponse.Body.String(), `"function_regions":1`) || !strings.Contains(metricsResponse.Body.String(), `"function_storage_mounts":1`) {
		t.Fatalf("expected function region and storage mount metrics: %s", metricsResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_function_regions_total 1") || !strings.Contains(prometheusResponse.Body.String(), "supadupa_function_storage_mounts_total 1") {
		t.Fatalf("expected function region and storage mount prometheus metrics: %s", prometheusResponse.Body.String())
	}
	deleteRegionResponse := perform(server, http.MethodDelete, "/v1/projects/fn-proj/functions/regions/"+regionID, "")
	if deleteRegionResponse.Code != http.StatusNoContent {
		t.Fatalf("expected function region delete 204, got %d: %s", deleteRegionResponse.Code, deleteRegionResponse.Body.String())
	}
	regionListResponse = perform(server, http.MethodGet, "/v1/projects/fn-proj/functions/regions", "")
	if strings.TrimSpace(regionListResponse.Body.String()) != "[]" {
		t.Fatalf("expected empty function region list, got: %s", regionListResponse.Body.String())
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "fn-proj", "functions", "hello-api", "regions.json")); !os.IsNotExist(err) {
		t.Fatalf("expected function region manifest removed, got %v", err)
	}
	deleteMountResponse := perform(server, http.MethodDelete, "/v1/projects/fn-proj/functions/storage-mounts/"+mountID, "")
	if deleteMountResponse.Code != http.StatusNoContent {
		t.Fatalf("expected storage mount delete 204, got %d: %s", deleteMountResponse.Code, deleteMountResponse.Body.String())
	}
	mountListResponse = perform(server, http.MethodGet, "/v1/projects/fn-proj/functions/storage-mounts", "")
	if strings.TrimSpace(mountListResponse.Body.String()) != "[]" {
		t.Fatalf("expected empty function storage mount list, got: %s", mountListResponse.Body.String())
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "fn-proj", "functions", "hello-api", "storage-mounts.json")); !os.IsNotExist(err) {
		t.Fatalf("expected function storage mount manifest removed, got %v", err)
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/fn-proj/functions/hello-api", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected function delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	listResponse = perform(server, http.MethodGet, "/v1/projects/fn-proj/functions", "")
	if strings.Contains(listResponse.Body.String(), `"name":"hello-api"`) {
		t.Fatalf("expected function removed: %s", listResponse.Body.String())
	}
	if strings.TrimSpace(listResponse.Body.String()) != "[]" {
		t.Fatalf("expected empty function list array, got: %s", listResponse.Body.String())
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "fn-proj", "functions", "hello-api")); !os.IsNotExist(err) {
		t.Fatalf("expected function artifact removed, got %v", err)
	}

	logsResponse := perform(server, http.MethodGet, "/v1/projects/fn-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Function deployed") || !strings.Contains(logsResponse.Body.String(), "Function regional invocation configured") || !strings.Contains(logsResponse.Body.String(), "Function regional invocation removed") || !strings.Contains(logsResponse.Body.String(), "Function storage mount configured") || !strings.Contains(logsResponse.Body.String(), "Function storage mount removed") || !strings.Contains(logsResponse.Body.String(), "Function deleted") {
		t.Fatalf("expected function logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.function_deploy", "project.function_region_create", "project.function_region_delete", "project.function_storage_mount_create", "project.function_storage_mount_delete", "project.function_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectLogDrainsCreateListDeleteAndAudit(t *testing.T) {
	projectRoot := t.TempDir()
	t.Setenv("SUPADUPA_PROJECT_ROOT", projectRoot)
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "log_drains")
	projectBody := `{"ref":"drain-proj","name":"Drain","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/drain-proj/log-drains", `{"target":"https","config":{"header":"x"}}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected missing url 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/drain-proj/log-drains", `{"target":"HTTPS","config":{"url":"https://logs.example.com/ingest","token":"redacted"}}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected log drain create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	if !strings.Contains(createResponse.Body.String(), `"target":"https"`) || !strings.Contains(createResponse.Body.String(), `"url":"https://logs.example.com/ingest"`) {
		t.Fatalf("expected normalized https log drain: %s", createResponse.Body.String())
	}
	if strings.Contains(createResponse.Body.String(), "redacted") || !strings.Contains(createResponse.Body.String(), `"token":"********"`) {
		t.Fatalf("expected masked log drain token in response: %s", createResponse.Body.String())
	}
	drainID := extractString(t, createResponse.Body.String(), "id")
	artifactPath := filepath.Join(projectRoot, "drain-proj", "log-drains", drainID+".toml")
	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("expected rendered log drain artifact: %v", err)
	}
	for _, expected := range []string{`type = "http"`, `uri = "https://logs.example.com/ingest"`, `token = "redacted"`} {
		if !strings.Contains(string(artifact), expected) {
			t.Fatalf("expected artifact to contain %q, got:\n%s", expected, artifact)
		}
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/drain-proj/log-drains", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), drainID) {
		t.Fatalf("expected log drain in list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	if strings.Contains(listResponse.Body.String(), "redacted") || !strings.Contains(listResponse.Body.String(), `"token":"********"`) {
		t.Fatalf("expected masked log drain token in list: %s", listResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/drain-proj/log-drains/"+drainID, "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected log drain delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, err := os.Stat(artifactPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected log drain artifact removed, got err=%v", err)
	}
	listResponse = perform(server, http.MethodGet, "/v1/projects/drain-proj/log-drains", "")
	if strings.Contains(listResponse.Body.String(), drainID) {
		t.Fatalf("expected log drain removed: %s", listResponse.Body.String())
	}
	if body := strings.TrimSpace(listResponse.Body.String()); body != "[]" {
		t.Fatalf("expected empty log drain list, got: %s", body)
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.log_drain_create", "project.log_drain_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/drain-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Log drain created") || !strings.Contains(logsResponse.Body.String(), "Log drain deleted") {
		t.Fatalf("expected log drain project logs: %s", logsResponse.Body.String())
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
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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

func TestCreateHostAndPlaceProject(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	getHostResponse := perform(server, http.MethodGet, "/v1/hosts/"+hostID, "")
	if getHostResponse.Code != http.StatusOK {
		t.Fatalf("expected get host status 200, got %d: %s", getHostResponse.Code, getHostResponse.Body.String())
	}

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"hosted-proj","name":"Hosted","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if !strings.Contains(projectResponse.Body.String(), `"host_id":"`+hostID+`"`) {
		t.Fatalf("expected project host id in response: %s", projectResponse.Body.String())
	}

	hostsResponse := perform(server, http.MethodGet, "/v1/hosts", "")
	for _, expected := range []string{`"cpu":1`, `"ram_mb":2048`, `"disk_gb":20`, `"projects":1`} {
		if !strings.Contains(hostsResponse.Body.String(), expected) {
			t.Fatalf("expected host usage %s: %s", expected, hostsResponse.Body.String())
		}
	}

	conflictResponse := perform(server, http.MethodDelete, "/v1/hosts/"+hostID, "")
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("expected host delete conflict, got %d: %s", conflictResponse.Code, conflictResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/hosted-proj", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected delete 204, got %d", deleteResponse.Code)
	}
	hostsResponse = perform(server, http.MethodGet, "/v1/hosts", "")
	for _, expected := range []string{`"cpu":0`, `"ram_mb":0`, `"disk_gb":0`, `"projects":0`} {
		if !strings.Contains(hostsResponse.Body.String(), expected) {
			t.Fatalf("expected host usage decrement %s: %s", expected, hostsResponse.Body.String())
		}
	}
	deleteHostResponse := perform(server, http.MethodDelete, "/v1/hosts/"+hostID, "")
	if deleteHostResponse.Code != http.StatusNoContent {
		t.Fatalf("expected host delete 204, got %d: %s", deleteHostResponse.Code, deleteHostResponse.Body.String())
	}
	missingHostResponse := perform(server, http.MethodGet, "/v1/hosts/"+hostID, "")
	if missingHostResponse.Code != http.StatusNotFound {
		t.Fatalf("expected deleted host 404, got %d: %s", missingHostResponse.Code, missingHostResponse.Body.String())
	}
}

func TestProjectPlacementRejectsInsufficientHostCapacity(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"tiny","address":"localhost","capacity":{"cpu":1,"ram_mb":2048,"disk_gb":20,"disk_iops":3000,"projects":1}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")

	firstProject := `{"ref":"tiny-one","name":"Tiny One","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	firstResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", firstProject)
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("expected first project status 201, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}

	secondProject := `{"ref":"tiny-two","name":"Tiny Two","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	secondResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", secondProject)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("expected second project capacity conflict, got %d: %s", secondResponse.Code, secondResponse.Body.String())
	}

	iopsHostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"iops-tight","address":"127.0.0.2","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"disk_iops":2999,"projects":10}}`)
	if iopsHostResponse.Code != http.StatusCreated {
		t.Fatalf("expected iops host status 201, got %d: %s", iopsHostResponse.Code, iopsHostResponse.Body.String())
	}
	iopsHostID := extractString(t, iopsHostResponse.Body.String(), "id")
	iopsProject := `{"ref":"iops-one","name":"IOPS One","host_id":"` + iopsHostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	iopsResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", iopsProject)
	if iopsResponse.Code != http.StatusConflict {
		t.Fatalf("expected iops capacity conflict, got %d: %s", iopsResponse.Code, iopsResponse.Body.String())
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

	updateQuotaResponse := perform(server, http.MethodPut, "/v1/orgs/"+orgID+"/quotas", `{"max_projects":1,"max_cpu":1,"max_ram_mb":2048,"max_disk_gb":20,"max_disk_iops":3000}`)
	if updateQuotaResponse.Code != http.StatusOK {
		t.Fatalf("expected quota update 200, got %d: %s", updateQuotaResponse.Code, updateQuotaResponse.Body.String())
	}

	firstProject := `{"ref":"quota-one","name":"Quota One","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	firstResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", firstProject)
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("expected first project status 201, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}

	quotaResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/quotas", "")
	for _, expected := range []string{`"max_projects":1`, `"max_disk_iops":3000`, `"cpu":1`, `"ram_mb":2048`, `"disk_gb":20`, `"disk_iops":3000`, `"projects":1`} {
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
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("expected first project status 201, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
	secondProject := `{"ref":"meter-two","name":"Meter Two","domain":"supadupa.test","profile":"full","resource_tier":"medium"}`
	secondResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", secondProject)
	if secondResponse.Code != http.StatusCreated {
		t.Fatalf("expected second project status 201, got %d: %s", secondResponse.Code, secondResponse.Body.String())
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
		`"cpu":3`,
		`"ram_mb":6144`,
		`"disk_gb":70`,
		`"disk_iops":9000`,
		`"projects":2`,
		`"healthy":2`,
		`"backup_count":1`,
		`"backup_storage_bytes":1048576`,
		`"custom_domains":1`,
		`"log_drains":1`,
		`"secrets":20`,
		`"db_allocated_bytes":75161927680`,
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
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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

func TestFleetMetricsJSONAndPrometheus(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"disk_iops":24000,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"metrics-proj","name":"Metrics","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if _, err := store.CreateBackup(context.Background(), control.BackupInput{ProjectRef: "metrics-proj", Kind: "logical", Location: "memory://metrics", SizeBytes: 2048, Status: "completed"}); err != nil {
		t.Fatalf("create metrics backup: %v", err)
	}
	functionResponse := perform(server, http.MethodPost, "/v1/projects/metrics-proj/functions", `{"name":"hello-api","source":"Deno.serve(() => new Response('ok'))"}`)
	if functionResponse.Code != http.StatusCreated {
		t.Fatalf("expected function deploy 201, got %d: %s", functionResponse.Code, functionResponse.Body.String())
	}
	telemetryResponse := perform(server, http.MethodPost, "/v1/projects/metrics-proj/telemetry", `{"source":"compose","cpu_percent":18.5,"memory_bytes":536870912,"memory_limit_bytes":2147483648,"disk_used_bytes":7516192768,"disk_limit_bytes":21474836480,"network_rx_bytes":123,"network_tx_bytes":456,"sampled_at":"2026-06-05T12:00:00Z"}`)
	if telemetryResponse.Code != http.StatusCreated {
		t.Fatalf("expected telemetry status 201, got %d: %s", telemetryResponse.Code, telemetryResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/metrics-proj/metrics", "")
	if projectMetricsResponse.Code != http.StatusOK {
		t.Fatalf("expected project metrics 200, got %d: %s", projectMetricsResponse.Code, projectMetricsResponse.Body.String())
	}
	for _, expected := range []string{
		`"project_ref":"metrics-proj"`,
		`"status":"healthy"`,
		`"function_deployments":1`,
		`"backups":1`,
		`"backup_storage_bytes":2048`,
		`"disk_iops":3000`,
		`"observed":{"project_ref":"metrics-proj","source":"compose","cpu_percent":18.5`,
		`"memory_bytes":536870912`,
	} {
		if !strings.Contains(projectMetricsResponse.Body.String(), expected) {
			t.Fatalf("expected project metrics value %s: %s", expected, projectMetricsResponse.Body.String())
		}
	}

	jsonResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if jsonResponse.Code != http.StatusOK {
		t.Fatalf("expected metrics json 200, got %d: %s", jsonResponse.Code, jsonResponse.Body.String())
	}
	for _, expected := range []string{
		`"orgs":1`,
		`"hosts":1`,
		`"projects":1`,
		`"healthy":1`,
		`"host_capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"disk_iops":24000,"projects":10}`,
		`"host_used":{"cpu":1,"ram_mb":2048,"disk_gb":20,"disk_iops":3000,"projects":1}`,
		`"observed":{"projects_sampled":1,"cpu_percent":18.5,"memory_bytes":536870912`,
		`"function_deployments":1`,
		`"backups":1`,
		`"backup_storage_bytes":2048`,
		`"audit_verified":true`,
	} {
		if !strings.Contains(jsonResponse.Body.String(), expected) {
			t.Fatalf("expected metrics value %s: %s", expected, jsonResponse.Body.String())
		}
	}

	promResponse := perform(server, http.MethodGet, "/metrics", "")
	if promResponse.Code != http.StatusOK {
		t.Fatalf("expected prometheus metrics 200, got %d: %s", promResponse.Code, promResponse.Body.String())
	}
	for _, expected := range []string{
		"supadupa_projects_total 1",
		"supadupa_projects_by_status{status=\"healthy\"} 1",
		"supadupa_host_capacity_cpu 8",
		"supadupa_host_used_cpu 1",
		"supadupa_host_capacity_disk_iops 24000",
		"supadupa_host_used_disk_iops 3000",
		"supadupa_observed_projects 1",
		"supadupa_observed_cpu_percent 18.5",
		"supadupa_observed_memory_bytes 536870912",
		"supadupa_function_deployments_total 1",
		"supadupa_backup_storage_bytes 2048",
		"supadupa_audit_verified 1",
	} {
		if !strings.Contains(promResponse.Body.String(), expected) {
			t.Fatalf("expected prometheus metric %s: %s", expected, promResponse.Body.String())
		}
	}
}

func TestFleetAdvisorFindingsEndpoint(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"advisor-proj","name":"Advisor","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if _, err := store.UpdateProjectStatus(context.Background(), "advisor-proj", control.ProjectDegraded, "db health check failed"); err != nil {
		t.Fatalf("update project status: %v", err)
	}
	databaseConfigResponse := perform(server, http.MethodPut, "/v1/projects/advisor-proj/config/database", `{"config":{"ssl_enforced":"false"}}`)
	if databaseConfigResponse.Code != http.StatusOK {
		t.Fatalf("expected database config update 200, got %d: %s", databaseConfigResponse.Code, databaseConfigResponse.Body.String())
	}
	backupPolicyResponse := perform(server, http.MethodPut, "/v1/projects/advisor-proj/backups/policy", `{"enabled":false,"schedule":"daily","kind":"logical"}`)
	if backupPolicyResponse.Code != http.StatusOK {
		t.Fatalf("expected backup policy update 200, got %d: %s", backupPolicyResponse.Code, backupPolicyResponse.Body.String())
	}
	bucketResponse := perform(server, http.MethodPost, "/v1/projects/advisor-proj/storage/buckets", `{"name":"public-assets","public":true,"file_size_limit":10485760,"cache_control":"3600"}`)
	if bucketResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage bucket create 201, got %d: %s", bucketResponse.Code, bucketResponse.Body.String())
	}

	response := perform(server, http.MethodGet, "/v1/advisor", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected advisor 200, got %d: %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`"project_ref":"advisor-proj"`,
		`"severity":"critical"`,
		`"title":"Project is not healthy"`,
		`"title":"Backups are disabled"`,
		`"title":"PITR is disabled"`,
		`"title":"Ingress is open to all IPs"`,
		`"title":"Database SSL is not enforced"`,
		`"title":"Fleet advisor mode is not enabled"`,
		`"title":"No log drains configured"`,
		`"title":"Public storage bucket"`,
		`"recommendation":"Inspect project logs and reconcile the project until it returns to healthy."`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("expected advisor value %s: %s", expected, response.Body.String())
		}
	}
}

func TestComplianceReportEndpoint(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "pitr", "log_drains")
	projectBody := `{"ref":"compliance-proj","name":"Compliance","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	networkResponse := perform(server, http.MethodPut, "/v1/projects/compliance-proj/config/network", `{"config":{"ip_allowlist":"10.0.0.0/8","ssl_enforced":"true"}}`)
	if networkResponse.Code != http.StatusOK {
		t.Fatalf("expected network config 200, got %d: %s", networkResponse.Code, networkResponse.Body.String())
	}
	pitrResponse := perform(server, http.MethodPut, "/v1/projects/compliance-proj/pitr/policy", `{"enabled":true,"archive_bucket":"s3://archive/compliance-proj","retention_days":14}`)
	if pitrResponse.Code != http.StatusOK {
		t.Fatalf("expected pitr policy 200, got %d: %s", pitrResponse.Code, pitrResponse.Body.String())
	}
	drainResponse := perform(server, http.MethodPost, "/v1/projects/compliance-proj/log-drains", `{"target":"https","config":{"url":"https://logs.example.com/ingest"}}`)
	if drainResponse.Code != http.StatusCreated {
		t.Fatalf("expected log drain create 201, got %d: %s", drainResponse.Code, drainResponse.Body.String())
	}
	rotateResponse := perform(server, http.MethodPost, "/v1/projects/compliance-proj/keys/rotate", `{"kind":"service_role"}`)
	if rotateResponse.Code != http.StatusOK {
		t.Fatalf("expected secret rotation 200, got %d: %s", rotateResponse.Code, rotateResponse.Body.String())
	}

	response := perform(server, http.MethodGet, "/v1/compliance/report", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected compliance report 200, got %d: %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`"frameworks":["SOC 2","HIPAA"]`,
		`"id":"COM-001"`,
		`"title":"Immutable audit chain"`,
		`"status":"pass"`,
		`"id":"COM-009"`,
		`"status":"manual_review"`,
		`"dpa_posture":"operator-owned: use these controls as evidence for the deploying organization's DPA and BAA posture"`,
		`"certification":"not certified by supadupa; certification remains the operator's responsibility"`,
		`"1/1 projects have backups enabled"`,
		`"1/1 projects have PITR enabled"`,
		`"1/1 projects export logs to a drain"`,
		`"1/1 projects have rotated at least one secret"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("expected compliance report value %s: %s", expected, response.Body.String())
		}
	}
}

func TestProjectReplicationPipelinesCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"repl-proj","name":"Replication","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/repl-proj/replication", `{"name":"bad","type":"etl","source_table":"orders","destination":"bigquery","config":{}}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid replication config 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/repl-proj/replication", `{"name":"orders-etl","type":"etl","source_schema":"public","source_table":"orders","destination":"s3","credential_handle":"secret://projects/repl-proj/etl","config":{"bucket":"analytics-lake","access_key":"secret://projects/repl-proj/s3-access-key"}}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected replication create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	pipelineID := extractString(t, createResponse.Body.String(), "id")
	for _, expected := range []string{
		`"name":"orders-etl"`,
		`"type":"etl"`,
		`"source_schema":"public"`,
		`"source_table":"orders"`,
		`"destination":"s3"`,
		`"credential_handle":"secret://projects/repl-proj/etl"`,
		`"access_key":"********"`,
	} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected create response value %s: %s", expected, createResponse.Body.String())
		}
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/repl-proj/replication", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"orders-etl"`) {
		t.Fatalf("expected replication pipeline in list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/repl-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"replication_pipelines":1`) {
		t.Fatalf("expected project replication metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"replication_pipelines":1`) {
		t.Fatalf("expected org replication usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"replication_pipelines":1`) {
		t.Fatalf("expected fleet replication metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_replication_pipelines_total 1") {
		t.Fatalf("expected prometheus replication metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/repl-proj/replication/"+pipelineID, "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected replication delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	listResponse = perform(server, http.MethodGet, "/v1/projects/repl-proj/replication", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"orders-etl"`) {
		t.Fatalf("expected empty replication list after delete: %d %s", listResponse.Code, listResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/repl-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Replication pipeline configured") || !strings.Contains(logsResponse.Body.String(), "Replication pipeline deleted") {
		t.Fatalf("expected replication project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{`"action":"project.replication_create"`, `"action":"project.replication_delete"`} {
		if !strings.Contains(auditResponse.Body.String(), action) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectVectorAIResourcesCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"ai-proj","name":"AI","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidEmbeddingResponse := perform(server, http.MethodPost, "/v1/projects/ai-proj/embeddings", `{"name":"bad","source_table":"documents","source_column":"body text"}`)
	if invalidEmbeddingResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid embedding 400, got %d: %s", invalidEmbeddingResponse.Code, invalidEmbeddingResponse.Body.String())
	}
	embeddingResponse := perform(server, http.MethodPost, "/v1/projects/ai-proj/embeddings", `{"name":"docs-embeddings","source_schema":"public","source_table":"documents","source_column":"body","primary_key_column":"id","destination_table":"document_embeddings","destination_column":"embedding","provider":"openai","model":"text-embedding-3-small","dimension":1536,"schedule":"manual","batch_size":100}`)
	if embeddingResponse.Code != http.StatusCreated {
		t.Fatalf("expected embedding create 201, got %d: %s", embeddingResponse.Code, embeddingResponse.Body.String())
	}
	embeddingID := extractString(t, embeddingResponse.Body.String(), "id")
	for _, expected := range []string{`"name":"docs-embeddings"`, `"source_table":"documents"`, `"source_column":"body"`, `"provider":"openai"`, `"dimension":1536`, `"status":"configured"`} {
		if !strings.Contains(embeddingResponse.Body.String(), expected) {
			t.Fatalf("expected embedding response value %s: %s", expected, embeddingResponse.Body.String())
		}
	}
	invalidBucketResponse := perform(server, http.MethodPost, "/v1/projects/ai-proj/vector-buckets", `{"name":"documents","storage_backend":"s3","metadata":{"access_key":"raw"}}`)
	if invalidBucketResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid vector bucket 400, got %d: %s", invalidBucketResponse.Code, invalidBucketResponse.Body.String())
	}
	bucketResponse := perform(server, http.MethodPost, "/v1/projects/ai-proj/vector-buckets", `{"name":"documents","dimension":1536,"distance":"cosine","index_method":"hnsw","storage_backend":"s3","storage_uri":"s3://vector-buckets/documents","metadata":{"purpose":"semantic-search","access_key":"secret://projects/ai-proj/vector-s3"}}`)
	if bucketResponse.Code != http.StatusCreated {
		t.Fatalf("expected vector bucket create 201, got %d: %s", bucketResponse.Code, bucketResponse.Body.String())
	}
	for _, expected := range []string{`"name":"documents"`, `"distance":"cosine"`, `"index_method":"hnsw"`, `"storage_backend":"s3"`, `"access_key":"********"`} {
		if !strings.Contains(bucketResponse.Body.String(), expected) {
			t.Fatalf("expected vector bucket response value %s: %s", expected, bucketResponse.Body.String())
		}
	}

	embeddingsList := perform(server, http.MethodGet, "/v1/projects/ai-proj/embeddings", "")
	if embeddingsList.Code != http.StatusOK || !strings.Contains(embeddingsList.Body.String(), `"name":"docs-embeddings"`) {
		t.Fatalf("expected embedding list: %d %s", embeddingsList.Code, embeddingsList.Body.String())
	}
	bucketsList := perform(server, http.MethodGet, "/v1/projects/ai-proj/vector-buckets", "")
	if bucketsList.Code != http.StatusOK || !strings.Contains(bucketsList.Body.String(), `"name":"documents"`) || !strings.Contains(bucketsList.Body.String(), `"access_key":"********"`) {
		t.Fatalf("expected vector bucket list: %d %s", bucketsList.Code, bucketsList.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/ai-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"embedding_jobs":1`) || !strings.Contains(projectMetricsResponse.Body.String(), `"vector_buckets":1`) {
		t.Fatalf("expected project vector ai metrics: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"embedding_jobs":1`) || !strings.Contains(usageResponse.Body.String(), `"vector_buckets":1`) {
		t.Fatalf("expected org vector ai usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"embedding_jobs":1`) || !strings.Contains(fleetResponse.Body.String(), `"vector_buckets":1`) {
		t.Fatalf("expected fleet vector ai metrics: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_embedding_jobs_total 1") || !strings.Contains(prometheusResponse.Body.String(), "supadupa_vector_buckets_total 1") {
		t.Fatalf("expected prometheus vector ai metrics: %s", prometheusResponse.Body.String())
	}

	deleteEmbeddingResponse := perform(server, http.MethodDelete, "/v1/projects/ai-proj/embeddings/"+embeddingID, "")
	if deleteEmbeddingResponse.Code != http.StatusNoContent {
		t.Fatalf("expected embedding delete 204, got %d: %s", deleteEmbeddingResponse.Code, deleteEmbeddingResponse.Body.String())
	}
	deleteBucketResponse := perform(server, http.MethodDelete, "/v1/projects/ai-proj/vector-buckets/documents", "")
	if deleteBucketResponse.Code != http.StatusNoContent {
		t.Fatalf("expected vector bucket delete 204, got %d: %s", deleteBucketResponse.Code, deleteBucketResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/ai-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Embedding job configured") || !strings.Contains(logsResponse.Body.String(), "Vector bucket configured") || !strings.Contains(logsResponse.Body.String(), "Embedding job deleted") || !strings.Contains(logsResponse.Body.String(), "Vector bucket deleted") {
		t.Fatalf("expected vector ai project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.embedding_create", "project.vector_bucket_create", "project.embedding_delete", "project.vector_bucket_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectAnalyticsBucketsCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"analytics-proj","name":"Analytics","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/analytics-proj/analytics-buckets", `{"name":"events","storage_uri":"http://bucket/path"}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid analytics bucket 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	rawSecretResponse := perform(server, http.MethodPost, "/v1/projects/analytics-proj/analytics-buckets", `{"name":"events","storage_uri":"s3://lakehouse/events","metadata":{"access_key":"raw"}}`)
	if rawSecretResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected raw analytics metadata secret 400, got %d: %s", rawSecretResponse.Code, rawSecretResponse.Body.String())
	}
	createResponse := perform(server, http.MethodPost, "/v1/projects/analytics-proj/analytics-buckets", `{"name":"events","storage_uri":"s3://lakehouse/events","catalog_uri":"http://iceberg-rest:8181","warehouse":"analytics","credential_handle":"secret://projects/analytics-proj/iceberg","format_version":2,"partitioning":"days(created_at)","retention_days":365,"compaction_schedule":"0 2 * * *","metadata":{"purpose":"warehouse","access_key":"secret://projects/analytics-proj/s3"}}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected analytics bucket create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"events"`, `"storage_uri":"s3://lakehouse/events"`, `"catalog_uri":"http://iceberg-rest:8181"`, `"credential_handle":"********"`, `"format_version":2`, `"partitioning":"days(created_at)"`, `"access_key":"********"`, `"status":"configured"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected analytics bucket response value %s: %s", expected, createResponse.Body.String())
		}
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/analytics-proj/analytics-buckets", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"events"`) || !strings.Contains(listResponse.Body.String(), `"credential_handle":"********"`) {
		t.Fatalf("expected analytics bucket list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/analytics-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"analytics_buckets":1`) {
		t.Fatalf("expected project analytics metrics: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"analytics_buckets":1`) {
		t.Fatalf("expected org analytics usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"analytics_buckets":1`) {
		t.Fatalf("expected fleet analytics metrics: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_analytics_buckets_total 1") {
		t.Fatalf("expected prometheus analytics metrics: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/analytics-proj/analytics-buckets/events", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected analytics bucket delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/analytics-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Analytics bucket configured") || !strings.Contains(logsResponse.Body.String(), "Analytics bucket deleted") {
		t.Fatalf("expected analytics bucket project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.analytics_bucket_create", "project.analytics_bucket_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectStorageBucketsCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"storage-proj","name":"Storage","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/storage-proj/storage/buckets", `{"name":"assets","allowed_mime_types":["not-a-mime"]}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid bucket 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	createResponse := perform(server, http.MethodPost, "/v1/projects/storage-proj/storage/buckets", `{"name":"assets","public":true,"file_size_limit":1048576,"allowed_mime_types":["image/png","image/jpeg"],"cache_control":"600","avif_autodetection":true,"metadata":{"purpose":"public-assets","access_key":"secret://projects/storage-proj/storage-s3"}}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage bucket create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"assets"`, `"public":true`, `"file_size_limit":1048576`, `"cache_control":"600"`, `"avif_autodetection":true`, `"access_key":"********"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected storage bucket response value %s: %s", expected, createResponse.Body.String())
		}
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/storage-proj/storage/buckets", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"assets"`) || !strings.Contains(listResponse.Body.String(), `"access_key":"********"`) {
		t.Fatalf("expected storage bucket list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/storage-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"storage_buckets":1`) {
		t.Fatalf("expected project storage bucket metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"storage_buckets":1`) {
		t.Fatalf("expected org storage bucket usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"storage_buckets":1`) {
		t.Fatalf("expected fleet storage bucket metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_storage_buckets_total 1") {
		t.Fatalf("expected prometheus storage bucket metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/storage-proj/storage/buckets/assets", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected storage bucket delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/storage-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Storage bucket configured") || !strings.Contains(logsResponse.Body.String(), "Storage bucket deleted") {
		t.Fatalf("expected storage bucket project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.storage_bucket_create", "project.storage_bucket_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDatabaseExtensionsListUpdateMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"ext-proj","name":"Extensions","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/ext-proj/database/extensions", "")
	for _, expected := range []string{`"name":"pg_graphql"`, `"schema":"graphql"`, `"name":"vector"`, `"name":"supabase_vault"`, `"enabled":true`} {
		if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), expected) {
			t.Fatalf("expected default extension %s: %d %s", expected, listResponse.Code, listResponse.Body.String())
		}
	}
	invalidResponse := perform(server, http.MethodPut, "/v1/projects/ext-proj/database/extensions/not_real", `{"enabled":true}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported extension 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	updateResponse := perform(server, http.MethodPut, "/v1/projects/ext-proj/database/extensions/pg_cron", `{"enabled":false,"schema":"extensions","version":"1.6"}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected extension update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	for _, expected := range []string{`"name":"pg_cron"`, `"schema":"extensions"`, `"version":"1.6"`, `"enabled":false`, `"status":"disabled"`} {
		if !strings.Contains(updateResponse.Body.String(), expected) {
			t.Fatalf("expected extension update value %s: %s", expected, updateResponse.Body.String())
		}
	}
	listAfterUpdate := perform(server, http.MethodGet, "/v1/projects/ext-proj/database/extensions", "")
	if !strings.Contains(listAfterUpdate.Body.String(), `"name":"pg_cron"`) || !strings.Contains(listAfterUpdate.Body.String(), `"enabled":false`) {
		t.Fatalf("expected extension override in list: %s", listAfterUpdate.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/ext-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_extensions":7`) {
		t.Fatalf("expected project enabled extension metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_extensions":7`) {
		t.Fatalf("expected org enabled extension usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_extensions":7`) {
		t.Fatalf("expected fleet enabled extension metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_extensions_enabled_total 7") {
		t.Fatalf("expected prometheus enabled extension metric: %s", prometheusResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/ext-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Database extension updated") {
		t.Fatalf("expected extension project log: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.database_extension_update"`) {
		t.Fatalf("expected extension audit action: %s", auditResponse.Body.String())
	}
}

func TestProjectDatabaseCronJobsCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"cron-proj","name":"Cron","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidSchedule := perform(server, http.MethodPost, "/v1/projects/cron-proj/database/cron-jobs", `{"name":"bad-job","schedule":"* * *","command":"select 1","active":true}`)
	if invalidSchedule.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid schedule 400, got %d: %s", invalidSchedule.Code, invalidSchedule.Body.String())
	}
	invalidSecret := perform(server, http.MethodPost, "/v1/projects/cron-proj/database/cron-jobs", `{"name":"bad-secret","schedule":"*/5 * * * *","command":"select 1","active":true,"metadata":{"password":"raw"}}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret metadata 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}

	createBody := `{"name":"refresh-rollups","schedule":"*/15 * * * *","command":"select analytics.refresh_rollups();","database":"postgres","username":"postgres","active":true,"timeout_seconds":90,"max_runtime_seconds":120,"metadata":{"owner":"analytics","password":"secret://projects/cron-proj/db/cron-password"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/cron-proj/database/cron-jobs", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected cron job create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"refresh-rollups"`, `"schedule":"*/15 * * * *"`, `"command":"select analytics.refresh_rollups();"`, `"database":"postgres"`, `"username":"postgres"`, `"active":true`, `"timeout_seconds":90`, `"max_runtime_seconds":120`, `"owner":"analytics"`, `"password":"********"`, `"status":"scheduled"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected cron create value %s: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "secret://projects") {
		t.Fatalf("expected cron metadata secret to be masked: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/cron-proj/database/cron-jobs", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"refresh-rollups"`) || !strings.Contains(listResponse.Body.String(), `"password":"********"`) {
		t.Fatalf("expected masked cron job list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/cron-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_cron_jobs":1`) {
		t.Fatalf("expected project cron metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_cron_jobs":1`) {
		t.Fatalf("expected org cron usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_cron_jobs":1`) {
		t.Fatalf("expected fleet cron metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_cron_jobs_total 1") {
		t.Fatalf("expected prometheus cron metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/cron-proj/database/cron-jobs/refresh-rollups", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected cron job delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/cron-proj/logs", "")
	for _, expected := range []string{"Database cron job configured", "Database cron job deleted"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected cron project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.database_cron_create", "project.database_cron_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectAuthClientsCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"authclient-proj","name":"Auth Clients","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidSecret := perform(server, http.MethodPost, "/v1/projects/authclient-proj/auth/clients", `{"name":"Bad Secret","client_secret_handle":"raw-secret","redirect_uris":["https://app.example.com/callback"],"confidential":true}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret handle 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}
	invalidRedirect := perform(server, http.MethodPost, "/v1/projects/authclient-proj/auth/clients", `{"name":"Bad Redirect","redirect_uris":["ftp://app.example.com/callback"],"confidential":false}`)
	if invalidRedirect.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid redirect 400, got %d: %s", invalidRedirect.Code, invalidRedirect.Body.String())
	}

	createBody := `{"name":"Dashboard App","client_id":"dashboard_app","client_secret_handle":"secret://projects/authclient-proj/auth/dashboard-app","redirect_uris":["https://app.example.com/auth/callback"],"grant_types":["authorization_code","refresh_token"],"scopes":["openid","email","profile"],"confidential":true}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/authclient-proj/auth/clients", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected auth client create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"Dashboard App"`, `"client_id":"dashboard_app"`, `"client_secret_handle":"********"`, `"redirect_uris":["https://app.example.com/auth/callback"]`, `"confidential":true`, `"status":"registered"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected auth client create value %s: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "secret://projects") {
		t.Fatalf("expected auth client secret handle to be masked: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/authclient-proj/auth/clients", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"client_secret_handle":"********"`) {
		t.Fatalf("expected masked auth client list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/authclient-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"auth_clients":1`) {
		t.Fatalf("expected project auth client metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"auth_clients":1`) {
		t.Fatalf("expected org auth client usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"auth_clients":1`) {
		t.Fatalf("expected fleet auth client metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_auth_clients_total 1") {
		t.Fatalf("expected prometheus auth client metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/authclient-proj/auth/clients/dashboard_app", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected auth client delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	listAfterDelete := perform(server, http.MethodGet, "/v1/projects/authclient-proj/auth/clients", "")
	if listAfterDelete.Code != http.StatusOK || strings.Contains(listAfterDelete.Body.String(), `"client_id":"dashboard_app"`) {
		t.Fatalf("expected auth client removed, got %d: %s", listAfterDelete.Code, listAfterDelete.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/authclient-proj/logs", "")
	for _, expected := range []string{"Auth client registered", "Auth client deleted"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected auth client project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.auth_client_create", "project.auth_client_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectAuthHooksCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"authhook-proj","name":"Auth Hooks","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidSecret := perform(server, http.MethodPost, "/v1/projects/authhook-proj/auth/hooks", `{"hook_type":"custom_access_token","enabled":true,"target_uri":"https://hooks.example.com/token","secret_handle":"raw-secret"}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret handle 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}
	invalidHeader := perform(server, http.MethodPost, "/v1/projects/authhook-proj/auth/hooks", `{"hook_type":"custom_access_token","enabled":true,"target_uri":"https://hooks.example.com/token","headers":{"authorization":"Bearer raw"}}`)
	if invalidHeader.Code != http.StatusBadRequest {
		t.Fatalf("expected raw authorization header 400, got %d: %s", invalidHeader.Code, invalidHeader.Body.String())
	}

	createBody := `{"hook_type":"custom_access_token","enabled":true,"target_uri":"https://hooks.example.com/token","secret_handle":"secret://projects/authhook-proj/auth/hook-secret","headers":{"authorization":"secret://projects/authhook-proj/auth/hook-auth","x-trace":"supadupa"},"timeout_ms":7000,"retry_attempts":2}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/authhook-proj/auth/hooks", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected auth hook create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"hook_type":"custom_access_token"`, `"enabled":true`, `"target_uri":"https://hooks.example.com/token"`, `"secret_handle":"********"`, `"authorization":"********"`, `"x-trace":"supadupa"`, `"timeout_ms":7000`, `"retry_attempts":2`, `"status":"configured"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected auth hook create value %s: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "secret://projects") {
		t.Fatalf("expected auth hook secret handles to be masked: %s", createResponse.Body.String())
	}
	if provisioner.syncedAuthHooksRef != "authhook-proj" || len(provisioner.syncedAuthHooks) != 1 || provisioner.syncedAuthHooks[0].HookType != "custom_access_token" {
		t.Fatalf("expected auth hook create to sync runtime hooks, got ref=%s hooks=%#v", provisioner.syncedAuthHooksRef, provisioner.syncedAuthHooks)
	}
	if provisioner.syncedAuthHooks[0].SecretHandle != "secret://projects/authhook-proj/auth/hook-secret" || provisioner.syncedAuthHooks[0].Headers["authorization"] != "secret://projects/authhook-proj/auth/hook-auth" {
		t.Fatalf("expected auth hook runtime sync to keep secret handles, got %#v", provisioner.syncedAuthHooks[0])
	}

	updateResponse := perform(server, http.MethodPost, "/v1/projects/authhook-proj/auth/hooks", `{"hook_type":"custom_access_token","enabled":false,"edge_function":"token-hook","headers":{"x-trace":"disabled"}}`)
	if updateResponse.Code != http.StatusCreated || !strings.Contains(updateResponse.Body.String(), `"enabled":false`) || !strings.Contains(updateResponse.Body.String(), `"edge_function":"token-hook"`) {
		t.Fatalf("expected auth hook update by hook_type, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	if len(provisioner.syncedAuthHooks) != 1 || provisioner.syncedAuthHooks[0].Enabled || provisioner.syncedAuthHooks[0].EdgeFunction != "token-hook" {
		t.Fatalf("expected auth hook update to sync updated runtime hooks, got %#v", provisioner.syncedAuthHooks)
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/authhook-proj/auth/hooks", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"hook_type":"custom_access_token"`) {
		t.Fatalf("expected auth hook list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/authhook-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"auth_hooks":1`) {
		t.Fatalf("expected project auth hook metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"auth_hooks":1`) {
		t.Fatalf("expected org auth hook usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"auth_hooks":1`) {
		t.Fatalf("expected fleet auth hook metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_auth_hooks_total 1") {
		t.Fatalf("expected prometheus auth hook metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/authhook-proj/auth/hooks/custom_access_token", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected auth hook delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if provisioner.syncedAuthHooksRef != "authhook-proj" || len(provisioner.syncedAuthHooks) != 0 {
		t.Fatalf("expected auth hook delete to sync empty runtime hooks, got ref=%s hooks=%#v", provisioner.syncedAuthHooksRef, provisioner.syncedAuthHooks)
	}
	listAfterDelete := perform(server, http.MethodGet, "/v1/projects/authhook-proj/auth/hooks", "")
	if listAfterDelete.Code != http.StatusOK || strings.Contains(listAfterDelete.Body.String(), `"hook_type":"custom_access_token"`) {
		t.Fatalf("expected auth hook removed, got %d: %s", listAfterDelete.Code, listAfterDelete.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/authhook-proj/logs", "")
	for _, expected := range []string{"Auth hook configured", "Auth hook deleted"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected auth hook project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.auth_hook_create", "project.auth_hook_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDatabaseQueuesCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"queue-proj","name":"Queues","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidRetention := perform(server, http.MethodPost, "/v1/projects/queue-proj/database/queues", `{"name":"events","retention_minutes":0,"visibility_timeout_seconds":90000,"active":true}`)
	if invalidRetention.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid visibility timeout 400, got %d: %s", invalidRetention.Code, invalidRetention.Body.String())
	}
	invalidSecret := perform(server, http.MethodPost, "/v1/projects/queue-proj/database/queues", `{"name":"events","active":true,"metadata":{"token":"raw"}}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret metadata 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}

	createBody := `{"name":"events","schema":"pgmq","retention_minutes":10080,"visibility_timeout_seconds":45,"max_retries":7,"dead_letter_queue":"events-dlq","active":true,"metadata":{"owner":"backend","token":"secret://projects/queue-proj/db/pgmq-token"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/queue-proj/database/queues", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected database queue create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"events"`, `"schema":"pgmq"`, `"retention_minutes":10080`, `"visibility_timeout_seconds":45`, `"max_retries":7`, `"dead_letter_queue":"events-dlq"`, `"active":true`, `"owner":"backend"`, `"token":"********"`, `"status":"ready"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected queue create value %s: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "secret://projects") {
		t.Fatalf("expected queue metadata secret to be masked: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/queue-proj/database/queues", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"events"`) || !strings.Contains(listResponse.Body.String(), `"token":"********"`) {
		t.Fatalf("expected masked queue list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/queue-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_queues":1`) {
		t.Fatalf("expected project queue metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_queues":1`) {
		t.Fatalf("expected org queue usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_queues":1`) {
		t.Fatalf("expected fleet queue metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_queues_total 1") {
		t.Fatalf("expected prometheus queue metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/queue-proj/database/queues/events", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected database queue delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/queue-proj/logs", "")
	for _, expected := range []string{"Database queue configured", "Database queue deleted"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected queue project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.database_queue_create", "project.database_queue_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDatabaseWebhooksCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"webhook-proj","name":"Webhooks","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidEndpoint := perform(server, http.MethodPost, "/v1/projects/webhook-proj/database/webhooks", `{"name":"orders-events","schema":"public","table":"orders","endpoint":"http://hooks.example.com/orders","active":true}`)
	if invalidEndpoint.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid endpoint 400, got %d: %s", invalidEndpoint.Code, invalidEndpoint.Body.String())
	}
	invalidSecret := perform(server, http.MethodPost, "/v1/projects/webhook-proj/database/webhooks", `{"name":"orders-events","schema":"public","table":"orders","endpoint":"https://hooks.example.com/orders","active":true,"headers":{"Authorization":"Bearer raw"}}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret header 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}

	createBody := `{"name":"orders-events","schema":"public","table":"orders","events":["insert","update"],"endpoint":"https://hooks.example.com/orders","http_method":"POST","headers":{"Authorization":"secret://projects/webhook-proj/webhooks/orders-token","X-Source":"supadupa"},"timeout_seconds":15,"retry_count":5,"active":true,"metadata":{"owner":"backend","token":"secret://projects/webhook-proj/webhooks/meta-token"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/webhook-proj/database/webhooks", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected database webhook create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"orders-events"`, `"schema":"public"`, `"table":"orders"`, `"events":["insert","update"]`, `"endpoint":"https://hooks.example.com/orders"`, `"http_method":"POST"`, `"authorization":"********"`, `"x-source":"supadupa"`, `"timeout_seconds":15`, `"retry_count":5`, `"active":true`, `"owner":"backend"`, `"token":"********"`, `"status":"ready"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected webhook create value %s: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "secret://projects") {
		t.Fatalf("expected webhook secrets to be masked: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/webhook-proj/database/webhooks", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"orders-events"`) || !strings.Contains(listResponse.Body.String(), `"authorization":"********"`) {
		t.Fatalf("expected masked webhook list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/webhook-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_webhooks":1`) {
		t.Fatalf("expected project webhook metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_webhooks":1`) {
		t.Fatalf("expected org webhook usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_webhooks":1`) {
		t.Fatalf("expected fleet webhook metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_webhooks_total 1") {
		t.Fatalf("expected prometheus webhook metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/webhook-proj/database/webhooks/orders-events", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected database webhook delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/webhook-proj/logs", "")
	for _, expected := range []string{"Database webhook configured", "Database webhook deleted"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected webhook project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.database_webhook_create", "project.database_webhook_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDatabaseSchemasCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"schema-proj","name":"Schemas","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidSQL := perform(server, http.MethodPost, "/v1/projects/schema-proj/database/schemas", `{"name":"app-schema","version":"20260605_001","schema":"public","sql":"","active":true}`)
	if invalidSQL.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid SQL 400, got %d: %s", invalidSQL.Code, invalidSQL.Body.String())
	}
	invalidSecret := perform(server, http.MethodPost, "/v1/projects/schema-proj/database/schemas", `{"name":"app-schema","version":"20260605_001","schema":"public","sql":"create table public.accounts(id uuid primary key);","active":true,"metadata":{"token":"raw"}}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret metadata 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}

	createBody := `{"name":"app-schema","version":"20260605_001","schema":"public","sql":"create table public.accounts(id uuid primary key);","apply_order":10,"active":true,"metadata":{"owner":"backend","token":"secret://projects/schema-proj/db/schema-token"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/schema-proj/database/schemas", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected database schema create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"app-schema"`, `"version":"20260605_001"`, `"schema":"public"`, `"sql":"create table public.accounts(id uuid primary key);"`, `"apply_order":10`, `"active":true`, `"owner":"backend"`, `"token":"********"`, `"status":"pending"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected schema create value %s: %s", expected, createResponse.Body.String())
		}
	}
	checksum := extractString(t, createResponse.Body.String(), "checksum")
	if len(checksum) != 64 {
		t.Fatalf("expected sha256 checksum, got %q", checksum)
	}
	if strings.Contains(createResponse.Body.String(), "secret://projects") {
		t.Fatalf("expected schema metadata secret to be masked: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/schema-proj/database/schemas", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"app-schema"`) || !strings.Contains(listResponse.Body.String(), `"token":"********"`) {
		t.Fatalf("expected masked schema list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/schema-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_schemas":1`) {
		t.Fatalf("expected project schema metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_schemas":1`) {
		t.Fatalf("expected org schema usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_schemas":1`) {
		t.Fatalf("expected fleet schema metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_schemas_total 1") {
		t.Fatalf("expected prometheus schema metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/schema-proj/database/schemas/app-schema/20260605_001", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected database schema delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/schema-proj/logs", "")
	for _, expected := range []string{"Declarative schema recorded", "Declarative schema deleted"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected schema project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.database_schema_create", "project.database_schema_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDatabaseRolesCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"dbrole-proj","name":"Database Roles","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	reservedResponse := perform(server, http.MethodPost, "/v1/projects/dbrole-proj/database/roles", `{"name":"service_role","login":true,"password_secret_handle":"secret://projects/dbrole-proj/db/app-role"}`)
	if reservedResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected reserved role 400, got %d: %s", reservedResponse.Code, reservedResponse.Body.String())
	}
	invalidSecretResponse := perform(server, http.MethodPost, "/v1/projects/dbrole-proj/database/roles", `{"name":"app_writer","login":true,"password_secret_handle":"raw-password"}`)
	if invalidSecretResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected raw password handle 400, got %d: %s", invalidSecretResponse.Code, invalidSecretResponse.Body.String())
	}
	createBody := `{"name":"app_writer","login":true,"bypass_rls":false,"connection_limit":25,"password_secret_handle":"secret://projects/dbrole-proj/db/app-writer","member_of":["authenticated"],"schema_grants":{"public":"usage,select,insert,update"},"metadata":{"purpose":"application-writes","api_key":"secret://projects/dbrole-proj/db-role-api"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/dbrole-proj/database/roles", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected database role create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"app_writer"`, `"login":true`, `"connection_limit":25`, `"password_secret_handle":"********"`, `"api_key":"********"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected database role response value %s: %s", expected, createResponse.Body.String())
		}
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/dbrole-proj/database/roles", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"app_writer"`) || !strings.Contains(listResponse.Body.String(), `"password_secret_handle":"********"`) {
		t.Fatalf("expected database role list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/dbrole-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_roles":1`) {
		t.Fatalf("expected project database role metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_roles":1`) {
		t.Fatalf("expected org database role usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_roles":1`) {
		t.Fatalf("expected fleet database role metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_roles_total 1") {
		t.Fatalf("expected prometheus database role metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/dbrole-proj/database/roles/app_writer", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected database role delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/dbrole-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Database role configured") || !strings.Contains(logsResponse.Body.String(), "Database role deleted") {
		t.Fatalf("expected database role project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.database_role_create", "project.database_role_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectCDNPolicyInvalidationsRoutesMetricsAndAudit(t *testing.T) {
	t.Setenv("SUPADUPA_ROUTES_ROOT", t.TempDir())
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"cdn-proj","name":"CDN","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	defaultResponse := perform(server, http.MethodGet, "/v1/projects/cdn-proj/cdn/policy", "")
	if defaultResponse.Code != http.StatusOK || !strings.Contains(defaultResponse.Body.String(), `"enabled":false`) {
		t.Fatalf("expected default cdn policy: %d %s", defaultResponse.Code, defaultResponse.Body.String())
	}
	invalidPolicyResponse := perform(server, http.MethodPut, "/v1/projects/cdn-proj/cdn/policy", `{"enabled":true,"included_paths":["storage/*"]}`)
	if invalidPolicyResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid cdn policy 400, got %d: %s", invalidPolicyResponse.Code, invalidPolicyResponse.Body.String())
	}
	updateResponse := perform(server, http.MethodPut, "/v1/projects/cdn-proj/cdn/policy", `{"enabled":true,"browser_ttl_seconds":300,"edge_ttl_seconds":600,"stale_while_revalidate_seconds":30,"included_paths":["/storage/v1/object/public/*"],"smart_revalidation":true}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected cdn policy update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	for _, expected := range []string{
		`"enabled":true`,
		`"browser_ttl_seconds":300`,
		`"edge_ttl_seconds":600`,
		`"smart_revalidation":true`,
		`"cache_control":"public, max-age=300, s-maxage=600, stale-while-revalidate=30"`,
	} {
		if !strings.Contains(updateResponse.Body.String(), expected) {
			t.Fatalf("expected cdn policy value %s: %s", expected, updateResponse.Body.String())
		}
	}
	routesResponse := perform(server, http.MethodGet, "/v1/projects/cdn-proj/routes", "")
	if routesResponse.Code != http.StatusOK || !strings.Contains(routesResponse.Body.String(), `"cache_control":"public, max-age=300, s-maxage=600, stale-while-revalidate=30"`) || !strings.Contains(routesResponse.Body.String(), `"smart_cdn":true`) {
		t.Fatalf("expected cdn route metadata: %d %s", routesResponse.Code, routesResponse.Body.String())
	}
	badInvalidationResponse := perform(server, http.MethodPost, "/v1/projects/cdn-proj/cdn/invalidations", `{"paths":[]}`)
	if badInvalidationResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected empty invalidation 400, got %d: %s", badInvalidationResponse.Code, badInvalidationResponse.Body.String())
	}
	invalidationResponse := perform(server, http.MethodPost, "/v1/projects/cdn-proj/cdn/invalidations", `{"paths":["/storage/v1/object/public/avatar.png","/storage/v1/object/public/*"]}`)
	if invalidationResponse.Code != http.StatusCreated {
		t.Fatalf("expected invalidation 201, got %d: %s", invalidationResponse.Code, invalidationResponse.Body.String())
	}
	if !strings.Contains(invalidationResponse.Body.String(), `"status":"completed"`) || !strings.Contains(invalidationResponse.Body.String(), `"source":"manual"`) || !strings.Contains(invalidationResponse.Body.String(), `/storage/v1/object/public/avatar.png`) {
		t.Fatalf("expected completed invalidation: %s", invalidationResponse.Body.String())
	}
	bucketResponse := perform(server, http.MethodPost, "/v1/projects/cdn-proj/storage/buckets", `{"name":"assets","public":true,"cache_control":"public, max-age=300"}`)
	if bucketResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage bucket 201, got %d: %s", bucketResponse.Code, bucketResponse.Body.String())
	}
	objectEventResponse := perform(server, http.MethodPost, "/v1/projects/cdn-proj/cdn/object-events", `{"event_id":"evt-1","bucket":"assets","object_path":"avatars/user.png","event_type":"object_updated"}`)
	if objectEventResponse.Code != http.StatusCreated {
		t.Fatalf("expected object event invalidation 201, got %d: %s", objectEventResponse.Code, objectEventResponse.Body.String())
	}
	for _, expected := range []string{`"source":"storage_object_event"`, `"event_id":"evt-1"`, `/storage/v1/object/public/assets/avatars/user.png`, "smart cdn revalidation recorded"} {
		if !strings.Contains(objectEventResponse.Body.String(), expected) {
			t.Fatalf("expected object event invalidation value %s: %s", expected, objectEventResponse.Body.String())
		}
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/cdn-proj/cdn/invalidations", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"status":"completed"`) || !strings.Contains(listResponse.Body.String(), `"source":"storage_object_event"`) {
		t.Fatalf("expected invalidation in list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/cdn-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"cdn_enabled":true`) || !strings.Contains(projectMetricsResponse.Body.String(), `"cdn_invalidations":2`) {
		t.Fatalf("expected project cdn metrics: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"cdn_enabled_projects":1`) || !strings.Contains(usageResponse.Body.String(), `"cdn_invalidations":2`) {
		t.Fatalf("expected org cdn usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"cdn_enabled_projects":1`) || !strings.Contains(fleetResponse.Body.String(), `"cdn_invalidations":2`) {
		t.Fatalf("expected fleet cdn metrics: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_cdn_enabled_projects_total 1") || !strings.Contains(prometheusResponse.Body.String(), "supadupa_cdn_invalidations_total 2") {
		t.Fatalf("expected prometheus cdn metrics: %s", prometheusResponse.Body.String())
	}
	activityResponse := perform(server, http.MethodGet, "/v1/projects/cdn-proj/activity", "")
	if !strings.Contains(activityResponse.Body.String(), "project.cdn_policy_update") || !strings.Contains(activityResponse.Body.String(), "project.cdn_invalidate") || !strings.Contains(activityResponse.Body.String(), "project.cdn_object_revalidate") {
		t.Fatalf("expected cdn audit activity: %s", activityResponse.Body.String())
	}
}

func TestProjectNetworkConnectionsCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "network_restrictions")
	projectBody := `{"ref":"net-proj","name":"Network","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidCIDRResponse := perform(server, http.MethodPost, "/v1/projects/net-proj/network-connections", `{"name":"bad-cidr","cidrs":["not-a-cidr"]}`)
	if invalidCIDRResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid CIDR status 400, got %d: %s", invalidCIDRResponse.Code, invalidCIDRResponse.Body.String())
	}
	rawSecretResponse := perform(server, http.MethodPost, "/v1/projects/net-proj/network-connections", `{"name":"raw-secret","cidrs":["10.0.0.0/16"],"config":{"token":"plain-token"}}`)
	if rawSecretResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected raw sensitive config status 400, got %d: %s", rawSecretResponse.Code, rawSecretResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/net-proj/network-connections", `{"name":"aws-prod","type":"privatelink","provider":"aws","region":"us-east-1","cidrs":["10.0.0.0/16","203.0.113.10"],"endpoint_id":"vpce-123","config":{"account_id":"123456789012","token":"secret://projects/net-proj/private-link-token"}}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected network connection status 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	connectionID := extractString(t, createResponse.Body.String(), "id")
	for _, expected := range []string{
		`"name":"aws-prod"`,
		`"type":"privatelink"`,
		`"provider":"aws"`,
		`"region":"us-east-1"`,
		`"endpoint_id":"vpce-123"`,
		`"10.0.0.0/16"`,
		`"203.0.113.10"`,
		`"token":"********"`,
		`"status":"requested"`,
	} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected create response value %s: %s", expected, createResponse.Body.String())
		}
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/net-proj/network-connections", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"aws-prod"`) || !strings.Contains(listResponse.Body.String(), `"token":"********"`) {
		t.Fatalf("expected network connection in list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	networkConfigResponse := perform(server, http.MethodPut, "/v1/projects/net-proj/config/network", `{"config":{"ip_allowlist":"10.0.0.0/8,203.0.113.10/32","ssl_enforced":"true"}}`)
	if networkConfigResponse.Code != http.StatusOK {
		t.Fatalf("expected network config update 200, got %d: %s", networkConfigResponse.Code, networkConfigResponse.Body.String())
	}
	networkResponse := perform(server, http.MethodGet, "/v1/projects/net-proj/network", "")
	if networkResponse.Code != http.StatusOK {
		t.Fatalf("expected network alias status 200, got %d: %s", networkResponse.Code, networkResponse.Body.String())
	}
	for _, expected := range []string{
		`"project_ref":"net-proj"`,
		`"area":"network"`,
		`"ip_allowlist":"10.0.0.0/8,203.0.113.10/32"`,
		`"ssl_enforced":"true"`,
		`"connections":[`,
		`"name":"aws-prod"`,
		`"token":"********"`,
	} {
		if !strings.Contains(networkResponse.Body.String(), expected) {
			t.Fatalf("expected network alias value %s: %s", expected, networkResponse.Body.String())
		}
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/net-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"network_connections":1`) {
		t.Fatalf("expected project network metrics: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"network_connections":1`) {
		t.Fatalf("expected org network usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"network_connections":1`) {
		t.Fatalf("expected fleet network metrics: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_network_connections_total 1") {
		t.Fatalf("expected prometheus network metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/net-proj/network-connections/"+connectionID, "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected network connection delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	emptyListResponse := perform(server, http.MethodGet, "/v1/projects/net-proj/network-connections", "")
	if emptyListResponse.Code != http.StatusOK || strings.Contains(emptyListResponse.Body.String(), `"name":"aws-prod"`) {
		t.Fatalf("expected empty network connection list: %d %s", emptyListResponse.Code, emptyListResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/net-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Private network connection requested") || !strings.Contains(logsResponse.Body.String(), "Private network connection removed") {
		t.Fatalf("expected network project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.network_connection_create", "project.network_connection_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectHostMustExist(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"missing-host","name":"Missing","host_id":"missing","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing host 404, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
}

func TestAuditEventsRecorded(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	_ = perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)

	response := perform(server, http.MethodGet, "/v1/audit-events", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"action":"org.create"`) {
		t.Fatalf("expected org.create audit event: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"chain_index":1`) || !strings.Contains(response.Body.String(), `"hash":"`) {
		t.Fatalf("expected chained audit fields: %s", response.Body.String())
	}

	integrityResponse := perform(server, http.MethodGet, "/v1/audit-events/integrity", "")
	if integrityResponse.Code != http.StatusOK {
		t.Fatalf("expected audit integrity status 200, got %d: %s", integrityResponse.Code, integrityResponse.Body.String())
	}
	if !strings.Contains(integrityResponse.Body.String(), `"verified":true`) || !strings.Contains(integrityResponse.Body.String(), `"events":1`) {
		t.Fatalf("expected verified audit chain: %s", integrityResponse.Body.String())
	}
}

func TestProjectActivityFiltersAuditEvents(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	alphaResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"activity-alpha","name":"Alpha","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if alphaResponse.Code != http.StatusCreated {
		t.Fatalf("expected alpha create 201, got %d: %s", alphaResponse.Code, alphaResponse.Body.String())
	}
	betaResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"activity-beta","name":"Beta","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if betaResponse.Code != http.StatusCreated {
		t.Fatalf("expected beta create 201, got %d: %s", betaResponse.Code, betaResponse.Body.String())
	}
	pauseResponse := perform(server, http.MethodPost, "/v1/projects/activity-alpha/pause", "")
	if pauseResponse.Code != http.StatusOK {
		t.Fatalf("expected alpha pause 200, got %d: %s", pauseResponse.Code, pauseResponse.Body.String())
	}

	activityResponse := perform(server, http.MethodGet, "/v1/projects/activity-alpha/activity", "")
	if activityResponse.Code != http.StatusOK {
		t.Fatalf("expected activity status 200, got %d: %s", activityResponse.Code, activityResponse.Body.String())
	}
	body := activityResponse.Body.String()
	if !strings.Contains(body, `"target":"project:activity-alpha"`) || !strings.Contains(body, `"action":"project.paused"`) {
		t.Fatalf("expected alpha project activity: %s", body)
	}
	if strings.Contains(body, "activity-beta") || strings.Contains(body, `"target":"org:`) {
		t.Fatalf("expected only alpha project activity: %s", body)
	}
}

func TestProjectBackupsAndLogs(t *testing.T) {
	t.Setenv("SUPADUPA_BACKUP_DRY_RUN", "true")
	t.Setenv("SUPADUPA_RESTORE_DRY_RUN", "true")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"backup-proj","name":"Backup","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	backupResponse := perform(server, http.MethodPost, "/v1/projects/backup-proj/backups", "")
	if backupResponse.Code != http.StatusCreated {
		t.Fatalf("expected backup status 201, got %d: %s", backupResponse.Code, backupResponse.Body.String())
	}

	backupsResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/backups", "")
	if backupsResponse.Code != http.StatusOK || !strings.Contains(backupsResponse.Body.String(), `"kind":"logical"`) {
		t.Fatalf("expected logical backup in response: %d %s", backupsResponse.Code, backupsResponse.Body.String())
	}
	backupID := extractString(t, backupResponse.Body.String(), "id")

	restoreResponse := perform(server, http.MethodPost, "/v1/projects/backup-proj/restore", `{"backup_id":"`+backupID+`"}`)
	if restoreResponse.Code != http.StatusAccepted {
		t.Fatalf("expected restore status 202, got %d: %s", restoreResponse.Code, restoreResponse.Body.String())
	}
	if !strings.Contains(restoreResponse.Body.String(), `"restore_state":"dry-run"`) || !strings.Contains(restoreResponse.Body.String(), backupID) || !strings.Contains(restoreResponse.Body.String(), `.sql`) {
		t.Fatalf("expected dry-run restore response: %s", restoreResponse.Body.String())
	}

	missingRestoreResponse := perform(server, http.MethodPost, "/v1/projects/backup-proj/restore", `{"backup_id":"missing"}`)
	if missingRestoreResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing restore backup 404, got %d: %s", missingRestoreResponse.Code, missingRestoreResponse.Body.String())
	}

	logsResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/logs", "")
	if logsResponse.Code != http.StatusOK || !strings.Contains(logsResponse.Body.String(), "Logical backup completed") || !strings.Contains(logsResponse.Body.String(), "Restore dry-run") {
		t.Fatalf("expected backup log in response: %d %s", logsResponse.Code, logsResponse.Body.String())
	}
	streamResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/logs/stream?follow=false", "")
	if streamResponse.Code != http.StatusOK {
		t.Fatalf("expected log stream status 200, got %d: %s", streamResponse.Code, streamResponse.Body.String())
	}
	if contentType := streamResponse.Header().Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected event stream content type, got %q", contentType)
	}
	if !strings.Contains(streamResponse.Body.String(), "event: log") || !strings.Contains(streamResponse.Body.String(), `"message":"Logical backup completed"`) {
		t.Fatalf("expected backup log stream events: %s", streamResponse.Body.String())
	}
	streamAliasResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/logs?stream=true&follow=false", "")
	if streamAliasResponse.Code != http.StatusOK {
		t.Fatalf("expected /logs stream alias status 200, got %d: %s", streamAliasResponse.Code, streamAliasResponse.Body.String())
	}
	if contentType := streamAliasResponse.Header().Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected /logs stream alias content type, got %q", contentType)
	}
	if !strings.Contains(streamAliasResponse.Body.String(), "event: log") || !strings.Contains(streamAliasResponse.Body.String(), `"message":"Restore dry-run"`) {
		t.Fatalf("expected /logs stream alias events: %s", streamAliasResponse.Body.String())
	}
	acceptStreamRequest := httptest.NewRequest(http.MethodGet, "/v1/projects/backup-proj/logs?follow=false", nil)
	acceptStreamRequest.Header.Set("Accept", "text/event-stream")
	acceptStreamResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(acceptStreamResponse, acceptStreamRequest)
	if acceptStreamResponse.Code != http.StatusOK {
		t.Fatalf("expected Accept stream status 200, got %d: %s", acceptStreamResponse.Code, acceptStreamResponse.Body.String())
	}
	if contentType := acceptStreamResponse.Header().Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected Accept stream content type, got %q", contentType)
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.restore"`) {
		t.Fatalf("expected project restore audit event: %s", auditResponse.Body.String())
	}

	policyResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/backups/policy", "")
	if policyResponse.Code != http.StatusOK || !strings.Contains(policyResponse.Body.String(), `"schedule":"daily"`) {
		t.Fatalf("expected default daily backup policy: %d %s", policyResponse.Code, policyResponse.Body.String())
	}

	updatePolicyResponse := perform(server, http.MethodPut, "/v1/projects/backup-proj/backups/policy", `{"enabled":false,"schedule":"hourly","kind":"logical"}`)
	if updatePolicyResponse.Code != http.StatusOK || !strings.Contains(updatePolicyResponse.Body.String(), `"enabled":false`) {
		t.Fatalf("expected updated backup policy: %d %s", updatePolicyResponse.Code, updatePolicyResponse.Body.String())
	}
	if strings.Contains(updatePolicyResponse.Body.String(), `"next_run_at"`) {
		t.Fatalf("expected disabled backup policy to omit next_run_at: %s", updatePolicyResponse.Body.String())
	}
}

func TestProjectPITRPolicyAndWALArchives(t *testing.T) {
	t.Setenv("SUPADUPA_WAL_ARCHIVE_DRY_RUN", "true")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "pitr")
	projectBody := `{"ref":"pitr-proj","name":"PITR","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	policyResponse := perform(server, http.MethodGet, "/v1/projects/pitr-proj/pitr/policy", "")
	if policyResponse.Code != http.StatusOK || !strings.Contains(policyResponse.Body.String(), `"enabled":false`) || !strings.Contains(policyResponse.Body.String(), `"retention_days":7`) {
		t.Fatalf("expected default disabled PITR policy: %d %s", policyResponse.Code, policyResponse.Body.String())
	}

	disabledArchiveResponse := perform(server, http.MethodPost, "/v1/projects/pitr-proj/pitr/wal", "")
	if disabledArchiveResponse.Code != http.StatusConflict {
		t.Fatalf("expected disabled PITR archive 409, got %d: %s", disabledArchiveResponse.Code, disabledArchiveResponse.Body.String())
	}

	invalidPolicyResponse := perform(server, http.MethodPut, "/v1/projects/pitr-proj/pitr/policy", `{"enabled":true,"archive_bucket":"","retention_days":7}`)
	if invalidPolicyResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected missing archive bucket 400, got %d: %s", invalidPolicyResponse.Code, invalidPolicyResponse.Body.String())
	}

	updatePolicyResponse := perform(server, http.MethodPut, "/v1/projects/pitr-proj/pitr/policy", `{"enabled":true,"archive_bucket":"s3://archive/pitr-proj","retention_days":14}`)
	if updatePolicyResponse.Code != http.StatusOK || !strings.Contains(updatePolicyResponse.Body.String(), `"enabled":true`) || !strings.Contains(updatePolicyResponse.Body.String(), `"retention_days":14`) || !strings.Contains(updatePolicyResponse.Body.String(), `"archive_bucket":"s3://archive/pitr-proj"`) {
		t.Fatalf("expected enabled PITR policy: %d %s", updatePolicyResponse.Code, updatePolicyResponse.Body.String())
	}

	archiveResponse := perform(server, http.MethodPost, "/v1/projects/pitr-proj/pitr/wal", "")
	if archiveResponse.Code != http.StatusCreated || !strings.Contains(archiveResponse.Body.String(), `"status":"archived"`) || !strings.Contains(archiveResponse.Body.String(), `"segment":"`) || !strings.Contains(archiveResponse.Body.String(), `"verified_at":"`) || !strings.Contains(archiveResponse.Body.String(), `.wal`) || strings.Contains(archiveResponse.Body.String(), `.wal.json`) {
		t.Fatalf("expected archived WAL segment: %d %s", archiveResponse.Code, archiveResponse.Body.String())
	}
	archiveID := extractString(t, archiveResponse.Body.String(), "id")

	archivesResponse := perform(server, http.MethodGet, "/v1/projects/pitr-proj/pitr/wal", "")
	if archivesResponse.Code != http.StatusOK || !strings.Contains(archivesResponse.Body.String(), archiveID) {
		t.Fatalf("expected WAL archive in list: %d %s", archivesResponse.Code, archivesResponse.Body.String())
	}

	policyResponse = perform(server, http.MethodGet, "/v1/projects/pitr-proj/pitr/policy", "")
	if policyResponse.Code != http.StatusOK || !strings.Contains(policyResponse.Body.String(), `"last_archive_at":"`) {
		t.Fatalf("expected PITR policy last archive timestamp: %d %s", policyResponse.Code, policyResponse.Body.String())
	}

	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if usageResponse.Code != http.StatusOK || !strings.Contains(usageResponse.Body.String(), `"wal_archives":1`) || !strings.Contains(usageResponse.Body.String(), `"wal_archive_bytes":`) {
		t.Fatalf("expected WAL usage metering: %d %s", usageResponse.Code, usageResponse.Body.String())
	}

	metricsResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if metricsResponse.Code != http.StatusOK || !strings.Contains(metricsResponse.Body.String(), `"wal_archives":1`) || !strings.Contains(metricsResponse.Body.String(), `"wal_archive_bytes":`) {
		t.Fatalf("expected WAL fleet metrics: %d %s", metricsResponse.Code, metricsResponse.Body.String())
	}

	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if prometheusResponse.Code != http.StatusOK || !strings.Contains(prometheusResponse.Body.String(), "supadupa_wal_archives_total 1") {
		t.Fatalf("expected WAL prometheus metric: %d %s", prometheusResponse.Code, prometheusResponse.Body.String())
	}

	logsResponse := perform(server, http.MethodGet, "/v1/projects/pitr-proj/logs", "")
	for _, message := range []string{"PITR policy updated", "WAL archive failed", "WAL segment archived"} {
		if !strings.Contains(logsResponse.Body.String(), message) {
			t.Fatalf("expected log message %q: %s", message, logsResponse.Body.String())
		}
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.pitr_policy_update", "project.wal_archive"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectSecretsMaskedAndRevealAudited(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"secret-proj","name":"Secret","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	secretsResponse := perform(server, http.MethodGet, "/v1/projects/secret-proj/secrets", "")
	if secretsResponse.Code != http.StatusOK {
		t.Fatalf("expected secrets status 200, got %d: %s", secretsResponse.Code, secretsResponse.Body.String())
	}
	if !strings.Contains(secretsResponse.Body.String(), `"kind":"service_role"`) {
		t.Fatalf("expected service_role secret metadata: %s", secretsResponse.Body.String())
	}
	if strings.Contains(secretsResponse.Body.String(), `"value"`) {
		t.Fatalf("secret list leaked values: %s", secretsResponse.Body.String())
	}

	revealResponse := perform(server, http.MethodGet, "/v1/projects/secret-proj/secrets/service_role/reveal", "")
	if revealResponse.Code != http.StatusOK {
		t.Fatalf("expected reveal status 200, got %d: %s", revealResponse.Code, revealResponse.Body.String())
	}
	if !strings.Contains(revealResponse.Body.String(), `"value":"svc_`) {
		t.Fatalf("expected revealed service key value: %s", revealResponse.Body.String())
	}
	firstValue := extractString(t, revealResponse.Body.String(), "value")

	copyResponse := perform(server, http.MethodPost, "/v1/projects/secret-proj/secrets/service_role/copy", "")
	if copyResponse.Code != http.StatusNoContent {
		t.Fatalf("expected copy status 204, got %d: %s", copyResponse.Code, copyResponse.Body.String())
	}
	if strings.Contains(copyResponse.Body.String(), `"value"`) {
		t.Fatalf("copy response leaked secret value: %s", copyResponse.Body.String())
	}

	rotateResponse := perform(server, http.MethodPost, "/v1/projects/secret-proj/keys/rotate", `{"kind":"service_role"}`)
	if rotateResponse.Code != http.StatusOK {
		t.Fatalf("expected rotate status 200, got %d: %s", rotateResponse.Code, rotateResponse.Body.String())
	}
	if !strings.Contains(rotateResponse.Body.String(), `"kind":"service_role"`) || !strings.Contains(rotateResponse.Body.String(), `"rotated_at"`) {
		t.Fatalf("expected rotated service_role metadata: %s", rotateResponse.Body.String())
	}
	if strings.Contains(rotateResponse.Body.String(), `"value"`) {
		t.Fatalf("rotate response leaked secret value: %s", rotateResponse.Body.String())
	}

	secondRevealResponse := perform(server, http.MethodGet, "/v1/projects/secret-proj/secrets/service_role/reveal", "")
	secondValue := extractString(t, secondRevealResponse.Body.String(), "value")
	if secondValue == firstValue {
		t.Fatalf("expected rotated value to change")
	}

	signingRotateResponse := perform(server, http.MethodPost, "/v1/projects/secret-proj/keys/rotate", `{"kind":"jwt_signing_key_current"}`)
	if signingRotateResponse.Code != http.StatusOK {
		t.Fatalf("expected signing key rotate status 200, got %d: %s", signingRotateResponse.Code, signingRotateResponse.Body.String())
	}
	if !strings.Contains(signingRotateResponse.Body.String(), `"kind":"jwt_signing_key_current"`) || strings.Contains(signingRotateResponse.Body.String(), `"value"`) {
		t.Fatalf("expected masked signing key rotation metadata: %s", signingRotateResponse.Body.String())
	}
	signingSecretsResponse := perform(server, http.MethodGet, "/v1/projects/secret-proj/secrets", "")
	if !strings.Contains(signingSecretsResponse.Body.String(), `"kind":"jwt_signing_key_previous_`) {
		t.Fatalf("expected archived signing key metadata: %s", signingSecretsResponse.Body.String())
	}
	signingConnectResponse := perform(server, http.MethodGet, "/v1/projects/secret-proj/connect", "")
	if !strings.Contains(signingConnectResponse.Body.String(), `"jwt_signing_keys"`) || !strings.Contains(signingConnectResponse.Body.String(), `"status":"previous"`) {
		t.Fatalf("expected signing key history in connect payload: %s", signingConnectResponse.Body.String())
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.secret_reveal"`) {
		t.Fatalf("expected secret reveal audit event: %s", auditResponse.Body.String())
	}
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.secret_copy"`) {
		t.Fatalf("expected secret copy audit event: %s", auditResponse.Body.String())
	}
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.secret_rotate"`) {
		t.Fatalf("expected secret rotate audit event: %s", auditResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/secret-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Secret revealed") {
		t.Fatalf("expected secret reveal project log: %s", logsResponse.Body.String())
	}
	if !strings.Contains(logsResponse.Body.String(), "Secret copied") {
		t.Fatalf("expected secret copy project log: %s", logsResponse.Body.String())
	}
	if !strings.Contains(logsResponse.Body.String(), "Secret rotated") {
		t.Fatalf("expected secret rotate project log: %s", logsResponse.Body.String())
	}
}

func TestProjectCreatePassesGeneratedSecretsToProvisioner(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")

	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"render-secret-proj","name":"Render Secret","domain":"supadupa.test","profile":"full","resource_tier":"small","environment":{"POSTGRES_PASSWORD":"caller-should-not-win"}}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if strings.Contains(projectResponse.Body.String(), "caller-should-not-win") || strings.Contains(projectResponse.Body.String(), `"JWT_SECRET"`) {
		t.Fatalf("project response leaked provisioner-only secret environment: %s", projectResponse.Body.String())
	}
	if provisioner.spec.Ref != "render-secret-proj" {
		t.Fatalf("expected provisioner create call, got %#v", provisioner.spec)
	}
	for key, prefix := range map[string]string{
		"JWT_SECRET":                       "jwt_",
		"GOTRUE_JWT_SECRET":                "jwt_",
		"PGRST_JWT_SECRET":                 "jwt_",
		"REALTIME_JWT_SECRET":              "jwt_",
		"SUPADUPA_JWT_SIGNING_KEY_CURRENT": "{",
		"SUPADUPA_JWT_SIGNING_KEY_NEXT":    "{",
		"ANON_KEY":                         "anon_",
		"SERVICE_ROLE_KEY":                 "svc_",
		"SUPABASE_PUBLISHABLE_KEY":         "pub_",
		"SUPABASE_SECRET_KEY":              "sec_",
		"POSTGRES_PASSWORD":                "db_",
		"S3_ACCESS_KEY":                    "s3ak_",
		"S3_SECRET_KEY":                    "s3sk_",
	} {
		value := provisioner.spec.Environment[key]
		if !strings.HasPrefix(value, prefix) {
			t.Fatalf("expected provisioner env %s to have prefix %q, got %q in %#v", key, prefix, value, provisioner.spec.Environment)
		}
	}
	if provisioner.spec.Environment["POSTGRES_PASSWORD"] == "caller-should-not-win" {
		t.Fatalf("caller supplied db password won over managed secret")
	}
}

func TestProjectSecretRotateSyncsProvisionerSecrets(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")

	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"rotate-sync-proj","name":"Rotate Sync","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	createdServiceKey := provisioner.spec.Environment["SERVICE_ROLE_KEY"]
	if createdServiceKey == "" {
		t.Fatalf("expected initial service role key in provisioner spec: %#v", provisioner.spec.Environment)
	}

	rotateResponse := perform(server, http.MethodPost, "/v1/projects/rotate-sync-proj/keys/rotate", `{"kind":"service_role"}`)
	if rotateResponse.Code != http.StatusOK {
		t.Fatalf("expected rotate status 200, got %d: %s", rotateResponse.Code, rotateResponse.Body.String())
	}
	if strings.Contains(rotateResponse.Body.String(), `"value"`) {
		t.Fatalf("rotate response leaked secret value: %s", rotateResponse.Body.String())
	}
	if provisioner.syncedRef != "rotate-sync-proj" {
		t.Fatalf("expected provisioner sync for rotate-sync-proj, got %q", provisioner.syncedRef)
	}
	nextServiceKey := provisioner.syncedSpec.Environment["SERVICE_ROLE_KEY"]
	if !strings.HasPrefix(nextServiceKey, "svc_") || nextServiceKey == createdServiceKey {
		t.Fatalf("expected rotated service key in synced spec, old=%q new=%q env=%#v", createdServiceKey, nextServiceKey, provisioner.syncedSpec.Environment)
	}
	for _, key := range []string{"JWT_SECRET", "SUPADUPA_JWT_SIGNING_KEY_CURRENT", "SUPADUPA_JWT_SIGNING_KEY_NEXT", "POSTGRES_PASSWORD", "ANON_KEY", "S3_ACCESS_KEY", "S3_SECRET_KEY"} {
		if provisioner.syncedSpec.Environment[key] == "" {
			t.Fatalf("expected synced spec to include managed key %s: %#v", key, provisioner.syncedSpec.Environment)
		}
	}
}

func TestProjectLifecycleActions(t *testing.T) {
	routesRoot := t.TempDir()
	certRoot := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", routesRoot)
	t.Setenv("SUPADUPA_CERT_ROOT", certRoot)
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "custom_domains")
	projectBody := `{"ref":"life-proj","name":"Life","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	routePath := filepath.Join(routesRoot, "life-proj.yaml")
	if _, err := os.Stat(routePath); err != nil {
		t.Fatalf("expected project route artifact: %v", err)
	}
	domainResponse := perform(server, http.MethodPost, "/v1/projects/life-proj/domains", `{"fqdn":"life.example.com"}`)
	if domainResponse.Code != http.StatusCreated {
		t.Fatalf("expected domain status 201, got %d: %s", domainResponse.Code, domainResponse.Body.String())
	}
	certPath := filepath.Join(certRoot, "life-proj", "life.example.com.json")
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("expected project certificate artifact: %v", err)
	}

	pauseResponse := perform(server, http.MethodPost, "/v1/projects/life-proj/pause", "")
	if pauseResponse.Code != http.StatusOK || !strings.Contains(pauseResponse.Body.String(), `"status":"paused"`) {
		t.Fatalf("expected paused project: %d %s", pauseResponse.Code, pauseResponse.Body.String())
	}

	resumeResponse := perform(server, http.MethodPost, "/v1/projects/life-proj/resume", "")
	if resumeResponse.Code != http.StatusOK || !strings.Contains(resumeResponse.Body.String(), `"status":"healthy"`) {
		t.Fatalf("expected resumed project: %d %s", resumeResponse.Code, resumeResponse.Body.String())
	}

	restartResponse := perform(server, http.MethodPost, "/v1/projects/life-proj/restart", "")
	if restartResponse.Code != http.StatusOK || !strings.Contains(restartResponse.Body.String(), `"message":"restarted"`) {
		t.Fatalf("expected restarted project: %d %s", restartResponse.Code, restartResponse.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/life-proj/upgrade", `{"version":"15.8.1.060"}`)
	if upgradeResponse.Code != http.StatusOK || !strings.Contains(upgradeResponse.Body.String(), `"stack_version":"15.8.1.060"`) {
		t.Fatalf("expected upgraded project: %d %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}

	logsResponse := perform(server, http.MethodGet, "/v1/projects/life-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Project restarted") || !strings.Contains(logsResponse.Body.String(), "Stack upgraded") {
		t.Fatalf("expected lifecycle logs: %s", logsResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/life-proj", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected delete status 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, err := os.Stat(routePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected route artifact removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(certRoot, "life-proj")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected certificate directory removed, got err=%v", err)
	}

	missingResponse := perform(server, http.MethodGet, "/v1/projects/life-proj", "")
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing project after delete, got %d: %s", missingResponse.Code, missingResponse.Body.String())
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.paused", "project.restart", "project.upgrade", "project.destroy"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDestroySurfacesRouteCleanupFailure(t *testing.T) {
	routesRoot := t.TempDir()
	certRoot := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", routesRoot)
	t.Setenv("SUPADUPA_CERT_ROOT", certRoot)
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"route-cleanup-proj","name":"Routes","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	badRouteRoot := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badRouteRoot, []byte("blocks cleanup"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUPADUPA_ROUTES_ROOT", badRouteRoot)

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/route-cleanup-proj", "")
	if deleteResponse.Code != http.StatusConflict {
		t.Fatalf("expected route cleanup conflict, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	projectResponse = perform(server, http.MethodGet, "/v1/projects/route-cleanup-proj", "")
	if projectResponse.Code != http.StatusOK {
		t.Fatalf("expected project metadata retained after route cleanup failure, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.route_cleanup_failed"`) {
		t.Fatalf("expected route cleanup failure audit event: %s", auditResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/route-cleanup-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Project route cleanup failed") {
		t.Fatalf("expected route cleanup failure project log: %s", logsResponse.Body.String())
	}
}

func TestProjectDestroyPassesRetainVolumesOption(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &retainDestroyProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"retain-proj","name":"Retain","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/retain-proj?retain_volumes=true", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected destroy 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if provisioner.destroyedRef != "retain-proj" || !provisioner.destroyOpts.RetainVolumes {
		t.Fatalf("expected retain destroy options, ref=%q opts=%#v", provisioner.destroyedRef, provisioner.destroyOpts)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"retain_volumes":"true"`) {
		t.Fatalf("expected retain_volumes audit metadata: %s", auditResponse.Body.String())
	}
}

func perform(server *http.Server, method string, path string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	return response
}

func performWithToken(server *http.Server, method string, path string, body string, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	return response
}

func extractString(t *testing.T, body string, field string) string {
	t.Helper()
	needle := `"` + field + `":"`
	start := strings.Index(body, needle)
	if start == -1 {
		t.Fatalf("field %q not found in %s", field, body)
	}
	start += len(needle)
	end := strings.Index(body[start:], `"`)
	if end == -1 {
		t.Fatalf("field %q is not terminated in %s", field, body)
	}
	return body[start : start+end]
}

func enableOrgFeaturesForTest(t *testing.T, store control.Store, orgID string, flags ...string) {
	t.Helper()
	current, err := store.GetOrgFeatureFlags(context.Background(), orgID)
	if err != nil {
		t.Fatalf("get org feature flags: %v", err)
	}
	overrides := map[string]bool{}
	for key, enabled := range current.Overrides {
		overrides[key] = enabled
	}
	for _, flag := range flags {
		overrides[flag] = true
	}
	if _, err := store.UpdateOrgFeatureFlags(context.Background(), orgID, control.OrgFeatureFlagsInput{Overrides: overrides}); err != nil {
		t.Fatalf("enable org feature flags: %v", err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(payload)
}

func testSAMLSigningCertificate(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "supadupa test idp"},
		NotBefore:    time.Now().UTC().Add(-time.Hour),
		NotAfter:     time.Now().UTC().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return privateKey, certificate
}

func signSAMLAssertion(t *testing.T, privateKey *rsa.PrivateKey, assertion control.PlatformSSOAssertion) string {
	t.Helper()
	sum := sha256.Sum256(control.PlatformSSOAssertionSignaturePayload(assertion))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	return base64.StdEncoding.EncodeToString(signature)
}

type fakeProvisioner struct{}

func (fakeProvisioner) Name() string { return "fake" }

func (fakeProvisioner) Create(ctx context.Context, spec control.ProjectSpec) error { return nil }

func (fakeProvisioner) SyncSecrets(ctx context.Context, ref string, spec control.ProjectSpec) error {
	return nil
}

func (fakeProvisioner) SyncConfig(ctx context.Context, ref string, config control.ProjectConfig) error {
	return nil
}

func (fakeProvisioner) Destroy(ctx context.Context, ref string) error { return nil }

func (fakeProvisioner) Status(ctx context.Context, ref string) (control.ProjectStatus, error) {
	return control.ProjectStatus{Ref: ref, Phase: control.ProjectHealthy}, nil
}

func (fakeProvisioner) Upgrade(ctx context.Context, ref string, version string) error { return nil }

func (fakeProvisioner) Pause(ctx context.Context, ref string) error { return nil }

func (fakeProvisioner) Resume(ctx context.Context, ref string) error { return nil }

func (fakeProvisioner) Scale(ctx context.Context, ref string, tier control.ResourceTier) error {
	return nil
}

func (fakeProvisioner) AddReplica(ctx context.Context, ref string, opts control.ReplicaOpts) error {
	return nil
}

type retainDestroyProvisioner struct {
	fakeProvisioner
	destroyedRef string
	destroyOpts  control.DestroyOptions
}

func (p *retainDestroyProvisioner) DestroyWithOptions(ctx context.Context, ref string, opts control.DestroyOptions) error {
	p.destroyedRef = ref
	p.destroyOpts = opts
	return nil
}

type capturingProvisioner struct {
	fakeProvisioner
	spec               control.ProjectSpec
	syncedRef          string
	syncedSpec         control.ProjectSpec
	syncedConfigRef    string
	syncedConfig       control.ProjectConfig
	syncedServicesRef  string
	syncedServicesSpec control.ProjectSpec
	syncedAuthHooksRef string
	syncedAuthHooks    []control.ProjectAuthHook
	clonedBranch       control.BranchCloneOptions
}

func (p *capturingProvisioner) Create(ctx context.Context, spec control.ProjectSpec) error {
	p.spec = spec
	return nil
}

func (p *capturingProvisioner) SyncSecrets(ctx context.Context, ref string, spec control.ProjectSpec) error {
	p.syncedRef = ref
	p.syncedSpec = spec
	return nil
}

func (p *capturingProvisioner) SyncConfig(ctx context.Context, ref string, config control.ProjectConfig) error {
	p.syncedConfigRef = ref
	p.syncedConfig = config
	return nil
}

func (p *capturingProvisioner) SyncServices(ctx context.Context, ref string, spec control.ProjectSpec) error {
	p.syncedServicesRef = ref
	p.syncedServicesSpec = spec
	return nil
}

func (p *capturingProvisioner) SyncAuthHooks(ctx context.Context, ref string, hooks []control.ProjectAuthHook) error {
	p.syncedAuthHooksRef = ref
	p.syncedAuthHooks = append([]control.ProjectAuthHook(nil), hooks...)
	for index := range p.syncedAuthHooks {
		if p.syncedAuthHooks[index].Headers == nil {
			continue
		}
		headers := make(map[string]string, len(p.syncedAuthHooks[index].Headers))
		for key, value := range p.syncedAuthHooks[index].Headers {
			headers[key] = value
		}
		p.syncedAuthHooks[index].Headers = headers
	}
	return nil
}

func (p *capturingProvisioner) CloneBranch(ctx context.Context, opts control.BranchCloneOptions) (control.BranchCloneResult, error) {
	p.clonedBranch = opts
	return control.BranchCloneResult{Path: "branch-clone.sql", State: "dry-run"}, nil
}

func TestProvisionerEndpoint(t *testing.T) {
	server := NewServer(Config{Provisioner: composeprovisioner.New()})
	request := httptest.NewRequest(http.MethodGet, "/v1/provisioner", nil)
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if body := response.Body.String(); body != "{\"provisioner\":\"compose\"}\n" {
		t.Fatalf("unexpected body: %s", body)
	}
}
