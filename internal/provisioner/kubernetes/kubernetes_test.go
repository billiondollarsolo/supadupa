package kubernetes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"supadupa2026/internal/control"

	"gopkg.in/yaml.v3"
)

func TestProvisionerImplementsContract(t *testing.T) {
	var _ control.Provisioner = New()
	var _ control.OptionedDestroyer = New()
	var _ control.ConfigSyncer = New()
	var _ control.ServiceSyncer = New()
	var _ control.AuthHookSyncer = New()
	var _ control.BranchCloner = New()
}

func decodeRenderedProjectManifest(t *testing.T, payload []byte) projectManifest {
	t.Helper()
	var manifest projectManifest
	if err := yaml.Unmarshal(payload, &manifest); err != nil {
		t.Fatalf("decode project manifest failed: %v\n%s", err, payload)
	}
	return manifest
}

func decodeYAMLManifest[T any](t *testing.T, payload []byte) T {
	t.Helper()
	var manifest T
	if err := yaml.Unmarshal(payload, &manifest); err != nil {
		t.Fatalf("decode YAML manifest failed: %v\n%s", err, payload)
	}
	return manifest
}

func hasRenderedDependency(dependencies []kubernetesRenderedDependency, service string, port int) bool {
	for _, dependency := range dependencies {
		if dependency.Service == service && dependency.Port == port {
			return true
		}
	}
	return false
}

func assertKubernetesServiceHasResources(t *testing.T, services map[string]kubernetesRenderedService, name string) {
	t.Helper()
	service, ok := services[name]
	if !ok {
		t.Fatalf("expected rendered service %s in %#v", name, services)
	}
	if service.Resources == nil || service.Resources.Requests["cpu"] == "" || service.Resources.Requests["memory"] == "" ||
		service.Resources.Limits["cpu"] == "" || service.Resources.Limits["memory"] == "" {
		t.Fatalf("expected %s to have CPU/memory requests and limits, got %#v", name, service.Resources)
	}
}

