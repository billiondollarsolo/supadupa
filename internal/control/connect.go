package control

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	publicPoolerTransactionPort = "6543"
	publicPoolerSessionPort     = "5432"
)

type ConnectPayload struct {
	Services       map[string]bool              `json:"services"`
	APIURL         string                       `json:"api_url"`
	LocalAPIURL    string                       `json:"local_api_url,omitempty"`
	CustomAPIURLs  []string                     `json:"custom_api_urls,omitempty"`
	CustomDomains  []ProjectDomain              `json:"custom_domains,omitempty"`
	StudioURL      string                       `json:"studio_url"`
	LocalStudioURL string                       `json:"local_studio_url,omitempty"`
	RESTURL        string                       `json:"rest_url"`
	AuthURL        string                       `json:"auth_url"`
	GraphQLURL     string                       `json:"graphql_url"`
	RealtimeURL    string                       `json:"realtime_url"`
	FunctionsURL   string                       `json:"functions_url"`
	StorageURL     string                       `json:"storage_url"`
	StorageS3URL   string                       `json:"storage_s3_url"`
	APIKeys        map[string]string            `json:"api_keys"`
	JWT            map[string]string            `json:"jwt"`
	Postgres       map[string]string            `json:"postgres"`
	PostgresParts  map[string]map[string]string `json:"postgres_parts"`
	Pooler         map[string]string            `json:"pooler"`
	Storage        map[string]string            `json:"storage"`
	Links          map[string]string            `json:"links"`
	Snippets       map[string]string            `json:"connection_snippets"`
	SDKSnippets    map[string]string            `json:"sdk_snippets"`
	SecretHandles  map[string]string            `json:"secret_handles"`
	JWTSigningKeys []JWTSigningKeySummary       `json:"jwt_signing_keys"`
}

type ProjectCLIProfile struct {
	ProjectRef               string            `json:"project_ref"`
	ProjectName              string            `json:"project_name"`
	APIURL                   string            `json:"api_url"`
	LocalAPIURL              string            `json:"local_api_url,omitempty"`
	CustomAPIURLs            []string          `json:"custom_api_urls,omitempty"`
	CustomDomains            []ProjectDomain   `json:"custom_domains,omitempty"`
	StudioURL                string            `json:"studio_url"`
	LocalStudioURL           string            `json:"local_studio_url,omitempty"`
	RESTURL                  string            `json:"rest_url"`
	AuthURL                  string            `json:"auth_url"`
	GraphQLURL               string            `json:"graphql_url"`
	RealtimeURL              string            `json:"realtime_url"`
	FunctionsURL             string            `json:"functions_url"`
	StorageURL               string            `json:"storage_url"`
	StorageS3URL             string            `json:"storage_s3_url"`
	DatabaseURL              string            `json:"database_url"`
	InternalDatabaseURL      string            `json:"internal_database_url"`
	PoolerTransactionURL     string            `json:"pooler_transaction_url"`
	InternalPoolerURL        string            `json:"internal_pooler_url"`
	PoolerSessionURL         string            `json:"pooler_session_url"`
	InternalPoolerSessionURL string            `json:"internal_pooler_session_url"`
	PublicDatabaseURL        string            `json:"public_database_url"`
	PublicPoolerURL          string            `json:"public_pooler_url"`
	Env                      map[string]string `json:"env"`
	SupabaseConfigTOML       string            `json:"supabase_config_toml"`
	Commands                 map[string]string `json:"commands"`
	SecretHandles            map[string]string `json:"secret_handles"`
	CompatibilityContracts   map[string]string `json:"compatibility_contracts"`
}

func ConnectPayloadForProject(project Project, secrets ...ProjectSecret) ConnectPayload {
	return connectPayloadForProject(project, nil, nil, nil, secrets...)
}

func ConnectPayloadForProjectWithPoolerConfig(project Project, poolerConfig ProjectConfig, secrets ...ProjectSecret) ConnectPayload {
	return connectPayloadForProject(project, poolerConfig.Config, nil, nil, secrets...)
}

