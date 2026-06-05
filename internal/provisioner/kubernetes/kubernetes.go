package kubernetes

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

type Options struct {
	RootDir   string
	Apply     bool
	Command   string
	Namespace string
}

type Provisioner struct {
	rootDir   string
	apply     bool
	command   string
	namespace string
}

type crdDefinition struct {
	Kind     string
	Plural   string
	Singular string
}

var crdDefinitions = []crdDefinition{
	{Kind: "Project", Plural: "projects", Singular: "project"},
	{Kind: "ProjectConfig", Plural: "projectconfigs", Singular: "projectconfig"},
	{Kind: "ProjectAuthHooks", Plural: "projectauthhooks", Singular: "projectauthhooks"},
	{Kind: "ProjectBranchClone", Plural: "projectbranchclones", Singular: "projectbranchclone"},
	{Kind: "ProjectReplica", Plural: "projectreplicas", Singular: "projectreplica"},
	{Kind: "RetainedProjectResources", Plural: "retainedprojectresources", Singular: "retainedprojectresources"},
}

func New() *Provisioner {
	return NewWithOptions(Options{
		RootDir:   envOrDefault("SUPADUPA_K8S_ROOT", "./runtime/kubernetes"),
		Apply:     os.Getenv("SUPADUPA_K8S_APPLY") == "true",
		Command:   envOrDefault("SUPADUPA_K8S_COMMAND", "kubectl"),
		Namespace: envOrDefault("SUPADUPA_K8S_NAMESPACE", "supadupa"),
	})
}

func NewWithOptions(opts Options) *Provisioner {
	if opts.RootDir == "" {
		opts.RootDir = "./runtime/kubernetes"
	}
	if opts.Command == "" {
		opts.Command = "kubectl"
	}
	if opts.Namespace == "" {
		opts.Namespace = "supadupa"
	}
	return &Provisioner{
		rootDir:   opts.RootDir,
		apply:     opts.Apply,
		command:   opts.Command,
		namespace: opts.Namespace,
	}
}

func (p *Provisioner) Name() string {
	return "kubernetes"
}

func (p *Provisioner) Create(ctx context.Context, spec control.ProjectSpec) error {
	if err := p.ensureCRDManifests(ctx); err != nil {
		return err
	}
	projectDir := filepath.Join(p.rootDir, spec.Ref)
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(projectDir, "project.yaml")
	if err := os.WriteFile(path, []byte(renderProjectCRD(spec, p.namespace, "running")), 0o600); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runKubectl(ctx, "apply", "-f", path)
}

func (p *Provisioner) ensureCRDManifests(ctx context.Context) error {
	crdDir := filepath.Join(p.rootDir, "_crds")
	if err := os.MkdirAll(crdDir, 0o700); err != nil {
		return err
	}
	for _, definition := range crdDefinitions {
		path := filepath.Join(crdDir, definition.Plural+".yaml")
		if err := os.WriteFile(path, []byte(renderCustomResourceDefinition(definition)), 0o600); err != nil {
			return err
		}
		if p.apply {
			if err := p.runKubectl(ctx, "apply", "-f", path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Provisioner) SyncSecrets(ctx context.Context, ref string, spec control.ProjectSpec) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "project.yaml")); err != nil {
		return err
	}
	path := filepath.Join(projectDir, "project-secrets.yaml")
	if err := os.WriteFile(path, []byte(renderProjectSecretManifest(ref, p.namespace, spec.Environment)), 0o600); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runKubectl(ctx, "apply", "-f", path)
}

func (p *Provisioner) SyncConfig(ctx context.Context, ref string, config control.ProjectConfig) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "project.yaml")); err != nil {
		return err
	}
	area := strings.TrimSpace(config.Area)
	if area == "" {
		return fmt.Errorf("config area is required")
	}
	configDir := filepath.Join(projectDir, "configs")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(configDir, area+".yaml")
	if err := os.WriteFile(path, []byte(renderProjectConfigCRD(ref, p.namespace, config)), 0o600); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runKubectl(ctx, "apply", "-f", path)
}

