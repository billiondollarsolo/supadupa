package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectSpecWithSecretsMapsManagedSecretsToEnvironment(t *testing.T) {
	spec := ProjectSpec{
		Ref: "alpha",
		Environment: map[string]string{
			"CUSTOM":            "kept",
			"POSTGRES_PASSWORD": "caller-value",
		},
	}
	secrets := []ProjectSecret{
		{Kind: "jwt_secret", Value: "jwt-value"},
		{Kind: "jwt_signing_key_current", Value: "current-signing-value"},
		{Kind: "jwt_signing_key_next", Value: "next-signing-value"},
		{Kind: "anon_key", Value: "anon-value"},
		{Kind: "service_role", Value: "service-value"},
		{Kind: "publishable_key", Value: "publishable-value"},
		{Kind: "secret_key", Value: "secret-key-value"},
		{Kind: "db_password", Value: "db-value"},
		{Kind: "s3_access_key", Value: "s3-access"},
		{Kind: "s3_secret_key", Value: "s3-secret"},
	}

	enriched := ProjectSpecWithSecrets(spec, secrets)

	expected := map[string]string{
		"CUSTOM":                           "kept",
		"JWT_SECRET":                       "jwt-value",
		"GOTRUE_JWT_SECRET":                "jwt-value",
		"PGRST_JWT_SECRET":                 "jwt-value",
		"REALTIME_JWT_SECRET":              "jwt-value",
		"SUPADUPA_JWT_SIGNING_KEY_CURRENT": "current-signing-value",
		"SUPADUPA_JWT_SIGNING_KEY_NEXT":    "next-signing-value",
		"ANON_KEY":                         "anon-value",
		"SERVICE_ROLE_KEY":                 "service-value",
		"SUPABASE_PUBLISHABLE_KEY":         "publishable-value",
		"SUPABASE_SECRET_KEY":              "secret-key-value",
		"POSTGRES_PASSWORD":                "db-value",
		"S3_ACCESS_KEY":                    "s3-access",
		"S3_SECRET_KEY":                    "s3-secret",
		"STORAGE_ACCESS_KEY_ID":            "s3-access",
		"STORAGE_SECRET_ACCESS_KEY":        "s3-secret",
		"S3_PROTOCOL_ACCESS_KEY_ID":        "s3-access",
		"S3_PROTOCOL_ACCESS_KEY_SECRET":    "s3-secret",
	}
	for key, value := range expected {
		if enriched.Environment[key] != value {
			t.Fatalf("expected %s=%q, got %q in %#v", key, value, enriched.Environment[key], enriched.Environment)
		}
	}
	if spec.Environment["POSTGRES_PASSWORD"] != "caller-value" {
		t.Fatalf("original spec environment was mutated: %#v", spec.Environment)
	}
}

func TestJWTSigningKeysRotateCurrentIntoHistory(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "jwt-keys", Name: "JWT Keys"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	secrets, err := store.EnsureProjectSecrets(ctx, project.Ref)
	if err != nil {
		t.Fatalf("ensure secrets: %v", err)
	}
	current := secretByKind(secrets, "jwt_signing_key_current")
	next := secretByKind(secrets, "jwt_signing_key_next")
	if current.Value == "" || next.Value == "" || current.Value == next.Value {
		t.Fatalf("expected distinct current and next signing keys")
	}
	nextKID := signingKeyKID(next.Value)
	if len(JWTSigningKeySummaries(project.Ref, secrets)) != 2 {
		t.Fatalf("expected current and next signing key summaries")
	}

	rotated, err := store.RotateProjectSecret(ctx, project.Ref, "jwt_signing_key_current")
	if err != nil {
		t.Fatalf("rotate signing key: %v", err)
	}
	if signingKeyKID(rotated.Value) != nextKID {
		t.Fatalf("expected next key to be promoted to current")
	}
	secrets, err = store.ListProjectSecrets(ctx, project.Ref)
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}
	previous := 0
	for _, secret := range secrets {
		if strings.HasPrefix(secret.Kind, "jwt_signing_key_previous_") {
			previous++
		}
	}
	if previous != 1 {
		t.Fatalf("expected one archived previous signing key, got %d in %#v", previous, secrets)
	}
	summaries := JWTSigningKeySummaries(project.Ref, secrets)
	if len(summaries) != 3 || summaries[0].Status != "current" || summaries[1].Status != "next" || summaries[2].Status != "previous" {
		t.Fatalf("expected current, next, previous summaries, got %#v", summaries)
	}
}