func ConnectPayloadForProjectWithConfigs(project Project, poolerConfig ProjectConfig, databaseConfig ProjectConfig, secrets ...ProjectSecret) ConnectPayload {
	return ConnectPayloadForProjectWithConfigsAndDomains(project, poolerConfig, databaseConfig, nil, secrets...)
}

func ConnectPayloadForProjectWithConfigsAndDomains(project Project, poolerConfig ProjectConfig, databaseConfig ProjectConfig, domains []ProjectDomain, secrets ...ProjectSecret) ConnectPayload {
	return connectPayloadForProject(project, poolerConfig.Config, databaseConfig.Config, domains, secrets...)
}

func ProjectCLIProfileForProjectWithConfigs(project Project, poolerConfig ProjectConfig, databaseConfig ProjectConfig, secrets ...ProjectSecret) ProjectCLIProfile {
	return ProjectCLIProfileForProjectWithConfigsAndDomains(project, poolerConfig, databaseConfig, nil, secrets...)
}

func ProjectCLIProfileForProjectWithConfigsAndDomains(project Project, poolerConfig ProjectConfig, databaseConfig ProjectConfig, domains []ProjectDomain, secrets ...ProjectSecret) ProjectCLIProfile {
	connect := ConnectPayloadForProjectWithConfigsAndDomains(project, poolerConfig, databaseConfig, domains, secrets...)
	databaseURL := connect.Postgres["public_direct"]
	if databaseURL == "" {
		databaseURL = connect.Postgres["direct"]
	}
	poolerTransactionURL := connect.Postgres["public_transaction"]
	if poolerTransactionURL == "" {
		poolerTransactionURL = connect.Postgres["transaction"]
	}
	poolerSessionURL := connect.Postgres["public_session"]
	if poolerSessionURL == "" {
		poolerSessionURL = connect.Postgres["session"]
	}
	env := map[string]string{
		"SUPADUPA_PROJECT_REF":      project.Ref,
		"SUPABASE_URL":              connect.APIURL,
		"SUPABASE_ANON_KEY":         connect.SecretHandles["anon_key"],
		"SUPABASE_SERVICE_ROLE_KEY": connect.SecretHandles["service_role"],
		"SUPABASE_DB_URL":           databaseURL,
		"DATABASE_URL":              databaseURL,
		"SUPABASE_DB_PASSWORD":      connect.SecretHandles["db_password"],
		"SUPADUPA_INTERNAL_DB_URL":  connect.Postgres["direct"],
	}
	if connect.GraphQLURL != "" {
		env["SUPABASE_GRAPHQL_URL"] = connect.GraphQLURL
	}
	if connect.FunctionsURL != "" {
		env["SUPABASE_FUNCTIONS_URL"] = connect.FunctionsURL
	}
	if connect.StorageURL != "" {
		env["SUPABASE_STORAGE_URL"] = connect.StorageURL
		env["SUPABASE_S3_ENDPOINT"] = connect.StorageS3URL
		env["SUPABASE_S3_ACCESS_KEY"] = connect.SecretHandles["s3_access_key"]
		env["SUPABASE_S3_SECRET_KEY"] = connect.SecretHandles["s3_secret_key"]
	}
	if connect.LocalAPIURL != "" {
		env["SUPADUPA_LOCAL_API_URL"] = connect.LocalAPIURL
		env["SUPABASE_LOCAL_URL"] = connect.LocalAPIURL
	}
	if len(connect.CustomAPIURLs) > 0 {
		env["SUPADUPA_CUSTOM_API_URL"] = connect.CustomAPIURLs[0]
		env["SUPADUPA_CUSTOM_API_URLS"] = strings.Join(connect.CustomAPIURLs, ",")
	}
	return ProjectCLIProfile{
		ProjectRef:               project.Ref,
		ProjectName:              project.Name,
		APIURL:                   connect.APIURL,
		LocalAPIURL:              connect.LocalAPIURL,
		CustomAPIURLs:            append([]string(nil), connect.CustomAPIURLs...),
		CustomDomains:            cloneProjectDomains(connect.CustomDomains),
		StudioURL:                connect.StudioURL,
		LocalStudioURL:           connect.LocalStudioURL,
		RESTURL:                  connect.RESTURL,
		AuthURL:                  connect.AuthURL,
		GraphQLURL:               connect.GraphQLURL,
		RealtimeURL:              connect.RealtimeURL,
		FunctionsURL:             connect.FunctionsURL,
		StorageURL:               connect.StorageURL,
		StorageS3URL:             connect.StorageS3URL,
		DatabaseURL:              databaseURL,
		InternalDatabaseURL:      connect.Postgres["direct"],
		PoolerTransactionURL:     poolerTransactionURL,
		InternalPoolerURL:        connect.Postgres["transaction"],
		PoolerSessionURL:         poolerSessionURL,
		InternalPoolerSessionURL: connect.Postgres["session"],
		PublicDatabaseURL:        databaseURL,
		PublicPoolerURL:          poolerTransactionURL,
		Env:                      env,
		SupabaseConfigTOML:       supabaseCLIConfigTOML(project, connect),
		Commands:                 cliProfileCommands(project, connect, databaseURL, poolerTransactionURL, poolerSessionURL),
		SecretHandles:            connect.SecretHandles,
		CompatibilityContracts: map[string]string{
			"control_plane": "Use supadupa Management API for lifecycle, RBAC, keys, backups, domains, config, branching, replicas, and audit.",
			"data_plane":    "Use Supabase-compatible URLs, Postgres URLs, SDKs, and selected Supabase CLI workflows against this project profile.",
			"secrets":       "Values are secret:// handles until an authorized audited reveal supplies concrete material.",
			"typegen":       "Use supadupa-cli projects gen-types for BYO-domain type generation, or supadupa-cli projects db-tunnel when an upstream Supabase CLI container cannot trust the public DB TLS chain.",
		},
	}
}

