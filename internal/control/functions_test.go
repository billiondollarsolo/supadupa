package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFunctionDeploymentServiceWritesRuntimeArtifact(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	function := ProjectFunction{
		ProjectRef:  "alpha",
		Name:        "hello-api",
		Version:     3,
		Entrypoint:  "handlers/index.ts",
		VerifyJWT:   true,
		SourceHash:  "abc123",
		SourceBytes: 36,
	}
	input := ProjectFunctionInput{
		Source:  "Deno.serve(() => new Response('ok'))",
		Secrets: map[string]string{"API_KEY": "super-secret"},
	}

	artifact, err := NewFunctionDeploymentServiceWithOptions(FunctionDeploymentOptions{ProjectRoot: root}).Deploy(ctx, function, input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(artifact.SourcePath, filepath.Join("alpha", "functions", "hello-api", "handlers", "index.ts")) {
		t.Fatalf("unexpected source path: %#v", artifact)
	}
	if !strings.HasSuffix(artifact.RuntimeDirectory, filepath.Join("alpha", "functions", ".supadupa-runtime", "hello-api-v3")) {
		t.Fatalf("unexpected runtime directory: %#v", artifact)
	}
	source, err := os.ReadFile(artifact.SourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(source) != input.Source {
		t.Fatalf("expected source artifact, got:\n%s", source)
	}
	secrets, err := os.ReadFile(artifact.SecretsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"SUPABASE_FUNCTION_NAME=hello-api", "SUPABASE_FUNCTION_VERSION=3", "VERIFY_JWT=true", "API_KEY=super-secret"} {
		if !strings.Contains(string(secrets), expected) {
			t.Fatalf("expected secret env %q, got:\n%s", expected, secrets)
		}
	}
	metadata, err := os.ReadFile(artifact.MetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"name": "hello-api"`, `"version": 3`, `"entrypoint": "handlers/index.ts"`, `"source_hash": "abc123"`} {
		if !strings.Contains(string(metadata), expected) {
			t.Fatalf("expected metadata %q, got:\n%s", expected, metadata)
		}
	}
	runtimeSource, err := os.ReadFile(filepath.Join(artifact.RuntimeDirectory, "handlers", "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if string(runtimeSource) != input.Source {
		t.Fatalf("expected versioned runtime source artifact, got:\n%s", runtimeSource)
	}
}

func TestFunctionDeploymentServiceRejectsEscapingEntrypoint(t *testing.T) {
	_, err := NewFunctionDeploymentServiceWithOptions(FunctionDeploymentOptions{ProjectRoot: t.TempDir()}).Deploy(context.Background(), ProjectFunction{
		ProjectRef: "alpha",
		Name:       "hello-api",
		Version:    1,
		Entrypoint: "../index.ts",
	}, ProjectFunctionInput{Source: "Deno.serve(() => new Response('ok'))"})
	if err == nil {
		t.Fatalf("expected invalid entrypoint error")
	}
}

func TestFunctionDeploymentServiceDeletesRuntimeArtifact(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	service := NewFunctionDeploymentServiceWithOptions(FunctionDeploymentOptions{ProjectRoot: root})
	artifact, err := service.Deploy(ctx, ProjectFunction{
		ProjectRef: "alpha",
		Name:       "hello-api",
		Version:    1,
		Entrypoint: "index.ts",
	}, ProjectFunctionInput{Source: "Deno.serve(() => new Response('ok'))"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, "alpha", "hello-api"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact.Directory); !os.IsNotExist(err) {
		t.Fatalf("expected artifact directory removed, got %v", err)
	}
	if _, err := os.Stat(artifact.RuntimeDirectory); !os.IsNotExist(err) {
		t.Fatalf("expected versioned runtime directory removed, got %v", err)
	}
}
