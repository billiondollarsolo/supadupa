package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"supadupa2026/internal/control"
)

func TestBrowserAuthResponsesOmitBearerToken(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}, AuthRequired: true})

	bootstrap := performWithHeader(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`, "X-Supadupa-Browser", "true")
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	if strings.Contains(bootstrap.Body.String(), `"token"`) {
		t.Fatalf("browser bootstrap response must not include bearer token: %s", bootstrap.Body.String())
	}
	assertAuthCookie(t, bootstrap.Result().Cookies(), false)

	login := performWithHeader(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`, "X-Supadupa-Browser", "true")
	if login.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d: %s", login.Code, login.Body.String())
	}
	if strings.Contains(login.Body.String(), `"token"`) {
		t.Fatalf("browser login response must not include bearer token: %s", login.Body.String())
	}
	assertAuthCookie(t, login.Result().Cookies(), false)

	secureLoginRequest := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"super-secure"}`))
	secureLoginRequest.Header.Set("Content-Type", "application/json")
	secureLoginRequest.Header.Set("X-Supadupa-Browser", "true")
	secureLoginRequest.Header.Set("X-Forwarded-Proto", "https")
	secureLogin := httptest.NewRecorder()
	server.Handler.ServeHTTP(secureLogin, secureLoginRequest)
	if secureLogin.Code != http.StatusOK {
		t.Fatalf("expected secure login status 200, got %d: %s", secureLogin.Code, secureLogin.Body.String())
	}
	assertAuthCookie(t, secureLogin.Result().Cookies(), true)
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

func TestStaleBearerTokenRejectedAfterPasswordChange(t *testing.T) {
	ctx := context.Background()
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
	users, err := store.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("expected bootstrap user, users=%d err=%v", len(users), err)
	}
	user := users[0]
	org, err := store.CreateOrg(ctx, "Stale Token Org")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertOrgMember(ctx, org.ID, control.MembershipInput{Email: user.Email, Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, control.CreateProjectRequest{OrgID: org.ID, Ref: "stale-token", Name: "Stale Token"}); err != nil {
		t.Fatal(err)
	}
	seedProjectSecrets(t, store, "stale-token", "custom_secret")

	allowed := performWithToken(server, http.MethodGet, "/v1/projects/stale-token/secrets/custom_secret/reveal", "", token)
	if allowed.Code != http.StatusOK {
		t.Fatalf("expected original token to reveal secret before password change, got %d: %s", allowed.Code, allowed.Body.String())
	}

	if _, err := store.UpdateUser(ctx, user.ID, control.UpdateUserRequest{Email: user.Email, Password: "new-super-secure", Role: user.Role}); err != nil {
		t.Fatal(err)
	}

	stale := performWithToken(server, http.MethodGet, "/v1/projects/stale-token/secrets/custom_secret/reveal", "", token)
	if stale.Code != http.StatusUnauthorized || !strings.Contains(stale.Body.String(), "stale bearer token") {
		t.Fatalf("expected stale token rejection after password change, got %d: %s", stale.Code, stale.Body.String())
	}
	authState := performWithToken(server, http.MethodGet, "/v1/auth/state", "", token)
	if authState.Code != http.StatusOK || strings.Contains(authState.Body.String(), `"authenticated":true`) {
		t.Fatalf("expected stale token to be unauthenticated in auth state, got %d: %s", authState.Code, authState.Body.String())
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
	updateWithoutAdapterFlag := performWithToken(server, http.MethodPut, "/v1/settings/sso", configBody, token)
	if updateWithoutAdapterFlag.Code != http.StatusBadRequest || !strings.Contains(updateWithoutAdapterFlag.Body.String(), "SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER") {
		t.Fatalf("expected platform sso JSON adapter opt-in rejection, got %d: %s", updateWithoutAdapterFlag.Code, updateWithoutAdapterFlag.Body.String())
	}

	t.Setenv("SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER", "true")
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
	callback := performWithHeader(server, http.MethodPost, "/v1/auth/sso/saml/callback", mustJSON(t, assertion), "Origin", "https://idp.example.com")
	if callback.Code != http.StatusOK || strings.Contains(callback.Body.String(), `"token"`) || !strings.Contains(callback.Body.String(), `"email":"engineer@example.com"`) || !strings.Contains(callback.Body.String(), `"role":"viewer"`) {
		t.Fatalf("expected sso callback user without bearer token: %d %s", callback.Code, callback.Body.String())
	}
	if len(callback.Result().Cookies()) == 0 {
		t.Fatal("expected sso callback to set session cookie")
	}

	replayCallback := perform(server, http.MethodPost, "/v1/auth/sso/saml/callback", mustJSON(t, assertion))
	if replayCallback.Code != http.StatusUnauthorized || !strings.Contains(replayCallback.Body.String(), "already been used") {
		t.Fatalf("expected sso assertion replay rejection: %d %s", replayCallback.Code, replayCallback.Body.String())
	}

	tamperedRoleAssertion := assertion
	tamperedRoleAssertion.Email = "other-engineer@example.com"
	tamperedRoleAssertion.NameID = "idp-user-456"
	tamperedRoleAssertion.Signature = signSAMLAssertion(t, privateKey, tamperedRoleAssertion)
	tamperedRoleAssertion.Role = "admin"
	tamperedRoleCallback := perform(server, http.MethodPost, "/v1/auth/sso/saml/callback", mustJSON(t, tamperedRoleAssertion))
	if tamperedRoleCallback.Code != http.StatusUnauthorized || !strings.Contains(tamperedRoleCallback.Body.String(), "signature is invalid") {
		t.Fatalf("expected role tampering rejection: %d %s", tamperedRoleCallback.Code, tamperedRoleCallback.Body.String())
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

func TestPlatformSSOCallbackFailuresAreThrottledAndAudited(t *testing.T) {
	t.Setenv("SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER", "true")
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
	privateKey, certificate := testSAMLSigningCertificate(t)
	configBody := mustJSON(t, map[string]any{
		"enabled":         true,
		"idp_entity_id":   "https://idp.example.com/saml",
		"sso_url":         "https://idp.example.com/login",
		"certificate_pem": certificate,
		"acs_url":         "https://supadupa.example.com/v1/auth/sso/saml/callback",
		"email_domain":    "example.com",
		"auto_provision":  true,
		"default_role":    "developer",
	})
	update := performWithToken(server, http.MethodPut, "/v1/settings/sso", configBody, token)
	if update.Code != http.StatusOK {
		t.Fatalf("expected sso settings update 200, got %d: %s", update.Code, update.Body.String())
	}

	badAssertion := control.PlatformSSOAssertion{
		Issuer:       "https://idp.example.com/saml",
		Audience:     "https://supadupa.example.com/v1/auth/sso/saml/callback",
		Email:        "engineer@other.test",
		NameID:       "idp-user-456",
		Role:         "developer",
		NotOnOrAfter: time.Now().UTC().Add(5 * time.Minute),
	}
	badAssertion.Signature = signSAMLAssertion(t, privateKey, badAssertion)
	for i := 0; i < maxSSOCallbackFailures; i++ {
		response := perform(server, http.MethodPost, "/v1/auth/sso/saml/callback", mustJSON(t, badAssertion))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected bad sso callback %d to be unauthorized, got %d: %s", i+1, response.Code, response.Body.String())
		}
	}

	validAssertion := badAssertion
	validAssertion.Email = "engineer@example.com"
	validAssertion.Signature = signSAMLAssertion(t, privateKey, validAssertion)
	throttled := perform(server, http.MethodPost, "/v1/auth/sso/saml/callback", mustJSON(t, validAssertion))
	if throttled.Code != http.StatusTooManyRequests || !strings.Contains(throttled.Body.String(), "too many sso callback failures") {
		t.Fatalf("expected throttled sso callback, got %d: %s", throttled.Code, throttled.Body.String())
	}
	if throttled.Header().Get("Retry-After") == "" {
		t.Fatal("expected throttled sso callback to include Retry-After")
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	if !strings.Contains(auditResponse.Body.String(), `"action":"user.sso_callback_failed"`) || !strings.Contains(auditResponse.Body.String(), `"reason":"validation_failed"`) {
		t.Fatalf("expected sso callback failure audit event: %s", auditResponse.Body.String())
	}
	if strings.Contains(auditResponse.Body.String(), badAssertion.Signature) {
		t.Fatalf("sso failure audit must not include assertion signature: %s", auditResponse.Body.String())
	}
}

func TestCORSRequiresOriginForCookieAuthenticatedMutation(t *testing.T) {
	server := NewServer(Config{Store: control.NewMemoryStore(), AuthRequired: true, CORSOrigins: []string{"https://admin.example.com"}})
	bootstrap := performWithHeader(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`, "X-Supadupa-Browser", "true")
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}

	noOrigin := httptest.NewRequest(http.MethodPost, "/v1/orgs", strings.NewReader(`{"name":"Cookie Org"}`))
	noOrigin.Header.Set("Content-Type", "application/json")
	for _, cookie := range bootstrap.Result().Cookies() {
		noOrigin.AddCookie(cookie)
	}
	noOriginResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(noOriginResponse, noOrigin)
	if noOriginResponse.Code != http.StatusForbidden || !strings.Contains(noOriginResponse.Body.String(), "origin is required") {
		t.Fatalf("expected cookie mutation without origin to be rejected, got %d: %s", noOriginResponse.Code, noOriginResponse.Body.String())
	}

	allowedOrigin := httptest.NewRequest(http.MethodPost, "/v1/orgs", strings.NewReader(`{"name":"Allowed Cookie Org"}`))
	allowedOrigin.Header.Set("Content-Type", "application/json")
	allowedOrigin.Header.Set("Origin", "https://admin.example.com")
	for _, cookie := range bootstrap.Result().Cookies() {
		allowedOrigin.AddCookie(cookie)
	}
	allowedOriginResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(allowedOriginResponse, allowedOrigin)
	if allowedOriginResponse.Code != http.StatusCreated {
		t.Fatalf("expected allowed-origin cookie mutation, got %d: %s", allowedOriginResponse.Code, allowedOriginResponse.Body.String())
	}

	login := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`)
	token := extractString(t, login.Body.String(), "token")
	bearerResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Bearer Org"}`, token)
	if bearerResponse.Code != http.StatusCreated {
		t.Fatalf("expected bearer mutation without origin to remain allowed, got %d: %s", bearerResponse.Code, bearerResponse.Body.String())
	}
}

