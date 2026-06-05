package control

import (
	"strings"
	"testing"
)

func TestConnectPayloadUsesRoutedStudioHost(t *testing.T) {
	project := Project{
		Ref:  "alpha",
		Name: "Alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
		},
	}

	payload := ConnectPayloadForProject(project)

	if payload.StudioURL != "https://studio.alpha.supadupa.test" {
		t.Fatalf("studio_url = %q", payload.StudioURL)
	}
	if payload.Links["studio"] != "https://studio.alpha.supadupa.test" {
		t.Fatalf("studio link = %q", payload.Links["studio"])
	}
	if payload.Links["studio_via_api"] != "https://alpha.supadupa.test/studio" {
		t.Fatalf("studio_via_api link = %q", payload.Links["studio_via_api"])
	}
	if payload.Links["rest_docs"] != "https://studio.alpha.supadupa.test/project/default/api" {
		t.Fatalf("rest docs link = %q", payload.Links["rest_docs"])
	}
	if payload.Links["graphql_explorer"] != "https://studio.alpha.supadupa.test/project/default/api?panel=graphql" {
		t.Fatalf("graphql explorer link = %q", payload.Links["graphql_explorer"])
	}

	profile := ProjectCLIProfileForProjectWithConfigs(project, ProjectConfig{}, ProjectConfig{})
	if profile.ProjectRef != "alpha" || profile.ProjectName != "Alpha" {
		t.Fatalf("unexpected profile identity: %#v", profile)
	}
	if profile.Env["SUPABASE_URL"] != "https://alpha.supadupa.test" {
		t.Fatalf("SUPABASE_URL = %q", profile.Env["SUPABASE_URL"])
	}
	if profile.Env["SUPABASE_SERVICE_ROLE_KEY"] != "secret://projects/alpha/service_role" {
		t.Fatalf("service role env = %q", profile.Env["SUPABASE_SERVICE_ROLE_KEY"])
	}
	if profile.DatabaseURL != "postgres://postgres:${DB_PASSWORD}@db.alpha.internal:5432/postgres?sslmode=require" {
		t.Fatalf("database URL = %q", profile.DatabaseURL)
	}
	if !strings.Contains(profile.Commands["supabase_db_push"], `supabase db push --db-url "postgres://postgres:${DB_PASSWORD}@db.alpha.internal:5432/postgres?sslmode=require"`) {
		t.Fatalf("expected supabase db push command: %#v", profile.Commands)
	}
	if !strings.Contains(profile.SupabaseConfigTOML, `[supadupa]`) || !strings.Contains(profile.SupabaseConfigTOML, `project_id = "alpha"`) {
		t.Fatalf("unexpected config toml:\n%s", profile.SupabaseConfigTOML)
	}
}

func TestConnectPayloadIncludesLocalStudioWhenConfigured(t *testing.T) {
	t.Setenv("SUPADUPA_LOCAL_RUNTIME_ORIGIN", "http://localhost:8088")
	project := Project{
		Ref:  "alpha",
		Name: "Alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
		},
	}

	payload := ConnectPayloadForProject(project)

	if payload.LocalAPIURL != "http://localhost:8088/projects/alpha/api" {
		t.Fatalf("local_api_url = %q", payload.LocalAPIURL)
	}
	if payload.LocalStudioURL != "http://localhost:8088/projects/alpha/studio" {
		t.Fatalf("local_studio_url = %q", payload.LocalStudioURL)
	}
	if payload.Links["api_local"] != payload.LocalAPIURL {
		t.Fatalf("api_local link = %q", payload.Links["api_local"])
	}
	if payload.Links["rest_local"] != "http://localhost:8088/projects/alpha/api/rest/v1" {
		t.Fatalf("rest_local link = %q", payload.Links["rest_local"])
	}
	if payload.Links["studio_local"] != payload.LocalStudioURL {
		t.Fatalf("studio_local link = %q", payload.Links["studio_local"])
	}
	if payload.Snippets["env_local_api_url"] != `SUPABASE_URL="http://localhost:8088/projects/alpha/api"` {
		t.Fatalf("local env snippet = %q", payload.Snippets["env_local_api_url"])
	}
	if !strings.Contains(payload.Snippets["local_supabase_env"], `SUPABASE_URL='http://localhost:8088/projects/alpha/api'`) {
		t.Fatalf("local Supabase env snippet = %q", payload.Snippets["local_supabase_env"])
	}
	if !strings.Contains(payload.SDKSnippets["javascript_local"], `"http://localhost:8088/projects/alpha/api"`) {
		t.Fatalf("local JS snippet = %q", payload.SDKSnippets["javascript_local"])
	}
	profile := ProjectCLIProfileForProjectWithConfigs(project, ProjectConfig{}, ProjectConfig{})
	if profile.LocalAPIURL != payload.LocalAPIURL {
		t.Fatalf("profile local API URL = %q", profile.LocalAPIURL)
	}
	if profile.LocalStudioURL != payload.LocalStudioURL {
		t.Fatalf("profile local studio URL = %q", profile.LocalStudioURL)
	}
	if profile.Env["SUPABASE_LOCAL_URL"] != payload.LocalAPIURL {
		t.Fatalf("profile local Supabase URL = %q", profile.Env["SUPABASE_LOCAL_URL"])
	}
	if !strings.Contains(profile.Commands["supabase_local_env"], `SUPABASE_URL='http://localhost:8088/projects/alpha/api'`) {
		t.Fatalf("expected local Supabase env command: %#v", profile.Commands)
	}
	if !strings.Contains(profile.SupabaseConfigTOML, `local_api_url = "http://localhost:8088/projects/alpha/api"`) {
		t.Fatalf("expected local API URL in config toml:\n%s", profile.SupabaseConfigTOML)
	}
	if !strings.Contains(profile.SupabaseConfigTOML, `local_studio_url = "http://localhost:8088/projects/alpha/studio"`) {
		t.Fatalf("expected local studio URL in config toml:\n%s", profile.SupabaseConfigTOML)
	}
}
