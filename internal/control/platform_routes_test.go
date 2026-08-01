package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsurePlatformRouteFileWritesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	getenv := func(key string) string {
		switch key {
		case "SUPADUPA_API_HOST":
			return "api.example.test"
		case "SUPADUPA_ADMIN_HOST":
			return "admin.example.test"
		default:
			return ""
		}
	}
	wrote, err := EnsurePlatformRouteFile(dir, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatal("expected write when missing")
	}
	body, err := os.ReadFile(filepath.Join(dir, "00-platform.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "Host(`api.example.test`)") || !strings.Contains(text, "Host(`admin.example.test`)") {
		t.Fatalf("unexpected platform route body:\n%s", text)
	}
	// second call is no-op
	wrote, err = EnsurePlatformRouteFile(dir, getenv)
	if err != nil || wrote {
		t.Fatalf("expected no-op when present, wrote=%v err=%v", wrote, err)
	}
}

func TestEnsurePlatformRouteFileNoopWithoutHosts(t *testing.T) {
	wrote, err := EnsurePlatformRouteFile(t.TempDir(), func(string) string { return "" })
	if err != nil || wrote {
		t.Fatalf("expected noop, wrote=%v err=%v", wrote, err)
	}
}
