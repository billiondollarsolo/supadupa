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
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"supadupa2026/internal/control"
)

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
	master := perform(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.supadupa.test","stack_version":"latest","profile":"full","resource_tier":"custom","backup_schedule":"daily","feature_flags":{"database_external_access":true}}`)
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

func (fakeProvisioner) Scale(ctx context.Context, ref string, spec control.ProjectSpec) error {
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

func isLowerHexForTest(value string) bool {
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
