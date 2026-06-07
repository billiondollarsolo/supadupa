package control

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCertificateServiceWritesManualPlan(t *testing.T) {
	service := NewCertificateServiceWithOptions(CertificateServiceOptions{RootDir: t.TempDir()})
	result, err := service.Provision(context.Background(), ProjectDomain{
		ProjectRef: "alpha",
		FQDN:       "API.Example.COM.",
	})
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}
	if result.Status != "pending" || result.State != "manual" || !strings.HasSuffix(result.Path, "alpha/api.example.com.json") {
		t.Fatalf("unexpected certificate result: %#v", result)
	}
	payload, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"project_ref": "alpha"`, `"fqdn": "api.example.com"`, `"status": "pending"`, "Traefik will request ACME certificates"} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("expected certificate plan to contain %q, got:\n%s", expected, payload)
		}
	}
	if err := service.Remove(context.Background(), "alpha", "api.example.com"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
		t.Fatalf("expected certificate plan removed, got err=%v", err)
	}
}

func TestCertificateServiceRunsCommand(t *testing.T) {
	service := NewCertificateServiceWithOptions(CertificateServiceOptions{
		RootDir: t.TempDir(),
		Command: "printf 'issued %s for %s at %s\\n' {{fqdn}} {{project_ref}} {{cert_path}}",
	})
	result, err := service.Provision(context.Background(), ProjectDomain{
		ProjectRef: "alpha",
		FQDN:       "api.example.com",
	})
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}
	if result.Status != "issued" || result.State != "completed" || !strings.HasSuffix(result.Path, "alpha/api.example.com.log") {
		t.Fatalf("unexpected certificate command result: %#v", result)
	}
	payload, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "issued api.example.com for alpha at "+result.Path) {
		t.Fatalf("expected certificate transcript, got:\n%s", payload)
	}
}

func TestCertificateServiceRemovesProjectArtifacts(t *testing.T) {
	root := t.TempDir()
	service := NewCertificateServiceWithOptions(CertificateServiceOptions{RootDir: root})
	for _, fqdn := range []string{"api.example.com", "studio.example.com"} {
		if _, err := service.Provision(context.Background(), ProjectDomain{
			ProjectRef: "alpha",
			FQDN:       fqdn,
		}); err != nil {
			t.Fatalf("provision %s failed: %v", fqdn, err)
		}
	}
	if err := service.RemoveProject(context.Background(), "alpha"); err != nil {
		t.Fatalf("remove project failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("expected project certificate directory removed, got err=%v", err)
	}
}

func TestCertificateServiceUploadsBYOCertificate(t *testing.T) {
	root := t.TempDir()
	service := NewCertificateServiceWithOptions(CertificateServiceOptions{RootDir: root})
	certPEM, keyPEM := testDomainCertificate(t, []string{"api.example.com"}, time.Now().UTC().Add(time.Hour))
	result, err := service.Upload(context.Background(), ProjectDomain{ProjectRef: "alpha", FQDN: "api.example.com"}, certPEM, keyPEM)
	if err != nil {
		t.Fatalf("upload certificate: %v", err)
	}
	if result.Status != "uploaded" || result.State != "byo" || result.Fingerprint == "" || result.NotAfter == nil {
		t.Fatalf("unexpected upload result: %#v", result)
	}
	for _, path := range []string{
		filepath.Join(root, "alpha", "api.example.com.crt"),
		filepath.Join(root, "alpha", "api.example.com.key"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected uploaded cert artifact %s: %v", path, err)
		}
	}
	if err := service.Remove(context.Background(), "alpha", "api.example.com"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha", "api.example.com.key")); !os.IsNotExist(err) {
		t.Fatalf("expected uploaded key removed, got err=%v", err)
	}
}

func TestCertificateServiceRejectsHostnameMismatch(t *testing.T) {
	service := NewCertificateServiceWithOptions(CertificateServiceOptions{RootDir: t.TempDir()})
	certPEM, keyPEM := testDomainCertificate(t, []string{"other.example.com"}, time.Now().UTC().Add(time.Hour))
	if _, err := service.Upload(context.Background(), ProjectDomain{ProjectRef: "alpha", FQDN: "api.example.com"}, certPEM, keyPEM); err == nil || !strings.Contains(err.Error(), "not valid for api.example.com") {
		t.Fatalf("expected hostname mismatch error, got %v", err)
	}
}

func testDomainCertificate(t *testing.T, dnsNames []string, notAfter time.Time) (string, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
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
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}))
	return certPEM, keyPEM
}