func TestCreateFailsWhenStackReleaseCatalogCannotResolve(t *testing.T) {
	root := t.TempDir()
	// Filter catalog so neither requested nor default stack release is present.
	t.Setenv("SUPADUPA_SUPPORTED_STACK_VERSIONS", "does-not-exist-in-catalog")
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})

	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "unresolvable",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
	})
	if err == nil {
		t.Fatal("expected create to fail when stack release catalog cannot resolve")
	}
	if !strings.Contains(err.Error(), "not available in the active catalog") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateRendersProjectCRD(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})

	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		OrgID:        "org-one",
		Name:         "Alpha",
		Domain:       "supadupa.test",
		StackVersion: "15.8.1.060",
		Profile:      control.StackProfileFull,
		ResourceTier: control.ResourceTierSmall,
		Environment:  map[string]string{"CUSTOM": "value"},
		Services: map[string]control.ServiceSpec{
			"storage": {Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "alpha", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeRenderedProjectManifest(t, payload)
	if manifest.Kind != "Project" {
		t.Fatalf("expected Project kind, got %q", manifest.Kind)
	}
	if manifest.Metadata.Namespace != "platform" {
		t.Fatalf("expected platform namespace, got %q", manifest.Metadata.Namespace)
	}
	if manifest.Spec.Ref != "alpha" || manifest.Spec.StackVersion != "15.8.1.060" || manifest.Spec.ResourceTier != "small" {
		t.Fatalf("unexpected project spec: %#v", manifest.Spec)
	}
	if manifest.Spec.RuntimeSecurityDefaults.SeccompProfile != "RuntimeDefault" ||
		manifest.Spec.RuntimeSecurityDefaults.AllowPrivilegeEscalation ||
		len(manifest.Spec.RuntimeSecurityDefaults.DropCapabilities) != 1 ||
		manifest.Spec.RuntimeSecurityDefaults.DropCapabilities[0] != "ALL" {
		t.Fatalf("unexpected runtime security defaults: %#v", manifest.Spec.RuntimeSecurityDefaults)
	}
	if manifest.Spec.Environment["CUSTOM"] != "value" {
		t.Fatalf("expected CUSTOM environment value, got %#v", manifest.Spec.Environment)
	}
	db := manifest.Spec.Services["db"]
	if db.Image != "supabase/postgres:15.8.1.060" || len(db.Ports) != 1 || db.Ports[0].Port != 5432 {
		t.Fatalf("unexpected db service: %#v", db)
	}
	if db.RunAsNonRoot == nil || *db.RunAsNonRoot {
		t.Fatalf("expected generated db service to opt out of runAsNonRoot for upstream postgres image, got %#v", db.RunAsNonRoot)
	}
	if db.ReadOnlyRootFilesystem || len(db.DropCapabilities) != 1 || db.DropCapabilities[0] != "NET_RAW" {
		t.Fatalf("expected generated db service to use upstream-compatible security overrides, got %#v", db)
	}
	if db.LivenessProbe == nil || db.LivenessProbe.InitialDelaySeconds != 120 {
		t.Fatalf("expected generated db liveness delay to protect first-boot seed scripts, got %#v", db.LivenessProbe)
	}
	if len(db.Volumes) != 1 || db.Volumes[0].MountPath != "/var/lib/postgresql/data" {
		t.Fatalf("unexpected db volumes: %#v", db.Volumes)
	}
	if len(db.Command) != 2 || db.Command[0] != "bash" || db.Command[1] != "-lc" || len(db.Args) != 1 ||
		!strings.Contains(db.Args[0], "docker-entrypoint.sh postgres -c listen_addresses='*'") ||
		!strings.Contains(db.Args[0], "-v \"supadupa_postgres_password=${POSTGRES_PASSWORD:?}\"") ||
		!strings.Contains(db.Args[0], "-f /etc/postgresql.schema.sql") {
		t.Fatalf("expected generated db startup bootstrap command, got command=%#v args=%#v", db.Command, db.Args)
	}
	if len(db.ConfigFiles) != 1 || db.ConfigFiles[0].MountPath != "/etc/postgresql.schema.sql" {
		t.Fatalf("expected generated db bootstrap SQL config file, got %#v", db.ConfigFiles)
	}
	if !strings.Contains(db.ConfigFiles[0].Content, "CREATE SCHEMA IF NOT EXISTS auth") ||
		!strings.Contains(db.ConfigFiles[0].Content, "CREATE ROLE authenticator NOINHERIT LOGIN") ||
		!strings.Contains(db.ConfigFiles[0].Content, "ALTER ROLE supabase_auth_admin WITH PASSWORD :'supadupa_postgres_password'") ||
		!strings.Contains(db.ConfigFiles[0].Content, "GRANT USAGE, CREATE ON SCHEMA public TO supabase_auth_admin, supabase_storage_admin") ||
		!strings.Contains(db.ConfigFiles[0].Content, "CREATE PUBLICATION supabase_realtime") {
		t.Fatalf("expected Kubernetes db bootstrap SQL to render Supabase roles, schemas, and publications, got:\n%s", db.ConfigFiles[0].Content)
	}
	kong := manifest.Spec.Services["kong"]
	if kong.Image != "kong/kong:3.9.1" || kong.Ingress == nil || kong.Ingress.Host != "alpha.supadupa.test" {
		t.Fatalf("unexpected kong service: %#v", kong)
	}
	if kong.RunAsNonRoot == nil || *kong.RunAsNonRoot {
		t.Fatalf("expected generated kong service to opt out of runAsNonRoot for upstream image metadata, got %#v", kong.RunAsNonRoot)
	}
	if kong.Env["SUPABASE_SERVICE_KEY"] != "$(SERVICE_ROLE_KEY)" || kong.Env["KONG_PLUGINS"] == "" ||
		kong.Env["KONG_DECLARATIVE_CONFIG"] != "/tmp/kong/kong.yml" || kong.Env["KONG_PREFIX"] != "/tmp/kong-prefix" {
		t.Fatalf("expected Kong service env parity keys, got %#v", kong.Env)
	}
	if len(kong.Command) != 2 || kong.Command[0] != "/bin/sh" || kong.Command[1] != "-ec" || len(kong.Args) != 1 ||
		!strings.Contains(kong.Args[0], "awk") || !strings.Contains(kong.Args[0], "exec /entrypoint.sh kong docker-start") {
		t.Fatalf("expected Kong startup script command/args, got command=%#v args=%#v", kong.Command, kong.Args)
	}
	if kong.ReadinessProbe == nil || kong.ReadinessProbe.Path != "/auth/v1/health" || kong.LivenessProbe == nil || kong.LivenessProbe.Path != "/auth/v1/health" {
		t.Fatalf("expected Kong probes to use routed health endpoint, got readiness=%#v liveness=%#v", kong.ReadinessProbe, kong.LivenessProbe)
	}
	if len(kong.WritablePaths) != 1 || kong.WritablePaths[0].MountPath != "/tmp" {
		t.Fatalf("expected Kong writable tmp path, got %#v", kong.WritablePaths)
	}
	if len(kong.ConfigFiles) != 1 || kong.ConfigFiles[0].MountPath != "/home/kong/kong.yml" {
		t.Fatalf("expected Kong declarative config file mount, got %#v", kong.ConfigFiles)
	}
	if !strings.Contains(kong.ConfigFiles[0].Content, `_format_version: "2.1"`) ||
		!strings.Contains(kong.ConfigFiles[0].Content, "url: http://alpha-auth:9999/") ||
		!strings.Contains(kong.ConfigFiles[0].Content, "paths: [/auth/v1/]") ||
		!strings.Contains(kong.ConfigFiles[0].Content, "keyauth_credentials:") ||
		!strings.Contains(kong.ConfigFiles[0].Content, `"Authorization: $LUA_AUTH_EXPR"`) {
		t.Fatalf("expected Kubernetes Kong config routes, got:\n%s", kong.ConfigFiles[0].Content)
	}
	meta := manifest.Spec.Services["meta"]
	if meta.Env["PG_META_DB_PASSWORD"] != "$(POSTGRES_PASSWORD)" || meta.Env["PG_META_DB_HOST"] != "alpha-db" {
		t.Fatalf("expected postgres-meta database env aliases, got %#v", meta.Env)
	}
	if meta.RunAsNonRoot == nil || *meta.RunAsNonRoot {
		t.Fatalf("expected generated meta service to opt out of runAsNonRoot for upstream image metadata, got %#v", meta.RunAsNonRoot)
	}
	auth := manifest.Spec.Services["auth"]
	if !auth.ReadOnlyRootFilesystem || len(auth.WritablePaths) != 1 || auth.WritablePaths[0].MountPath != "/tmp" {
		t.Fatalf("expected auth read-only root filesystem with writable tmp path: %#v", auth)
	}
	if auth.RunAsNonRoot == nil || *auth.RunAsNonRoot {
		t.Fatalf("expected generated auth service to opt out of runAsNonRoot for upstream image metadata, got %#v", auth.RunAsNonRoot)
	}
	if auth.Env["GOTRUE_DB_DRIVER"] != "postgres" {
		t.Fatalf("expected auth env defaults, got %#v", auth.Env)
	}
	if auth.ReadinessProbe == nil || auth.ReadinessProbe.Type != "http" || auth.ReadinessProbe.Path != "/health" || auth.ReadinessProbe.Port != 9999 {
		t.Fatalf("unexpected auth readiness probe: %#v", auth.ReadinessProbe)
	}
	if auth.LivenessProbe == nil || auth.LivenessProbe.Type != "http" || auth.LivenessProbe.Path != "/health" || auth.LivenessProbe.Port != 9999 {
		t.Fatalf("unexpected auth liveness probe: %#v", auth.LivenessProbe)
	}
	if !hasRenderedDependency(auth.DependsOn, "db", 5432) {
		t.Fatalf("expected auth dependency on db:5432, got %#v", auth.DependsOn)
	}
	storage := manifest.Spec.Services["storage"]
	if !storage.Enabled || storage.Image != "supabase/storage-api:v1.60.4" || len(storage.Ports) != 1 || storage.Ports[0].Port != 5000 {
		t.Fatalf("unexpected storage service: %#v", storage)
	}
	if storage.Env["DATABASE_URL"] != "postgres://supabase_storage_admin:$(POSTGRES_PASSWORD)@alpha-db:5432/$(POSTGRES_DB)" ||
		storage.Env["POSTGREST_URL"] != "http://alpha-rest:3000" ||
		storage.Env["SERVICE_KEY"] != "$(SERVICE_ROLE_KEY)" ||
		storage.Env["FILE_STORAGE_BACKEND_PATH"] != "/var/lib/storage" {
		t.Fatalf("expected storage Kubernetes service env defaults, got %#v", storage.Env)
	}
	if len(storage.Volumes) != 1 || storage.Volumes[0].MountPath != "/var/lib/storage" {
		t.Fatalf("unexpected storage volumes: %#v", storage.Volumes)
	}
	if storage.Ingress == nil || storage.Ingress.Host != "storage-alpha.supadupa.test" {
		t.Fatalf("unexpected storage ingress: %#v", storage.Ingress)
	}
	if !hasRenderedDependency(storage.DependsOn, "db", 5432) || !hasRenderedDependency(storage.DependsOn, "rest", 3000) {
		t.Fatalf("expected storage dependencies on db and rest, got %#v", storage.DependsOn)
	}
	realtime := manifest.Spec.Services["realtime"]
	if realtime.Env["DB_HOST"] != "alpha-db" ||
		realtime.Env["DB_AFTER_CONNECT_QUERY"] != "SET search_path TO _realtime" ||
		realtime.Env["SELF_HOST_TENANT_NAME"] != "$(PROJECT_REF)" {
		t.Fatalf("expected realtime Kubernetes service env defaults, got %#v", realtime.Env)
	}
	functions := manifest.Spec.Services["functions"]
	if functions.Env["SUPABASE_DB_URL"] != "postgresql://postgres:$(POSTGRES_PASSWORD)@alpha-db:5432/$(POSTGRES_DB)" ||
		functions.Env["SUPABASE_URL"] != "http://alpha-kong:8000" {
		t.Fatalf("expected functions Kubernetes service env defaults, got %#v", functions.Env)
	}
	pooler := manifest.Spec.Services["pooler"]
	if pooler.Env["DATABASE_URL"] != "ecto://supabase_admin:$(POSTGRES_PASSWORD)@alpha-db:5432/_supabase" ||
		pooler.Env["POSTGRES_HOST"] != "alpha-db" ||
		pooler.Env["POOLER_POOL_MODE"] != "transaction" {
		t.Fatalf("expected pooler Kubernetes service env defaults, got %#v", pooler.Env)
	}
	analytics := manifest.Spec.Services["analytics"]
	if analytics.Env["POSTGRES_BACKEND_URL"] != "postgresql://supabase_admin:$(POSTGRES_PASSWORD)@alpha-db:5432/$(POSTGRES_DB)" ||
		analytics.Env["DB_HOSTNAME"] != "alpha-db" ||
		analytics.Env["LOGFLARE_SUPABASE_MODE"] != "true" {
		t.Fatalf("expected analytics Kubernetes service env defaults, got %#v", analytics.Env)
	}
}

func TestCreateStampsRuntimeNamespace(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform", Isolation: true})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{Ref: "alpha", Domain: "supadupa.test"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "alpha", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeRenderedProjectManifest(t, payload)
	if manifest.Metadata.Namespace != "platform" {
		t.Fatalf("expected Project CR to stay in control namespace, got %q", manifest.Metadata.Namespace)
	}
	if manifest.Spec.RuntimeNamespace != "supadupa-proj-alpha" {
		t.Fatalf("expected stamped runtime namespace, got %q", manifest.Spec.RuntimeNamespace)
	}
}

func TestCreateStampsRuntimeNamespaceWithCustomPrefix(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform", Isolation: true, RuntimeNamespacePrefix: "tenant-"})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{Ref: "alpha", Domain: "supadupa.test"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "alpha", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeRenderedProjectManifest(t, payload)
	if manifest.Spec.RuntimeNamespace != "tenant-alpha" {
		t.Fatalf("expected custom prefixed runtime namespace, got %q", manifest.Spec.RuntimeNamespace)
	}
}

func TestCreateLegacyModeOmitsRuntimeNamespace(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform", Isolation: false})

	if err := provisioner.Create(context.Background(), control.ProjectSpec{Ref: "alpha", Domain: "supadupa.test"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "alpha", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeRenderedProjectManifest(t, payload)
	if manifest.Spec.RuntimeNamespace != "" {
		t.Fatalf("expected empty runtime namespace in legacy mode, got %q", manifest.Spec.RuntimeNamespace)
	}
}

func TestCreateFiltersDependenciesForDisabledOptionalServices(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})

	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
		Services: map[string]control.ServiceSpec{
			"functions": {Enabled: false},
		},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "alpha", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeRenderedProjectManifest(t, payload)
	kong := manifest.Spec.Services["kong"]
	if hasRenderedDependency(kong.DependsOn, "functions", 9000) {
		t.Fatalf("kong should not wait on disabled functions service: %#v", kong.DependsOn)
	}
	if !hasRenderedDependency(kong.DependsOn, "auth", 9999) {
		t.Fatalf("kong should retain dependencies on enabled services: %#v", kong.DependsOn)
	}
}

func TestCreateRendersCustomResourceDefinitions(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	if err := provisioner.Create(context.Background(), control.ProjectSpec{Ref: "alpha", Domain: "supadupa.test"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	expected := map[string]string{
		"projects.yaml":                 "Project",
		"projectconfigs.yaml":           "ProjectConfig",
		"projectauthhooks.yaml":         "ProjectAuthHooks",
		"projectbranchclones.yaml":      "ProjectBranchClone",
		"projectreplicas.yaml":          "ProjectReplica",
		"retainedprojectresources.yaml": "RetainedProjectResources",
	}
	for filename, kind := range expected {
		payload, err := os.ReadFile(filepath.Join(root, "_crds", filename))
		if err != nil {
			t.Fatalf("expected CRD file %s: %v", filename, err)
		}
		manifest := decodeYAMLManifest[customResourceDefinitionManifest](t, payload)
		if manifest.Kind != "CustomResourceDefinition" || manifest.Metadata.Name != strings.TrimSuffix(filename, ".yaml")+".platform.supadupa.dev" {
			t.Fatalf("unexpected CRD metadata for %s: %#v", filename, manifest)
		}
		if manifest.Spec.Group != "platform.supadupa.dev" || manifest.Spec.Scope != "Namespaced" || manifest.Spec.Names.Kind != kind {
			t.Fatalf("unexpected CRD spec for %s: %#v", filename, manifest.Spec)
		}
		if len(manifest.Spec.Versions) != 1 || !manifest.Spec.Versions[0].Served || !manifest.Spec.Versions[0].Storage {
			t.Fatalf("unexpected CRD versions for %s: %#v", filename, manifest.Spec.Versions)
		}
		if _, ok := manifest.Spec.Versions[0].Subresources["status"]; !ok {
			t.Fatalf("expected status subresource in %s: %#v", filename, manifest.Spec.Versions[0].Subresources)
		}
		schema := manifest.Spec.Versions[0].Schema["openAPIV3Schema"].(map[string]any)
		if schema["type"] != "object" {
			t.Fatalf("expected object schema in %s: %#v", filename, schema)
		}
		if kind == "Project" {
			specSchema := schema["properties"].(map[string]any)["spec"].(map[string]any)
			specProperties := specSchema["properties"].(map[string]any)
			services := specProperties["services"].(map[string]any)
			serviceProperties := services["additionalProperties"].(map[string]any)["properties"].(map[string]any)
			configFiles := serviceProperties["configFiles"].(map[string]any)
			configFileProperties := configFiles["items"].(map[string]any)["properties"].(map[string]any)
			runtimeDefaults := specProperties["runtimeSecurityDefaults"].(map[string]any)
			runtimeNetwork := specProperties["runtimeNetwork"].(map[string]any)["properties"].(map[string]any)
			statusProperties := schema["properties"].(map[string]any)["status"].(map[string]any)["properties"].(map[string]any)
			if specProperties["runtimeNamespace"].(map[string]any)["type"] != "string" ||
				runtimeNetwork["databaseService"].(map[string]any)["type"] != "string" ||
				runtimeNetwork["allowedEgressCidrs"].(map[string]any)["items"].(map[string]any)["type"] != "string" {
				t.Fatalf("expected Project runtime namespace/network schema in %s: %#v", filename, specProperties)
			}
			if specSchema["x-kubernetes-preserve-unknown-fields"] == true ||
				specProperties["orgId"].(map[string]any)["type"] != "string" ||
				specProperties["displayName"].(map[string]any)["type"] != "string" ||
				specProperties["hostId"].(map[string]any)["type"] != "string" ||
				runtimeDefaults["properties"].(map[string]any)["allowPrivilegeEscalation"].(map[string]any)["type"] != "boolean" ||
				serviceProperties["command"].(map[string]any)["items"].(map[string]any)["type"] != "string" ||
				serviceProperties["args"].(map[string]any)["items"].(map[string]any)["type"] != "string" ||
				serviceProperties["runAsNonRoot"].(map[string]any)["type"] != "boolean" ||
				serviceProperties["allowPrivilegeEscalation"].(map[string]any)["type"] != "boolean" ||
				serviceProperties["dropCapabilities"].(map[string]any)["items"].(map[string]any)["type"] != "string" ||
				configFileProperties["content"].(map[string]any)["type"] != "string" ||
				statusProperties["observedGeneration"].(map[string]any)["format"] != "int64" {
				t.Fatalf("expected Project structural service/runtime/status schema in %s: %#v", filename, schema)
			}
		} else if kind == "ProjectReplica" {
			specSchema := schema["properties"].(map[string]any)["spec"].(map[string]any)
			runtimeDefaults := specSchema["properties"].(map[string]any)["runtimeSecurityDefaults"].(map[string]any)
			allowPrivilegeEscalation := runtimeDefaults["properties"].(map[string]any)["allowPrivilegeEscalation"].(map[string]any)
			if allowPrivilegeEscalation["type"] != "boolean" {
				t.Fatalf("expected ProjectReplica runtime security schema in %s: %#v", filename, schema)
			}
		} else if kind == "ProjectConfig" {
			specSchema := schema["properties"].(map[string]any)["spec"].(map[string]any)
			config := specSchema["properties"].(map[string]any)["config"].(map[string]any)
			values := config["additionalProperties"].(map[string]any)
			if specSchema["x-kubernetes-preserve-unknown-fields"] == true || values["type"] != "string" {
				t.Fatalf("expected ProjectConfig structural config schema in %s: %#v", filename, schema)
			}
		} else if kind == "ProjectAuthHooks" {
			specSchema := schema["properties"].(map[string]any)["spec"].(map[string]any)
			hooks := specSchema["properties"].(map[string]any)["hooks"].(map[string]any)
			headers := hooks["items"].(map[string]any)["properties"].(map[string]any)["headers"].(map[string]any)
			if specSchema["x-kubernetes-preserve-unknown-fields"] == true || headers["additionalProperties"].(map[string]any)["type"] != "string" {
				t.Fatalf("expected ProjectAuthHooks structural hooks schema in %s: %#v", filename, schema)
			}
		} else if kind == "ProjectBranchClone" {
			specSchema := schema["properties"].(map[string]any)["spec"].(map[string]any)
			sourceRef := specSchema["properties"].(map[string]any)["sourceRef"].(map[string]any)
			if specSchema["x-kubernetes-preserve-unknown-fields"] == true || sourceRef["minLength"] != 1 {
				t.Fatalf("expected ProjectBranchClone structural sourceRef schema in %s: %#v", filename, schema)
			}
		} else if kind == "RetainedProjectResources" {
			specSchema := schema["properties"].(map[string]any)["spec"].(map[string]any)
			resources := specSchema["properties"].(map[string]any)["resources"].(map[string]any)
			name := resources["items"].(map[string]any)["properties"].(map[string]any)["name"].(map[string]any)
			if specSchema["x-kubernetes-preserve-unknown-fields"] == true || name["minLength"] != 1 {
				t.Fatalf("expected RetainedProjectResources structural resource schema in %s: %#v", filename, schema)
			}
		}
	}
}

func TestCreateCanSkipApplyingCustomResourceDefinitions(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "kubectl.log")
	kubectlPath := filepath.Join(root, "kubectl")
	if err := os.WriteFile(kubectlPath, []byte("#!/usr/bin/env sh\nprintf '%s\\n' \"$*\" >>\""+logPath+"\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform", Apply: true, SkipCRDApply: true, Command: kubectlPath})
	if err := provisioner.Create(context.Background(), control.ProjectSpec{Ref: "alpha", Domain: "supadupa.test"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	payload, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logged := strings.TrimSpace(string(payload))
	expected := "apply -f " + filepath.Join(root, "alpha", "project.yaml")
	if logged != expected {
		t.Fatalf("expected only project manifest apply %q, got %q", expected, logged)
	}
	if _, err := os.Stat(filepath.Join(root, "_crds", "projects.yaml")); err != nil {
		t.Fatalf("expected CRD manifest rendered even when apply is skipped: %v", err)
	}
}

func TestSyncSecretsUpdatesProjectEnvironment(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:         "alpha",
		Domain:      "supadupa.test",
		Environment: map[string]string{"CUSTOM": "before", "JWT_SECRET": "old-jwt"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	err = provisioner.SyncSecrets(context.Background(), "alpha", control.ProjectSpec{
		Ref: "alpha",
		Environment: map[string]string{
			"JWT_SECRET":        "jwt-rotated",
			"POSTGRES_PASSWORD": "db-rotated",
			"SERVICE_ROLE_KEY":  "service-rotated",
			"CUSTOM":            "not-secret",
		},
	})
	if err != nil {
		t.Fatalf("sync secrets failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "alpha", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeRenderedProjectManifest(t, payload)
	for key, expected := range map[string]string{
		"JWT_SECRET":        "jwt-rotated",
		"POSTGRES_PASSWORD": "db-rotated",
		"SERVICE_ROLE_KEY":  "service-rotated",
		"CUSTOM":            "not-secret",
	} {
		if manifest.Spec.Environment[key] != expected {
			t.Fatalf("expected project environment %s=%q, got %#v", key, expected, manifest.Spec.Environment)
		}
	}
}

func TestSyncSecretsApplyReappliesProjectManifest(t *testing.T) {
	root := t.TempDir()
	renderer := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	if err := renderer.Create(context.Background(), control.ProjectSpec{Ref: "alpha", Domain: "supadupa.test"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	logPath := filepath.Join(root, "kubectl.log")
	appliedProjectPath := filepath.Join(root, "applied-project.yaml")
	kubectlPath := filepath.Join(root, "kubectl")
	script := fmt.Sprintf(`#!/usr/bin/env sh
printf '%%s\n' "$*" >>%q
if [ "$1" = "apply" ] && [ "$2" = "-f" ] && [ "$(basename "$3")" = "project.yaml" ]; then
  cp "$3" %q
fi
`, logPath, appliedProjectPath)
	if err := os.WriteFile(kubectlPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform", Apply: true, SkipCRDApply: true, Command: kubectlPath})
	if err := provisioner.SyncSecrets(context.Background(), "alpha", control.ProjectSpec{
		Ref:         "alpha",
		Environment: map[string]string{"POSTGRES_PASSWORD": "db-rotated"},
	}); err != nil {
		t.Fatalf("sync secrets failed: %v", err)
	}

	logPayload, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := "apply -f " + filepath.Join(root, "alpha", "project.yaml")
	if strings.TrimSpace(string(logPayload)) != expected {
		t.Fatalf("expected kubectl apply of Project manifest %q, got:\n%s", expected, logPayload)
	}
	appliedPayload, err := os.ReadFile(appliedProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	applied := decodeRenderedProjectManifest(t, appliedPayload)
	if applied.Spec.Environment["POSTGRES_PASSWORD"] != "db-rotated" {
		t.Fatalf("expected applied Project environment to include rotated secret, got %#v", applied.Spec.Environment)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha", "project-secrets.yaml")); !os.IsNotExist(err) {
		t.Fatalf("SyncSecrets should not render unused standalone project secret manifest, got err=%v", err)
	}
}

func TestCreateQuotesSpecialYAMLKeysAndValues(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Name:   "Alpha \"Quoted\"\nProject",
		Domain: "supadupa.test",
		Environment: map[string]string{
			"PLAIN_KEY":   "plain",
			"123":         "numeric key",
			"true":        "boolean-looking key",
			"key:with":    "colon:value",
			"space key":   "line one\nline two",
			"path/key":    `quote " slash \`,
			"percent%key": "100%",
		},
		Services: map[string]control.ServiceSpec{
			"edge/functions": {
				Enabled: true,
				Config: map[string]string{
					"allowPrivilegeEscalation": "true",
					"config key":               "value:with:colon",
					"dropCapabilities":         "NET_RAW,CHOWN",
					"env.CUSTOM_ENV":           "custom:value",
					"runAsNonRoot":             "false",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "alpha", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeRenderedProjectManifest(t, payload)
	if manifest.Spec.DisplayName != "Alpha \"Quoted\"\nProject" {
		t.Fatalf("unexpected display name %q", manifest.Spec.DisplayName)
	}
	for key, expected := range map[string]string{
		"PLAIN_KEY":   "plain",
		"123":         "numeric key",
		"true":        "boolean-looking key",
		"key:with":    "colon:value",
		"space key":   "line one\nline two",
		"path/key":    `quote " slash \`,
		"percent%key": "100%",
	} {
		if manifest.Spec.Environment[key] != expected {
			t.Fatalf("expected environment %q=%q, got %#v", key, expected, manifest.Spec.Environment)
		}
	}
	edgeFunctions := manifest.Spec.Services["edge/functions"]
	if !edgeFunctions.Enabled || edgeFunctions.Config["config key"] != "value:with:colon" || edgeFunctions.Env["CUSTOM_ENV"] != "custom:value" ||
		edgeFunctions.RunAsNonRoot == nil || *edgeFunctions.RunAsNonRoot ||
		edgeFunctions.AllowPrivilegeEscalation == nil || !*edgeFunctions.AllowPrivilegeEscalation ||
		len(edgeFunctions.DropCapabilities) != 2 || edgeFunctions.DropCapabilities[0] != "NET_RAW" || edgeFunctions.DropCapabilities[1] != "CHOWN" {
		t.Fatalf("unexpected edge/functions service: %#v", edgeFunctions)
	}
}

func TestDestroyWithOptionsRetainsResourceManifest(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	if err := provisioner.Create(context.Background(), control.ProjectSpec{Ref: "alpha", Domain: "supadupa.test"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "storage",
		Config:     map[string]string{"s3_compat_enabled": "true"},
	}); err != nil {
		t.Fatalf("sync config failed: %v", err)
	}
	if err := provisioner.DestroyWithOptions(context.Background(), "alpha", control.DestroyOptions{RetainVolumes: true}); err != nil {
		t.Fatalf("destroy failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("expected project directory removed, got err=%v", err)
	}
	matches, err := filepath.Glob(filepath.Join(root, "_retained", "alpha-*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one retained resource manifest, got %#v", matches)
	}
	payload, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeYAMLManifest[retainedProjectResourcesManifest](t, payload)
	if manifest.Kind != "RetainedProjectResources" || manifest.Metadata.Namespace != "platform" || manifest.Spec.ProjectRef != "alpha" {
		t.Fatalf("unexpected retained resources manifest: %#v", manifest)
	}
	expectedResources := map[string]struct{}{"alpha-postgres-data": {}, "alpha-storage-data": {}, "alpha-logs": {}}
	for _, resource := range manifest.Spec.Resources {
		delete(expectedResources, resource.Name)
	}
	if len(expectedResources) != 0 {
		t.Fatalf("retained resources missing expected PVCs: %#v", expectedResources)
	}
	if len(manifest.Spec.Instructions) == 0 || !strings.Contains(manifest.Spec.Instructions[0], "retain_volumes=true") {
		t.Fatalf("expected retain_volumes instruction, got %#v", manifest.Spec.Instructions)
	}
}

func TestDestroyApplyHandsOffCleanupToOperator(t *testing.T) {
	root := t.TempDir()
	renderer := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	if err := renderer.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
		Services: map[string]control.ServiceSpec{
			"storage": {Enabled: true},
		},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	logPath := filepath.Join(root, "kubectl.log")
	appliedProjectPath := filepath.Join(root, "applied-project.yaml")
	kubectlPath := filepath.Join(root, "kubectl")
	script := fmt.Sprintf(`#!/usr/bin/env sh
printf '%%s\n' "$*" >>%q
if [ "$1" = "apply" ] && [ "$2" = "-f" ] && [ "$(basename "$3")" = "project.yaml" ]; then
  cp "$3" %q
fi
if [ "$1" = "get" ]; then
  printf 'Terminating'
fi
`, logPath, appliedProjectPath)
	if err := os.WriteFile(kubectlPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform", Apply: true, SkipCRDApply: true, Command: kubectlPath})
	if err := provisioner.DestroyWithOptions(context.Background(), "alpha", control.DestroyOptions{RetainVolumes: true}); err != nil {
		t.Fatalf("destroy failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("expected project directory removed, got err=%v", err)
	}
	appliedPayload, err := os.ReadFile(appliedProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	applied := decodeRenderedProjectManifest(t, appliedPayload)
	if applied.Spec.DesiredState != "destroying" {
		t.Fatalf("expected destroying handoff Project, got %#v", applied.Spec.DesiredState)
	}
	volumeCount := 0
	for serviceName, service := range applied.Spec.Services {
		for _, volume := range service.Volumes {
			volumeCount++
			if !volume.Retain {
				t.Fatalf("expected retain_volumes to mark service %s volume %#v retained", serviceName, volume)
			}
		}
	}
	if volumeCount == 0 {
		t.Fatalf("expected generated project to include retained volumes")
	}

	logPayload, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logged := string(logPayload)
	for _, expected := range []string{
		"apply -f " + filepath.Join(root, "_retained"),
		"apply -f " + filepath.Join(root, "alpha", "project.yaml"),
		"get project alpha -n platform -o jsonpath={.status.phase}",
		"delete -f " + filepath.Join(root, "alpha", "project.yaml") + " --ignore-not-found=true",
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("expected kubectl call containing %q in:\n%s", expected, logged)
		}
	}
	if strings.Index(logged, "apply -f "+filepath.Join(root, "alpha", "project.yaml")) > strings.Index(logged, "get project alpha -n platform") {
		t.Fatalf("expected Project apply before status wait:\n%s", logged)
	}
}

func TestSyncConfigRendersProjectConfigCRD(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	err = provisioner.SyncConfig(context.Background(), "alpha", control.ProjectConfig{
		ProjectRef: "alpha",
		Area:       "auth_providers",
		Config: map[string]string{
			"oauth_google_enabled":              "true",
			"oauth_google_client_id":            "google-client",
			"oauth_google_client_secret_handle": "secret://projects/alpha/google-oauth",
			"saml_enabled":                      "true",
		},
	})
	if err != nil {
		t.Fatalf("sync config failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "alpha", "configs", "auth_providers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeYAMLManifest[projectConfigManifest](t, payload)
	if manifest.Kind != "ProjectConfig" || manifest.Metadata.Name != "alpha-auth_providers" || manifest.Metadata.Namespace != "platform" {
		t.Fatalf("unexpected config manifest metadata: %#v", manifest)
	}
	if manifest.Metadata.Labels["supadupa.dev/project-ref"] != "alpha" || manifest.Metadata.Labels["supadupa.dev/config-area"] != "auth_providers" {
		t.Fatalf("unexpected config labels: %#v", manifest.Metadata.Labels)
	}
	if manifest.Spec.ProjectRef != "alpha" || manifest.Spec.Area != "auth_providers" {
		t.Fatalf("unexpected config spec: %#v", manifest.Spec)
	}
	for key, expected := range map[string]string{
		"oauth_google_enabled":              "true",
		"oauth_google_client_id":            "google-client",
		"oauth_google_client_secret_handle": "secret://projects/alpha/google-oauth",
		"saml_enabled":                      "true",
	} {
		if manifest.Spec.Config[key] != expected {
			t.Fatalf("expected config %s=%q, got %#v", key, expected, manifest.Spec.Config)
		}
	}
}

func TestSyncServicesPreservesPausedDesiredState(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		OrgID:        "org-one",
		Name:         "Alpha",
		Domain:       "example.test",
		StackVersion: "15.8.1.060",
		Profile:      control.StackProfileFull,
		ResourceTier: control.ResourceTierMedium,
		HostID:       "host-one",
		Environment:  map[string]string{"CUSTOM": "value"},
		Services: map[string]control.ServiceSpec{
			"storage": {Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := provisioner.Pause(context.Background(), "alpha"); err != nil {
		t.Fatalf("pause failed: %v", err)
	}
	if err := provisioner.SyncServices(context.Background(), "alpha", control.ProjectSpec{
		Services: map[string]control.ServiceSpec{
			"storage": {Enabled: false},
		},
	}); err != nil {
		t.Fatalf("sync services failed: %v", err)
	}
	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectPaused {
		t.Fatalf("expected service sync to preserve paused state, got %#v", status)
	}
	payload, err := os.ReadFile(filepath.Join(root, "alpha", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeRenderedProjectManifest(t, payload)
	if manifest.Spec.DesiredState != "paused" {
		t.Fatalf("expected paused desired state, got %q\n%s", manifest.Spec.DesiredState, payload)
	}
	if manifest.Spec.OrgID != "org-one" || manifest.Spec.DisplayName != "Alpha" || manifest.Spec.HostID != "host-one" {
		t.Fatalf("service sync should preserve identity fields: %#v", manifest.Spec)
	}
	if manifest.Spec.Domain != "example.test" || manifest.Spec.StackVersion != "15.8.1.060" || manifest.Spec.Profile != "full" || manifest.Spec.ResourceTier != "medium" {
		t.Fatalf("service sync should preserve runtime fields: %#v", manifest.Spec)
	}
	if manifest.Spec.Environment["CUSTOM"] != "value" {
		t.Fatalf("service sync should preserve environment: %#v", manifest.Spec.Environment)
	}
	if manifest.Spec.Services["storage"].Enabled {
		t.Fatalf("expected storage to be disabled after service sync: %#v", manifest.Spec.Services["storage"])
	}
	if manifest.Spec.Services["db"].Image != "supabase/postgres:15.8.1.060" {
		t.Fatalf("service sync should preserve existing stack version for default service rendering: %#v", manifest.Spec.Services["db"])
	}
	if manifest.Spec.Services["kong"].Ingress == nil || manifest.Spec.Services["kong"].Ingress.Host != "alpha.example.test" {
		t.Fatalf("service sync should preserve existing domain for default ingress rendering: %#v", manifest.Spec.Services["kong"].Ingress)
	}
}

func TestSyncAuthHooksRendersDesiredStateCRD(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	hooks := []control.ProjectAuthHook{
		{
			ID:            "hook-b",
			ProjectRef:    "alpha",
			HookType:      "send_email",
			Enabled:       true,
			EdgeFunction:  "mail-hook",
			Headers:       map[string]string{"x-trace": "email"},
			TimeoutMS:     5000,
			RetryAttempts: 1,
			Status:        "configured",
		},
		{
			ID:            "hook-a",
			ProjectRef:    "alpha",
			HookType:      "custom_access_token",
			Enabled:       true,
			TargetURI:     "https://hooks.example.com/token",
			SecretHandle:  "secret://projects/alpha/auth/hook-secret",
			Headers:       map[string]string{"authorization": "secret://projects/alpha/auth/hook-header", "x-trace": "token"},
			TimeoutMS:     7000,
			RetryAttempts: 2,
			Status:        "configured",
		},
	}
	if err := provisioner.SyncAuthHooks(context.Background(), "alpha", hooks); err != nil {
		t.Fatalf("sync auth hooks failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "alpha", "auth-hooks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeYAMLManifest[projectAuthHooksManifest](t, payload)
	if manifest.Kind != "ProjectAuthHooks" || manifest.Metadata.Name != "alpha-auth-hooks" || manifest.Metadata.Namespace != "platform" {
		t.Fatalf("unexpected auth hooks manifest metadata: %#v", manifest)
	}
	if manifest.Metadata.Labels["supadupa.dev/project-ref"] != "alpha" || manifest.Spec.ProjectRef != "alpha" {
		t.Fatalf("unexpected auth hooks project ref: %#v", manifest)
	}
	if len(manifest.Spec.Hooks) != 2 {
		t.Fatalf("expected two auth hooks, got %#v", manifest.Spec.Hooks)
	}
	tokenHook := manifest.Spec.Hooks[0]
	if tokenHook.Type != "custom_access_token" ||
		tokenHook.TargetURI != "https://hooks.example.com/token" ||
		tokenHook.SecretHandle != "secret://projects/alpha/auth/hook-secret" ||
		tokenHook.Headers["authorization"] != "secret://projects/alpha/auth/hook-header" ||
		tokenHook.RetryAttempts != 2 {
		t.Fatalf("unexpected custom access token hook: %#v", tokenHook)
	}
	emailHook := manifest.Spec.Hooks[1]
	if emailHook.Type != "send_email" || emailHook.EdgeFunction != "mail-hook" || emailHook.Headers["x-trace"] != "email" {
		t.Fatalf("unexpected email hook: %#v", emailHook)
	}

	if err := provisioner.SyncAuthHooks(context.Background(), "alpha", nil); err != nil {
		t.Fatalf("clear auth hooks failed: %v", err)
	}
	cleared, err := os.ReadFile(filepath.Join(root, "alpha", "auth-hooks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	clearedManifest := decodeYAMLManifest[projectAuthHooksManifest](t, cleared)
	if len(clearedManifest.Spec.Hooks) != 0 {
		t.Fatalf("expected cleared auth hook desired state, got %#v", clearedManifest.Spec.Hooks)
	}
}

func TestLifecycleMutatesProjectCRD(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root})
	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		StackVersion: "old",
		ResourceTier: control.ResourceTierSmall,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := provisioner.Upgrade(context.Background(), "alpha", "new"); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}
	if err := provisioner.Scale(context.Background(), "alpha", control.ProjectSpec{
		Ref:           "alpha",
		Domain:        "supadupa.test",
		StackVersion:  "new",
		ResourceTier:  control.ResourceTierCustom,
		CPU:           6,
		RAMMB:         12288,
		DiskGB:        120,
		EnforceLimits: true,
	}); err != nil {
		t.Fatalf("scale failed: %v", err)
	}
	if err := provisioner.Pause(context.Background(), "alpha"); err != nil {
		t.Fatalf("pause failed: %v", err)
	}
	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectPaused || status.Message != "kubernetes project paused" {
		t.Fatalf("expected paused status, got %#v", status)
	}
	if err := provisioner.Resume(context.Background(), "alpha"); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	status, err = provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectHealthy || status.Message != "kubernetes project rendered" {
		t.Fatalf("expected healthy status after resume, got %#v", status)
	}
	if err := provisioner.Pause(context.Background(), "alpha"); err != nil {
		t.Fatalf("pause failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "alpha", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeRenderedProjectManifest(t, payload)
	if manifest.Spec.StackVersion != "new" || manifest.Spec.ResourceTier != "custom" || manifest.Spec.DesiredState != "paused" || manifest.Spec.CPU != 6 || manifest.Spec.RAMMB != 12288 || manifest.Spec.DiskGB != 120 || !manifest.Spec.EnforceLimits {
		t.Fatalf("unexpected project lifecycle state: %#v\n%s", manifest.Spec, payload)
	}
	for _, service := range []string{"db", "kong", "meta", "auth", "functions", "analytics", "vector"} {
		assertKubernetesServiceHasResources(t, manifest.Spec.Services, service)
	}
	if manifest.Spec.Services["graphql"].Resources != nil {
		t.Fatalf("expected graphql extension to avoid standalone container resources, got %#v", manifest.Spec.Services["graphql"].Resources)
	}
	if manifest.Spec.Services["db"].Resources.Limits["cpu"] == "6" || manifest.Spec.Services["db"].Resources.Limits["memory"] == "12288Mi" {
		t.Fatalf("expected db to receive a per-service share rather than the full project budget, got %#v", manifest.Spec.Services["db"].Resources)
	}
}

func TestLifecycleMutatesProjectCRDWithStructuredYAML(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "alpha")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.yaml"), []byte(`apiVersion: platform.supadupa.dev/v1alpha1
kind: Project
metadata:
  name: alpha
spec:
    ref: alpha
    desiredState: running
    stackVersion: old
    resourceTier: small
`), 0o600); err != nil {
		t.Fatal(err)
	}

	provisioner := NewWithOptions(Options{RootDir: root})
	if err := provisioner.Upgrade(context.Background(), "alpha", "new"); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}
	if err := provisioner.Scale(context.Background(), "alpha", control.ProjectSpec{
		Ref:           "alpha",
		StackVersion:  "new",
		ResourceTier:  control.ResourceTierCustom,
		CPU:           6,
		RAMMB:         12288,
		DiskGB:        120,
		EnforceLimits: true,
	}); err != nil {
		t.Fatalf("scale failed: %v", err)
	}
	if err := provisioner.Pause(context.Background(), "alpha"); err != nil {
		t.Fatalf("pause failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(projectDir, "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"desiredState": "paused",
		"stackVersion": "new",
		"resourceTier": "custom",
	} {
		actual, err := projectSpecScalar(payload, key)
		if err != nil {
			t.Fatalf("read %s failed: %v\n%s", key, err, payload)
		}
		if actual != expected {
			t.Fatalf("expected spec.%s=%q, got %q\n%s", key, expected, actual, payload)
		}
		if strings.Count(string(payload), key+":") != 1 {
			t.Fatalf("expected one spec.%s field after mutation:\n%s", key, payload)
		}
	}
}

func TestProjectStatusReportsNonScalarDesiredStateAsDrift(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "alpha")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.yaml"), []byte(`apiVersion: platform.supadupa.dev/v1alpha1
kind: Project
metadata:
  name: alpha
spec:
  desiredState:
    - running
`), 0o600); err != nil {
		t.Fatal(err)
	}

	provisioner := NewWithOptions(Options{RootDir: root})
	status, err := provisioner.Status(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if status.Phase != control.ProjectDegraded || !strings.Contains(status.Message, "spec.desiredState must be a scalar") {
		t.Fatalf("expected degraded non-scalar desired state, got %#v", status)
	}
	if err := provisioner.Pause(context.Background(), "alpha"); err == nil || !strings.Contains(err.Error(), "spec.desiredState must be a scalar") {
		t.Fatalf("expected pause to reject non-scalar desired state, got %v", err)
	}
}

func TestCloneBranchRendersBranchCloneCRD(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	for _, ref := range []string{"alpha", "alpha-preview"} {
		if err := provisioner.Create(context.Background(), control.ProjectSpec{
			Ref:    ref,
			Domain: "supadupa.test",
		}); err != nil {
			t.Fatalf("create %s failed: %v", ref, err)
		}
	}
	expires := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	result, err := provisioner.CloneBranch(context.Background(), control.BranchCloneOptions{
		SourceRef: "alpha",
		BranchRef: "alpha-preview",
		BranchID:  "branch-one",
		Name:      "Preview",
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("clone branch failed: %v", err)
	}
	if result.State != "rendered" || !strings.HasSuffix(result.Path, filepath.Join("alpha-preview", "branch-clone.yaml")) {
		t.Fatalf("expected rendered branch clone result, got %#v", result)
	}
	payload, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeYAMLManifest[projectBranchCloneManifest](t, payload)
	if manifest.Kind != "ProjectBranchClone" || manifest.Metadata.Name != "alpha-preview-clone" || manifest.Metadata.Namespace != "platform" {
		t.Fatalf("unexpected branch clone manifest metadata: %#v", manifest)
	}
	if manifest.Metadata.Labels["supadupa.dev/source-ref"] != "alpha" || manifest.Metadata.Labels["supadupa.dev/branch-ref"] != "alpha-preview" {
		t.Fatalf("unexpected branch clone labels: %#v", manifest.Metadata.Labels)
	}
	if manifest.Spec.SourceRef != "alpha" ||
		manifest.Spec.BranchRef != "alpha-preview" ||
		manifest.Spec.BranchID != "branch-one" ||
		manifest.Spec.DisplayName != "Preview" ||
		manifest.Spec.ExpiresAt != "2026-06-05T12:00:00Z" {
		t.Fatalf("unexpected branch clone spec: %#v", manifest.Spec)
	}
}

func TestAddReplicaRendersReplicaCRD(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:          "alpha",
		Domain:       "supadupa.test",
		ResourceTier: control.ResourceTierSmall,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	err = provisioner.AddReplica(context.Background(), "alpha", control.ReplicaOpts{
		ID:     "replica-one",
		Name:   "east",
		Region: "us-east",
		Tier:   control.ResourceTierMedium,
	})
	if err != nil {
		t.Fatalf("add replica failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "alpha", "replicas", "replica-one.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeYAMLManifest[projectReplicaManifest](t, payload)
	if manifest.Kind != "ProjectReplica" || manifest.Metadata.Name != "alpha-east" || manifest.Metadata.Namespace != "platform" {
		t.Fatalf("unexpected replica manifest metadata: %#v", manifest)
	}
	if manifest.Spec.ProjectRef != "alpha" ||
		manifest.Spec.Name != "east" ||
		manifest.Spec.Region != "us-east" ||
		manifest.Spec.ResourceTier != "medium" {
		t.Fatalf("unexpected replica spec: %#v", manifest.Spec)
	}
	if manifest.Spec.RuntimeSecurityDefaults.SeccompProfile != "RuntimeDefault" ||
		manifest.Spec.RuntimeSecurityDefaults.AllowPrivilegeEscalation ||
		len(manifest.Spec.RuntimeSecurityDefaults.DropCapabilities) != 1 ||
		manifest.Spec.RuntimeSecurityDefaults.DropCapabilities[0] != "ALL" {
		t.Fatalf("unexpected replica runtime security defaults: %#v", manifest.Spec.RuntimeSecurityDefaults)
	}
}
