package kubernetes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"supadupa2026/internal/control"
)

func TestProvisionerImplementsContract(t *testing.T) {
	var _ control.Provisioner = New()
	var _ control.OptionedDestroyer = New()
	var _ control.ConfigSyncer = New()
	var _ control.ServiceSyncer = New()
	var _ control.AuthHookSyncer = New()
	var _ control.BranchCloner = New()
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
	for _, expected := range []string{
		"kind: Project",
		`namespace: "platform"`,
		`ref: "alpha"`,
		`stackVersion: "15.8.1.060"`,
		`resourceTier: "small"`,
		`CUSTOM: "value"`,
		"storage:",
		"enabled: true",
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("expected %s in project CRD:\n%s", expected, payload)
		}
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
		for _, expectedText := range []string{
			"kind: CustomResourceDefinition",
			`group: platform.supadupa.dev`,
			`kind: "` + kind + `"`,
			`x-kubernetes-preserve-unknown-fields: true`,
		} {
			if !strings.Contains(string(payload), expectedText) {
				t.Fatalf("expected %s in %s:\n%s", expectedText, filename, payload)
			}
		}
	}
}

func TestSyncSecretsRendersProjectSecretManifest(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
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

	payload, err := os.ReadFile(filepath.Join(root, "alpha", "project-secrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"kind: Secret",
		`name: "alpha-secrets"`,
		`namespace: "platform"`,
		`supadupa.dev/project-ref: "alpha"`,
		`JWT_SECRET: "jwt-rotated"`,
		`POSTGRES_PASSWORD: "db-rotated"`,
		`SERVICE_ROLE_KEY: "service-rotated"`,
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("expected %s in secret manifest:\n%s", expected, payload)
		}
	}
	if strings.Contains(string(payload), "CUSTOM") {
		t.Fatalf("secret manifest should only contain managed secret keys:\n%s", payload)
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
	for _, expected := range []string{
		"kind: RetainedProjectResources",
		`namespace: "platform"`,
		`projectRef: "alpha"`,
		`alpha-postgres-data`,
		`alpha-storage-data`,
		`alpha-logs`,
		`retain_volumes=true`,
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("expected retained manifest to contain %q, got:\n%s", expected, payload)
		}
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
	for _, expected := range []string{
		"kind: ProjectConfig",
		`name: "alpha-auth_providers"`,
		`namespace: "platform"`,
		`supadupa.dev/project-ref: "alpha"`,
		`supadupa.dev/config-area: "auth_providers"`,
		`projectRef: "alpha"`,
		`area: "auth_providers"`,
		`oauth_google_enabled: "true"`,
		`oauth_google_client_id: "google-client"`,
		`oauth_google_client_secret_handle: "secret://projects/alpha/google-oauth"`,
		`saml_enabled: "true"`,
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("expected %s in config manifest:\n%s", expected, payload)
		}
	}
}

func TestSyncServicesPreservesPausedDesiredState(t *testing.T) {
	root := t.TempDir()
	provisioner := NewWithOptions(Options{RootDir: root, Namespace: "platform"})
	err := provisioner.Create(context.Background(), control.ProjectSpec{
		Ref:    "alpha",
		Domain: "supadupa.test",
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
		Domain: "supadupa.test",
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
	body := string(payload)
	for _, expected := range []string{
		`desiredState: "paused"`,
		"storage:",
		"enabled: false",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %s in service-synced manifest:\n%s", expected, payload)
		}
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
	body := string(payload)
	for _, expected := range []string{
		"kind: ProjectAuthHooks",
		`name: "alpha-auth-hooks"`,
		`namespace: "platform"`,
		`supadupa.dev/project-ref: "alpha"`,
		`projectRef: "alpha"`,
		`- type: "custom_access_token"`,
		`targetURI: "https://hooks.example.com/token"`,
		`secretHandle: "secret://projects/alpha/auth/hook-secret"`,
		`authorization: "secret://projects/alpha/auth/hook-header"`,
		`retryAttempts: 2`,
		`- type: "send_email"`,
		`edgeFunction: "mail-hook"`,
		`x-trace: "email"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %s in auth hooks manifest:\n%s", expected, payload)
		}
	}
	if strings.Index(body, `- type: "custom_access_token"`) > strings.Index(body, `- type: "send_email"`) {
		t.Fatalf("expected hooks sorted by type:\n%s", payload)
	}

	if err := provisioner.SyncAuthHooks(context.Background(), "alpha", nil); err != nil {
		t.Fatalf("clear auth hooks failed: %v", err)
	}
	cleared, err := os.ReadFile(filepath.Join(root, "alpha", "auth-hooks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cleared), "  hooks:\n    []") {
		t.Fatalf("expected cleared auth hook desired state, got:\n%s", cleared)
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
	if err := provisioner.Scale(context.Background(), "alpha", control.ResourceTierLarge); err != nil {
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
	for _, expected := range []string{
		`stackVersion: "new"`,
		`resourceTier: "large"`,
		`desiredState: "paused"`,
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("expected %s in project CRD:\n%s", expected, payload)
		}
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
	for _, expected := range []string{
		"kind: ProjectBranchClone",
		`name: "alpha-preview-clone"`,
		`namespace: "platform"`,
		`supadupa.dev/source-ref: "alpha"`,
		`supadupa.dev/branch-ref: "alpha-preview"`,
		`sourceRef: "alpha"`,
		`branchRef: "alpha-preview"`,
		`branchId: "branch-one"`,
		`displayName: "Preview"`,
		`expiresAt: "2026-06-05T12:00:00Z"`,
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("expected %s in branch clone manifest:\n%s", expected, payload)
		}
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
	for _, expected := range []string{
		"kind: ProjectReplica",
		`projectRef: "alpha"`,
		`name: "east"`,
		`region: "us-east"`,
		`resourceTier: "medium"`,
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("expected %s in replica CRD:\n%s", expected, payload)
		}
	}
}