func TestAuthStateIncludesCookieAuthenticatedUser(t *testing.T) {
	server := NewServer(Config{Store: control.NewMemoryStore(), AuthRequired: true})
	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/auth/state", nil)
	for _, cookie := range bootstrap.Result().Cookies() {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"authenticated":true`) || !strings.Contains(response.Body.String(), `"email":"admin@example.com"`) {
		t.Fatalf("expected authenticated auth state, got %d: %s", response.Code, response.Body.String())
	}
}

func TestLoginThrottlesRepeatedFailures(t *testing.T) {
	server := NewServer(Config{Store: control.NewMemoryStore(), AuthRequired: true})
	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	token := extractString(t, bootstrap.Body.String(), "token")

	for i := 0; i < maxAuthFailures; i++ {
		response := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"wrong-password"}`)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected failed login %d to be unauthorized, got %d: %s", i+1, response.Code, response.Body.String())
		}
	}
	throttled := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`)
	if throttled.Code != http.StatusTooManyRequests || !strings.Contains(throttled.Body.String(), "too many failed authentication attempts") {
		t.Fatalf("expected throttled login after repeated failures, got %d: %s", throttled.Code, throttled.Body.String())
	}
	spoofedForwardedFor := performWithHeader(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`, "X-Forwarded-For", "203.0.113.10")
	if spoofedForwardedFor.Code != http.StatusTooManyRequests {
		t.Fatalf("expected spoofed X-Forwarded-For to stay throttled, got %d: %s", spoofedForwardedFor.Code, spoofedForwardedFor.Body.String())
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	if !strings.Contains(auditResponse.Body.String(), `"action":"user.login_failed"`) || !strings.Contains(auditResponse.Body.String(), `"reason":"invalid_credentials"`) {
		t.Fatalf("expected failed login audit event: %s", auditResponse.Body.String())
	}
	if strings.Contains(auditResponse.Body.String(), "wrong-password") {
		t.Fatalf("login audit events must not include submitted passwords: %s", auditResponse.Body.String())
	}
}

