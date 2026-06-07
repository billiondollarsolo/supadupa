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
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	updatedOrgFlags, err := store.UpdateOrgFeatureFlags(ctx, org.ID, OrgFeatureFlagsInput{Overrides: map[string]bool{"billing": false, "custom_domains": true}})
	if err != nil {
		t.Fatalf("update org feature flags: %v", err)
	}
	host, err := store.CreateHost(ctx, CreateHostRequest{Name: "host-a", Address: "10.0.0.10", Capacity: HostCapacity{CPU: 4, RAMMB: 8192, DiskGB: 100, Project: 3}})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	updatedDefaults, err := store.UpdatePlatformDefaults(ctx, PlatformDefaultsInput{
		Domain:         "apps.example.com",
		StackVersion:   "15.8.1.060",
		Profile:        StackProfileEssential,
		ResourceTier:   ResourceTierMedium,
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
	if projects[0].Spec.Domain != updatedDefaults.Domain || projects[0].Spec.ResourceTier != updatedDefaults.ResourceTier {
		t.Fatalf("expected restored project to use defaults, got %#v", projects[0].Spec)
	}
	teams, err := restored.ListOrgTeams(ctx, org.ID)
	if err != nil {
		t.Fatalf("list restored teams: %v", err)
	}
	if len(teams) != 1 || teams[0].Slug != team.Slug {
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
