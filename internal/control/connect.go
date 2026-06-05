package control

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type ConnectPayload struct {
	APIURL         string                       `json:"api_url"`
	LocalAPIURL    string                       `json:"local_api_url,omitempty"`
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
	ProjectRef             string            `json:"project_ref"`
	ProjectName            string            `json:"project_name"`
	APIURL                 string            `json:"api_url"`
	LocalAPIURL            string            `json:"local_api_url,omitempty"`
	StudioURL              string            `json:"studio_url"`
	LocalStudioURL         string            `json:"local_studio_url,omitempty"`
	RESTURL                string            `json:"rest_url"`
	AuthURL                string            `json:"auth_url"`
	GraphQLURL             string            `json:"graphql_url"`
	RealtimeURL            string            `json:"realtime_url"`
	FunctionsURL           string            `json:"functions_url"`
	StorageURL             string            `json:"storage_url"`
	StorageS3URL           string            `json:"storage_s3_url"`
	DatabaseURL            string            `json:"database_url"`
	PoolerTransactionURL   string            `json:"pooler_transaction_url"`
	PoolerSessionURL       string            `json:"pooler_session_url"`
	Env                    map[string]string `json:"env"`
	SupabaseConfigTOML     string            `json:"supabase_config_toml"`
	Commands               map[string]string `json:"commands"`
	SecretHandles          map[string]string `json:"secret_handles"`
	CompatibilityContracts map[string]string `json:"compatibility_contracts"`
}

func ConnectPayloadForProject(project Project, secrets ...ProjectSecret) ConnectPayload {
	return connectPayloadForProject(project, nil, nil, secrets...)
}

func ConnectPayloadForProjectWithPoolerConfig(project Project, poolerConfig ProjectConfig, secrets ...ProjectSecret) ConnectPayload {
	return connectPayloadForProject(project, poolerConfig.Config, nil, secrets...)
}

func ConnectPayloadForProjectWithConfigs(project Project, poolerConfig ProjectConfig, databaseConfig ProjectConfig, secrets ...ProjectSecret) ConnectPayload {
	return connectPayloadForProject(project, poolerConfig.Config, databaseConfig.Config, secrets...)
}

func ProjectCLIProfileForProjectWithConfigs(project Project, poolerConfig ProjectConfig, databaseConfig ProjectConfig, secrets ...ProjectSecret) ProjectCLIProfile {
	connect := ConnectPayloadForProjectWithConfigs(project, poolerConfig, databaseConfig, secrets...)
	databaseURL := connect.Postgres["direct"]
	poolerTransactionURL := connect.Postgres["transaction"]
	poolerSessionURL := connect.Postgres["session"]
	env := map[string]string{
		"SUPADUPA_PROJECT_REF":      project.Ref,
		"SUPABASE_URL":              connect.APIURL,
		"SUPABASE_ANON_KEY":         connect.SecretHandles["anon_key"],
		"SUPABASE_SERVICE_ROLE_KEY": connect.SecretHandles["service_role"],
		"SUPABASE_DB_URL":           databaseURL,
		"DATABASE_URL":              databaseURL,
		"SUPABASE_DB_PASSWORD":      connect.SecretHandles["db_password"],
		"SUPABASE_GRAPHQL_URL":      connect.GraphQLURL,
		"SUPABASE_FUNCTIONS_URL":    connect.FunctionsURL,
		"SUPABASE_STORAGE_URL":      connect.StorageURL,
		"SUPABASE_S3_ENDPOINT":      connect.StorageS3URL,
		"SUPABASE_S3_ACCESS_KEY":    connect.SecretHandles["s3_access_key"],
		"SUPABASE_S3_SECRET_KEY":    connect.SecretHandles["s3_secret_key"],
	}
	if connect.LocalAPIURL != "" {
		env["SUPADUPA_LOCAL_API_URL"] = connect.LocalAPIURL
		env["SUPABASE_LOCAL_URL"] = connect.LocalAPIURL
	}
	return ProjectCLIProfile{
		ProjectRef:           project.Ref,
		ProjectName:          project.Name,
		APIURL:               connect.APIURL,
		LocalAPIURL:          connect.LocalAPIURL,
		StudioURL:            connect.StudioURL,
		LocalStudioURL:       connect.LocalStudioURL,
		RESTURL:              connect.RESTURL,
		AuthURL:              connect.AuthURL,
		GraphQLURL:           connect.GraphQLURL,
		RealtimeURL:          connect.RealtimeURL,
		FunctionsURL:         connect.FunctionsURL,
		StorageURL:           connect.StorageURL,
		StorageS3URL:         connect.StorageS3URL,
		DatabaseURL:          databaseURL,
		PoolerTransactionURL: poolerTransactionURL,
		PoolerSessionURL:     poolerSessionURL,
		Env:                  env,
		SupabaseConfigTOML:   supabaseCLIConfigTOML(project, connect),
		Commands: map[string]string{
			"psql_direct":             "psql " + databaseURL,
			"psql_pool_transaction":   "psql " + poolerTransactionURL,
			"supabase_db_push":        "supabase db push --db-url " + shellDoubleQuoteAllowEnv(databaseURL),
			"supabase_db_pull":        "supabase db pull --db-url " + shellDoubleQuoteAllowEnv(databaseURL),
			"supabase_functions_env":  "SUPABASE_URL=" + shellQuote(connect.APIURL) + " SUPABASE_ANON_KEY=" + shellQuote(connect.SecretHandles["anon_key"]),
			"supabase_local_env":      localSupabaseEnvCommand(connect),
			"supadupa_secret_reveal":  "supadupa-cli secrets reveal --ref " + project.Ref + " --kind <kind>",
			"supadupa_project_config": "supadupa-cli projects cli-profile --ref " + project.Ref,
		},
		SecretHandles: connect.SecretHandles,
		CompatibilityContracts: map[string]string{
			"control_plane": "Use supadupa Management API for lifecycle, RBAC, keys, backups, domains, config, branching, replicas, and audit.",
			"data_plane":    "Use Supabase-compatible URLs, Postgres URLs, SDKs, and selected Supabase CLI workflows against this project profile.",
			"secrets":       "Values are secret:// handles until an authorized audited reveal supplies concrete material.",
		},
	}
}

