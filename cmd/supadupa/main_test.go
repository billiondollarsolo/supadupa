package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"supadupa2026/internal/control"
)

func TestBootstrapInitialAdminNoEnvDoesNothing(t *testing.T) {
	t.Setenv("SUPADUPA_BOOTSTRAP_EMAIL", "")
	t.Setenv("SUPADUPA_BOOTSTRAP_PASSWORD", "")
	store := control.NewMemoryStore()

	if err := bootstrapInitialAdmin(context.Background(), store, discardLogger()); err != nil {
		t.Fatalf("bootstrapInitialAdmin returned error: %v", err)
	}
	if store.HasUsers(context.Background()) {
		t.Fatal("expected no users without bootstrap env")
	}
}

func TestBootstrapInitialAdminCreatesAdminFromEnv(t *testing.T) {
	t.Setenv("SUPADUPA_BOOTSTRAP_EMAIL", "Admin@SUPADUPA.local")
	t.Setenv("SUPADUPA_BOOTSTRAP_PASSWORD", "supadupa2026")
	store := control.NewMemoryStore()
	ctx := context.Background()

	if err := bootstrapInitialAdmin(ctx, store, discardLogger()); err != nil {
		t.Fatalf("bootstrapInitialAdmin returned error: %v", err)
	}
	user, err := store.AuthenticateUser(ctx, "admin@supadupa.local", "supadupa2026")
	if err != nil {
		t.Fatalf("expected seeded admin to authenticate: %v", err)
	}
	if user.Role != "admin" {
		t.Fatalf("expected admin role, got %q", user.Role)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatalf("expected audit events: %v", err)
	}
	if len(events) != 1 || events[0].Action != "user.bootstrap_env" {
		t.Fatalf("expected bootstrap audit event, got %#v", events)
	}

	if err := bootstrapInitialAdmin(ctx, store, discardLogger()); err != nil {
		t.Fatalf("second bootstrapInitialAdmin returned error: %v", err)
	}
	events, err = store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatalf("expected audit events after second bootstrap: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected second bootstrap to skip existing users, got %d audit events", len(events))
	}
}

func TestBootstrapInitialAdminRequiresEmailAndPasswordTogether(t *testing.T) {
	t.Setenv("SUPADUPA_BOOTSTRAP_EMAIL", "admin@supadupa.local")
	t.Setenv("SUPADUPA_BOOTSTRAP_PASSWORD", "")

	err := bootstrapInitialAdmin(context.Background(), control.NewMemoryStore(), discardLogger())
	if err == nil || !strings.Contains(err.Error(), "must be set together") {
		t.Fatalf("expected paired-env error, got %v", err)
	}
}

func TestValidateRuntimeSecretsRequiresStrongSecrets(t *testing.T) {
	err := validateRuntimeSecretsFromEnv(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), control.PlatformSecretKeyEnv) {
		t.Fatalf("expected missing platform secret error, got %v", err)
	}

	values := map[string]string{
		control.PlatformSecretKeyEnv: "local-dev-secret-change-me",
		control.AuthSecretEnv:        strings.Repeat("a", 32),
	}
	err = validateRuntimeSecretsFromEnv(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "known development placeholder") {
		t.Fatalf("expected placeholder error, got %v", err)
	}
	if strings.Contains(err.Error(), values[control.PlatformSecretKeyEnv]) {
		t.Fatalf("runtime secret error leaked platform secret value: %v", err)
	}

	values[control.PlatformSecretKeyEnv] = strings.Repeat("p", 32)
	values[control.AuthSecretEnv] = "short"
	err = validateRuntimeSecretsFromEnv(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "at least 32 characters") {
		t.Fatalf("expected short auth secret error, got %v", err)
	}
	if strings.Contains(err.Error(), values[control.AuthSecretEnv]) {
		t.Fatalf("runtime secret error leaked auth secret value: %v", err)
	}

	values[control.AuthSecretEnv] = strings.Repeat("a", 32)
	if err := validateRuntimeSecretsFromEnv(func(key string) string { return values[key] }); err != nil {
		t.Fatalf("expected strong secrets to pass, got %v", err)
	}
}

