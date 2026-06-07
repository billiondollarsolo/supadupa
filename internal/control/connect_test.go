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

	if payload.StudioURL != "https://studio-alpha.supadupa.test" {
		t.Fatalf("studio_url = %q", payload.StudioURL)
	}
	if payload.Links["studio"] != "https://studio-alpha.supadupa.test" {
		t.Fatalf("studio link = %q", payload.Links["studio"])
	}
	if _, ok := payload.Links["studio_via_api"]; ok {
		t.Fatalf("studio_via_api should not be exposed in links: %#v", payload.Links)
	}
	if payload.Links["rest_docs"] != "https://studio-alpha.supadupa.test/project/alpha/api" {
		t.Fatalf("rest docs link = %q", payload.Links["rest_docs"])
	}
	if payload.Links["graphql_explorer"] != "https://studio-alpha.supadupa.test/project/alpha/api?panel=graphql" {
		t.Fatalf("graphql explorer link = %q", payload.Links["graphql_explorer"])
	}
	if payload.StorageS3URL != "https://storage-alpha.supadupa.test/storage/v1/s3" {
		t.Fatalf("storage_s3_url = %q", payload.StorageS3URL)
	}
	if payload.Storage["s3_endpoint"] != payload.StorageS3URL {
		t.Fatalf("storage s3 endpoint = %q", payload.Storage["s3_endpoint"])
	}
	if payload.Links["storage_s3"] != payload.StorageS3URL {
		t.Fatalf("storage_s3 link = %q", payload.Links["storage_s3"])
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
	if profile.DatabaseURL != "postgres://postgres:${DB_PASSWORD}@db-alpha.supadupa.test:5432/postgres?sslmode=require" {
		t.Fatalf("database URL = %q", profile.DatabaseURL)
	}
	if profile.PublicDatabaseURL != profile.DatabaseURL {
		t.Fatalf("public database URL = %q", profile.PublicDatabaseURL)
	}
	if profile.InternalDatabaseURL != "postgres://postgres:${DB_PASSWORD}@db.alpha.internal:5432/postgres?sslmode=require" {
		t.Fatalf("internal database URL = %q", profile.InternalDatabaseURL)
	}
	if profile.PoolerTransactionURL != "postgres://postgres.alpha:${DB_PASSWORD}@pooler-alpha.supadupa.test:6543/postgres?sslmode=require" {
		t.Fatalf("public pooler URL = %q", profile.PoolerTransactionURL)
	}
	if profile.InternalPoolerURL != "postgres://postgres.alpha:${DB_PASSWORD}@pooler.alpha.internal:6543/postgres?sslmode=require" {
		t.Fatalf("internal pooler URL = %q", profile.InternalPoolerURL)
	}
	if profile.Env["SUPABASE_DB_URL"] != profile.DatabaseURL {
		t.Fatalf("SUPABASE_DB_URL = %q", profile.Env["SUPABASE_DB_URL"])
	}
	if profile.StorageS3URL != "https://storage-alpha.supadupa.test/storage/v1/s3" {
		t.Fatalf("profile storage S3 URL = %q", profile.StorageS3URL)
	}
	if profile.Env["SUPADUPA_INTERNAL_DB_URL"] != profile.InternalDatabaseURL {
		t.Fatalf("SUPADUPA_INTERNAL_DB_URL = %q", profile.Env["SUPADUPA_INTERNAL_DB_URL"])
	}
	if payload.Snippets["uri_direct"] != "postgres://postgres:${DB_PASSWORD}@db-alpha.supadupa.test:5432/postgres?sslmode=require" {
		t.Fatalf("public direct snippet = %q", payload.Snippets["uri_direct"])
	}
	if payload.Snippets["uri_internal_direct"] != "postgres://postgres:${DB_PASSWORD}@db.alpha.internal:5432/postgres?sslmode=require" {
		t.Fatalf("internal direct snippet = %q", payload.Snippets["uri_internal_direct"])
	}
	if payload.Snippets["psql_direct"] != "psql postgres://postgres:${DB_PASSWORD}@db-alpha.supadupa.test:5432/postgres?sslmode=require" {
		t.Fatalf("public psql snippet = %q", payload.Snippets["psql_direct"])
	}
	if payload.PostgresParts["public_direct"]["host"] != "db-alpha.supadupa.test" {
		t.Fatalf("public direct host = %#v", payload.PostgresParts["public_direct"])
	}
	if payload.PostgresParts["public_transaction"]["host"] != "pooler-alpha.supadupa.test" || payload.PostgresParts["public_transaction"]["port"] != "6543" {
		t.Fatalf("public transaction parts = %#v", payload.PostgresParts["public_transaction"])
	}
	if payload.PostgresParts["public_session"]["host"] != "pooler-alpha.supadupa.test" || payload.PostgresParts["public_session"]["port"] != "5432" {
		t.Fatalf("public session parts = %#v", payload.PostgresParts["public_session"])
	}
	if !strings.Contains(profile.Commands["supabase_db_push"], `supabase db push --db-url "postgres://postgres:${DB_PASSWORD}@db-alpha.supadupa.test:5432/postgres?sslmode=require"`) {
		t.Fatalf("expected supabase db push command: %#v", profile.Commands)
	}
	if profile.Commands["supadupa_env_reveal"] != "supadupa-cli projects env --ref alpha --reveal-secrets --out .supadupa/supabase.env" {
		t.Fatalf("expected materialized env command: %#v", profile.Commands)
	}
	if profile.Commands["supadupa_link_reveal"] != "supadupa-cli projects link --ref alpha --reveal-secrets" {
		t.Fatalf("expected link reveal command: %#v", profile.Commands)
	}
	if profile.Commands["supabase_db_push_env"] != `set -a; . .supadupa/supabase.env; set +a; supabase db push --db-url "$SUPABASE_DB_URL"` {
		t.Fatalf("expected runnable env push command: %#v", profile.Commands)
	}
	if profile.Commands["supabase_db_pull_env"] != `set -a; . .supadupa/supabase.env; set +a; supabase db pull --db-url "$SUPABASE_DB_URL"` {
		t.Fatalf("expected runnable env pull command: %#v", profile.Commands)
	}
	if profile.Commands["supadupa_gen_types"] != "supadupa-cli projects gen-types --ref alpha --out database.types.ts" {
		t.Fatalf("expected supadupa typegen command: %#v", profile.Commands)
	}
	if profile.Commands["supadupa_db_tunnel"] != "supadupa-cli projects db-tunnel --ref alpha" {
		t.Fatalf("expected supadupa db tunnel command: %#v", profile.Commands)
	}
	if !strings.Contains(profile.CompatibilityContracts["typegen"], "supadupa-cli projects gen-types") || !strings.Contains(profile.CompatibilityContracts["typegen"], "db-tunnel") {
		t.Fatalf("expected typegen compatibility contract: %#v", profile.CompatibilityContracts)
	}
	for _, expected := range []string{
		`project_id = "alpha"`,
		`[supadupa]`,
		`project_ref = "alpha"`,
		`api_url = "https://alpha.supadupa.test"`,
		`database_url = "postgres://postgres:${DB_PASSWORD}@db-alpha.supadupa.test:5432/postgres?sslmode=require"`,
		`[supadupa.secret_handles]`,
		`anon_key = "secret://projects/alpha/anon_key"`,
		`db_password = "secret://projects/alpha/db_password"`,
	} {
		if !strings.Contains(profile.SupabaseConfigTOML, expected) {
			t.Fatalf("expected %q in Supabase config toml:\n%s", expected, profile.SupabaseConfigTOML)
		}
	}
	for _, unexpected := range []string{`internal_database_url`, `local_api_url`} {
		if strings.Contains(profile.SupabaseConfigTOML, unexpected) {
			t.Fatalf("unexpected %q in Supabase config toml:\n%s", unexpected, profile.SupabaseConfigTOML)
		}
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
	if strings.Contains(profile.SupabaseConfigTOML, `local_api_url`) {
		t.Fatalf("Supabase config toml should stay official CLI compatible:\n%s", profile.SupabaseConfigTOML)
	}
}

func TestConnectPayloadExposesReadyCustomAPIDomains(t *testing.T) {
	project := Project{
		Ref:  "alpha",
		Name: "Alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
		},
	}
	domains := []ProjectDomain{
		{ProjectRef: "alpha", FQDN: "pending.example.com", CertStatus: "pending", CertMode: "acme"},
		{ProjectRef: "alpha", FQDN: "API.Example.COM", CertStatus: "uploaded", CertMode: "byo"},
		{ProjectRef: "alpha", FQDN: "issued.example.com", CertStatus: "issued", CertMode: "acme"},
	}

	payload := ConnectPayloadForProjectWithConfigsAndDomains(project, ProjectConfig{}, ProjectConfig{}, domains)

	if payload.APIURL != "https://alpha.supadupa.test" {
		t.Fatalf("generated api_url should remain canonical, got %q", payload.APIURL)
	}
	if len(payload.CustomDomains) != 3 {
		t.Fatalf("expected all custom domains in metadata, got %#v", payload.CustomDomains)
	}
	expectedURLs := []string{"https://api.example.com", "https://issued.example.com"}
	if strings.Join(payload.CustomAPIURLs, ",") != strings.Join(expectedURLs, ",") {
		t.Fatalf("custom_api_urls = %#v", payload.CustomAPIURLs)
	}
	if payload.Links["api_custom"] != expectedURLs[0] || payload.Links["api_custom_2"] != expectedURLs[1] {
		t.Fatalf("expected custom API links, got %#v", payload.Links)
	}
	if strings.Contains(strings.Join(payload.CustomAPIURLs, ","), "pending.example.com") {
		t.Fatalf("pending cert domain should not be a ready API URL: %#v", payload.CustomAPIURLs)
	}
	if !strings.Contains(payload.Snippets["env_custom_api_url"], `SUPADUPA_CUSTOM_API_URL="https://api.example.com"`) {
		t.Fatalf("expected custom API env snippet, got %q", payload.Snippets["env_custom_api_url"])
	}
	if !strings.Contains(payload.SDKSnippets["javascript_custom"], `"https://api.example.com"`) {
		t.Fatalf("expected custom API SDK snippet, got %q", payload.SDKSnippets["javascript_custom"])
	}

	profile := ProjectCLIProfileForProjectWithConfigsAndDomains(project, ProjectConfig{}, ProjectConfig{}, domains)
	if profile.APIURL != "https://alpha.supadupa.test" || profile.Env["SUPABASE_URL"] != "https://alpha.supadupa.test" {
		t.Fatalf("generated API URL should remain default in profile: %#v", profile)
	}
	if profile.Env["SUPADUPA_CUSTOM_API_URL"] != "https://api.example.com" {
		t.Fatalf("SUPADUPA_CUSTOM_API_URL = %q", profile.Env["SUPADUPA_CUSTOM_API_URL"])
	}
	if profile.Env["SUPADUPA_CUSTOM_API_URLS"] != "https://api.example.com,https://issued.example.com" {
		t.Fatalf("SUPADUPA_CUSTOM_API_URLS = %q", profile.Env["SUPADUPA_CUSTOM_API_URLS"])
	}
	if !strings.Contains(profile.SupabaseConfigTOML, `custom_api_urls = ["https://api.example.com", "https://issued.example.com"]`) {
		t.Fatalf("expected custom_api_urls in TOML:\n%s", profile.SupabaseConfigTOML)
	}
}

