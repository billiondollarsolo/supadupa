package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"supadupa2026/internal/cli"
)

// TestCLIEntrypointWiresHelp exercises the same Runner.Run path main() uses
// (cli.Runner{}.Run(ctx, args)) without spawning a subprocess.
func TestCLIEntrypointWiresHelp(t *testing.T) {
	var stderr bytes.Buffer
	code := cli.Runner{Stderr: &stderr}.Run(context.Background(), []string{"help"})
	if code != 0 {
		t.Fatalf("help exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "commands:") {
		t.Fatalf("expected CLI usage from entrypoint wiring, got %q", stderr.String())
	}
}

// TestCLIEntrypointWiresFlagHelp covers main-style --help via the flag set.
func TestCLIEntrypointWiresFlagHelp(t *testing.T) {
	var stderr bytes.Buffer
	code := cli.Runner{Stderr: &stderr}.Run(context.Background(), []string{"-h"})
	if code != 0 {
		t.Fatalf("-h exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "commands:") {
		t.Fatalf("expected usage on -h, got %q", stderr.String())
	}
}
