package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"supadupa2026/internal/control"
)

func TestProjectSecretsMaskedAndRevealAudited(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"secret-proj","name":"Secret","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
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