func (p *Provisioner) SyncAuthHooks(ctx context.Context, ref string, hooks []control.ProjectAuthHook) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "project.yaml")); err != nil {
		return err
	}
	path := filepath.Join(projectDir, "auth-hooks.yaml")
	if err := os.WriteFile(path, []byte(renderProjectAuthHooksCRD(ref, p.namespace, hooks)), 0o600); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runKubectl(ctx, "apply", "-f", path)
}

func (p *Provisioner) SyncServices(ctx context.Context, ref string, spec control.ProjectSpec) error {
	projectDir := filepath.Join(p.rootDir, ref)
	path := filepath.Join(projectDir, "project.yaml")
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	desiredState := renderedDesiredState(string(payload))
	if desiredState == "" {
		desiredState = "running"
	}
	spec.Ref = ref
	if err := os.WriteFile(path, []byte(renderProjectCRD(spec, p.namespace, desiredState)), 0o600); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runKubectl(ctx, "apply", "-f", path)
}

func (p *Provisioner) Destroy(ctx context.Context, ref string) error {
	return p.DestroyWithOptions(ctx, ref, control.DestroyOptions{})
}

func (p *Provisioner) DestroyWithOptions(ctx context.Context, ref string, opts control.DestroyOptions) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if p.apply {
		paths, err := renderedManifestPaths(projectDir)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if err := p.runKubectl(ctx, "delete", "-f", path, "--ignore-not-found=true"); err != nil {
				return err
			}
		}
	}
	if opts.RetainVolumes {
		if err := p.writeRetainedResourceManifest(ref); err != nil {
			return err
		}
	}
	return os.RemoveAll(projectDir)
}

func (p *Provisioner) Status(ctx context.Context, ref string) (control.ProjectStatus, error) {
	path := filepath.Join(p.rootDir, ref, "project.yaml")
	payload, err := os.ReadFile(path)
	if err != nil {
		return control.ProjectStatus{Ref: ref, Phase: control.ProjectError, Message: err.Error()}, err
	}
	phase := control.ProjectHealthy
	message := "kubernetes project rendered"
	switch renderedDesiredState(string(payload)) {
	case "paused":
		phase = control.ProjectPaused
		message = "kubernetes project paused"
	case "running", "":
	default:
		phase = control.ProjectDegraded
		message = "kubernetes render drift: unsupported desired state"
	}
	return control.ProjectStatus{
		Ref:     ref,
		Phase:   phase,
		Message: message,
		Endpoints: map[string]string{
			"ingress": fmt.Sprintf("https://%s", ref),
		},
	}, nil
}

func (p *Provisioner) Upgrade(ctx context.Context, ref string, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("version is required")
	}
	return p.updateProjectManifest(ctx, ref, func(payload string) string {
		return replaceYAMLScalar(payload, "stackVersion", version)
	})
}

func (p *Provisioner) Pause(ctx context.Context, ref string) error {
	return p.updateProjectManifest(ctx, ref, func(payload string) string {
		return replaceYAMLScalar(payload, "desiredState", "paused")
	})
}

func (p *Provisioner) Resume(ctx context.Context, ref string) error {
	return p.updateProjectManifest(ctx, ref, func(payload string) string {
		return replaceYAMLScalar(payload, "desiredState", "running")
	})
}

func (p *Provisioner) Scale(ctx context.Context, ref string, tier control.ResourceTier) error {
	if tier != control.ResourceTierSmall && tier != control.ResourceTierMedium && tier != control.ResourceTierLarge {
		return fmt.Errorf("unsupported resource tier %q", tier)
	}
	return p.updateProjectManifest(ctx, ref, func(payload string) string {
		return replaceYAMLScalar(payload, "resourceTier", string(tier))
	})
}

