package provisioner

import "testing"

func TestNewFromEnvDefaultsToCompose(t *testing.T) {
	provisioner, err := NewFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatalf("expected default provisioner: %v", err)
	}
	if provisioner.Name() != "compose" {
		t.Fatalf("expected compose provisioner, got %s", provisioner.Name())
	}
}

func TestNewFromEnvSelectsKubernetes(t *testing.T) {
	provisioner, err := NewFromEnv(func(key string) string {
		if key == "SUPADUPA_PROVISIONER" {
			return "k8s"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("expected kubernetes provisioner: %v", err)
	}
	if provisioner.Name() != "kubernetes" {
		t.Fatalf("expected kubernetes provisioner, got %s", provisioner.Name())
	}
}

func TestNewFromEnvRejectsUnknown(t *testing.T) {
	if _, err := NewFromEnv(func(key string) string {
		if key == "SUPADUPA_PROVISIONER" {
			return "nomad"
		}
		return ""
	}); err == nil {
		t.Fatal("expected unknown provisioner error")
	}
}