func TestCustomProjectSecretsCanBeStoredAndRevealed(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "custom-secrets", Name: "Custom Secrets"})
	if err != nil {
		t.Fatal(err)
	}

	secret, err := store.UpsertProjectSecret(ctx, project.Ref, "smtp-password", ProjectSecretInput{Value: "smtp-secret"})
	if err != nil {
		t.Fatalf("upsert custom secret: %v", err)
	}
	if secret.Value != "smtp-secret" || secret.Masked == "smtp-secret" || secret.RotatedAt != nil {
		t.Fatalf("unexpected custom secret: %#v", secret)
	}
	revealed, err := store.RevealProjectSecret(ctx, project.Ref, "smtp-password")
	if err != nil {
		t.Fatalf("reveal custom secret: %v", err)
	}
	if revealed.Value != "smtp-secret" {
		t.Fatalf("expected revealed custom secret value, got %#v", revealed)
	}
	updated, err := store.UpsertProjectSecret(ctx, project.Ref, "smtp-password", ProjectSecretInput{Value: "smtp-secret-2"})
	if err != nil {
		t.Fatalf("update custom secret: %v", err)
	}
	if updated.Value != "smtp-secret-2" || updated.RotatedAt == nil {
		t.Fatalf("expected updated custom secret with rotated_at, got %#v", updated)
	}
	if _, err := store.UpsertProjectSecret(ctx, project.Ref, "service_role", ProjectSecretInput{Value: "raw-service"}); err == nil {
		t.Fatalf("expected managed secret upsert to be rejected")
	}
	if _, err := store.UpsertProjectSecret(ctx, project.Ref, "nested/secret", ProjectSecretInput{Value: "raw"}); err == nil {
		t.Fatalf("expected nested secret kind to be rejected")
	}
	if err := store.DeleteProjectSecret(ctx, project.Ref, "smtp-password"); err != nil {
		t.Fatalf("delete custom secret: %v", err)
	}
	if _, err := store.RevealProjectSecret(ctx, project.Ref, "smtp-password"); err == nil {
		t.Fatalf("expected deleted custom secret to be missing")
	}
	if err := store.DeleteProjectSecret(ctx, project.Ref, "service_role"); err == nil {
		t.Fatalf("expected managed secret delete to be rejected")
	}
}

func TestGeneratedSupabaseAPIKeysAreSignedJWTsAndRepairLegacyValues(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "api-keys", Name: "API Keys"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	secrets, err := store.EnsureProjectSecrets(ctx, project.Ref)
	if err != nil {
		t.Fatalf("ensure secrets: %v", err)
	}
	jwtSecret := secretByKind(secrets, "jwt_secret").Value
	for _, tc := range []struct {
		kind string
		role string
	}{
		{kind: "anon_key", role: "anon"},
		{kind: "service_role", role: "service_role"},
	} {
		secret := secretByKind(secrets, tc.kind)
		if !looksLikeJWT(secret.Value) || !verifySupabaseRoleJWT(secret.Value, tc.role, jwtSecret) {
			t.Fatalf("%s was not a valid signed %s JWT: %q", tc.kind, tc.role, secret.Value)
		}
		claims := jwtClaims(t, secret.Value)
		if claims["role"] != tc.role || claims["aud"] != "authenticated" || claims["iss"] != "supabase" {
			t.Fatalf("unexpected claims for %s: %#v", tc.kind, claims)
		}
	}

	store.secrets[project.Ref]["anon_key"] = ProjectSecret{ID: "legacy-anon", ProjectRef: project.Ref, Kind: "anon_key", Value: "anon_legacy", Masked: "********"}
	store.secrets[project.Ref]["service_role"] = ProjectSecret{ID: "legacy-service", ProjectRef: project.Ref, Kind: "service_role", Value: "svc_legacy", Masked: "********"}
	repaired, err := store.EnsureProjectSecrets(ctx, project.Ref)
	if err != nil {
		t.Fatalf("repair secrets: %v", err)
	}
	if !verifySupabaseRoleJWT(secretByKind(repaired, "anon_key").Value, "anon", jwtSecret) {
		t.Fatalf("legacy anon key was not repaired")
	}
	if !verifySupabaseRoleJWT(secretByKind(repaired, "service_role").Value, "service_role", jwtSecret) {
		t.Fatalf("legacy service role key was not repaired")
	}
	rotatedAnon, err := store.RotateProjectSecret(ctx, project.Ref, "anon_key")
	if err != nil {
		t.Fatalf("rotate anon key: %v", err)
	}
	if !verifySupabaseRoleJWT(rotatedAnon.Value, "anon", jwtSecret) {
		t.Fatalf("rotated anon key was not a signed anon JWT")
	}
	rotatedService, err := store.RotateProjectSecret(ctx, project.Ref, "service_role")
	if err != nil {
		t.Fatalf("rotate service role: %v", err)
	}
	if !verifySupabaseRoleJWT(rotatedService.Value, "service_role", jwtSecret) {
		t.Fatalf("rotated service role was not a signed service_role JWT")
	}
}

func signingKeyKID(value string) string {
	material := JWTSigningKeyMaterial{}
	_ = json.Unmarshal([]byte(value), &material)
	return material.KID
}

func jwtClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return claims
}

func secretByKind(secrets []ProjectSecret, kind string) ProjectSecret {
	for _, secret := range secrets {
		if secret.Kind == kind {
			return secret
		}
	}
	return ProjectSecret{}
}