func TestConnectPayloadUsesHostedPoolerPortsDespiteConfig(t *testing.T) {
	project := Project{
		Ref:  "alpha",
		Name: "Alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
		},
	}
	poolerConfig := ProjectConfig{
		ProjectRef: "alpha",
		Area:       "pooler",
		Config: map[string]string{
			"transaction_port": "7654",
			"session_port":     "55432",
		},
	}

	payload := ConnectPayloadForProjectWithPoolerConfig(project, poolerConfig)

	if payload.Postgres["public_transaction"] != "postgres://postgres.alpha:${DB_PASSWORD}@pooler-alpha.supadupa.test:6543/postgres?sslmode=require" {
		t.Fatalf("public transaction URL drifted from hosted port: %q", payload.Postgres["public_transaction"])
	}
	if payload.Postgres["public_session"] != "postgres://postgres.alpha:${DB_PASSWORD}@pooler-alpha.supadupa.test:5432/postgres?sslmode=require" {
		t.Fatalf("public session URL drifted from hosted port: %q", payload.Postgres["public_session"])
	}
	if payload.Postgres["transaction"] != "postgres://postgres.alpha:${DB_PASSWORD}@pooler.alpha.internal:6543/postgres?sslmode=require" {
		t.Fatalf("internal transaction URL drifted from runtime port: %q", payload.Postgres["transaction"])
	}
	if payload.Postgres["session"] != "postgres://postgres.alpha:${DB_PASSWORD}@pooler.alpha.internal:5432/postgres?sslmode=require" {
		t.Fatalf("internal session URL drifted from runtime port: %q", payload.Postgres["session"])
	}
	if payload.Pooler["transaction_port"] != "6543" || payload.Pooler["session_port"] != "5432" {
		t.Fatalf("pooler metadata should expose fixed hosted ports: %#v", payload.Pooler)
	}
}

