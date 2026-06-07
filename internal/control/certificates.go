package control

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type CertificateService struct {
	rootDir string
	command string
}

type CertificateServiceOptions struct {
	RootDir string
	Command string
}

type CertificateResult struct {
	Path        string     `json:"path"`
	Status      string     `json:"status"`
	State       string     `json:"state"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	NotAfter    *time.Time `json:"not_after,omitempty"`
}

func NewCertificateService() *CertificateService {
	return NewCertificateServiceWithOptions(CertificateServiceOptions{})
}

func NewCertificateServiceWithOptions(opts CertificateServiceOptions) *CertificateService {
	rootDir := strings.TrimSpace(opts.RootDir)
	if rootDir == "" {
		rootDir = os.Getenv("SUPADUPA_CERT_ROOT")
	}
	if rootDir == "" {
		rootDir = "./runtime/certs"
	}
	command := opts.Command
	if command == "" {
		command = os.Getenv("SUPADUPA_CERT_COMMAND")
	}
	return &CertificateService{rootDir: rootDir, command: command}
}

func (s *CertificateService) Provision(ctx context.Context, domain ProjectDomain) (CertificateResult, error) {
	select {
	case <-ctx.Done():
		return CertificateResult{}, ctx.Err()
	default:
	}
	if strings.TrimSpace(domain.ProjectRef) == "" {
		return CertificateResult{}, fmt.Errorf("project ref is required")
	}
	fqdn, err := normalizeDomain(domain.FQDN)
	if err != nil {
		return CertificateResult{}, err
	}
	dir := filepath.Join(s.rootDir, domain.ProjectRef)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return CertificateResult{}, err
	}
	if strings.TrimSpace(s.command) == "" {
		path := filepath.Join(dir, fqdn+".json")
		if err := writeCertificatePlan(path, domain.ProjectRef, fqdn); err != nil {
			return CertificateResult{}, err
		}
		return CertificateResult{Path: path, Status: "pending", State: "manual"}, nil
	}
	path := filepath.Join(dir, fqdn+".log")
	if err := s.runCertificateCommand(ctx, path, domain.ProjectRef, fqdn); err != nil {
		return CertificateResult{Path: path, Status: "failed", State: "failed"}, err
	}
	return CertificateResult{Path: path, Status: "issued", State: "completed"}, nil
}

func (s *CertificateService) Upload(ctx context.Context, domain ProjectDomain, certificatePEM string, privateKeyPEM string) (CertificateResult, error) {
	select {
	case <-ctx.Done():
		return CertificateResult{}, ctx.Err()
	default:
	}
	if strings.TrimSpace(domain.ProjectRef) == "" {
		return CertificateResult{}, fmt.Errorf("project ref is required")
	}
	fqdn, err := normalizeDomain(domain.FQDN)
	if err != nil {
		return CertificateResult{}, err
	}
	certificatePEM = strings.TrimSpace(certificatePEM)
	privateKeyPEM = strings.TrimSpace(privateKeyPEM)
	if certificatePEM == "" || privateKeyPEM == "" {
		return CertificateResult{}, fmt.Errorf("certificate_pem and private_key_pem are required")
	}
	keyPair, err := tls.X509KeyPair([]byte(certificatePEM), []byte(privateKeyPEM))
	if err != nil {
		return CertificateResult{}, fmt.Errorf("invalid certificate key pair: %w", err)
	}
	if len(keyPair.Certificate) == 0 {
		return CertificateResult{}, fmt.Errorf("certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		return CertificateResult{}, fmt.Errorf("parse leaf certificate: %w", err)
	}
	now := time.Now().UTC()
	if now.Before(leaf.NotBefore) {
		return CertificateResult{}, fmt.Errorf("certificate is not valid before %s", leaf.NotBefore.UTC().Format(time.RFC3339))
	}
	if !now.Before(leaf.NotAfter) {
		return CertificateResult{}, fmt.Errorf("certificate expired at %s", leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	if err := leaf.VerifyHostname(fqdn); err != nil {
		return CertificateResult{}, fmt.Errorf("certificate is not valid for %s: %w", fqdn, err)
	}
	dir := filepath.Join(s.rootDir, domain.ProjectRef)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return CertificateResult{}, err
	}
	certPath := filepath.Join(dir, fqdn+".crt")
	keyPath := filepath.Join(dir, fqdn+".key")
	if err := atomicWriteFile(certPath, []byte(certificatePEM+"\n"), 0o600); err != nil {
		return CertificateResult{}, err
	}
	if err := atomicWriteFile(keyPath, []byte(privateKeyPEM+"\n"), 0o600); err != nil {
		_ = os.Remove(certPath)
		return CertificateResult{}, err
	}
	fingerprint := sha256.Sum256(leaf.Raw)
	notAfter := leaf.NotAfter.UTC()
	return CertificateResult{
		Path:        certPath,
		Status:      "uploaded",
		State:       "byo",
		Fingerprint: strings.ToUpper(hex.EncodeToString(fingerprint[:])),
		NotAfter:    &notAfter,
	}, nil
}

func (s *CertificateService) Remove(ctx context.Context, ref string, fqdn string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	normalized, err := normalizeDomain(fqdn)
	if err != nil {
		return err
	}
	dir := filepath.Join(s.rootDir, strings.TrimSpace(ref))
	for _, ext := range []string{".json", ".log", ".crt", ".key"} {
		if err := os.Remove(filepath.Join(dir, normalized+ext)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *CertificateService) RemoveProject(ctx context.Context, ref string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("project ref is required")
	}
	if err := os.RemoveAll(filepath.Join(s.rootDir, ref)); err != nil {
		return err
	}
	return nil
}

func (s *CertificateService) runCertificateCommand(ctx context.Context, path string, ref string, fqdn string) error {
	command := renderCertificateCommand(s.command, ref, fqdn, path)
	output, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
	if len(output) > 0 {
		_ = os.WriteFile(path, output, 0o600)
	}
	if err != nil {
		return fmt.Errorf("certificate command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if len(output) == 0 {
		output = []byte("certificate command completed without output\n")
	}
	return os.WriteFile(path, output, 0o600)
}

func writeCertificatePlan(path string, ref string, fqdn string) error {
	payload, err := json.MarshalIndent(map[string]any{
		"project_ref": ref,
		"fqdn":        fqdn,
		"status":      "pending",
		"state":       "manual",
		"created_at":  time.Now().UTC(),
		"instructions": []string{
			"Point the custom domain at the supadupa edge proxy.",
			"Configure SUPADUPA_CERT_COMMAND to automate certificate issuance.",
			"Traefik will request ACME certificates for rendered TLS routers when its cert resolver is configured.",
		},
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}

func atomicWriteFile(path string, payload []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func renderCertificateCommand(template string, ref string, fqdn string, certPath string) string {
	replacer := strings.NewReplacer(
		"{{ref}}", shellQuote(ref),
		"{{project_ref}}", shellQuote(ref),
		"{{fqdn}}", shellQuote(fqdn),
		"{{cert_path}}", shellQuote(certPath),
		"{{cert_root}}", shellQuote(filepath.Dir(certPath)),
	)
	return replacer.Replace(template)
}
