package control

import (
	"context"
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
	Path   string `json:"path"`
	Status string `json:"status"`
	State  string `json:"state"`
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
	for _, ext := range []string{".json", ".log"} {
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