func TestLoginThrottlesRepeatedFailuresByClientIP(t *testing.T) {
	server := NewServer(Config{Store: control.NewMemoryStore(), AuthRequired: true})
	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}

	for i := 0; i < maxAuthFailures; i++ {
		body := fmt.Sprintf(`{"email":"attacker-%d@example.com","password":"wrong-password"}`, i)
		response := perform(server, http.MethodPost, "/v1/auth/login", body)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected failed login %d to be unauthorized, got %d: %s", i+1, response.Code, response.Body.String())
		}
	}
	throttled := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`)
	if throttled.Code != http.StatusTooManyRequests || !strings.Contains(throttled.Body.String(), "too many failed authentication attempts") {
		t.Fatalf("expected valid login throttled by client IP failures, got %d: %s", throttled.Code, throttled.Body.String())
	}
}

func TestAuthAttemptLimiterPrunesExpiredEntriesAndCapsKeys(t *testing.T) {
	limiter := newAuthAttemptLimiter()
	old := time.Now().UTC().Add(-2 * authAttemptWindow)
	limiter.RecordFailure("old@example.com|192.0.2.1", old)
	limiter.RecordFailure("new@example.com|192.0.2.1", time.Now().UTC())

	limiter.mu.Lock()
	if _, ok := limiter.attempts["old@example.com|192.0.2.1"]; ok {
		limiter.mu.Unlock()
		t.Fatal("expected expired auth attempt key to be pruned")
	}
	limiter.mu.Unlock()

	limiter = newAuthAttemptLimiter()
	now := time.Now().UTC()
	for i := 0; i < maxAuthAttemptKeys+50; i++ {
		limiter.RecordFailure(fmt.Sprintf("user-%d@example.com|192.0.2.1", i), now.Add(time.Duration(i)*time.Millisecond))
	}
	limiter.mu.Lock()
	size := len(limiter.attempts)
	limiter.mu.Unlock()
	if size > maxAuthAttemptKeys {
		t.Fatalf("expected auth attempt limiter to cap keys at %d, got %d", maxAuthAttemptKeys, size)
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
	now := time.Now().UTC()
	code, err := control.TOTPCode(secret, now.Add(-30*time.Second))
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
	staleAfterVerify := performWithToken(server, http.MethodGet, "/v1/account/mfa", "", token)
	if staleAfterVerify.Code != http.StatusUnauthorized || !strings.Contains(staleAfterVerify.Body.String(), "stale bearer token") {
		t.Fatalf("expected original token to be stale after mfa verify, got %d: %s", staleAfterVerify.Code, staleAfterVerify.Body.String())
	}

	challenge := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`)
	if challenge.Code != http.StatusAccepted || !strings.Contains(challenge.Body.String(), `"mfa_required":true`) || strings.Contains(challenge.Body.String(), `"token"`) {
		t.Fatalf("expected mfa login challenge without token: %d %s", challenge.Code, challenge.Body.String())
	}

	loginCode, err := control.TOTPCode(secret, now)
	if err != nil {
		t.Fatalf("expected login totp code: %v", err)
	}
	login := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure","totp_code":"`+loginCode+`"}`)
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `"token"`) {
		t.Fatalf("expected login with mfa token: %d %s", login.Code, login.Body.String())
	}
	mfaToken := extractString(t, login.Body.String(), "token")

	disableCode, err := control.TOTPCode(secret, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("expected disable totp code: %v", err)
	}
	disable := performWithToken(server, http.MethodDelete, "/v1/account/mfa", `{"code":"`+disableCode+`"}`, mfaToken)
	if disable.Code != http.StatusOK || !strings.Contains(disable.Body.String(), `"enabled":false`) {
		t.Fatalf("expected mfa disabled: %d %s", disable.Code, disable.Body.String())
	}
	staleAfterDisable := performWithToken(server, http.MethodGet, "/v1/account/mfa", "", mfaToken)
	if staleAfterDisable.Code != http.StatusUnauthorized || !strings.Contains(staleAfterDisable.Body.String(), "stale bearer token") {
		t.Fatalf("expected mfa login token to be stale after mfa disable, got %d: %s", staleAfterDisable.Code, staleAfterDisable.Body.String())
	}

	postDisableLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`)
	if postDisableLogin.Code != http.StatusOK || !strings.Contains(postDisableLogin.Body.String(), `"token"`) {
		t.Fatalf("expected login after mfa disable: %d %s", postDisableLogin.Code, postDisableLogin.Body.String())
	}
	auditToken := extractString(t, postDisableLogin.Body.String(), "token")
	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", auditToken)
	for _, action := range []string{"user.mfa_enroll", "user.mfa_verify", "user.mfa_disable"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestPlatformMFAFailuresAreThrottledAndAudited(t *testing.T) {
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
	enroll := performWithToken(server, http.MethodPost, "/v1/account/mfa/enroll", "", token)
	if enroll.Code != http.StatusCreated {
		t.Fatalf("expected mfa enrollment: %d %s", enroll.Code, enroll.Body.String())
	}

	for i := 0; i < maxMFAAccessAttempts; i++ {
		response := performWithToken(server, http.MethodPost, "/v1/account/mfa/verify", `{"code":"000000"}`, token)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("expected bad mfa verify %d status 400, got %d: %s", i+1, response.Code, response.Body.String())
		}
	}
	throttled := performWithToken(server, http.MethodPost, "/v1/account/mfa/verify", `{"code":"000000"}`, token)
	if throttled.Code != http.StatusTooManyRequests || !strings.Contains(throttled.Body.String(), "too many mfa attempts") {
		t.Fatalf("expected throttled mfa verify, got %d: %s", throttled.Code, throttled.Body.String())
	}
	if throttled.Header().Get("Retry-After") == "" {
		t.Fatal("expected throttled mfa verify to include Retry-After")
	}

	login := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`)
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `"token"`) {
		t.Fatalf("expected login after pending mfa failures: %d %s", login.Code, login.Body.String())
	}
	auditToken := extractString(t, login.Body.String(), "token")
	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", auditToken)
	if !strings.Contains(auditResponse.Body.String(), `"action":"user.mfa_verify_failed"`) {
		t.Fatalf("expected failed mfa verify audit event: %s", auditResponse.Body.String())
	}
	if strings.Contains(auditResponse.Body.String(), "000000") {
		t.Fatalf("mfa audit events must not include submitted codes: %s", auditResponse.Body.String())
	}
}

