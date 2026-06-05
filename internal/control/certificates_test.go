package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
