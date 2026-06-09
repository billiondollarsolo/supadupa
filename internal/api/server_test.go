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
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		if got := response.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "PATCH") {
			t.Fatalf("expected CORS preflight to allow PATCH for SCIM updates, got %q", got)
		}
		if got := response.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-Supadupa-Browser") {
			t.Fatalf("expected CORS preflight to allow browser auth marker, got %q", got)
		}
	}
}

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

func assertAuthCookie(t *testing.T, cookies []*http.Cookie, secure bool) {
	t.Helper()
	if len(cookies) == 0 {
		t.Fatalf("expected auth response to set auth cookie")
	}
	cookie := cookies[0]
	if cookie.Name != authCookieName {
		t.Fatalf("expected %s cookie, got %q", authCookieName, cookie.Name)
	}
	if cookie.Value == "" {
		t.Fatalf("expected auth cookie value")
	}
	if cookie.Path != "/" {
		t.Fatalf("expected auth cookie path /, got %q", cookie.Path)
	}
	if !cookie.HttpOnly {
		t.Fatalf("expected auth cookie to be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected auth cookie SameSite=Lax, got %v", cookie.SameSite)
	}
	if cookie.Domain != "" {
		t.Fatalf("expected auth cookie to be host-only by default, got domain %q", cookie.Domain)
	}
	if cookie.Secure != secure {
		t.Fatalf("expected auth cookie Secure=%v, got %v", secure, cookie.Secure)
	}
	if cookie.MaxAge <= 0 {
		t.Fatalf("expected auth cookie MaxAge to be positive, got %d", cookie.MaxAge)
	}
}

func TestDefaultCORSOriginsIncludeConfiguredAdminHost(t *testing.T) {
	t.Setenv("SUPADUPA_ADMIN_HOST", "admin.example.com")
	server := NewServer(Config{Provisioner: composeprovisioner.New()})

	request := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
	request.Header.Set("Origin", "https://admin.example.com")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("expected admin host origin to be allowed, got %q", got)
	}

	localRequest := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
	localRequest.Header.Set("Origin", "http://localhost:3000")
	localResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(localResponse, localRequest)
	if got := localResponse.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected configured admin host to suppress local defaults, got %q", got)
	}
}