func connectPayloadForProject(project Project, poolerConfig map[string]string, databaseConfig map[string]string, domains []ProjectDomain, secrets ...ProjectSecret) ConnectPayload {
	base := "https://" + projectHost(project.Ref, project.Spec.Domain)
	studioBase := "https://" + studioHost(project.Ref, project.Spec.Domain)
	storageBase := "https://" + storageHost(project.Ref, project.Spec.Domain)
	services := ProjectServiceStates(project.Spec.Services)
	localAPIBase := localAPIURL(project.Ref)
	localStudioBase := localStudioURL(project.Ref)
	transactionPort := publicPoolerTransactionPort
	sessionPort := publicPoolerSessionPort
	sslMode := postgresSSLMode(databaseConfig)
	publicDomain := strings.TrimSpace(project.Spec.Domain)
	if publicDomain == "" {
		publicDomain = "supadupa.test"
	}
	pooler := map[string]string{
		"dedicated":              configValueOrDefault(poolerConfig, "dedicated_pooler_enabled", "false"),
		"dedicated_tier":         configValueOrDefault(poolerConfig, "dedicated_pooler_tier", "small"),
		"pool_mode":              configValueOrDefault(poolerConfig, "pool_mode", "transaction"),
		"default_pool_size":      configValueOrDefault(poolerConfig, "default_pool_size", "20"),
		"max_client_connections": configValueOrDefault(poolerConfig, "max_client_connections", "200"),
		"transaction_port":       transactionPort,
		"session_port":           sessionPort,
	}
	directURI := postgresURIWithSSLMode(fmt.Sprintf("postgres://postgres:${DB_PASSWORD}@db.%s.internal:5432/postgres", project.Ref), sslMode)
	transactionURI := postgresURIWithSSLMode(fmt.Sprintf("postgres://postgres.%s:${DB_PASSWORD}@pooler.%s.internal:%s/postgres", project.Ref, project.Ref, transactionPort), sslMode)
	sessionURI := postgresURIWithSSLMode(fmt.Sprintf("postgres://postgres.%s:${DB_PASSWORD}@pooler.%s.internal:%s/postgres", project.Ref, project.Ref, sessionPort), sslMode)
	publicDirectURI := postgresURIWithSSLMode(fmt.Sprintf("postgres://postgres:${DB_PASSWORD}@db-%s.%s:5432/postgres", project.Ref, publicDomain), "require")
	publicTransactionURI := postgresURIWithSSLMode(fmt.Sprintf("postgres://postgres.%s:${DB_PASSWORD}@pooler-%s.%s:%s/postgres", project.Ref, project.Ref, publicDomain, publicPoolerTransactionPort), "require")
	publicSessionURI := postgresURIWithSSLMode(fmt.Sprintf("postgres://postgres.%s:${DB_PASSWORD}@pooler-%s.%s:%s/postgres", project.Ref, project.Ref, publicDomain, publicPoolerSessionPort), "require")
	secretHandles := map[string]string{
		"jwt_secret":              fmt.Sprintf("secret://projects/%s/jwt_secret", project.Ref),
		"jwt_signing_key_current": fmt.Sprintf("secret://projects/%s/jwt_signing_key_current", project.Ref),
		"jwt_signing_key_next":    fmt.Sprintf("secret://projects/%s/jwt_signing_key_next", project.Ref),
		"publishable_key":         fmt.Sprintf("secret://projects/%s/publishable_key", project.Ref),
		"secret_key":              fmt.Sprintf("secret://projects/%s/secret_key", project.Ref),
		"anon_key":                fmt.Sprintf("secret://projects/%s/anon_key", project.Ref),
		"service_role":            fmt.Sprintf("secret://projects/%s/service_role", project.Ref),
		"db_password":             fmt.Sprintf("secret://projects/%s/db_password", project.Ref),
		"s3_access_key":           fmt.Sprintf("secret://projects/%s/s3_access_key", project.Ref),
		"s3_secret_key":           fmt.Sprintf("secret://projects/%s/s3_secret_key", project.Ref),
	}
	links := map[string]string{
		"api": base,
	}
	snippets := map[string]string{
		"uri_direct":            publicDirectURI,
		"uri_internal_direct":   directURI,
		"psql_direct":           "psql " + publicDirectURI,
		"psql_internal_direct":  "psql " + directURI,
		"env_api_url":           fmt.Sprintf("SUPABASE_URL=%q", base),
		"env_publishable_key":   "SUPABASE_PUBLISHABLE_KEY=" + secretHandles["publishable_key"],
		"env_secret_key":        "SUPABASE_SECRET_KEY=" + secretHandles["secret_key"],
		"env_database_password": "SUPABASE_DB_PASSWORD=" + secretHandles["db_password"],
	}
	sdkSnippets := map[string]string{
		"javascript": fmt.Sprintf("createClient(%q, process.env.SUPABASE_PUBLISHABLE_KEY)", base),
		"python":     fmt.Sprintf("create_client(%q, os.environ[\"SUPABASE_PUBLISHABLE_KEY\"])", base),
		"flutter":    fmt.Sprintf("Supabase.initialize(url: %q, anonKey: supabaseAnonKey)", base),
		"swift":      fmt.Sprintf("SupabaseClient(supabaseURL: URL(string: %q)!, supabaseKey: supabasePublishableKey)", base),
	}
	if services["studio"] {
		links["studio"] = studioBase
		studioProjectAPIPath := "/project/" + project.Ref + "/api"
		links["rest_docs"] = studioBase + studioProjectAPIPath
		links["graphql_explorer"] = studioBase + studioProjectAPIPath + "?panel=graphql"
	}
	if services["rest"] {
		links["rest"] = base + "/rest/v1"
		links["rest_service"] = base + "/rest/v1"
	}
	if services["graphql"] {
		links["graphql"] = base + "/graphql/v1"
		links["graphql_service"] = base + "/graphql/v1"
	}
	if services["auth"] {
		links["auth"] = base + "/auth/v1"
	}
	if services["storage"] {
		links["storage"] = base + "/storage/v1"
		links["storage_s3"] = storageBase + "/storage/v1/s3"
		links["storage_service"] = base + "/storage/v1"
		snippets["env_storage_access_key"] = "SUPABASE_S3_ACCESS_KEY=" + secretHandles["s3_access_key"]
		snippets["env_storage_secret_key"] = "SUPABASE_S3_SECRET_KEY=" + secretHandles["s3_secret_key"]
	}
	if services["functions"] {
		links["functions"] = base + "/functions/v1"
		links["functions_service"] = base + "/functions/v1"
	}
	if services["realtime"] {
		links["realtime"] = base + "/realtime/v1"
		links["realtime_service"] = base + "/realtime/v1"
	}
	customAPIURLs := readyCustomAPIURLs(domains)
	if len(customAPIURLs) > 0 {
		links["api_custom"] = customAPIURLs[0]
		snippets["env_custom_api_url"] = fmt.Sprintf("SUPADUPA_CUSTOM_API_URL=%q", customAPIURLs[0])
		sdkSnippets["javascript_custom"] = fmt.Sprintf("createClient(%q, process.env.SUPABASE_PUBLISHABLE_KEY)", customAPIURLs[0])
		for i, url := range customAPIURLs {
			links[fmt.Sprintf("api_custom_%d", i+1)] = url
		}
	}
	if services["pooler"] {
		snippets["uri_pool_transaction"] = publicTransactionURI
		snippets["uri_pool_session"] = publicSessionURI
		snippets["uri_internal_pool_transaction"] = transactionURI
		snippets["uri_internal_pool_session"] = sessionURI
		snippets["psql_pool_transaction"] = "psql " + publicTransactionURI
		snippets["psql_pool_session"] = "psql " + publicSessionURI
		snippets["psql_internal_pool_transaction"] = "psql " + transactionURI
		snippets["psql_internal_pool_session"] = "psql " + sessionURI
	}
	if localStudioBase != "" && services["studio"] {
		links["studio_local"] = localStudioBase
	}
	if localAPIBase != "" {
		links["api_local"] = localAPIBase
		if services["rest"] {
			links["rest_local"] = localAPIBase + "/rest/v1"
		}
		if services["auth"] {
			links["auth_local"] = localAPIBase + "/auth/v1"
		}
		if services["graphql"] {
			links["graphql_local"] = localAPIBase + "/graphql/v1"
		}
		if services["storage"] {
			links["storage_local"] = localAPIBase + "/storage/v1"
		}
		if services["functions"] {
			links["functions_local"] = localAPIBase + "/functions/v1"
		}
		if services["realtime"] {
			links["realtime_local"] = localAPIBase + "/realtime/v1"
		}
		snippets["env_local_api_url"] = fmt.Sprintf("SUPABASE_URL=%q", localAPIBase)
		snippets["local_supabase_env"] = "SUPABASE_URL=" + shellQuote(localAPIBase) + " SUPABASE_ANON_KEY=" + shellQuote(secretHandles["anon_key"])
		sdkSnippets["javascript_local"] = fmt.Sprintf("createClient(%q, process.env.SUPABASE_PUBLISHABLE_KEY)", localAPIBase)
		sdkSnippets["python_local"] = fmt.Sprintf("create_client(%q, os.environ[\"SUPABASE_PUBLISHABLE_KEY\"])", localAPIBase)
	}
	postgres := map[string]string{
		"direct":        directURI,
		"public_direct": publicDirectURI,
		"psql":          "psql " + publicDirectURI,
	}
	postgresParts := map[string]map[string]string{
		"direct": {
			"host":            fmt.Sprintf("db.%s.internal", project.Ref),
			"port":            "5432",
			"database":        "postgres",
			"user":            "postgres",
			"password_handle": secretHandles["db_password"],
			"sslmode":         sslMode,
		},
		"public_direct": {
			"host":            fmt.Sprintf("db-%s.%s", project.Ref, publicDomain),
			"port":            "5432",
			"database":        "postgres",
			"user":            "postgres",
			"password_handle": secretHandles["db_password"],
			"sslmode":         "require",
		},
	}
	if services["pooler"] {
		postgres["transaction"] = transactionURI
		postgres["session"] = sessionURI
		postgres["public_transaction"] = publicTransactionURI
		postgres["public_session"] = publicSessionURI
		postgresParts["transaction"] = map[string]string{
			"host":            fmt.Sprintf("pooler.%s.internal", project.Ref),
			"port":            transactionPort,
			"database":        "postgres",
			"user":            "postgres." + project.Ref,
			"password_handle": secretHandles["db_password"],
			"sslmode":         sslMode,
		}
		postgresParts["session"] = map[string]string{
			"host":            fmt.Sprintf("pooler.%s.internal", project.Ref),
			"port":            sessionPort,
			"database":        "postgres",
			"user":            "postgres." + project.Ref,
			"password_handle": secretHandles["db_password"],
			"sslmode":         sslMode,
		}
		postgresParts["public_transaction"] = map[string]string{
			"host":            fmt.Sprintf("pooler-%s.%s", project.Ref, publicDomain),
			"port":            publicPoolerTransactionPort,
			"database":        "postgres",
			"user":            "postgres." + project.Ref,
			"password_handle": secretHandles["db_password"],
			"sslmode":         "require",
		}
		postgresParts["public_session"] = map[string]string{
			"host":            fmt.Sprintf("pooler-%s.%s", project.Ref, publicDomain),
			"port":            publicPoolerSessionPort,
			"database":        "postgres",
			"user":            "postgres." + project.Ref,
			"password_handle": secretHandles["db_password"],
			"sslmode":         "require",
		}
	}
	storage := map[string]string{}
	if services["storage"] {
		storage = map[string]string{
			"s3_endpoint":       storageBase + "/storage/v1/s3",
			"rest_endpoint":     base + "/storage/v1",
			"access_key_handle": secretHandles["s3_access_key"],
			"secret_key_handle": secretHandles["s3_secret_key"],
		}
	}
	return ConnectPayload{
		Services:       services,
		APIURL:         base,
		LocalAPIURL:    localAPIBase,
		CustomAPIURLs:  customAPIURLs,
		CustomDomains:  cloneProjectDomains(domains),
		StudioURL:      serviceURL(services["studio"], studioBase),
		LocalStudioURL: serviceURL(services["studio"], localStudioBase),
		RESTURL:        serviceURL(services["rest"], base+"/rest/v1"),
		AuthURL:        serviceURL(services["auth"], base+"/auth/v1"),
		GraphQLURL:     serviceURL(services["graphql"], base+"/graphql/v1"),
		RealtimeURL:    serviceURL(services["realtime"], base+"/realtime/v1"),
		FunctionsURL:   serviceURL(services["functions"], base+"/functions/v1"),
		StorageURL:     serviceURL(services["storage"], base+"/storage/v1"),
		StorageS3URL:   serviceURL(services["storage"], storageBase+"/storage/v1/s3"),
		APIKeys: map[string]string{
			"publishable":  secretHandles["publishable_key"],
			"secret":       secretHandles["secret_key"],
			"anon":         secretHandles["anon_key"],
			"service_role": secretHandles["service_role"],
		},
		JWT: map[string]string{
			"mode":                "shared-secret",
			"secret":              secretHandles["jwt_secret"],
			"signing_key_current": secretHandles["jwt_signing_key_current"],
			"signing_key_next":    secretHandles["jwt_signing_key_next"],
		},
		Postgres:       postgres,
		PostgresParts:  postgresParts,
		Pooler:         pooler,
		Storage:        storage,
		Links:          links,
		Snippets:       snippets,
		SDKSnippets:    sdkSnippets,
		SecretHandles:  secretHandles,
		JWTSigningKeys: JWTSigningKeySummaries(project.Ref, secrets),
	}
}