func (p *Provisioner) AddReplica(ctx context.Context, ref string, opts control.ReplicaOpts) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "project.yaml")); err != nil {
		return err
	}
	replicaID := strings.TrimSpace(opts.ID)
	if replicaID == "" {
		replicaID = "replica"
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = replicaID
	}
	replicaDir := filepath.Join(projectDir, "replicas")
	if err := os.MkdirAll(replicaDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(replicaDir, replicaID+".yaml")
	if err := os.WriteFile(path, []byte(renderReplicaCRD(ref, replicaID, name, p.namespace, opts)), 0o600); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runKubectl(ctx, "apply", "-f", path)
}

func (p *Provisioner) CloneBranch(ctx context.Context, opts control.BranchCloneOptions) (control.BranchCloneResult, error) {
	sourceRef := strings.TrimSpace(opts.SourceRef)
	branchRef := strings.TrimSpace(opts.BranchRef)
	if sourceRef == "" {
		return control.BranchCloneResult{}, fmt.Errorf("source ref is required")
	}
	if branchRef == "" {
		return control.BranchCloneResult{}, fmt.Errorf("branch ref is required")
	}
	sourceDir := filepath.Join(p.rootDir, sourceRef)
	branchDir := filepath.Join(p.rootDir, branchRef)
	if _, err := os.Stat(filepath.Join(sourceDir, "project.yaml")); err != nil {
		return control.BranchCloneResult{}, fmt.Errorf("source project is not rendered: %w", err)
	}
	if _, err := os.Stat(filepath.Join(branchDir, "project.yaml")); err != nil {
		return control.BranchCloneResult{}, fmt.Errorf("branch project is not rendered: %w", err)
	}
	path := filepath.Join(branchDir, "branch-clone.yaml")
	if err := os.WriteFile(path, []byte(renderBranchCloneCRD(p.namespace, opts)), 0o600); err != nil {
		return control.BranchCloneResult{}, err
	}
	if p.apply {
		if err := p.runKubectl(ctx, "apply", "-f", path); err != nil {
			return control.BranchCloneResult{}, err
		}
	}
	return control.BranchCloneResult{Path: path, State: "rendered"}, nil
}

func (p *Provisioner) updateProjectManifest(ctx context.Context, ref string, mutate func(string) string) error {
	path := filepath.Join(p.rootDir, ref, "project.yaml")
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next := mutate(string(payload))
	if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runKubectl(ctx, "apply", "-f", path)
}