func TestListStackReleasesReturnsConfiguredManifests(t *testing.T) {
	t.Setenv("SUPADUPA_SUPPORTED_STACK_VERSIONS", "2026.06.06")
	t.Setenv("SUPADUPA_STACK_RELEASES_JSON", `{
		"2026.06.06": {
			"postgres": "pg-tag",
			"kong": "kong-tag",
			"studio": "studio-tag",
			"postgres_meta": "meta-tag",
			"auth": "auth-tag",
			"rest": "rest-tag",
			"realtime": "realtime-tag",
			"storage": "storage-tag",
			"imgproxy": "imgproxy-tag",
			"edge_runtime": "edge-tag",
			"pooler": "pooler-tag",
			"analytics": "analytics-tag",
			"vector": "vector-tag"
		}
	}`)
	store := control.NewMemoryStore()
	user, err := store.CreateUser(context.Background(), control.CreateUserRequest{Email: "admin@example.com", Password: "super-secure", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	auth := control.NewAuthService("stack-release-test-secret")
	token, err := auth.Issue(user, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store, Auth: auth, Provisioner: fakeProvisioner{}, AuthRequired: true})

	unauthorized := perform(server, http.MethodGet, "/v1/stack-releases", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}
	response := performWithToken(server, http.MethodGet, "/v1/stack-releases", "", token)
	if response.Code != http.StatusOK {
		t.Fatalf("expected release list status 200, got %d: %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`"version":"2026.06.06"`,
		`"postgres":"pg-tag"`,
		`"kong":"kong-tag"`,
		`"studio":"studio-tag"`,
		`"postgres_meta":"meta-tag"`,
		`"auth":"auth-tag"`,
		`"rest":"rest-tag"`,
		`"realtime":"realtime-tag"`,
		`"storage":"storage-tag"`,
		`"edge_runtime":"edge-tag"`,
		`"pooler":"pooler-tag"`,
		`"analytics":"analytics-tag"`,
		`"vector":"vector-tag"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("expected release response to include %s: %s", expected, response.Body.String())
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

func TestRequestBodyLimitRejectsDeclaredOversizedBody(t *testing.T) {
	server := NewServer(Config{Store: control.NewMemoryStore(), AuthRequired: true})
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	request.ContentLength = maxRequestBodyBytes + 1
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "request body too large") {
		t.Fatalf("expected oversized body rejection, got %d: %s", response.Code, response.Body.String())
	}
}

func TestJSONBodyLimitRejectsDeclaredOversizedJSONBody(t *testing.T) {
	server := NewServer(Config{Store: control.NewMemoryStore(), AuthRequired: true})
	body := `{"email":"admin@example.com","password":"` + strings.Repeat("a", defaultJSONBodyBytes) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if len(body) >= maxRequestBodyBytes {
		t.Fatalf("test body should stay below global request cap")
	}
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "request body too large") {
		t.Fatalf("expected oversized JSON body rejection, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCORSRejectsMutatingDisallowedOrigin(t *testing.T) {
	server := NewServer(Config{Store: control.NewMemoryStore(), AuthRequired: true, CORSOrigins: []string{"https://admin.example.com"}})
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"secret"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example.com")
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "origin is not allowed") {
		t.Fatalf("expected disallowed origin rejection, got %d: %s", response.Code, response.Body.String())
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

	disableCode, err := control.TOTPCode(secret, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("expected disable totp code: %v", err)
	}
	disable := performWithToken(server, http.MethodDelete, "/v1/account/mfa", `{"code":"`+disableCode+`"}`, token)
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

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
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

	for i := 0; i < maxMFAAccessAttempts-1; i++ {
		response := performWithToken(server, http.MethodDelete, "/v1/account/mfa", `{"code":"111111"}`, token)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("expected bad mfa disable %d status 400, got %d: %s", i+1, response.Code, response.Body.String())
		}
	}
	disableCode, err := control.TOTPCode(secret, now)
	if err != nil {
		t.Fatalf("expected disable totp code: %v", err)
	}
	disable := performWithToken(server, http.MethodDelete, "/v1/account/mfa", `{"code":"`+disableCode+`"}`, token)
	if disable.Code != http.StatusOK || !strings.Contains(disable.Body.String(), `"enabled":false`) {
		t.Fatalf("expected successful mfa disable before throttle, got %d: %s", disable.Code, disable.Body.String())
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
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

func TestRuntimeConfigRequiresAdminAndRedactsOperationalCommands(t *testing.T) {
	t.Setenv("SUPADUPA_COMPOSE_APPLY", "true")
	t.Setenv("SUPADUPA_COMPOSE_BACKUP_DEFAULTS", "true")
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "true")
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "true")
	t.Setenv("SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE", "true")
	t.Setenv("SUPADUPA_LOGICAL_BACKUP_COMMAND", "secret logical command")
	t.Setenv("SUPADUPA_PITR_RESTORE_COMMAND", "secret pitr command")

	store := control.NewMemoryStore()
	user, err := store.CreateUser(context.Background(), control.CreateUserRequest{Email: "admin@example.com", Password: "super-secure", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	auth := control.NewAuthService("runtime-config-test-secret")
	token, err := auth.Issue(user, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store, Auth: auth, Provisioner: fakeProvisioner{}, AuthRequired: true})

	unauthorized := perform(server, http.MethodGet, "/v1/runtime-config", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized runtime config status, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}
	response := performWithToken(server, http.MethodGet, "/v1/runtime-config", "", token)
	if response.Code != http.StatusOK {
		t.Fatalf("expected runtime config 200, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{
		`"provisioner":"fake"`,
		`"compose":true`,
		`"compose_defaults":true`,
		`"logical_configured":true`,
		`"pitr_restore_configured":true`,
		`"require_recovery_ready_targets":true`,
		`"require_durable_backup":true`,
		`"failure_auto_restore":true`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected runtime config to contain %s, got %s", expected, body)
		}
	}
	for _, unexpected := range []string{"secret logical command", "secret pitr command", "SUPADUPA_LOGICAL_BACKUP_COMMAND", "SUPADUPA_PITR_RESTORE_COMMAND"} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("runtime config leaked %q: %s", unexpected, body)
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

	invalidStack := performWithToken(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.example.com","stack_version":"2026.06.05","profile":"essential","resource_tier":"medium","backup_schedule":"hourly"}`, token)
	if invalidStack.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid stack defaults 400, got %d: %s", invalidStack.Code, invalidStack.Body.String())
	}

	update := performWithToken(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.example.com","stack_version":"15.8.1.060","profile":"essential","resource_tier":"medium","backup_schedule":"hourly","feature_flags":{"single_org_mode":false,"read_replicas":true,"kubernetes_operator":true},"smtp":{"enabled":true,"host":"smtp.example.com","port":2525,"sender_name":"supadupa","sender_email":"noreply@example.com","username":"apikey","password_handle":"secret://platform/smtp-password","tls_mode":"implicit"}}`, token)
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"domain":"apps.example.com"`) || !strings.Contains(update.Body.String(), `"backup_schedule":"hourly"`) || !strings.Contains(update.Body.String(), `"host":"smtp.example.com"`) || !strings.Contains(update.Body.String(), `"password_handle":"secret://platform/smtp-password"`) || !strings.Contains(update.Body.String(), `"single_org_mode":false`) || !strings.Contains(update.Body.String(), `"read_replicas":true`) || !strings.Contains(update.Body.String(), `"kubernetes_operator":true`) || !strings.Contains(update.Body.String(), `"supabase_cli_compat":true`) {
		t.Fatalf("expected updated defaults: %d %s", update.Code, update.Body.String())
	}
	invalidSMTP := performWithToken(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.example.com","stack_version":"15.8.1.060","profile":"essential","resource_tier":"medium","backup_schedule":"hourly","smtp":{"enabled":true,"host":"smtp.example.com","port":587,"password_handle":"raw","tls_mode":"starttls"}}`, token)
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
	if provisioner.spec.Domain != "apps.example.com" || provisioner.spec.StackVersion != "15.8.1.060" || provisioner.spec.Profile != control.StackProfileEssential || provisioner.spec.ResourceTier != control.ResourceTierMedium {
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

func TestBackupStorageTargetsAPIAndProjectPolicy(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	unauthorized := perform(server, http.MethodGet, "/v1/backup-storage-targets", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized target list, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	token := extractString(t, bootstrap.Body.String(), "token")
	var s3Mu sync.Mutex
	s3Objects := map[string][]byte{}
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read fake s3 body: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			s3Mu.Lock()
			s3Objects[r.URL.Path] = body
			s3Mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			s3Mu.Lock()
			body, ok := s3Objects[r.URL.Path]
			s3Mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(body)
		case http.MethodDelete:
			s3Mu.Lock()
			delete(s3Objects, r.URL.Path)
			s3Mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3Server.Close()
	targetBody := fmt.Sprintf(`{"name":"Primary S3","type":"s3","endpoint":%q,"region":"auto","bucket":"supadupa-backups","prefix":"control","access_key_id":"access","secret_access_key":"super-secret","force_path_style":true,"default":false}`, s3Server.URL)
	created := performWithToken(server, http.MethodPost, "/v1/backup-storage-targets", targetBody, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected target create 201, got %d: %s", created.Code, created.Body.String())
	}
	targetID := extractString(t, created.Body.String(), "id")
	if strings.Contains(created.Body.String(), "super-secret") || !strings.Contains(created.Body.String(), `"secret_configured":true`) {
		t.Fatalf("expected redacted target response, got %s", created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"durable_off_host":false`) || !strings.Contains(created.Body.String(), `"recovery_ready":false`) || !strings.Contains(created.Body.String(), `"readiness_status":"local-or-loopback"`) {
		t.Fatalf("expected local target readiness metadata, got %s", created.Body.String())
	}

	listed := performWithToken(server, http.MethodGet, "/v1/backup-storage-targets", "", token)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), targetID) || !strings.Contains(listed.Body.String(), `"readiness_status":"local-or-loopback"`) || strings.Contains(listed.Body.String(), "super-secret") {
		t.Fatalf("expected redacted target list, got %d: %s", listed.Code, listed.Body.String())
	}
	tested := performWithToken(server, http.MethodPost, "/v1/backup-storage-targets/"+targetID+"/test", "", token)
	if tested.Code != http.StatusOK || !strings.Contains(tested.Body.String(), `"last_test_status":"passed"`) || !strings.Contains(tested.Body.String(), `"recovery_ready":false`) || !strings.Contains(tested.Body.String(), `"readiness_status":"local-or-loopback"`) || strings.Contains(tested.Body.String(), "super-secret") {
		t.Fatalf("expected passed redacted target test, got %d: %s", tested.Code, tested.Body.String())
	}
	listedAfterTest := performWithToken(server, http.MethodGet, "/v1/backup-storage-targets", "", token)
	if listedAfterTest.Code != http.StatusOK || !strings.Contains(listedAfterTest.Body.String(), `"last_test_status":"passed"`) || strings.Contains(listedAfterTest.Body.String(), "super-secret") {
		t.Fatalf("expected listed target test status, got %d: %s", listedAfterTest.Code, listedAfterTest.Body.String())
	}
	platformBackup := performWithToken(server, http.MethodPost, "/v1/platform/backups", "", token)
	if platformBackup.Code != http.StatusCreated || !strings.Contains(platformBackup.Body.String(), `"kind":"control-plane"`) || strings.Contains(platformBackup.Body.String(), `"storage_target_id"`) || strings.Contains(platformBackup.Body.String(), `"remote_location"`) {
		t.Fatalf("expected platform backup response, got %d: %s", platformBackup.Code, platformBackup.Body.String())
	}
	if strings.Contains(platformBackup.Body.String(), "super-secret") {
		t.Fatalf("platform backup response leaked target secret: %s", platformBackup.Body.String())
	}
	platformBackups := performWithToken(server, http.MethodGet, "/v1/platform/backups", "", token)
	if platformBackups.Code != http.StatusOK || !strings.Contains(platformBackups.Body.String(), `"kind":"control-plane"`) {
		t.Fatalf("expected platform backups list, got %d: %s", platformBackups.Code, platformBackups.Body.String())
	}

	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Backup API"}`, token)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("expected org create 201, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"backup-target-api","name":"Backup Target API","domain":"apps.example.test"}`, token)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	policy := performWithToken(server, http.MethodPut, "/v1/projects/backup-target-api/backups/policy", `{"enabled":true,"schedule":"hourly","kind":"logical","storage_target_id":"`+targetID+`"}`, token)
	if policy.Code != http.StatusOK || !strings.Contains(policy.Body.String(), `"storage_target_id":"`+targetID+`"`) {
		t.Fatalf("expected policy target, got %d: %s", policy.Code, policy.Body.String())
	}

	audit := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	if strings.Contains(audit.Body.String(), "super-secret") {
		t.Fatalf("audit log leaked target secret: %s", audit.Body.String())
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
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	otherProjectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"studio-other","name":"Studio Other","domain":"apps.example.test"}`, token)
	if otherProjectResponse.Code != http.StatusCreated {
		t.Fatalf("expected other project status 201, got %d: %s", otherProjectResponse.Code, otherProjectResponse.Body.String())
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
	enableOrgFeaturesForTest(t, store, orgID, "preview_branches", "custom_domains")
	projectBody := `{"ref":"branch-source","name":"Branch Source","domain":"supadupa.test","profile":"full","resource_tier":"small","services":{"storage":true},"environment":{"CUSTOM":"value"}}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	otherProjectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"branch-other","name":"Branch Other","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if otherProjectResponse.Code != http.StatusCreated {
		t.Fatalf("expected other project status 201, got %d: %s", otherProjectResponse.Code, otherProjectResponse.Body.String())
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
		`"with_data":false`,
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
	for _, reserved := range []string{
		"branch-preview.supadupa.test",
		"studio-branch-preview.supadupa.test",
		"storage-branch-preview.supadupa.test",
		"db-branch-preview.supadupa.test",
		"pooler-branch-preview.supadupa.test",
	} {
		reservedResponse := perform(server, http.MethodPost, "/v1/projects/branch-other/domains", fmt.Sprintf(`{"fqdn":%q}`, reserved))
		if reservedResponse.Code != http.StatusConflict {
			t.Fatalf("expected branch generated domain %s conflict, got %d: %s", reserved, reservedResponse.Code, reservedResponse.Body.String())
		}
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.branch_create"`) {
		t.Fatalf("expected branch create audit event: %s", auditResponse.Body.String())
	}
	if strings.Contains(auditResponse.Body.String(), `"clone_state"`) || !strings.Contains(auditResponse.Body.String(), `"with_data":"false"`) {
		t.Fatalf("expected data-less branch audit metadata without clone state: %s", auditResponse.Body.String())
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

	createResponse := perform(server, http.MethodPost, "/v1/projects/branch-source/branches", `{"ref":"branch-secret-preview","name":"Preview","ttl_hours":24,"with_data":true}`)
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
		"SUPABASE_PUBLISHABLE_KEY":         "pub_",
		"SUPABASE_SECRET_KEY":              "sec_",
		"S3_ACCESS_KEY":                    "s3ak_",
		"S3_SECRET_KEY":                    "s3sk_",
	} {
		value := provisioner.spec.Environment[key]
		if !strings.HasPrefix(value, prefix) {
			t.Fatalf("expected branch provisioner env %s to have prefix %q, got %q in %#v", key, prefix, value, provisioner.spec.Environment)
		}
	}
	if value := provisioner.spec.Environment["POSTGRES_PASSWORD"]; len(value) != 48 || !isLowerHexForTest(value) {
		t.Fatalf("expected branch provisioner env POSTGRES_PASSWORD to be 48 lowercase hex chars, got %q in %#v", value, provisioner.spec.Environment)
	}
	for _, key := range []string{"ANON_KEY", "SERVICE_ROLE_KEY"} {
		if strings.Count(provisioner.spec.Environment[key], ".") != 2 {
			t.Fatalf("expected branch provisioner env %s to be a JWT, got %q in %#v", key, provisioner.spec.Environment[key], provisioner.spec.Environment)
		}
	}
	if provisioner.spec.Environment["POSTGRES_PASSWORD"] == "source-should-not-win" {
		t.Fatalf("source db password won over branch managed secret")
	}
	if provisioner.clonedBranch.SourceRef != "branch-source" || provisioner.clonedBranch.BranchRef != "branch-secret-preview" || provisioner.clonedBranch.BranchID == "" || !provisioner.clonedBranch.WithData {
		t.Fatalf("expected branch clone call, got %#v", provisioner.clonedBranch)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"clone_state":"dry-run"`) || !strings.Contains(auditResponse.Body.String(), `"clone_path":"branch-clone.sql"`) || !strings.Contains(auditResponse.Body.String(), `"with_data":"true"`) {
		t.Fatalf("expected branch clone metadata in audit: %s", auditResponse.Body.String())
	}
}

func TestProjectReplicasCreateListUsageAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host create 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	otherOrgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Replica Domain Peer"}`)
	otherOrgID := extractString(t, otherOrgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "read_replicas", "custom_domains")
	enableOrgFeaturesForTest(t, store, otherOrgID, "custom_domains")
	projectBody := `{"ref":"replica-proj","name":"Replica","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	otherProjectResponse := perform(server, http.MethodPost, "/v1/orgs/"+otherOrgID+"/projects", `{"ref":"replica-domain-other","name":"Replica Domain Other","host_id":"`+hostID+`","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if otherProjectResponse.Code != http.StatusCreated {
		t.Fatalf("expected other replica domain project status 201, got %d: %s", otherProjectResponse.Code, otherProjectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"bad name","tier":"small"}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid replica name 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	invalidDNSLabelResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"east-","tier":"small"}`)
	if invalidDNSLabelResponse.Code != http.StatusBadRequest || !strings.Contains(invalidDNSLabelResponse.Body.String(), "cannot start or end with a dash") {
		t.Fatalf("expected invalid replica DNS label 400, got %d: %s", invalidDNSLabelResponse.Code, invalidDNSLabelResponse.Body.String())
	}
	tooLongPublicHostResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","tier":"small"}`)
	if tooLongPublicHostResponse.Code != http.StatusBadRequest || !strings.Contains(tooLongPublicHostResponse.Body.String(), "63-character DNS label limit") {
		t.Fatalf("expected replica public DNS label 400, got %d: %s", tooLongPublicHostResponse.Code, tooLongPublicHostResponse.Body.String())
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
		`"read_uri":"postgres://postgres:${DB_PASSWORD}@db-replica-east-replica-proj.supadupa.test:5432/postgres?sslmode=require"`,
		`"public_read_uri":"postgres://postgres:${DB_PASSWORD}@db-replica-east-replica-proj.supadupa.test:5432/postgres?sslmode=require"`,
		`"internal_read_uri":"postgres://postgres:${DB_PASSWORD}@east.replica-proj.replica.internal:5432/postgres"`,
	} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected %s in replica create response: %s", expected, createResponse.Body.String())
		}
	}
	enableDatabaseExposure(t, server, "replica-proj", "public", "")
	manifestResponse := perform(server, http.MethodGet, "/v1/projects/replica-proj/route-manifest", "")
	for _, expected := range []string{
		`"name":"db-replica-east"`,
		`"fqdn":"db-replica-east-replica-proj.supadupa.test"`,
		`"entrypoint":"postgres"`,
		`"upstream_address":"replica-proj-db-replica-east:5432"`,
	} {
		if !strings.Contains(manifestResponse.Body.String(), expected) {
			t.Fatalf("expected route manifest value %s: %s", expected, manifestResponse.Body.String())
		}
	}
	replicaReservedResponse := perform(server, http.MethodPost, "/v1/projects/replica-domain-other/domains", `{"fqdn":"db-replica-east-replica-proj.supadupa.test"}`)
	if replicaReservedResponse.Code != http.StatusConflict {
		t.Fatalf("expected replica generated domain conflict, got %d: %s", replicaReservedResponse.Code, replicaReservedResponse.Body.String())
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
	for _, expected := range []string{`"read_replicas":2`, `"cpu":3`, `"ram_mb":6144`, `"disk_gb":60`, `"projects":1`} {
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
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
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
	for _, expected := range []string{`"cpu":4`, `"ram_mb":8192`, `"disk_gb":100`, `"projects":1`} {
		if !strings.Contains(usageResponse.Body.String(), expected) {
			t.Fatalf("expected scaled usage %s: %s", expected, usageResponse.Body.String())
		}
	}
	hostsResponse := perform(server, http.MethodGet, "/v1/hosts", "")
	for _, expected := range []string{`"used":{"cpu":4`, `"ram_mb":8192`, `"disk_gb":100`, `"projects":1`} {
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
	t.Setenv("SUPADUPA_ADMIN_HOST", "admin.supadupa.test")
	t.Setenv("SUPADUPA_API_HOST", "api.supadupa.test")
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

	collisionResponse := perform(server, http.MethodPost, "/v1/projects/domain-proj/domains", `{"fqdn":"api-example.com"}`)
	if collisionResponse.Code != http.StatusConflict {
		t.Fatalf("expected route-name collision conflict, got %d: %s", collisionResponse.Code, collisionResponse.Body.String())
	}

	secondProjectBody := `{"ref":"domain-proj-two","name":"Domain Two","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	secondProjectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", secondProjectBody)
	if secondProjectResponse.Code != http.StatusCreated {
		t.Fatalf("expected second project status 201, got %d: %s", secondProjectResponse.Code, secondProjectResponse.Body.String())
	}
	crossProjectDuplicateResponse := perform(server, http.MethodPost, "/v1/projects/domain-proj-two/domains", `{"fqdn":"api.example.com"}`)
	if crossProjectDuplicateResponse.Code != http.StatusConflict {
		t.Fatalf("expected cross-project duplicate domain conflict, got %d: %s", crossProjectDuplicateResponse.Code, crossProjectDuplicateResponse.Body.String())
	}

	for _, reserved := range []string{
		"admin.supadupa.test",
		"api.supadupa.test",
		"supadupa.test",
		"domain-proj.supadupa.test",
		"studio-domain-proj.supadupa.test",
		"storage-domain-proj.supadupa.test",
		"db-domain-proj.supadupa.test",
		"pooler-domain-proj.supadupa.test",
		"domain-proj-two.supadupa.test",
		"studio-domain-proj-two.supadupa.test",
		"storage-domain-proj-two.supadupa.test",
		"db-domain-proj-two.supadupa.test",
		"pooler-domain-proj-two.supadupa.test",
	} {
		reservedResponse := perform(server, http.MethodPost, "/v1/projects/domain-proj/domains", fmt.Sprintf(`{"fqdn":%q}`, reserved))
		if reservedResponse.Code != http.StatusConflict {
			t.Fatalf("expected reserved domain %s conflict, got %d: %s", reserved, reservedResponse.Code, reservedResponse.Body.String())
		}
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

func TestProjectDomainBYOCertificateLifecycle(t *testing.T) {
	certRoot := t.TempDir()
	routeRoot := t.TempDir()
	t.Setenv("SUPADUPA_CERT_ROOT", certRoot)
	t.Setenv("SUPADUPA_ROUTES_ROOT", routeRoot)
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "custom_domains")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"byo-domain","name":"BYO Domain","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	addResponse := perform(server, http.MethodPost, "/v1/projects/byo-domain/domains", `{"fqdn":"api.example.com"}`)
	if addResponse.Code != http.StatusCreated {
		t.Fatalf("expected domain create 201, got %d: %s", addResponse.Code, addResponse.Body.String())
	}

	certPEM, keyPEM := testServerDomainCertificate(t, []string{"api.example.com"}, time.Now().UTC().Add(time.Hour))
	uploadBody, err := json.Marshal(control.ProjectDomainCertificateInput{CertificatePEM: certPEM, PrivateKeyPEM: keyPEM})
	if err != nil {
		t.Fatal(err)
	}
	uploadResponse := perform(server, http.MethodPut, "/v1/projects/byo-domain/domains/api.example.com/certificate", string(uploadBody))
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("expected certificate upload 200, got %d: %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	for _, expected := range []string{`"cert_status":"uploaded"`, `"cert_mode":"byo"`, `"cert_fingerprint":`, `"cert_not_after":`} {
		if !strings.Contains(uploadResponse.Body.String(), expected) {
			t.Fatalf("expected upload response to contain %s: %s", expected, uploadResponse.Body.String())
		}
	}
	if strings.Contains(uploadResponse.Body.String(), "PRIVATE KEY") {
		t.Fatalf("private key leaked in upload response: %s", uploadResponse.Body.String())
	}
	for _, path := range []string{
		filepath.Join(certRoot, "byo-domain", "api.example.com.crt"),
		filepath.Join(certRoot, "byo-domain", "api.example.com.key"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected uploaded certificate file %s: %v", path, err)
		}
	}
	routePayload, err := os.ReadFile(filepath.Join(routeRoot, "byo-domain.yaml"))
	if err != nil {
		t.Fatalf("read route file: %v", err)
	}
	if !strings.Contains(string(routePayload), `certFile: "/certs/byo-domain/api.example.com.crt"`) || !strings.Contains(string(routePayload), `keyFile: "/certs/byo-domain/api.example.com.key"`) {
		t.Fatalf("expected BYO cert route config, got:\n%s", routePayload)
	}
	connectResponse := perform(server, http.MethodGet, "/v1/projects/byo-domain/connect", "")
	if connectResponse.Code != http.StatusOK {
		t.Fatalf("expected connect payload 200, got %d: %s", connectResponse.Code, connectResponse.Body.String())
	}
	for _, expected := range []string{
		`"api_url":"https://byo-domain.supadupa.test"`,
		`"custom_api_urls":["https://api.example.com"]`,
		`"fqdn":"api.example.com"`,
		`"api_custom":"https://api.example.com"`,
	} {
		if !strings.Contains(connectResponse.Body.String(), expected) {
			t.Fatalf("expected connect payload to contain %s: %s", expected, connectResponse.Body.String())
		}
	}
	cliResponse := perform(server, http.MethodGet, "/v1/projects/byo-domain/connect/cli", "")
	if cliResponse.Code != http.StatusOK {
		t.Fatalf("expected cli profile 200, got %d: %s", cliResponse.Code, cliResponse.Body.String())
	}
	for _, expected := range []string{
		`"api_url":"https://byo-domain.supadupa.test"`,
		`"custom_api_urls":["https://api.example.com"]`,
		`"SUPADUPA_CUSTOM_API_URL":"https://api.example.com"`,
		`custom_api_urls = [\"https://api.example.com\"]`,
	} {
		if !strings.Contains(cliResponse.Body.String(), expected) {
			t.Fatalf("expected cli profile to contain %s: %s", expected, cliResponse.Body.String())
		}
	}

	badCert, badKey := testServerDomainCertificate(t, []string{"other.example.com"}, time.Now().UTC().Add(time.Hour))
	badBody, err := json.Marshal(control.ProjectDomainCertificateInput{CertificatePEM: badCert, PrivateKeyPEM: badKey})
	if err != nil {
		t.Fatal(err)
	}
	badResponse := perform(server, http.MethodPut, "/v1/projects/byo-domain/domains/api.example.com/certificate", string(badBody))
	if badResponse.Code != http.StatusBadRequest || !strings.Contains(badResponse.Body.String(), "not valid for api.example.com") {
		t.Fatalf("expected hostname mismatch rejection, got %d: %s", badResponse.Code, badResponse.Body.String())
	}

	resetResponse := perform(server, http.MethodDelete, "/v1/projects/byo-domain/domains/api.example.com/certificate", "")
	if resetResponse.Code != http.StatusOK || !strings.Contains(resetResponse.Body.String(), `"cert_mode":"manual"`) {
		t.Fatalf("expected cert reset to manual plan, got %d: %s", resetResponse.Code, resetResponse.Body.String())
	}
	if _, err := os.Stat(filepath.Join(certRoot, "byo-domain", "api.example.com.key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected uploaded key removed, got err=%v", err)
	}

	logsResponse := perform(server, http.MethodGet, "/v1/projects/byo-domain/logs", "")
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if strings.Contains(logsResponse.Body.String(), "PRIVATE KEY") || strings.Contains(auditResponse.Body.String(), "PRIVATE KEY") {
		t.Fatalf("private key leaked in logs or audit: logs=%s audit=%s", logsResponse.Body.String(), auditResponse.Body.String())
	}
}

func TestProjectCustomDomainsReserveAppsControlPlaneTopology(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "custom_domains")
	collidingProjectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"admin","name":"Admin Collision","domain":"example.com"}`)
	if collidingProjectResponse.Code != http.StatusConflict || !strings.Contains(collidingProjectResponse.Body.String(), "platform host topology") {
		t.Fatalf("expected generated platform host conflict, got %d: %s", collidingProjectResponse.Code, collidingProjectResponse.Body.String())
	}
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"apps-proj","name":"Apps Project","domain":"apps.example.com"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	for _, reserved := range []string{
		"admin.example.com",
		"api.example.com",
		"apps.example.com",
		"apps-proj.apps.example.com",
		"studio-apps-proj.apps.example.com",
		"storage-apps-proj.apps.example.com",
		"db-apps-proj.apps.example.com",
		"pooler-apps-proj.apps.example.com",
	} {
		response := perform(server, http.MethodPost, "/v1/projects/apps-proj/domains", fmt.Sprintf(`{"fqdn":%q}`, reserved))
		if response.Code != http.StatusConflict {
			t.Fatalf("expected reserved domain %s conflict, got %d: %s", reserved, response.Code, response.Body.String())
		}
	}

	response := perform(server, http.MethodPost, "/v1/projects/apps-proj/domains", `{"fqdn":"app.example.net"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected unrelated custom domain accepted, got %d: %s", response.Code, response.Body.String())
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
	seedProjectSecrets(t, store, "config-proj", "captcha-secret", "google-oauth-secret", "discord-secret", "figma-secret", "snapchat-secret", "messagebird-key", "twilio-token", "sms-test-otp", "smtp-password")

	defaultResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/auth", "")
	if defaultResponse.Code != http.StatusOK || !strings.Contains(defaultResponse.Body.String(), `"email_enabled":"true"`) || !strings.Contains(defaultResponse.Body.String(), `"mfa_totp_enroll_enabled":"true"`) || !strings.Contains(defaultResponse.Body.String(), `"mfa_phone_otp_length":"6"`) || !strings.Contains(defaultResponse.Body.String(), `"captcha_secret_handle":""`) {
		t.Fatalf("expected default auth config: %d %s", defaultResponse.Code, defaultResponse.Body.String())
	}

	defaultFunctionsResponse := perform(server, http.MethodGet, "/v1/projects/config-proj/config/functions", "")
	if defaultFunctionsResponse.Code != http.StatusOK || !strings.Contains(defaultFunctionsResponse.Body.String(), `"worker_timeout_ms":"60000"`) {
		t.Fatalf("expected default functions timeout config: %d %s", defaultFunctionsResponse.Code, defaultFunctionsResponse.Body.String())
	}
	invalidFunctionsTimeoutResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/functions", `{"config":{"worker_timeout_ms":"50"}}`)
	if invalidFunctionsTimeoutResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid functions timeout 400, got %d: %s", invalidFunctionsTimeoutResponse.Code, invalidFunctionsTimeoutResponse.Body.String())
	}
	functionsPartialUpdateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/functions", `{"config":{"deployment_policy":"locked"}}`)
	if functionsPartialUpdateResponse.Code != http.StatusOK || !strings.Contains(functionsPartialUpdateResponse.Body.String(), `"worker_timeout_ms":"60000"`) || !strings.Contains(functionsPartialUpdateResponse.Body.String(), `"deployment_policy":"locked"`) {
		t.Fatalf("expected functions config defaults to survive partial update: %d %s", functionsPartialUpdateResponse.Code, functionsPartialUpdateResponse.Body.String())
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
	if providersDefaultResponse.Code != http.StatusOK || !strings.Contains(providersDefaultResponse.Body.String(), `"oauth_google_enabled":"false"`) || !strings.Contains(providersDefaultResponse.Body.String(), `"oauth_discord_enabled":"false"`) || !strings.Contains(providersDefaultResponse.Body.String(), `"oauth_figma_enabled":"false"`) || !strings.Contains(providersDefaultResponse.Body.String(), `"oauth_snapchat_enabled":"false"`) || !strings.Contains(providersDefaultResponse.Body.String(), `"saml_enabled":"false"`) {
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
	invalidSMSLengthResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"sms_otp_length":"3"}}`)
	if invalidSMSLengthResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid sms otp length 400, got %d: %s", invalidSMSLengthResponse.Code, invalidSMSLengthResponse.Body.String())
	}
	invalidSMSFrequencyResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"sms_max_frequency":"often"}}`)
	if invalidSMSFrequencyResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid sms frequency 400, got %d: %s", invalidSMSFrequencyResponse.Code, invalidSMSFrequencyResponse.Body.String())
	}
	invalidSMSTestOTPResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"sms_test_otp_handle":"raw-test-otp"}}`)
	if invalidSMSTestOTPResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid sms test otp handle 400, got %d: %s", invalidSMSTestOTPResponse.Code, invalidSMSTestOTPResponse.Body.String())
	}
	invalidSMSTestOTPExpiryResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"sms_test_otp_valid_until":"tomorrow"}}`)
	if invalidSMSTestOTPExpiryResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid sms test otp expiry 400, got %d: %s", invalidSMSTestOTPExpiryResponse.Code, invalidSMSTestOTPExpiryResponse.Body.String())
	}
	invalidOIDCIssuerResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"oauth_oidc_issuer_url":"http://issuer.example.com"}}`)
	if invalidOIDCIssuerResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid oidc issuer 400, got %d: %s", invalidOIDCIssuerResponse.Code, invalidOIDCIssuerResponse.Body.String())
	}
	providersUpdateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/auth_providers", `{"config":{"oauth_google_enabled":"true","oauth_google_client_id":"google-client","oauth_google_client_secret_handle":"secret://projects/config-proj/google-oauth-secret","oauth_discord_enabled":"true","oauth_discord_client_id":"discord-client","oauth_discord_client_secret_handle":"secret://projects/config-proj/discord-secret","oauth_figma_enabled":"true","oauth_figma_client_id":"figma-client","oauth_figma_client_secret_handle":"secret://projects/config-proj/figma-secret","oauth_gitlab_enabled":"true","oauth_gitlab_url":"https://gitlab.example.com","oauth_gitlab_redirect_uri":"https://app.example.com/auth/callback","oauth_gitlab_skip_nonce_check":"true","oauth_snapchat_enabled":"true","oauth_snapchat_client_id":"snapchat-client","oauth_snapchat_client_secret_handle":"secret://projects/config-proj/snapchat-secret","oauth_oidc_enabled":"true","oauth_oidc_issuer_url":"https://issuer.example.com","oauth_oidc_client_id":"oidc-client","oauth_oidc_client_secret_handle":"secret://projects/config-proj/oidc-secret","oauth_oidc_scopes":"openid email profile","phone_enabled":"true","sms_provider":"messagebird","sms_otp_exp":"90","sms_otp_length":"8","sms_max_frequency":"45s","sms_template":"Code: {{ .Code }}","sms_test_otp_handle":"secret://projects/config-proj/sms-test-otp","sms_test_otp_valid_until":"2026-12-31T23:59:59Z","sms_messagebird_originator":"Supadupa","sms_messagebird_access_key_handle":"secret://projects/config-proj/messagebird-key","sms_twilio_auth_token_handle":"secret://projects/config-proj/twilio-token","saml_enabled":"true","saml_metadata_url":"https://idp.example.com/metadata","third_party_jwt_issuer":"https://issuer.example.com","web3_ethereum_enabled":"true"}}`)
	if providersUpdateResponse.Code != http.StatusOK {
		t.Fatalf("expected auth providers update 200, got %d: %s", providersUpdateResponse.Code, providersUpdateResponse.Body.String())
	}
	for _, expected := range []string{`"oauth_google_enabled":"true"`, `"oauth_google_client_id":"google-client"`, `"oauth_google_client_secret_handle":"secret://projects/config-proj/google-oauth-secret"`, `"oauth_discord_client_secret_handle":"secret://projects/config-proj/discord-secret"`, `"oauth_figma_client_id":"figma-client"`, `"oauth_figma_client_secret_handle":"secret://projects/config-proj/figma-secret"`, `"oauth_gitlab_url":"https://gitlab.example.com"`, `"oauth_gitlab_skip_nonce_check":"true"`, `"oauth_snapchat_client_id":"snapchat-client"`, `"oauth_snapchat_client_secret_handle":"secret://projects/config-proj/snapchat-secret"`, `"oauth_oidc_enabled":"true"`, `"oauth_oidc_issuer_url":"https://issuer.example.com"`, `"oauth_oidc_client_secret_handle":"secret://projects/config-proj/oidc-secret"`, `"phone_enabled":"true"`, `"sms_provider":"messagebird"`, `"sms_otp_exp":"90"`, `"sms_otp_length":"8"`, `"sms_max_frequency":"45s"`, `"sms_template":"Code: {{ .Code }}"`, `"sms_test_otp_handle":"secret://projects/config-proj/sms-test-otp"`, `"sms_test_otp_valid_until":"2026-12-31T23:59:59Z"`, `"sms_messagebird_access_key_handle":"secret://projects/config-proj/messagebird-key"`, `"sms_twilio_auth_token_handle":"secret://projects/config-proj/twilio-token"`, `"saml_metadata_url":"https://idp.example.com/metadata"`, `"web3_ethereum_enabled":"true"`} {
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
	invalidPoolerPortResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/pooler", `{"config":{"transaction_port":"7654"}}`)
	if invalidPoolerPortResponse.Code != http.StatusBadRequest || !strings.Contains(invalidPoolerPortResponse.Body.String(), "fixed at 6543") {
		t.Fatalf("expected fixed pooler port rejection, got %d: %s", invalidPoolerPortResponse.Code, invalidPoolerPortResponse.Body.String())
	}
	poolerUpdateResponse := perform(server, http.MethodPut, "/v1/projects/config-proj/config/pooler", `{"config":{"dedicated_pooler_enabled":"true","dedicated_pooler_tier":"large","pool_mode":"both","default_pool_size":"50","max_client_connections":"500","transaction_port":"6543","session_port":"5432"}}`)
	if poolerUpdateResponse.Code != http.StatusOK {
		t.Fatalf("expected pooler config update 200, got %d: %s", poolerUpdateResponse.Code, poolerUpdateResponse.Body.String())
	}
	for _, expected := range []string{`"dedicated_pooler_enabled":"true"`, `"dedicated_pooler_tier":"large"`, `"pool_mode":"both"`, `"transaction_port":"6543"`, `"session_port":"5432"`} {
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
	routesRoot := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", routesRoot)
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
	routePath := filepath.Join(routesRoot, "runtime-services-proj.yaml")
	if err := os.WriteFile(routePath, []byte("http:\n  routers: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	enableDatabaseExposure(t, server, "runtime-services-proj", "public", "")
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
	routePayload, err := os.ReadFile(routePath)
	if err != nil {
		t.Fatal(err)
	}
	routeBody := string(routePayload)
	for _, expected := range []string{
		"tcp:",
		"HostSNI(`db-runtime-services-proj.supadupa.test`)",
		"HostSNI(`pooler-runtime-services-proj.supadupa.test`)",
		"runtime-services-proj-postgres-alpn",
	} {
		if !strings.Contains(routeBody, expected) {
			t.Fatalf("expected refreshed route config to contain %q:\n%s", expected, routeBody)
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.services_update"`) || !strings.Contains(auditResponse.Body.String(), `"runtime_synced":"true"`) || !strings.Contains(auditResponse.Body.String(), `"route_path":"`+routePath+`"`) {
		t.Fatalf("expected services audit metadata: %s", auditResponse.Body.String())
	}
}