func supabaseCLIConfigTOML(project Project, connect ConnectPayload) string {
	lines := []string{
		fmt.Sprintf("project_id = %q", project.Ref),
		"",
		"[supadupa]",
		fmt.Sprintf("project_ref = %q", project.Ref),
		fmt.Sprintf("project_name = %q", project.Name),
		fmt.Sprintf("api_url = %q", connect.APIURL),
		fmt.Sprintf("database_url = %q", connect.Postgres["public_direct"]),
	}
	if connect.StudioURL != "" {
		lines = append(lines, fmt.Sprintf("studio_url = %q", connect.StudioURL))
	}
	if connect.Postgres["public_transaction"] != "" {
		lines = append(lines, fmt.Sprintf("pooler_transaction_url = %q", connect.Postgres["public_transaction"]))
	}
	if connect.Postgres["public_session"] != "" {
		lines = append(lines, fmt.Sprintf("pooler_session_url = %q", connect.Postgres["public_session"]))
	}
	if len(connect.CustomAPIURLs) > 0 {
		lines = append(lines, fmt.Sprintf("custom_api_urls = %s", tomlStringArray(connect.CustomAPIURLs)))
	}
	lines = append(lines, "", "[supadupa.secret_handles]")
	keys := make([]string, 0, len(connect.SecretHandles))
	for key := range connect.SecretHandles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s = %q", key, connect.SecretHandles[key]))
	}
	return strings.Join(lines, "\n") + "\n"
}

