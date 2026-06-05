package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogDrainDeploymentWritesHTTPVectorSink(t *testing.T) {
	root := t.TempDir()
	service := NewLogDrainDeploymentServiceWithOptions(LogDrainDeploymentOptions{ProjectRoot: root})
	drain := LogDrain{
		ID:         "drain-one",
		ProjectRef: "alpha",
		Target:     "https",
		Config: map[string]string{
			"url":   "https://logs.example.com/ingest",
			"token": "secret-token",
		},
		CreatedAt: time.Now().UTC(),
	}

	artifact, err := service.Deploy(context.Background(), drain)
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}
	expectedPath := filepath.Join(root, "alpha", "log-drains", "drain-one.toml")
	if artifact.Path != expectedPath {
		t.Fatalf("expected artifact path %s, got %s", expectedPath, artifact.Path)
	}
	body, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`[sinks.log_drain_drain_one]`,
		`type = "http"`,
		`inputs = ["add_project"]`,
		`uri = "https://logs.example.com/ingest"`,
		`token = "secret-token"`,
		`x-supadupa-project-ref = "alpha"`,
	} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("expected artifact to contain %q, got:\n%s", expected, body)
		}
	}

	if err := service.Delete(context.Background(), "alpha", "drain-one"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := os.Stat(expectedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected artifact removed, got err=%v", err)
	}
}

func TestLogDrainDeploymentWritesTargetSpecificSinks(t *testing.T) {
	root := t.TempDir()
	service := NewLogDrainDeploymentServiceWithOptions(LogDrainDeploymentOptions{ProjectRoot: root})
	cases := []struct {
		name     string
		drain    LogDrain
		expected []string
	}{
		{
			name: "datadog",
			drain: LogDrain{
				ID:         "dd",
				ProjectRef: "alpha",
				Target:     "datadog",
				Config:     map[string]string{"api_key": "dd-key", "site": "datadoghq.eu"},
			},
			expected: []string{`type = "datadog_logs"`, `default_api_key = "dd-key"`, `site = "datadoghq.eu"`},
		},
		{
			name: "s3",
			drain: LogDrain{
				ID:         "archive",
				ProjectRef: "alpha",
				Target:     "s3",
				Config:     map[string]string{"bucket": "supadupa-logs", "region": "us-east-1"},
			},
			expected: []string{`type = "aws_s3"`, `bucket = "supadupa-logs"`, `key_prefix = "alpha/%Y/%m/%d/"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			artifact, err := service.Deploy(context.Background(), tc.drain)
			if err != nil {
				t.Fatalf("deploy failed: %v", err)
			}
			body, err := os.ReadFile(artifact.Path)
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range tc.expected {
				if !strings.Contains(string(body), expected) {
					t.Fatalf("expected artifact to contain %q, got:\n%s", expected, body)
				}
			}
		})
	}
}