func TestConnectPayloadOmitsDisabledServiceEndpoints(t *testing.T) {
	t.Setenv("SUPADUPA_LOCAL_RUNTIME_ORIGIN", "http://localhost:8088")
	project := Project{
		Ref:  "alpha",
		Name: "Alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
			Services: map[string]ServiceSpec{
				"studio":    {Enabled: false},
				"storage":   {Enabled: false},
				"functions": {Enabled: false},
				"realtime":  {Enabled: false},
				"graphql":   {Enabled: false},
				"pooler":    {Enabled: false},
			},
		},
	}

	payload := ConnectPayloadForProject(project)

	if payload.Services["studio"] || payload.Services["storage"] || payload.Services["functions"] || payload.Services["realtime"] || payload.Services["graphql"] || payload.Services["pooler"] {
		t.Fatalf("expected disabled service states, got %#v", payload.Services)
	}
	if payload.StudioURL != "" || payload.LocalStudioURL != "" || payload.StorageURL != "" || payload.StorageS3URL != "" || payload.FunctionsURL != "" || payload.RealtimeURL != "" || payload.GraphQLURL != "" {
		t.Fatalf("disabled service URLs should be empty: %#v", payload)
	}
	for _, key := range []string{"studio", "studio_local", "storage", "storage_s3", "functions", "realtime", "graphql", "graphql_explorer"} {
		if value := payload.Links[key]; value != "" {
			t.Fatalf("disabled link %s should be omitted, got %q", key, value)
		}
	}
	for _, key := range []string{"uri_pool_transaction", "uri_pool_session", "uri_internal_pool_transaction", "uri_internal_pool_session", "env_storage_access_key", "env_storage_secret_key"} {
		if value := payload.Snippets[key]; value != "" {
			t.Fatalf("disabled snippet %s should be omitted, got %q", key, value)
		}
	}
	for _, key := range []string{"transaction", "session", "public_transaction", "public_session"} {
		if value := payload.Postgres[key]; value != "" {
			t.Fatalf("disabled pooler postgres %s should be omitted, got %q", key, value)
		}
		if parts := payload.PostgresParts[key]; parts != nil {
			t.Fatalf("disabled pooler postgres parts %s should be omitted, got %#v", key, parts)
		}
	}
	if payload.Storage["s3_endpoint"] != "" || payload.Storage["access_key_handle"] != "" {
		t.Fatalf("disabled storage map should be empty, got %#v", payload.Storage)
	}
	if payload.APIURL != "https://alpha.supadupa.test" || payload.RESTURL != "https://alpha.supadupa.test/rest/v1" || payload.AuthURL != "https://alpha.supadupa.test/auth/v1" {
		t.Fatalf("enabled API/Auth/REST URLs should remain available: %#v", payload)
	}

	profile := ProjectCLIProfileForProjectWithConfigs(project, ProjectConfig{}, ProjectConfig{})
	if profile.PoolerTransactionURL != "" || profile.PublicPoolerURL != "" || profile.InternalPoolerURL != "" {
		t.Fatalf("disabled pooler profile fields should be empty: %#v", profile)
	}
	for _, key := range []string{"SUPABASE_GRAPHQL_URL", "SUPABASE_FUNCTIONS_URL", "SUPABASE_STORAGE_URL", "SUPABASE_S3_ENDPOINT"} {
		if value := profile.Env[key]; value != "" {
			t.Fatalf("disabled service env %s should be omitted, got %q", key, value)
		}
	}
	for _, key := range []string{"psql_pool_transaction", "psql_pool_session", "psql_internal_pooler", "supabase_functions_env"} {
		if value := profile.Commands[key]; value != "" {
			t.Fatalf("disabled service command %s should be omitted, got %q", key, value)
		}
	}
	if profile.Commands["supadupa_env_reveal"] == "" || profile.Commands["supabase_db_push_env"] == "" {
		t.Fatalf("materialized database workflow commands should remain available: %#v", profile.Commands)
	}
	for _, unexpected := range []string{"studio_url", "pooler_transaction_url", "pooler_session_url"} {
		if strings.Contains(profile.SupabaseConfigTOML, unexpected) {
			t.Fatalf("disabled %s should be omitted from TOML:\n%s", unexpected, profile.SupabaseConfigTOML)
		}
	}
}
