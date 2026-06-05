package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoginPostsCredentialsAndPrintsJSON(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/login" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"abc","user":{"email":"admin@example.com"}}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"login", "--email", "admin@example.com", "--password", "secret"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if got["email"] != "admin@example.com" || got["password"] != "secret" {
		t.Fatalf("unexpected payload %#v", got)
	}
	if !strings.Contains(stdout.String(), "\n  \"token\": \"abc\"") {
		t.Fatalf("expected pretty JSON, got %q", stdout.String())
	}
}

func TestOrgsCreateSendsBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing bearer auth: %q", r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/orgs" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]string
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["name"] != "Platform" {
			t.Fatalf("unexpected org name %q", got["name"])
		}
		_, _ = w.Write([]byte(`{"id":"org_1","name":"Platform"}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env: map[string]string{
			"SUPADUPA_API_URL": server.URL,
			"SUPADUPA_TOKEN":   "test-token",
		},
	}.Run(context.Background(), []string{"orgs", "create", "--name", "Platform"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestOrgLifecycleCommandsUseOrgEndpoints(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1":
			_, _ = w.Write([]byte(`{"id":"org_1","name":"Platform"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/orgs/org_1":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["name"] != "Renamed" {
				t.Fatalf("unexpected org update payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"org_1","name":"Renamed"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/orgs/org_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"orgs", "get", "--id", "org_1"},
		{"orgs", "update", "--id", "org_1", "--name", "Renamed"},
		{"orgs", "delete", "--id", "org_1", "--yes"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("exit=%d stderr=%s for %v", exitCode, stderr.String(), args)
		}
	}
	expected := []string{
		"GET /v1/orgs/org_1",
		"PUT /v1/orgs/org_1",
		"DELETE /v1/orgs/org_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestHostLifecycleCommandsUseHostEndpoints(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/hosts":
			var got struct {
				Name     string         `json:"name"`
				Address  string         `json:"address"`
				Capacity map[string]int `json:"capacity"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "east-1a" || got.Address != "10.0.0.12" || got.Capacity["cpu"] != 8 || got.Capacity["projects"] != 10 {
				t.Fatalf("unexpected host payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"host_1","name":"east-1a"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/hosts/host_1":
			_, _ = w.Write([]byte(`{"id":"host_1","name":"east-1a"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/hosts/host_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"hosts", "create", "--name", "east-1a", "--address", "10.0.0.12", "--cpu", "8", "--ram-mb", "32768", "--disk-gb", "500", "--disk-iops", "24000", "--projects", "10"},
		{"hosts", "get", "--id", "host_1"},
		{"hosts", "delete", "--id", "host_1", "--yes"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("exit=%d stderr=%s for %v", exitCode, stderr.String(), args)
		}
	}

	expected := []string{
		"POST /v1/hosts",
		"GET /v1/hosts/host_1",
		"DELETE /v1/hosts/host_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestOrgsDeleteRequiresConfirmation(t *testing.T) {
	var stderr strings.Builder
	exitCode := Runner{
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": "http://example.test"},
	}.Run(context.Background(), []string{"orgs", "delete", "--id", "org_1"})

	if exitCode != 1 {
		t.Fatalf("exit=%d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "requires --yes") {
		t.Fatalf("expected confirmation error, got %q", stderr.String())
	}
}

func TestProjectsDestroyRequiresConfirmation(t *testing.T) {
	var stderr strings.Builder
	exitCode := Runner{
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": "http://example.test"},
	}.Run(context.Background(), []string{"projects", "destroy", "--ref", "alpha"})

	if exitCode != 1 {
		t.Fatalf("exit=%d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "requires --yes") {
		t.Fatalf("expected confirmation error, got %q", stderr.String())
	}
}

func TestProjectsDestroyCanRequestRetainedVolumes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/projects/alpha" || r.URL.Query().Get("retain_volumes") != "true" {
			t.Fatalf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"projects", "destroy", "--ref", "alpha", "--yes", "--retain-volumes"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestProjectsListUsesFleetEndpointByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"ref":"alpha","org_id":"org_1"}]`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"projects", "list"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ref": "alpha"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestProjectsListUsesOrgEndpointWhenOrgIDProvided(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/orgs/org_1/projects" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"ref":"alpha","org_id":"org_1"}]`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"projects", "list", "--org-id", "org_1"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestProjectsCLIProfileSupportsEnvAndTOMLOutput(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/connect/cli" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"env": {
				"SUPABASE_URL": "https://alpha.supadupa.test",
				"SUPABASE_SERVICE_ROLE_KEY": "secret://projects/alpha/service_role",
				"SUPABASE_DB_URL": "postgres://postgres:${DB_PASSWORD}@db.alpha.internal:5432/postgres?sslmode=require"
			},
			"supabase_config_toml": "project_id = \"alpha\"\n\n[supadupa]\napi_url = \"https://alpha.supadupa.test\"\n"
		}`))
	}))
	defer server.Close()

	for _, tc := range []struct {
		format string
		want   string
	}{
		{
			format: "env",
			want:   "SUPABASE_DB_URL='postgres://postgres:${DB_PASSWORD}@db.alpha.internal:5432/postgres?sslmode=require'\nSUPABASE_SERVICE_ROLE_KEY='secret://projects/alpha/service_role'\nSUPABASE_URL='https://alpha.supadupa.test'\n",
		},
		{
			format: "toml",
			want:   "project_id = \"alpha\"\n\n[supadupa]\napi_url = \"https://alpha.supadupa.test\"\n",
		},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), []string{"projects", "cli-profile", "--ref", "alpha", "--format", tc.format})
		if exitCode != 0 {
			t.Fatalf("exit=%d stderr=%s for format %s", exitCode, stderr.String(), tc.format)
		}
		if stdout.String() != tc.want {
			t.Fatalf("unexpected %s output:\n%s", tc.format, stdout.String())
		}
	}
	if strings.Join(requests, "\n") != "GET /v1/projects/alpha/connect/cli\nGET /v1/projects/alpha/connect/cli" {
		t.Fatalf("unexpected requests:\n%s", strings.Join(requests, "\n"))
	}
}

func TestSettingsDefaultsSetPostsPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/settings/defaults" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["domain"] != "apps.example.com" || got["stack_version"] != "2026.06.05" || got["profile"] != "essential" || got["resource_tier"] != "medium" || got["backup_schedule"] != "hourly" {
			t.Fatalf("unexpected settings payload %#v", got)
		}
		smtp, ok := got["smtp"].(map[string]any)
		if !ok {
			t.Fatalf("expected smtp payload, got %#v", got["smtp"])
		}
		if smtp["enabled"] != true || smtp["host"] != "smtp.example.com" || smtp["port"] != float64(2525) || smtp["password_handle"] != "secret://platform/smtp-password" || smtp["tls_mode"] != "implicit" {
			t.Fatalf("unexpected smtp payload %#v", smtp)
		}
		_, _ = w.Write([]byte(`{"domain":"apps.example.com","stack_version":"2026.06.05","profile":"essential","resource_tier":"medium","backup_schedule":"hourly","smtp":{"enabled":true,"host":"smtp.example.com","port":2525,"sender_name":"supadupa","sender_email":"noreply@example.com","username":"apikey","password_handle":"secret://platform/smtp-password","tls_mode":"implicit"}}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"settings", "defaults", "set", "--domain", "apps.example.com", "--stack-version", "2026.06.05", "--profile", "essential", "--tier", "medium", "--backup-schedule", "hourly", "--smtp-enabled", "--smtp-host", "smtp.example.com", "--smtp-port", "2525", "--smtp-sender-name", "supadupa", "--smtp-sender-email", "noreply@example.com", "--smtp-username", "apikey", "--smtp-password-handle", "secret://platform/smtp-password", "--smtp-tls-mode", "implicit"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"domain": "apps.example.com"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestSettingsDefaultsGetUsesEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/settings/defaults" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"domain":"supadupa.test"}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"settings", "defaults", "get"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestSettingsSSOSetPostsPayload(t *testing.T) {
	certFile := filepath.Join(t.TempDir(), "idp.pem")
	if err := os.WriteFile(certFile, []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/settings/sso" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["enabled"] != true || got["idp_entity_id"] != "https://idp.example.com/saml" || got["sso_url"] != "https://idp.example.com/login" || got["acs_url"] != "https://supadupa.example.com/v1/auth/sso/saml/callback" || got["email_domain"] != "example.com" || got["auto_provision"] != true || got["default_role"] != "developer" {
			t.Fatalf("unexpected sso payload %#v", got)
		}
		if !strings.Contains(got["certificate_pem"].(string), "BEGIN CERTIFICATE") {
			t.Fatalf("expected certificate payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"enabled":true,"provider":"saml","idp_entity_id":"https://idp.example.com/saml"}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"settings", "sso", "set", "--enabled", "--idp-entity-id", "https://idp.example.com/saml", "--sso-url", "https://idp.example.com/login", "--certificate-file", certFile, "--acs-url", "https://supadupa.example.com/v1/auth/sso/saml/callback", "--email-domain", "example.com", "--auto-provision", "--default-role", "developer"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"provider": "saml"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestSettingsSSOGetUsesEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/settings/sso" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"enabled":false,"provider":"saml"}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"settings", "sso", "get"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestServicesSetPostsBooleanPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/projects/alpha/services" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got struct {
			Services map[string]bool `json:"services"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Services["storage"] || !got.Services["functions"] || got.Services["studio"] {
			t.Fatalf("unexpected services payload %#v", got.Services)
		}
		_, _ = w.Write([]byte(`{"project_ref":"alpha","services":{"storage":false,"functions":true,"studio":false}}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"services", "set", "--ref", "alpha", "--service", "storage=false,functions=true", "--service", "studio=off"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"storage": false`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestTeamsCreatePostsPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/orgs/org_1/teams" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]string
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["name"] != "Developers" || got["slug"] != "developers" {
			t.Fatalf("unexpected team payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"name":"Developers","slug":"developers"}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"teams", "create", "--org-id", "org_1", "--name", "Developers", "--slug", "developers"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestAccessGrantPostsPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/projects/alpha/access" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]string
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["subject_type"] != "team" || got["subject_id"] != "developers" || got["role"] != "developer" {
			t.Fatalf("unexpected access payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"project_ref":"alpha","subject_type":"team","subject_name":"Developers","role":"developer"}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"access", "grant", "--ref", "alpha", "--subject-type", "team", "--subject-id", "developers", "--role", "developer"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestAccessReviewUsesOrgEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/orgs/org_1/access-review" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"org_id":"org_1","projects":[]}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"access", "review", "--org-id", "org_1"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"org_id": "org_1"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestProjectsScalePostsTierPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/projects/alpha/scale" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]string
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["resource_tier"] != "large" {
			t.Fatalf("unexpected scale payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"ref":"alpha","spec":{"resource_tier":"large"}}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"projects", "scale", "--ref", "alpha", "--tier", "large"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"resource_tier": "large"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestHostsCreatePostsCapacityPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/hosts" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got struct {
			Name     string `json:"name"`
			Address  string `json:"address"`
			Capacity struct {
				CPU      int `json:"cpu"`
				RAMMB    int `json:"ram_mb"`
				DiskGB   int `json:"disk_gb"`
				DiskIOPS int `json:"disk_iops"`
				Projects int `json:"projects"`
			} `json:"capacity"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Name != "box-one" || got.Address != "10.0.0.10" || got.Capacity.CPU != 8 || got.Capacity.RAMMB != 32768 || got.Capacity.DiskGB != 500 || got.Capacity.DiskIOPS != 24000 || got.Capacity.Projects != 12 {
			t.Fatalf("unexpected host payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"name":"box-one","address":"10.0.0.10"}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"hosts", "create", "--name", "box-one", "--address", "10.0.0.10", "--cpu", "8", "--ram-mb", "32768", "--disk-gb", "500", "--disk-iops", "24000", "--projects", "12"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestQuotasSetPostsQuotaPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/orgs/org_1/quotas" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]int
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["max_projects"] != 4 || got["max_cpu"] != 16 || got["max_ram_mb"] != 65536 || got["max_disk_gb"] != 1000 || got["max_disk_iops"] != 24000 {
			t.Fatalf("unexpected quota payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"org_id":"org_1","max_projects":4}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"quotas", "set", "--org-id", "org_1", "--max-projects", "4", "--max-cpu", "16", "--max-ram-mb", "65536", "--max-disk-gb", "1000", "--max-disk-iops", "24000"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestUsageCommandsUseOrgEndpoints(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/scim/v2/Users":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			extension, ok := got["urn:supadupa:params:scim:schemas:extension:User"].(map[string]any)
			emails, _ := got["emails"].([]any)
			if !ok || got["userName"] != "dev@example.com" || got["displayName"] != "Dev User" || got["active"] != true || got["password"] != "initial-secret" || extension["role"] != "developer" || len(emails) != 1 {
				t.Fatalf("unexpected SCIM user create payload %#v", got)
			}
		case r.Method == http.MethodPut && r.URL.Path == "/v1/scim/v2/Users/usr_1":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			extension, ok := got["urn:supadupa:params:scim:schemas:extension:User"].(map[string]any)
			if !ok || got["userName"] != "admin@example.com" || got["active"] != false || extension["role"] != "admin" {
				t.Fatalf("unexpected SCIM user replace payload %#v", got)
			}
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/scim/v2/Users/usr_1":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			operations, _ := got["Operations"].([]any)
			if len(operations) != 1 {
				t.Fatalf("unexpected SCIM deprovision payload %#v", got)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/scim/v2/Groups":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			extension, ok := got["urn:supadupa:params:scim:schemas:extension:Group"].(map[string]any)
			members, _ := got["members"].([]any)
			if !ok || got["externalId"] != "org_1" || got["displayName"] != "Developers" || extension["slug"] != "developers" || len(members) != 2 {
				t.Fatalf("unexpected SCIM group create payload %#v", got)
			}
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	runner := Runner{
		Stdout: &strings.Builder{},
		Stderr: &strings.Builder{},
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}
	for _, args := range [][]string{
		{"usage", "current", "--org-id", "org_1"},
		{"usage", "snapshot", "--org-id", "org_1"},
		{"usage", "snapshots", "--org-id", "org_1", "--limit", "2"},
	} {
		if exitCode := runner.Run(context.Background(), args); exitCode != 0 {
			t.Fatalf("exit=%d for %v", exitCode, args)
		}
	}

	expected := []string{
		"GET /v1/orgs/org_1/usage",
		"POST /v1/orgs/org_1/usage/snapshots",
		"GET /v1/orgs/org_1/usage/snapshots?limit=2",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestBillingCommandsUseOrgEndpoints(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["usage_snapshot_id"] != "snap_1" || got["currency"] != "USD" || got["status"] != "draft" || got["due_days"] != float64(14) {
				t.Fatalf("unexpected billing payload %#v", got)
			}
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	runner := Runner{
		Stdout: &strings.Builder{},
		Stderr: &strings.Builder{},
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}
	for _, args := range [][]string{
		{"billing", "invoices", "--org-id", "org_1", "--limit", "2"},
		{"billing", "create-invoice", "--org-id", "org_1", "--usage-snapshot-id", "snap_1", "--currency", "USD", "--status", "draft", "--due-days", "14"},
		{"billing", "get-invoice", "--org-id", "org_1", "--invoice-id", "inv_1"},
	} {
		if exitCode := runner.Run(context.Background(), args); exitCode != 0 {
			t.Fatalf("exit=%d for %v", exitCode, args)
		}
	}

	expected := []string{
		"GET /v1/orgs/org_1/billing/invoices?limit=2",
		"POST /v1/orgs/org_1/billing/invoices",
		"GET /v1/orgs/org_1/billing/invoices/inv_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestMFACommandsUseAccountEndpoints(t *testing.T) {
	var seen []string
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if (r.Method == http.MethodPost && r.URL.Path == "/v1/account/mfa/verify") || (r.Method == http.MethodDelete && r.URL.Path == "/v1/account/mfa") {
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["code"] != "123456" {
				t.Fatalf("unexpected mfa payload %#v", got)
			}
		}
		_, _ = w.Write([]byte(`{"enabled":true,"pending":false}`))
	}))
	defer server.Close()

	runner := Runner{
		Stdout: &strings.Builder{},
		Stderr: &strings.Builder{},
		Env: map[string]string{
			"SUPADUPA_API_URL": server.URL,
			"SUPADUPA_TOKEN":   "test-token",
		},
	}
	for _, args := range [][]string{
		{"mfa", "status"},
		{"mfa", "enroll"},
		{"mfa", "verify", "--code", "123456"},
		{"mfa", "disable", "--code", "123456"},
	} {
		if exitCode := runner.Run(context.Background(), args); exitCode != 0 {
			t.Fatalf("exit=%d for %v", exitCode, args)
		}
	}

	expected := []string{
		"GET /v1/account/mfa",
		"POST /v1/account/mfa/enroll",
		"POST /v1/account/mfa/verify",
		"DELETE /v1/account/mfa",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
	if authHeader != "Bearer test-token" {
		t.Fatalf("expected bearer token forwarding, got %q", authHeader)
	}
}

func TestSCIMCommandsUseSCIMEndpoints(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/scim/v2/Users":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			extension, ok := got["urn:supadupa:params:scim:schemas:extension:User"].(map[string]any)
			emails, _ := got["emails"].([]any)
			if !ok || got["userName"] != "dev@example.com" || got["displayName"] != "Dev User" || got["active"] != true || got["password"] != "initial-secret" || extension["role"] != "developer" || len(emails) != 1 {
				t.Fatalf("unexpected SCIM user create payload %#v", got)
			}
		case r.Method == http.MethodPut && r.URL.Path == "/v1/scim/v2/Users/usr_1":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			extension, ok := got["urn:supadupa:params:scim:schemas:extension:User"].(map[string]any)
			if !ok || got["userName"] != "admin@example.com" || got["active"] != false || extension["role"] != "admin" {
				t.Fatalf("unexpected SCIM user replace payload %#v", got)
			}
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/scim/v2/Users/usr_1":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			operations, _ := got["Operations"].([]any)
			if len(operations) != 1 {
				t.Fatalf("unexpected SCIM deprovision payload %#v", got)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/scim/v2/Groups":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			extension, ok := got["urn:supadupa:params:scim:schemas:extension:Group"].(map[string]any)
			members, _ := got["members"].([]any)
			if !ok || got["externalId"] != "org_1" || got["displayName"] != "Developers" || extension["slug"] != "developers" || len(members) != 2 {
				t.Fatalf("unexpected SCIM group create payload %#v", got)
			}
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	runner := Runner{
		Stdout: &strings.Builder{},
		Stderr: &strings.Builder{},
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}
	for _, args := range [][]string{
		{"scim", "service-provider-config"},
		{"scim", "users"},
		{"scim", "users", "--id", "usr_1"},
		{"scim", "create-user", "--user-name", "dev@example.com", "--display-name", "Dev User", "--password", "initial-secret", "--role", "developer"},
		{"scim", "replace-user", "--id", "usr_1", "--email", "admin@example.com", "--role", "admin", "--active=false"},
		{"scim", "deprovision-user", "--id", "usr_1"},
		{"scim", "delete-user", "--id", "usr_1"},
		{"scim", "groups", "--org-id", "org_1"},
		{"scim", "groups", "--id", "team_1"},
		{"scim", "create-group", "--org-id", "org_1", "--display-name", "Developers", "--slug", "developers", "--member", "dev@example.com", "--member", "admin@example.com"},
		{"scim", "delete-group", "--id", "team_1"},
	} {
		if exitCode := runner.Run(context.Background(), args); exitCode != 0 {
			t.Fatalf("exit=%d for %v", exitCode, args)
		}
	}

	expected := []string{
		"GET /v1/scim/v2/ServiceProviderConfig",
		"GET /v1/scim/v2/Users",
		"GET /v1/scim/v2/Users/usr_1",
		"POST /v1/scim/v2/Users",
		"PUT /v1/scim/v2/Users/usr_1",
		"PATCH /v1/scim/v2/Users/usr_1",
		"DELETE /v1/scim/v2/Users/usr_1",
		"GET /v1/scim/v2/Groups?org_id=org_1",
		"GET /v1/scim/v2/Groups/team_1",
		"POST /v1/scim/v2/Groups",
		"DELETE /v1/scim/v2/Groups/team_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestProvisionerCommandUsesProvisionerEndpoint(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"provisioner":"compose"}`))
	}))
	defer server.Close()

	runner := Runner{
		Stdout: &strings.Builder{},
		Stderr: &strings.Builder{},
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}
	if exitCode := runner.Run(context.Background(), []string{"provisioner", "status"}); exitCode != 0 {
		t.Fatalf("exit=%d for provisioner status", exitCode)
	}
	if strings.Join(seen, "\n") != "GET /v1/provisioner" {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestStorageBucketCommandsUseProjectEndpoints(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var got struct {
				Name             string            `json:"name"`
				Public           bool              `json:"public"`
				FileSizeLimit    int64             `json:"file_size_limit"`
				AllowedMimeTypes []string          `json:"allowed_mime_types"`
				CacheControl     string            `json:"cache_control"`
				Avif             bool              `json:"avif_autodetection"`
				Metadata         map[string]string `json:"metadata"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "assets" || !got.Public || got.FileSizeLimit != 1048576 || got.CacheControl != "600" || !got.Avif || got.Metadata["purpose"] != "public-assets" {
				t.Fatalf("unexpected storage bucket payload %#v", got)
			}
			if strings.Join(got.AllowedMimeTypes, ",") != "image/png,image/jpeg" {
				t.Fatalf("unexpected mime types %#v", got.AllowedMimeTypes)
			}
			_, _ = w.Write([]byte(`{"id":"bucket_1","name":"assets"}`))
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"storage-buckets", "list", "--ref", "alpha"},
		{"storage-buckets", "create", "--ref", "alpha", "--name", "assets", "--public", "--file-size-limit", "1048576", "--mime-type", "image/png,image/jpeg", "--cache-control", "600", "--avif", "--metadata", "purpose=public-assets"},
		{"storage-buckets", "delete", "--ref", "alpha", "--name", "assets"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("exit=%d stderr=%s for %v", exitCode, stderr.String(), args)
		}
	}
	expected := []string{
		"GET /v1/projects/alpha/storage/buckets",
		"POST /v1/projects/alpha/storage/buckets",
		"DELETE /v1/projects/alpha/storage/buckets/assets",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestMembersUpsertPostsMemberPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/orgs/org_1/members" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]string
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["email"] != "dev@example.com" || got["role"] != "developer" {
			t.Fatalf("unexpected member payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"email":"dev@example.com","role":"developer"}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"members", "upsert", "--org-id", "org_1", "--email", "dev@example.com", "--role", "developer"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestUsersCreatePostsPlatformUserPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/users" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]string
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["email"] != "admin@example.com" || got["password"] != "initial-secret" || got["role"] != "admin" {
			t.Fatalf("unexpected user payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"email":"admin@example.com","role":"admin"}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"users", "create", "--email", "admin@example.com", "--password", "initial-secret", "--role", "admin"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"role": "admin"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestUsersListGetsPlatformUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/users" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"email":"admin@example.com","role":"admin"}]`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"users", "list"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"email": "admin@example.com"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestConfigSetPostsConfigPayload(t *testing.T) {
	cases := []struct {
		name     string
		area     string
		args     []string
		expected map[string]string
		response string
	}{
		{
			name: "realtime",
			area: "realtime",
			args: []string{"--set", "broadcast_replay=true", "--set", "broadcast_from_database=true"},
			expected: map[string]string{
				"broadcast_replay":        "true",
				"broadcast_from_database": "true",
			},
			response: `{"area":"realtime","config":{"broadcast_replay":"true","broadcast_from_database":"true"}}`,
		},
		{
			name: "pooler",
			area: "pooler",
			args: []string{"--set", "dedicated_pooler_enabled=true", "--set", "pool_mode=both", "--set", "transaction_port=7654"},
			expected: map[string]string{
				"dedicated_pooler_enabled": "true",
				"pool_mode":                "both",
				"transaction_port":         "7654",
			},
			response: `{"area":"pooler","config":{"dedicated_pooler_enabled":"true","pool_mode":"both","transaction_port":"7654"}}`,
		},
		{
			name: "smtp",
			area: "smtp",
			args: []string{"--set", "enabled=true", "--set", "host=smtp.example.com", "--set", "username=apikey", "--set", "password_handle=secret://projects/alpha/smtp-password", "--set", "tls_mode=implicit"},
			expected: map[string]string{
				"enabled":         "true",
				"host":            "smtp.example.com",
				"username":        "apikey",
				"password_handle": "secret://projects/alpha/smtp-password",
				"tls_mode":        "implicit",
			},
			response: `{"area":"smtp","config":{"enabled":"true","host":"smtp.example.com","username":"apikey","password_handle":"secret://projects/alpha/smtp-password","tls_mode":"implicit"}}`,
		},
		{
			name: "auth phone mfa",
			area: "auth",
			args: []string{"--set", "mfa_totp_enroll_enabled=true", "--set", "mfa_totp_verify_enabled=true", "--set", "mfa_phone_enroll_enabled=true", "--set", "mfa_phone_verify_enabled=true", "--set", "mfa_phone_otp_length=8", "--set", "mfa_phone_max_frequency=20s"},
			expected: map[string]string{
				"mfa_totp_enroll_enabled":  "true",
				"mfa_totp_verify_enabled":  "true",
				"mfa_phone_enroll_enabled": "true",
				"mfa_phone_verify_enabled": "true",
				"mfa_phone_otp_length":     "8",
				"mfa_phone_max_frequency":  "20s",
			},
			response: `{"area":"auth","config":{"mfa_totp_enroll_enabled":"true","mfa_totp_verify_enabled":"true","mfa_phone_enroll_enabled":"true","mfa_phone_verify_enabled":"true","mfa_phone_otp_length":"8","mfa_phone_max_frequency":"20s"}}`,
		},
		{
			name: "email notification templates",
			area: "email_templates",
			args: []string{"--set", "notification_password_changed_subject=Password changed", "--set", "notification_identity_linked_body=Identity linked"},
			expected: map[string]string{
				"notification_password_changed_subject": "Password changed",
				"notification_identity_linked_body":     "Identity linked",
			},
			response: `{"area":"email_templates","config":{"notification_password_changed_subject":"Password changed","notification_identity_linked_body":"Identity linked"}}`,
		},
		{
			name: "auth providers oidc",
			area: "auth_providers",
			args: []string{"--set", "oauth_oidc_enabled=true", "--set", "oauth_oidc_issuer_url=https://issuer.example.com", "--set", "oauth_oidc_client_id=oidc-client", "--set", "oauth_oidc_client_secret_handle=secret://projects/alpha/oidc", "--set", "oauth_discord_enabled=true", "--set", "oauth_discord_client_secret_handle=secret://projects/alpha/discord", "--set", "sms_provider=messagebird", "--set", "sms_messagebird_originator=Supadupa", "--set", "sms_messagebird_access_key_handle=secret://projects/alpha/messagebird"},
			expected: map[string]string{
				"oauth_oidc_enabled":                 "true",
				"oauth_oidc_issuer_url":              "https://issuer.example.com",
				"oauth_oidc_client_id":               "oidc-client",
				"oauth_oidc_client_secret_handle":    "secret://projects/alpha/oidc",
				"oauth_discord_enabled":              "true",
				"oauth_discord_client_secret_handle": "secret://projects/alpha/discord",
				"sms_provider":                       "messagebird",
				"sms_messagebird_originator":         "Supadupa",
				"sms_messagebird_access_key_handle":  "secret://projects/alpha/messagebird",
			},
			response: `{"area":"auth_providers","config":{"oauth_oidc_enabled":"true","oauth_oidc_issuer_url":"https://issuer.example.com","oauth_oidc_client_id":"oidc-client","oauth_oidc_client_secret_handle":"secret://projects/alpha/oidc","oauth_discord_enabled":"true","oauth_discord_client_secret_handle":"secret://projects/alpha/discord","sms_provider":"messagebird","sms_messagebird_originator":"Supadupa","sms_messagebird_access_key_handle":"secret://projects/alpha/messagebird"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/v1/projects/alpha/config/"+tc.area {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				var got struct {
					Config map[string]string `json:"config"`
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				for key, value := range tc.expected {
					if got.Config[key] != value {
						t.Fatalf("expected %s=%s in payload %#v", key, value, got.Config)
					}
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()

			args := append([]string{"config", "set", "--ref", "alpha", "--area", tc.area}, tc.args...)
			var stdout, stderr strings.Builder
			exitCode := Runner{
				Stdout: &stdout,
				Stderr: &stderr,
				Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
			}.Run(context.Background(), args)

			if exitCode != 0 {
				t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
			}
		})
	}
}

func TestDomainsLogDrainsAndSecretsUseProjectEndpoints(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		seen[key] = true
		switch key {
		case "POST /v1/projects/alpha/domains":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["fqdn"] != "api.example.com" {
				t.Fatalf("unexpected domain payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"fqdn":"api.example.com"}`))
		case "POST /v1/projects/alpha/log-drains":
			var got struct {
				Target string            `json:"target"`
				Config map[string]string `json:"config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Target != "https" || got.Config["url"] != "https://logs.example.com/ingest" || got.Config["token"] != "secret" {
				t.Fatalf("unexpected log drain payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"drain_1","target":"https"}`))
		case "GET /v1/projects/alpha/secrets/service_role/reveal":
			_, _ = w.Write([]byte(`{"kind":"service_role","value":"secret"}`))
		case "POST /v1/projects/alpha/secrets/service_role/copy":
			w.WriteHeader(http.StatusNoContent)
		case "POST /v1/projects/alpha/keys/rotate":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["kind"] != "service_role" {
				t.Fatalf("unexpected secret rotation payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"kind":"service_role","masked":"svc_..."}`))
		default:
			t.Fatalf("unexpected request %s", key)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"domains", "add", "--ref", "alpha", "--fqdn", "api.example.com"},
		{"log-drains", "create", "--ref", "alpha", "--target", "https", "--config", "url=https://logs.example.com/ingest", "--config", "token=secret"},
		{"secrets", "reveal", "--ref", "alpha", "--kind", "service_role"},
		{"secrets", "copy", "--ref", "alpha", "--kind", "service_role"},
		{"secrets", "rotate", "--ref", "alpha", "--kind", "service_role"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("%v exit=%d stderr=%s", args, exitCode, stderr.String())
		}
	}
	for _, expected := range []string{
		"POST /v1/projects/alpha/domains",
		"POST /v1/projects/alpha/log-drains",
		"GET /v1/projects/alpha/secrets/service_role/reveal",
		"POST /v1/projects/alpha/secrets/service_role/copy",
		"POST /v1/projects/alpha/keys/rotate",
	} {
		if !seen[expected] {
			t.Fatalf("expected request %s", expected)
		}
	}
}

func TestRoutesCommandUsesProjectRoutesEndpoint(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/routes" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"route_1","fqdn":"alpha.supadupa.test","upstream_url":"http://alpha-kong:8000","tls":true}]`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"routes", "list", "--ref", "alpha"})
	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"fqdn": "alpha.supadupa.test"`) || !strings.Contains(stdout.String(), `"tls": true`) {
		t.Fatalf("expected routes response, got %s", stdout.String())
	}
	if strings.Join(requests, "\n") != "GET /v1/projects/alpha/routes" {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestBranchesCreatePostsBranchPayload(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/source/branches/source-preview" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/projects/source/branches" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["ref"] != "source-preview" || got["name"] != "Preview" || got["ttl_hours"].(float64) != 12 {
			t.Fatalf("unexpected branch payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"branch":{"project_ref":"source-preview"},"project":{"ref":"source-preview"}}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"branches", "create", "--ref", "source", "--branch-ref", "source-preview", "--name", "Preview", "--ttl-hours", "12"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"project_ref": "source-preview"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"branches", "delete", "--ref", "source", "--branch-ref", "source-preview"})
	if exitCode != 0 {
		t.Fatalf("delete exit=%d stderr=%s", exitCode, stderr.String())
	}
	expected := strings.Join([]string{
		"POST /v1/projects/source/branches",
		"DELETE /v1/projects/source/branches/source-preview",
	}, "\n")
	if strings.Join(requests, "\n") != expected {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestReplicasCreatePostsReplicaPayload(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/source/replicas/routing":
			_, _ = w.Write([]byte(`{"read_strategy":"weighted-healthy"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/source/replicas":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["name"] != "east" || got["host_id"] != "host-one" || got["region"] != "us-east" || got["tier"] != "medium" || got["read_weight"] != float64(75) || got["failover_priority"] != float64(2) {
				t.Fatalf("unexpected replica payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"replica_1","name":"east","status":"healthy"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/source/replicas/replica_1/promote":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["reason"] != "planned maintenance" {
				t.Fatalf("unexpected promote payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"replica_1","role":"primary"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/source/replicas/failover":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["reason"] != "primary degraded" {
				t.Fatalf("unexpected failover payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"replica_1","role":"primary"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/source/replicas/replica_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"replicas", "routing", "--ref", "source"},
		{"replicas", "create", "--ref", "source", "--name", "east", "--host-id", "host-one", "--region", "us-east", "--tier", "medium", "--read-weight", "75", "--failover-priority", "2"},
		{"replicas", "promote", "--ref", "source", "--id", "replica_1", "--reason", "planned maintenance"},
		{"replicas", "failover", "--ref", "source", "--reason", "primary degraded"},
		{"replicas", "delete", "--ref", "source", "--id", "replica_1"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("exit=%d stderr=%s for %v", exitCode, stderr.String(), args)
		}
	}
	expected := []string{
		"GET /v1/projects/source/replicas/routing",
		"POST /v1/projects/source/replicas",
		"POST /v1/projects/source/replicas/replica_1/promote",
		"POST /v1/projects/source/replicas/failover",
		"DELETE /v1/projects/source/replicas/replica_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestBackupsRestorePostsBackupID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/projects/alpha/restore" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got map[string]string
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["backup_id"] != "bkp_123" {
			t.Fatalf("unexpected restore payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"restore_state":"completed","backup":{"id":"bkp_123"}}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"backups", "restore", "--ref", "alpha", "--backup-id", "bkp_123"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"restore_state": "completed"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestBackupsPolicyCommandsUsePolicyEndpoint(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		seen[key] = true
		switch key {
		case "GET /v1/projects/alpha/backups/policy":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"schedule":"daily","kind":"logical"}`))
		case "PUT /v1/projects/alpha/backups/policy":
			var got struct {
				Enabled  bool   `json:"enabled"`
				Schedule string `json:"schedule"`
				Kind     string `json:"kind"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if !got.Enabled || got.Schedule != "0 2 * * *" || got.Kind != "physical" {
				t.Fatalf("unexpected backup policy payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"schedule":"0 2 * * *","kind":"physical"}`))
		default:
			t.Fatalf("unexpected request %s", key)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"backups", "policy", "--ref", "alpha"},
		{"backups", "set-policy", "--ref", "alpha", "--enabled", "--schedule", "0 2 * * *", "--kind", "physical"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("%v exit=%d stderr=%s", args, exitCode, stderr.String())
		}
	}
	for _, expected := range []string{
		"GET /v1/projects/alpha/backups/policy",
		"PUT /v1/projects/alpha/backups/policy",
	} {
		if !seen[expected] {
			t.Fatalf("expected request %s", expected)
		}
	}
}

func TestPITRSetPolicyPostsPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/projects/alpha/pitr/policy" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got struct {
			Enabled       bool   `json:"enabled"`
			ArchiveBucket string `json:"archive_bucket"`
			RetentionDays int    `json:"retention_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if !got.Enabled || got.ArchiveBucket != "s3://archive/alpha" || got.RetentionDays != 14 {
			t.Fatalf("unexpected PITR payload %#v", got)
		}
		_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"archive_bucket":"s3://archive/alpha","retention_days":14}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"pitr", "set-policy", "--ref", "alpha", "--enabled", "--archive-bucket", "s3://archive/alpha", "--retention-days", "14"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"retention_days": 14`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestPITRArchiveUsesWALEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/projects/alpha/pitr/wal" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"wal_1","status":"archived"}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"pitr", "archive", "--ref", "alpha"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "archived"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestMetricsPrometheusUsesTextEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/plain" {
			t.Fatalf("unexpected Accept %q", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte("supadupa_projects_total 3\n"))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"metrics", "--prometheus"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if stdout.String() != "supadupa_projects_total 3\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestMetricsRefUsesProjectEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/metrics" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"project_ref":"alpha","routes":2}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"metrics", "--ref", "alpha"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"project_ref": "alpha"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestAdvisorUsesFleetEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/advisor" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"project_ref":"alpha","severity":"high","title":"Database SSL is not enforced"}]`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"advisor"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"title": "Database SSL is not enforced"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestComplianceReportUsesFleetEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/compliance/report" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"frameworks":["SOC 2","HIPAA"],"summary":{"passed":1,"total":2},"controls":[{"id":"COM-001","status":"pass"}]}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"compliance", "report"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"frameworks": [`) || !strings.Contains(stdout.String(), `"COM-001"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestReplicationCommandsUseProjectEndpoints(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/replication":
			_, _ = w.Write([]byte(`[{"id":"pipe_1","name":"orders-etl","destination":"s3"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/replication":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			config, _ := got["config"].(map[string]any)
			if got["name"] != "orders-etl" || got["type"] != "etl" || got["source_schema"] != "public" || got["source_table"] != "orders" || got["destination"] != "s3" || got["credential_handle"] != "secret://projects/alpha/etl" || config["bucket"] != "lake" {
				t.Fatalf("unexpected replication payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"pipe_1","name":"orders-etl"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/replication/pipe_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"replication", "list", "--ref", "alpha"},
		{"replication", "create", "--ref", "alpha", "--name", "orders-etl", "--type", "etl", "--source-schema", "public", "--source-table", "orders", "--destination", "s3", "--credential-handle", "secret://projects/alpha/etl", "--config", "bucket=lake"},
		{"replication", "delete", "--ref", "alpha", "--id", "pipe_1"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("args=%v exit=%d stderr=%s", args, exitCode, stderr.String())
		}
	}

	expected := []string{
		"GET /v1/projects/alpha/replication",
		"POST /v1/projects/alpha/replication",
		"DELETE /v1/projects/alpha/replication/pipe_1",
	}
	if strings.Join(requests, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestVectorAICommandsUseProjectEndpoints(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "refresh.sql")
	if err := os.WriteFile(commandPath, []byte("select analytics.refresh_rollups();"), 0o600); err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join(dir, "20260605_001_app_schema.sql")
	if err := os.WriteFile(schemaPath, []byte("create table public.accounts(id uuid primary key);"), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/embeddings":
			_, _ = w.Write([]byte(`[{"id":"emb_1","name":"docs-embeddings"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/embeddings":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode embedding body: %v", err)
			}
			if got["name"] != "docs-embeddings" || got["source_schema"] != "public" || got["source_table"] != "documents" || got["source_column"] != "body" || got["provider"] != "openai" || got["dimension"] != float64(1536) || got["batch_size"] != float64(100) {
				t.Fatalf("unexpected embedding payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"emb_1","name":"docs-embeddings"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/embeddings/emb_1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/extensions":
			_, _ = w.Write([]byte(`[{"id":"default:pg_cron","name":"pg_cron","enabled":true}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/database/extensions/pg_cron":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode database extension body: %v", err)
			}
			if got["schema"] != "extensions" || got["version"] != "1.6" || got["enabled"] != false {
				t.Fatalf("unexpected database extension payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"ext_1","name":"pg_cron","enabled":false}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/cron-jobs":
			_, _ = w.Write([]byte(`[{"id":"cron_1","name":"refresh-rollups"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/cron-jobs":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode database cron body: %v", err)
			}
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "refresh-rollups" || got["schedule"] != "*/15 * * * *" || got["command"] != "select analytics.refresh_rollups();" || got["database"] != "postgres" || got["username"] != "postgres" || got["active"] != true || got["timeout_seconds"] != float64(90) || got["max_runtime_seconds"] != float64(120) || metadata["owner"] != "analytics" {
				t.Fatalf("unexpected database cron payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"cron_1","name":"refresh-rollups"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/cron-jobs/refresh-rollups":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/queues":
			_, _ = w.Write([]byte(`[{"id":"queue_1","name":"events"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/queues":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode database queue body: %v", err)
			}
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "events" || got["schema"] != "pgmq" || got["retention_minutes"] != float64(10080) || got["visibility_timeout_seconds"] != float64(45) || got["max_retries"] != float64(7) || got["dead_letter_queue"] != "events-dlq" || got["active"] != true || metadata["owner"] != "backend" {
				t.Fatalf("unexpected database queue payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"queue_1","name":"events"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/queues/events":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/webhooks":
			_, _ = w.Write([]byte(`[{"id":"webhook_1","name":"orders-events"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/webhooks":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode database webhook body: %v", err)
			}
			events, _ := got["events"].([]any)
			headers, _ := got["headers"].(map[string]any)
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "orders-events" || got["schema"] != "public" || got["table"] != "orders" || len(events) != 2 || events[0] != "insert" || events[1] != "update" || got["endpoint"] != "https://hooks.example.com/orders" || got["http_method"] != "POST" || got["timeout_seconds"] != float64(15) || got["retry_count"] != float64(5) || got["active"] != true || headers["Authorization"] != "secret://projects/alpha/webhooks/orders-token" || metadata["owner"] != "backend" {
				t.Fatalf("unexpected database webhook payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"webhook_1","name":"orders-events"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/webhooks/orders-events":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/schemas":
			_, _ = w.Write([]byte(`[{"id":"schema_1","name":"app-schema","version":"20260605_001"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/schemas":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode database schema body: %v", err)
			}
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "app-schema" || got["version"] != "20260605_001" || got["schema"] != "public" || got["sql"] != "create table public.accounts(id uuid primary key);" || got["apply_order"] != float64(10) || got["active"] != true || metadata["owner"] != "backend" {
				t.Fatalf("unexpected database schema payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"schema_1","name":"app-schema","version":"20260605_001"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/schemas/app-schema/20260605_001":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/roles":
			_, _ = w.Write([]byte(`[{"id":"role_1","name":"app_writer"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/roles":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode database role body: %v", err)
			}
			memberOf, _ := got["member_of"].([]any)
			grants, _ := got["schema_grants"].(map[string]any)
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "app_writer" || got["login"] != true || got["inherit"] != false || got["bypass_rls"] != true || got["connection_limit"] != float64(25) || got["password_secret_handle"] != "secret://projects/alpha/db/app-writer" || len(memberOf) != 1 || memberOf[0] != "authenticated" || grants["public"] != "usage,select,insert" || metadata["purpose"] != "app" {
				t.Fatalf("unexpected database role payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"role_1","name":"app_writer"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/roles/app_writer":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/vector-buckets":
			_, _ = w.Write([]byte(`[{"id":"vb_1","name":"documents"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/vector-buckets":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode vector bucket body: %v", err)
			}
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "documents" || got["dimension"] != float64(1536) || got["distance"] != "cosine" || got["index_method"] != "hnsw" || got["storage_backend"] != "s3" || got["storage_uri"] != "s3://vectors/documents" || metadata["purpose"] != "search" {
				t.Fatalf("unexpected vector bucket payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"vb_1","name":"documents"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/vector-buckets/documents":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/analytics-buckets":
			_, _ = w.Write([]byte(`[{"id":"ab_1","name":"events"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/analytics-buckets":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode analytics bucket body: %v", err)
			}
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "events" || got["storage_uri"] != "s3://lakehouse/events" || got["catalog_uri"] != "http://iceberg-rest:8181" || got["warehouse"] != "analytics" || got["credential_handle"] != "secret://projects/alpha/iceberg" || got["format_version"] != float64(2) || got["partitioning"] != "days(created_at)" || got["retention_days"] != float64(365) || got["compaction_schedule"] != "0 2 * * *" || metadata["purpose"] != "warehouse" {
				t.Fatalf("unexpected analytics bucket payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"ab_1","name":"events"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/analytics-buckets/events":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"embeddings", "list", "--ref", "alpha"},
		{"embeddings", "create", "--ref", "alpha", "--name", "docs-embeddings", "--source-schema", "public", "--source-table", "documents", "--source-column", "body", "--provider", "openai", "--model", "text-embedding-3-small", "--dimension", "1536", "--batch-size", "100"},
		{"embeddings", "delete", "--ref", "alpha", "--id", "emb_1"},
		{"database-extensions", "list", "--ref", "alpha"},
		{"database-extensions", "set", "--ref", "alpha", "--name", "pg_cron", "--schema", "extensions", "--version", "1.6", "--enabled=false"},
		{"database-cron", "list", "--ref", "alpha"},
		{"database-cron", "create", "--ref", "alpha", "--name", "refresh-rollups", "--schedule", "*/15 * * * *", "--command-file", commandPath, "--database", "postgres", "--username", "postgres", "--timeout-seconds", "90", "--max-runtime-seconds", "120", "--metadata", "owner=analytics"},
		{"database-cron", "delete", "--ref", "alpha", "--name", "refresh-rollups"},
		{"database-queues", "list", "--ref", "alpha"},
		{"database-queues", "create", "--ref", "alpha", "--name", "events", "--schema", "pgmq", "--retention-minutes", "10080", "--visibility-timeout-seconds", "45", "--max-retries", "7", "--dead-letter-queue", "events-dlq", "--metadata", "owner=backend"},
		{"database-queues", "delete", "--ref", "alpha", "--name", "events"},
		{"database-webhooks", "list", "--ref", "alpha"},
		{"database-webhooks", "create", "--ref", "alpha", "--name", "orders-events", "--schema", "public", "--table", "orders", "--events", "insert,update", "--endpoint", "https://hooks.example.com/orders", "--method", "POST", "--timeout-seconds", "15", "--retry-count", "5", "--header", "Authorization=secret://projects/alpha/webhooks/orders-token", "--metadata", "owner=backend"},
		{"database-webhooks", "delete", "--ref", "alpha", "--name", "orders-events"},
		{"database-schemas", "list", "--ref", "alpha"},
		{"database-schemas", "create", "--ref", "alpha", "--name", "app-schema", "--version", "20260605_001", "--schema", "public", "--sql-file", schemaPath, "--apply-order", "10", "--metadata", "owner=backend"},
		{"database-schemas", "delete", "--ref", "alpha", "--name", "app-schema", "--version", "20260605_001"},
		{"database-roles", "list", "--ref", "alpha"},
		{"database-roles", "create", "--ref", "alpha", "--name", "app_writer", "--login", "--no-inherit", "--bypass-rls", "--connection-limit", "25", "--password-secret-handle", "secret://projects/alpha/db/app-writer", "--member-of", "authenticated", "--grant", "public=usage,select,insert", "--metadata", "purpose=app"},
		{"database-roles", "delete", "--ref", "alpha", "--name", "app_writer"},
		{"vector-buckets", "list", "--ref", "alpha"},
		{"vector-buckets", "create", "--ref", "alpha", "--name", "documents", "--dimension", "1536", "--distance", "cosine", "--index-method", "hnsw", "--storage-backend", "s3", "--storage-uri", "s3://vectors/documents", "--metadata", "purpose=search"},
		{"vector-buckets", "delete", "--ref", "alpha", "--name", "documents"},
		{"analytics-buckets", "list", "--ref", "alpha"},
		{"analytics-buckets", "create", "--ref", "alpha", "--name", "events", "--storage-uri", "s3://lakehouse/events", "--catalog-uri", "http://iceberg-rest:8181", "--warehouse", "analytics", "--credential-handle", "secret://projects/alpha/iceberg", "--format-version", "2", "--partitioning", "days(created_at)", "--retention-days", "365", "--compaction-schedule", "0 2 * * *", "--metadata", "purpose=warehouse"},
		{"analytics-buckets", "delete", "--ref", "alpha", "--name", "events"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("args=%v exit=%d stderr=%s", args, exitCode, stderr.String())
		}
	}

	expected := []string{
		"GET /v1/projects/alpha/embeddings",
		"POST /v1/projects/alpha/embeddings",
		"DELETE /v1/projects/alpha/embeddings/emb_1",
		"GET /v1/projects/alpha/database/extensions",
		"PUT /v1/projects/alpha/database/extensions/pg_cron",
		"GET /v1/projects/alpha/database/cron-jobs",
		"POST /v1/projects/alpha/database/cron-jobs",
		"DELETE /v1/projects/alpha/database/cron-jobs/refresh-rollups",
		"GET /v1/projects/alpha/database/queues",
		"POST /v1/projects/alpha/database/queues",
		"DELETE /v1/projects/alpha/database/queues/events",
		"GET /v1/projects/alpha/database/webhooks",
		"POST /v1/projects/alpha/database/webhooks",
		"DELETE /v1/projects/alpha/database/webhooks/orders-events",
		"GET /v1/projects/alpha/database/schemas",
		"POST /v1/projects/alpha/database/schemas",
		"DELETE /v1/projects/alpha/database/schemas/app-schema/20260605_001",
		"GET /v1/projects/alpha/database/roles",
		"POST /v1/projects/alpha/database/roles",
		"DELETE /v1/projects/alpha/database/roles/app_writer",
		"GET /v1/projects/alpha/vector-buckets",
		"POST /v1/projects/alpha/vector-buckets",
		"DELETE /v1/projects/alpha/vector-buckets/documents",
		"GET /v1/projects/alpha/analytics-buckets",
		"POST /v1/projects/alpha/analytics-buckets",
		"DELETE /v1/projects/alpha/analytics-buckets/events",
	}
	if strings.Join(requests, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestCDNCommandsUseProjectEndpoints(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/cdn/policy":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/cdn/policy":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			included, _ := got["included_paths"].([]any)
			excluded, _ := got["excluded_paths"].([]any)
			if got["enabled"] != true || got["browser_ttl_seconds"] != float64(300) || got["edge_ttl_seconds"] != float64(600) || got["stale_while_revalidate_seconds"] != float64(30) || got["smart_revalidation"] != true || len(included) != 1 || included[0] != "/storage/*" || len(excluded) != 1 || excluded[0] != "/storage/private/*" {
				t.Fatalf("unexpected cdn policy payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/cdn/invalidations":
			_, _ = w.Write([]byte(`[{"id":"inv_1","status":"completed"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/cdn/invalidations":
			var got map[string][]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if strings.Join(got["paths"], ",") != "/storage/avatar.png,/storage/*" {
				t.Fatalf("unexpected cdn invalidation payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"inv_1","status":"completed"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/cdn/object-events":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got["event_id"] != "evt-1" || got["bucket"] != "assets" || got["object_path"] != "avatars/user.png" || got["event_type"] != "object_updated" {
				t.Fatalf("unexpected cdn object event payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"inv_2","source":"storage_object_event","status":"completed"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"cdn", "policy", "--ref", "alpha"},
		{"cdn", "set-policy", "--ref", "alpha", "--enabled", "--browser-ttl", "300", "--edge-ttl", "600", "--stale-while-revalidate", "30", "--smart", "--include", "/storage/*", "--exclude", "/storage/private/*"},
		{"cdn", "invalidations", "--ref", "alpha"},
		{"cdn", "invalidate", "--ref", "alpha", "--path", "/storage/avatar.png", "--path", "/storage/*"},
		{"cdn", "object-event", "--ref", "alpha", "--event-id", "evt-1", "--bucket", "assets", "--object-path", "avatars/user.png", "--event-type", "object_updated"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("args=%v exit=%d stderr=%s", args, exitCode, stderr.String())
		}
	}

	expected := []string{
		"GET /v1/projects/alpha/cdn/policy",
		"PUT /v1/projects/alpha/cdn/policy",
		"GET /v1/projects/alpha/cdn/invalidations",
		"POST /v1/projects/alpha/cdn/invalidations",
		"POST /v1/projects/alpha/cdn/object-events",
	}
	if strings.Join(requests, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestNetworkConnectionCommandsUseProjectEndpoints(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/network-connections":
			_, _ = w.Write([]byte(`[{"id":"net_1","name":"aws-prod"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/network-connections":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			cidrs, _ := got["cidrs"].([]any)
			config, _ := got["config"].(map[string]any)
			if got["name"] != "aws-prod" || got["type"] != "privatelink" || got["provider"] != "aws" || got["region"] != "us-east-1" || got["endpoint_id"] != "vpce-123" || len(cidrs) != 2 || cidrs[0] != "10.0.0.0/16" || cidrs[1] != "203.0.113.10" || config["token"] != "secret://projects/alpha/private-link-token" {
				t.Fatalf("unexpected network connection payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"net_1","name":"aws-prod"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/network-connections/net_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"network-connections", "list", "--ref", "alpha"},
		{"network-connections", "create", "--ref", "alpha", "--name", "aws-prod", "--type", "privatelink", "--provider", "aws", "--region", "us-east-1", "--cidr", "10.0.0.0/16", "--cidr", "203.0.113.10", "--endpoint-id", "vpce-123", "--config", "account_id=123456789012", "--config", "token=secret://projects/alpha/private-link-token"},
		{"network-connections", "delete", "--ref", "alpha", "--id", "net_1"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("args=%v exit=%d stderr=%s", args, exitCode, stderr.String())
		}
	}

	expected := []string{
		"GET /v1/projects/alpha/network-connections",
		"POST /v1/projects/alpha/network-connections",
		"DELETE /v1/projects/alpha/network-connections/net_1",
	}
	if strings.Join(requests, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestNetworkCommandUsesProjectNetworkEndpoint(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/network" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"project_ref":"alpha","allowlist":"10.0.0.0/8","ssl_enforced":"true","connections":[{"id":"net_1","name":"aws-prod"}]}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"network", "get", "--ref", "alpha"})
	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"allowlist": "10.0.0.0/8"`) || !strings.Contains(stdout.String(), `"ssl_enforced": "true"`) {
		t.Fatalf("expected network policy response, got %s", stdout.String())
	}
	if strings.Join(requests, "\n") != "GET /v1/projects/alpha/network" {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestAuditCommandsUseAuditEndpoints(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		seen[key] = true
		switch key {
		case "GET /v1/audit-events":
			_, _ = w.Write([]byte(`[{"action":"project.create","chain_index":1}]`))
		case "GET /v1/audit-events/integrity":
			_, _ = w.Write([]byte(`{"verified":true,"events":1,"head_hash":"abc"}`))
		default:
			t.Fatalf("unexpected request %s", key)
		}
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"audit", "list"},
		{"audit", "integrity"},
	} {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("%v exit=%d stderr=%s", args, exitCode, stderr.String())
		}
		if stdout.Len() == 0 {
			t.Fatalf("expected output for %v", args)
		}
	}
	for _, expected := range []string{
		"GET /v1/audit-events",
		"GET /v1/audit-events/integrity",
	} {
		if !seen[expected] {
			t.Fatalf("expected request %s", expected)
		}
	}
}

func TestLogsListUsesProjectLogsEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/logs" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"level":"info","message":"Project created"}]`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"logs", "list", "--ref", "alpha"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"message": "Project created"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestLogsTailUsesProjectLogStreamEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/logs/stream" || r.URL.Query().Get("follow") != "false" {
			t.Fatalf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("expected event stream accept header, got %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: log\ndata: {\"message\":\"Project created\"}\n\n"))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"logs", "tail", "--ref", "alpha", "--once"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "event: log") || !strings.Contains(stdout.String(), "Project created") {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestProjectsActivityUsesProjectActivityEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/activity" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"action":"project.pause","target":"project:alpha"}]`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{"projects", "activity", "--ref", "alpha"})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"target": "project:alpha"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestFunctionsDeployReadsSourceFileAndSecrets(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "index.ts")
	if err := os.WriteFile(sourcePath, []byte("Deno.serve(() => new Response('ok'))"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/projects/demo/functions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var got struct {
			Name       string            `json:"name"`
			Entrypoint string            `json:"entrypoint"`
			VerifyJWT  bool              `json:"verify_jwt"`
			Source     string            `json:"source"`
			Secrets    map[string]string `json:"secrets"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Name != "hello" || got.Entrypoint != "main.ts" || !got.VerifyJWT {
			t.Fatalf("unexpected function payload %#v", got)
		}
		if got.Source != "Deno.serve(() => new Response('ok'))" {
			t.Fatalf("unexpected source %q", got.Source)
		}
		if got.Secrets["API_KEY"] != "one" || got.Secrets["OTHER"] != "two" {
			t.Fatalf("unexpected secrets %#v", got.Secrets)
		}
		_, _ = w.Write([]byte(`{"name":"hello","version":1}`))
	}))
	defer server.Close()

	var stdout, stderr strings.Builder
	exitCode := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
	}.Run(context.Background(), []string{
		"functions", "deploy",
		"--ref", "demo",
		"--name", "hello",
		"--entrypoint", "main.ts",
		"--source-file", sourcePath,
		"--secret", "API_KEY=one",
		"--secret", "OTHER=two",
	})

	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
}

func TestFunctionStorageMountCommandsUseProjectEndpoints(t *testing.T) {
	seen := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/demo/functions/storage-mounts":
			_, _ = w.Write([]byte(`[{"id":"mount_1","function_name":"hello","bucket_name":"assets"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/demo/functions/storage-mounts":
			var got struct {
				FunctionName string `json:"function_name"`
				BucketName   string `json:"bucket_name"`
				MountPath    string `json:"mount_path"`
				ReadOnly     bool   `json:"read_only"`
				Prefix       string `json:"prefix"`
				EnvAlias     string `json:"env_alias"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.FunctionName != "hello" || got.BucketName != "assets" || got.MountPath != "/mnt/assets" || !got.ReadOnly || got.Prefix != "public" || got.EnvAlias != "ASSETS_MOUNT" {
				t.Fatalf("unexpected mount payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"mount_1","function_name":"hello","bucket_name":"assets"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/demo/functions/storage-mounts/mount_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	commands := [][]string{
		{"functions", "mounts", "--ref", "demo"},
		{"functions", "mount", "--ref", "demo", "--function", "hello", "--bucket", "assets", "--mount-path", "/mnt/assets", "--prefix", "public", "--env-alias", "ASSETS_MOUNT"},
		{"functions", "unmount", "--ref", "demo", "--id", "mount_1"},
	}
	for _, args := range commands {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("args=%v exit=%d stderr=%s", args, exitCode, stderr.String())
		}
	}
	for _, expected := range []string{
		"GET /v1/projects/demo/functions/storage-mounts",
		"POST /v1/projects/demo/functions/storage-mounts",
		"DELETE /v1/projects/demo/functions/storage-mounts/mount_1",
	} {
		if !slices.Contains(seen, expected) {
			t.Fatalf("expected request %s in %#v", expected, seen)
		}
	}
}

func TestFunctionRegionCommandsUseProjectEndpoints(t *testing.T) {
	seen := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/demo/functions/regions":
			_, _ = w.Write([]byte(`[{"id":"region_1","function_name":"hello","region":"us-east-1"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/demo/functions/regions":
			var got struct {
				FunctionName  string `json:"function_name"`
				HostID        string `json:"host_id"`
				Region        string `json:"region"`
				RoutingPolicy string `json:"routing_policy"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.FunctionName != "hello" || got.HostID != "host_1" || got.Region != "us-east-1" || got.RoutingPolicy != "nearest" {
				t.Fatalf("unexpected region payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"region_1","function_name":"hello","region":"us-east-1"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/demo/functions/regions/region_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	commands := [][]string{
		{"functions", "regions", "--ref", "demo"},
		{"functions", "region", "--ref", "demo", "--function", "hello", "--host-id", "host_1", "--region", "us-east-1", "--routing-policy", "nearest"},
		{"functions", "unregion", "--ref", "demo", "--id", "region_1"},
	}
	for _, args := range commands {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), args)
		if exitCode != 0 {
			t.Fatalf("args=%v exit=%d stderr=%s", args, exitCode, stderr.String())
		}
	}
	for _, expected := range []string{
		"GET /v1/projects/demo/functions/regions",
		"POST /v1/projects/demo/functions/regions",
		"DELETE /v1/projects/demo/functions/regions/region_1",
	} {
		if !slices.Contains(seen, expected) {
			t.Fatalf("expected request %s in %#v", expected, seen)
		}
	}
}

func TestAuthClientCommandsUseProjectEndpoints(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/demo/auth/clients":
			var got struct {
				Name               string   `json:"name"`
				ClientID           string   `json:"client_id"`
				ClientSecretHandle string   `json:"client_secret_handle"`
				RedirectURIs       []string `json:"redirect_uris"`
				GrantTypes         []string `json:"grant_types"`
				Scopes             []string `json:"scopes"`
				Confidential       bool     `json:"confidential"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "Dashboard App" || got.ClientID != "dashboard_app" || got.ClientSecretHandle != "secret://projects/demo/auth/dashboard" || !got.Confidential {
				t.Fatalf("unexpected auth client payload %#v", got)
			}
			if strings.Join(got.RedirectURIs, ",") != "https://app.example.com/auth/callback,https://app.example.com/auth/alt" {
				t.Fatalf("unexpected redirect uris %#v", got.RedirectURIs)
			}
			if strings.Join(got.GrantTypes, ",") != "authorization_code,refresh_token" {
				t.Fatalf("unexpected grant types %#v", got.GrantTypes)
			}
			if strings.Join(got.Scopes, ",") != "openid,email,profile" {
				t.Fatalf("unexpected scopes %#v", got.Scopes)
			}
			_, _ = w.Write([]byte(`{"client_id":"dashboard_app"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/demo/auth/clients":
			_, _ = w.Write([]byte(`[{"client_id":"dashboard_app"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/demo/auth/clients/dashboard_app":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	commands := [][]string{
		{
			"auth-clients", "create",
			"--ref", "demo",
			"--name", "Dashboard App",
			"--client-id", "dashboard_app",
			"--client-secret-handle", "secret://projects/demo/auth/dashboard",
			"--redirect-uri", "https://app.example.com/auth/callback,https://app.example.com/auth/alt",
			"--grant-type", "authorization_code",
			"--grant-type", "refresh_token",
			"--scope", "openid,email",
			"--scope", "profile",
		},
		{"auth-clients", "list", "--ref", "demo"},
		{"auth-clients", "delete", "--ref", "demo", "--client-id", "dashboard_app"},
	}
	for _, command := range commands {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), command)
		if exitCode != 0 {
			t.Fatalf("command %v exit=%d stderr=%s", command, exitCode, stderr.String())
		}
	}
	expected := strings.Join([]string{
		"POST /v1/projects/demo/auth/clients",
		"GET /v1/projects/demo/auth/clients",
		"DELETE /v1/projects/demo/auth/clients/dashboard_app",
	}, "\n")
	if strings.Join(requests, "\n") != expected {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestAuthHookCommandsUseProjectEndpoints(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/demo/auth/hooks":
			var got struct {
				HookType      string            `json:"hook_type"`
				Enabled       bool              `json:"enabled"`
				TargetURI     string            `json:"target_uri"`
				EdgeFunction  string            `json:"edge_function"`
				SecretHandle  string            `json:"secret_handle"`
				Headers       map[string]string `json:"headers"`
				TimeoutMS     int               `json:"timeout_ms"`
				RetryAttempts int               `json:"retry_attempts"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.HookType != "custom_access_token" || !got.Enabled || got.TargetURI != "https://hooks.example.com/token" || got.EdgeFunction != "token-hook" {
				t.Fatalf("unexpected auth hook payload %#v", got)
			}
			if got.SecretHandle != "secret://projects/demo/auth/hook" || got.Headers["authorization"] != "secret://projects/demo/auth/header" || got.Headers["x-trace"] != "supadupa" {
				t.Fatalf("unexpected auth hook secrets/headers %#v", got)
			}
			if got.TimeoutMS != 7000 || got.RetryAttempts != 2 {
				t.Fatalf("unexpected auth hook timing %#v", got)
			}
			_, _ = w.Write([]byte(`{"hook_type":"custom_access_token"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/demo/auth/hooks":
			_, _ = w.Write([]byte(`[{"hook_type":"custom_access_token"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/demo/auth/hooks/custom_access_token":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	commands := [][]string{
		{
			"auth-hooks", "set",
			"--ref", "demo",
			"--hook-type", "custom_access_token",
			"--target-uri", "https://hooks.example.com/token",
			"--edge-function", "token-hook",
			"--secret-handle", "secret://projects/demo/auth/hook",
			"--header", "authorization=secret://projects/demo/auth/header",
			"--header", "x-trace=supadupa",
			"--timeout-ms", "7000",
			"--retry-attempts", "2",
		},
		{"auth-hooks", "list", "--ref", "demo"},
		{"auth-hooks", "delete", "--ref", "demo", "--hook-type", "custom_access_token"},
	}
	for _, command := range commands {
		var stdout, stderr strings.Builder
		exitCode := Runner{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    map[string]string{"SUPADUPA_API_URL": server.URL},
		}.Run(context.Background(), command)
		if exitCode != 0 {
			t.Fatalf("command %v exit=%d stderr=%s", command, exitCode, stderr.String())
		}
	}
	expected := strings.Join([]string{
		"POST /v1/projects/demo/auth/hooks",
		"GET /v1/projects/demo/auth/hooks",
		"DELETE /v1/projects/demo/auth/hooks/custom_access_token",
	}, "\n")
	if strings.Join(requests, "\n") != expected {
		t.Fatalf("unexpected requests %#v", requests)
	}
}
