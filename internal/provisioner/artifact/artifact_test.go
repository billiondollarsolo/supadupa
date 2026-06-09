package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileReplacesExistingArtifact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.yaml")

	if err := WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}
	if err := WriteFile(path, []byte("new\n"), 0o640); err != nil {
		t.Fatalf("replacement write failed: %v", err)
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replacement failed: %v", err)
	}
	if string(payload) != "new\n" {
		t.Fatalf("payload = %q, want new", payload)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat replacement failed: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("mode = %o, want 640", got)
	}
}

func TestWriteFileRemovesTempOnError(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFile(filepath.Join(dir, "missing", "project.yaml"), []byte("new\n"), 0o600); err == nil {
		t.Fatal("expected write in missing directory to fail")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no temp files, got %d", len(entries))
	}
}

func TestWriteFileRemovesTempWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.yaml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create conflicting directory failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "existing"), []byte("old\n"), 0o600); err != nil {
		t.Fatalf("seed conflicting directory failed: %v", err)
	}

	if err := WriteFile(path, []byte("new\n"), 0o600); err == nil {
		t.Fatal("expected rename over non-empty directory to fail")
	}

	payload, err := os.ReadFile(filepath.Join(path, "existing"))
	if err != nil {
		t.Fatalf("conflicting directory was not preserved: %v", err)
	}
	if string(payload) != "old\n" {
		t.Fatalf("conflicting payload = %q, want old", payload)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read artifact dir failed: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "project.yaml" {
			t.Fatalf("unexpected temp artifact left behind: %s", entry.Name())
		}
	}
}
