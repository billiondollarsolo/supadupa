package compose

import (
	"context"
	"encoding/json"
	"errors"
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
	var _ control.ReplicaSyncer = New()
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

	for _, name := range []string{".env", "compose.yaml", "kong.yml", "kong-entrypoint.sh", "vector.yml", "pooler.exs", "pg_hba.conf", "00-supadupa-init.sql", "auth-hooks.json", "functions", "log-drains"} {
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
		"kong/kong:3.9.1",
		"supabase/postgres-meta:v0.96.6",
		"supabase/gotrue:v2.189.0",
		"postgrest/postgrest:v14.12",
		"supabase/realtime:v2.102.3",
		"supabase/storage-api:v1.60.4",
		"darthsim/imgproxy:v3.30.1",
		"supabase/edge-runtime:v1.74.0",
		"supabase/supavisor:2.9.5",
		"supabase/logflare:1.43.1",
		"timberio/vector:0.53.0-alpine",
		"./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro",
		"./00-supadupa-init.sql:/etc/postgresql.schema.sql:ro",
		"./functions:/home/deno/functions",
		"./pooler.exs:/etc/pooler/pooler.exs:ro",
		"./kong.yml:/home/kong/kong.yml:ro",
		"./kong-entrypoint.sh:/home/kong/kong-entrypoint.sh:ro",
		"entrypoint: /home/kong/kong-entrypoint.sh",
		"KONG_PLUGINS: request-transformer,cors,key-auth,acl,basic-auth,post-function",
		"SUPABASE_ANON_KEY: ${ANON_KEY}",
		"SUPABASE_SERVICE_KEY: ${SERVICE_ROLE_KEY}",
		`command: ["/bin/sh", "-c", "/app/bin/migrate && /app/bin/supavisor eval \"$$(cat /etc/pooler/pooler.exs)\" && /app/bin/server"]`,
		"DATABASE_URL: ecto://supabase_admin:${POSTGRES_PASSWORD}@alpha-db:5432/_supabase",
		"POSTGRES_HOST: alpha-db",
		"SUPABASE_URL: http://alpha-kong:8000",
		"GLOBAL_S3_BUCKET: ${GLOBAL_S3_BUCKET}",
		"REQUEST_ALLOW_X_FORWARDED_PATH: ${REQUEST_ALLOW_X_FORWARDED_PATH}",
		"IMGPROXY_LOCAL_FILESYSTEM_ROOT: /",
		"- storage-data:/var/lib/storage:ro",
		"SUPADUPA_FUNCTION_STORAGE_ROOT: /mnt/.supadupa-storage/${STORAGE_TENANT_ID}/${GLOBAL_S3_BUCKET}",
		"- storage-data:/mnt/.supadupa-storage:ro",
		"TUS_URL_PATH: ${TUS_URL_PATH}",
		"UPLOAD_FILE_SIZE_LIMIT: ${UPLOAD_FILE_SIZE_LIMIT}",
		"S3_PROTOCOL_ACCESS_KEY_ID: ${S3_PROTOCOL_ACCESS_KEY_ID}",
		"SELF_HOST_TENANT_NAME: ${PROJECT_REF}",
		"./log-drains:/etc/vector/log-drains:ro",
		"name: alpha-edge",
		"egress: {}",
		"- alpha-db",
		"- alpha-kong",
		"- alpha-pooler",
		"- alpha-studio",
		"- alpha.supabase-realtime",
		"security_opt:\n      - no-new-privileges:true",
	} {
		if !strings.Contains(string(compose), expected) {
			t.Fatalf("expected compose to contain %q, got:\n%s", expected, compose)
		}
	}
	if count := strings.Count(string(compose), "- no-new-privileges:true"); count != 13 {
		t.Fatalf("expected every generated service to set no-new-privileges, got %d occurrences:\n%s", count, compose)
	}
	if strings.Contains(string(compose), "ports:") {
		t.Fatalf("compose should not expose per-service host ports, got:\n%s", compose)
	}
	if strings.Contains(string(compose), "STORAGE_PUBLIC_URL: ${SUPABASE_PUBLIC_URL}") {
		t.Fatalf("storage must use the forwarded request host for S3 SigV4 canonicalization, got:\n%s", compose)
	}
	if !strings.Contains(string(compose), "STORAGE_PUBLIC_URL: ${STORAGE_PUBLIC_URL}") {
		t.Fatalf("compose should pass the dedicated storage public URL to storage, got:\n%s", compose)
	}
	if strings.Contains(string(compose), "logs:") || strings.Contains(string(compose), "/var/log/supadupa") {
		t.Fatalf("compose should use docker log ingestion rather than an unused logs volume, got:\n%s", compose)
	}
	if count := strings.Count(string(compose), "networks: [internal, egress]"); count < 5 {
		t.Fatalf("expected outbound-capable services to join egress network, got %d occurrences:\n%s", count, compose)
	}

	vector, err := os.ReadFile(filepath.Join(root, "alpha", "vector.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"type: internal_logs",
		`supadupa_log_source = "vector_internal"`,
	} {
		if !strings.Contains(string(vector), expected) {
			t.Fatalf("expected vector config to contain %q, got:\n%s", expected, vector)
		}
	}
	if strings.Contains(string(compose), "/var/run/docker.sock") || strings.Contains(string(vector), "/var/run/docker.sock") || strings.Contains(string(vector), "type: docker_logs") {
		t.Fatalf("project compose/vector should not use Docker socket by default, got compose:\n%s\nvector:\n%s", compose, vector)
	}
	if strings.Contains(string(vector), "/var/log/supadupa") {
		t.Fatalf("vector config should not tail unused file logs, got:\n%s", vector)
	}

	hba, err := os.ReadFile(filepath.Join(root, "alpha", "pg_hba.conf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"local replication supabase_replication_admin scram-sha-256",
		"host replication supabase_replication_admin 127.0.0.1/32 scram-sha-256",
		"host replication supabase_replication_admin ::1/128 scram-sha-256",
		"host replication supabase_replication_admin 0.0.0.0/0 scram-sha-256",
		"host replication supabase_replication_admin ::/0 scram-sha-256",
		"host all all 0.0.0.0/0 scram-sha-256",
	} {
		if !strings.Contains(string(hba), expected) {
			t.Fatalf("expected pg_hba.conf to contain %q, got:\n%s", expected, hba)
		}
	}

	kong, err := os.ReadFile(filepath.Join(root, "alpha", "kong.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"/auth/v1", "/rest/v1", "/graphql/v1", "/realtime/v1", "/storage/v1", "/functions/v1"} {
		if !strings.Contains(string(kong), expected) {
			t.Fatalf("expected kong config to contain %q, got:\n%s", expected, kong)
		}
	}
	for _, expected := range []string{
		`consumers:`,
		`username: anon`,
		`key: $SUPABASE_ANON_KEY`,
		`username: service_role`,
		`key: $SUPABASE_SERVICE_KEY`,
		`name: key-auth`,
		`name: acl`,
		`Authorization: $LUA_AUTH_EXPR`,
		`url: http://auth:9999/health`,
		`paths: [/auth/v1/health]`,
		`url: http://rest:3000/rpc/graphql`,
		`"Content-Profile: graphql_public"`,
	} {
		if !strings.Contains(string(kong), expected) {
			t.Fatalf("expected protected kong config to contain %q, got:\n%s", expected, kong)
		}
	}
	for _, expected := range []string{
		`name: realtime-v1-ws`,
		`url: http://alpha.supabase-realtime:4000/socket`,
		`protocol: ws`,
		`paths: [/realtime/v1/]`,
		`name: realtime-v1-rest`,
		`url: http://alpha.supabase-realtime:4000/api`,
		`protocol: http`,
		`paths: [/realtime/v1/api]`,
	} {
		if !strings.Contains(string(kong), expected) {
			t.Fatalf("expected kong Realtime config to contain %q, got:\n%s", expected, kong)
		}
	}
	for _, unexpected := range []string{
		`url: http://realtime:4000/socket/`,
		`url: http://realtime-dev.supabase-realtime:4000/socket`,
		`name: realtime-v1
`,
	} {
		if strings.Contains(string(kong), unexpected) {
			t.Fatalf("expected kong Realtime config to omit %q, got:\n%s", unexpected, kong)
		}
	}
	if strings.Contains(string(kong), "/studio") {
		t.Fatalf("kong should not expose studio on the project API host, got:\n%s", kong)
	}
	entrypoint, err := os.ReadFile(filepath.Join(root, "alpha", "kong-entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"LUA_AUTH_EXPR", "LUA_RT_WS_EXPR", "/home/kong/kong.yml", "$KONG_DECLARATIVE_CONFIG"} {
		if !strings.Contains(string(entrypoint), expected) {
			t.Fatalf("expected kong entrypoint to contain %q, got:\n%s", expected, entrypoint)
		}
	}
	info, err := os.Stat(filepath.Join(root, "alpha", "kong-entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected kong entrypoint to be executable, got mode %v", info.Mode().Perm())
	}

	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"API_EXTERNAL_URL=https://alpha.supadupa.test", "POSTGRES_USER=supabase_admin", "PGRST_DB_URI=postgres://authenticator:", "SUPAVISOR_DB_HOST=db", "STORAGE_IMGPROXY_URL=http://imgproxy:5001", "STORAGE_PUBLIC_URL=https://storage-alpha.supadupa.test", "REQUEST_ALLOW_X_FORWARDED_PATH=true", "GLOBAL_S3_BUCKET=alpha", "TUS_URL_PATH=/upload/resumable", "TUS_URL_EXPIRY_MS=3600000", "UPLOAD_FILE_SIZE_LIMIT=524288000", "UPLOAD_FILE_SIZE_LIMIT_STANDARD=52428800", "UPLOAD_SIGNED_URL_EXPIRATION_TIME=120", "S3_PROTOCOL_ACCESS_KEY_ID=", "S3_PROTOCOL_ACCESS_KEY_SECRET="} {
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
	if dbEncKey := envValue(string(env), "DB_ENC_KEY"); len(dbEncKey) != 16 {
		t.Fatalf("expected DB_ENC_KEY to be 16 characters for realtime AES-128, got %d: %q", len(dbEncKey), dbEncKey)
	}

	initSQL, err := os.ReadFile(filepath.Join(root, "alpha", "00-supadupa-init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"CREATE SCHEMA IF NOT EXISTS auth",
		"CREATE SCHEMA IF NOT EXISTS storage",
		"CREATE SCHEMA IF NOT EXISTS _realtime",
		"CREATE SCHEMA IF NOT EXISTS pgmq",
		"CREATE SCHEMA IF NOT EXISTS _supavisor",
		"ALTER SCHEMA _supavisor OWNER TO supabase_admin",
		"CREATE EXTENSION IF NOT EXISTS pg_graphql",
		"CREATE EXTENSION IF NOT EXISTS pg_cron",
		"CREATE EXTENSION IF NOT EXISTS pgmq WITH SCHEMA pgmq",
		"CREATE EXTENSION IF NOT EXISTS vector",
		"CREATE EXTENSION IF NOT EXISTS supabase_vault",
		"CREATE PUBLICATION supabase_realtime",
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

	poolerConfig, err := os.ReadFile(filepath.Join(root, "alpha", "pooler.exs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`Supavisor.Tenants.create_tenant(params)`,
		`Supavisor.Tenants.delete_tenant_by_external_id(params["external_id"])`,
		`"external_id" => System.get_env("POOLER_TENANT_ID")`,
		`"require_user" => true`,
		`"db_user" => "postgres"`,
		`"mode_type" => "transaction"`,
	} {
		if !strings.Contains(string(poolerConfig), expected) {
			t.Fatalf("expected pooler config to contain %q, got:\n%s", expected, poolerConfig)
		}
	}

	functionMain, err := os.ReadFile(filepath.Join(root, "alpha", "functions", "main", "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"functionNameFromPath",
		`const servicePath = "/home/deno/functions/" + functionName;`,
		"EdgeRuntime.userWorkers.create",
		"await loadFunctionEnv(servicePath)",
		"await resolveFunctionServicePath(functionName, servicePath, functionEnv)",
		`"/home/deno/functions/.supadupa-runtime/" + functionName + "-v" + version`,
		"await requestIsAuthorized(req, functionEnv)",
		"async function resolveFunctionRegion",
		`url.searchParams.get("forceFunctionRegion") ?? req.headers.get("x-region")`,
		`SB_REGION: resolvedRegion`,
		`headers.set("x-sb-edge-region", resolvedRegion)`,
		"function requestTimeoutMs",
		`json(504, { msg: "function timed out", timeout_ms: timeoutMs })`,
		`forceCreate: Deno.env.get("EDGE_RUNTIME_FORCE_CREATE") !== "false"`,
		`context: {`,
		`importMapPath: Deno.env.get("EDGE_RUNTIME_IMPORT_MAP") || null`,
	} {
		if !strings.Contains(string(functionMain), expected) {
			t.Fatalf("expected function dispatcher to contain %q, got:\n%s", expected, functionMain)
		}
	}

	authHooks, err := os.ReadFile(filepath.Join(root, "alpha", "auth-hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(authHooks)) != "[]" {
		t.Fatalf("expected empty auth hook desired state, got:\n%s", authHooks)
	}
}

func TestCreateRendersServiceLimitsOnlyWhenEnforced(t *testing.T) {
	// Default: no enforcement -> no deploy limits emitted.
	root := t.TempDir()
	if err := NewWithOptions(Options{RootDir: root}).Create(context.Background(), control.ProjectSpec{
		Ref: "noenforce", Domain: "supadupa.test", StackVersion: "15.8.1.060", ResourceTier: control.ResourceTierMedium,
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	compose, err := os.ReadFile(filepath.Join(root, "noenforce", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(compose), "limits:") {
		t.Fatalf("expected no deploy limits without enforcement:\n%s", compose)
	}

	// Enforced with an exact override -> per-service deploy limits are emitted
	// across the enabled container set.
	root2 := t.TempDir()
	if err := NewWithOptions(Options{RootDir: root2}).Create(context.Background(), control.ProjectSpec{
		Ref: "enforced", Domain: "supadupa.test", StackVersion: "15.8.1.060",
		ResourceTier: control.ResourceTierMedium, CPU: 3, RAMMB: 6144, EnforceLimits: true,
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	compose2, err := os.ReadFile(filepath.Join(root2, "enforced", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"db", "kong", "meta", "auth", "rest", "realtime", "storage", "imgproxy", "edge-runtime", "pooler", "studio", "analytics", "vector"} {
		assertComposeServiceHasLimits(t, compose2, service)
	}
	if count := strings.Count(string(compose2), "    deploy:\n"); count != 13 {
		t.Fatalf("expected deploy limits for 13 enabled containers, got %d:\n%s", count, compose2)
	}
}

func TestCreateRendersHostBindMountsWhenHostRootConfigured(t *testing.T) {
	root := t.TempDir()
	hostRoot := "/var/lib/supadupa/projects"
	provisioner := NewWithOptions(Options{RootDir: root, HostRootDir: hostRoot})

	spec := control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}
	if err := provisioner.Create(context.Background(), spec); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	compose, err := os.ReadFile(filepath.Join(root, "alpha", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"/var/lib/supadupa/projects/alpha/pg_hba.conf:/etc/postgresql/pg_hba.conf:ro",
		"/var/lib/supadupa/projects/alpha/00-supadupa-init.sql:/etc/postgresql.schema.sql:ro",
		"/var/lib/supadupa/projects/alpha/kong.yml:/home/kong/kong.yml:ro",
		"/var/lib/supadupa/projects/alpha/kong-entrypoint.sh:/home/kong/kong-entrypoint.sh:ro",
		"/var/lib/supadupa/projects/alpha/functions:/home/deno/functions",
		"/var/lib/supadupa/projects/alpha/pooler.exs:/etc/pooler/pooler.exs:ro",
		"/var/lib/supadupa/projects/alpha/vector.yml:/etc/vector/vector.yml:ro",
		"/var/lib/supadupa/projects/alpha/log-drains:/etc/vector/log-drains:ro",
	} {
		if !strings.Contains(string(compose), expected) {
			t.Fatalf("expected compose to contain %q, got:\n%s", expected, compose)
		}
	}
	for _, unexpected := range []string{
		"./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro",
		"./functions:/home/deno/functions",
		"./log-drains:/etc/vector/log-drains:ro",
	} {
		if strings.Contains(string(compose), unexpected) {
			t.Fatalf("expected host-root mode to avoid relative mount %q, got:\n%s", unexpected, compose)
		}
	}
}

func TestCreateCanOptIntoProjectDockerLogCollection(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, ProjectDockerLogs: true})

	spec := control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}
	if err := provisioner.Create(context.Background(), spec); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	compose, err := os.ReadFile(filepath.Join(root, "alpha", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	vector, err := os.ReadFile(filepath.Join(root, "alpha", "vector.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"/var/run/docker.sock:/var/run/docker.sock:ro",
		"type: docker_logs",
		"docker_host: unix:///var/run/docker.sock",
		"com.docker.compose.project=alpha",
	} {
		if !strings.Contains(string(compose)+"\n"+string(vector), expected) {
			t.Fatalf("expected Docker log opt-in output to contain %q, got compose:\n%s\nvector:\n%s", expected, compose, vector)
		}
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
			RuntimeSecret: "hook-secret-value",
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
		"GOTRUE_HOOK_CUSTOM_ACCESS_TOKEN_SECRETS=hook-secret-value",
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

func TestCreateRendersConfiguredStackReleaseManifest(t *testing.T) {
	root := t.TempDir()
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
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "manifest",
		Domain:       "supadupa.test",
		StackVersion: "2026.06.06",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "manifest", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(payload)
	for _, expected := range []string{
		"supabase/postgres:pg-tag",
		"kong/kong:kong-tag",
		"supabase/studio:studio-tag",
		"supabase/postgres-meta:meta-tag",
		"supabase/gotrue:auth-tag",
		"postgrest/postgrest:rest-tag",
		"supabase/realtime:realtime-tag",
		"supabase/storage-api:storage-tag",
		"darthsim/imgproxy:imgproxy-tag",
		"supabase/edge-runtime:edge-tag",
		"supabase/supavisor:pooler-tag",
		"supabase/logflare:analytics-tag",
		"timberio/vector:vector-tag",
	} {
		if !strings.Contains(compose, expected) {
			t.Fatalf("expected compose to contain %q, got:\n%s", expected, compose)
		}
	}
}

func TestCreateFailsWhenStackReleaseCatalogCannotResolve(t *testing.T) {
	root := t.TempDir()
	// Filter catalog to a version that has no manifest so neither requested nor default resolve.
	t.Setenv("SUPADUPA_SUPPORTED_STACK_VERSIONS", "does-not-exist-in-catalog")
	provisioner := NewWithOptions(Options{RootDir: root})

	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "unresolvable",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	})
	if err == nil {
		t.Fatal("expected create to fail when stack release catalog cannot resolve")
	}
	if !strings.Contains(err.Error(), "not available in the active catalog") {
		t.Fatalf("unexpected error: %v", err)
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

func TestDestroySkipsComposeDownWhenProjectWasNeverRendered(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alpha", "functions"), 0o700); err != nil {
		t.Fatal(err)
	}
	provisioner := NewWithOptions(Options{RootDir: root, Apply: true, Command: "definitely-missing-compose-command"})

	if err := provisioner.Destroy(context.Background(), "alpha"); err != nil {
		t.Fatalf("destroy failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected partial project dir removed, got err=%v", err)
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
			"JWT_SECRET":                    "jwt-rotated",
			"ANON_KEY":                      "anon-rotated",
			"SERVICE_ROLE_KEY":              "service-rotated",
			"SUPABASE_PUBLISHABLE_KEY":      "publishable-rotated",
			"SUPABASE_SECRET_KEY":           "secret-rotated",
			"POSTGRES_PASSWORD":             "db-rotated",
			"S3_ACCESS_KEY":                 "s3-access-rotated",
			"S3_SECRET_KEY":                 "s3-secret-rotated",
			"S3_PROTOCOL_ACCESS_KEY_ID":     "s3-access-rotated",
			"S3_PROTOCOL_ACCESS_KEY_SECRET": "s3-secret-rotated",
			"UNMANAGED":                     "should-not-be-added",
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
		"S3_PROTOCOL_ACCESS_KEY_ID=s3-access-rotated",
		"S3_PROTOCOL_ACCESS_KEY_SECRET=s3-secret-rotated",
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

	composePath := filepath.Join(root, "alpha", "compose.yaml")
	composeBefore, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	legacyCompose := strings.ReplaceAll(string(composeBefore), "      - ./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro\n", "")
	if err := os.WriteFile(composePath, []byte(legacyCompose), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := provisioner.SyncSecrets(context.Background(), "alpha", control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
		Environment: map[string]string{
			"JWT_SECRET":        "jwt-rotated",
			"POSTGRES_PASSWORD": "db-rotated",
			"SERVICE_ROLE_KEY":  "service-rotated",
		},
	}); err != nil {
		t.Fatalf("sync secrets after legacy compose failed: %v", err)
	}
	composeAfter, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(composeAfter), "./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro") {
		t.Fatalf("expected sync secrets to refresh compose hba mount, got:\n%s", composeAfter)
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
	envPath := filepath.Join(root, "alpha", ".env")
	initialEnv, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyEnv := removeEnvLines(string(initialEnv), []string{
		"GLOBAL_S3_BUCKET",
		"REQUEST_ALLOW_X_FORWARDED_PATH",
		"S3_PROTOCOL_ACCESS_KEY_ID",
		"S3_PROTOCOL_ACCESS_KEY_SECRET",
	})
	if err := os.WriteFile(envPath, []byte(legacyEnv), 0o600); err != nil {
		t.Fatal(err)
	}
	vectorPath := filepath.Join(root, "alpha", "vector.yml")
	if err := os.WriteFile(vectorPath, []byte("sources:\n  legacy:\n    type: file\n"), 0o600); err != nil {
		t.Fatal(err)
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
	for _, unexpected := range []string{"supabase/storage-api:v1.60.4", "darthsim/imgproxy:v3.30.1", "supabase/edge-runtime:v1.74.0", "supabase/studio:2026.06.03-sha-0bca601"} {
		if strings.Contains(string(compose), unexpected) {
			t.Fatalf("expected compose to omit disabled %q, got:\n%s", unexpected, compose)
		}
	}
	for _, expected := range []string{"supabase/gotrue:v2.189.0", "postgrest/postgrest:v14.12", "supabase/supavisor:2.9.5"} {
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
	for _, expected := range []string{
		"SUPADUPA_SERVICE_STORAGE_ENABLED=false",
		"SUPADUPA_SERVICE_FUNCTIONS_ENABLED=false",
		"SUPADUPA_SERVICE_AUTH_ENABLED=true",
		"GLOBAL_S3_BUCKET=alpha",
		"REQUEST_ALLOW_X_FORWARDED_PATH=true",
		"S3_PROTOCOL_ACCESS_KEY_ID=",
		"S3_PROTOCOL_ACCESS_KEY_SECRET=",
	} {
		if !strings.Contains(string(env), expected) {
			t.Fatalf("expected env to contain %q, got:\n%s", expected, env)
		}
	}
	vector, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"type: internal_logs", `supadupa_log_source = "vector_internal"`} {
		if !strings.Contains(string(vector), expected) {
			t.Fatalf("expected sync services to refresh vector config with %q, got:\n%s", expected, vector)
		}
	}
	if strings.Contains(string(vector), "/var/run/docker.sock") || strings.Contains(string(vector), "type: docker_logs") {
		t.Fatalf("sync services should keep Docker log collection disabled by default, got:\n%s", vector)
	}
}

func TestSyncServicesApplyForceRecreatesEdgeRuntimeDispatcher(t *testing.T) {
	root := t.TempDir()
	renderer := NewWithOptions(Options{RootDir: root})
	if err := renderer.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
		Services: map[string]control.ServiceSpec{
			"functions": {Enabled: true},
		},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	logPath := filepath.Join(root, "compose-commands.log")
	provisioner := NewWithOptions(Options{
		RootDir: root,
		Apply:   true,
		Command: fakeComposeCommandRecorder(t, logPath),
	})

	if err := provisioner.SyncServices(context.Background(), "alpha", control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
		Services: map[string]control.ServiceSpec{
			"functions": {Enabled: true},
		},
	}); err != nil {
		t.Fatalf("sync services failed: %v", err)
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"up -d --force-recreate kong",
		"up -d --remove-orphans",
		"up -d --force-recreate edge-runtime",
	} {
		if !strings.Contains(string(logs), expected) {
			t.Fatalf("expected compose command log to contain %q, got:\n%s", expected, logs)
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

func removeEnvLines(payload string, keys []string) string {
	remove := make(map[string]bool, len(keys))
	for _, key := range keys {
		remove[key] = true
	}
	lines := make([]string, 0)
	for _, line := range strings.Split(payload, "\n") {
		name, _, ok := strings.Cut(line, "=")
		if ok && remove[name] {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func TestCreateApplyRunsDatabaseBootstrap(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUPADUPA_POOLER_START_STABLE_SECONDS", "0")
	t.Setenv("SUPADUPA_POOLER_RESTART_STABLE_SECONDS", "0")
	logPath := filepath.Join(root, "compose-commands.log")
	provisioner := NewWithOptions(Options{
		RootDir: root,
		Apply:   true,
		Command: fakeComposeCommandRecorder(t, logPath),
	})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.085",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"up -d db",
		"exec -T db psql -v ON_ERROR_STOP=1 -U supabase_admin -d postgres -f /etc/postgresql.schema.sql",
		"up -d --scale pooler=0",
		"up -d --force-recreate pooler",
		"up -d pooler",
		"exec -T pooler sh -lc PGPASSWORD=\"$POSTGRES_PASSWORD\" psql -h 127.0.0.1 -p '5432' -U 'postgres.alpha'",
		"exec -T pooler sh -lc PGPASSWORD=\"$POSTGRES_PASSWORD\" psql -h 127.0.0.1 -p '6543' -U 'postgres.alpha'",
	} {
		if !strings.Contains(string(logs), expected) {
			t.Fatalf("expected compose command log to contain %q, got:\n%s", expected, logs)
		}
	}
	dbUpIndex := strings.Index(string(logs), "up -d db")
	bootstrapIndex := strings.Index(string(logs), "exec -T db psql")
	fullUpIndex := strings.Index(string(logs), "up -d --scale pooler=0")
	secondBootstrapIndex := strings.LastIndex(string(logs), "exec -T db psql")
	poolerUpIndex := strings.LastIndex(string(logs), "up -d pooler")
	poolerRecreateIndex := strings.LastIndex(string(logs), "up -d --force-recreate pooler")
	if dbUpIndex < 0 || bootstrapIndex < 0 || fullUpIndex < 0 || secondBootstrapIndex <= bootstrapIndex || poolerUpIndex < 0 ||
		poolerRecreateIndex < 0 || dbUpIndex > bootstrapIndex || bootstrapIndex > fullUpIndex || fullUpIndex > poolerUpIndex ||
		poolerUpIndex > secondBootstrapIndex || secondBootstrapIndex > poolerRecreateIndex {
		t.Fatalf("expected create to start db, bootstrap, start non-pooler stack, start pooler, bootstrap again, then force-recreate pooler, got:\n%s", logs)
	}
	sessionReadyIndex := strings.Index(string(logs), "psql -h 127.0.0.1 -p '5432'")
	transactionReadyIndex := strings.Index(string(logs), "psql -h 127.0.0.1 -p '6543'")
	if sessionReadyIndex <= poolerRecreateIndex || transactionReadyIndex <= sessionReadyIndex {
		t.Fatalf("expected create to probe session then transaction pooler listeners after pooler recreate, got:\n%s", logs)
	}
}

func TestWaitForComposeServiceRunningToleratesTransientRestart(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUPADUPA_COMPOSE_SERVICE_START_TIMEOUT_SECONDS", "1")
	t.Setenv("SUPADUPA_COMPOSE_SERVICE_POLL_INTERVAL_SECONDS", "0")
	countPath := filepath.Join(root, "pooler-ps-count")
	provisioner := NewWithOptions(Options{
		RootDir: root,
		Apply:   true,
		Command: fakeComposeCommandWithTransientPoolerRestart(t, countPath),
	})

	if err := provisioner.waitForComposeServiceRunning(context.Background(), root, "alpha", "pooler", 0); err != nil {
		t.Fatalf("expected transient restarting service to become ready: %v", err)
	}
}

func TestWaitForPoolerConnectionsRetriesUntilListenersAcceptQueries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUPADUPA_POOLER_CONNECTION_READY_TIMEOUT_SECONDS", "1")
	t.Setenv("SUPADUPA_COMPOSE_SERVICE_POLL_INTERVAL_SECONDS", "0")
	countPath := filepath.Join(root, "pooler-ready-count")
	provisioner := NewWithOptions(Options{
		RootDir: root,
		Apply:   true,
		Command: fakeComposeCommandWithTransientPoolerConnection(t, countPath),
	})

	if err := provisioner.waitForPoolerConnections(context.Background(), root, "alpha"); err != nil {
		t.Fatalf("expected transient pooler connection failure to become ready: %v", err)
	}
}

func fakeComposeCommand(t *testing.T, psOutput string) string {
	return fakeComposeCommandWithStderr(t, psOutput, "")
}

func fakeComposeCommandWithStderr(t *testing.T, psOutput string, stderrOutput string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-compose")
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"ps\" ]; then\n" +
		"    cat >&2 <<'WARN'\n" + stderrOutput + "\nWARN\n" +
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

func fakeComposeCommandWithTransientPoolerRestart(t *testing.T, countPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-compose")
	script := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *\"ps --format json pooler\"*)\n" +
		"    if [ ! -f " + shellQuote(countPath) + " ]; then\n" +
		"      printf 1 > " + shellQuote(countPath) + "\n" +
		"      printf '{\"Service\":\"pooler\",\"State\":\"restarting\"}\\n'\n" +
		"    else\n" +
		"      printf '{\"Service\":\"pooler\",\"State\":\"running\"}\\n'\n" +
		"    fi\n" +
		"    exit 0 ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeComposeCommandWithTransientPoolerConnection(t *testing.T, countPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-compose")
	script := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *\"exec -T pooler sh -lc\"*)\n" +
		"    count=0\n" +
		"    if [ -f " + shellQuote(countPath) + " ]; then count=$(cat " + shellQuote(countPath) + "); fi\n" +
		"    count=$((count + 1))\n" +
		"    printf '%s' \"$count\" > " + shellQuote(countPath) + "\n" +
		"    if [ \"$count\" -eq 1 ]; then echo 'pooler not ready' >&2; exit 1; fi\n" +
		"    exit 0 ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeComposeCommandRecorder(t *testing.T, logPath string) string {
	t.Helper()
	t.Setenv("SUPADUPA_POOLER_START_STABLE_SECONDS", "0")
	t.Setenv("SUPADUPA_POOLER_RESTART_STABLE_SECONDS", "0")
	path := filepath.Join(t.TempDir(), "fake-compose")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n" +
		"case \"$*\" in\n" +
		"  *\"ps --format json pooler\"*) printf '{\"Service\":\"pooler\",\"State\":\"running\"}\\n'; exit 0 ;;\n" +
		"  *\"select pg_is_in_recovery()\"*) printf 't\\n'; exit 0 ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertRuntimeService(t *testing.T, services []control.RuntimeService, composeService string, desired bool, state string) {
	t.Helper()
	for _, service := range services {
		if service.ComposeService != composeService {
			continue
		}
		if service.Desired != desired || service.State != state {
			t.Fatalf("runtime service %s = %#v, want desired=%t state=%s", composeService, service, desired, state)
		}
		return
	}
	t.Fatalf("runtime service %s not found in %#v", composeService, services)
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

func TestSyncConfigMaterializesFunctionImportMap(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})
	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	importMap := `{"imports":{"compat:message":"/home/deno/functions/_shared/message.ts"}}`
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "functions",
		Config: map[string]string{
			"import_map":        importMap,
			"worker_timeout_ms": "1500",
		},
	}); err != nil {
		t.Fatalf("sync functions config failed: %v", err)
	}
	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if envValue(string(env), "EDGE_RUNTIME_IMPORT_MAP") != "/home/deno/functions/import_map.json" {
		t.Fatalf("expected managed import map path, got:\n%s", env)
	}
	if envValue(string(env), "FUNCTION_WORKER_TIMEOUT_MS") != "1500" {
		t.Fatalf("expected function worker timeout env, got:\n%s", env)
	}
	materialized, err := os.ReadFile(filepath.Join(root, "alpha", "functions", "import_map.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(materialized), `"compat:message"`) || !strings.Contains(string(materialized), `"/home/deno/functions/_shared/message.ts"`) {
		t.Fatalf("expected import map contents, got:\n%s", materialized)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "functions",
		Config: map[string]string{
			"import_map": "",
		},
	}); err != nil {
		t.Fatalf("sync blank functions config failed: %v", err)
	}
	env, err = os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if envValue(string(env), "EDGE_RUNTIME_IMPORT_MAP") != "" {
		t.Fatalf("expected import map env to be cleared, got:\n%s", env)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha", "functions", "import_map.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected import map file to be removed, got err=%v", err)
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
			"email_enabled":                    "false",
			"magic_link_enabled":               "false",
			"mfa_totp_enabled":                 "true",
			"mfa_totp_enroll_enabled":          "true",
			"mfa_totp_verify_enabled":          "true",
			"mfa_phone_enabled":                "true",
			"mfa_phone_enroll_enabled":         "true",
			"mfa_phone_verify_enabled":         "true",
			"mfa_phone_otp_length":             "8",
			"mfa_phone_max_frequency":          "20s",
			"captcha_provider":                 "turnstile",
			"captcha_site_key":                 "site-key",
			"captcha_secret_handle":            "secret://projects/alpha/captcha-secret",
			"__resolved_captcha_secret_handle": "captcha-secret-value",
			"site_url":                         "https://app.example.com",
			"additional_redirects":             "https://app.example.com/auth/callback,https://admin.example.com/callback",
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
			"oauth_google_enabled":                           "true",
			"oauth_google_client_id":                         "google-client",
			"oauth_google_client_secret_handle":              "secret://projects/alpha/google_oauth_secret",
			"__resolved_oauth_google_client_secret_handle":   "google-oauth-secret-value",
			"oauth_github_enabled":                           "true",
			"oauth_github_client_id":                         "github-client",
			"oauth_discord_enabled":                          "true",
			"oauth_discord_client_id":                        "discord-client",
			"oauth_discord_client_secret_handle":             "secret://projects/alpha/discord_secret",
			"__resolved_oauth_discord_client_secret_handle":  "discord-secret-value",
			"oauth_figma_enabled":                            "true",
			"oauth_figma_client_id":                          "figma-client",
			"oauth_figma_client_secret_handle":               "secret://projects/alpha/figma_secret",
			"__resolved_oauth_figma_client_secret_handle":    "figma-secret-value",
			"oauth_gitlab_enabled":                           "true",
			"oauth_gitlab_url":                               "https://gitlab.example.com",
			"oauth_gitlab_redirect_uri":                      "https://app.example.com/auth/gitlab",
			"oauth_gitlab_skip_nonce_check":                  "true",
			"oauth_snapchat_enabled":                         "true",
			"oauth_snapchat_client_id":                       "snapchat-client",
			"oauth_snapchat_client_secret_handle":            "secret://projects/alpha/snapchat_secret",
			"__resolved_oauth_snapchat_client_secret_handle": "snapchat-secret-value",
			"oauth_oidc_enabled":                             "true",
			"oauth_oidc_issuer_url":                          "https://issuer.example.com",
			"oauth_oidc_client_id":                           "oidc-client",
			"oauth_oidc_client_secret_handle":                "secret://projects/alpha/oidc_secret",
			"oauth_oidc_scopes":                              "openid email profile",
			"phone_enabled":                                  "true",
			"sms_provider":                                   "vonage",
			"sms_otp_exp":                                    "90",
			"sms_otp_length":                                 "8",
			"sms_max_frequency":                              "45s",
			"sms_template":                                   "Code: {{ .Code }}",
			"sms_test_otp_handle":                            "secret://projects/alpha/sms_test_otp",
			"__resolved_sms_test_otp_handle":                 "+15555550123:123456",
			"sms_test_otp_valid_until":                       "2026-12-31T23:59:59Z",
			"sms_twilio_account_sid":                         "sid",
			"sms_twilio_auth_token_handle":                   "secret://projects/alpha/twilio_token",
			"__resolved_sms_twilio_auth_token_handle":        "twilio-token-value",
			"sms_messagebird_originator":                     "Supadupa",
			"sms_messagebird_access_key_handle":              "secret://projects/alpha/messagebird_key",
			"__resolved_sms_messagebird_access_key_handle":   "messagebird-key-value",
			"sms_textlocal_sender":                           "Supadupa",
			"sms_textlocal_api_key_handle":                   "secret://projects/alpha/textlocal_key",
			"__resolved_sms_textlocal_api_key_handle":        "textlocal-key-value",
			"sms_vonage_from":                                "Supadupa",
			"sms_vonage_api_key":                             "vonage-key",
			"sms_vonage_api_secret_handle":                   "secret://projects/alpha/vonage_secret",
			"__resolved_sms_vonage_api_secret_handle":        "vonage-secret-value",
			"saml_enabled":                                   "true",
			"saml_metadata_url":                              "https://idp.example.com/metadata",
			"third_party_jwt_issuer":                         "https://issuer.example.com",
			"web3_ethereum_enabled":                          "true",
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
			"enabled":                    "true",
			"host":                       "smtp.example.com",
			"port":                       "2525",
			"sender_name":                "Supadupa",
			"sender_email":               "noreply@example.com",
			"username":                   "apikey",
			"password_handle":            "secret://projects/alpha/smtp-password",
			"__resolved_password_handle": "smtp-password-value",
			"tls_mode":                   "implicit",
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
		"GOTRUE_SECURITY_CAPTCHA_SECRET=captcha-secret-value",
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
		"GOTRUE_EXTERNAL_GOOGLE_SECRET=google-oauth-secret-value",
		"GOTRUE_EXTERNAL_GITHUB_ENABLED=true",
		"GOTRUE_EXTERNAL_GITHUB_CLIENT_ID=github-client",
		"GOTRUE_EXTERNAL_DISCORD_ENABLED=true",
		"GOTRUE_EXTERNAL_DISCORD_CLIENT_ID=discord-client",
		"GOTRUE_EXTERNAL_DISCORD_SECRET=discord-secret-value",
		"GOTRUE_EXTERNAL_FIGMA_ENABLED=true",
		"GOTRUE_EXTERNAL_FIGMA_CLIENT_ID=figma-client",
		"GOTRUE_EXTERNAL_FIGMA_SECRET=figma-secret-value",
		"GOTRUE_EXTERNAL_GITLAB_ENABLED=true",
		"GOTRUE_EXTERNAL_GITLAB_URL=https://gitlab.example.com",
		"GOTRUE_EXTERNAL_GITLAB_REDIRECT_URI=https://app.example.com/auth/gitlab",
		"GOTRUE_EXTERNAL_GITLAB_SKIP_NONCE_CHECK=true",
		"GOTRUE_EXTERNAL_SNAPCHAT_ENABLED=true",
		"GOTRUE_EXTERNAL_SNAPCHAT_CLIENT_ID=snapchat-client",
		"GOTRUE_EXTERNAL_SNAPCHAT_SECRET=snapchat-secret-value",
		"SUPADUPA_AUTH_OIDC_ENABLED=true",
		"SUPADUPA_AUTH_OIDC_ISSUER_URL=https://issuer.example.com",
		"SUPADUPA_AUTH_OIDC_CLIENT_ID=oidc-client",
		"SUPADUPA_AUTH_OIDC_CLIENT_SECRET_HANDLE=secret://projects/alpha/oidc_secret",
		"SUPADUPA_AUTH_OIDC_SCOPES=openid email profile",
		"GOTRUE_EXTERNAL_PHONE_ENABLED=true",
		"GOTRUE_SMS_PROVIDER=vonage",
		"GOTRUE_SMS_OTP_EXP=90",
		"GOTRUE_SMS_OTP_LENGTH=8",
		"GOTRUE_SMS_MAX_FREQUENCY=45s",
		"GOTRUE_SMS_TEST_OTP=+15555550123:123456",
		"SUPADUPA_SMS_TEST_OTP_HANDLE=secret://projects/alpha/sms_test_otp",
		"GOTRUE_SMS_TEST_OTP_VALID_UNTIL=2026-12-31T23:59:59Z",
		"GOTRUE_SMS_TWILIO_ACCOUNT_SID=sid",
		"GOTRUE_SMS_TWILIO_AUTH_TOKEN=twilio-token-value",
		"GOTRUE_SMS_MESSAGEBIRD_ORIGINATOR=Supadupa",
		"GOTRUE_SMS_MESSAGEBIRD_ACCESS_KEY=messagebird-key-value",
		"GOTRUE_SMS_TEXTLOCAL_SENDER=Supadupa",
		"GOTRUE_SMS_TEXTLOCAL_API_KEY=textlocal-key-value",
		"GOTRUE_SMS_VONAGE_FROM=Supadupa",
		"GOTRUE_SMS_VONAGE_API_KEY=vonage-key",
		"GOTRUE_SMS_VONAGE_API_SECRET=vonage-secret-value",
		"GOTRUE_SAML_ENABLED=true",
		"GOTRUE_SAML_METADATA_URL=https://idp.example.com/metadata",
		"GOTRUE_JWT_EXTERNAL_ISSUER=https://issuer.example.com",
		"SUPADUPA_AUTH_WEB3_ETHEREUM_ENABLED=true",
		"GOTRUE_MAILER_SUBJECTS_CONFIRMATION=Confirm your account",
		`GOTRUE_MAILER_TEMPLATES_CONFIRMATION=Line 1\nLine 2`,
		"GOTRUE_MAILER_SUBJECTS_MAGIC_LINK=Your magic link",
		"GOTRUE_SMS_TEMPLATE=Code: {{ .Token }}",
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
		"SMTP_PASS=smtp-password-value",
		"GOTRUE_SMTP_PASS=smtp-password-value",
		"SUPADUPA_SMTP_PASSWORD_HANDLE=secret://projects/alpha/smtp-password",
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
	if len(status.Services) == 0 {
		t.Fatalf("expected rendered service status list")
	}
	assertRuntimeService(t, status.Services, "db", true, "rendered")
	assertRuntimeService(t, status.Services, "edge-runtime", true, "rendered")
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
	composePayload = []byte(strings.ReplaceAll(string(composePayload), "supabase/storage-api:v1.60.4", "storage-api-disabled"))
	composePayload = []byte(strings.ReplaceAll(string(composePayload), "      - ./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro\n", ""))
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
	if !strings.Contains(status.Message, "missing vector.yml") ||
		!strings.Contains(status.Message, "compose missing supabase/storage-api:") ||
		!strings.Contains(status.Message, "compose missing ./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro") {
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
	assertRuntimeService(t, status.Services, "db", true, "running")
	assertRuntimeService(t, status.Services, "storage", false, "disabled")
	assertRuntimeService(t, status.Services, "edge-runtime", false, "disabled")
}

func TestStatusTreatsRenderedReplicasAsExpectedLiveServices(t *testing.T) {
	root := t.TempDir()
	renderer := NewWithOptions(Options{RootDir: root})
	if err := renderer.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	replicas := []control.ProjectReplica{
		{
			ID:         "replica-one",
			ProjectRef: "alpha",
			Name:       "east",
			Tier:       control.ResourceTierSmall,
		},
	}
	if err := renderer.SyncReplicas(context.Background(), "alpha", replicas); err != nil {
		t.Fatalf("sync replicas failed: %v", err)
	}
	psOutput := `{"Service":"db","State":"running","Health":"healthy"}
{"Service":"kong","State":"running","Health":"healthy"}
{"Service":"meta","State":"running","Health":"healthy"}
{"Service":"auth","State":"running"}
{"Service":"rest","State":"running"}
{"Service":"realtime","State":"running"}
{"Service":"storage","State":"running"}
{"Service":"imgproxy","State":"running"}
{"Service":"edge-runtime","State":"running"}
{"Service":"pooler","State":"running"}
{"Service":"studio","State":"running","Health":"healthy"}
{"Service":"analytics","State":"running"}
{"Service":"vector","State":"running"}
{"Service":"db-replica-east","State":"running","Health":"healthy"}`
	provisioner := NewWithOptions(Options{RootDir: root, Apply: true, Command: fakeComposeCommand(t, psOutput)})

	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectHealthy || status.Message != "compose project running" {
		t.Fatalf("expected live healthy status with replica, got %#v", status)
	}
	assertRuntimeService(t, status.Services, "db-replica-east", true, "running")

	if err := renderer.SyncReplicas(context.Background(), "alpha", nil); err != nil {
		t.Fatalf("sync empty replicas failed: %v", err)
	}
	status, err = provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("stale replica status should report drift without error: %v", err)
	}
	if status.Phase != control.ProjectDegraded || !strings.Contains(status.Message, "unexpected live services db-replica-east=running") {
		t.Fatalf("expected stale replica drift, got %#v", status)
	}
	assertRuntimeService(t, status.Services, "db-replica-east", false, "running")
}

func TestStatusIgnoresComposeWarningsOnStderrWhenParsingLivePS(t *testing.T) {
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
	command := fakeComposeCommandWithStderr(t,
		`{"Service":"db","State":"running","Health":"healthy"}
{"Service":"kong","State":"running","Health":"healthy"}
{"Service":"meta","State":"running","Health":"healthy"}
{"Service":"auth","State":"running"}
{"Service":"rest","State":"running"}
{"Service":"realtime","State":"running"}
{"Service":"pooler","State":"running"}
{"Service":"analytics","State":"running"}
{"Service":"vector","State":"running"}`,
		`time="2026-06-06T05:27:33Z" level=warning msg="The \"GLOBAL_S3_BUCKET\" variable is not set. Defaulting to a blank string."`)
	provisioner := NewWithOptions(Options{RootDir: root, Apply: true, Command: command})

	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectHealthy || status.Message != "compose project running" {
		t.Fatalf("expected live healthy status, got %#v", status)
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
	assertRuntimeService(t, status.Services, "kong", true, "exited")
	assertRuntimeService(t, status.Services, "auth", true, "missing")
}

func TestStatusReportsStartingWhenServiceHealthcheckStillComingUp(t *testing.T) {
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
	// kong is up but its healthcheck is still in the start window, and meta was
	// just created — both transient come-up states, not failures.
	command := fakeComposeCommand(t, `[{"Service":"db","State":"running","Health":"healthy"},{"Service":"kong","State":"running","Health":"starting"},{"Service":"meta","State":"created"},{"Service":"auth","State":"running"},{"Service":"rest","State":"running"},{"Service":"realtime","State":"running"},{"Service":"pooler","State":"running"},{"Service":"analytics","State":"running"},{"Service":"vector","State":"running"}]`)
	provisioner := NewWithOptions(Options{RootDir: root, Apply: true, Command: command})

	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectStarting {
		t.Fatalf("expected starting phase, got %#v", status)
	}
	if !strings.Contains(status.Message, "compose project starting") || !strings.Contains(status.Message, "kong/starting") || !strings.Contains(status.Message, "meta") {
		t.Fatalf("expected starting detail, got %q", status.Message)
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
	assertRuntimeService(t, status.Services, "db", true, "exited")
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
	for _, name := range []string{"kong.yml", "kong-entrypoint.sh", "vector.yml", "pg_hba.conf", "00-supadupa-init.sql"} {
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
	for _, name := range []string{"kong.yml", "kong-entrypoint.sh", "vector.yml", "pg_hba.conf", "00-supadupa-init.sql"} {
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

func TestUpgradeApplyRunsDatabaseBootstrap(t *testing.T) {
	root := t.TempDir()
	renderer := NewWithOptions(Options{RootDir: root})
	if err := renderer.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "old",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	logPath := filepath.Join(root, "compose-commands.log")
	provisioner := NewWithOptions(Options{
		RootDir: root,
		Apply:   true,
		Command: fakeComposeCommandRecorder(t, logPath),
	})

	if err := provisioner.Upgrade(context.Background(), "alpha", "new"); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"up -d --scale pooler=0",
		"up -d --force-recreate kong",
		"exec -T db psql -v ON_ERROR_STOP=1 -U supabase_admin -d postgres -f /etc/postgresql.schema.sql",
		"up -d pooler",
		"up -d --force-recreate pooler",
	} {
		if !strings.Contains(string(logs), expected) {
			t.Fatalf("expected compose command log to contain %q, got:\n%s", expected, logs)
		}
	}
}

func TestUpgradeRerendersConfiguredStackReleaseManifest(t *testing.T) {
	root := t.TempDir()
	// Keep the initial create version resolvable; upgrade target is the configured override.
	t.Setenv("SUPADUPA_SUPPORTED_STACK_VERSIONS", "15.8.1.060,2026.06.06")
	t.Setenv("SUPADUPA_STACK_RELEASES_JSON", `{
		"2026.06.06": {
			"postgres": "pg-upgrade",
			"kong": "kong-upgrade",
			"studio": "studio-upgrade",
			"postgres_meta": "meta-upgrade",
			"auth": "auth-upgrade",
			"rest": "rest-upgrade",
			"realtime": "realtime-upgrade",
			"storage": "storage-upgrade",
			"imgproxy": "imgproxy-upgrade",
			"edge_runtime": "edge-upgrade",
			"pooler": "pooler-upgrade",
			"analytics": "analytics-upgrade",
			"vector": "vector-upgrade"
		}
	}`)
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := provisioner.Upgrade(context.Background(), "alpha", "2026.06.06"); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "alpha", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(payload)
	for _, expected := range []string{
		"supabase/postgres:pg-upgrade",
		"kong/kong:kong-upgrade",
		"supabase/studio:studio-upgrade",
		"supabase/postgres-meta:meta-upgrade",
		"supabase/gotrue:auth-upgrade",
		"postgrest/postgrest:rest-upgrade",
		"supabase/realtime:realtime-upgrade",
		"supabase/storage-api:storage-upgrade",
		"darthsim/imgproxy:imgproxy-upgrade",
		"supabase/edge-runtime:edge-upgrade",
		"supabase/supavisor:pooler-upgrade",
		"supabase/logflare:analytics-upgrade",
		"timberio/vector:vector-upgrade",
	} {
		if !strings.Contains(compose, expected) {
			t.Fatalf("expected upgraded compose to contain %q, got:\n%s", expected, compose)
		}
	}
	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "STACK_VERSION=2026.06.06") {
		t.Fatalf("expected env stack version to update, got:\n%s", env)
	}
}

func TestUpgradePreservesServiceToggles(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "old",
		Services: map[string]control.ServiceSpec{
			"auth":      {Enabled: true},
			"rest":      {Enabled: true},
			"realtime":  {Enabled: true},
			"storage":   {Enabled: true},
			"imgproxy":  {Enabled: true},
			"functions": {Enabled: false},
			"pooler":    {Enabled: true},
			"studio":    {Enabled: false},
			"analytics": {Enabled: true},
			"vector":    {Enabled: true},
		},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := provisioner.Upgrade(context.Background(), "alpha", "new"); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "alpha", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(payload)
	if strings.Contains(compose, "supabase/edge-runtime:") {
		t.Fatalf("expected functions to remain disabled after upgrade:\n%s", compose)
	}
	if strings.Contains(compose, "supabase/studio:") {
		t.Fatalf("expected studio to remain disabled after upgrade:\n%s", compose)
	}
	if !strings.Contains(compose, "supabase/postgres:new") {
		t.Fatalf("expected upgraded postgres image, got:\n%s", compose)
	}
	kong, err := os.ReadFile(filepath.Join(root, "alpha", "kong.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(kong), "functions-v1") || strings.Contains(string(kong), "studio-v1") {
		t.Fatalf("expected kong routes for disabled services to stay absent:\n%s", kong)
	}
}

func TestScaleRewritesComposeResourceLimits(t *testing.T) {
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
	if err := provisioner.Scale(context.Background(), "alpha", control.ProjectSpec{
		Ref:           "alpha",
		Domain:        "supadupa.test",
		StackVersion:  "15.8.1.060",
		ResourceTier:  control.ResourceTierCustom,
		CPU:           6,
		RAMMB:         12288,
		DiskGB:        120,
		EnforceLimits: true,
	}); err != nil {
		t.Fatalf("scale failed: %v", err)
	}
	env, err := os.ReadFile(filepath.Join(root, "alpha", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "RESOURCE_TIER=custom") || !strings.Contains(string(env), "SUPADUPA_RESOURCE_CPU=6") || !strings.Contains(string(env), "SUPADUPA_ENFORCE_LIMITS=true") {
		t.Fatalf("expected RESOURCE_TIER update, got:\n%s", env)
	}
	compose, err := os.ReadFile(filepath.Join(root, "alpha", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"db", "kong", "auth", "edge-runtime", "analytics", "vector"} {
		assertComposeServiceHasLimits(t, compose, service)
	}
	if strings.Contains(composeServiceBlock(t, compose, "db"), `cpus: "6"`) || strings.Contains(composeServiceBlock(t, compose, "db"), "memory: 12288M") {
		t.Fatalf("expected db to receive a per-service share rather than the full project budget:\n%s", composeServiceBlock(t, compose, "db"))
	}
}

func assertComposeServiceHasLimits(t *testing.T, payload []byte, service string) {
	t.Helper()
	block := composeServiceBlock(t, payload, service)
	for _, expected := range []string{"deploy:", "resources:", "limits:", "cpus:", "memory:"} {
		if !strings.Contains(block, expected) {
			t.Fatalf("expected %s block to include %q, got:\n%s", service, expected, block)
		}
	}
}

func composeServiceBlock(t *testing.T, payload []byte, service string) string {
	t.Helper()
	lines := strings.Split(string(payload), "\n")
	header := "  " + service + ":"
	start := -1
	for index, line := range lines {
		if line == header {
			start = index
			break
		}
	}
	if start < 0 {
		t.Fatalf("expected compose service %s in:\n%s", service, payload)
	}
	end := len(lines)
	for index := start + 1; index < len(lines); index++ {
		line := lines[index]
		if line != "" && !strings.HasPrefix(line, " ") {
			end = index
			break
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(strings.TrimSpace(line), ":") {
			end = index
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
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

func TestReadNodeNetworkUsesHostProcNetDev(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev")
	payload := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 10 1 0 0 0 0 0 0 20 1 0 0 0 0 0 0
  eth0: 1000 1 0 0 0 0 0 0 2000 1 0 0 0 0 0 0
docker0: 3000 1 0 0 0 0 0 0 4000 1 0 0 0 0 0 0
  ens5: 5000 1 0 0 0 0 0 0 6000 1 0 0 0 0 0 0
`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUPADUPA_NODE_NET_DEV_PATH", path)

	sampled, rx, tx := readNodeNetwork()
	if !sampled || rx != 6000 || tx != 8000 {
		t.Fatalf("node network = sampled:%v rx:%d tx:%d, want sampled:true rx:6000 tx:8000", sampled, rx, tx)
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
	if !strings.Contains(string(manifest), "image: supabase/postgres:${STACK_VERSION}") || strings.Contains(string(manifest), "supabase/postgres:latest") {
		t.Fatalf("expected replica manifest to use project stack version instead of latest, got:\n%s", manifest)
	}
	if !strings.Contains(string(manifest), "security_opt:\n      - no-new-privileges:true") {
		t.Fatalf("expected replica manifest to set no-new-privileges, got:\n%s", manifest)
	}
	env, err := os.ReadFile(filepath.Join(root, "alpha", "replicas", "replica-one.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "REPLICA_REGION=us-east") || !strings.Contains(string(env), "REPLICATION_MODE=read_replica") {
		t.Fatalf("expected replica env values, got:\n%s", env)
	}
}

func TestSyncReplicasRendersStandbyOverlayAndCleansStaleFiles(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	replicaDir := filepath.Join(root, "alpha", "replicas")
	if err := os.MkdirAll(replicaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replicaDir, "stale.env"), []byte("REPLICA_ID=stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replicas := []control.ProjectReplica{
		{
			ID:               "replica-one",
			ProjectRef:       "alpha",
			Name:             "east",
			Region:           "us-east",
			Tier:             control.ResourceTierSmall,
			ReadWeight:       75,
			FailoverPriority: 2,
		},
	}
	if err := provisioner.SyncReplicas(context.Background(), "alpha", replicas); err != nil {
		t.Fatalf("sync replicas failed: %v", err)
	}
	overlay, err := os.ReadFile(filepath.Join(replicaDir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"db-replica-east:",
		"image: supabase/postgres:${STACK_VERSION}",
		"security_opt:\n      - no-new-privileges:true",
		"- .env",
		"- replicas/replica-one.env",
		"name: alpha-edge",
		"- alpha-db-replica-east",
		"pg_basebackup -h db -p 5432 -U supabase_replication_admin",
		"select pg_is_in_recovery()",
	} {
		if !strings.Contains(string(overlay), expected) {
			t.Fatalf("expected replica overlay to contain %q, got:\n%s", expected, overlay)
		}
	}
	env, err := os.ReadFile(filepath.Join(replicaDir, "replica-one.env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"REPLICA_ID=replica-one", "REPLICA_REGION=us-east", "REPLICA_READ_WEIGHT=75", "FAILOVER_PRIORITY=2"} {
		if !strings.Contains(string(env), expected) {
			t.Fatalf("expected replica env to contain %q, got:\n%s", expected, env)
		}
	}
	if _, err := os.Stat(filepath.Join(replicaDir, "stale.env")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale env removed, got err=%v", err)
	}
	hba, err := os.ReadFile(filepath.Join(root, "alpha", "pg_hba.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hba), "host replication supabase_replication_admin 0.0.0.0/0 scram-sha-256") {
		t.Fatalf("expected replica network hba entry, got:\n%s", hba)
	}

	if err := provisioner.SyncReplicas(context.Background(), "alpha", nil); err != nil {
		t.Fatalf("sync empty replicas failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(replicaDir, "compose.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected empty sync to remove overlay, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(replicaDir, "replica-one.env")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected empty sync to remove env, got err=%v", err)
	}
}

func TestSyncReplicasApplyReloadsPrimaryAsSupabaseAdmin(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "compose-commands.log")
	provisioner := NewWithOptions(Options{
		RootDir: root,
		Apply:   true,
		Command: fakeComposeCommandRecorder(t, logPath),
	})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	replicas := []control.ProjectReplica{
		{
			ID:         "replica-one",
			ProjectRef: "alpha",
			Name:       "east",
			Tier:       control.ResourceTierSmall,
		},
	}
	if err := provisioner.SyncReplicas(context.Background(), "alpha", replicas); err != nil {
		t.Fatalf("sync replicas failed: %v", err)
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"exec -T db psql -U supabase_admin -d postgres -c SELECT pg_reload_conf()",
		"exec -T db-replica-east psql -U postgres -d postgres -Atc select pg_is_in_recovery()",
	} {
		if !strings.Contains(string(logs), expected) {
			t.Fatalf("expected compose command log to contain %q, got:\n%s", expected, logs)
		}
	}
	if strings.Contains(string(logs), "exec -T db psql -U postgres -d postgres -c SELECT pg_reload_conf()") {
		t.Fatalf("primary reload must not use non-superuser postgres role, got:\n%s", logs)
	}
}

func TestDestroyIncludesReplicaOverlay(t *testing.T) {
	root := t.TempDir()
	renderer := NewWithOptions(Options{RootDir: root})
	if err := renderer.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := renderer.SyncReplicas(context.Background(), "alpha", []control.ProjectReplica{
		{
			ID:         "replica-one",
			ProjectRef: "alpha",
			Name:       "east",
			Tier:       control.ResourceTierSmall,
		},
	}); err != nil {
		t.Fatalf("sync replicas failed: %v", err)
	}
	logPath := filepath.Join(root, "compose-commands.log")
	destroyer := NewWithOptions(Options{
		RootDir: root,
		Apply:   true,
		Command: fakeComposeCommandRecorder(t, logPath),
	})
	if err := destroyer.Destroy(context.Background(), "alpha"); err != nil {
		t.Fatalf("destroy failed: %v", err)
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"-f " + filepath.Join(root, "alpha", "compose.yaml"),
		"-f " + filepath.Join(root, "alpha", "replicas", "compose.yaml"),
		"down --remove-orphans -v",
	} {
		if !strings.Contains(string(logs), expected) {
			t.Fatalf("expected destroy command log to contain %q, got:\n%s", expected, logs)
		}
	}
}

func TestReplicaRecoveryTimeoutFromEnv(t *testing.T) {
	t.Setenv("SUPADUPA_REPLICA_READY_TIMEOUT_SECONDS", "17")
	if got := replicaRecoveryTimeout(); got != 17*time.Second {
		t.Fatalf("replicaRecoveryTimeout() = %s, want 17s", got)
	}
	t.Setenv("SUPADUPA_REPLICA_READY_TIMEOUT_SECONDS", "invalid")
	if got := replicaRecoveryTimeout(); got != 240*time.Second {
		t.Fatalf("replicaRecoveryTimeout() invalid fallback = %s, want 240s", got)
	}
}

func TestComposeVolumeCLI(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{command: "docker compose", want: "docker"},
		{command: "podman compose", want: "podman"},
		{command: "/tmp/fake-compose", want: ""},
		{command: "", want: ""},
	}
	for _, tt := range tests {
		if got := composeVolumeCLI(tt.command); got != tt.want {
			t.Fatalf("composeVolumeCLI(%q) = %q, want %q", tt.command, got, tt.want)
		}
	}
}