func readyCustomAPIURLs(domains []ProjectDomain) []string {
	ready := make([]string, 0, len(domains))
	seen := map[string]struct{}{}
	for _, domain := range domains {
		fqdn := strings.TrimSpace(domain.FQDN)
		if fqdn == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(domain.CertStatus)) {
		case "issued", "uploaded":
		default:
			continue
		}
		host := strings.ToLower(fqdn)
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		ready = append(ready, "https://"+host)
	}
	sort.Strings(ready)
	return ready
}

func tomlStringArray(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%q", value))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func cliProfileCommands(project Project, connect ConnectPayload, databaseURL string, poolerTransactionURL string, poolerSessionURL string) map[string]string {
	commands := map[string]string{
		"psql_direct":                 "psql " + databaseURL,
		"psql_internal_direct":        "psql " + connect.Postgres["direct"],
		"supabase_db_push":            "supabase db push --db-url " + shellDoubleQuoteAllowEnv(databaseURL),
		"supabase_db_pull":            "supabase db pull --db-url " + shellDoubleQuoteAllowEnv(databaseURL),
		"supabase_db_push_env":        `set -a; . .supadupa/supabase.env; set +a; supabase db push --db-url "$SUPABASE_DB_URL"`,
		"supabase_db_pull_env":        `set -a; . .supadupa/supabase.env; set +a; supabase db pull --db-url "$SUPABASE_DB_URL"`,
		"supadupa_gen_types":          "supadupa-cli projects gen-types --ref " + project.Ref + " --out database.types.ts",
		"supadupa_db_tunnel":          "supadupa-cli projects db-tunnel --ref " + project.Ref,
		"supabase_typegen_tunnel":     "supadupa-cli projects db-tunnel --ref " + project.Ref + " --listen 127.0.0.1:15432",
		"supadupa_env_handles":        "supadupa-cli projects env --ref " + project.Ref + " --out .supadupa/supabase.env",
		"supadupa_env_reveal":         "supadupa-cli projects env --ref " + project.Ref + " --reveal-secrets --out .supadupa/supabase.env",
		"supadupa_link_handles":       "supadupa-cli projects link --ref " + project.Ref,
		"supadupa_link_reveal":        "supadupa-cli projects link --ref " + project.Ref + " --reveal-secrets",
		"supabase_local_env":          localSupabaseEnvCommand(connect),
		"supadupa_secret_reveal":      "supadupa-cli secrets reveal --ref " + project.Ref + " --kind <kind>",
		"supadupa_project_config":     "supadupa-cli projects cli-profile --ref " + project.Ref,
		"supadupa_project_config_env": "supadupa-cli projects cli-profile --ref " + project.Ref + " --format env",
	}
	if poolerTransactionURL != "" {
		commands["psql_pool_transaction"] = "psql " + poolerTransactionURL
	}
	if poolerSessionURL != "" {
		commands["psql_pool_session"] = "psql " + poolerSessionURL
	}
	if connect.Postgres["transaction"] != "" {
		commands["psql_internal_pooler"] = "psql " + connect.Postgres["transaction"]
	}
	if connect.FunctionsURL != "" {
		commands["supabase_functions_env"] = "SUPABASE_URL=" + shellQuote(connect.APIURL) + " SUPABASE_ANON_KEY=" + shellQuote(connect.SecretHandles["anon_key"])
	}
	return commands
}