func TestValidateRuntimeSecretsAllowsExplicitDevOverride(t *testing.T) {
	values := map[string]string{
		"SUPADUPA_ALLOW_DEV_SECRETS": "true",
		control.PlatformSecretKeyEnv: "",
		control.AuthSecretEnv:        "",
	}
	if err := validateRuntimeSecretsFromEnv(func(key string) string { return values[key] }); err != nil {
		t.Fatalf("expected dev override to pass, got %v", err)
	}
}

func TestWarnPlatformSSOJSONAdapterLogsWhenEnabled(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	warnPlatformSSOJSONAdapter(logger, func(key string) string {
		if key == "SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER" {
			return "true"
		}
		return ""
	})
	out := buf.String()
	if !strings.Contains(out, "platform SSO JSON adapter is enabled") {
		t.Fatalf("expected SSO adapter warning log, got %q", out)
	}
	if !strings.Contains(out, "not production SAML") {
		t.Fatalf("expected production SAML honesty in warning, got %q", out)
	}

	buf.Reset()
	warnPlatformSSOJSONAdapter(logger, func(string) string { return "" })
	if strings.TrimSpace(buf.String()) != "" {
		t.Fatalf("expected no warning when adapter disabled, got %q", buf.String())
	}
}

func TestBootstrapPlatformDefaultsNoEnvDoesNothing(t *testing.T) {
	store := control.NewMemoryStore()
	ctx := context.Background()

	before, err := store.GetPlatformDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := bootstrapPlatformDefaults(ctx, store, discardLogger()); err != nil {
		t.Fatalf("bootstrapPlatformDefaults returned error: %v", err)
	}
	after, err := store.GetPlatformDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.Domain != after.Domain || before.StackVersion != after.StackVersion || before.Profile != after.Profile || before.ResourceTier != after.ResourceTier || before.BackupSchedule != after.BackupSchedule {
		t.Fatalf("expected defaults to stay unchanged, before=%#v after=%#v", before, after)
	}
}

