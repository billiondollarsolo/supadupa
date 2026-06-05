package compose

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"supadupa2026/internal/control"
)

func TestProvisionerImplementsContract(t *testing.T) {
	var _ control.Provisioner = New()
	var _ control.ServiceSyncer = New()
	var _ control.AuthHookSyncer = New()
	var _ control.TelemetryCollector = New()
}

func TestCreateRendersProjectFiles(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	spec := control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}
	if err := provisioner.Create(context.Background(), spec); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	for _, name := range []string{".env", "compose.yaml", "kong.yml", "vector.yml", "00-supadupa-init.sql", "auth-hooks.json", "functions", "log-drains"} {
		path := filepath.Join(root, "alpha", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected rendered file %s: %v", path, err)
		}
	}

	compose, err := os.ReadFile(filepath.Join(root, "alpha", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"supabase/postgres:15.8.1.060",
		"supabase/postgres-meta:latest",
		"supabase/gotrue:latest",
		"postgrest/postgrest:latest",
		"supabase/realtime:latest",
		"supabase/storage-api:latest",
		"darthsim/imgproxy:latest",
		"supabase/edge-runtime:latest",
		"supabase/supavisor:latest",
		"supabase/logflare:latest",
		"timberio/vector:latest-alpine",
		"./00-supadupa-init.sql:/docker-entrypoint-initdb.d/00-supadupa-init.sql:ro",
		"./functions:/home/deno/functions:ro",
		"./log-drains:/etc/vector/log-drains:ro",
		"supadupa-ingress:",
		"- alpha-kong",
		"- alpha-studio",
	} {
		if !strings.Contains(string(compose), expected) {
			t.Fatalf("expected compose to contain %q, got:\n%s", expected, compose)
		}
	}
	if strings.Contains(string(compose), "ports:") {
		t.Fatalf("compose should not expose per-service host ports, got:\n%s", compose)
	}

	kong, err := os.ReadFile(filepath.Join(root, "alpha", "kong.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"/auth/v1", "/rest/v1", "/graphql/v1", "/realtime/v1", "/storage/v1", "/functions/v1", "/studio"} {
		if !strings.Contains(string(kong), expected) {
			t.Fatalf("expected kong config to contain %q, got:\n%s", expected, kong)
		}
	}

	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"API_EXTERNAL_URL=https://alpha.supadupa.test", "PGRST_DB_URI=postgres://authenticator:", "SUPAVISOR_DB_HOST=db", "STORAGE_IMGPROXY_URL=http://imgproxy:5001"} {
		if !strings.Contains(string(env), expected) {
			t.Fatalf("expected env to contain %q, got:\n%s", expected, env)
		}
	}
	for _, expected := range []string{
		"GOTRUE_HOOK_CUSTOM_ACCESS_TOKEN_ENABLED=false",
		"GOTRUE_HOOK_SEND_EMAIL_ENABLED=false",
		"GOTRUE_HOOK_BEFORE_USER_CREATED_ENABLED=false",
	} {
		if !strings.Contains(string(env), expected) {
			t.Fatalf("expected env to contain auth hook default %q, got:\n%s", expected, env)
		}
	}
	postgresPassword := envValue(string(env), "POSTGRES_PASSWORD")
	if postgresPassword == "" {
		t.Fatalf("expected POSTGRES_PASSWORD in env, got:\n%s", env)
	}

	initSQL, err := os.ReadFile(filepath.Join(root, "alpha", "00-supadupa-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"CREATE SCHEMA IF NOT EXISTS auth",
		"CREATE SCHEMA IF NOT EXISTS storage",
		"CREATE EXTENSION IF NOT EXISTS pg_graphql",
		"CREATE EXTENSION IF NOT EXISTS pg_cron",
		"CREATE EXTENSION IF NOT EXISTS pgmq",
		"CREATE EXTENSION IF NOT EXISTS vector",
		"CREATE EXTENSION IF NOT EXISTS supabase_vault",
		"CREATE ROLE service_role",
		"GRANT anon, authenticated, service_role TO authenticator",
	} {
		if !strings.Contains(string(initSQL), expected) {
			t.Fatalf("expected init SQL to contain %q, got:\n%s", expected, initSQL)
		}
	}
	if !strings.Contains(string(initSQL), "PASSWORD '"+postgresPassword+"'") {
		t.Fatalf("expected init SQL to use env postgres password, got:\n%s", initSQL)
	}

	authHooks, err := os.ReadFile(filepath.Join(root, "alpha", "auth-hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(authHooks)) != "[]" {
		t.Fatalf("expected empty auth hook desired state, got:\n%s", authHooks)
	}
}

func TestSyncAuthHooksWritesDesiredState(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	hooks := []control.ProjectAuthHook{
		{
			ID:            "hook-b",
			ProjectRef:    "alpha",
			HookType:      "send_email",
			Enabled:       true,
			EdgeFunction:  "mail-hook",
			Headers:       map[string]string{"x-trace": "email"},
			TimeoutMS:     5000,
			RetryAttempts: 1,
			Status:        "configured",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "hook-a",
			ProjectRef:    "alpha",
			HookType:      "custom_access_token",
			Enabled:       true,
			TargetURI:     "https://hooks.example.com/token",
			SecretHandle:  "secret://projects/alpha/auth/hook-secret",
			Headers:       map[string]string{"authorization": "secret://projects/alpha/auth/hook-header", "x-trace": "token"},
			TimeoutMS:     7000,
			RetryAttempts: 2,
			Status:        "configured",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}
	if err := provisioner.SyncAuthHooks(context.Background(), "alpha", hooks); err != nil {
		t.Fatalf("sync auth hooks failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "alpha", "auth-hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rendered []control.ProjectAuthHook
	if err := json.Unmarshal(payload, &rendered); err != nil {
		t.Fatalf("expected auth hooks json: %v\n%s", err, payload)
	}
	if len(rendered) != 2 {
		t.Fatalf("expected two auth hooks, got %#v", rendered)
	}
	if rendered[0].HookType != "custom_access_token" || rendered[1].HookType != "send_email" {
		t.Fatalf("expected hooks sorted by type, got %#v", rendered)
	}
	if rendered[0].SecretHandle != "secret://projects/alpha/auth/hook-secret" || rendered[0].Headers["authorization"] != "secret://projects/alpha/auth/hook-header" {
		t.Fatalf("expected desired state to preserve secret handles, got %#v", rendered[0])
	}
	if rendered[1].EdgeFunction != "mail-hook" {
		t.Fatalf("expected edge function target, got %#v", rendered[1])
	}
	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"GOTRUE_HOOK_CUSTOM_ACCESS_TOKEN_ENABLED=true",
		"GOTRUE_HOOK_CUSTOM_ACCESS_TOKEN_URI=https://hooks.example.com/token",
		"GOTRUE_HOOK_CUSTOM_ACCESS_TOKEN_SECRETS=",
		"SUPADUPA_AUTH_HOOK_CUSTOM_ACCESS_TOKEN_SECRET_HANDLE=secret://projects/alpha/auth/hook-secret",
		"SUPADUPA_AUTH_HOOK_CUSTOM_ACCESS_TOKEN_TIMEOUT_MS=7000",
		"SUPADUPA_AUTH_HOOK_CUSTOM_ACCESS_TOKEN_RETRY_ATTEMPTS=2",
		"GOTRUE_HOOK_SEND_EMAIL_ENABLED=true",
		"GOTRUE_HOOK_SEND_EMAIL_URI=https://alpha.supadupa.test/functions/v1/mail-hook",
		"SUPADUPA_AUTH_HOOK_SEND_EMAIL_TIMEOUT_MS=5000",
		"GOTRUE_HOOK_BEFORE_USER_CREATED_ENABLED=false",
	} {
		if !strings.Contains(string(env), expected) {
			t.Fatalf("expected env to contain auth hook runtime value %q, got:\n%s", expected, env)
		}
	}

	if err := provisioner.SyncAuthHooks(context.Background(), "alpha", nil); err != nil {
		t.Fatalf("clear auth hooks failed: %v", err)
	}
	cleared, err := os.ReadFile(filepath.Join(root, "alpha", "auth-hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(cleared)) != "[]" {
		t.Fatalf("expected cleared auth hook desired state, got:\n%s", cleared)
	}
	env, err = os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"GOTRUE_HOOK_CUSTOM_ACCESS_TOKEN_ENABLED=false",
		"GOTRUE_HOOK_CUSTOM_ACCESS_TOKEN_URI=",
		"SUPADUPA_AUTH_HOOK_CUSTOM_ACCESS_TOKEN_SECRET_HANDLE=",
		"GOTRUE_HOOK_SEND_EMAIL_ENABLED=false",
		"GOTRUE_HOOK_SEND_EMAIL_URI=",
	} {
		if !strings.Contains(string(env), expected) {
			t.Fatalf("expected cleared env to contain %q, got:\n%s", expected, env)
		}
	}
}

func TestCreateRendersSuppliedManagedSecrets(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
		Environment: map[string]string{
			"JWT_SECRET":        "jwt-from-control-plane",
			"ANON_KEY":          "anon-from-control-plane",
			"SERVICE_ROLE_KEY":  "service-from-control-plane",
			"POSTGRES_PASSWORD": "db-from-control-plane",
		},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"JWT_SECRET=jwt-from-control-plane",
		"GOTRUE_JWT_SECRET=jwt-from-control-plane",
		"PGRST_JWT_SECRET=jwt-from-control-plane",
		"REALTIME_JWT_SECRET=jwt-from-control-plane",
		"ANON_KEY=anon-from-control-plane",
		"SERVICE_ROLE_KEY=service-from-control-plane",
		"POSTGRES_PASSWORD=db-from-control-plane",
		"GOTRUE_DB_DATABASE_URL=postgres://supabase_auth_admin:db-from-control-plane@db:5432/postgres",
		"PGRST_DB_URI=postgres://authenticator:db-from-control-plane@db:5432/postgres",
		"SUPAVISOR_DB_PASSWORD=db-from-control-plane",
	} {
		if !strings.Contains(string(env), expected) {
			t.Fatalf("expected env to contain %q, got:\n%s", expected, env)
		}
	}
	initSQL, err := os.ReadFile(filepath.Join(root, "alpha", "00-supadupa-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(initSQL), "PASSWORD 'db-from-control-plane'") {
		t.Fatalf("expected init SQL to use supplied db password, got:\n%s", initSQL)
	}
}

func TestCreateRendersOrioleDBProfileMetadata(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:     "oriole",
		Domain:  "supadupa.test",
		Profile: control.StackProfileOrioleDB,
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	env, err := os.ReadFile(filepath.Join(root, "oriole", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"STACK_PROFILE=orioledb",
		"SUPADUPA_STACK_PROFILE=orioledb",
		"SUPADUPA_ORIOLEDB_PROFILE=preview",
	} {
		if !strings.Contains(string(env), expected) {
			t.Fatalf("expected env to contain %q, got:\n%s", expected, env)
		}
	}
}

func TestDestroyWithOptionsRetainsVolumeManifest(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})
	if err := provisioner.Create(context.Background(), control.ProjectSpec{Ref: "alpha", Domain: "supadupa.test"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := provisioner.DestroyWithOptions(context.Background(), "alpha", control.DestroyOptions{RetainVolumes: true}); err != nil {
		t.Fatalf("destroy failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("expected project directory removed, got err=%v", err)
	}
	matches, err := filepath.Glob(filepath.Join(root, "_retained", "alpha-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one retained volume manifest, got %#v", matches)
	}
	payload, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"project_ref": "alpha"`, `"alpha_db-data"`, `"alpha_storage-data"`, `"alpha_logs"`} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("expected retained manifest to contain %q, got:\n%s", expected, payload)
		}
	}
}

func TestSyncSecretsUpdatesManagedValuesAndPreservesGeneratedEnv(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
		Environment: map[string]string{
			"JWT_SECRET":        "jwt-initial",
			"POSTGRES_PASSWORD": "db-initial",
			"SERVICE_ROLE_KEY":  "service-initial",
		},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	envBefore, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	dashboardPassword := envValue(string(envBefore), "DASHBOARD_PASSWORD")
	logflareKey := envValue(string(envBefore), "LOGFLARE_API_KEY")

	if err := provisioner.SyncSecrets(context.Background(), "alpha", control.ProjectSpec{
		Ref: "alpha",
		Environment: map[string]string{
			"JWT_SECRET":               "jwt-rotated",
			"ANON_KEY":                 "anon-rotated",
			"SERVICE_ROLE_KEY":         "service-rotated",
			"SUPABASE_PUBLISHABLE_KEY": "publishable-rotated",
			"SUPABASE_SECRET_KEY":      "secret-rotated",
			"POSTGRES_PASSWORD":        "db-rotated",
			"S3_ACCESS_KEY":            "s3-access-rotated",
			"S3_SECRET_KEY":            "s3-secret-rotated",
			"UNMANAGED":                "should-not-be-added",
		},
	}); err != nil {
		t.Fatalf("sync secrets failed: %v", err)
	}

	envAfter, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"JWT_SECRET=jwt-rotated",
		"GOTRUE_JWT_SECRET=jwt-rotated",
		"PGRST_JWT_SECRET=jwt-rotated",
		"REALTIME_JWT_SECRET=jwt-rotated",
		"ANON_KEY=anon-rotated",
		"SERVICE_ROLE_KEY=service-rotated",
		"SUPABASE_PUBLISHABLE_KEY=publishable-rotated",
		"SUPABASE_SECRET_KEY=secret-rotated",
		"POSTGRES_PASSWORD=db-rotated",
		"GOTRUE_DB_DATABASE_URL=postgres://supabase_auth_admin:db-rotated@db:5432/postgres",
		"PGRST_DB_URI=postgres://authenticator:db-rotated@db:5432/postgres",
		"REALTIME_DB_PASSWORD=db-rotated",
		"SUPAVISOR_DB_PASSWORD=db-rotated",
		"S3_ACCESS_KEY=s3-access-rotated",
		"S3_SECRET_KEY=s3-secret-rotated",
	} {
		if !strings.Contains(string(envAfter), expected) {
			t.Fatalf("expected env to contain %q, got:\n%s", expected, envAfter)
		}
	}
	if strings.Contains(string(envAfter), "UNMANAGED=should-not-be-added") {
		t.Fatalf("sync should only add managed secret env keys, got:\n%s", envAfter)
	}
	if envValue(string(envAfter), "DASHBOARD_PASSWORD") != dashboardPassword || envValue(string(envAfter), "LOGFLARE_API_KEY") != logflareKey {
		t.Fatalf("sync should preserve existing generated env values, before:\n%s\nafter:\n%s", envBefore, envAfter)
	}

	initSQL, err := os.ReadFile(filepath.Join(root, "alpha", "00-supadupa-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(initSQL), "PASSWORD 'db-rotated'") {
		t.Fatalf("expected init SQL to use rotated db password, got:\n%s", initSQL)
	}
}

func TestSyncServicesUpdatesComposeKongAndEnv(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	spec := control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
		Services: map[string]control.ServiceSpec{
			"auth":      {Enabled: true},
			"rest":      {Enabled: true},
			"graphql":   {Enabled: true},
			"realtime":  {Enabled: true},
			"storage":   {Enabled: false},
			"imgproxy":  {Enabled: false},
			"functions": {Enabled: false},
			"pooler":    {Enabled: true},
			"studio":    {Enabled: false},
			"analytics": {Enabled: true},
			"vector":    {Enabled: true},
		},
	}
	if err := provisioner.SyncServices(context.Background(), "alpha", spec); err != nil {
		t.Fatalf("sync services failed: %v", err)
	}

	compose, err := os.ReadFile(filepath.Join(root, "alpha", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, unexpected := range []string{"supabase/storage-api:latest", "darthsim/imgproxy:latest", "supabase/edge-runtime:latest", "supabase/studio:latest"} {
		if strings.Contains(string(compose), unexpected) {
			t.Fatalf("expected compose to omit disabled %q, got:\n%s", unexpected, compose)
		}
	}
	for _, expected := range []string{"supabase/gotrue:latest", "postgrest/postgrest:latest", "supabase/supavisor:latest"} {
		if !strings.Contains(string(compose), expected) {
			t.Fatalf("expected compose to keep enabled %q, got:\n%s", expected, compose)
		}
	}

	kong, err := os.ReadFile(filepath.Join(root, "alpha", "kong.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, unexpected := range []string{"/storage/v1", "/functions/v1", "/studio"} {
		if strings.Contains(string(kong), unexpected) {
			t.Fatalf("expected kong to omit disabled route %q, got:\n%s", unexpected, kong)
		}
	}
	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"SUPADUPA_SERVICE_STORAGE_ENABLED=false", "SUPADUPA_SERVICE_FUNCTIONS_ENABLED=false", "SUPADUPA_SERVICE_AUTH_ENABLED=true"} {
		if !strings.Contains(string(env), expected) {
			t.Fatalf("expected env to contain %q, got:\n%s", expected, env)
		}
	}
}

func envValue(payload string, key string) string {
	for _, line := range strings.Split(payload, "\n") {
		name, value, ok := strings.Cut(line, "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}

func fakeComposeCommand(t *testing.T, psOutput string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-compose")
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"ps\" ]; then\n" +
		"    cat <<'JSON'\n" + psOutput + "\nJSON\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSyncConfigCanDisableCaptcha(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})
	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "auth",
		Config: map[string]string{
			"captcha_provider": "turnstile",
			"captcha_site_key": "site-key",
		},
	}); err != nil {
		t.Fatalf("enable captcha failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "auth",
		Config: map[string]string{
			"captcha_provider": "",
			"captcha_site_key": "site-key",
		},
	}); err != nil {
		t.Fatalf("disable captcha failed: %v", err)
	}
	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if envValue(string(env), "GOTRUE_SECURITY_CAPTCHA_ENABLED") != "false" {
		t.Fatalf("expected captcha enabled env to be false, got:\n%s", env)
	}
	if envValue(string(env), "GOTRUE_SECURITY_CAPTCHA_SITE_KEY") != "site-key" {
		t.Fatalf("expected GoTrue captcha site key, got:\n%s", env)
	}
}

func TestSyncConfigUpdatesRuntimeEnvAndPreservesSecrets(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
		Environment: map[string]string{
			"JWT_SECRET":        "jwt-initial",
			"POSTGRES_PASSWORD": "db-initial",
			"SERVICE_ROLE_KEY":  "service-initial",
		},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "auth",
		Config: map[string]string{
			"email_enabled":            "false",
			"magic_link_enabled":       "false",
			"mfa_totp_enabled":         "true",
			"mfa_totp_enroll_enabled":  "true",
			"mfa_totp_verify_enabled":  "true",
			"mfa_phone_enabled":        "true",
			"mfa_phone_enroll_enabled": "true",
			"mfa_phone_verify_enabled": "true",
			"mfa_phone_otp_length":     "8",
			"mfa_phone_max_frequency":  "20s",
			"captcha_provider":         "turnstile",
			"captcha_site_key":         "site-key",
			"captcha_secret_handle":    "secret://projects/alpha/captcha-secret",
			"site_url":                 "https://app.example.com",
			"additional_redirects":     "https://app.example.com/auth/callback,https://admin.example.com/callback",
		},
	}); err != nil {
		t.Fatalf("sync auth config failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "storage",
		Config: map[string]string{
			"file_size_limit_mb":       "100",
			"image_transform_enabled":  "false",
			"resumable_upload_enabled": "true",
			"s3_compat_enabled":        "false",
		},
	}); err != nil {
		t.Fatalf("sync storage config failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "auth_providers",
		Config: map[string]string{
			"oauth_google_enabled":               "true",
			"oauth_google_client_id":             "google-client",
			"oauth_google_client_secret_handle":  "secret://projects/alpha/google_oauth_secret",
			"oauth_github_enabled":               "true",
			"oauth_github_client_id":             "github-client",
			"oauth_discord_enabled":              "true",
			"oauth_discord_client_id":            "discord-client",
			"oauth_discord_client_secret_handle": "secret://projects/alpha/discord_secret",
			"oauth_gitlab_enabled":               "true",
			"oauth_gitlab_url":                   "https://gitlab.example.com",
			"oauth_gitlab_redirect_uri":          "https://app.example.com/auth/gitlab",
			"oauth_gitlab_skip_nonce_check":      "true",
			"oauth_oidc_enabled":                 "true",
			"oauth_oidc_issuer_url":              "https://issuer.example.com",
			"oauth_oidc_client_id":               "oidc-client",
			"oauth_oidc_client_secret_handle":    "secret://projects/alpha/oidc_secret",
			"oauth_oidc_scopes":                  "openid email profile",
			"phone_enabled":                      "true",
			"sms_provider":                       "vonage",
			"sms_twilio_account_sid":             "sid",
			"sms_twilio_auth_token_handle":       "secret://projects/alpha/twilio_token",
			"sms_messagebird_originator":         "Supadupa",
			"sms_messagebird_access_key_handle":  "secret://projects/alpha/messagebird_key",
			"sms_textlocal_sender":               "Supadupa",
			"sms_textlocal_api_key_handle":       "secret://projects/alpha/textlocal_key",
			"sms_vonage_from":                    "Supadupa",
			"sms_vonage_api_key":                 "vonage-key",
			"sms_vonage_api_secret_handle":       "secret://projects/alpha/vonage_secret",
			"saml_enabled":                       "true",
			"saml_metadata_url":                  "https://idp.example.com/metadata",
			"third_party_jwt_issuer":             "https://issuer.example.com",
			"web3_ethereum_enabled":              "true",
		},
	}); err != nil {
		t.Fatalf("sync auth provider config failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "email_templates",
		Config: map[string]string{
			"confirmation_subject":                  "Confirm your account",
			"confirmation_body":                     "Line 1\nLine 2",
			"magic_link_subject":                    "Your magic link",
			"sms_otp_message":                       "Code: {{ .Token }}",
			"notification_password_changed_enabled": "true",
			"notification_password_changed_subject": "Password changed",
			"notification_identity_linked_enabled":  "true",
			"notification_identity_linked_body":     "Identity {{ .IdentityID }} linked",
		},
	}); err != nil {
		t.Fatalf("sync email template config failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "smtp",
		Config: map[string]string{
			"enabled":         "true",
			"host":            "smtp.example.com",
			"port":            "2525",
			"sender_name":     "Supadupa",
			"sender_email":    "noreply@example.com",
			"username":        "apikey",
			"password_handle": "secret://projects/alpha/smtp-password",
			"tls_mode":        "implicit",
		},
	}); err != nil {
		t.Fatalf("sync smtp config failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "realtime",
		Config: map[string]string{
			"postgres_changes_enabled": "true",
			"broadcast_enabled":        "true",
			"presence_enabled":         "true",
			"broadcast_replay":         "true",
			"broadcast_from_database":  "true",
		},
	}); err != nil {
		t.Fatalf("sync realtime config failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "pooler",
		Config: map[string]string{
			"dedicated_pooler_enabled": "true",
			"dedicated_pooler_tier":    "medium",
			"pool_mode":                "both",
			"default_pool_size":        "50",
			"max_client_connections":   "500",
			"transaction_port":         "7654",
			"session_port":             "55432",
		},
	}); err != nil {
		t.Fatalf("sync pooler config failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "ai",
		Config: map[string]string{
			"openai_enabled":              "true",
			"openai_api_key_handle":       "secret://projects/alpha/openai",
			"default_embedding_provider":  "openai",
			"default_embedding_model":     "text-embedding-3-small",
			"default_embedding_dimension": "1536",
			"embedding_queue_enabled":     "true",
			"studio_assistant_enabled":    "true",
			"studio_assistant_provider":   "openai",
			"studio_assistant_model":      "assistant-default",
			"studio_assistant_key_handle": "secret://projects/alpha/studio-ai",
		},
	}); err != nil {
		t.Fatalf("sync ai config failed: %v", err)
	}

	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"JWT_SECRET=jwt-initial",
		"POSTGRES_PASSWORD=db-initial",
		"SERVICE_ROLE_KEY=service-initial",
		"GOTRUE_EXTERNAL_EMAIL_ENABLED=false",
		"SUPADUPA_AUTH_MAGIC_LINK_ENABLED=false",
		"SUPADUPA_AUTH_MFA_TOTP_ENABLED=true",
		"GOTRUE_MFA_TOTP_ENROLL_ENABLED=true",
		"GOTRUE_MFA_TOTP_VERIFY_ENABLED=true",
		"SUPADUPA_AUTH_MFA_PHONE_ENABLED=true",
		"GOTRUE_MFA_PHONE_ENROLL_ENABLED=true",
		"GOTRUE_MFA_PHONE_VERIFY_ENABLED=true",
		"GOTRUE_MFA_PHONE_OTP_LENGTH=8",
		"GOTRUE_MFA_PHONE_MAX_FREQUENCY=20s",
		"GOTRUE_SECURITY_CAPTCHA_ENABLED=true",
		"GOTRUE_SECURITY_CAPTCHA_PROVIDER=turnstile",
		"SUPADUPA_AUTH_CAPTCHA_PROVIDER=turnstile",
		"GOTRUE_SECURITY_CAPTCHA_SITE_KEY=site-key",
		"SUPADUPA_AUTH_CAPTCHA_SITE_KEY=site-key",
		"GOTRUE_SECURITY_CAPTCHA_SECRET=secret://projects/alpha/captcha-secret",
		"SUPADUPA_AUTH_CAPTCHA_SECRET_HANDLE=secret://projects/alpha/captcha-secret",
		"SITE_URL=https://app.example.com",
		"GOTRUE_SITE_URL=https://app.example.com",
		"ADDITIONAL_REDIRECT_URLS=https://app.example.com/auth/callback,https://admin.example.com/callback",
		"GOTRUE_URI_ALLOW_LIST=https://app.example.com/auth/callback,https://admin.example.com/callback",
		"FILE_SIZE_LIMIT=104857600",
		"STORAGE_FILE_SIZE_LIMIT=104857600",
		"ENABLE_IMAGE_TRANSFORMATION=false",
		"STORAGE_TUS_ENABLED=true",
		"STORAGE_S3_PROTOCOL_ENABLED=false",
		"GOTRUE_EXTERNAL_GOOGLE_ENABLED=true",
		"GOTRUE_EXTERNAL_GOOGLE_CLIENT_ID=google-client",
		"GOTRUE_EXTERNAL_GOOGLE_SECRET=secret://projects/alpha/google_oauth_secret",
		"GOTRUE_EXTERNAL_GITHUB_ENABLED=true",
		"GOTRUE_EXTERNAL_GITHUB_CLIENT_ID=github-client",
		"GOTRUE_EXTERNAL_DISCORD_ENABLED=true",
		"GOTRUE_EXTERNAL_DISCORD_CLIENT_ID=discord-client",
		"GOTRUE_EXTERNAL_DISCORD_SECRET=secret://projects/alpha/discord_secret",
		"GOTRUE_EXTERNAL_GITLAB_ENABLED=true",
		"GOTRUE_EXTERNAL_GITLAB_URL=https://gitlab.example.com",
		"GOTRUE_EXTERNAL_GITLAB_REDIRECT_URI=https://app.example.com/auth/gitlab",
		"GOTRUE_EXTERNAL_GITLAB_SKIP_NONCE_CHECK=true",
		"SUPADUPA_AUTH_OIDC_ENABLED=true",
		"SUPADUPA_AUTH_OIDC_ISSUER_URL=https://issuer.example.com",
		"SUPADUPA_AUTH_OIDC_CLIENT_ID=oidc-client",
		"SUPADUPA_AUTH_OIDC_CLIENT_SECRET_HANDLE=secret://projects/alpha/oidc_secret",
		"SUPADUPA_AUTH_OIDC_SCOPES=openid email profile",
		"GOTRUE_EXTERNAL_PHONE_ENABLED=true",
		"GOTRUE_SMS_PROVIDER=vonage",
		"GOTRUE_SMS_TWILIO_ACCOUNT_SID=sid",
		"GOTRUE_SMS_TWILIO_AUTH_TOKEN=secret://projects/alpha/twilio_token",
		"GOTRUE_SMS_MESSAGEBIRD_ORIGINATOR=Supadupa",
		"GOTRUE_SMS_MESSAGEBIRD_ACCESS_KEY=secret://projects/alpha/messagebird_key",
		"GOTRUE_SMS_TEXTLOCAL_SENDER=Supadupa",
		"GOTRUE_SMS_TEXTLOCAL_API_KEY=secret://projects/alpha/textlocal_key",
		"GOTRUE_SMS_VONAGE_FROM=Supadupa",
		"GOTRUE_SMS_VONAGE_API_KEY=vonage-key",
		"GOTRUE_SMS_VONAGE_API_SECRET=secret://projects/alpha/vonage_secret",
		"GOTRUE_SAML_ENABLED=true",
		"GOTRUE_SAML_METADATA_URL=https://idp.example.com/metadata",
		"GOTRUE_JWT_EXTERNAL_ISSUER=https://issuer.example.com",
		"SUPADUPA_AUTH_WEB3_ETHEREUM_ENABLED=true",
		"GOTRUE_MAILER_SUBJECTS_CONFIRMATION=Confirm your account",
		`GOTRUE_MAILER_TEMPLATES_CONFIRMATION=Line 1\nLine 2`,
		"GOTRUE_MAILER_SUBJECTS_MAGIC_LINK=Your magic link",
		"GOTRUE_SMS_OTP_MESSAGE=Code: {{ .Token }}",
		"SUPADUPA_EMAIL_NOTIFICATION_PASSWORD_CHANGED_ENABLED=true",
		"SUPADUPA_EMAIL_NOTIFICATION_PASSWORD_CHANGED_SUBJECT=Password changed",
		"GOTRUE_MAILER_NOTIFICATIONS_PASSWORD_CHANGED_ENABLED=true",
		"SUPADUPA_EMAIL_NOTIFICATION_IDENTITY_LINKED_ENABLED=true",
		"SUPADUPA_EMAIL_NOTIFICATION_IDENTITY_LINKED_BODY=Identity {{ .IdentityID }} linked",
		"GOTRUE_MAILER_NOTIFICATIONS_IDENTITY_LINKED_ENABLED=true",
		"GOTRUE_MAILER_TEMPLATES_IDENTITY_LINKED_NOTIFICATION=Identity {{ .IdentityID }} linked",
		"SUPADUPA_SMTP_ENABLED=true",
		"SMTP_HOST=smtp.example.com",
		"GOTRUE_SMTP_HOST=smtp.example.com",
		"SMTP_PORT=2525",
		"GOTRUE_SMTP_PORT=2525",
		"SMTP_SENDER_NAME=Supadupa",
		"GOTRUE_SMTP_SENDER_NAME=Supadupa",
		"SMTP_ADMIN_EMAIL=noreply@example.com",
		"GOTRUE_SMTP_ADMIN_EMAIL=noreply@example.com",
		"SMTP_USER=apikey",
		"GOTRUE_SMTP_USER=apikey",
		"SMTP_PASS=secret://projects/alpha/smtp-password",
		"GOTRUE_SMTP_PASS=secret://projects/alpha/smtp-password",
		"SMTP_TLS_MODE=implicit",
		"GOTRUE_SMTP_TLS_MODE=implicit",
		"REALTIME_POSTGRES_CHANGES_ENABLED=true",
		"REALTIME_BROADCAST_ENABLED=true",
		"REALTIME_PRESENCE_ENABLED=true",
		"REALTIME_BROADCAST_REPLAY_ENABLED=true",
		"REALTIME_BROADCAST_FROM_DATABASE_ENABLED=true",
		"SUPADUPA_DEDICATED_POOLER_ENABLED=true",
		"SUPADUPA_DEDICATED_POOLER_TIER=medium",
		"SUPADUPA_POOLER_MODE=both",
		"SUPADUPA_POOLER_DEFAULT_POOL_SIZE=50",
		"SUPADUPA_POOLER_MAX_CLIENT_CONNECTIONS=500",
		"SUPADUPA_POOLER_TRANSACTION_PORT=7654",
		"SUPADUPA_POOLER_SESSION_PORT=55432",
		"SUPADUPA_AI_OPENAI_ENABLED=true",
		"SUPADUPA_AI_OPENAI_API_KEY_HANDLE=secret://projects/alpha/openai",
		"SUPADUPA_STUDIO_AI_ASSISTANT_ENABLED=true",
		"SUPADUPA_STUDIO_AI_ASSISTANT_PROVIDER=openai",
		"SUPADUPA_STUDIO_AI_ASSISTANT_MODEL=assistant-default",
		"SUPADUPA_STUDIO_AI_ASSISTANT_KEY_HANDLE=secret://projects/alpha/studio-ai",
	} {
		if !strings.Contains(string(env), expected) {
			t.Fatalf("expected env to contain %q, got:\n%s", expected, env)
		}
	}
}

func TestStatusReportsRenderedProjectEndpoints(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectHealthy {
		t.Fatalf("expected healthy status, got %#v", status)
	}
	for _, endpoint := range []string{"api", "kong", "studio", "rest", "graphql", "realtime", "storage", "functions"} {
		if status.Endpoints[endpoint] == "" {
			t.Fatalf("expected endpoint %q in %#v", endpoint, status.Endpoints)
		}
	}
}

func TestPauseResumeStatusTracksRenderedDesiredState(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := provisioner.Pause(context.Background(), "alpha"); err != nil {
		t.Fatalf("pause failed: %v", err)
	}
	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectPaused || status.Message != "compose project paused" {
		t.Fatalf("expected paused status, got %#v", status)
	}
	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "SUPADUPA_DESIRED_STATE=paused") {
		t.Fatalf("expected paused desired state in env, got:\n%s", env)
	}

	if err := provisioner.Resume(context.Background(), "alpha"); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	status, err = provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectHealthy || status.Message != "compose project rendered" {
		t.Fatalf("expected healthy status after resume, got %#v", status)
	}
}

func TestStatusReportsRenderDrift(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "alpha", "vector.yml")); err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(root, "alpha", "compose.yaml")
	composePayload, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	composePayload = []byte(strings.ReplaceAll(string(composePayload), "supabase/storage-api:latest", "storage-api-disabled"))
	if err := os.WriteFile(composePath, composePayload, 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status should report degraded drift without error: %v", err)
	}
	if status.Phase != control.ProjectDegraded {
		t.Fatalf("expected degraded status, got %#v", status)
	}
	if !strings.Contains(status.Message, "missing vector.yml") || !strings.Contains(status.Message, "compose missing supabase/storage-api:") {
		t.Fatalf("expected drift details in message, got %q", status.Message)
	}
}

func TestStatusUsesLiveComposePSWhenApplyEnabled(t *testing.T) {
	root := t.TempDir()
	renderer := NewWithOptions(Options{RootDir: root})
	if err := renderer.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
		Services: map[string]control.ServiceSpec{
			"storage":   {Enabled: false},
			"imgproxy":  {Enabled: false},
			"functions": {Enabled: false},
			"studio":    {Enabled: false},
		},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	command := fakeComposeCommand(t, `[{"Service":"db","State":"running","Health":"healthy"},{"Service":"kong","State":"running"},{"Service":"meta","State":"running"},{"Service":"auth","State":"running"},{"Service":"rest","State":"running"},{"Service":"realtime","State":"running"},{"Service":"pooler","State":"running"},{"Service":"analytics","State":"running"},{"Service":"vector","State":"running"}]`)
	provisioner := NewWithOptions(Options{RootDir: root, Apply: true, Command: command})

	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectHealthy || status.Message != "compose project running" {
		t.Fatalf("expected live healthy status, got %#v", status)
	}
	if status.Endpoints["storage"] != "" || status.Endpoints["functions"] != "" || status.Endpoints["studio"] != "" {
		t.Fatalf("disabled service endpoints should not be surfaced, got %#v", status.Endpoints)
	}
}

func TestStatusReportsLiveComposeDrift(t *testing.T) {
	root := t.TempDir()
	renderer := NewWithOptions(Options{RootDir: root})
	if err := renderer.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	command := fakeComposeCommand(t, `{"Service":"db","State":"running","Health":"healthy"}
{"Service":"kong","State":"exited","ExitCode":1}
{"Service":"meta","State":"running"}`)
	provisioner := NewWithOptions(Options{RootDir: root, Apply: true, Command: command})

	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status should report live drift without error: %v", err)
	}
	if status.Phase != control.ProjectDegraded {
		t.Fatalf("expected degraded status, got %#v", status)
	}
	if !strings.Contains(status.Message, "missing live services") || !strings.Contains(status.Message, "unhealthy live services kong=exited") {
		t.Fatalf("expected live drift details, got %q", status.Message)
	}
}

func TestStatusReportsLivePausedState(t *testing.T) {
	root := t.TempDir()
	renderer := NewWithOptions(Options{RootDir: root})
	if err := renderer.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
		Services: map[string]control.ServiceSpec{
			"storage":   {Enabled: false},
			"imgproxy":  {Enabled: false},
			"functions": {Enabled: false},
			"studio":    {Enabled: false},
		},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := updateEnvValue(filepath.Join(root, "alpha", ".env"), "SUPADUPA_DESIRED_STATE", "paused"); err != nil {
		t.Fatal(err)
	}
	command := fakeComposeCommand(t, `[{"Service":"db","State":"exited","ExitCode":0},{"Service":"kong","State":"exited","ExitCode":0},{"Service":"meta","State":"exited","ExitCode":0},{"Service":"auth","State":"exited","ExitCode":0},{"Service":"rest","State":"exited","ExitCode":0},{"Service":"realtime","State":"exited","ExitCode":0},{"Service":"pooler","State":"exited","ExitCode":0},{"Service":"analytics","State":"exited","ExitCode":0},{"Service":"vector","State":"exited","ExitCode":0}]`)
	provisioner := NewWithOptions(Options{RootDir: root, Apply: true, Command: command})

	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectPaused || status.Message != "compose project paused" {
		t.Fatalf("expected live paused status, got %#v", status)
	}
}

func TestUpgradeRerendersVersion(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "old",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	envBefore, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	postgresPassword := envValue(string(envBefore), "POSTGRES_PASSWORD")
	for _, name := range []string{"kong.yml", "vector.yml", "00-supadupa-init.sql"} {
		if err := os.Remove(filepath.Join(root, "alpha", name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := provisioner.Upgrade(context.Background(), "alpha", "new"); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "alpha", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "supabase/postgres:new") {
		t.Fatalf("expected upgraded compose image, got:\n%s", payload)
	}
	for _, name := range []string{"kong.yml", "vector.yml", "00-supadupa-init.sql"} {
		if _, err := os.Stat(filepath.Join(root, "alpha", name)); err != nil {
			t.Fatalf("expected upgrade to repair %s: %v", name, err)
		}
	}
	initSQL, err := os.ReadFile(filepath.Join(root, "alpha", "00-supadupa-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(initSQL), "PASSWORD '"+postgresPassword+"'") {
		t.Fatalf("expected upgrade to preserve postgres password in init SQL, got:\n%s", initSQL)
	}
}

func TestScaleWritesTierManifest(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
		ResourceTier: control.ResourceTierSmall,
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := provisioner.Scale(context.Background(), "alpha", control.ResourceTierLarge); err != nil {
		t.Fatalf("scale failed: %v", err)
	}
	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "RESOURCE_TIER=large") {
		t.Fatalf("expected RESOURCE_TIER update, got:\n%s", env)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "alpha", "scale.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "resource_tier: large") || !strings.Contains(string(manifest), `cpus: "4.0"`) {
		t.Fatalf("expected large scale manifest, got:\n%s", manifest)
	}
}

func TestParseComposeStatsAggregatesContainers(t *testing.T) {
	sampledAt := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	payload := []byte(`{"Name":"alpha-db","CPUPerc":"2.50%","MemUsage":"128MiB / 1GiB","NetIO":"1.5kB / 2kB"}
{"Name":"alpha-kong","CPUPerc":"0.75%","MemUsage":"64MiB / 1GiB","NetIO":"512B / 1KiB"}`)

	sample, err := parseComposeStats(payload, sampledAt)
	if err != nil {
		t.Fatalf("parse compose stats failed: %v", err)
	}
	if sample.Source != "compose" || sample.SampledAt != sampledAt {
		t.Fatalf("unexpected sample identity %#v", sample)
	}
	if sample.CPUPercent != 3.25 {
		t.Fatalf("cpu percent = %v, want 3.25", sample.CPUPercent)
	}
	if sample.MemoryBytes != 192*1024*1024 {
		t.Fatalf("memory bytes = %d, want %d", sample.MemoryBytes, int64(192*1024*1024))
	}
	if sample.MemoryLimitBytes != 1024*1024*1024 {
		t.Fatalf("memory limit = %d, want %d", sample.MemoryLimitBytes, int64(1024*1024*1024))
	}
	if sample.NetworkRxBytes != 2012 || sample.NetworkTxBytes != 3024 {
		t.Fatalf("network bytes = %d/%d, want 2012/3024", sample.NetworkRxBytes, sample.NetworkTxBytes)
	}
	if sample.DiskUsedBytes != 0 || sample.DiskLimitBytes != 0 {
		t.Fatalf("compose stats should not report disk usage from block IO counters, got %#v", sample)
	}
}

func TestParseComposeStatsRejectsEmptyPayload(t *testing.T) {
	if _, err := parseComposeStats(nil, time.Now()); err == nil {
		t.Fatal("expected empty compose stats to fail")
	}
}

func TestCloneBranchWritesDryRunPlan(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	for _, ref := range []string{"alpha", "alpha-preview"} {
		if err := provisioner.Create(context.Background(), control.ProjectSpec{
			Ref:          ref,
			Domain:       "supadupa.test",
			StackVersion: "15.8.1.060",
		}); err != nil {
			t.Fatalf("create %s failed: %v", ref, err)
		}
	}
	expires := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	result, err := provisioner.CloneBranch(context.Background(), control.BranchCloneOptions{
		SourceRef: "alpha",
		BranchRef: "alpha-preview",
		BranchID:  "branch-one",
		Name:      "Preview",
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("clone branch failed: %v", err)
	}
	if result.State != "dry-run" || !strings.HasSuffix(result.Path, filepath.Join("alpha-preview", "branch-clone", "clone-plan.sql")) {
		t.Fatalf("expected dry-run clone plan result, got %#v", result)
	}
	payload, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"-- mode: dry-run",
		"-- source_ref: alpha",
		"-- branch_ref: alpha-preview",
		"-- branch_id: branch-one",
		"-- expires_at: 2026-06-05T12:00:00Z",
		"-- no-op dry-run clone marker for alpha-preview from alpha",
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("expected clone plan to contain %q, got:\n%s", expected, payload)
		}
	}
}

func TestCloneBranchRunsConfiguredCommand(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{
		RootDir:            root,
		BranchCloneCommand: "printf 'clone %s to %s via %s\\n' {{source_ref}} {{branch_ref}} {{clone_path}}",
	})

	for _, ref := range []string{"alpha", "alpha-preview"} {
		if err := provisioner.Create(context.Background(), control.ProjectSpec{
			Ref:          ref,
			Domain:       "supadupa.test",
			StackVersion: "15.8.1.060",
		}); err != nil {
			t.Fatalf("create %s failed: %v", ref, err)
		}
	}
	result, err := provisioner.CloneBranch(context.Background(), control.BranchCloneOptions{
		SourceRef: "alpha",
		BranchRef: "alpha-preview",
		BranchID:  "branch-one",
		Name:      "Preview",
	})
	if err != nil {
		t.Fatalf("clone branch failed: %v", err)
	}
	if result.State != "completed" || !strings.HasSuffix(result.Path, filepath.Join("alpha-preview", "branch-clone", "clone.log")) {
		t.Fatalf("expected completed clone command result, got %#v", result)
	}
	payload, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "clone alpha to alpha-preview via "+result.Path) {
		t.Fatalf("expected clone command transcript, got:\n%s", payload)
	}
}

func TestAddReplicaRendersReplicaFiles(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := provisioner.AddReplica(context.Background(), "alpha", control.ReplicaOpts{
		ID:     "replica-one",
		Name:   "east",
		Region: "us-east",
		Tier:   control.ResourceTierSmall,
	}); err != nil {
		t.Fatalf("add replica failed: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "alpha", "replicas", "replica-one.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "db-replica-east") {
		t.Fatalf("expected replica service in manifest, got:\n%s", manifest)
	}
	env, err := os.ReadFile(filepath.Join(root, "alpha", "replicas", "replica-one.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "REPLICA_REGION=us-east") || !strings.Contains(string(env), "REPLICATION_MODE=read_replica") {
		t.Fatalf("expected replica env values, got:\n%s", env)
	}
}