func localSupabaseEnvCommand(connect ConnectPayload) string {
	apiURL := connect.APIURL
	if connect.LocalAPIURL != "" {
		apiURL = connect.LocalAPIURL
	}
	return "SUPABASE_URL=" + shellQuote(apiURL) + " SUPABASE_ANON_KEY=" + shellQuote(connect.SecretHandles["anon_key"])
}

func serviceURL(enabled bool, value string) string {
	if !enabled {
		return ""
	}
	return value
}

func localAPIURL(ref string) string {
	origin := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPADUPA_LOCAL_RUNTIME_ORIGIN")), "/")
	if origin == "" {
		return ""
	}
	return origin + "/projects/" + ref + "/api"
}

func localStudioURL(ref string) string {
	origin := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPADUPA_LOCAL_RUNTIME_ORIGIN")), "/")
	if origin == "" {
		return ""
	}
	return origin + "/projects/" + ref + "/studio"
}

func shellDoubleQuoteAllowEnv(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	return `"` + escaped + `"`
}

func postgresSSLMode(config map[string]string) string {
	if strings.EqualFold(strings.TrimSpace(configValueOrDefault(config, "ssl_enforced", "true")), "false") {
		return "prefer"
	}
	return "require"
}

func postgresURIWithSSLMode(uri string, sslMode string) string {
	if strings.Contains(uri, "?") {
		return uri + "&sslmode=" + sslMode
	}
	return uri + "?sslmode=" + sslMode
}

