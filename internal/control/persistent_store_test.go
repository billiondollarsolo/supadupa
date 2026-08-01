package control

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"supadupa2026/internal/metadb"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPersistentStoreRestoresCheckpoint(t *testing.T) {
	db := openCheckpointDB(t)
	ctx := context.Background()
	store, err := NewPersistentStore(ctx, db)
	if err != nil {
		t.Fatalf("new persistent store: %v", err)
	}

	user, err := store.CreateUser(ctx, CreateUserRequest{Email: "admin@example.com", Password: "super-secure", Role: "admin"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	mfaEnrollment, err := store.BeginUserMFAEnrollment(ctx, user.ID)
	if err != nil {
		t.Fatalf("begin mfa enrollment: %v", err)
	}
	mfaNow := time.Now().UTC()
	mfaEnrollCode, err := TOTPCode(mfaEnrollment.Secret, mfaNow.Add(-30*time.Second))
	if err != nil {
		t.Fatalf("mfa enrollment code: %v", err)
	}
	if _, err := store.ConfirmUserMFA(ctx, user.ID, mfaEnrollCode); err != nil {
		t.Fatalf("confirm mfa: %v", err)
	}
	mfaCurrentCode, err := TOTPCode(mfaEnrollment.Secret, mfaNow)
	if err != nil {
		t.Fatalf("mfa current code: %v", err)
	}
	if _, err := store.VerifyUserMFA(ctx, user.ID, mfaCurrentCode); err != nil {
		t.Fatalf("verify mfa current code: %v", err)
	}
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	updatedOrgFlags, err := store.UpdateOrgFeatureFlags(ctx, org.ID, OrgFeatureFlagsInput{Overrides: map[string]bool{"billing": false, "custom_domains": true}})
	if err != nil {
		t.Fatalf("update org feature flags: %v", err)
	}
	host, err := store.CreateHost(ctx, CreateHostRequest{Name: "host-a", Address: "10.0.0.10", Capacity: HostCapacity{CPU: 8, RAMMB: 16384, DiskGB: 160, Project: 3}})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	updatedDefaults, err := store.UpdatePlatformDefaults(ctx, PlatformDefaultsInput{
		Domain:         "apps.example.com",
		StackVersion:   "15.8.1.060",
		Profile:        StackProfileEssential,
		ResourceTier:   ResourceTierCustom,
		BackupSchedule: "hourly",
		FeatureFlags: map[string]bool{
			"single_org_mode": false,
			"read_replicas":   true,
			"billing":         true,
		},
		SMTP: PlatformSMTP{
			Enabled:        true,
			Host:           "smtp.example.com",
			Port:           2525,
			SenderName:     "supadupa",
			SenderEmail:    "noreply@example.com",
			Username:       "apikey",
			PasswordHandle: "secret://platform/smtp-password",
			TLSMode:        "implicit",
		},
	})
	if err != nil {
		t.Fatalf("update platform defaults: %v", err)
	}
	updatedSSO, err := store.UpdatePlatformSSOConfig(ctx, PlatformSSOConfigInput{IDPEntityID: "https://idp.example.com/saml", SSOURL: "https://idp.example.com/login", ACSURL: "https://apps.example.com/v1/auth/sso/saml/callback", EmailDomain: "example.com", AutoProvision: true, DefaultRole: "viewer"})
	if err != nil {
		t.Fatalf("update platform sso: %v", err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "alpha", Name: "Alpha", HostID: host.ID})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	authClient, err := store.CreateProjectAuthClient(ctx, project.Ref, ProjectAuthClientInput{
		Name:               "Partner app",
		ClientID:           "partner-app",
		ClientSecretHandle: "secret://projects/alpha/oauth/partner",
		RedirectURIs:       []string{"https://partner.example.com/callback", "https://partner.example.com/return"},
		GrantTypes:         []string{"authorization_code", "refresh_token"},
		Scopes:             []string{"openid", "email"},
		Confidential:       true,
	})
	if err != nil {
		t.Fatalf("create auth client: %v", err)
	}
	authHook, err := store.CreateProjectAuthHook(ctx, project.Ref, ProjectAuthHookInput{
		HookType:     "custom_access_token",
		Enabled:      true,
		TargetURI:    "https://hooks.example.com/token",
		SecretHandle: "secret://projects/alpha/auth-hook-signing",
		Headers: map[string]string{
			"authorization": "secret://projects/alpha/auth-hook-header",
			"x-trace":       "restore-test",
		},
		TimeoutMS:     7000,
		RetryAttempts: 3,
	})
	if err != nil {
		t.Fatalf("create auth hook: %v", err)
	}
	databaseWebhook, err := store.CreateProjectDatabaseWebhook(ctx, project.Ref, ProjectDatabaseWebhookInput{
		Name:       "orders-outbox",
		Schema:     "public",
		Table:      "orders",
		Events:     []string{"insert", "update"},
		Endpoint:   "https://webhooks.example.com/orders",
		HTTPMethod: "PATCH",
		Headers: map[string]string{
			"authorization": "secret://projects/alpha/db-webhook-auth",
			"x-source":      "postgres",
		},
		TimeoutSeconds: 42,
		RetryCount:     7,
		Active:         true,
		Metadata: map[string]string{
			"owner":             "data-platform",
			"credential_handle": "secret://projects/alpha/db-webhook-metadata",
		},
	})
	if err != nil {
		t.Fatalf("create database webhook: %v", err)
	}
	networkConnection, err := store.CreateProjectNetworkConnection(ctx, project.Ref, ProjectNetworkConnectionInput{
		Name:       "analytics-vpc",
		Type:       "privatelink",
		Provider:   "aws",
		Region:     "us-east-1",
		CIDRs:      []string{"10.10.0.0/16", "10.20.0.0/16"},
		EndpointID: "vpce-12345",
		Config: map[string]string{
			"account_id":        "123456789012",
			"credential_handle": "secret://projects/alpha/network/aws",
		},
	})
	if err != nil {
		t.Fatalf("create network connection: %v", err)
	}
	additionalChildFields := seedAdditionalProjectChildFieldFixtures(t, ctx, store, project.Ref)
	team, err := store.CreateOrgTeam(ctx, org.ID, TeamInput{Name: "Developers", Slug: "developers"})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := store.UpsertTeamMember(ctx, org.ID, team.Slug, TeamMemberInput{Email: "admin@example.com"}); err != nil {
		t.Fatalf("upsert team member: %v", err)
	}
	if _, err := store.UpsertProjectAccess(ctx, project.Ref, ProjectAccessInput{SubjectType: "team", SubjectID: team.Slug, Role: "admin"}); err != nil {
		t.Fatalf("upsert project access: %v", err)
	}
	secrets, err := store.ListProjectSecrets(ctx, project.Ref)
	if err != nil {
		t.Fatalf("list project secrets: %v", err)
	}
	if len(secrets) == 0 {
		t.Fatalf("expected project secrets")
	}
	payload := checkpointPayload(t)
	if !bytes.HasPrefix(payload, []byte(encryptedPayloadPrefix)) {
		previewLength := len(encryptedPayloadPrefix)
		if len(payload) < previewLength {
			previewLength = len(payload)
		}
		t.Fatalf("expected encrypted checkpoint prefix, got %q", payload[:previewLength])
	}
	for _, secret := range secrets {
		if bytes.Contains(payload, []byte(secret.Value)) {
			t.Fatalf("checkpoint payload contains plaintext secret %s", secret.Kind)
		}
	}

	restored, err := NewPersistentStore(ctx, db)
	if err != nil {
		t.Fatalf("restore persistent store: %v", err)
	}
	authenticated, err := restored.AuthenticateUser(ctx, "admin@example.com", "super-secure")
	if err != nil {
		t.Fatalf("authenticate restored user: %v", err)
	}
	if authenticated.ID != user.ID {
		t.Fatalf("expected restored user %s, got %s", user.ID, authenticated.ID)
	}
	if _, err := restored.VerifyUserMFA(ctx, user.ID, mfaCurrentCode); err == nil || !strings.Contains(err.Error(), "already been used") {
		t.Fatalf("expected restored store to reject replayed mfa code, got %v", err)
	}
	mfaNextCode, err := TOTPCode(mfaEnrollment.Secret, mfaNow.Add(30*time.Second))
	if err != nil {
		t.Fatalf("mfa next code: %v", err)
	}
	if _, err := restored.VerifyUserMFA(ctx, user.ID, mfaNextCode); err != nil {
		t.Fatalf("expected restored store to accept next mfa code: %v", err)
	}
	orgs, err := restored.ListOrgs(ctx)
	if err != nil {
		t.Fatalf("list restored orgs: %v", err)
	}
	if len(orgs) != 1 || orgs[0].ID != org.ID {
		t.Fatalf("expected restored org %#v, got %#v", org, orgs)
	}
	billingOverride, hasBillingOverride := orgs[0].FeatureFlagOverrides["billing"]
	if orgs[0].FeatureFlags["custom_domains"] != true || !hasBillingOverride || billingOverride != false {
		t.Fatalf("expected restored org feature flags %#v, got %#v", updatedOrgFlags, orgs[0])
	}
	restoredOrgFlags, err := restored.GetOrgFeatureFlags(ctx, org.ID)
	if err != nil {
		t.Fatalf("get restored org feature flags: %v", err)
	}
	restoredBillingOverride, hasRestoredBillingOverride := restoredOrgFlags.Overrides["billing"]
	if restoredOrgFlags.Effective["custom_domains"] != true || !hasRestoredBillingOverride || restoredBillingOverride != false {
		t.Fatalf("expected restored org feature override %#v, got %#v", updatedOrgFlags, restoredOrgFlags)
	}
	projects, err := restored.ListProjectsByOrg(ctx, org.ID)
	if err != nil {
		t.Fatalf("list restored projects: %v", err)
	}
	if len(projects) != 1 || projects[0].Ref != project.Ref {
		t.Fatalf("expected restored project %#v, got %#v", project, projects)
	}
	defaults, err := restored.GetPlatformDefaults(ctx)
	if err != nil {
		t.Fatalf("get restored platform defaults: %v", err)
	}
	if defaults.Domain != updatedDefaults.Domain || defaults.StackVersion != updatedDefaults.StackVersion || defaults.Profile != updatedDefaults.Profile || defaults.ResourceTier != updatedDefaults.ResourceTier || defaults.BackupSchedule != updatedDefaults.BackupSchedule || defaults.SMTP != updatedDefaults.SMTP || defaults.FeatureFlags["single_org_mode"] || !defaults.FeatureFlags["read_replicas"] || !defaults.FeatureFlags["billing"] {
		t.Fatalf("expected restored defaults %#v, got %#v", updatedDefaults, defaults)
	}
	sso, err := restored.GetPlatformSSOConfig(ctx)
	if err != nil {
		t.Fatalf("get restored platform sso: %v", err)
	}
	if sso.IDPEntityID != updatedSSO.IDPEntityID || sso.SSOURL != updatedSSO.SSOURL || sso.ACSURL != updatedSSO.ACSURL || sso.EmailDomain != updatedSSO.EmailDomain || sso.AutoProvision != updatedSSO.AutoProvision || sso.DefaultRole != updatedSSO.DefaultRole {
		t.Fatalf("expected restored sso %#v, got %#v", updatedSSO, sso)
	}
	if projects[0].Spec.Domain != updatedDefaults.Domain || projects[0].Spec.ResourceTier != ResourceTierCustom || projects[0].Spec.CPU <= 0 || projects[0].Spec.RAMMB <= 0 || projects[0].Spec.DiskGB <= 0 {
		t.Fatalf("expected restored project to use defaults, got %#v", projects[0].Spec)
	}
	assertRestoredProjectChildFields(t, ctx, restored, project.Ref, authClient, authHook, databaseWebhook, networkConnection, additionalChildFields)
	teams, err := restored.ListOrgTeams(ctx, org.ID)
	if err != nil {
		t.Fatalf("list restored teams: %v", err)
	}
	if _, ok := findTeam(teams, team.Slug); !ok {
		t.Fatalf("expected restored team %#v, got %#v", team, teams)
	}
	role, err := restored.ResolveProjectRole(ctx, project.Ref, "admin@example.com")
	if err != nil {
		t.Fatalf("resolve restored project role: %v", err)
	}
	if role != "admin" {
		t.Fatalf("expected restored admin project role, got %q", role)
	}
	secrets, err = restored.ListProjectSecrets(ctx, project.Ref)
	if err != nil {
		t.Fatalf("list restored secrets: %v", err)
	}
	if len(secrets) == 0 {
		t.Fatalf("expected restored project secrets")
	}
}

func TestPersistentStoreRestoresProjectChildFieldsFromNormalizedTables(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("SUPADUPA_TEST_META_DSN"))
	if dsn == "" {
		t.Skip("set SUPADUPA_TEST_META_DSN to a disposable Postgres database to run normalized persistence restore coverage")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := metadb.LoadMigrations(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := metadb.Apply(ctx, db, migrations); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	store, err := NewPersistentStore(ctx, db)
	if err != nil {
		t.Fatalf("new persistent store: %v", err)
	}
	org, err := store.CreateOrg(ctx, "Normalized "+strings.ReplaceAll(t.Name(), "/", "-"))
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	ref := "norm-" + strings.ToLower(newID()[:12])
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: ref, Name: "Normalized"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	authClient, err := store.CreateProjectAuthClient(ctx, project.Ref, ProjectAuthClientInput{
		Name:               "Normalized app",
		ClientID:           "normalized-app",
		ClientSecretHandle: "secret://projects/" + project.Ref + "/oauth/client",
		RedirectURIs:       []string{"https://normalized.example.com/callback", "https://normalized.example.com/return"},
		GrantTypes:         []string{"authorization_code", "refresh_token"},
		Scopes:             []string{"openid", "email"},
		Confidential:       true,
	})
	if err != nil {
		t.Fatalf("create auth client: %v", err)
	}
	authHook, err := store.CreateProjectAuthHook(ctx, project.Ref, ProjectAuthHookInput{
		HookType:     "custom_access_token",
		Enabled:      true,
		TargetURI:    "https://normalized.example.com/auth-hook",
		SecretHandle: "secret://projects/" + project.Ref + "/auth-hook-secret",
		Headers: map[string]string{
			"authorization": "secret://projects/" + project.Ref + "/auth-hook-header",
			"x-trace":       "normalized",
		},
		TimeoutMS:     9000,
		RetryAttempts: 2,
	})
	if err != nil {
		t.Fatalf("create auth hook: %v", err)
	}
	databaseWebhook, err := store.CreateProjectDatabaseWebhook(ctx, project.Ref, ProjectDatabaseWebhookInput{
		Name:       "normalized-outbox",
		Schema:     "public",
		Table:      "orders",
		Events:     []string{"insert", "delete"},
		Endpoint:   "https://normalized.example.com/db-webhook",
		HTTPMethod: "PUT",
		Headers: map[string]string{
			"authorization": "secret://projects/" + project.Ref + "/db-webhook-auth",
			"x-source":      "postgres",
		},
		TimeoutSeconds: 31,
		RetryCount:     6,
		Active:         true,
		Metadata: map[string]string{
			"owner":             "normalized-test",
			"credential_handle": "secret://projects/" + project.Ref + "/db-webhook-metadata",
		},
	})
	if err != nil {
		t.Fatalf("create database webhook: %v", err)
	}
	networkConnection, err := store.CreateProjectNetworkConnection(ctx, project.Ref, ProjectNetworkConnectionInput{
		Name:       "normalized-vpc",
		Type:       "privatelink",
		Provider:   "aws",
		Region:     "us-west-2",
		CIDRs:      []string{"10.30.0.0/16", "10.40.0.0/16"},
		EndpointID: "vpce-normalized",
		Config: map[string]string{
			"account_id":        "210987654321",
			"credential_handle": "secret://projects/" + project.Ref + "/network/aws",
		},
	})
	if err != nil {
		t.Fatalf("create network connection: %v", err)
	}
	additionalChildFields := seedAdditionalProjectChildFieldFixtures(t, ctx, store, project.Ref)

	if _, err := db.ExecContext(ctx, `DELETE FROM control_state_checkpoints WHERE id = $1`, controlStateCheckpointID); err != nil {
		t.Fatalf("delete checkpoint row: %v", err)
	}
	restored, err := NewPersistentStore(ctx, db)
	if err != nil {
		t.Fatalf("restore from normalized tables: %v", err)
	}

	assertRestoredProjectChildFields(t, ctx, restored, project.Ref, authClient, authHook, databaseWebhook, networkConnection, additionalChildFields)
}

type additionalProjectChildFieldFixtures struct {
	replicationPipeline ProjectReplicationPipeline
	embeddingJob        ProjectEmbeddingJob
	databaseExtension   ProjectDatabaseExtension
	databaseCronJob     ProjectDatabaseCronJob
	databaseQueue       ProjectDatabaseQueue
	databaseSchema      ProjectDatabaseSchema
	databaseRole        ProjectDatabaseRole
	projectFunction     ProjectFunction
	functionRegion      ProjectFunctionRegion
	functionMount       ProjectFunctionStorageMount
	storageBucket       ProjectStorageBucket
	vectorBucket        ProjectVectorBucket
	analyticsBucket     ProjectAnalyticsBucket
	projectConfig       ProjectConfig
	projectDomain       ProjectDomain
	projectAccess       ProjectAccessGrant
	backupPolicy        BackupPolicy
	cdnPolicy           ProjectCDNPolicy
	pitrPolicy          PITRPolicy
	logDrain            LogDrain
}

func seedAdditionalProjectChildFieldFixtures(t *testing.T, ctx context.Context, store *PersistentStore, ref string) additionalProjectChildFieldFixtures {
	t.Helper()
	project, err := store.GetProject(ctx, ref)
	if err != nil {
		t.Fatalf("get project for child fixtures: %v", err)
	}
	accessEmail := "access-" + ref + "@example.com"
	if _, err := store.CreateUser(ctx, CreateUserRequest{Email: accessEmail, Password: "super-secure", Role: "member"}); err != nil {
		t.Fatalf("create access fixture user: %v", err)
	}
	if _, err := store.UpsertOrgMember(ctx, project.OrgID, MembershipInput{Email: accessEmail, Role: "viewer"}); err != nil {
		t.Fatalf("upsert access fixture org member: %v", err)
	}
	accessTeamSlug := "access-" + ref
	accessTeam, err := store.CreateOrgTeam(ctx, project.OrgID, TeamInput{Name: "Access " + ref, Slug: accessTeamSlug})
	if err != nil {
		t.Fatalf("create access fixture team: %v", err)
	}
	if _, err := store.UpsertTeamMember(ctx, project.OrgID, accessTeam.Slug, TeamMemberInput{Email: accessEmail}); err != nil {
		t.Fatalf("upsert access fixture team member: %v", err)
	}
	projectAccess, err := store.UpsertProjectAccess(ctx, ref, ProjectAccessInput{SubjectType: "team", SubjectID: accessTeam.Slug, Role: "developer"})
	if err != nil {
		t.Fatalf("upsert project access fixture: %v", err)
	}
	projectConfig, err := store.UpdateProjectConfig(ctx, ref, "network", ProjectConfigInput{Config: map[string]string{
		"ip_allowlist": "10.42.0.0/16, 192.0.2.10",
		"ssl_enforced": "false",
	}})
	if err != nil {
		t.Fatalf("update project config fixture: %v", err)
	}
	projectDomain, err := store.AddProjectDomain(ctx, ref, ProjectDomainInput{FQDN: "restore-" + ref + ".example.net"})
	if err != nil {
		t.Fatalf("add project domain fixture: %v", err)
	}
	notAfter := time.Date(2031, time.January, 2, 3, 4, 5, 0, time.UTC)
	projectDomain, err = store.UpdateProjectDomainCertificate(ctx, ref, projectDomain.FQDN, ProjectDomainCertificateMetadata{
		Status:      "uploaded",
		Mode:        "byo",
		Fingerprint: "sha256:restore-" + ref,
		NotAfter:    &notAfter,
	})
	if err != nil {
		t.Fatalf("update project domain certificate fixture: %v", err)
	}
	replicationPipeline, err := store.CreateProjectReplicationPipeline(ctx, ref, ProjectReplicationPipelineInput{
		Name:             "orders-etl",
		Type:             "etl",
		SourceSchema:     "public",
		SourceTable:      "orders",
		Destination:      "s3",
		DestinationURI:   "s3://lake/orders",
		CredentialHandle: "secret://projects/" + ref + "/replication/etl",
		Config: map[string]string{
			"bucket":     "lake",
			"access_key": "secret://projects/" + ref + "/replication/s3-access",
		},
	})
	if err != nil {
		t.Fatalf("create replication pipeline: %v", err)
	}
	embeddingJob, err := store.CreateProjectEmbeddingJob(ctx, ref, ProjectEmbeddingJobInput{
		Name:              "orders-embeddings",
		SourceSchema:      "public",
		SourceTable:       "orders",
		SourceColumn:      "description",
		PrimaryKeyColumn:  "id",
		DestinationTable:  "order_embeddings",
		DestinationColumn: "embedding",
		Provider:          "huggingface",
		Model:             "sentence-transformers/all-minilm-l6-v2",
		Dimension:         384,
		Schedule:          "*/15 * * * *",
		BatchSize:         250,
	})
	if err != nil {
		t.Fatalf("create embedding job: %v", err)
	}
	extensionEnabled := false
	databaseExtension, err := store.UpdateProjectDatabaseExtension(ctx, ref, "vector", ProjectDatabaseExtensionInput{
		Schema:  "extensions",
		Version: "0.7.4",
		Enabled: &extensionEnabled,
	})
	if err != nil {
		t.Fatalf("update database extension: %v", err)
	}
	databaseCronJob, err := store.CreateProjectDatabaseCronJob(ctx, ref, ProjectDatabaseCronJobInput{
		Name:              "nightly-rollup",
		Schedule:          "0 2 * * *",
		Command:           "select refresh_rollups();",
		Database:          "postgres",
		Username:          "postgres",
		Active:            true,
		TimeoutSeconds:    123,
		MaxRuntimeSeconds: 456,
		Metadata: map[string]string{
			"owner": "analytics",
			"token": "secret://projects/" + ref + "/cron/token",
		},
	})
	if err != nil {
		t.Fatalf("create database cron job: %v", err)
	}
	databaseQueue, err := store.CreateProjectDatabaseQueue(ctx, ref, ProjectDatabaseQueueInput{
		Name:                     "jobs",
		Schema:                   "pgmq",
		RetentionMinutes:         2880,
		VisibilityTimeoutSeconds: 45,
		MaxRetries:               9,
		DeadLetterQueue:          "dead-jobs",
		Active:                   true,
		Metadata: map[string]string{
			"owner": "backend",
			"token": "secret://projects/" + ref + "/queue/token",
		},
	})
	if err != nil {
		t.Fatalf("create database queue: %v", err)
	}
	databaseSchema, err := store.CreateProjectDatabaseSchema(ctx, ref, ProjectDatabaseSchemaInput{
		Name:       "analytics-ddl",
		Version:    "2026.06.08",
		Schema:     "analytics",
		SQL:        "create table analytics.events (id bigint primary key, payload jsonb);",
		ApplyOrder: 42,
		Active:     true,
		Metadata: map[string]string{
			"owner": "analytics",
			"token": "secret://projects/" + ref + "/schema/token",
		},
	})
	if err != nil {
		t.Fatalf("create database schema: %v", err)
	}
	inherit := false
	databaseRole, err := store.CreateProjectDatabaseRole(ctx, ref, ProjectDatabaseRoleInput{
		Name:                 "app_writer",
		Login:                true,
		Inherit:              &inherit,
		BypassRLS:            true,
		ConnectionLimit:      25,
		PasswordSecretHandle: "secret://projects/" + ref + "/database/app-writer-password",
		MemberOf:             []string{"authenticated"},
		SchemaGrants:         map[string]string{"public": "usage,select,insert"},
		Metadata: map[string]string{
			"owner":   "app",
			"api_key": "secret://projects/" + ref + "/database/app-writer-api-key",
		},
	})
	if err != nil {
		t.Fatalf("create database role: %v", err)
	}
	storageBucket, err := store.CreateProjectStorageBucket(ctx, ref, ProjectStorageBucketInput{
		Name:              "assets",
		Public:            true,
		FileSizeLimit:     1048576,
		AllowedMimeTypes:  []string{"image/png", "image/jpeg"},
		CacheControl:      "public, max-age=3600",
		AvifAutodetection: true,
		Metadata: map[string]string{
			"owner": "web",
			"token": "secret://projects/" + ref + "/storage/token",
		},
	})
	if err != nil {
		t.Fatalf("create storage bucket: %v", err)
	}
	projectFunction, err := store.DeployProjectFunction(ctx, ref, ProjectFunctionInput{
		Name:       "orders-hook",
		Entrypoint: "functions/orders/index.ts",
		VerifyJWT:  true,
		Source:     "Deno.serve(() => new Response('ok'))",
		Secrets: map[string]string{
			"API_TOKEN": "secret://projects/" + ref + "/functions/orders-api-token",
		},
	})
	if err != nil {
		t.Fatalf("deploy project function: %v", err)
	}
	functionRegion, err := store.CreateProjectFunctionRegion(ctx, ref, ProjectFunctionRegionInput{
		FunctionName:  projectFunction.Name,
		Region:        "us-east-1",
		RoutingPolicy: "weighted",
	})
	if err != nil {
		t.Fatalf("create function region: %v", err)
	}
	functionMount, err := store.CreateProjectFunctionStorageMount(ctx, ref, ProjectFunctionStorageMountInput{
		FunctionName: projectFunction.Name,
		BucketName:   storageBucket.Name,
		MountPath:    "/mnt/assets/uploads",
		ReadOnly:     true,
		Prefix:       "public/uploads",
		EnvAlias:     "ORDERS_ASSETS_MOUNT",
	})
	if err != nil {
		t.Fatalf("create function storage mount: %v", err)
	}
	vectorBucket, err := store.CreateProjectVectorBucket(ctx, ref, ProjectVectorBucketInput{
		Name:           "documents",
		Dimension:      768,
		Distance:       "l2",
		IndexMethod:    "ivfflat",
		StorageBackend: "s3",
		StorageURI:     "s3://vectors/documents",
		Metadata: map[string]string{
			"owner":      "search",
			"access_key": "secret://projects/" + ref + "/vectors/s3-access",
		},
	})
	if err != nil {
		t.Fatalf("create vector bucket: %v", err)
	}
	analyticsBucket, err := store.CreateProjectAnalyticsBucket(ctx, ref, ProjectAnalyticsBucketInput{
		Name:               "events",
		StorageURI:         "s3://analytics/events",
		CatalogURI:         "https://iceberg.example.com/catalog",
		Warehouse:          "analytics",
		CredentialHandle:   "secret://projects/" + ref + "/analytics/iceberg",
		FormatVersion:      2,
		Partitioning:       "days(created_at)",
		RetentionDays:      365,
		CompactionSchedule: "0 3 * * *",
		Metadata: map[string]string{
			"owner":      "analytics",
			"access_key": "secret://projects/" + ref + "/analytics/s3-access",
		},
	})
	if err != nil {
		t.Fatalf("create analytics bucket: %v", err)
	}
	backupTarget, err := store.CreateBackupStorageTarget(ctx, BackupStorageTargetInput{
		Name:            "restore-target-" + ref,
		Type:            "s3",
		Endpoint:        "https://s3.example.com",
		Region:          "us-east-1",
		Bucket:          "supadupa-backups-" + ref,
		Prefix:          "projects/" + ref,
		AccessKeyID:     "access-key-" + ref,
		SecretAccessKey: "secret-key-" + ref,
		Default:         false,
	})
	if err != nil {
		t.Fatalf("create backup storage target: %v", err)
	}
	backupPolicy, err := store.UpdateBackupPolicy(ctx, ref, BackupPolicyInput{
		Enabled:         true,
		Schedule:        "hourly",
		Kind:            "physical",
		StorageTargetID: backupTarget.ID,
	})
	if err != nil {
		t.Fatalf("update backup policy: %v", err)
	}
	cdnPolicy, err := store.UpdateProjectCDNPolicy(ctx, ref, ProjectCDNPolicyInput{
		Enabled:                     true,
		BrowserTTLSeconds:           120,
		EdgeTTLSeconds:              600,
		StaleWhileRevalidateSeconds: 30,
		IncludedPaths:               []string{"/storage/v1/object/public/assets/*", "/rest/v1/public_cache"},
		ExcludedPaths:               []string{"/storage/v1/object/public/private/*"},
		SmartRevalidation:           true,
		CacheControl:                "public, max-age=120, s-maxage=600, stale-while-revalidate=30",
	})
	if err != nil {
		t.Fatalf("update cdn policy: %v", err)
	}
	pitrPolicy, err := store.UpdatePITRPolicy(ctx, ref, PITRPolicyInput{
		Enabled:       true,
		ArchiveBucket: "s3://wal-archive/" + ref,
		RetentionDays: 21,
	})
	if err != nil {
		t.Fatalf("update pitr policy: %v", err)
	}
	logDrain, err := store.CreateProjectLogDrain(ctx, ref, LogDrainInput{Target: "https", Config: map[string]string{"url": "https://logs.example.com/ingest", "token": "first-token"}})
	if err != nil {
		t.Fatalf("create log drain: %v", err)
	}
	updatedLogDrain, err := store.UpdateProjectLogDrain(ctx, ref, logDrain.ID, LogDrainInput{Target: "loki", Config: map[string]string{"url": "https://loki.example.com/api/v1/push"}})
	if err != nil {
		t.Fatalf("update log drain: %v", err)
	}
	return additionalProjectChildFieldFixtures{
		replicationPipeline: replicationPipeline,
		embeddingJob:        embeddingJob,
		databaseExtension:   databaseExtension,
		databaseCronJob:     databaseCronJob,
		databaseQueue:       databaseQueue,
		databaseSchema:      databaseSchema,
		databaseRole:        databaseRole,
		projectFunction:     projectFunction,
		functionRegion:      functionRegion,
		functionMount:       functionMount,
		storageBucket:       storageBucket,
		vectorBucket:        vectorBucket,
		analyticsBucket:     analyticsBucket,
		projectConfig:       projectConfig,
		projectDomain:       projectDomain,
		projectAccess:       projectAccess,
		backupPolicy:        backupPolicy,
		cdnPolicy:           cdnPolicy,
		pitrPolicy:          pitrPolicy,
		logDrain:            updatedLogDrain,
	}
}

func assertRestoredProjectChildFields(t *testing.T, ctx context.Context, store Store, ref string, authClient ProjectAuthClient, authHook ProjectAuthHook, databaseWebhook ProjectDatabaseWebhook, networkConnection ProjectNetworkConnection, additional additionalProjectChildFieldFixtures) {
	t.Helper()
	authClients, err := store.ListProjectAuthClients(ctx, ref)
	if err != nil {
		t.Fatalf("list restored auth clients: %v", err)
	}
	if len(authClients) != 1 ||
		authClients[0].ID != authClient.ID ||
		authClients[0].ClientSecretHandle != authClient.ClientSecretHandle ||
		authClients[0].Confidential != authClient.Confidential ||
		!sameStrings(authClients[0].RedirectURIs, authClient.RedirectURIs) ||
		!sameStrings(authClients[0].GrantTypes, authClient.GrantTypes) ||
		!sameStrings(authClients[0].Scopes, authClient.Scopes) {
		t.Fatalf("expected restored auth client %#v, got %#v", authClient, authClients)
	}
	authHooks, err := store.ListProjectAuthHooks(ctx, ref)
	if err != nil {
		t.Fatalf("list restored auth hooks: %v", err)
	}
	if len(authHooks) != 1 ||
		authHooks[0].ID != authHook.ID ||
		authHooks[0].Enabled != authHook.Enabled ||
		authHooks[0].SecretHandle != authHook.SecretHandle ||
		authHooks[0].Headers["authorization"] != authHook.Headers["authorization"] ||
		authHooks[0].Headers["x-trace"] != authHook.Headers["x-trace"] ||
		authHooks[0].TimeoutMS != authHook.TimeoutMS ||
		authHooks[0].RetryAttempts != authHook.RetryAttempts {
		t.Fatalf("expected restored auth hook %#v, got %#v", authHook, authHooks)
	}
	databaseWebhooks, err := store.ListProjectDatabaseWebhooks(ctx, ref)
	if err != nil {
		t.Fatalf("list restored database webhooks: %v", err)
	}
	if len(databaseWebhooks) != 1 ||
		databaseWebhooks[0].ID != databaseWebhook.ID ||
		databaseWebhooks[0].HTTPMethod != databaseWebhook.HTTPMethod ||
		!sameStrings(databaseWebhooks[0].Events, databaseWebhook.Events) ||
		databaseWebhooks[0].Headers["authorization"] != databaseWebhook.Headers["authorization"] ||
		databaseWebhooks[0].Headers["x-source"] != databaseWebhook.Headers["x-source"] ||
		databaseWebhooks[0].Metadata["credential_handle"] != databaseWebhook.Metadata["credential_handle"] ||
		databaseWebhooks[0].Metadata["owner"] != databaseWebhook.Metadata["owner"] ||
		databaseWebhooks[0].TimeoutSeconds != databaseWebhook.TimeoutSeconds ||
		databaseWebhooks[0].RetryCount != databaseWebhook.RetryCount ||
		databaseWebhooks[0].Active != databaseWebhook.Active {
		t.Fatalf("expected restored database webhook %#v, got %#v", databaseWebhook, databaseWebhooks)
	}
	networkConnections, err := store.ListProjectNetworkConnections(ctx, ref)
	if err != nil {
		t.Fatalf("list restored network connections: %v", err)
	}
	if len(networkConnections) != 1 ||
		networkConnections[0].ID != networkConnection.ID ||
		networkConnections[0].Type != networkConnection.Type ||
		networkConnections[0].Provider != networkConnection.Provider ||
		networkConnections[0].Region != networkConnection.Region ||
		networkConnections[0].EndpointID != networkConnection.EndpointID ||
		!sameStrings(networkConnections[0].CIDRs, networkConnection.CIDRs) ||
		networkConnections[0].Config["credential_handle"] != networkConnection.Config["credential_handle"] ||
		networkConnections[0].Config["account_id"] != networkConnection.Config["account_id"] {
		t.Fatalf("expected restored network connection %#v, got %#v", networkConnection, networkConnections)
	}
	replicationPipelines, err := store.ListProjectReplicationPipelines(ctx, ref)
	if err != nil {
		t.Fatalf("list restored replication pipelines: %v", err)
	}
	if len(replicationPipelines) != 1 ||
		replicationPipelines[0].ID != additional.replicationPipeline.ID ||
		replicationPipelines[0].CredentialHandle != additional.replicationPipeline.CredentialHandle ||
		replicationPipelines[0].DestinationURI != additional.replicationPipeline.DestinationURI ||
		replicationPipelines[0].Config["bucket"] != additional.replicationPipeline.Config["bucket"] ||
		replicationPipelines[0].Config["access_key"] != additional.replicationPipeline.Config["access_key"] {
		t.Fatalf("expected restored replication pipeline %#v, got %#v", additional.replicationPipeline, replicationPipelines)
	}
	embeddingJobs, err := store.ListProjectEmbeddingJobs(ctx, ref)
	if err != nil {
		t.Fatalf("list restored embedding jobs: %v", err)
	}
	if len(embeddingJobs) != 1 ||
		embeddingJobs[0].ID != additional.embeddingJob.ID ||
		embeddingJobs[0].Provider != additional.embeddingJob.Provider ||
		embeddingJobs[0].Model != additional.embeddingJob.Model ||
		embeddingJobs[0].Dimension != additional.embeddingJob.Dimension ||
		embeddingJobs[0].Schedule != additional.embeddingJob.Schedule ||
		embeddingJobs[0].BatchSize != additional.embeddingJob.BatchSize ||
		embeddingJobs[0].DestinationTable != additional.embeddingJob.DestinationTable ||
		embeddingJobs[0].DestinationColumn != additional.embeddingJob.DestinationColumn {
		t.Fatalf("expected restored embedding job %#v, got %#v", additional.embeddingJob, embeddingJobs)
	}
	databaseExtensions, err := store.ListProjectDatabaseExtensions(ctx, ref)
	if err != nil {
		t.Fatalf("list restored database extensions: %v", err)
	}
	restoredExtension, ok := findDatabaseExtension(databaseExtensions, additional.databaseExtension.Name)
	if !ok ||
		restoredExtension.ID != additional.databaseExtension.ID ||
		restoredExtension.Schema != additional.databaseExtension.Schema ||
		restoredExtension.Version != additional.databaseExtension.Version ||
		restoredExtension.Enabled != additional.databaseExtension.Enabled ||
		restoredExtension.Status != additional.databaseExtension.Status {
		t.Fatalf("expected restored database extension %#v, got %#v", additional.databaseExtension, databaseExtensions)
	}
	databaseCronJobs, err := store.ListProjectDatabaseCronJobs(ctx, ref)
	if err != nil {
		t.Fatalf("list restored database cron jobs: %v", err)
	}
	if len(databaseCronJobs) != 1 ||
		databaseCronJobs[0].ID != additional.databaseCronJob.ID ||
		databaseCronJobs[0].TimeoutSeconds != additional.databaseCronJob.TimeoutSeconds ||
		databaseCronJobs[0].MaxRuntimeSeconds != additional.databaseCronJob.MaxRuntimeSeconds ||
		databaseCronJobs[0].Metadata["owner"] != additional.databaseCronJob.Metadata["owner"] ||
		databaseCronJobs[0].Metadata["token"] != additional.databaseCronJob.Metadata["token"] {
		t.Fatalf("expected restored database cron job %#v, got %#v", additional.databaseCronJob, databaseCronJobs)
	}
	databaseQueues, err := store.ListProjectDatabaseQueues(ctx, ref)
	if err != nil {
		t.Fatalf("list restored database queues: %v", err)
	}
	if len(databaseQueues) != 1 ||
		databaseQueues[0].ID != additional.databaseQueue.ID ||
		databaseQueues[0].RetentionMinutes != additional.databaseQueue.RetentionMinutes ||
		databaseQueues[0].VisibilityTimeoutSeconds != additional.databaseQueue.VisibilityTimeoutSeconds ||
		databaseQueues[0].MaxRetries != additional.databaseQueue.MaxRetries ||
		databaseQueues[0].DeadLetterQueue != additional.databaseQueue.DeadLetterQueue ||
		databaseQueues[0].Metadata["owner"] != additional.databaseQueue.Metadata["owner"] ||
		databaseQueues[0].Metadata["token"] != additional.databaseQueue.Metadata["token"] {
		t.Fatalf("expected restored database queue %#v, got %#v", additional.databaseQueue, databaseQueues)
	}
	databaseSchemas, err := store.ListProjectDatabaseSchemas(ctx, ref)
	if err != nil {
		t.Fatalf("list restored database schemas: %v", err)
	}
	if len(databaseSchemas) != 1 ||
		databaseSchemas[0].ID != additional.databaseSchema.ID ||
		databaseSchemas[0].Version != additional.databaseSchema.Version ||
		databaseSchemas[0].Schema != additional.databaseSchema.Schema ||
		databaseSchemas[0].SQL != additional.databaseSchema.SQL ||
		databaseSchemas[0].Checksum != additional.databaseSchema.Checksum ||
		databaseSchemas[0].ApplyOrder != additional.databaseSchema.ApplyOrder ||
		databaseSchemas[0].Active != additional.databaseSchema.Active ||
		databaseSchemas[0].Metadata["owner"] != additional.databaseSchema.Metadata["owner"] ||
		databaseSchemas[0].Metadata["token"] != additional.databaseSchema.Metadata["token"] {
		t.Fatalf("expected restored database schema %#v, got %#v", additional.databaseSchema, databaseSchemas)
	}
	databaseRoles, err := store.ListProjectDatabaseRoles(ctx, ref)
	if err != nil {
		t.Fatalf("list restored database roles: %v", err)
	}
	if len(databaseRoles) != 1 ||
		databaseRoles[0].ID != additional.databaseRole.ID ||
		databaseRoles[0].PasswordSecretHandle != additional.databaseRole.PasswordSecretHandle ||
		!sameStrings(databaseRoles[0].MemberOf, additional.databaseRole.MemberOf) ||
		databaseRoles[0].SchemaGrants["public"] != additional.databaseRole.SchemaGrants["public"] ||
		databaseRoles[0].Metadata["owner"] != additional.databaseRole.Metadata["owner"] ||
		databaseRoles[0].Metadata["api_key"] != additional.databaseRole.Metadata["api_key"] {
		t.Fatalf("expected restored database role %#v, got %#v", additional.databaseRole, databaseRoles)
	}
	functions, err := store.ListProjectFunctions(ctx, ref)
	if err != nil {
		t.Fatalf("list restored project functions: %v", err)
	}
	if len(functions) != 1 ||
		functions[0].ID != additional.projectFunction.ID ||
		functions[0].Version != additional.projectFunction.Version ||
		functions[0].Entrypoint != additional.projectFunction.Entrypoint ||
		functions[0].VerifyJWT != additional.projectFunction.VerifyJWT ||
		functions[0].SourceHash != additional.projectFunction.SourceHash ||
		functions[0].SourceBytes != additional.projectFunction.SourceBytes ||
		functions[0].Secrets["API_TOKEN"] != additional.projectFunction.Secrets["API_TOKEN"] {
		t.Fatalf("expected restored project function %#v, got %#v", additional.projectFunction, functions)
	}
	functionRegions, err := store.ListProjectFunctionRegions(ctx, ref)
	if err != nil {
		t.Fatalf("list restored function regions: %v", err)
	}
	if len(functionRegions) != 1 ||
		functionRegions[0].ID != additional.functionRegion.ID ||
		functionRegions[0].FunctionName != additional.functionRegion.FunctionName ||
		functionRegions[0].Region != additional.functionRegion.Region ||
		functionRegions[0].RoutingPolicy != additional.functionRegion.RoutingPolicy ||
		functionRegions[0].InvocationURL != additional.functionRegion.InvocationURL {
		t.Fatalf("expected restored function region %#v, got %#v", additional.functionRegion, functionRegions)
	}
	functionMounts, err := store.ListProjectFunctionStorageMounts(ctx, ref)
	if err != nil {
		t.Fatalf("list restored function storage mounts: %v", err)
	}
	if len(functionMounts) != 1 ||
		functionMounts[0].ID != additional.functionMount.ID ||
		functionMounts[0].FunctionName != additional.functionMount.FunctionName ||
		functionMounts[0].BucketName != additional.functionMount.BucketName ||
		functionMounts[0].MountPath != additional.functionMount.MountPath ||
		functionMounts[0].ReadOnly != additional.functionMount.ReadOnly ||
		functionMounts[0].Prefix != additional.functionMount.Prefix ||
		functionMounts[0].EnvAlias != additional.functionMount.EnvAlias {
		t.Fatalf("expected restored function storage mount %#v, got %#v", additional.functionMount, functionMounts)
	}
	storageBuckets, err := store.ListProjectStorageBuckets(ctx, ref)
	if err != nil {
		t.Fatalf("list restored storage buckets: %v", err)
	}
	if len(storageBuckets) != 1 ||
		storageBuckets[0].ID != additional.storageBucket.ID ||
		!sameStrings(storageBuckets[0].AllowedMimeTypes, additional.storageBucket.AllowedMimeTypes) ||
		storageBuckets[0].CacheControl != additional.storageBucket.CacheControl ||
		storageBuckets[0].AvifAutodetection != additional.storageBucket.AvifAutodetection ||
		storageBuckets[0].Metadata["owner"] != additional.storageBucket.Metadata["owner"] ||
		storageBuckets[0].Metadata["token"] != additional.storageBucket.Metadata["token"] {
		t.Fatalf("expected restored storage bucket %#v, got %#v", additional.storageBucket, storageBuckets)
	}
	vectorBuckets, err := store.ListProjectVectorBuckets(ctx, ref)
	if err != nil {
		t.Fatalf("list restored vector buckets: %v", err)
	}
	if len(vectorBuckets) != 1 ||
		vectorBuckets[0].ID != additional.vectorBucket.ID ||
		vectorBuckets[0].StorageURI != additional.vectorBucket.StorageURI ||
		vectorBuckets[0].Distance != additional.vectorBucket.Distance ||
		vectorBuckets[0].IndexMethod != additional.vectorBucket.IndexMethod ||
		vectorBuckets[0].Metadata["owner"] != additional.vectorBucket.Metadata["owner"] ||
		vectorBuckets[0].Metadata["access_key"] != additional.vectorBucket.Metadata["access_key"] {
		t.Fatalf("expected restored vector bucket %#v, got %#v", additional.vectorBucket, vectorBuckets)
	}
	analyticsBuckets, err := store.ListProjectAnalyticsBuckets(ctx, ref)
	if err != nil {
		t.Fatalf("list restored analytics buckets: %v", err)
	}
	if len(analyticsBuckets) != 1 ||
		analyticsBuckets[0].ID != additional.analyticsBucket.ID ||
		analyticsBuckets[0].CatalogURI != additional.analyticsBucket.CatalogURI ||
		analyticsBuckets[0].CredentialHandle != additional.analyticsBucket.CredentialHandle ||
		analyticsBuckets[0].Partitioning != additional.analyticsBucket.Partitioning ||
		analyticsBuckets[0].Metadata["owner"] != additional.analyticsBucket.Metadata["owner"] ||
		analyticsBuckets[0].Metadata["access_key"] != additional.analyticsBucket.Metadata["access_key"] {
		t.Fatalf("expected restored analytics bucket %#v, got %#v", additional.analyticsBucket, analyticsBuckets)
	}
	logDrains, err := store.ListProjectLogDrains(ctx, ref)
	if err != nil {
		t.Fatalf("list restored log drains: %v", err)
	}
	if len(logDrains) != 1 ||
		logDrains[0].ID != additional.logDrain.ID ||
		logDrains[0].Target != additional.logDrain.Target ||
		logDrains[0].Config["url"] != additional.logDrain.Config["url"] {
		t.Fatalf("expected restored log drain %#v, got %#v", additional.logDrain, logDrains)
	}
	projectConfig, err := store.GetProjectConfig(ctx, ref, additional.projectConfig.Area)
	if err != nil {
		t.Fatalf("get restored project config: %v", err)
	}
	if projectConfig.ProjectRef != additional.projectConfig.ProjectRef ||
		projectConfig.Area != additional.projectConfig.Area ||
		projectConfig.Config["ip_allowlist"] != additional.projectConfig.Config["ip_allowlist"] ||
		projectConfig.Config["ssl_enforced"] != additional.projectConfig.Config["ssl_enforced"] {
		t.Fatalf("expected restored project config %#v, got %#v", additional.projectConfig, projectConfig)
	}
	projectDomains, err := store.ListProjectDomains(ctx, ref)
	if err != nil {
		t.Fatalf("list restored project domains: %v", err)
	}
	projectDomain, ok := findProjectDomain(projectDomains, additional.projectDomain.FQDN)
	if !ok ||
		projectDomain.ProjectRef != additional.projectDomain.ProjectRef ||
		projectDomain.CertStatus != additional.projectDomain.CertStatus ||
		projectDomain.CertMode != additional.projectDomain.CertMode ||
		projectDomain.CertFingerprint != additional.projectDomain.CertFingerprint ||
		projectDomain.CertNotAfter == nil ||
		additional.projectDomain.CertNotAfter == nil ||
		!projectDomain.CertNotAfter.Equal(*additional.projectDomain.CertNotAfter) {
		t.Fatalf("expected restored project domain %#v, got %#v", additional.projectDomain, projectDomains)
	}
	projectAccess, err := store.ListProjectAccess(ctx, ref)
	if err != nil {
		t.Fatalf("list restored project access: %v", err)
	}
	restoredAccess, ok := findProjectAccessGrant(projectAccess, additional.projectAccess.ID)
	if !ok ||
		restoredAccess.ProjectRef != additional.projectAccess.ProjectRef ||
		restoredAccess.OrgID != additional.projectAccess.OrgID ||
		restoredAccess.SubjectType != additional.projectAccess.SubjectType ||
		restoredAccess.SubjectID != additional.projectAccess.SubjectID ||
		restoredAccess.SubjectName != additional.projectAccess.SubjectName ||
		restoredAccess.Role != additional.projectAccess.Role {
		t.Fatalf("expected restored project access %#v, got %#v", additional.projectAccess, projectAccess)
	}
	backupPolicy, err := store.GetBackupPolicy(ctx, ref)
	if err != nil {
		t.Fatalf("get restored backup policy: %v", err)
	}
	if backupPolicy.ProjectRef != additional.backupPolicy.ProjectRef ||
		backupPolicy.Enabled != additional.backupPolicy.Enabled ||
		backupPolicy.Schedule != additional.backupPolicy.Schedule ||
		backupPolicy.Kind != additional.backupPolicy.Kind ||
		backupPolicy.StorageTargetID != additional.backupPolicy.StorageTargetID ||
		backupPolicy.LastRunAt != additional.backupPolicy.LastRunAt ||
		backupPolicy.NextRunAt == nil != (additional.backupPolicy.NextRunAt == nil) {
		t.Fatalf("expected restored backup policy %#v, got %#v", additional.backupPolicy, backupPolicy)
	}
	cdnPolicy, err := store.GetProjectCDNPolicy(ctx, ref)
	if err != nil {
		t.Fatalf("get restored cdn policy: %v", err)
	}
	if cdnPolicy.ProjectRef != additional.cdnPolicy.ProjectRef ||
		cdnPolicy.Enabled != additional.cdnPolicy.Enabled ||
		cdnPolicy.BrowserTTLSeconds != additional.cdnPolicy.BrowserTTLSeconds ||
		cdnPolicy.EdgeTTLSeconds != additional.cdnPolicy.EdgeTTLSeconds ||
		cdnPolicy.StaleWhileRevalidateSeconds != additional.cdnPolicy.StaleWhileRevalidateSeconds ||
		!sameStrings(cdnPolicy.IncludedPaths, additional.cdnPolicy.IncludedPaths) ||
		!sameStrings(cdnPolicy.ExcludedPaths, additional.cdnPolicy.ExcludedPaths) ||
		cdnPolicy.SmartRevalidation != additional.cdnPolicy.SmartRevalidation ||
		cdnPolicy.CacheControl != additional.cdnPolicy.CacheControl {
		t.Fatalf("expected restored cdn policy %#v, got %#v", additional.cdnPolicy, cdnPolicy)
	}
	pitrPolicy, err := store.GetPITRPolicy(ctx, ref)
	if err != nil {
		t.Fatalf("get restored pitr policy: %v", err)
	}
	if pitrPolicy.ProjectRef != additional.pitrPolicy.ProjectRef ||
		pitrPolicy.Enabled != additional.pitrPolicy.Enabled ||
		pitrPolicy.ArchiveBucket != additional.pitrPolicy.ArchiveBucket ||
		pitrPolicy.RetentionDays != additional.pitrPolicy.RetentionDays {
		t.Fatalf("expected restored pitr policy %#v, got %#v", additional.pitrPolicy, pitrPolicy)
	}
}

func findDatabaseExtension(extensions []ProjectDatabaseExtension, name string) (ProjectDatabaseExtension, bool) {
	for _, extension := range extensions {
		if extension.Name == name {
			return extension, true
		}
	}
	return ProjectDatabaseExtension{}, false
}

func findProjectAccessGrant(grants []ProjectAccessGrant, id string) (ProjectAccessGrant, bool) {
	for _, grant := range grants {
		if grant.ID == id {
			return grant, true
		}
	}
	return ProjectAccessGrant{}, false
}

func findProjectDomain(domains []ProjectDomain, fqdn string) (ProjectDomain, bool) {
	for _, domain := range domains {
		if domain.FQDN == fqdn {
			return domain, true
		}
	}
	return ProjectDomain{}, false
}

func findTeam(teams []Team, slug string) (Team, bool) {
	for _, team := range teams {
		if team.Slug == slug {
			return team, true
		}
	}
	return Team{}, false
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestPersistentPayloadDecryptsPlaintextForMigration(t *testing.T) {
	plain := []byte("legacy checkpoint bytes")
	decrypted, err := decryptPersistentPayload(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plain) {
		t.Fatalf("expected plaintext passthrough, got %q", decrypted)
	}
}

func TestPersistentStoreUsesVaultFileEncryption(t *testing.T) {
	db := openCheckpointDB(t)
	ctx := context.Background()
	keyPath := filepath.Join(t.TempDir(), "vault.key")
	if err := os.WriteFile(keyPath, []byte("vault-managed-key-material"), 0o600); err != nil {
		t.Fatal(err)
	}
	encryption, err := PersistentEncryptionFromEnv(func(key string) string {
		switch key {
		case "SUPADUPA_KMS_PROVIDER":
			return "vault-file"
		case "SUPADUPA_VAULT_KEY_FILE":
			return keyPath
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("vault encryption: %v", err)
	}
	store, err := NewPersistentStoreWithEncryption(ctx, db, encryption)
	if err != nil {
		t.Fatalf("new persistent store: %v", err)
	}
	org, err := store.CreateOrg(ctx, "Vaulted")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "vaulted", Name: "Vaulted"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	payload := checkpointPayload(t)
	if !bytes.HasPrefix(payload, []byte(vaultFileEncryptedPayloadPrefix)) {
		t.Fatalf("expected vault-file encrypted checkpoint prefix, got %q", payload[:len(vaultFileEncryptedPayloadPrefix)])
	}
	secrets, err := store.ListProjectSecrets(ctx, project.Ref)
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}
	for _, secret := range secrets {
		if bytes.Contains(payload, []byte(secret.Value)) {
			t.Fatalf("vault-file checkpoint contains plaintext secret %s", secret.Kind)
		}
	}
	restored, err := NewPersistentStoreWithEncryption(ctx, db, encryption)
	if err != nil {
		t.Fatalf("restore persistent store: %v", err)
	}
	projects, err := restored.ListProjectsByOrg(ctx, org.ID)
	if err != nil {
		t.Fatalf("list restored projects: %v", err)
	}
	if len(projects) != 1 || projects[0].Ref != project.Ref {
		t.Fatalf("expected restored vaulted project, got %#v", projects)
	}
}

func TestPersistentStoreEncryptsNormalizedMFASeedStrings(t *testing.T) {
	store := &PersistentStore{encryption: newLocalPersistentCipher("mfa-seed-test-secret")}
	encoded, err := store.encryptOptionalString("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("encrypt mfa seed: %v", err)
	}
	encodedString, ok := encoded.(string)
	if !ok {
		t.Fatalf("expected encoded encrypted string, got %#v", encoded)
	}
	if strings.Contains(encodedString, "JBSWY3DPEHPK3PXP") {
		t.Fatalf("encrypted mfa seed storage contains plaintext seed: %s", encodedString)
	}
	if !strings.HasPrefix(encodedString, encryptedStringPrefix) {
		t.Fatalf("expected encrypted string prefix, got %q", encodedString)
	}
	decrypted, err := store.decryptOptionalString(sql.NullString{String: encodedString, Valid: true})
	if err != nil {
		t.Fatalf("decrypt mfa seed: %v", err)
	}
	if decrypted != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("expected decrypted seed, got %q", decrypted)
	}
	beforeLegacyLoads := LegacyMFAPlaintextLoadCount()
	legacy, err := store.decryptOptionalString(sql.NullString{String: "LEGACYPLAINTEXT", Valid: true})
	if err != nil {
		t.Fatalf("decrypt legacy mfa seed: %v", err)
	}
	if legacy != "LEGACYPLAINTEXT" {
		t.Fatalf("expected legacy plaintext passthrough, got %q", legacy)
	}
	if LegacyMFAPlaintextLoadCount() != beforeLegacyLoads+1 {
		t.Fatalf("expected legacy MFA plaintext load counter +1, before=%d after=%d", beforeLegacyLoads, LegacyMFAPlaintextLoadCount())
	}
	// Encrypted path must not count as legacy plaintext load.
	beforeEncrypted := LegacyMFAPlaintextLoadCount()
	if _, err := store.decryptOptionalString(sql.NullString{String: encodedString, Valid: true}); err != nil {
		t.Fatalf("decrypt encrypted again: %v", err)
	}
	if LegacyMFAPlaintextLoadCount() != beforeEncrypted {
		t.Fatalf("encrypted load must not increment legacy counter, before=%d after=%d", beforeEncrypted, LegacyMFAPlaintextLoadCount())
	}
	empty, err := store.encryptOptionalString("")
	if err != nil {
		t.Fatalf("encrypt empty mfa seed: %v", err)
	}
	if empty != nil {
		t.Fatalf("expected empty mfa seed to store as nil, got %#v", empty)
	}
}

func TestPersistentEncryptionCommandRoundTrip(t *testing.T) {
	commandPath := filepath.Join(t.TempDir(), "kms-command.sh")
	script := `#!/bin/sh
if [ "$SUPADUPA_KMS_OPERATION" = "encrypt" ]; then
  printf 'kms:'
  tr 'A-Za-z' 'N-ZA-Mn-za-m'
elif [ "$SUPADUPA_KMS_OPERATION" = "decrypt" ]; then
  sed 's/^kms://' | tr 'A-Za-z' 'N-ZA-Mn-za-m'
else
  exit 2
fi
`
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	encryption, err := PersistentEncryptionFromEnv(func(key string) string {
		switch key {
		case "SUPADUPA_KMS_PROVIDER":
			return "command"
		case "SUPADUPA_KMS_COMMAND":
			return commandPath
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("command encryption: %v", err)
	}
	payload, err := encryption.Encrypt([]byte("external-kms-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !bytes.HasPrefix(payload, []byte(commandEncryptedPayloadPrefix)) || bytes.Contains(payload, []byte("external-kms-secret")) {
		t.Fatalf("expected command-encrypted payload, got %q", payload)
	}
	plaintext, err := encryption.Decrypt(payload)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(plaintext) != "external-kms-secret" {
		t.Fatalf("expected command decrypt round trip, got %q", plaintext)
	}
}

func TestPersistentStoreConcurrentAuditEventsSerializeCheckpoints(t *testing.T) {
	db := openCheckpointDB(t)
	ctx := context.Background()
	store, err := NewPersistentStore(ctx, db)
	if err != nil {
		t.Fatalf("new persistent store: %v", err)
	}

	const events = 64
	var wg sync.WaitGroup
	errs := make(chan error, events)
	for i := 0; i < events; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.RecordAuditEvent(ctx, AuditEventInput{
				Action: "project.inspect",
				Target: "project:smoke",
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("record concurrent audit event: %v", err)
	}

	auditEvents, err := store.ListAuditEvents(ctx, events)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(auditEvents) != events {
		t.Fatalf("expected %d audit events, got %d", events, len(auditEvents))
	}
	ids := map[string]struct{}{}
	for _, event := range auditEvents {
		if event.ID == "" {
			t.Fatalf("expected audit event ID: %#v", event)
		}
		if _, exists := ids[event.ID]; exists {
			t.Fatalf("duplicate audit event ID %q", event.ID)
		}
		ids[event.ID] = struct{}{}
	}
	integrity, err := store.VerifyAuditLog(ctx)
	if err != nil {
		t.Fatalf("verify audit log: %v", err)
	}
	if !integrity.Verified || integrity.Events != events {
		t.Fatalf("expected verified audit chain with %d events, got %+v", events, integrity)
	}
	if max := checkpointMaxActive(t); max > 1 {
		t.Fatalf("expected serialized checkpoint writes, saw %d active writes", max)
	}
}

var (
	checkpointDriverOnce sync.Once
	checkpointDriversMu  sync.Mutex
	checkpointDrivers    = map[string]*checkpointState{}
)

type checkpointState struct {
	mu        sync.Mutex
	state     []byte
	active    int
	maxActive int
}

func openCheckpointDB(t *testing.T) *sql.DB {
	t.Helper()
	checkpointDriverOnce.Do(func() {
		sql.Register("control_checkpoint_fake", checkpointDriver{})
	})
	dsn := t.Name()
	state := &checkpointState{}
	checkpointDriversMu.Lock()
	checkpointDrivers[dsn] = state
	checkpointDriversMu.Unlock()
	db, err := sql.Open("control_checkpoint_fake", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		checkpointDriversMu.Lock()
		delete(checkpointDrivers, dsn)
		checkpointDriversMu.Unlock()
	})
	return db
}

func checkpointPayload(t *testing.T) []byte {
	t.Helper()
	checkpointDriversMu.Lock()
	state := checkpointDrivers[t.Name()]
	checkpointDriversMu.Unlock()
	if state == nil {
		t.Fatalf("missing checkpoint state for %s", t.Name())
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return append([]byte(nil), state.state...)
}

func checkpointMaxActive(t *testing.T) int {
	t.Helper()
	checkpointDriversMu.Lock()
	state := checkpointDrivers[t.Name()]
	checkpointDriversMu.Unlock()
	if state == nil {
		t.Fatalf("missing checkpoint state for %s", t.Name())
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.maxActive
}

type checkpointDriver struct{}

func (checkpointDriver) Open(name string) (driver.Conn, error) {
	checkpointDriversMu.Lock()
	state := checkpointDrivers[name]
	checkpointDriversMu.Unlock()
	return checkpointConn{state: state}, nil
}

type checkpointConn struct {
	state *checkpointState
}

func (checkpointConn) Prepare(string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}

func (checkpointConn) Close() error {
	return nil
}

func (checkpointConn) Begin() (driver.Tx, error) {
	return checkpointTx{}, nil
}

func (checkpointConn) CheckNamedValue(*driver.NamedValue) error {
	return nil
}

func (conn checkpointConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(query, "control_state_checkpoints") {
		return driver.RowsAffected(1), nil
	}
	conn.state.mu.Lock()
	conn.state.active++
	if conn.state.active > conn.state.maxActive {
		conn.state.maxActive = conn.state.active
	}
	conn.state.mu.Unlock()
	defer func() {
		conn.state.mu.Lock()
		conn.state.active--
		conn.state.mu.Unlock()
	}()
	time.Sleep(2 * time.Millisecond)

	conn.state.mu.Lock()
	defer conn.state.mu.Unlock()
	if len(args) >= 2 {
		if payload, ok := args[1].Value.([]byte); ok {
			conn.state.state = append([]byte(nil), payload...)
		}
	}
	return driver.RowsAffected(1), nil
}

func (conn checkpointConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	conn.state.mu.Lock()
	defer conn.state.mu.Unlock()
	if len(conn.state.state) == 0 {
		return &checkpointRows{}, nil
	}
	return &checkpointRows{values: []driver.Value{append([]byte(nil), conn.state.state...)}}, nil
}

type checkpointTx struct{}

func (checkpointTx) Commit() error {
	return nil
}

func (checkpointTx) Rollback() error {
	return nil
}

type checkpointRows struct {
	values []driver.Value
	read   bool
}

func (checkpointRows) Columns() []string {
	return []string{"state"}
}

func (rows *checkpointRows) Close() error {
	return nil
}

func (rows *checkpointRows) Next(dest []driver.Value) error {
	if rows.read || len(rows.values) == 0 {
		return io.EOF
	}
	rows.read = true
	copy(dest, rows.values)
	return nil
}