func connectPayloadForProject(project Project, poolerConfig map[string]string, databaseConfig map[string]string, secrets ...ProjectSecret) ConnectPayload {
	base := fmt.Sprintf("https://%s.%s", project.Ref, project.Spec.Domain)
	studioBase := fmt.Sprintf("https://studio.%s.%s", project.Ref, project.Spec.Domain)
	localAPIBase := localAPIURL(project.Ref)
	localStudioBase := localStudioURL(project.Ref)
	transactionPort := configValueOrDefault(poolerConfig, "transaction_port", "6543")
	sessionPort := configValueOrDefault(poolerConfig, "session_port", "5432")
	sslMode := postgresSSLMode(databaseConfig)
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
		"api":               base,
		"studio":            studioBase,
		"studio_via_api":    base + "/studio",
		"rest":              base + "/rest/v1",
		"rest_docs":         studioBase + "/project/default/api",
		"graphql":           base + "/graphql/v1",
		"graphql_explorer":  studioBase + "/project/default/api?panel=graphql",
		"logs":              base + "/logs",
		"auth":              base + "/auth/v1",
		"storage":           base + "/storage/v1",
		"storage_s3":        base + "/storage/v1/s3",
		"functions":         base + "/functions/v1",
		"realtime":          base + "/realtime/v1",
		"rest_service":      base + "/rest/v1",
		"graphql_service":   base + "/graphql/v1",
		"storage_service":   base + "/storage/v1",
		"functions_service": base + "/functions/v1",
		"realtime_service":  base + "/realtime/v1",
	}
	snippets := map[string]string{
		"uri_direct":             directURI,
		"uri_pool_transaction":   transactionURI,
		"uri_pool_session":       sessionURI,
		"psql_direct":            "psql " + directURI,
		"psql_pool_transaction":  "psql " + transactionURI,
		"psql_pool_session":      "psql " + sessionURI,
		"env_api_url":            fmt.Sprintf("SUPABASE_URL=%q", base),
		"env_publishable_key":    "SUPABASE_PUBLISHABLE_KEY=" + secretHandles["publishable_key"],
		"env_secret_key":         "SUPABASE_SECRET_KEY=" + secretHandles["secret_key"],
		"env_database_password":  "SUPABASE_DB_PASSWORD=" + secretHandles["db_password"],
		"env_storage_access_key": "SUPABASE_S3_ACCESS_KEY=" + secretHandles["s3_access_key"],
		"env_storage_secret_key": "SUPABASE_S3_SECRET_KEY=" + secretHandles["s3_secret_key"],
	}
	sdkSnippets := map[string]string{
		"javascript": fmt.Sprintf("createClient(%q, process.env.SUPABASE_PUBLISHABLE_KEY)", base),
		"python":     fmt.Sprintf("create_client(%q, os.environ[\"SUPABASE_PUBLISHABLE_KEY\"])", base),
		"flutter":    fmt.Sprintf("Supabase.initialize(url: %q, anonKey: supabaseAnonKey)", base),
		"swift":      fmt.Sprintf("SupabaseClient(supabaseURL: URL(string: %q)!, supabaseKey: supabasePublishableKey)", base),
	}
	if localStudioBase != "" {
		links["studio_local"] = localStudioBase
	}
	if localAPIBase != "" {
		links["api_local"] = localAPIBase
		links["rest_local"] = localAPIBase + "/rest/v1"
		links["auth_local"] = localAPIBase + "/auth/v1"
		links["graphql_local"] = localAPIBase + "/graphql/v1"
		links["storage_local"] = localAPIBase + "/storage/v1"
		links["functions_local"] = localAPIBase + "/functions/v1"
		links["realtime_local"] = localAPIBase + "/realtime/v1"
		snippets["env_local_api_url"] = fmt.Sprintf("SUPABASE_URL=%q", localAPIBase)
		snippets["local_supabase_env"] = "SUPABASE_URL=" + shellQuote(localAPIBase) + " SUPABASE_ANON_KEY=" + shellQuote(secretHandles["anon_key"])
		sdkSnippets["javascript_local"] = fmt.Sprintf("createClient(%q, process.env.SUPABASE_PUBLISHABLE_KEY)", localAPIBase)
		sdkSnippets["python_local"] = fmt.Sprintf("create_client(%q, os.environ[\"SUPABASE_PUBLISHABLE_KEY\"])", localAPIBase)
	}
	return ConnectPayload{
		APIURL:         base,
		LocalAPIURL:    localAPIBase,
		StudioURL:      studioBase,
		LocalStudioURL: localStudioBase,
		RESTURL:        base + "/rest/v1",
		AuthURL:        base + "/auth/v1",
		GraphQLURL:     base + "/graphql/v1",
		RealtimeURL:    base + "/realtime/v1",
		FunctionsURL:   base + "/functions/v1",
		StorageURL:     base + "/storage/v1",
		StorageS3URL:   base + "/storage/v1/s3",
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
		Postgres: map[string]string{
			"direct":      directURI,
			"transaction": transactionURI,
			"session":     sessionURI,
			"psql":        "psql " + directURI,
		},
		PostgresParts: map[string]map[string]string{
			"direct": {
				"host":            fmt.Sprintf("db.%s.internal", project.Ref),
				"port":            "5432",
				"database":        "postgres",
				"user":            "postgres",
				"password_handle": secretHandles["db_password"],
				"sslmode":         sslMode,
			},
			"transaction": {
				"host":            fmt.Sprintf("pooler.%s.internal", project.Ref),
				"port":            transactionPort,
				"database":        "postgres",
				"user":            "postgres." + project.Ref,
				"password_handle": secretHandles["db_password"],
				"sslmode":         sslMode,
			},
			"session": {
				"host":            fmt.Sprintf("pooler.%s.internal", project.Ref),
				"port":            sessionPort,
				"database":        "postgres",
				"user":            "postgres." + project.Ref,
				"password_handle": secretHandles["db_password"],
				"sslmode":         sslMode,
			},
		},
		Pooler: pooler,
		Storage: map[string]string{
			"s3_endpoint":       base + "/storage/v1/s3",
			"rest_endpoint":     base + "/storage/v1",
			"access_key_handle": secretHandles["s3_access_key"],
			"secret_key_handle": secretHandles["s3_secret_key"],
		},
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
		"# supadupa compatibility metadata for tools that need to bind a local workspace to a managed project.",
		"# Upstream Supabase CLI commands that accept --db-url or SUPABASE_* env vars should use the profile values below.",
		"[supadupa]",
		fmt.Sprintf("api_url = %q", connect.APIURL),
		fmt.Sprintf("local_api_url = %q", connect.LocalAPIURL),
		fmt.Sprintf("studio_url = %q", connect.StudioURL),
		fmt.Sprintf("local_studio_url = %q", connect.LocalStudioURL),
		fmt.Sprintf("rest_url = %q", connect.RESTURL),
		fmt.Sprintf("graphql_url = %q", connect.GraphQLURL),
		fmt.Sprintf("realtime_url = %q", connect.RealtimeURL),
		fmt.Sprintf("functions_url = %q", connect.FunctionsURL),
		fmt.Sprintf("storage_url = %q", connect.StorageURL),
		fmt.Sprintf("database_url = %q", connect.Postgres["direct"]),
		fmt.Sprintf("pooler_transaction_url = %q", connect.Postgres["transaction"]),
		fmt.Sprintf("pooler_session_url = %q", connect.Postgres["session"]),
		"",
		"[supadupa.secret_handles]",
		fmt.Sprintf("anon_key = %q", connect.SecretHandles["anon_key"]),
		fmt.Sprintf("service_role = %q", connect.SecretHandles["service_role"]),
		fmt.Sprintf("db_password = %q", connect.SecretHandles["db_password"]),
	}
	return strings.Join(lines, "\n") + "\n"
}

func localSupabaseEnvCommand(connect ConnectPayload) string {
	apiURL := connect.APIURL
	if connect.LocalAPIURL != "" {
		apiURL = connect.LocalAPIURL
	}
	return "SUPABASE_URL=" + shellQuote(apiURL) + " SUPABASE_ANON_KEY=" + shellQuote(connect.SecretHandles["anon_key"])
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
