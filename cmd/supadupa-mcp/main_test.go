package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"supadupa2026/internal/mcp"
)

// TestMCPEntrypointAcceptsDefaultsAndEOF mirrors main(): mcp.Runner{}.Run(ctx, args)
// with empty stdin so the stdio server exits cleanly on EOF.
func TestMCPEntrypointAcceptsDefaultsAndEOF(t *testing.T) {
	var stderr bytes.Buffer
	code := mcp.Runner{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
	}.Run(context.Background(), nil)
	if code != 0 {
		t.Fatalf("expected exit 0 on EOF, got %d stderr=%s", code, stderr.String())
	}
}

// TestMCPEntrypointRejectsInvalidAPI exercises the same flag/API wiring main uses.
func TestMCPEntrypointRejectsInvalidAPI(t *testing.T) {
	var stderr bytes.Buffer
	code := mcp.Runner{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
	}.Run(context.Background(), []string{"--api", "not-a-url"})
	if code != 2 {
		t.Fatalf("expected exit 2 for invalid --api, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "invalid --api") {
		t.Fatalf("expected invalid --api message, got %q", stderr.String())
	}
}