func TestPlatformMFADisableFailuresAreThrottledAndResetAfterSuccess(t *testing.T) {
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
	enroll := performWithToken(server, http.MethodPost, "/v1/account/mfa/enroll", "", token)
	secret := extractString(t, enroll.Body.String(), "secret")
	now := time.Now().UTC()
	code, err := control.TOTPCode(secret, now.Add(-30*time.Second))
	if err != nil {
		t.Fatalf("expected totp code: %v", err)
	}
	verify := performWithToken(server, http.MethodPost, "/v1/account/mfa/verify", `{"code":"`+code+`"}`, token)
	if verify.Code != http.StatusOK {
		t.Fatalf("expected mfa verify: %d %s", verify.Code, verify.Body.String())
	}
	loginCode, err := control.TOTPCode(secret, now)
	if err != nil {
		t.Fatalf("expected login totp code: %v", err)
	}
	login := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure","totp_code":"`+loginCode+`"}`)
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `"token"`) {
		t.Fatalf("expected login with mfa token: %d %s", login.Code, login.Body.String())
	}
	mfaToken := extractString(t, login.Body.String(), "token")

	for i := 0; i < maxMFAAccessAttempts-1; i++ {
		response := performWithToken(server, http.MethodDelete, "/v1/account/mfa", `{"code":"111111"}`, mfaToken)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("expected bad mfa disable %d status 400, got %d: %s", i+1, response.Code, response.Body.String())
		}
	}
	disableCode, err := control.TOTPCode(secret, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("expected disable totp code: %v", err)
	}
	disable := performWithToken(server, http.MethodDelete, "/v1/account/mfa", `{"code":"`+disableCode+`"}`, mfaToken)
	if disable.Code != http.StatusOK || !strings.Contains(disable.Body.String(), `"enabled":false`) {
		t.Fatalf("expected successful mfa disable before throttle, got %d: %s", disable.Code, disable.Body.String())
	}

	postDisableLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`)
	if postDisableLogin.Code != http.StatusOK || !strings.Contains(postDisableLogin.Body.String(), `"token"`) {
		t.Fatalf("expected login after mfa disable: %d %s", postDisableLogin.Code, postDisableLogin.Body.String())
	}
	auditToken := extractString(t, postDisableLogin.Body.String(), "token")
	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", auditToken)
	for _, action := range []string{"user.mfa_disable_failed", "user.mfa_disable"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
	if strings.Contains(auditResponse.Body.String(), "111111") || strings.Contains(auditResponse.Body.String(), disableCode) {
		t.Fatalf("mfa audit events must not include submitted codes: %s", auditResponse.Body.String())
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

	patchUser := performWithToken(server, http.MethodPatch, "/v1/scim/v2/Users/"+userID, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"userName","value":"viewer@example.com"},{"op":"replace","path":"urn:supadupa:params:scim:schemas:extension:User.role","value":"viewer"}]}`, token)
	if patchUser.Code != http.StatusOK || !strings.Contains(patchUser.Body.String(), `"userName":"viewer@example.com"`) || !strings.Contains(patchUser.Body.String(), `"role":"viewer"`) {
		t.Fatalf("expected SCIM user patch: %d %s", patchUser.Code, patchUser.Body.String())
	}

	listUsers := performWithToken(server, http.MethodGet, "/v1/scim/v2/Users", "", token)
	if listUsers.Code != http.StatusOK || !strings.Contains(listUsers.Body.String(), `"totalResults":2`) || !strings.Contains(listUsers.Body.String(), `"userName":"viewer@example.com"`) {
		t.Fatalf("expected SCIM user list: %d %s", listUsers.Code, listUsers.Body.String())
	}

	groupBody := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group","urn:supadupa:params:scim:schemas:extension:Group"],"externalId":"` + orgID + `","displayName":"Platform Engineers","members":[{"value":"` + userID + `"}]}`
	createGroup := performWithToken(server, http.MethodPost, "/v1/scim/v2/Groups", groupBody, token)
	if createGroup.Code != http.StatusCreated {
		t.Fatalf("expected SCIM group create 201, got %d: %s", createGroup.Code, createGroup.Body.String())
	}
	groupID := extractString(t, createGroup.Body.String(), "id")
	for _, expected := range []string{`"displayName":"Platform Engineers"`, `"display":"viewer@example.com"`, `"org_id":"` + orgID + `"`} {
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
	if getGroup.Code != http.StatusOK || strings.Contains(getGroup.Body.String(), "viewer@example.com") {
		t.Fatalf("expected SCIM deprovision to remove team membership: %d %s", getGroup.Code, getGroup.Body.String())
	}

	deleteGroup := performWithToken(server, http.MethodDelete, "/v1/scim/v2/Groups/"+groupID, "", token)
	if deleteGroup.Code != http.StatusNoContent {
		t.Fatalf("expected SCIM group delete 204, got %d: %s", deleteGroup.Code, deleteGroup.Body.String())
	}
	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	for _, action := range []string{"scim.user_create", "scim.user_replace", "scim.user_patch", "scim.user_deprovision", "scim.group_create", "scim.group_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected SCIM audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestSCIMGeneratedPasswordIsOpaqueRandom(t *testing.T) {
	first, err := generatedSCIMPassword()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generatedSCIMPassword()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("expected distinct SCIM placeholder passwords")
	}
	for _, password := range []string{first, second} {
		if strings.HasPrefix(password, "scim-") || !strings.HasPrefix(password, "scim_") || len(password) < 48 {
			t.Fatalf("expected opaque non-timestamp SCIM placeholder password, got %q", password)
		}
	}
}