func TestProjectServicesUpdateClearsStaleErrorAfterSuccessfulRuntimeSync(t *testing.T) {
	routesRoot := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", routesRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"service-error-proj","name":"Service Error","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if _, err := store.UpdateProjectStatus(context.Background(), "service-error-proj", control.ProjectError, "previous reconcile error"); err != nil {
		t.Fatal(err)
	}

	updateResponse := perform(server, http.MethodPut, "/v1/projects/service-error-proj/services", `{"services":{"storage":true,"functions":true}}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected services update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}

	project, err := store.GetProject(context.Background(), "service-error-proj")
	if err != nil {
		t.Fatal(err)
	}
	if project.Status != control.ProjectHealthy || project.Message != "enabled services updated" {
		t.Fatalf("expected services update to clear stale error, got status=%q message=%q", project.Status, project.Message)
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

	invalidResponse := perform(server, http.MethodPut, "/v1/projects/network-proj/config/network", `{"config":{"http_allowlist":"bad-cidr"}}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid network config 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	updateResponse := perform(server, http.MethodPut, "/v1/projects/network-proj/config/network", `{"config":{"http_allowlist":"10.0.0.0/8, 203.0.113.10","ssl_enforced":"true"}}`)
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

func TestPlatformDatabaseIngressAllowlistReconcilesProjectTCPRoutes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", root)
	t.Setenv("SUPADUPA_POSTGRES_ADDR", "0.0.0.0:5432")
	t.Setenv("SUPADUPA_POOLER_ADDR", "0.0.0.0:6543")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"db-ingress-proj","name":"DB Ingress","domain":"apps.supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	// Master on + this project set to allowlisted with its own CIDRs.
	enableDatabaseExposure(t, server, "db-ingress-proj", "allowlisted", "203.0.113.10/32\n198.51.100.0/24")

	routeFile, err := os.ReadFile(filepath.Join(root, "db-ingress-proj.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	routeBody := string(routeFile)
	for _, expected := range []string{
		"db-ingress-proj-db-tcp-ipallowlist",
		"db-ingress-proj-pooler-transaction-tcp-ipallowlist",
		"db-ingress-proj-pooler-session-tcp-ipallowlist",
		"- \"203.0.113.10/32\"",
		"- \"198.51.100.0/24\"",
	} {
		if !strings.Contains(routeBody, expected) {
			t.Fatalf("expected %q in reconciled route file:\n%s", expected, routeBody)
		}
	}

	manifestResponse := perform(server, http.MethodGet, "/v1/projects/db-ingress-proj/route-manifest", "")
	if manifestResponse.Code != http.StatusOK || !strings.Contains(manifestResponse.Body.String(), `"ip_allowlist":["203.0.113.10/32","198.51.100.0/24"]`) {
		t.Fatalf("expected route manifest to include the project allowlist, got %d: %s", manifestResponse.Code, manifestResponse.Body.String())
	}
	if !strings.Contains(manifestResponse.Body.String(), `"database_external_access_enabled":true`) || !strings.Contains(manifestResponse.Body.String(), `"database_ingress_published":true`) {
		t.Fatalf("expected manifest to report master+publish enabled: %s", manifestResponse.Body.String())
	}

	// Master kill switch off forces every project private regardless of mode.
	off := perform(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.supadupa.test","stack_version":"latest","profile":"full","resource_tier":"small","backup_schedule":"daily","feature_flags":{"database_external_access":false}}`)
	if off.Code != http.StatusOK {
		t.Fatalf("expected master-off update 200, got %d: %s", off.Code, off.Body.String())
	}
	gatedFile, err := os.ReadFile(filepath.Join(root, "db-ingress-proj.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gatedFile), "db-ingress-proj-db:") || strings.Contains(string(gatedFile), "HostSNI(`db-db-ingress-proj") {
		t.Fatalf("expected no database TCP routers when master is off:\n%s", string(gatedFile))
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
	for _, expected := range []string{`"name":"hello-api"`, `"version":1`, `"status":"deployed"`, `"source_hash":"`, `"source_bytes":36`, `"API_KEY":"super-************cret"`} {
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
	for _, expected := range []string{"SUPABASE_FUNCTION_VERSION=1", "VERIFY_JWT=true", "API_KEY=super-secret"} {
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
	if !strings.Contains(string(secretEnv), "SUPABASE_FUNCTION_VERSION=2") || strings.Contains(string(secretEnv), "API_KEY=super-secret") {
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

	updateResponse := perform(server, http.MethodPut, "/v1/projects/drain-proj/log-drains/"+drainID, `{"target":"loki","config":{"url":"https://loki.example.com/api/v1/push"}}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected log drain update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	if !strings.Contains(updateResponse.Body.String(), `"id":"`+drainID+`"`) || !strings.Contains(updateResponse.Body.String(), `"target":"loki"`) {
		t.Fatalf("expected in-place log drain update, got: %s", updateResponse.Body.String())
	}
	artifact, err = os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("expected updated log drain artifact: %v", err)
	}
	for _, expected := range []string{`type = "loki"`, `endpoint = "https://loki.example.com/api/v1/push"`} {
		if !strings.Contains(string(artifact), expected) {
			t.Fatalf("expected updated artifact to contain %q, got:\n%s", expected, artifact)
		}
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
	for _, action := range []string{"project.log_drain_create", "project.log_drain_update", "project.log_drain_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/drain-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Log drain created") || !strings.Contains(logsResponse.Body.String(), "Log drain updated") || !strings.Contains(logsResponse.Body.String(), "Log drain deleted") {
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
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"tiny","address":"localhost","capacity":{"cpu":1,"ram_mb":2048,"disk_gb":20,"projects":1}}`)
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

	// A host with ample CPU/RAM/slots but too little disk must still reject a
	// small-tier project (which reserves 20 GiB), proving placement enforces a
	// per-dimension capacity check beyond the project-slot count.
	diskHostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"disk-tight","address":"127.0.0.2","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":10,"projects":10}}`)
	if diskHostResponse.Code != http.StatusCreated {
		t.Fatalf("expected disk host status 201, got %d: %s", diskHostResponse.Code, diskHostResponse.Body.String())
	}
	diskHostID := extractString(t, diskHostResponse.Body.String(), "id")
	diskProject := `{"ref":"disk-one","name":"Disk One","host_id":"` + diskHostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	diskResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", diskProject)
	if diskResponse.Code != http.StatusConflict {
		t.Fatalf("expected disk capacity conflict, got %d: %s", diskResponse.Code, diskResponse.Body.String())
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

	updateQuotaResponse := perform(server, http.MethodPut, "/v1/orgs/"+orgID+"/quotas", `{"max_projects":1,"max_cpu":1,"max_ram_mb":2048,"max_disk_gb":20}`)
	if updateQuotaResponse.Code != http.StatusOK {
		t.Fatalf("expected quota update 200, got %d: %s", updateQuotaResponse.Code, updateQuotaResponse.Body.String())
	}

	firstProject := `{"ref":"quota-one","name":"Quota One","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	firstResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", firstProject)
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("expected first project status 201, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}

	quotaResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/quotas", "")
	for _, expected := range []string{`"max_projects":1`, `"cpu":1`, `"ram_mb":2048`, `"disk_gb":20`, `"projects":1`} {
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
	t.Setenv("SUPADUPA_POSTGRES_ADDR", "127.0.0.1:5432")
	t.Setenv("SUPADUPA_POOLER_ADDR", "127.0.0.1:6543")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	if _, err := store.RecordNodeTelemetry(context.Background(), hostID, control.NodeTelemetrySampleInput{
		Source:             "compose-local-node",
		CPUPercent:         12.5,
		CPUUsedCores:       1,
		CPUCapacityCores:   8,
		MemoryUsedBytes:    4294967296,
		MemoryTotalBytes:   34359738368,
		DiskUsedBytes:      85899345920,
		DiskTotalBytes:     536870912000,
		DiskAvailableBytes: 450971566080,
		NetworkSampled:     true,
		NetworkRxBytes:     1234,
		NetworkTxBytes:     5678,
		SampledAt:          time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record node telemetry: %v", err)
	}
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
	telemetryPayload := `{"source":"compose","cpu_percent":18.5,"memory_bytes":536870912,"memory_limit_bytes":2147483648,"disk_used_bytes":7516192768,"disk_limit_bytes":21474836480,"network_rx_bytes":123,"network_tx_bytes":456,"sampled_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	telemetryResponse := perform(server, http.MethodPost, "/v1/projects/metrics-proj/telemetry", telemetryPayload)
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
		`"host_capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}`,
		`"host_used":{"cpu":1,"ram_mb":2048,"disk_gb":20,"projects":1}`,
		`"database_ingress":{"mode":"private","public":false,"postgres_addr":"127.0.0.1:5432","pooler_addr":"127.0.0.1:6543","postgres_public":false,"pooler_public":false`,
		`"node_observed":[{"host_id":"` + hostID + `","source":"compose-local-node","cpu_percent":12.5`,
		`"network_sampled":true,"network_rx_bytes":1234,"network_tx_bytes":5678`,
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
		"supadupa_node_cpu_percent{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 12.5",
		"supadupa_node_memory_used_bytes{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 4294967296",
		"supadupa_node_disk_used_bytes{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 85899345920",
		"supadupa_node_network_sampled{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 1",
		"supadupa_node_network_rx_bytes{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 1234",
		"supadupa_node_network_tx_bytes{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 5678",
		"supadupa_observed_projects 1",
		"supadupa_observed_cpu_percent 18.5",
		"supadupa_observed_memory_bytes 536870912",
		"supadupa_function_deployments_total 1",
		"supadupa_backup_storage_bytes 2048",
		"supadupa_audit_verified 1",
		"supadupa_project_resource_cpu{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",status=\"healthy\"} 1",
		"supadupa_project_logs_total{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",status=\"healthy\"}",
		"supadupa_project_backups_total{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",status=\"healthy\"} 1",
		"supadupa_project_backup_storage_bytes{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",status=\"healthy\"} 2048",
		"supadupa_project_observed_cpu_percent{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",source=\"compose\",status=\"healthy\"} 18.5",
		"supadupa_project_observed_memory_bytes{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",source=\"compose\",status=\"healthy\"} 536870912",
		"supadupa_project_telemetry_sampled_at_unix{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",source=\"compose\",status=\"healthy\"}",
	} {
		if !strings.Contains(promResponse.Body.String(), expected) {
			t.Fatalf("expected prometheus metric %s: %s", expected, promResponse.Body.String())
		}
	}
	if strings.Count(promResponse.Body.String(), "# HELP supadupa_project_resource_cpu ") != 1 {
		t.Fatalf("expected one HELP line for project CPU metric: %s", promResponse.Body.String())
	}
}

func TestDatabaseIngressStatusFromEnv(t *testing.T) {
	private := databaseIngressStatusFromEnv(func(key string) string {
		return map[string]string{
			"SUPADUPA_POSTGRES_ADDR": "127.0.0.1:5432",
			"SUPADUPA_POOLER_ADDR":   "localhost:6543",
		}[key]
	})
	if private.Mode != "private" || private.Public || private.PostgresPublic || private.PoolerPublic || len(private.Warnings) != 0 {
		t.Fatalf("expected private database ingress, got %#v", private)
	}

	public := databaseIngressStatusFromEnv(func(key string) string {
		return map[string]string{
			"SUPADUPA_POSTGRES_ADDR":            "0.0.0.0:5432",
			"SUPADUPA_POOLER_ADDR":              "[::]:6543",
			"SUPADUPA_DB_INGRESS_ALLOWED_CIDRS": "203.0.113.0/24, 2001:db8::/32",
		}[key]
	})
	if public.Mode != "public" || !public.Public || !public.PostgresPublic || !public.PoolerPublic || !public.AllowlistConfigured || len(public.AllowedCIDRs) != 2 {
		t.Fatalf("expected public allowlisted database ingress, got %#v", public)
	}
	for _, warning := range public.Warnings {
		if strings.Contains(warning, "no database ingress CIDR allowlist") {
			t.Fatalf("allowlisted public ingress should not warn about missing allowlist: %#v", public)
		}
	}

	unrestricted := databaseIngressStatusFromEnv(func(key string) string {
		return map[string]string{
			"SUPADUPA_POSTGRES_ADDR": "198.51.100.10:5432",
			"SUPADUPA_POOLER_ADDR":   "127.0.0.1:6543",
		}[key]
	})
	if unrestricted.Mode != "public" || !unrestricted.PostgresPublic || unrestricted.PoolerPublic || len(unrestricted.Warnings) != 1 {
		t.Fatalf("expected public ingress with a single informational warning, got %#v", unrestricted)
	}
}

func TestFleetAdvisorFindingsEndpoint(t *testing.T) {
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "false")
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "false")
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
	// Mark the project production so posture findings keep their full severity;
	// development projects intentionally downgrade those to info.
	generalConfigResponse := perform(server, http.MethodPut, "/v1/projects/advisor-proj/config/general", `{"config":{"environment":"production"}}`)
	if generalConfigResponse.Code != http.StatusOK {
		t.Fatalf("expected general config update 200, got %d: %s", generalConfigResponse.Code, generalConfigResponse.Body.String())
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
		`"project_ref":"platform"`,
		`"severity":"critical"`,
		`"title":"Recovery-ready target guard is disabled"`,
		`"title":"Durable upgrade backup guard is disabled"`,
		`"title":"No recovery-ready backup target"`,
		`"title":"Project is not healthy"`,
		`"title":"Backups are disabled"`,
		`"title":"PITR is disabled"`,
		`"title":"Database ports are open to all IPs"`,
		`"title":"Database SSL is not enforced"`,
		`"title":"Public storage bucket"`,
		`"recommendation":"Inspect project logs and reconcile the project until it returns to healthy."`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("expected advisor value %s: %s", expected, response.Body.String())
		}
	}
}

func TestComplianceReportEndpoint(t *testing.T) {
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "false")
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "false")
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
	networkResponse := perform(server, http.MethodPut, "/v1/projects/compliance-proj/config/network", `{"config":{"db_allowlist":"10.0.0.0/8","ssl_enforced":"true"}}`)
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
		`"title":"Hosted-grade recovery guards"`,
		`"recovery-ready target guard enabled: false"`,
		`"durable upgrade backup guard enabled: false"`,
		`"id":"COM-010"`,
		`"title":"Off-host recovery target readiness"`,
		`"0 recovery-ready backup targets"`,
		`"id":"COM-011"`,
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

func TestProjectReplicasSyncRuntimeWhenProvisionerSupportsReplicaSyncer(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host create 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "read_replicas")
	projectBody := `{"ref":"replica-sync","name":"Replica Sync","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/replica-sync/replicas", `{"name":"east","host_id":"`+hostID+`","region":"us-east","tier":"small","read_weight":75,"failover_priority":2}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected replica create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	eastID := extractString(t, createResponse.Body.String(), "id")
	if provisioner.syncedReplicasRef != "replica-sync" || len(provisioner.syncedReplicas) != 1 || provisioner.syncedReplicas[0].Name != "east" {
		t.Fatalf("expected replica create to sync runtime replicas, got ref=%s replicas=%#v", provisioner.syncedReplicasRef, provisioner.syncedReplicas)
	}

	promoteResponse := perform(server, http.MethodPost, "/v1/projects/replica-sync/replicas/"+eastID+"/promote", `{"reason":"planned"}`)
	if promoteResponse.Code != http.StatusOK {
		t.Fatalf("expected replica promote 200, got %d: %s", promoteResponse.Code, promoteResponse.Body.String())
	}
	if len(provisioner.syncedReplicas) != 1 || provisioner.syncedReplicas[0].Role != "primary" {
		t.Fatalf("expected promote to sync primary role, got %#v", provisioner.syncedReplicas)
	}

	westResponse := perform(server, http.MethodPost, "/v1/projects/replica-sync/replicas", `{"name":"west","host_id":"`+hostID+`","region":"us-west","tier":"small","read_weight":50,"failover_priority":3}`)
	if westResponse.Code != http.StatusCreated {
		t.Fatalf("expected west replica create 201, got %d: %s", westResponse.Code, westResponse.Body.String())
	}
	westID := extractString(t, westResponse.Body.String(), "id")
	if len(provisioner.syncedReplicas) != 2 {
		t.Fatalf("expected west create to sync two replicas, got %#v", provisioner.syncedReplicas)
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/replica-sync/replicas/"+westID, "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected replica delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if provisioner.syncedReplicasRef != "replica-sync" || len(provisioner.syncedReplicas) != 1 || provisioner.syncedReplicas[0].ID != eastID {
		t.Fatalf("expected delete to sync remaining replicas before metadata removal, got ref=%s replicas=%#v", provisioner.syncedReplicasRef, provisioner.syncedReplicas)
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

func TestProjectStorageBucketsApplyToStorageDataPlane(t *testing.T) {
	t.Setenv("SUPADUPA_STORAGE_APPLY", "true")
	var requestsMu sync.Mutex
	requests := []string{}
	storageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		requestsMu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("apikey") == "" {
			http.Error(w, "missing service role", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/storage/v1/bucket":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload["id"] != "assets" || payload["name"] != "assets" || payload["public"] != true {
				http.Error(w, fmt.Sprintf("unexpected payload %#v", payload), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"assets"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/storage/v1/bucket/assets":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer storageServer.Close()
	t.Setenv("SUPADUPA_STORAGE_APPLY_BASE_URL", storageServer.URL)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"storage-live","name":"Storage Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/storage-live/storage/buckets", `{"name":"assets","public":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage bucket create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/storage-live/storage/buckets/assets", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected storage bucket delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(requests) != 2 || !strings.HasPrefix(requests[0], "POST /storage/v1/bucket Bearer ") || !strings.HasPrefix(requests[1], "DELETE /storage/v1/bucket/assets Bearer ") {
		t.Fatalf("unexpected storage data-plane requests: %#v", requests)
	}
}

func TestProjectStorageBucketCreateRollsBackMetadataWhenDataPlaneFails(t *testing.T) {
	t.Setenv("SUPADUPA_STORAGE_APPLY", "true")
	storageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "storage unavailable", http.StatusBadGateway)
	}))
	defer storageServer.Close()
	t.Setenv("SUPADUPA_STORAGE_APPLY_BASE_URL", storageServer.URL)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"storage-fail","name":"Storage Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/storage-fail/storage/buckets", `{"name":"assets","public":true}`)
	if createResponse.Code != http.StatusConflict {
		t.Fatalf("expected storage bucket create conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/storage-fail/storage/buckets", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"assets"`) {
		t.Fatalf("expected failed bucket create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectStorageBucketDeleteCleansMetadataWhenDataPlaneBucketAlreadyMissing(t *testing.T) {
	t.Setenv("SUPADUPA_STORAGE_APPLY", "true")
	storageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/storage/v1/bucket":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"assets"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/storage/v1/bucket/assets":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"statusCode":"404","error":"Bucket not found","message":"Bucket not found"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer storageServer.Close()
	t.Setenv("SUPADUPA_STORAGE_APPLY_BASE_URL", storageServer.URL)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"storage-missing","name":"Storage Missing","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/storage-missing/storage/buckets", `{"name":"assets","public":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage bucket create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/storage-missing/storage/buckets/assets", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected storage bucket delete 204 despite missing data-plane bucket, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/storage-missing/storage/buckets", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"assets"`) {
		t.Fatalf("expected metadata cleanup after missing data-plane bucket, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestStorageDataPlaneNotFoundAcceptsSupabaseStorageShape(t *testing.T) {
	if !storageDataPlaneNotFound(http.StatusNotFound, []byte(`not found`)) {
		t.Fatalf("expected HTTP 404 to be treated as not found")
	}
	if !storageDataPlaneNotFound(http.StatusBadRequest, []byte(`{"statusCode":"404","error":"Bucket not found","message":"Bucket not found"}`)) {
		t.Fatalf("expected Supabase Storage bucket-not-found body to be treated as not found")
	}
	if storageDataPlaneNotFound(http.StatusBadRequest, []byte(`{"statusCode":"400","message":"bad request"}`)) {
		t.Fatalf("expected generic bad request to remain an error")
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

func TestProjectDatabaseExtensionUpdateAppliesLiveDDL(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "extension-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	argsPath := filepath.Join(root, "compose.args")
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuoteForTest(argsPath) + "\ncat > " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"extension-live","name":"Extension Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	updateResponse := perform(server, http.MethodPut, "/v1/projects/extension-live/database/extensions/uuid-ossp", `{"enabled":true,"schema":"extensions"}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected extension update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read fake compose args: %v", err)
	}
	for _, expected := range []string{"-p extension-live", "-f " + filepath.Join(projectDir, "compose.yaml"), "exec -T db sh -c", `PGPASSWORD="$POSTGRES_PASSWORD" exec psql`} {
		if !strings.Contains(string(args), expected) {
			t.Fatalf("expected fake compose args to contain %q, got %s", expected, string(args))
		}
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	for _, expected := range []string{`CREATE SCHEMA IF NOT EXISTS "extensions";`, `CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA "extensions";`} {
		if !strings.Contains(string(stdin), expected) {
			t.Fatalf("expected extension DDL %q, got:\n%s", expected, stdin)
		}
	}
}

func TestProjectDatabaseExtensionUpdateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "extension-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"extension-fail","name":"Extension Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	disableResponse := perform(server, http.MethodPut, "/v1/projects/extension-fail/database/extensions/pg_cron", `{"enabled":false,"schema":"extensions"}`)
	if disableResponse.Code != http.StatusOK {
		t.Fatalf("expected initial extension update 200, got %d: %s", disableResponse.Code, disableResponse.Body.String())
	}

	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\necho 'extension install failed' >&2\nexit 1\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	enableResponse := perform(server, http.MethodPut, "/v1/projects/extension-fail/database/extensions/pg_cron", `{"enabled":true,"schema":"extensions"}`)
	if enableResponse.Code != http.StatusConflict || !strings.Contains(enableResponse.Body.String(), "extension install failed") {
		t.Fatalf("expected extension apply conflict, got %d: %s", enableResponse.Code, enableResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/extension-fail/database/extensions", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"pg_cron"`) || !strings.Contains(listResponse.Body.String(), `"enabled":false`) {
		t.Fatalf("expected failed extension update to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
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

func TestProjectDatabaseCronJobCreateDeleteAppliesLiveDDL(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "cron-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\ncat >> " + shellQuoteForTest(stdinPath) + "\nprintf '\\n-- call --\\n' >> " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"cron-live","name":"Cron Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/cron-live/database/cron-jobs", `{"name":"refresh-rollups","schedule":"*/15 * * * *","command":"select public.refresh_rollups();","database":"postgres","username":"postgres","active":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected cron create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/cron-live/database/cron-jobs/refresh-rollups", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected cron delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	for _, expected := range []string{
		`CREATE EXTENSION IF NOT EXISTS pg_cron WITH SCHEMA extensions;`,
		`SELECT cron.schedule_in_database('refresh-rollups', '*/15 * * * *', 'select public.refresh_rollups();', 'postgres', 'postgres', true);`,
		`SELECT cron.unschedule(jobid) FROM cron.job WHERE jobname = 'refresh-rollups';`,
	} {
		if !strings.Contains(string(stdin), expected) {
			t.Fatalf("expected cron DDL %q, got:\n%s", expected, stdin)
		}
	}
}

func TestProjectDatabaseCronJobCreateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "cron-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\necho 'cron schedule failed' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"cron-fail","name":"Cron Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/cron-fail/database/cron-jobs", `{"name":"refresh-rollups","schedule":"*/15 * * * *","command":"select 1;","active":true}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "cron schedule failed") {
		t.Fatalf("expected cron apply conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/cron-fail/database/cron-jobs", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"refresh-rollups"`) {
		t.Fatalf("expected failed cron create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectConfigRuntimeSecretResolutionAndRollback(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"runtime-secret-proj","name":"Runtime Secret","domain":"supadupa.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	missingResponse := perform(server, http.MethodPut, "/v1/projects/runtime-secret-proj/config/smtp", `{"config":{"enabled":"true","host":"smtp.example.com","password_handle":"secret://projects/runtime-secret-proj/smtp-password"}}`)
	if missingResponse.Code != http.StatusConflict || !strings.Contains(missingResponse.Body.String(), "secret smtp-password") {
		t.Fatalf("expected missing secret conflict, got %d: %s", missingResponse.Code, missingResponse.Body.String())
	}
	getResponse := perform(server, http.MethodGet, "/v1/projects/runtime-secret-proj/config/smtp", "")
	if strings.Contains(getResponse.Body.String(), "secret://projects/runtime-secret-proj/smtp-password") {
		t.Fatalf("expected failed config sync to roll back metadata, got %s", getResponse.Body.String())
	}

	upsertResponse := perform(server, http.MethodPut, "/v1/projects/runtime-secret-proj/secrets/smtp-password", `{"value":"smtp-secret-value"}`)
	if upsertResponse.Code != http.StatusOK || strings.Contains(upsertResponse.Body.String(), "smtp-secret-value") || !strings.Contains(upsertResponse.Body.String(), `"kind":"smtp-password"`) {
		t.Fatalf("expected masked custom secret upsert, got %d: %s", upsertResponse.Code, upsertResponse.Body.String())
	}
	updateResponse := perform(server, http.MethodPut, "/v1/projects/runtime-secret-proj/config/smtp", `{"config":{"enabled":"true","host":"smtp.example.com","password_handle":"secret://projects/runtime-secret-proj/smtp-password"}}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected smtp config update after secret upsert, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	if provisioner.syncedConfig.Config["__resolved_password_handle"] != "smtp-secret-value" {
		t.Fatalf("expected runtime config to include resolved secret value, got %#v", provisioner.syncedConfig.Config)
	}
	if strings.Contains(updateResponse.Body.String(), "smtp-secret-value") || !strings.Contains(updateResponse.Body.String(), `"password_handle":"secret://projects/runtime-secret-proj/smtp-password"`) {
		t.Fatalf("expected response to keep handle and hide secret value: %s", updateResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/runtime-secret-proj/secrets/smtp-password", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected custom secret delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	revealDeletedResponse := perform(server, http.MethodGet, "/v1/projects/runtime-secret-proj/secrets/smtp-password/reveal", "")
	if revealDeletedResponse.Code != http.StatusNotFound {
		t.Fatalf("expected deleted custom secret reveal 404, got %d: %s", revealDeletedResponse.Code, revealDeletedResponse.Body.String())
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
	seedProjectSecrets(t, store, "authhook-proj", "auth-hook-secret", "auth-hook-auth")

	invalidSecret := perform(server, http.MethodPost, "/v1/projects/authhook-proj/auth/hooks", `{"hook_type":"custom_access_token","enabled":true,"target_uri":"https://hooks.example.com/token","secret_handle":"raw-secret"}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret handle 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}
	invalidHeader := perform(server, http.MethodPost, "/v1/projects/authhook-proj/auth/hooks", `{"hook_type":"custom_access_token","enabled":true,"target_uri":"https://hooks.example.com/token","headers":{"authorization":"Bearer raw"}}`)
	if invalidHeader.Code != http.StatusBadRequest {
		t.Fatalf("expected raw authorization header 400, got %d: %s", invalidHeader.Code, invalidHeader.Body.String())
	}

	createBody := `{"hook_type":"custom_access_token","enabled":true,"target_uri":"https://hooks.example.com/token","secret_handle":"secret://projects/authhook-proj/auth-hook-secret","headers":{"authorization":"secret://projects/authhook-proj/auth-hook-auth","x-trace":"supadupa"},"timeout_ms":7000,"retry_attempts":2}`
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
	if provisioner.syncedAuthHooks[0].SecretHandle != "secret://projects/authhook-proj/auth-hook-secret" || provisioner.syncedAuthHooks[0].RuntimeSecret != "auth-hook-secret-value" || provisioner.syncedAuthHooks[0].Headers["authorization"] != "secret://projects/authhook-proj/auth-hook-auth" || provisioner.syncedAuthHooks[0].RuntimeHeaders["authorization"] != "auth-hook-auth-value" {
		t.Fatalf("expected auth hook runtime sync to keep handles and include resolved runtime secrets, got %#v", provisioner.syncedAuthHooks[0])
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

func TestProjectAuthHookCreateRollsBackWhenSecretResolutionFails(t *testing.T) {
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
	projectBody := `{"ref":"authhook-fail","name":"Auth Hooks","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createBody := `{"hook_type":"custom_access_token","enabled":true,"target_uri":"https://hooks.example.com/token","secret_handle":"secret://projects/authhook-fail/missing-hook-secret","timeout_ms":7000,"retry_attempts":2}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/authhook-fail/auth/hooks", createBody)
	if createResponse.Code != http.StatusConflict {
		t.Fatalf("expected auth hook unresolved secret 409, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	if !strings.Contains(createResponse.Body.String(), "auth hook secret_handle") || !strings.Contains(createResponse.Body.String(), "missing-hook-secret") {
		t.Fatalf("expected unresolved auth hook secret error, got %s", createResponse.Body.String())
	}
	if provisioner.syncedAuthHooksRef != "" || len(provisioner.syncedAuthHooks) != 0 {
		t.Fatalf("expected unresolved auth hook to skip runtime sync, got ref=%s hooks=%#v", provisioner.syncedAuthHooksRef, provisioner.syncedAuthHooks)
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/authhook-fail/auth/hooks", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"hook_type":"custom_access_token"`) {
		t.Fatalf("expected unresolved auth hook to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/authhook-fail/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Auth hooks sync failed") {
		t.Fatalf("expected auth hook sync failure log, got %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.auth_hooks_sync_failed"`) {
		t.Fatalf("expected auth hook sync failure audit, got %s", auditResponse.Body.String())
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

func TestProjectDatabaseQueueCreateDeleteAppliesLiveDDL(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "queue-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\ncat >> " + shellQuoteForTest(stdinPath) + "\nprintf '\\n-- call --\\n' >> " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"queue-live","name":"Queue Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/queue-live/database/queues", `{"name":"events","schema":"pgmq","dead_letter_queue":"events-dlq","active":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected queue create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/queue-live/database/queues/events", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected queue delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	for _, expected := range []string{
		`CREATE SCHEMA IF NOT EXISTS "pgmq";`,
		`CREATE EXTENSION IF NOT EXISTS pgmq WITH SCHEMA "pgmq";`,
		`SELECT pgmq.create('events-dlq');`,
		`SELECT pgmq.create('events');`,
		`SELECT pgmq.drop_queue('events');`,
		`SELECT pgmq.drop_queue('events-dlq');`,
	} {
		if !strings.Contains(string(stdin), expected) {
			t.Fatalf("expected queue DDL %q, got:\n%s", expected, stdin)
		}
	}
}

func TestProjectDatabaseQueueCreateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "queue-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\necho 'queue create failed' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"queue-fail","name":"Queue Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/queue-fail/database/queues", `{"name":"events","schema":"pgmq","active":true}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "queue create failed") {
		t.Fatalf("expected queue apply conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/queue-fail/database/queues", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"events"`) {
		t.Fatalf("expected failed queue create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
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

func TestProjectDatabaseWebhookCreateDeleteAppliesLiveDDL(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "webhook-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\ncat >> " + shellQuoteForTest(stdinPath) + "\nprintf '\\n-- call --\\n' >> " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"webhook-live","name":"Webhook Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	seedProjectSecrets(t, store, "webhook-live", "orders-token")

	createResponse := perform(server, http.MethodPost, "/v1/projects/webhook-live/database/webhooks", `{"name":"orders-events","schema":"public","table":"orders","events":["insert","update"],"endpoint":"https://hooks.example.com/orders","http_method":"POST","headers":{"Authorization":"secret://projects/webhook-live/orders-token","X-Source":"supadupa"},"timeout_seconds":15,"active":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected webhook create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/webhook-live/database/webhooks/orders-events", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected webhook delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	sql := string(stdin)
	for _, expected := range []string{
		`CREATE SCHEMA IF NOT EXISTS supadupa;`,
		`CREATE EXTENSION IF NOT EXISTS pg_net;`,
		`CREATE OR REPLACE FUNCTION supadupa."webhook_orders_events"()`,
		`"authorization":"orders-token-value"`,
		`"x-source":"supadupa"`,
		`PERFORM net.http_post(`,
		`url := 'https://hooks.example.com/orders'`,
		`timeout_milliseconds := 15000`,
		`CREATE TRIGGER "supadupa_webhook_orders_events_insert"`,
		`AFTER INSERT ON "public"."orders"`,
		`CREATE TRIGGER "supadupa_webhook_orders_events_update"`,
		`AFTER UPDATE ON "public"."orders"`,
		`DROP TRIGGER IF EXISTS "supadupa_webhook_orders_events_insert" ON "public"."orders"`,
		`DROP FUNCTION IF EXISTS supadupa."webhook_orders_events"();`,
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("expected webhook DDL %q, got:\n%s", expected, sql)
		}
	}
	if strings.Contains(sql, "secret://projects") {
		t.Fatalf("expected live webhook DDL to contain resolved secrets, got:\n%s", sql)
	}
}

func TestProjectDatabaseWebhookCreateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "webhook-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\necho 'webhook create failed' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"webhook-fail","name":"Webhook Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/webhook-fail/database/webhooks", `{"name":"orders-events","schema":"public","table":"orders","endpoint":"https://hooks.example.com/orders","active":true}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "webhook create failed") {
		t.Fatalf("expected webhook apply conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/webhook-fail/database/webhooks", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"orders-events"`) {
		t.Fatalf("expected failed webhook create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectDatabaseWebhookCreateRequiresRevealableSecretHeadersWhenApplyEnabled(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "webhook-secret")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"webhook-secret","name":"Webhook Secret","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/webhook-secret/database/webhooks", `{"name":"orders-events","schema":"public","table":"orders","endpoint":"https://hooks.example.com/orders","headers":{"Authorization":"secret://projects/webhook-secret/webhooks/orders-token"},"active":true}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "not revealable") {
		t.Fatalf("expected unrevealable header conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/webhook-secret/database/webhooks", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"orders-events"`) {
		t.Fatalf("expected failed webhook create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
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

func TestProjectDatabaseSchemaCreateAppliesActiveSQLToProjectDatabase(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "schema-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	argsPath := filepath.Join(root, "compose.args")
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuoteForTest(argsPath) + "\ncat > " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"schema-live","name":"Schema Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	sql := "create table public.live_schema_probe(id uuid primary key);"
	createResponse := perform(server, http.MethodPost, "/v1/projects/schema-live/database/schemas", `{"name":"live-schema","version":"20260606_001","schema":"public","sql":"`+sql+`","active":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected schema create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read fake compose args: %v", err)
	}
	for _, expected := range []string{"-p schema-live", "-f " + filepath.Join(projectDir, "compose.yaml"), "exec -T db sh -c", `PGPASSWORD="$POSTGRES_PASSWORD" exec psql`, "-v ON_ERROR_STOP=1", "-U supabase_admin", "-d postgres"} {
		if !strings.Contains(string(args), expected) {
			t.Fatalf("expected fake compose args to contain %q, got %s", expected, string(args))
		}
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	if strings.TrimSpace(string(stdin)) != sql {
		t.Fatalf("expected SQL stdin %q, got %q", sql, strings.TrimSpace(string(stdin)))
	}
}

func TestProjectDatabaseSchemaCreateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "schema-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\necho 'syntax error near bad_sql' >&2\nexit 1\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"schema-fail","name":"Schema Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/schema-fail/database/schemas", `{"name":"bad-schema","version":"20260606_001","schema":"public","sql":"select bad_sql();","active":true}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "syntax error near bad_sql") {
		t.Fatalf("expected schema apply conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/schema-fail/database/schemas", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"bad-schema"`) {
		t.Fatalf("expected failed schema create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
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

func TestProjectDatabaseRoleCreateDeleteAppliesLiveDDL(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "role-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\ncat >> " + shellQuoteForTest(stdinPath) + "\nprintf '\\n-- call --\\n' >> " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"role-live","name":"Role Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/role-live/database/roles", `{"name":"app_reader","login":false,"bypass_rls":false,"connection_limit":25,"member_of":["authenticated"],"schema_grants":{"public":"usage,select"}}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected role create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/role-live/database/roles/app_reader", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected role delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	for _, expected := range []string{
		`CREATE ROLE "app_reader";`,
		`ALTER ROLE "app_reader" NOLOGIN INHERIT NOBYPASSRLS CONNECTION LIMIT 25;`,
		`GRANT "authenticated" TO "app_reader";`,
		`GRANT USAGE ON SCHEMA "public" TO "app_reader";`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA "public" TO "app_reader";`,
		`REVOKE SELECT ON ALL TABLES IN SCHEMA "public" FROM "app_reader";`,
		`REVOKE USAGE ON SCHEMA "public" FROM "app_reader";`,
		`REVOKE "authenticated" FROM "app_reader";`,
		`DROP ROLE IF EXISTS "app_reader";`,
	} {
		if !strings.Contains(string(stdin), expected) {
			t.Fatalf("expected role DDL %q, got:\n%s", expected, stdin)
		}
	}
}

func TestProjectDatabaseRoleCreateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "role-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\necho 'role create failed' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"role-fail","name":"Role Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/role-fail/database/roles", `{"name":"app_reader","login":false,"schema_grants":{"public":"usage"}}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "role create failed") {
		t.Fatalf("expected role apply conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/role-fail/database/roles", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"app_reader"`) {
		t.Fatalf("expected failed role create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectDatabaseRoleLoginCreateRequiresRevealableProjectSecret(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "role-secret")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"role-secret","name":"Role Secret","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	unrevealableResponse := perform(server, http.MethodPost, "/v1/projects/role-secret/database/roles", `{"name":"app_login","login":true,"password_secret_handle":"secret://projects/role-secret/db/app-login"}`)
	if unrevealableResponse.Code != http.StatusConflict || !strings.Contains(unrevealableResponse.Body.String(), "not revealable") {
		t.Fatalf("expected unrevealable secret conflict, got %d: %s", unrevealableResponse.Code, unrevealableResponse.Body.String())
	}
	crossProjectResponse := perform(server, http.MethodPost, "/v1/projects/role-secret/database/roles", `{"name":"other_login","login":true,"password_secret_handle":"secret://projects/other/db_password"}`)
	if crossProjectResponse.Code != http.StatusConflict || !strings.Contains(crossProjectResponse.Body.String(), "must reference project role-secret") {
		t.Fatalf("expected cross-project secret conflict, got %d: %s", crossProjectResponse.Code, crossProjectResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/role-secret/database/roles", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"login":true`) {
		t.Fatalf("expected failed login role creates to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
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
		`"excluded_paths":[]`,
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
	networkConfigResponse := perform(server, http.MethodPut, "/v1/projects/net-proj/config/network", `{"config":{"db_allowlist":"10.0.0.0/8,203.0.113.10/32","ssl_enforced":"true"}}`)
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
		`"db_allowlist":"10.0.0.0/8,203.0.113.10/32"`,
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
	hostedBackupsResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/database/backups", "")
	if hostedBackupsResponse.Code != http.StatusOK || !strings.Contains(hostedBackupsResponse.Body.String(), `"kind":"logical"`) {
		t.Fatalf("expected hosted-shaped logical backup list response: %d %s", hostedBackupsResponse.Code, hostedBackupsResponse.Body.String())
	}
	backupID := extractString(t, backupResponse.Body.String(), "id")

	recoverabilityResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/recoverability", "")
	if recoverabilityResponse.Code != http.StatusOK {
		t.Fatalf("expected recoverability status 200, got %d: %s", recoverabilityResponse.Code, recoverabilityResponse.Body.String())
	}
	for _, expected := range []string{
		`"status":"local-backup-only"`,
		`"off_host_backup_configured":false`,
		`"off_host_backup_verified":false`,
		`"restore_to_time_available":false`,
		`"restore_to_time_unavailable":"physical base backup plus WAL replay is not configured"`,
	} {
		if !strings.Contains(recoverabilityResponse.Body.String(), expected) {
			t.Fatalf("expected recoverability response to include %s: %s", expected, recoverabilityResponse.Body.String())
		}
	}

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
	invalidPITRRestoreResponse := perform(server, http.MethodPost, "/v1/projects/backup-proj/database/backups/restore-pitr", `{"recovery_time_target_unix":"not-a-timestamp"}`)
	if invalidPITRRestoreResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid PITR restore timestamp 400, got %d: %s", invalidPITRRestoreResponse.Code, invalidPITRRestoreResponse.Body.String())
	}
	unavailablePITRRestoreResponse := perform(server, http.MethodPost, "/v1/projects/backup-proj/database/backups/restore-pitr", `{"recovery_time_target_unix":"1735689600"}`)
	if unavailablePITRRestoreResponse.Code != http.StatusConflict || !strings.Contains(unavailablePITRRestoreResponse.Body.String(), `"restore_to_time_available":false`) || !strings.Contains(unavailablePITRRestoreResponse.Body.String(), `"status":"local-backup-only"`) {
		t.Fatalf("expected unavailable PITR restore conflict with recoverability: %d %s", unavailablePITRRestoreResponse.Code, unavailablePITRRestoreResponse.Body.String())
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

func TestProjectPhysicalBackupPolicyAndTrigger(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", root)
	t.Setenv("SUPADUPA_PHYSICAL_BACKUP_COMMAND", "printf 'physical backup for %s\\n' {{ref}}")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"physical-api","name":"Physical","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	policyResponse := perform(server, http.MethodPut, "/v1/projects/physical-api/backups/policy", `{"enabled":true,"schedule":"daily","kind":"physical"}`)
	if policyResponse.Code != http.StatusOK || !strings.Contains(policyResponse.Body.String(), `"kind":"physical"`) {
		t.Fatalf("expected physical backup policy: %d %s", policyResponse.Code, policyResponse.Body.String())
	}
	backupResponse := perform(server, http.MethodPost, "/v1/projects/physical-api/backups", "")
	if backupResponse.Code != http.StatusCreated || !strings.Contains(backupResponse.Body.String(), `"kind":"physical"`) || !strings.Contains(backupResponse.Body.String(), `physical.base`) {
		t.Fatalf("expected physical backup response: %d %s", backupResponse.Code, backupResponse.Body.String())
	}
	backupPath := extractString(t, backupResponse.Body.String(), "location")
	payload, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "physical backup for physical-api") {
		t.Fatalf("expected physical backup artifact body, got:\n%s", string(payload))
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/physical-api/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Physical backup completed") {
		t.Fatalf("expected physical backup log: %s", logsResponse.Body.String())
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

	target, err := store.CreateBackupStorageTarget(context.Background(), control.BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupPolicy(context.Background(), "pitr-proj", control.BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	derivedPolicyResponse := perform(server, http.MethodPut, "/v1/projects/pitr-proj/pitr/policy", `{"enabled":true,"archive_bucket":"","retention_days":7}`)
	if derivedPolicyResponse.Code != http.StatusOK || !strings.Contains(derivedPolicyResponse.Body.String(), `"archive_bucket":"s3://backups/supadupa/projects/pitr-proj/wal"`) {
		t.Fatalf("expected derived PITR archive bucket from backup target: %d %s", derivedPolicyResponse.Code, derivedPolicyResponse.Body.String())
	}
	if err := store.DeleteBackupStorageTarget(context.Background(), target.ID); err != nil {
		t.Fatal(err)
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

	recoverabilityResponse := perform(server, http.MethodGet, "/v1/projects/pitr-proj/recoverability", "")
	if recoverabilityResponse.Code != http.StatusOK {
		t.Fatalf("expected recoverability status 200, got %d: %s", recoverabilityResponse.Code, recoverabilityResponse.Body.String())
	}
	for _, expected := range []string{
		`"pitr_enabled":true`,
		`"latest_wal_archive"`,
		`"wal_archive_off_host_verified":false`,
		`"restore_to_time_configured":false`,
		`"restore_to_time_available":false`,
		`"verified WAL archives exist only on local disk"`,
		`"no verified physical base backup is available for PITR restore"`,
	} {
		if !strings.Contains(recoverabilityResponse.Body.String(), expected) {
			t.Fatalf("expected recoverability response to include %s: %s", expected, recoverabilityResponse.Body.String())
		}
	}
	unavailablePITRRestoreResponse := perform(server, http.MethodPost, "/v1/projects/pitr-proj/database/backups/restore-pitr", `{"recovery_time_target_unix":"1735689600"}`)
	if unavailablePITRRestoreResponse.Code != http.StatusConflict || !strings.Contains(unavailablePITRRestoreResponse.Body.String(), `"recoverability"`) || !strings.Contains(unavailablePITRRestoreResponse.Body.String(), `"pitr_enabled":true`) {
		t.Fatalf("expected PITR restore conflict with recoverability: %d %s", unavailablePITRRestoreResponse.Code, unavailablePITRRestoreResponse.Body.String())
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

func TestProjectPITRRestoreAPI(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", root)
	t.Setenv("SUPADUPA_PITR_RESTORE_COMMAND", "printf 'pitr restore %s %s %s\\n' {{recovery_time_target_unix}} {{backup_path}} {{wal_segment}}")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"pitr-restore","name":"PITR Restore","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	target, err := store.CreateBackupStorageTarget(context.Background(), control.BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupStorageTargetTestResult(context.Background(), target.ID, time.Now().UTC(), "passed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupPolicy(context.Background(), "pitr-restore", control.BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(root, "base.tar")
	basePayload := []byte("physical base backup")
	if err := os.WriteFile(basePath, basePayload, 0o600); err != nil {
		t.Fatal(err)
	}
	baseHash := sha256.Sum256(basePayload)
	now := time.Now().UTC()
	if _, err := store.CreateBackup(context.Background(), control.BackupInput{
		ProjectRef:      "pitr-restore",
		Kind:            "physical",
		Location:        basePath,
		RemoteLocation:  "s3://backups/supadupa/projects/pitr-restore/backups/base.tar",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len(basePayload)),
		ChecksumSHA256:  hex.EncodeToString(baseHash[:]),
		Status:          "completed",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePITRPolicy(context.Background(), "pitr-restore", control.PITRPolicyInput{Enabled: true, ArchiveBucket: "s3://archive/pitr-restore", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	walPath := filepath.Join(root, "wal")
	walPayload := []byte("wal archive")
	if err := os.WriteFile(walPath, walPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	walHash := sha256.Sum256(walPayload)
	archive, err := store.CreateWALArchive(context.Background(), control.WALArchiveInput{
		ProjectRef:      "pitr-restore",
		Segment:         "000000010000000000000001",
		SegmentSource:   "postgres",
		Location:        walPath,
		RemoteLocation:  "s3://backups/supadupa/projects/pitr-restore/wal/000000010000000000000001.wal",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len(walPayload)),
		ChecksumSHA256:  hex.EncodeToString(walHash[:]),
		Status:          "archived",
		VerifiedAt:      &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	restoreResponse := perform(server, http.MethodPost, "/v1/projects/pitr-restore/database/backups/restore-pitr", `{"recovery_time_target_unix":"`+fmt.Sprintf("%d", archive.CreatedAt.Unix())+`"}`)
	if restoreResponse.Code != http.StatusCreated || !strings.Contains(restoreResponse.Body.String(), `"restore_state":"completed"`) || !strings.Contains(restoreResponse.Body.String(), `"recovery_time_target_unix":`+fmt.Sprintf("%d", archive.CreatedAt.Unix())) {
		t.Fatalf("expected created PITR restore response: %d %s", restoreResponse.Code, restoreResponse.Body.String())
	}
	restorePath := extractString(t, restoreResponse.Body.String(), "restore_path")
	transcript, err := os.ReadFile(restorePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "pitr restore "+fmt.Sprintf("%d", archive.CreatedAt.Unix())) || !strings.Contains(string(transcript), basePath) || !strings.Contains(string(transcript), archive.Segment) {
		t.Fatalf("expected PITR restore transcript, got:\n%s", string(transcript))
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.restore_pitr"`) {
		t.Fatalf("expected PITR restore audit event: %s", auditResponse.Body.String())
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
	serviceValue := extractString(t, revealResponse.Body.String(), "value")
	if strings.Count(serviceValue, ".") != 2 {
		t.Fatalf("expected revealed service key to be a JWT, got: %s", revealResponse.Body.String())
	}
	firstValue := serviceValue

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

func TestProjectSecretRevealIsThrottled(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"secret-throttle","name":"Secret Throttle","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	for i := 0; i < maxSecretAccessAttempts; i++ {
		response := perform(server, http.MethodGet, "/v1/projects/secret-throttle/secrets/service_role/reveal", "")
		if response.Code != http.StatusOK {
			t.Fatalf("expected reveal %d status 200, got %d: %s", i+1, response.Code, response.Body.String())
		}
	}
	throttled := perform(server, http.MethodGet, "/v1/projects/secret-throttle/secrets/service_role/reveal", "")
	if throttled.Code != http.StatusTooManyRequests || !strings.Contains(throttled.Body.String(), "too many secret access attempts") {
		t.Fatalf("expected throttled secret reveal, got %d: %s", throttled.Code, throttled.Body.String())
	}
	if throttled.Header().Get("Retry-After") == "" {
		t.Fatal("expected throttled secret reveal to include Retry-After")
	}
}

func TestProjectSecretRevealThrottleFollowsAccountAcrossIPs(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})
	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"owner@example.com","password":"super-secure"}`)
	ownerToken := extractString(t, bootstrap.Body.String(), "token")
	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, ownerToken)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"secret-account-throttle","name":"Secret Account Throttle","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody, ownerToken)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	for i := 0; i < maxSecretAccessAttempts; i++ {
		remoteAddr := fmt.Sprintf("203.0.113.%d:12345", i+1)
		response := performWithTokenAndRemoteAddr(server, http.MethodGet, "/v1/projects/secret-account-throttle/secrets/service_role/reveal", "", ownerToken, remoteAddr)
		if response.Code != http.StatusOK {
			t.Fatalf("expected reveal %d status 200, got %d: %s", i+1, response.Code, response.Body.String())
		}
	}
	throttled := performWithTokenAndRemoteAddr(server, http.MethodGet, "/v1/projects/secret-account-throttle/secrets/service_role/reveal", "", ownerToken, "203.0.113.250:12345")
	if throttled.Code != http.StatusTooManyRequests || !strings.Contains(throttled.Body.String(), "too many secret access attempts") {
		t.Fatalf("expected same account to stay throttled after changing IP, got %d: %s", throttled.Code, throttled.Body.String())
	}
	if throttled.Header().Get("Retry-After") == "" {
		t.Fatal("expected throttled secret reveal to include Retry-After")
	}
}

func TestProjectSecretCopyThrottleFollowsIPAcrossAccounts(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})
	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"owner@example.com","password":"super-secure"}`)
	ownerToken := extractString(t, bootstrap.Body.String(), "token")
	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, ownerToken)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"secret-ip-throttle","name":"Secret IP Throttle","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody, ownerToken)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	createUserResponse := performWithToken(server, http.MethodPost, "/v1/users", `{"email":"admin@example.com","password":"super-secure","role":"admin"}`, ownerToken)
	if createUserResponse.Code != http.StatusCreated {
		t.Fatalf("expected admin user status 201, got %d: %s", createUserResponse.Code, createUserResponse.Body.String())
	}
	memberResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/members", `{"email":"admin@example.com","role":"admin"}`, ownerToken)
	if memberResponse.Code != http.StatusOK {
		t.Fatalf("expected admin membership status 200, got %d: %s", memberResponse.Code, memberResponse.Body.String())
	}
	adminLogin := perform(server, http.MethodPost, "/v1/auth/login", `{"email":"admin@example.com","password":"super-secure"}`)
	adminToken := extractString(t, adminLogin.Body.String(), "token")

	tokens := []string{ownerToken, adminToken}
	for i := 0; i < maxSecretAccessAttempts; i++ {
		response := performWithTokenAndRemoteAddr(server, http.MethodPost, "/v1/projects/secret-ip-throttle/secrets/service_role/copy", "", tokens[i%len(tokens)], "198.51.100.80:12345")
		if response.Code != http.StatusNoContent {
			t.Fatalf("expected copy %d status 204, got %d: %s", i+1, response.Code, response.Body.String())
		}
	}
	throttled := performWithTokenAndRemoteAddr(server, http.MethodPost, "/v1/projects/secret-ip-throttle/secrets/service_role/copy", "", ownerToken, "198.51.100.80:54321")
	if throttled.Code != http.StatusTooManyRequests || !strings.Contains(throttled.Body.String(), "too many secret access attempts") {
		t.Fatalf("expected same IP to stay throttled across accounts, got %d: %s", throttled.Code, throttled.Body.String())
	}
	if throttled.Header().Get("Retry-After") == "" {
		t.Fatal("expected throttled secret copy to include Retry-After")
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
		"SUPABASE_PUBLISHABLE_KEY":         "pub_",
		"SUPABASE_SECRET_KEY":              "sec_",
		"S3_ACCESS_KEY":                    "s3ak_",
		"S3_SECRET_KEY":                    "s3sk_",
	} {
		value := provisioner.spec.Environment[key]
		if !strings.HasPrefix(value, prefix) {
			t.Fatalf("expected provisioner env %s to have prefix %q, got %q in %#v", key, prefix, value, provisioner.spec.Environment)
		}
	}
	if value := provisioner.spec.Environment["POSTGRES_PASSWORD"]; len(value) != 48 || !isLowerHexForTest(value) {
		t.Fatalf("expected provisioner env POSTGRES_PASSWORD to be 48 lowercase hex chars, got %q in %#v", value, provisioner.spec.Environment)
	}
	for _, key := range []string{"ANON_KEY", "SERVICE_ROLE_KEY"} {
		if strings.Count(provisioner.spec.Environment[key], ".") != 2 {
			t.Fatalf("expected provisioner env %s to be a JWT, got %q in %#v", key, provisioner.spec.Environment[key], provisioner.spec.Environment)
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
	if strings.Count(nextServiceKey, ".") != 2 || nextServiceKey == createdServiceKey {
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
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", routesRoot)
	t.Setenv("SUPADUPA_CERT_ROOT", certRoot)
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
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

func TestProjectUpgradeCreatesPreUpgradeBackupAndReturnsRollbackMetadata(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-safe","name":"Upgrade Safe","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("expected project status 201, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-safe/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusOK {
		t.Fatalf("expected upgrade status 200, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	for _, expected := range []string{
		`"previous_version":"15.8.1.060"`,
		`"target_version":"15.8.1.085"`,
		`"rollback_available":true`,
		`"backup":{`,
		`"status":"completed"`,
		`"stack_version":"15.8.1.085"`,
	} {
		if !strings.Contains(upgradeResponse.Body.String(), expected) {
			t.Fatalf("expected %s in upgrade response: %s", expected, upgradeResponse.Body.String())
		}
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085" {
		t.Fatalf("expected one provisioner upgrade to target version, got %#v", provisioner.upgradeVersions)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-safe")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].Status != "completed" {
		t.Fatalf("expected completed pre-upgrade backup, got %#v", backups)
	}
	if _, err := os.Stat(backups[0].Location); err != nil {
		t.Fatalf("expected backup artifact to exist: %v", err)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.upgrade_backup", "project.upgrade"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectUpgradeRejectsUnsupportedStackVersionBeforeBackup(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-reject","name":"Upgrade Reject","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-reject/upgrade", `{"version":"nightly"}`)
	if upgradeResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected upgrade status 400, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), "unsupported stack version") {
		t.Fatalf("expected unsupported version error: %s", upgradeResponse.Body.String())
	}
	if len(provisioner.upgradeVersions) != 0 {
		t.Fatalf("provisioner should not run for unsupported version, got %#v", provisioner.upgradeVersions)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-reject")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("backup should not run for unsupported version, got %#v", backups)
	}
}

func TestProjectUpgradeUsesVerifiedBackupID(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-with-backup","name":"Upgrade With Backup","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}

	artifact := filepath.Join(backupRoot, "verified.sql")
	body := []byte("-- verified backup\n")
	if err := os.WriteFile(artifact, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	now := time.Now().UTC()
	backup, err := store.CreateBackup(context.Background(), control.BackupInput{
		ProjectRef:     "upgrade-with-backup",
		Kind:           "logical",
		Location:       artifact,
		SizeBytes:      int64(len(body)),
		ChecksumSHA256: hex.EncodeToString(sum[:]),
		Status:         "completed",
		VerifiedAt:     &now,
	})
	if err != nil {
		t.Fatal(err)
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-with-backup/upgrade", `{"version":"15.8.1.085","backup_id":"`+backup.ID+`"}`)
	if upgradeResponse.Code != http.StatusOK {
		t.Fatalf("expected upgrade status 200, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), `"id":"`+backup.ID+`"`) {
		t.Fatalf("expected supplied backup in response: %s", upgradeResponse.Body.String())
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085" {
		t.Fatalf("expected one provisioner upgrade to target version, got %#v", provisioner.upgradeVersions)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-with-backup")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected no extra pre-upgrade backup when backup_id is supplied, got %#v", backups)
	}
}

func TestProjectUpgradeRequiresDurableBackupWhenConfigured(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "true")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-durable-required","name":"Upgrade Durable Required","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-durable-required/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), "local-only") || !strings.Contains(upgradeResponse.Body.String(), "SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP") {
		t.Fatalf("expected durable backup requirement error: %s", upgradeResponse.Body.String())
	}
	if len(provisioner.upgradeVersions) != 0 {
		t.Fatalf("provisioner should not run without durable backup, got %#v", provisioner.upgradeVersions)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-durable-required")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].RemoteLocation != "" {
		t.Fatalf("expected one rejected local pre-upgrade backup artifact, got %#v", backups)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.upgrade_backup_failed"`) || !strings.Contains(auditResponse.Body.String(), `"durable_required":"true"`) {
		t.Fatalf("expected durable backup failure audit: %s", auditResponse.Body.String())
	}
}

func TestProjectUpgradeRejectsUntestedRemoteBackupWhenDurableRequired(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "true")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-untested-target","name":"Upgrade Untested Target","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}
	target, err := store.CreateBackupStorageTarget(context.Background(), control.BackupStorageTargetInput{
		Name:            "Off-host",
		Type:            "s3",
		Region:          "us-east-1",
		Bucket:          "supadupa-upgrade-backups",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	backup := createVerifiedUpgradeBackup(t, store, backupRoot, "upgrade-untested-target", target.ID, "s3://supadupa-upgrade-backups/projects/upgrade-untested-target/backups/verified.sql")

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-untested-target/upgrade", `{"version":"15.8.1.085","backup_id":"`+backup.ID+`"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), "validation-pending") {
		t.Fatalf("expected target validation-pending error: %s", upgradeResponse.Body.String())
	}
	if len(provisioner.upgradeVersions) != 0 {
		t.Fatalf("provisioner should not run without tested durable target, got %#v", provisioner.upgradeVersions)
	}
}

func TestProjectUpgradeAllowsTestedRemoteBackupWhenDurableRequired(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "true")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-durable-ok","name":"Upgrade Durable OK","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}
	target, err := store.CreateBackupStorageTarget(context.Background(), control.BackupStorageTargetInput{
		Name:            "Off-host",
		Type:            "s3",
		Region:          "us-east-1",
		Bucket:          "supadupa-upgrade-backups",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupStorageTargetTestResult(context.Background(), target.ID, time.Now().UTC(), "passed", "ok"); err != nil {
		t.Fatal(err)
	}
	backup := createVerifiedUpgradeBackup(t, store, backupRoot, "upgrade-durable-ok", target.ID, "s3://supadupa-upgrade-backups/projects/upgrade-durable-ok/backups/verified.sql")

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-durable-ok/upgrade", `{"version":"15.8.1.085","backup_id":"`+backup.ID+`"}`)
	if upgradeResponse.Code != http.StatusOK {
		t.Fatalf("expected upgrade status 200, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), `"id":"`+backup.ID+`"`) || !strings.Contains(upgradeResponse.Body.String(), `"remote_location":"s3://supadupa-upgrade-backups/projects/upgrade-durable-ok/backups/verified.sql"`) {
		t.Fatalf("expected durable supplied backup in response: %s", upgradeResponse.Body.String())
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085" {
		t.Fatalf("expected one provisioner upgrade to target version, got %#v", provisioner.upgradeVersions)
	}
}

func TestProjectUpgradeRejectsInvalidBackupIDArtifact(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-bad-backup","name":"Upgrade Bad Backup","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}

	artifact := filepath.Join(backupRoot, "corrupt.sql")
	if err := os.WriteFile(artifact, []byte("-- corrupt backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	backup, err := store.CreateBackup(context.Background(), control.BackupInput{
		ProjectRef:     "upgrade-bad-backup",
		Kind:           "logical",
		Location:       artifact,
		SizeBytes:      18,
		ChecksumSHA256: strings.Repeat("0", 64),
		Status:         "completed",
		VerifiedAt:     &now,
	})
	if err != nil {
		t.Fatal(err)
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-bad-backup/upgrade", `{"version":"15.8.1.085","backup_id":"`+backup.ID+`"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error: %s", upgradeResponse.Body.String())
	}
	if len(provisioner.upgradeVersions) != 0 {
		t.Fatalf("provisioner should not run for invalid backup, got %#v", provisioner.upgradeVersions)
	}
}

func createVerifiedUpgradeBackup(t *testing.T, store control.Store, backupRoot string, ref string, storageTargetID string, remoteLocation string) control.Backup {
	t.Helper()
	artifact := filepath.Join(backupRoot, ref+"-verified.sql")
	body := []byte("-- verified backup for " + ref + "\n")
	if err := os.WriteFile(artifact, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	now := time.Now().UTC()
	backup, err := store.CreateBackup(context.Background(), control.BackupInput{
		ProjectRef:      ref,
		Kind:            "logical",
		Location:        artifact,
		RemoteLocation:  remoteLocation,
		StorageTargetID: storageTargetID,
		SizeBytes:       int64(len(body)),
		ChecksumSHA256:  hex.EncodeToString(sum[:]),
		Status:          "completed",
		VerifiedAt:      &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return backup
}

func TestProjectUpgradeFailureAttemptsRollbackToPreviousVersion(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{upgradeErr: errors.New("apply failed")}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-fail","name":"Upgrade Fail","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-fail/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	for _, expected := range []string{
		`"error":"apply failed"`,
		`"backup":{`,
		`"previous_version":"15.8.1.060"`,
		`"target_version":"15.8.1.085"`,
		`"rollback_available":true`,
		`"rollback_attempted":true`,
	} {
		if !strings.Contains(upgradeResponse.Body.String(), expected) {
			t.Fatalf("expected %s in failed upgrade response: %s", expected, upgradeResponse.Body.String())
		}
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085,15.8.1.060" {
		t.Fatalf("expected target upgrade then rollback to previous version, got %#v", provisioner.upgradeVersions)
	}
	project, err := store.GetProject(context.Background(), "upgrade-fail")
	if err != nil {
		t.Fatal(err)
	}
	if project.Spec.StackVersion != "15.8.1.060" {
		t.Fatalf("store version should remain previous after failed upgrade, got %q", project.Spec.StackVersion)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-fail")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].Status != "completed" {
		t.Fatalf("expected pre-upgrade backup retained after failure, got %#v", backups)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.upgrade_failed"`) || !strings.Contains(auditResponse.Body.String(), `"rollback":"attempted"`) {
		t.Fatalf("expected failed upgrade audit with rollback metadata: %s", auditResponse.Body.String())
	}
}

func TestProjectUpgradeFailureAutoRestoresPreUpgradeBackupWhenEnabled(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_LOGICAL_BACKUP_COMMAND", "printf 'backup for %s\\n' {{ref}}")
	t.Setenv("SUPADUPA_LOGICAL_RESTORE_COMMAND", "printf 'restored %s from %s\\n' {{ref}} {{backup_id}}")
	t.Setenv("SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE", "true")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{upgradeErr: errors.New("apply failed")}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-autorestore","name":"Upgrade Auto Restore","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-autorestore/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	for _, expected := range []string{
		`"error":"apply failed"`,
		`"rollback_attempted":true`,
		`"restore_attempted":true`,
		`"restore_state":"completed"`,
	} {
		if !strings.Contains(upgradeResponse.Body.String(), expected) {
			t.Fatalf("expected %s in failed upgrade response: %s", expected, upgradeResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.upgrade_failed"`) ||
		!strings.Contains(auditResponse.Body.String(), `"restore":"attempted"`) ||
		!strings.Contains(auditResponse.Body.String(), `"restore_state":"completed"`) {
		t.Fatalf("expected failed upgrade audit with restore metadata: %s", auditResponse.Body.String())
	}
}

func TestProjectUpgradeFailureAutoRestoreReportsDryRunAsRestoreError(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_LOGICAL_BACKUP_COMMAND", "printf 'backup for %s\\n' {{ref}}")
	t.Setenv("SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE", "true")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{upgradeErr: errors.New("apply failed")}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-autorestore-dry","name":"Upgrade Auto Restore Dry","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-autorestore-dry/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	for _, expected := range []string{
		`"restore_attempted":true`,
		`"restore_state":"dry-run"`,
		`"restore_error":"logical restore returned state \"dry-run\"`,
	} {
		if !strings.Contains(upgradeResponse.Body.String(), expected) {
			t.Fatalf("expected %s in failed upgrade response: %s", expected, upgradeResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"restore_state":"dry-run"`) ||
		!strings.Contains(auditResponse.Body.String(), `"restore_error":"logical restore returned state \"dry-run\"`) {
		t.Fatalf("expected failed upgrade audit with dry-run restore error metadata: %s", auditResponse.Body.String())
	}
}

func TestProjectUpgradeFailureReportsRollbackAttemptEvenWhenRollbackFails(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{
		upgradeErr:  errors.New("apply failed"),
		rollbackErr: errors.New("rollback failed"),
	}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-rollback-fail","name":"Upgrade Rollback Fail","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-rollback-fail/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	for _, expected := range []string{
		`"error":"apply failed"`,
		`"rollback_available":true`,
		`"rollback_attempted":true`,
		`"rollback_error":"rollback failed"`,
		`"previous_version":"15.8.1.060"`,
		`"target_version":"15.8.1.085"`,
	} {
		if !strings.Contains(upgradeResponse.Body.String(), expected) {
			t.Fatalf("expected %s in failed rollback response: %s", expected, upgradeResponse.Body.String())
		}
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085,15.8.1.060" {
		t.Fatalf("expected target upgrade then rollback attempt, got %#v", provisioner.upgradeVersions)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.upgrade_failed"`) || !strings.Contains(auditResponse.Body.String(), `"rollback_error":"rollback failed"`) {
		t.Fatalf("expected failed upgrade audit with rollback error: %s", auditResponse.Body.String())
	}
}

func TestProjectUpgradeCompatFailureInjectionRequiresHeader(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_COMPAT_UPGRADE_FAILURE_TARGETS", "15.8.1.085")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-inject","name":"Upgrade Inject","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusCreated {
		t.Fatalf("expected project create 201, got %d: %s", response.Code, response.Body.String())
	}

	normalResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-inject/upgrade", `{"version":"15.8.1.085"}`)
	if normalResponse.Code != http.StatusOK {
		t.Fatalf("expected normal upgrade status 200, got %d: %s", normalResponse.Code, normalResponse.Body.String())
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085" {
		t.Fatalf("expected normal upgrade to run once, got %#v", provisioner.upgradeVersions)
	}

	if _, err := store.UpdateProjectStackVersion(context.Background(), "upgrade-inject", "15.8.1.060"); err != nil {
		t.Fatal(err)
	}
	injectedResponse := performWithHeader(server, http.MethodPost, "/v1/projects/upgrade-inject/upgrade", `{"version":"15.8.1.085"}`, "X-Supadupa-Compat-Inject-Upgrade-Failure", "true")
	if injectedResponse.Code != http.StatusConflict {
		t.Fatalf("expected injected upgrade status 409, got %d: %s", injectedResponse.Code, injectedResponse.Body.String())
	}
	for _, expected := range []string{
		`"error":"compat upgrade failure injection for 15.8.1.085"`,
		`"backup":{`,
		`"previous_version":"15.8.1.060"`,
		`"target_version":"15.8.1.085"`,
		`"rollback_attempted":true`,
	} {
		if !strings.Contains(injectedResponse.Body.String(), expected) {
			t.Fatalf("expected %s in injected response: %s", expected, injectedResponse.Body.String())
		}
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085,15.8.1.060" {
		t.Fatalf("expected rollback after injected failure, got %#v", provisioner.upgradeVersions)
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

func TestReconcilePlatformRestoreRuntimeRecreatesAndStopsProjects(t *testing.T) {
	ctx := context.Background()
	routesRoot := t.TempDir()
	certRoot := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", routesRoot)
	t.Setenv("SUPADUPA_CERT_ROOT", certRoot)
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := store.CreateProject(ctx, control.CreateProjectRequest{OrgID: org.ID, Ref: "restored-proj", Name: "Restored", Domain: "apps.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	provisioner := &restoreRuntimeProvisioner{}
	beforeProjects := []control.Project{
		restored,
		{
			Ref: "stale-proj",
			Spec: control.ProjectSpec{
				Ref:    "stale-proj",
				OrgID:  org.ID,
				Name:   "Stale",
				Domain: "apps.example.test",
			},
		},
	}
	staleRoutePath := filepath.Join(routesRoot, "stale-proj.yaml")
	if err := os.WriteFile(staleRoutePath, []byte("project_ref: stale-proj\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	staleCertDir := filepath.Join(certRoot, "stale-proj")
	if err := os.MkdirAll(staleCertDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleCertDir, "stale.example.test.json"), []byte(`{"state":"ready"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	summary := reconcilePlatformRestoreRuntime(ctx, store, provisioner, beforeProjects)

	if summary.Reconciled != 1 || summary.Destroyed != 1 || len(summary.Errors) != 0 {
		t.Fatalf("unexpected restore runtime summary: %#v", summary)
	}
	if len(provisioner.createdRefs) != 1 || provisioner.createdRefs[0] != "restored-proj" {
		t.Fatalf("expected restored project reconcile, got %#v", provisioner.createdRefs)
	}
	if provisioner.destroyedRef != "stale-proj" || !provisioner.destroyOpts.RetainVolumes {
		t.Fatalf("expected stale project retained destroy, ref=%q opts=%#v", provisioner.destroyedRef, provisioner.destroyOpts)
	}
	if _, err := os.Stat(staleRoutePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale route artifact removed, got err=%v", err)
	}
	if _, err := os.Stat(staleCertDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale certificate directory removed, got err=%v", err)
	}
	logs, err := store.ListProjectLogs(ctx, "restored-proj", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 || !strings.Contains(logs[0].Message, "Runtime reconciled after control-plane restore") {
		t.Fatalf("expected restored project reconcile log, got %#v", logs)
	}
	audit, err := store.ListAuditEvents(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	seenStaleDestroy := false
	for _, event := range audit {
		if event.Action == "project.restore_stale_destroyed" && event.Target == "project:stale-proj" && event.Metadata["retain_volumes"] == "true" {
			seenStaleDestroy = true
			break
		}
	}
	if !seenStaleDestroy {
		t.Fatalf("expected stale destroy audit event, got %#v", audit)
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

// enableDatabaseExposure flips the platform master switch on and sets a single
// project's per-project exposure mode/allowlist, mirroring how an operator would
// open a database from the UI (master on + per-project opt-in).
func enableDatabaseExposure(t *testing.T, server *http.Server, ref string, mode string, allowlist string) {
	t.Helper()
	master := perform(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.supadupa.test","stack_version":"latest","profile":"full","resource_tier":"small","backup_schedule":"daily","feature_flags":{"database_external_access":true}}`)
	if master.Code != http.StatusOK {
		t.Fatalf("enable database external access: %d %s", master.Code, master.Body.String())
	}
	cfg := fmt.Sprintf(`{"config":{"db_ingress_mode":%q,"db_allowlist":%q}}`, mode, allowlist)
	resp := perform(server, http.MethodPut, "/v1/projects/"+ref+"/config/network", cfg)
	if resp.Code != http.StatusOK {
		t.Fatalf("set db exposure for %s: %d %s", ref, resp.Code, resp.Body.String())
	}
}

func seedProjectSecrets(t *testing.T, store control.Store, ref string, kinds ...string) {
	t.Helper()
	for _, kind := range kinds {
		if _, err := store.UpsertProjectSecret(context.Background(), ref, kind, control.ProjectSecretInput{Value: kind + "-value"}); err != nil {
			t.Fatalf("seed project secret %s: %v", kind, err)
		}
	}
}

func performWithHeader(server *http.Server, method string, path string, body string, header string, value string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set(header, value)
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

func performWithTokenAndRemoteAddr(server *http.Server, method string, path string, body string, token string, remoteAddr string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.RemoteAddr = remoteAddr
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

func testServerDomainCertificate(t *testing.T, dnsNames []string, notAfter time.Time) (string, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: dnsNames[0]},
		DNSNames:     dnsNames,
		NotBefore:    time.Now().UTC().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	privateKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}))
	return certificate, privateKeyPEM
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

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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

type restoreRuntimeProvisioner struct {
	fakeProvisioner
	createdRefs  []string
	destroyedRef string
	destroyOpts  control.DestroyOptions
}

func (p *restoreRuntimeProvisioner) Create(ctx context.Context, spec control.ProjectSpec) error {
	p.createdRefs = append(p.createdRefs, spec.Ref)
	return nil
}

func (p *restoreRuntimeProvisioner) DestroyWithOptions(ctx context.Context, ref string, opts control.DestroyOptions) error {
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
	syncedReplicasRef  string
	syncedReplicas     []control.ProjectReplica
	clonedBranch       control.BranchCloneOptions
	upgradeVersions    []string
	upgradeErr         error
	rollbackErr        error
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
		if p.syncedAuthHooks[index].Headers != nil {
			headers := make(map[string]string, len(p.syncedAuthHooks[index].Headers))
			for key, value := range p.syncedAuthHooks[index].Headers {
				headers[key] = value
			}
			p.syncedAuthHooks[index].Headers = headers
		}
		if p.syncedAuthHooks[index].RuntimeHeaders != nil {
			headers := make(map[string]string, len(p.syncedAuthHooks[index].RuntimeHeaders))
			for key, value := range p.syncedAuthHooks[index].RuntimeHeaders {
				headers[key] = value
			}
			p.syncedAuthHooks[index].RuntimeHeaders = headers
		}
	}
	return nil
}

func (p *capturingProvisioner) SyncReplicas(ctx context.Context, ref string, replicas []control.ProjectReplica) error {
	p.syncedReplicasRef = ref
	p.syncedReplicas = append([]control.ProjectReplica(nil), replicas...)
	return nil
}

func (p *capturingProvisioner) Upgrade(ctx context.Context, ref string, version string) error {
	p.upgradeVersions = append(p.upgradeVersions, version)
	if len(p.upgradeVersions) == 1 && p.upgradeErr != nil {
		return p.upgradeErr
	}
	if len(p.upgradeVersions) > 1 && p.rollbackErr != nil {
		return p.rollbackErr
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

func isLowerHexForTest(value string) bool {
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
