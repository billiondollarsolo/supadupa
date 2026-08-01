package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"supadupa2026/internal/control"
)

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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if secondProjectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected second project status 202, got %d: %s", secondProjectResponse.Code, secondProjectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	off := perform(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.supadupa.test","stack_version":"latest","profile":"full","resource_tier":"custom","backup_schedule":"daily","feature_flags":{"database_external_access":false}}`)
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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

func TestProjectConfigRuntimeSecretResolutionAndRollback(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"runtime-secret-proj","name":"Runtime Secret","domain":"supadupa.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