func (p *Provisioner) runKubectl(ctx context.Context, args ...string) error {
	parts := strings.Fields(p.command)
	if len(parts) == 0 {
		return fmt.Errorf("kubectl command is empty")
	}
	cmd := exec.CommandContext(ctx, parts[0], append(parts[1:], args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func renderedManifestPaths(projectDir string) ([]string, error) {
	paths := []string{}
	err := filepath.WalkDir(projectDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if os.IsNotExist(err) {
		return paths, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(paths, func(i, j int) bool {
		leftProject := filepath.Base(paths[i]) == "project.yaml"
		rightProject := filepath.Base(paths[j]) == "project.yaml"
		if leftProject != rightProject {
			return !leftProject
		}
		return paths[i] > paths[j]
	})
	return paths, nil
}

func (p *Provisioner) writeRetainedResourceManifest(ref string) error {
	dir := filepath.Join(p.rootDir, "_retained")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	now := time.Now().UTC()
	path := filepath.Join(dir, ref+"-"+now.Format("20060102T150405Z")+".yaml")
	var builder strings.Builder
	builder.WriteString("apiVersion: platform.supadupa.dev/v1alpha1\n")
	builder.WriteString("kind: RetainedProjectResources\n")
	builder.WriteString("metadata:\n")
	builder.WriteString(fmt.Sprintf("  name: %s\n", yamlString(ref+"-retained")))
	builder.WriteString(fmt.Sprintf("  namespace: %s\n", yamlString(p.namespace)))
	builder.WriteString("  labels:\n")
	builder.WriteString("    app.kubernetes.io/managed-by: supadupa\n")
	builder.WriteString(fmt.Sprintf("    supadupa.dev/project-ref: %s\n", yamlString(ref)))
	builder.WriteString("spec:\n")
	builder.WriteString(fmt.Sprintf("  projectRef: %s\n", yamlString(ref)))
	builder.WriteString(fmt.Sprintf("  retainedAt: %s\n", yamlString(now.Format("2006-01-02T15:04:05Z"))))
	builder.WriteString("  resources:\n")
	for _, resource := range []string{ref + "-postgres-data", ref + "-storage-data", ref + "-logs"} {
		builder.WriteString(fmt.Sprintf("    - kind: PersistentVolumeClaim\n      name: %s\n", yamlString(resource)))
	}
	builder.WriteString("  instructions:\n")
	builder.WriteString("    - \"These Kubernetes resources were intentionally retained by DELETE /v1/projects/{ref}?retain_volumes=true.\"\n")
	builder.WriteString("    - \"Remove them manually after confirming the data is no longer needed.\"\n")
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func renderCustomResourceDefinition(definition crdDefinition) string {
	var builder strings.Builder
	builder.WriteString("apiVersion: apiextensions.k8s.io/v1\n")
	builder.WriteString("kind: CustomResourceDefinition\n")
	builder.WriteString("metadata:\n")
	builder.WriteString(fmt.Sprintf("  name: %s\n", yamlString(definition.Plural+".platform.supadupa.dev")))
	builder.WriteString("  labels:\n")
	builder.WriteString("    app.kubernetes.io/managed-by: supadupa\n")
	builder.WriteString("spec:\n")
	builder.WriteString("  group: platform.supadupa.dev\n")
	builder.WriteString("  scope: Namespaced\n")
	builder.WriteString("  names:\n")
	builder.WriteString(fmt.Sprintf("    plural: %s\n", yamlString(definition.Plural)))
	builder.WriteString(fmt.Sprintf("    singular: %s\n", yamlString(definition.Singular)))
	builder.WriteString(fmt.Sprintf("    kind: %s\n", yamlString(definition.Kind)))
	builder.WriteString("  versions:\n")
	builder.WriteString("    - name: v1alpha1\n")
	builder.WriteString("      served: true\n")
	builder.WriteString("      storage: true\n")
	builder.WriteString("      schema:\n")
	builder.WriteString("        openAPIV3Schema:\n")
	builder.WriteString("          type: object\n")
	builder.WriteString("          x-kubernetes-preserve-unknown-fields: true\n")
	return builder.String()
}

func renderProjectCRD(spec control.ProjectSpec, namespace string, desiredState string) string {
	stackVersion := defaultString(spec.StackVersion, "latest")
	profile := defaultString(string(spec.Profile), string(control.StackProfileFull))
	tier := defaultString(string(spec.ResourceTier), string(control.ResourceTierSmall))
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(spec.Domain)), ".")
	if domain == "" {
		domain = "supadupa.test"
	}

	var builder strings.Builder
	builder.WriteString("apiVersion: platform.supadupa.dev/v1alpha1\n")
	builder.WriteString("kind: Project\n")
	builder.WriteString("metadata:\n")
	builder.WriteString(fmt.Sprintf("  name: %s\n", yamlString(spec.Ref)))
	builder.WriteString(fmt.Sprintf("  namespace: %s\n", yamlString(namespace)))
	builder.WriteString("  labels:\n")
	builder.WriteString("    app.kubernetes.io/managed-by: supadupa\n")
	builder.WriteString("spec:\n")
	builder.WriteString(fmt.Sprintf("  ref: %s\n", yamlString(spec.Ref)))
	builder.WriteString(fmt.Sprintf("  orgId: %s\n", yamlString(spec.OrgID)))
	builder.WriteString(fmt.Sprintf("  displayName: %s\n", yamlString(spec.Name)))
	builder.WriteString(fmt.Sprintf("  desiredState: %s\n", yamlString(desiredState)))
	builder.WriteString(fmt.Sprintf("  domain: %s\n", yamlString(domain)))
	builder.WriteString(fmt.Sprintf("  stackVersion: %s\n", yamlString(stackVersion)))
	builder.WriteString(fmt.Sprintf("  profile: %s\n", yamlString(profile)))
	builder.WriteString(fmt.Sprintf("  resourceTier: %s\n", yamlString(tier)))
	if spec.HostID != "" {
		builder.WriteString(fmt.Sprintf("  hostId: %s\n", yamlString(spec.HostID)))
	}
	renderStringMap(&builder, "environment", spec.Environment)
	renderServices(&builder, spec.Services)
	return builder.String()
}

func renderBranchCloneCRD(namespace string, opts control.BranchCloneOptions) string {
	sourceRef := strings.TrimSpace(opts.SourceRef)
	branchRef := strings.TrimSpace(opts.BranchRef)
	branchID := strings.TrimSpace(opts.BranchID)
	name := strings.TrimSpace(opts.Name)
	var builder strings.Builder
	builder.WriteString("apiVersion: platform.supadupa.dev/v1alpha1\n")
	builder.WriteString("kind: ProjectBranchClone\n")
	builder.WriteString("metadata:\n")
	builder.WriteString(fmt.Sprintf("  name: %s\n", yamlString(branchRef+"-clone")))
	builder.WriteString(fmt.Sprintf("  namespace: %s\n", yamlString(namespace)))
	builder.WriteString("  labels:\n")
	builder.WriteString("    app.kubernetes.io/managed-by: supadupa\n")
	builder.WriteString(fmt.Sprintf("    supadupa.dev/source-ref: %s\n", yamlString(sourceRef)))
	builder.WriteString(fmt.Sprintf("    supadupa.dev/branch-ref: %s\n", yamlString(branchRef)))
	builder.WriteString("spec:\n")
	builder.WriteString(fmt.Sprintf("  sourceRef: %s\n", yamlString(sourceRef)))
	builder.WriteString(fmt.Sprintf("  branchRef: %s\n", yamlString(branchRef)))
	if branchID != "" {
		builder.WriteString(fmt.Sprintf("  branchId: %s\n", yamlString(branchID)))
	}
	if name != "" {
		builder.WriteString(fmt.Sprintf("  displayName: %s\n", yamlString(name)))
	}
	if opts.ExpiresAt != nil {
		builder.WriteString(fmt.Sprintf("  expiresAt: %s\n", yamlString(opts.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"))))
	}
	return builder.String()
}

func renderProjectConfigCRD(ref string, namespace string, config control.ProjectConfig) string {
	area := strings.TrimSpace(config.Area)
	var builder strings.Builder
	builder.WriteString("apiVersion: platform.supadupa.dev/v1alpha1\n")
	builder.WriteString("kind: ProjectConfig\n")
	builder.WriteString("metadata:\n")
	builder.WriteString(fmt.Sprintf("  name: %s\n", yamlString(ref+"-"+area)))
	builder.WriteString(fmt.Sprintf("  namespace: %s\n", yamlString(namespace)))
	builder.WriteString("  labels:\n")
	builder.WriteString("    app.kubernetes.io/managed-by: supadupa\n")
	builder.WriteString(fmt.Sprintf("    supadupa.dev/project-ref: %s\n", yamlString(ref)))
	builder.WriteString(fmt.Sprintf("    supadupa.dev/config-area: %s\n", yamlString(area)))
	builder.WriteString("spec:\n")
	builder.WriteString(fmt.Sprintf("  projectRef: %s\n", yamlString(ref)))
	builder.WriteString(fmt.Sprintf("  area: %s\n", yamlString(area)))
	renderStringMap(&builder, "config", config.Config)
	return builder.String()
}

func renderProjectAuthHooksCRD(ref string, namespace string, hooks []control.ProjectAuthHook) string {
	out := append([]control.ProjectAuthHook(nil), hooks...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].HookType == out[j].HookType {
			return out[i].ID < out[j].ID
		}
		return out[i].HookType < out[j].HookType
	})

	var builder strings.Builder
	builder.WriteString("apiVersion: platform.supadupa.dev/v1alpha1\n")
	builder.WriteString("kind: ProjectAuthHooks\n")
	builder.WriteString("metadata:\n")
	builder.WriteString(fmt.Sprintf("  name: %s\n", yamlString(ref+"-auth-hooks")))
	builder.WriteString(fmt.Sprintf("  namespace: %s\n", yamlString(namespace)))
	builder.WriteString("  labels:\n")
	builder.WriteString("    app.kubernetes.io/managed-by: supadupa\n")
	builder.WriteString(fmt.Sprintf("    supadupa.dev/project-ref: %s\n", yamlString(ref)))
	builder.WriteString("spec:\n")
	builder.WriteString(fmt.Sprintf("  projectRef: %s\n", yamlString(ref)))
	builder.WriteString("  hooks:\n")
	if len(out) == 0 {
		builder.WriteString("    []\n")
		return builder.String()
	}
	for _, hook := range out {
		builder.WriteString(fmt.Sprintf("    - type: %s\n", yamlString(hook.HookType)))
		builder.WriteString(fmt.Sprintf("      enabled: %t\n", hook.Enabled))
		builder.WriteString(fmt.Sprintf("      status: %s\n", yamlString(hook.Status)))
		if hook.TargetURI != "" {
			builder.WriteString(fmt.Sprintf("      targetURI: %s\n", yamlString(hook.TargetURI)))
		}
		if hook.EdgeFunction != "" {
			builder.WriteString(fmt.Sprintf("      edgeFunction: %s\n", yamlString(hook.EdgeFunction)))
		}
		if hook.SecretHandle != "" {
			builder.WriteString(fmt.Sprintf("      secretHandle: %s\n", yamlString(hook.SecretHandle)))
		}
		builder.WriteString(fmt.Sprintf("      timeoutMS: %d\n", hook.TimeoutMS))
		builder.WriteString(fmt.Sprintf("      retryAttempts: %d\n", hook.RetryAttempts))
		renderAuthHookHeaders(&builder, hook.Headers)
	}
	return builder.String()
}

func renderProjectSecretManifest(ref string, namespace string, environment map[string]string) string {
	values := map[string]string{}
	for _, key := range control.ManagedSecretEnvironmentKeys() {
		if value := strings.TrimSpace(environment[key]); value != "" {
			values[key] = value
		}
	}
	var builder strings.Builder
	builder.WriteString("apiVersion: v1\n")
	builder.WriteString("kind: Secret\n")
	builder.WriteString("metadata:\n")
	builder.WriteString(fmt.Sprintf("  name: %s\n", yamlString(ref+"-secrets")))
	builder.WriteString(fmt.Sprintf("  namespace: %s\n", yamlString(namespace)))
	builder.WriteString("  labels:\n")
	builder.WriteString("    app.kubernetes.io/managed-by: supadupa\n")
	builder.WriteString(fmt.Sprintf("    supadupa.dev/project-ref: %s\n", yamlString(ref)))
	builder.WriteString("type: Opaque\n")
	builder.WriteString("stringData:\n")
	if len(values) == 0 {
		builder.WriteString("  {}\n")
		return builder.String()
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(fmt.Sprintf("  %s: %s\n", yamlKey(key), yamlString(values[key])))
	}
	return builder.String()
}

func renderedDesiredState(payload string) string {
	for _, line := range strings.Split(payload, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || key != "desiredState" {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return ""
}

func renderReplicaCRD(ref string, replicaID string, name string, namespace string, opts control.ReplicaOpts) string {
	tier := defaultString(string(opts.Tier), string(control.ResourceTierSmall))
	var builder strings.Builder
	builder.WriteString("apiVersion: platform.supadupa.dev/v1alpha1\n")
	builder.WriteString("kind: ProjectReplica\n")
	builder.WriteString("metadata:\n")
	builder.WriteString(fmt.Sprintf("  name: %s\n", yamlString(ref+"-"+name)))
	builder.WriteString(fmt.Sprintf("  namespace: %s\n", yamlString(namespace)))
	builder.WriteString("  labels:\n")
	builder.WriteString("    app.kubernetes.io/managed-by: supadupa\n")
	builder.WriteString(fmt.Sprintf("    supadupa.dev/project-ref: %s\n", yamlString(ref)))
	builder.WriteString("spec:\n")
	builder.WriteString(fmt.Sprintf("  id: %s\n", yamlString(replicaID)))
	builder.WriteString(fmt.Sprintf("  projectRef: %s\n", yamlString(ref)))
	builder.WriteString(fmt.Sprintf("  name: %s\n", yamlString(name)))
	builder.WriteString(fmt.Sprintf("  resourceTier: %s\n", yamlString(tier)))
	if opts.Region != "" {
		builder.WriteString(fmt.Sprintf("  region: %s\n", yamlString(opts.Region)))
	}
	if opts.HostID != "" {
		builder.WriteString(fmt.Sprintf("  hostId: %s\n", yamlString(opts.HostID)))
	}
	if opts.ReadWeight > 0 {
		builder.WriteString(fmt.Sprintf("  readWeight: %d\n", opts.ReadWeight))
	}
	if opts.FailoverPriority > 0 {
		builder.WriteString(fmt.Sprintf("  failoverPriority: %d\n", opts.FailoverPriority))
	}
	return builder.String()
}

func renderStringMap(builder *strings.Builder, name string, values map[string]string) {
	builder.WriteString(fmt.Sprintf("  %s:\n", name))
	if len(values) == 0 {
		builder.WriteString("    {}\n")
		return
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(fmt.Sprintf("    %s: %s\n", yamlKey(key), yamlString(values[key])))
	}
}

func renderServices(builder *strings.Builder, services map[string]control.ServiceSpec) {
	builder.WriteString("  services:\n")
	if len(services) == 0 {
		builder.WriteString("    {}\n")
		return
	}
	keys := make([]string, 0, len(services))
	for key := range services {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(fmt.Sprintf("    %s:\n", yamlKey(key)))
		builder.WriteString(fmt.Sprintf("      enabled: %t\n", services[key].Enabled))
		renderNestedStringMap(builder, "config", services[key].Config)
	}
}

func renderNestedStringMap(builder *strings.Builder, name string, values map[string]string) {
	builder.WriteString(fmt.Sprintf("      %s:\n", name))
	if len(values) == 0 {
		builder.WriteString("        {}\n")
		return
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(fmt.Sprintf("        %s: %s\n", yamlKey(key), yamlString(values[key])))
	}
}

func renderAuthHookHeaders(builder *strings.Builder, values map[string]string) {
	builder.WriteString("      headers:\n")
	if len(values) == 0 {
		builder.WriteString("        {}\n")
		return
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(fmt.Sprintf("        %s: %s\n", yamlKey(key), yamlString(values[key])))
	}
}

func replaceYAMLScalar(payload string, key string, value string) string {
	lines := strings.Split(payload, "\n")
	prefix := "  " + key + ": "
	for index, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[index] = prefix + yamlString(value)
			return strings.Join(lines, "\n")
		}
	}
	for index, line := range lines {
		if line == "spec:" {
			next := append(lines[:index+1], append([]string{prefix + yamlString(value)}, lines[index+1:]...)...)
			return strings.Join(next, "\n")
		}
	}
	return payload
}

func yamlKey(value string) string {
	if value == "" {
		return `""`
	}
	return value
}

func yamlString(value string) string {
	return fmt.Sprintf("%q", value)
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
