package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"supadupa2026/internal/control"
	k8sprovisioner "supadupa2026/internal/provisioner/kubernetes"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintf(os.Stderr, "usage: %s <root-dir> <namespace> <project-ref> <domain>\n", os.Args[0])
		os.Exit(2)
	}
	rootDir, namespace, ref, domain := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	spec := control.ProjectSpec{
		Ref:          ref,
		OrgID:        "kind-smoke-org",
		Name:         "Kind Supabase Core Smoke",
		Domain:       domain,
		StackVersion: "15.8.1.060",
		Profile:      control.StackProfileEssential,
		ResourceTier: control.ResourceTierSmall,
		Environment: map[string]string{
			"ANON_KEY":                       "kind-smoke-anon-key",
			"DASHBOARD_PASSWORD":             "kind-smoke-dashboard-password",
			"DASHBOARD_USERNAME":             "kind-smoke-dashboard",
			"JWT_SECRET":                     "kind-smoke-jwt-secret-000000000000000000000000",
			"POSTGRES_DB":                    "postgres",
			"POSTGRES_PASSWORD":              "kind-smoke-postgres-password",
			"POSTGRES_PORT":                  "5432",
			"POSTGRES_USER":                  "postgres",
			"PROJECT_REF":                    ref,
			"SERVICE_ROLE_KEY":               "kind-smoke-service-role-key",
			"SITE_URL":                       "http://" + ref + "-kong:8000",
			"SUPABASE_ANON_KEY":              "kind-smoke-anon-key",
			"SUPABASE_PUBLIC_URL":            "http://" + ref + "-kong:8000",
			"SUPABASE_SERVICE_KEY":           "kind-smoke-service-role-key",
			"SUPABASE_SECRET_KEY":            "kind-smoke-secret-key",
			"SUPABASE_PUBLISHABLE_KEY":       "kind-smoke-publishable-key",
			"SECRET_KEY_BASE":                "kind-smoke-secret-key-base-000000000000000000000000",
			"VAULT_ENC_KEY":                  "kind-smoke-vault-enc-key",
			"GOTRUE_API_HOST":                "0.0.0.0",
			"GOTRUE_API_PORT":                "9999",
			"API_EXTERNAL_URL":               "http://" + ref + "-kong:8000/auth/v1",
			"GOTRUE_DB_DATABASE_URL":         "postgres://supabase_auth_admin:kind-smoke-postgres-password@" + ref + "-db:5432/postgres",
			"GOTRUE_DISABLE_SIGNUP":          "true",
			"GOTRUE_JWT_SECRET":              "kind-smoke-jwt-secret-000000000000000000000000",
			"GOTRUE_SITE_URL":                "http://" + ref + "-kong:8000",
			"PGRST_DB_ANON_ROLE":             "anon",
			"PGRST_DB_SCHEMAS":               "public,storage,graphql_public",
			"PGRST_DB_URI":                   "postgres://authenticator:kind-smoke-postgres-password@" + ref + "-db:5432/postgres",
			"PGRST_JWT_SECRET":               "kind-smoke-jwt-secret-000000000000000000000000",
			"PGRST_SERVER_PORT":              "3000",
			"PGRST_SERVER_HOST":              "0.0.0.0",
			"PGRST_DB_USE_LEGACY_GUCS":       "false",
			"DB_ENC_KEY":                     "kind-smoke-db-enc-key",
			"FUNCTIONS_VERIFY_JWT":           "true",
			"REGION":                         "local",
			"REQUEST_ALLOW_X_FORWARDED_PATH": "true",
			"STORAGE_BACKEND":                "file",
			"STORAGE_PUBLIC_URL":             "http://" + ref + "-kong:8000/storage/v1",
			"STORAGE_TENANT_ID":              ref,
		},
		Services: map[string]control.ServiceSpec{
			"auth":      {Enabled: true},
			"rest":      {Enabled: true},
			"realtime":  {Enabled: false},
			"storage":   {Enabled: false},
			"imgproxy":  {Enabled: false},
			"functions": {Enabled: false},
			"pooler":    {Enabled: false},
			"studio":    {Enabled: false},
			"analytics": {Enabled: false},
			"vector":    {Enabled: false},
		},
	}
	provisioner := k8sprovisioner.NewWithOptions(k8sprovisioner.Options{RootDir: rootDir, Namespace: namespace})
	if err := provisioner.Create(context.Background(), spec); err != nil {
		fmt.Fprintf(os.Stderr, "render Kubernetes core smoke project: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(filepath.Join(rootDir, ref, "project.yaml"))
}