func configValueOrDefault(config map[string]string, key string, fallback string) string {
	if config == nil {
		return fallback
	}
	value := strings.TrimSpace(config[key])
	if value == "" {
		return fallback
	}
	return value
}

func JWTSigningKeySummaries(ref string, secrets []ProjectSecret) []JWTSigningKeySummary {
	summaries := make([]JWTSigningKeySummary, 0)
	for _, secret := range secrets {
		if !strings.HasPrefix(secret.Kind, "jwt_signing_key_") {
			continue
		}
		material := JWTSigningKeyMaterial{}
		if err := json.Unmarshal([]byte(secret.Value), &material); err != nil {
			material.KID = secret.Kind
			material.Alg = "EdDSA"
			material.Status = signingKeyStatus(secret.Kind)
		}
		if material.Status == "" {
			material.Status = signingKeyStatus(secret.Kind)
		}
		summaries = append(summaries, JWTSigningKeySummary{
			Kind:      secret.Kind,
			KID:       material.KID,
			Alg:       material.Alg,
			Status:    material.Status,
			PublicKey: material.PublicKey,
			Handle:    fmt.Sprintf("secret://projects/%s/%s", ref, secret.Kind),
			CreatedAt: secret.CreatedAt,
			RotatedAt: secret.RotatedAt,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		leftRank := signingKeyStatusRank(summaries[i].Status)
		rightRank := signingKeyStatusRank(summaries[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return summaries[i].CreatedAt.After(summaries[j].CreatedAt)
	})
	return summaries
}

func signingKeyStatus(kind string) string {
	switch kind {
	case "jwt_signing_key_current":
		return "current"
	case "jwt_signing_key_next":
		return "next"
	default:
		if strings.HasPrefix(kind, "jwt_signing_key_previous_") {
			return "previous"
		}
		return ""
	}
}

func signingKeyStatusRank(status string) int {
	switch status {
	case "current":
		return 0
	case "next":
		return 1
	case "previous":
		return 2
	default:
		return 3
	}
}
