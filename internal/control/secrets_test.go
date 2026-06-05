package control

import (
	"context"
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

func signingKeyKID(value string) string {
	material := JWTSigningKeyMaterial{}
	_ = json.Unmarshal([]byte(value), &material)
	return material.KID
}

func secretByKind(secrets []ProjectSecret, kind string) ProjectSecret {
	for _, secret := range secrets {
		if secret.Kind == kind {
			return secret
		}
	}
	return ProjectSecret{}
}