func TestSCIMBearerTokenProvisioningIsSeparateFromPlatformAdminAuth(t *testing.T) {
	store := control.NewMemoryStore()
	serverAuth := control.NewAuthService("scim-test-secret")
	server := NewServer(Config{
		Store:        store,
		Auth:         serverAuth,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	adminToken := extractString(t, bootstrap.Body.String(), "token")
	scimToken := "scim-secret-token-value-123456"
	update := performWithToken(server, http.MethodPut, "/v1/settings/sso", `{"enabled":false,"default_role":"developer","scim_enabled":true,"scim_token":"`+scimToken+`"}`, adminToken)
	if update.Code != http.StatusOK {
		t.Fatalf("expected SCIM config update 200, got %d: %s", update.Code, update.Body.String())
	}
	for _, forbidden := range []string{scimToken, "scim_token_hash", "hmac-sha256"} {
		if strings.Contains(update.Body.String(), forbidden) {
			t.Fatalf("expected SCIM token material to be redacted from settings response: %s", update.Body.String())
		}
	}
	config, err := store.GetPlatformSSOConfig(context.Background())
	if err != nil {
		t.Fatalf("get sso config: %v", err)
	}
	if !strings.HasPrefix(config.SCIMTokenHash, "hmac-sha256$") {
		t.Fatalf("expected versioned SCIM token hash, got %q", config.SCIMTokenHash)
	}
	if !strings.Contains(update.Body.String(), `"scim_enabled":true`) || !strings.Contains(update.Body.String(), `"scim_token_configured":true`) {
		t.Fatalf("expected SCIM settings status in response: %s", update.Body.String())
	}

	invalid := performWithToken(server, http.MethodGet, "/v1/scim/v2/Users", "", "wrong-token")
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid SCIM bearer to be unauthorized, got %d: %s", invalid.Code, invalid.Body.String())
	}

	create := performWithToken(server, http.MethodPost, "/v1/scim/v2/Users", `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"idp-user@example.com","active":true}`, scimToken)
	if create.Code != http.StatusCreated {
		t.Fatalf("expected SCIM bearer user create 201, got %d: %s", create.Code, create.Body.String())
	}
	if !strings.Contains(create.Body.String(), `"userName":"idp-user@example.com"`) {
		t.Fatalf("expected SCIM user response, got %s", create.Body.String())
	}

	editWithoutToken := performWithToken(server, http.MethodPut, "/v1/settings/sso", `{"enabled":false,"default_role":"viewer","scim_enabled":true}`, adminToken)
	if editWithoutToken.Code != http.StatusOK || !strings.Contains(editWithoutToken.Body.String(), `"scim_token_configured":true`) {
		t.Fatalf("expected SCIM token hash to be preserved on SSO edit, got %d: %s", editWithoutToken.Code, editWithoutToken.Body.String())
	}
	list := performWithToken(server, http.MethodGet, "/v1/scim/v2/Users", "", scimToken)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"userName":"idp-user@example.com"`) {
		t.Fatalf("expected preserved SCIM token to keep working, got %d: %s", list.Code, list.Body.String())
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", adminToken)
	if strings.Contains(auditResponse.Body.String(), scimToken) {
		t.Fatalf("expected audit events to redact SCIM token material: %s", auditResponse.Body.String())
	}
}

func TestStudioForwardAuthUsesSupadupaSessionCookie(t *testing.T) {
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
	cookies := bootstrap.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected bootstrap to set auth cookie")
	}
	if cookies[0].Domain != "" {
		t.Fatalf("expected auth cookie to be host-only by default, got domain %q", cookies[0].Domain)
	}
	token := extractString(t, bootstrap.Body.String(), "token")
	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, token)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("expected org status 201, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"studio-auth","name":"Studio Auth","domain":"apps.example.test"}`, token)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	otherProjectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"studio-other","name":"Studio Other","domain":"apps.example.test"}`, token)
	if otherProjectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected other project status 202, got %d: %s", otherProjectResponse.Code, otherProjectResponse.Body.String())
	}

	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/studio/verify?project_ref=studio-auth", nil)
	unauthorizedRequest.Header.Set("X-Forwarded-Host", "studio-studio-auth.apps.example.test")
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(unauthorizedResponse, unauthorizedRequest)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated studio forward auth to be denied, got %d: %s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/studio/verify?project_ref=studio-auth", nil)
	authorizedRequest.Header.Set("X-Forwarded-Host", "studio-studio-auth.apps.example.test")
	authorizedRequest.AddCookie(cookies[0])
	authorizedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(authorizedResponse, authorizedRequest)
	if authorizedResponse.Code != http.StatusNoContent {
		t.Fatalf("expected authenticated studio forward auth to pass, got %d: %s", authorizedResponse.Code, authorizedResponse.Body.String())
	}
	if got := authorizedResponse.Header().Get("X-Supadupa-User"); got != "admin@example.com" {
		t.Fatalf("expected forwarded user header, got %q", got)
	}
	if got := authorizedResponse.Header().Get("X-Supadupa-Project"); got != "studio-auth" {
		t.Fatalf("expected forwarded project header, got %q", got)
	}

	mismatchRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/studio/verify?project_ref=studio-auth", nil)
	mismatchRequest.Header.Set("X-Forwarded-Host", "studio-studio-other.apps.example.test")
	mismatchRequest.AddCookie(cookies[0])
	mismatchResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(mismatchResponse, mismatchRequest)
	if mismatchResponse.Code != http.StatusNotFound {
		t.Fatalf("expected studio host/project mismatch to fail closed, got %d: %s", mismatchResponse.Code, mismatchResponse.Body.String())
	}

	sessionResponse := performWithToken(server, http.MethodGet, "/v1/projects/studio-auth/studio-session", "", token)
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("expected studio session status 200, got %d: %s", sessionResponse.Code, sessionResponse.Body.String())
	}
	studioCode := extractString(t, sessionResponse.Body.String(), "code")
	studioRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/studio/verify?project_ref=studio-auth", nil)
	studioRequest.Header.Set("X-Forwarded-Host", "studio-studio-auth.apps.example.test")
	studioRequest.Header.Set("X-Forwarded-Uri", "/?supadupa_studio_code="+url.QueryEscape(studioCode))
	studioResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(studioResponse, studioRequest)
	if studioResponse.Code != http.StatusNoContent {
		t.Fatalf("expected scoped studio code to pass, got %d: %s", studioResponse.Code, studioResponse.Body.String())
	}
	if got := studioResponse.Header().Get("X-Supadupa-Project"); got != "studio-auth" {
		t.Fatalf("expected scoped studio project header, got %q", got)
	}
	if len(studioResponse.Result().Cookies()) == 0 {
		t.Fatalf("expected scoped studio code to set follow-up auth cookie")
	}

	// Studio echoes the one-time code back in its own redirect URL (/ ->
	// /project/default?supadupa_studio_code=...). A follow-up request that
	// already carries the session cookie must authenticate via the cookie and
	// ignore the now-spent code, instead of failing to re-consume it.
	echoRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/studio/verify?project_ref=studio-auth", nil)
	echoRequest.Header.Set("X-Forwarded-Host", "studio-studio-auth.apps.example.test")
	echoRequest.Header.Set("X-Forwarded-Uri", "/project/default?supadupa_studio_code="+url.QueryEscape(studioCode))
	for _, cookie := range studioResponse.Result().Cookies() {
		echoRequest.AddCookie(cookie)
	}
	echoResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(echoResponse, echoRequest)
	if echoResponse.Code != http.StatusNoContent {
		t.Fatalf("expected echoed studio code with session cookie to pass via cookie, got %d: %s", echoResponse.Code, echoResponse.Body.String())
	}

	replayRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/studio/verify?project_ref=studio-auth", nil)
	replayRequest.Header.Set("X-Forwarded-Host", "studio-studio-auth.apps.example.test")
	replayRequest.Header.Set("X-Forwarded-Uri", "/?supadupa_studio_code="+url.QueryEscape(studioCode))
	replayResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected one-time studio code replay to be rejected, got %d: %s", replayResponse.Code, replayResponse.Body.String())
	}

	wrongProjectSessionResponse := performWithToken(server, http.MethodGet, "/v1/projects/studio-auth/studio-session", "", token)
	if wrongProjectSessionResponse.Code != http.StatusOK {
		t.Fatalf("expected second studio session status 200, got %d: %s", wrongProjectSessionResponse.Code, wrongProjectSessionResponse.Body.String())
	}
	wrongProjectCode := extractString(t, wrongProjectSessionResponse.Body.String(), "code")
	wrongProjectRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/studio/verify?project_ref=studio-other", nil)
	wrongProjectRequest.Header.Set("X-Forwarded-Host", "studio-studio-other.apps.example.test")
	wrongProjectRequest.Header.Set("X-Forwarded-Uri", "/?supadupa_studio_code="+url.QueryEscape(wrongProjectCode))
	wrongProjectResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(wrongProjectResponse, wrongProjectRequest)
	if wrongProjectResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected scoped studio code to be rejected for another project, got %d: %s", wrongProjectResponse.Code, wrongProjectResponse.Body.String())
	}
}

func TestAuthCookieDomainRequiresExplicitOptIn(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	request := httptest.NewRequest(http.MethodPost, "https://api.supadupa.example/v1/auth/bootstrap", strings.NewReader(`{"email":"admin@example.com","password":"super-secure"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", response.Code, response.Body.String())
	}
	if cookies := response.Result().Cookies(); len(cookies) == 0 || cookies[0].Domain != "" {
		t.Fatalf("expected host-only cookie by default, got %#v", cookies)
	}

	t.Setenv("SUPADUPA_COOKIE_DOMAIN", "supadupa.example")
	request = httptest.NewRequest(http.MethodPost, "https://api.supadupa.example/v1/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"super-secure"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d: %s", response.Code, response.Body.String())
	}
	if cookies := response.Result().Cookies(); len(cookies) == 0 || cookies[0].Domain != "supadupa.example" {
		t.Fatalf("expected explicit cookie domain, got %#v", cookies)
	}
}