func TestBootstrapPlatformDefaultsSeedsAppsDomainWhenDefault(t *testing.T) {
	t.Setenv("SUPADUPA_APPS_DOMAIN", "apps.example.com")
	store := control.NewMemoryStore()
	ctx := context.Background()

	if err := bootstrapPlatformDefaults(ctx, store, discardLogger()); err != nil {
		t.Fatalf("bootstrapPlatformDefaults returned error: %v", err)
	}
	defaults, err := store.GetPlatformDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Domain != "apps.example.com" {
		t.Fatalf("expected apps domain bootstrap, got %#v", defaults)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "platform_defaults.bootstrap_env" || events[0].Metadata["domain"] != "apps.example.com" {
		t.Fatalf("expected platform defaults bootstrap audit event, got %#v", events)
	}
}

func TestBootstrapPlatformDefaultsDoesNotOverwriteUserConfiguredAppsDomain(t *testing.T) {
	t.Setenv("SUPADUPA_APPS_DOMAIN", "apps.env.example.com")
	store := control.NewMemoryStore()
	ctx := context.Background()
	if _, err := store.UpdatePlatformDefaults(ctx, control.PlatformDefaultsInput{Domain: "apps.ui.example.com"}); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapPlatformDefaults(ctx, store, discardLogger()); err != nil {
		t.Fatalf("bootstrapPlatformDefaults returned error: %v", err)
	}
	defaults, err := store.GetPlatformDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Domain != "apps.ui.example.com" {
		t.Fatalf("expected user-configured domain to remain, got %#v", defaults)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no audit event for skipped bootstrap, got %#v", events)
	}
}

func TestBootstrapPlatformDefaultsProjectDomainOverridesUserConfiguredDomain(t *testing.T) {
	t.Setenv("SUPADUPA_PROJECT_DOMAIN", "apps.env.example.com")
	store := control.NewMemoryStore()
	ctx := context.Background()
	if _, err := store.UpdatePlatformDefaults(ctx, control.PlatformDefaultsInput{Domain: "apps.ui.example.com"}); err != nil {
		t.Fatal(err)
	}

	if err := bootstrapPlatformDefaults(ctx, store, discardLogger()); err != nil {
		t.Fatalf("bootstrapPlatformDefaults returned error: %v", err)
	}
	defaults, err := store.GetPlatformDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Domain != "apps.env.example.com" {
		t.Fatalf("expected explicit env domain override, got %#v", defaults)
	}
}

func TestBootstrapDefaultBackupStorageTargetNoEnvDoesNothing(t *testing.T) {
	store := control.NewMemoryStore()

	if err := bootstrapDefaultBackupStorageTarget(context.Background(), store, discardLogger()); err != nil {
		t.Fatalf("bootstrapDefaultBackupStorageTarget returned error: %v", err)
	}
	targets, err := store.ListBackupStorageTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 0 {
		t.Fatalf("expected no backup targets without env, got %#v", targets)
	}
}

func TestBootstrapDefaultBackupStorageTargetIgnoresSetupComposeEmptyDefaults(t *testing.T) {
	t.Setenv("SUPADUPA_BACKUP_TARGET_NAME", "")
	t.Setenv("SUPADUPA_BACKUP_TARGET_ENDPOINT", "")
	t.Setenv("SUPADUPA_BACKUP_TARGET_REGION", "auto")
	t.Setenv("SUPADUPA_BACKUP_TARGET_BUCKET", "")
	t.Setenv("SUPADUPA_BACKUP_TARGET_PREFIX", "supadupa")
	t.Setenv("SUPADUPA_BACKUP_TARGET_ACCESS_KEY_ID", "")
	t.Setenv("SUPADUPA_BACKUP_TARGET_SECRET_ACCESS_KEY", "")
	t.Setenv("SUPADUPA_BACKUP_TARGET_FORCE_PATH_STYLE", "false")
	t.Setenv("SUPADUPA_BACKUP_TARGET_AUTO_TEST", "false")
	store := control.NewMemoryStore()

	if err := bootstrapDefaultBackupStorageTarget(context.Background(), store, discardLogger()); err != nil {
		t.Fatalf("bootstrapDefaultBackupStorageTarget returned error: %v", err)
	}
	targets, err := store.ListBackupStorageTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 0 {
		t.Fatalf("expected setup-compose empty backup target defaults to be ignored, got %#v", targets)
	}
}

func TestBootstrapDefaultBackupStorageTargetCreatesDefaultFromEnv(t *testing.T) {
	t.Setenv("SUPADUPA_BACKUP_S3_NAME", "R2")
	t.Setenv("SUPADUPA_BACKUP_S3_ENDPOINT", "https://account.r2.cloudflarestorage.com")
	t.Setenv("SUPADUPA_BACKUP_S3_REGION", "auto")
	t.Setenv("SUPADUPA_BACKUP_S3_BUCKET", "supadupa-backups")
	t.Setenv("SUPADUPA_BACKUP_S3_PREFIX", "prod")
	t.Setenv("SUPADUPA_BACKUP_S3_ACCESS_KEY_ID", "access-key")
	t.Setenv("SUPADUPA_BACKUP_S3_SECRET_ACCESS_KEY", "secret-key")
	store := control.NewMemoryStore()
	ctx := context.Background()

	if err := bootstrapDefaultBackupStorageTarget(ctx, store, discardLogger()); err != nil {
		t.Fatalf("bootstrapDefaultBackupStorageTarget returned error: %v", err)
	}
	targets, err := store.ListBackupStorageTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || !targets[0].Default || targets[0].Name != "R2" || targets[0].Bucket != "supadupa-backups" || !targets[0].SecretConfigured || targets[0].SecretAccessKey != "" {
		t.Fatalf("expected redacted default target, got %#v", targets)
	}
	fullTarget, err := store.GetBackupStorageTarget(ctx, targets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if fullTarget.SecretAccessKey != "secret-key" || fullTarget.Prefix != "prod" {
		t.Fatalf("expected stored target credentials and prefix, got %#v", fullTarget)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "backup_storage_target.bootstrap_env_create" {
		t.Fatalf("expected one bootstrap create audit event, got %#v", events)
	}

	if err := bootstrapDefaultBackupStorageTarget(ctx, store, discardLogger()); err != nil {
		t.Fatalf("second bootstrapDefaultBackupStorageTarget returned error: %v", err)
	}
	targets, err = store.ListBackupStorageTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected idempotent bootstrap to keep one target, got %#v", targets)
	}
	events, err = store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected idempotent bootstrap to skip audit churn, got %#v", events)
	}
}

func TestBootstrapDefaultBackupStorageTargetAutoTestRecordsFailure(t *testing.T) {
	t.Setenv("SUPADUPA_BACKUP_S3_NAME", "Local test target")
	t.Setenv("SUPADUPA_BACKUP_S3_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("SUPADUPA_BACKUP_S3_REGION", "auto")
	t.Setenv("SUPADUPA_BACKUP_S3_BUCKET", "supadupa-backups")
	t.Setenv("SUPADUPA_BACKUP_S3_ACCESS_KEY_ID", "access-key")
	t.Setenv("SUPADUPA_BACKUP_S3_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("SUPADUPA_BACKUP_S3_FORCE_PATH_STYLE", "true")
	t.Setenv("SUPADUPA_BACKUP_TARGET_AUTO_TEST", "true")
	t.Setenv("SUPADUPA_BACKUP_TARGET_AUTO_TEST_TIMEOUT", "250ms")
	t.Setenv("SUPADUPA_ALLOW_UNSAFE_BACKUP_ENDPOINTS", "true")
	store := control.NewMemoryStore()
	ctx := context.Background()

	if err := bootstrapDefaultBackupStorageTarget(ctx, store, discardLogger()); err != nil {
		t.Fatalf("bootstrapDefaultBackupStorageTarget returned error: %v", err)
	}
	targets, err := store.ListBackupStorageTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected one target, got %#v", targets)
	}
	target, err := store.GetBackupStorageTarget(ctx, targets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if target.LastTestedAt == nil || target.LastTestStatus != "failed" || target.LastTestError == "" {
		t.Fatalf("expected failed startup target test result, got %#v", target)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Action != "backup_storage_target.bootstrap_env_test" || events[0].Metadata["test_status"] != "failed" {
		t.Fatalf("expected bootstrap create and failed test audit events, got %#v", events)
	}
}

func TestBootstrapDefaultBackupStorageTargetUpdatesExistingByName(t *testing.T) {
	t.Setenv("SUPADUPA_BACKUP_TARGET_NAME", "Default backups")
	t.Setenv("SUPADUPA_BACKUP_TARGET_BUCKET", "new-bucket")
	store := control.NewMemoryStore()
	ctx := context.Background()
	target, err := store.CreateBackupStorageTarget(ctx, control.BackupStorageTargetInput{
		Name:            "Default backups",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "old-bucket",
		AccessKeyID:     "existing-access",
		SecretAccessKey: "existing-secret",
		Default:         false,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := bootstrapDefaultBackupStorageTarget(ctx, store, discardLogger()); err != nil {
		t.Fatalf("bootstrapDefaultBackupStorageTarget returned error: %v", err)
	}
	fullTarget, err := store.GetBackupStorageTarget(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fullTarget.Bucket != "new-bucket" || fullTarget.AccessKeyID != "existing-access" || fullTarget.SecretAccessKey != "existing-secret" || !fullTarget.Default {
		t.Fatalf("expected existing target update preserving credentials, got %#v", fullTarget)
	}
}

func TestBootstrapDefaultBackupStorageTargetRequiresCompleteCreateEnv(t *testing.T) {
	t.Setenv("SUPADUPA_BACKUP_TARGET_BUCKET", "supadupa-backups")
	store := control.NewMemoryStore()

	err := bootstrapDefaultBackupStorageTarget(context.Background(), store, discardLogger())
	if err == nil || !strings.Contains(err.Error(), "access key id is required") {
		t.Fatalf("expected incomplete target env error, got %v", err)
	}
}

func enablePlatformDatabaseExternalAccess(ctx context.Context, t *testing.T, store control.Store) {
	t.Helper()
	defaults, err := store.GetPlatformDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	flags := map[string]bool{}
	for k, v := range defaults.FeatureFlags {
		flags[k] = v
	}
	flags[control.DatabaseExternalAccessFlag] = true
	if _, err := store.UpdatePlatformDefaults(ctx, control.PlatformDefaultsInput{
		Domain:                      defaults.Domain,
		StackVersion:                defaults.StackVersion,
		Profile:                     defaults.Profile,
		ResourceTier:                defaults.ResourceTier,
		BackupSchedule:              defaults.BackupSchedule,
		FeatureFlags:                flags,
		DatabaseIngressAllowedCIDRs: defaults.DatabaseIngressAllowedCIDRs,
		SMTP:                        defaults.SMTP,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileExistingProjectRoutesRepairsStaleRouteFile(t *testing.T) {
	ctx := context.Background()
	routesRoot := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", routesRoot)
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "startup-route",
		Name:   "Startup Route",
		Domain: "apps.supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddProjectDomain(ctx, project.Ref, control.ProjectDomainInput{FQDN: "api.example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProjectConfig(ctx, project.Ref, "network", control.ProjectConfigInput{Config: map[string]string{
		"db_allowlist":    "10.0.0.0/8",
		"db_ingress_mode": "allowlisted",
	}}); err != nil {
		t.Fatal(err)
	}
	enablePlatformDatabaseExternalAccess(ctx, t, store)
	if _, err := store.CreateProjectReplica(ctx, project.Ref, control.ProjectReplicaInput{Name: "east", Region: "us-east-1", Tier: control.ResourceTierSmall}); err != nil {
		t.Fatal(err)
	}
	routePath := filepath.Join(routesRoot, project.Ref+".yaml")
	if err := os.WriteFile(routePath, []byte("stale route file without tcp routers\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := reconcileExistingProjectRoutes(ctx, store, nil, discardLogger()); err != nil {
		t.Fatal(err)
	}

	payload, err := os.ReadFile(routePath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, expected := range []string{
		"Host(`startup-route.apps.supadupa.test`)",
		"Host(`storage-startup-route.apps.supadupa.test`)",
		"Host(`api.example.com`)",
		"ipAllowList:",
		"HostSNI(`db-startup-route.apps.supadupa.test`)",
		"HostSNI(`db-replica-east-startup-route.apps.supadupa.test`)",
		`address: "startup-route-db-replica-east:5432"`,
		"HostSNI(`pooler-startup-route.apps.supadupa.test`)",
		"startup-route-postgres-alpn",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected reconciled route file to contain %q:\n%s", expected, body)
		}
	}
	routes, err := store.ListProjectRoutes(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 4 {
		t.Fatalf("expected base API, Studio, Storage, and custom domain routes in store, got %#v", routes)
	}
}

func TestReconcileExistingProjectSecretsSyncsManagedRuntimeKeys(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "startup-secrets",
		Name:   "Startup Secrets",
		Domain: "apps.supadupa.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := store.EnsureProjectSecrets(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	provisioner := &startupSecretProvisioner{}

	if err := reconcileExistingProjectSecrets(ctx, store, provisioner, discardLogger()); err != nil {
		t.Fatal(err)
	}

	if provisioner.syncedRef != project.Ref {
		t.Fatalf("expected sync for %s, got %q", project.Ref, provisioner.syncedRef)
	}
	secretValues := map[string]string{}
	for _, secret := range secrets {
		secretValues[secret.Kind] = secret.Value
	}
	for key, expected := range map[string]string{
		"S3_ACCESS_KEY":                 secretValues["s3_access_key"],
		"S3_SECRET_KEY":                 secretValues["s3_secret_key"],
		"STORAGE_ACCESS_KEY_ID":         secretValues["s3_access_key"],
		"STORAGE_SECRET_ACCESS_KEY":     secretValues["s3_secret_key"],
		"S3_PROTOCOL_ACCESS_KEY_ID":     secretValues["s3_access_key"],
		"S3_PROTOCOL_ACCESS_KEY_SECRET": secretValues["s3_secret_key"],
		"POSTGRES_PASSWORD":             secretValues["db_password"],
		"SUPABASE_PUBLISHABLE_KEY":      secretValues["publishable_key"],
		"SUPABASE_SECRET_KEY":           secretValues["secret_key"],
	} {
		if provisioner.syncedSpec.Environment[key] != expected {
			t.Fatalf("expected %s=%q, got %q in %#v", key, expected, provisioner.syncedSpec.Environment[key], provisioner.syncedSpec.Environment)
		}
	}
	if provisioner.syncedSpec.Environment["S3_PROTOCOL_ACCESS_KEY_ID"] != secretValues["s3_access_key"] ||
		provisioner.syncedSpec.Environment["S3_PROTOCOL_ACCESS_KEY_SECRET"] != secretValues["s3_secret_key"] {
		t.Fatalf("expected S3 protocol keys to match revealed handles, got %#v", provisioner.syncedSpec.Environment)
	}
}

func TestReconcileExistingProjectSecretsMarksSyncFailureDegraded(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, control.CreateProjectRequest{
		OrgID: org.ID,
		Ref:   "startup-secret-error",
		Name:  "Startup Secret Error",
	})
	if err != nil {
		t.Fatal(err)
	}
	provisioner := &startupSecretProvisioner{syncErr: os.ErrNotExist}

	if err := reconcileExistingProjectSecrets(ctx, store, provisioner, discardLogger()); err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetProject(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != control.ProjectDegraded {
		t.Fatalf("expected degraded project, got %#v", updated)
	}
	logs, err := store.ListProjectLogs(ctx, project.Ref, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Message != "Startup secret sync failed" {
		t.Fatalf("expected startup secret sync log, got %#v", logs)
	}
}

func TestReconcileExistingProjectRuntimeReplaysStoredDesiredState(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, control.CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "startup-runtime",
		Name:   "Startup Runtime",
		Domain: "apps.supadupa.test",
		Services: map[string]bool{
			"auth":      true,
			"storage":   true,
			"functions": true,
			"studio":    false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertProjectSecret(ctx, project.Ref, "smtp-password", control.ProjectSecretInput{Value: "smtp-secret-value"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertProjectSecret(ctx, project.Ref, "auth-hook-secret", control.ProjectSecretInput{Value: "hook-secret-value"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertProjectSecret(ctx, project.Ref, "auth-header-secret", control.ProjectSecretInput{Value: "header-secret-value"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProjectConfig(ctx, project.Ref, "smtp", control.ProjectConfigInput{Config: map[string]string{
		"enabled":         "true",
		"host":            "smtp.example.test",
		"password_handle": "secret://projects/startup-runtime/smtp-password",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProjectAuthHook(ctx, project.Ref, control.ProjectAuthHookInput{
		HookType:     "custom_access_token",
		Enabled:      true,
		TargetURI:    "https://startup-runtime.apps.supadupa.test/functions/v1/token-hook",
		SecretHandle: "secret://projects/startup-runtime/auth-hook-secret",
		Headers: map[string]string{
			"authorization": "secret://projects/startup-runtime/auth-header-secret",
			"x-trace":       "startup",
		},
	}); err != nil {
		t.Fatal(err)
	}
	replica, err := store.CreateProjectReplica(ctx, project.Ref, control.ProjectReplicaInput{
		Name:   "east",
		Region: "us-east",
		Tier:   control.ResourceTierSmall,
	})
	if err != nil {
		t.Fatal(err)
	}
	provisioner := &startupSecretProvisioner{}

	if err := reconcileExistingProjectRuntime(ctx, store, provisioner, discardLogger()); err != nil {
		t.Fatal(err)
	}

	if provisioner.syncedServicesRef != project.Ref || !provisioner.syncedServices.Services["storage"].Enabled || provisioner.syncedServices.Services["studio"].Enabled {
		t.Fatalf("expected service desired state replay, got ref=%q spec=%#v", provisioner.syncedServicesRef, provisioner.syncedServices)
	}
	if provisioner.syncedConfigs["smtp"].Config["__resolved_password_handle"] != "smtp-secret-value" {
		t.Fatalf("expected smtp password handle resolved for runtime, got %#v", provisioner.syncedConfigs["smtp"].Config)
	}
	if provisioner.syncedAuthHooksRef != project.Ref || len(provisioner.syncedAuthHooks) != 1 {
		t.Fatalf("expected auth hook replay, got ref=%q hooks=%#v", provisioner.syncedAuthHooksRef, provisioner.syncedAuthHooks)
	}
	hook := provisioner.syncedAuthHooks[0]
	if hook.RuntimeSecret != "hook-secret-value" || hook.RuntimeHeaders["authorization"] != "header-secret-value" || hook.Headers["authorization"] != "secret://projects/startup-runtime/auth-header-secret" {
		t.Fatalf("expected auth hook runtime secrets resolved without replacing handles, got %#v", hook)
	}
	if provisioner.syncedReplicasRef != project.Ref || len(provisioner.syncedReplicas) != 1 || provisioner.syncedReplicas[0].ID != replica.ID {
		t.Fatalf("expected replica replay, got ref=%q replicas=%#v", provisioner.syncedReplicasRef, provisioner.syncedReplicas)
	}
}

type startupSecretProvisioner struct {
	syncedRef          string
	syncedSpec         control.ProjectSpec
	syncedServicesRef  string
	syncedServices     control.ProjectSpec
	syncedConfigs      map[string]control.ProjectConfig
	syncedAuthHooksRef string
	syncedAuthHooks    []control.ProjectAuthHook
	syncedReplicasRef  string
	syncedReplicas     []control.ProjectReplica
	syncErr            error
}

func (p *startupSecretProvisioner) Name() string { return "startup-secret-test" }

func (p *startupSecretProvisioner) Create(ctx context.Context, spec control.ProjectSpec) error {
	return nil
}

func (p *startupSecretProvisioner) SyncSecrets(ctx context.Context, ref string, spec control.ProjectSpec) error {
	p.syncedRef = ref
	p.syncedSpec = spec
	return p.syncErr
}

func (p *startupSecretProvisioner) SyncServices(ctx context.Context, ref string, spec control.ProjectSpec) error {
	p.syncedServicesRef = ref
	p.syncedServices = spec
	return p.syncErr
}

func (p *startupSecretProvisioner) SyncConfig(ctx context.Context, ref string, config control.ProjectConfig) error {
	if p.syncedConfigs == nil {
		p.syncedConfigs = map[string]control.ProjectConfig{}
	}
	p.syncedConfigs[config.Area] = config
	return p.syncErr
}

func (p *startupSecretProvisioner) SyncAuthHooks(ctx context.Context, ref string, hooks []control.ProjectAuthHook) error {
	p.syncedAuthHooksRef = ref
	p.syncedAuthHooks = append([]control.ProjectAuthHook(nil), hooks...)
	return p.syncErr
}

func (p *startupSecretProvisioner) SyncReplicas(ctx context.Context, ref string, replicas []control.ProjectReplica) error {
	p.syncedReplicasRef = ref
	p.syncedReplicas = append([]control.ProjectReplica(nil), replicas...)
	return p.syncErr
}

func (p *startupSecretProvisioner) Destroy(ctx context.Context, ref string) error { return nil }

func (p *startupSecretProvisioner) Status(ctx context.Context, ref string) (control.ProjectStatus, error) {
	return control.ProjectStatus{Ref: ref, Phase: control.ProjectHealthy}, nil
}

func (p *startupSecretProvisioner) Upgrade(ctx context.Context, ref string, version string) error {
	return nil
}

func (p *startupSecretProvisioner) Pause(ctx context.Context, ref string) error { return nil }

func (p *startupSecretProvisioner) Resume(ctx context.Context, ref string) error { return nil }

func (p *startupSecretProvisioner) Scale(ctx context.Context, ref string, spec control.ProjectSpec) error {
	return nil
}

func (p *startupSecretProvisioner) AddReplica(ctx context.Context, ref string, opts control.ReplicaOpts) error {
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
