package control

type secretEnvironmentMapping struct {
	EnvKey     string
	SecretKind string
}

var managedSecretEnvironmentMappings = []secretEnvironmentMapping{
	{EnvKey: "JWT_SECRET", SecretKind: "jwt_secret"},
	{EnvKey: "GOTRUE_JWT_SECRET", SecretKind: "jwt_secret"},
	{EnvKey: "PGRST_JWT_SECRET", SecretKind: "jwt_secret"},
	{EnvKey: "REALTIME_JWT_SECRET", SecretKind: "jwt_secret"},
	{EnvKey: "SUPADUPA_JWT_SIGNING_KEY_CURRENT", SecretKind: "jwt_signing_key_current"},
	{EnvKey: "SUPADUPA_JWT_SIGNING_KEY_NEXT", SecretKind: "jwt_signing_key_next"},
	{EnvKey: "ANON_KEY", SecretKind: "anon_key"},
	{EnvKey: "SERVICE_ROLE_KEY", SecretKind: "service_role"},
	{EnvKey: "SUPABASE_PUBLISHABLE_KEY", SecretKind: "publishable_key"},
	{EnvKey: "SUPABASE_SECRET_KEY", SecretKind: "secret_key"},
	{EnvKey: "POSTGRES_PASSWORD", SecretKind: "db_password"},
	{EnvKey: "S3_ACCESS_KEY", SecretKind: "s3_access_key"},
	{EnvKey: "S3_SECRET_KEY", SecretKind: "s3_secret_key"},
	{EnvKey: "STORAGE_ACCESS_KEY_ID", SecretKind: "s3_access_key"},
	{EnvKey: "STORAGE_SECRET_ACCESS_KEY", SecretKind: "s3_secret_key"},
	{EnvKey: "S3_PROTOCOL_ACCESS_KEY_ID", SecretKind: "s3_access_key"},
	{EnvKey: "S3_PROTOCOL_ACCESS_KEY_SECRET", SecretKind: "s3_secret_key"},
}

func ManagedSecretEnvironmentKeys() []string {
	keys := make([]string, 0, len(managedSecretEnvironmentMappings))
	for _, mapping := range managedSecretEnvironmentMappings {
		keys = append(keys, mapping.EnvKey)
	}
	return keys
}

func ProjectSpecWithSecrets(spec ProjectSpec, secrets []ProjectSecret) ProjectSpec {
	next := spec
	next.Environment = cloneStringMap(spec.Environment)
	values := map[string]string{}
	for _, secret := range secrets {
		values[secret.Kind] = secret.Value
	}
	for _, mapping := range managedSecretEnvironmentMappings {
		if value := values[mapping.SecretKind]; value != "" {
			next.Environment[mapping.EnvKey] = value
		}
	}
	return next
}
