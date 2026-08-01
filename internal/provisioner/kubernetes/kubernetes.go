package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"supadupa2026/internal/env"
	"time"

	"supadupa2026/internal/control"
	"supadupa2026/internal/provisioner/artifact"
	"supadupa2026/internal/provisioner/dbbootstrap"

	"gopkg.in/yaml.v3"
)

type Options struct {
	RootDir                string
	Apply                  bool
	SkipCRDApply           bool
	Command                string
	Namespace              string
	Isolation              bool
	RuntimeNamespacePrefix string
}

type Provisioner struct {
	rootDir                string
	apply                  bool
	skipCRDApply           bool
	command                string
	namespace              string
	isolation              bool
	runtimeNamespacePrefix string
}

type crdDefinition struct {
	Kind     string
	Plural   string
	Singular string
}

type customResourceDefinitionManifest struct {
	APIVersion string                       `yaml:"apiVersion"`
	Kind       string                       `yaml:"kind"`
	Metadata   customResourceDefinitionMeta `yaml:"metadata"`
	Spec       customResourceDefinitionSpec `yaml:"spec"`
}

type customResourceDefinitionMeta struct {
	Name   string            `yaml:"name"`
	Labels map[string]string `yaml:"labels"`
}

type customResourceDefinitionSpec struct {
	Group    string                            `yaml:"group"`
	Scope    string                            `yaml:"scope"`
	Names    customResourceDefinitionNames     `yaml:"names"`
	Versions []customResourceDefinitionVersion `yaml:"versions"`
}

type customResourceDefinitionNames struct {
	Plural   string `yaml:"plural"`
	Singular string `yaml:"singular"`
	Kind     string `yaml:"kind"`
}

type customResourceDefinitionVersion struct {
	Name         string                    `yaml:"name"`
	Served       bool                      `yaml:"served"`
	Storage      bool                      `yaml:"storage"`
	Subresources map[string]map[string]any `yaml:"subresources"`
	Schema       map[string]any            `yaml:"schema"`
}

type projectManifest struct {
	APIVersion string              `yaml:"apiVersion"`
	Kind       string              `yaml:"kind"`
	Metadata   projectManifestMeta `yaml:"metadata"`
	Spec       projectManifestSpec `yaml:"spec"`
}

type projectManifestMeta struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

type projectManifestSpec struct {
	Ref                     string                               `yaml:"ref"`
	OrgID                   string                               `yaml:"orgId"`
	DisplayName             string                               `yaml:"displayName"`
	DesiredState            string                               `yaml:"desiredState"`
	Domain                  string                               `yaml:"domain"`
	StackVersion            string                               `yaml:"stackVersion"`
	Profile                 string                               `yaml:"profile"`
	ResourceTier            string                               `yaml:"resourceTier"`
	CPU                     int                                  `yaml:"cpu,omitempty"`
	RAMMB                   int                                  `yaml:"ramMB,omitempty"`
	DiskGB                  int                                  `yaml:"diskGB,omitempty"`
	EnforceLimits           bool                                 `yaml:"enforceLimits,omitempty"`
	HostID                  string                               `yaml:"hostId,omitempty"`
	RuntimeNamespace        string                               `yaml:"runtimeNamespace,omitempty"`
	RuntimeSecurityDefaults kubernetesRuntimeSecurityDefaults    `yaml:"runtimeSecurityDefaults"`
	Environment             map[string]string                    `yaml:"environment"`
	Services                map[string]kubernetesRenderedService `yaml:"services"`
}

type kubernetesRuntimeSecurityDefaults struct {
	SeccompProfile           string   `yaml:"seccompProfile"`
	AllowPrivilegeEscalation bool     `yaml:"allowPrivilegeEscalation"`
	DropCapabilities         []string `yaml:"dropCapabilities"`
}

type projectConfigManifest struct {
	APIVersion string              `yaml:"apiVersion"`
	Kind       string              `yaml:"kind"`
	Metadata   projectManifestMeta `yaml:"metadata"`
	Spec       projectConfigSpec   `yaml:"spec"`
}

type projectConfigSpec struct {
	ProjectRef string            `yaml:"projectRef"`
	Area       string            `yaml:"area"`
	Config     map[string]string `yaml:"config"`
}

type projectAuthHooksManifest struct {
	APIVersion string               `yaml:"apiVersion"`
	Kind       string               `yaml:"kind"`
	Metadata   projectManifestMeta  `yaml:"metadata"`
	Spec       projectAuthHooksSpec `yaml:"spec"`
}

type projectAuthHooksSpec struct {
	ProjectRef string                    `yaml:"projectRef"`
	Hooks      []projectAuthHookManifest `yaml:"hooks"`
}

type projectAuthHookManifest struct {
	Type          string            `yaml:"type"`
	Enabled       bool              `yaml:"enabled"`
	Status        string            `yaml:"status"`
	TargetURI     string            `yaml:"targetURI,omitempty"`
	EdgeFunction  string            `yaml:"edgeFunction,omitempty"`
	SecretHandle  string            `yaml:"secretHandle,omitempty"`
	TimeoutMS     int               `yaml:"timeoutMS"`
	RetryAttempts int               `yaml:"retryAttempts"`
	Headers       map[string]string `yaml:"headers"`
}

type projectBranchCloneManifest struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Metadata   projectManifestMeta    `yaml:"metadata"`
	Spec       projectBranchCloneSpec `yaml:"spec"`
}

type projectBranchCloneSpec struct {
	SourceRef   string `yaml:"sourceRef"`
	BranchRef   string `yaml:"branchRef"`
	BranchID    string `yaml:"branchId,omitempty"`
	DisplayName string `yaml:"displayName,omitempty"`
	ExpiresAt   string `yaml:"expiresAt,omitempty"`
}

type projectReplicaManifest struct {
	APIVersion string              `yaml:"apiVersion"`
	Kind       string              `yaml:"kind"`
	Metadata   projectManifestMeta `yaml:"metadata"`
	Spec       projectReplicaSpec  `yaml:"spec"`
}

type projectReplicaSpec struct {
	ID                      string                            `yaml:"id"`
	ProjectRef              string                            `yaml:"projectRef"`
	Name                    string                            `yaml:"name"`
	ResourceTier            string                            `yaml:"resourceTier"`
	Region                  string                            `yaml:"region,omitempty"`
	HostID                  string                            `yaml:"hostId,omitempty"`
	ReadWeight              int                               `yaml:"readWeight,omitempty"`
	FailoverPriority        int                               `yaml:"failoverPriority,omitempty"`
	RuntimeSecurityDefaults kubernetesRuntimeSecurityDefaults `yaml:"runtimeSecurityDefaults"`
}

type retainedProjectResourcesManifest struct {
	APIVersion string                       `yaml:"apiVersion"`
	Kind       string                       `yaml:"kind"`
	Metadata   projectManifestMeta          `yaml:"metadata"`
	Spec       retainedProjectResourcesSpec `yaml:"spec"`
}

type retainedProjectResourcesSpec struct {
	ProjectRef   string                       `yaml:"projectRef"`
	RetainedAt   string                       `yaml:"retainedAt"`
	Resources    []retainedProjectResourceRef `yaml:"resources"`
	Instructions []string                     `yaml:"instructions"`
}

type retainedProjectResourceRef struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
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
		RootDir:                env.OrDefault("SUPADUPA_K8S_ROOT", "./runtime/kubernetes"),
		Apply:                  os.Getenv("SUPADUPA_K8S_APPLY") == "true",
		SkipCRDApply:           strings.EqualFold(strings.TrimSpace(os.Getenv("SUPADUPA_K8S_MANAGE_CRDS")), "false"),
		Command:                env.OrDefault("SUPADUPA_K8S_COMMAND", "kubectl"),
		Namespace:              env.OrDefault("SUPADUPA_K8S_NAMESPACE", "supadupa"),
		Isolation:              !strings.EqualFold(strings.TrimSpace(os.Getenv("SUPADUPA_K8S_ISOLATION")), "false"),
		RuntimeNamespacePrefix: env.OrDefault("SUPADUPA_K8S_RUNTIME_NAMESPACE_PREFIX", "supadupa-proj-"),
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
	if opts.RuntimeNamespacePrefix == "" {
		opts.RuntimeNamespacePrefix = "supadupa-proj-"
	}
	return &Provisioner{
		rootDir:                opts.RootDir,
		apply:                  opts.Apply,
		skipCRDApply:           opts.SkipCRDApply,
		command:                opts.Command,
		namespace:              opts.Namespace,
		isolation:              opts.Isolation,
		runtimeNamespacePrefix: opts.RuntimeNamespacePrefix,
	}
}

func (p *Provisioner) Name() string {
	return "kubernetes"
}

// runtimeNamespaceForRef returns the per-project runtime namespace stamped onto
// the Project CR spec so the operator and observability tooling agree on where
// runtime resources live. When isolation is disabled it returns "" and the
// operator falls back to the control namespace (legacy single-namespace mode).
func (p *Provisioner) runtimeNamespaceForRef(ref string) string {
	if !p.isolation {
		return ""
	}
	return p.runtimeNamespacePrefix + kubernetesDNSLabel(ref)
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
	manifestYAML, err := renderProjectCRD(spec, p.namespace, "running", p.runtimeNamespaceForRef(spec.Ref))
	if err != nil {
		return err
	}
	if err := artifact.WriteFile(path, []byte(manifestYAML), 0o600); err != nil {
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
		if err := artifact.WriteFile(path, []byte(renderCustomResourceDefinition(definition)), 0o600); err != nil {
			return err
		}
		if p.apply && !p.skipCRDApply {
			if err := p.runKubectl(ctx, "apply", "-f", path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Provisioner) SyncSecrets(ctx context.Context, ref string, spec control.ProjectSpec) error {
	projectDir := filepath.Join(p.rootDir, ref)
	path := filepath.Join(projectDir, "project.yaml")
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	environment := spec.Environment
	if environment == nil {
		environment = map[string]string{}
	}
	next, err := replaceProjectSpecValue(payload, "environment", environment)
	if err != nil {
		return err
	}
	if err := artifact.WriteFile(path, next, 0o600); err != nil {
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
	if err := artifact.WriteFile(path, []byte(renderProjectConfigCRD(ref, p.namespace, config)), 0o600); err != nil {
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
	if err := artifact.WriteFile(path, []byte(renderProjectAuthHooksCRD(ref, p.namespace, hooks)), 0o600); err != nil {
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
	desiredState, err := renderedDesiredState(payload)
	if err != nil {
		return err
	}
	spec.Ref = ref
	if spec.Domain == "" {
		spec.Domain, err = projectSpecScalar(payload, "domain")
		if err != nil {
			return err
		}
	}
	if spec.StackVersion == "" {
		spec.StackVersion, err = projectSpecScalar(payload, "stackVersion")
		if err != nil {
			return err
		}
	}
	services, err := kubernetesRenderedServiceMap(spec)
	if err != nil {
		return err
	}
	next, err := replaceProjectSpecValue(payload, "services", services)
	if err != nil {
		return err
	}
	if desiredState == "" {
		next, err = replaceProjectSpecScalar(next, "desiredState", "running")
		if err != nil {
			return err
		}
	}
	if err := artifact.WriteFile(path, next, 0o600); err != nil {
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
	projectPath := filepath.Join(projectDir, "project.yaml")
	retainedPath := ""
	if opts.RetainVolumes {
		path, err := p.writeRetainedResourceManifest(ref)
		if err != nil {
			return err
		}
		retainedPath = path
	}
	if p.apply {
		payload, err := os.ReadFile(projectPath)
		if err != nil {
			return err
		}
		if opts.RetainVolumes {
			payload, err = markProjectVolumesRetained(payload)
			if err != nil {
				return err
			}
		}
		payload, err = replaceProjectSpecScalar(payload, "desiredState", "destroying")
		if err != nil {
			return err
		}
		if err := artifact.WriteFile(projectPath, payload, 0o600); err != nil {
			return err
		}
		if retainedPath != "" {
			if err := p.runKubectl(ctx, "apply", "-f", retainedPath); err != nil {
				return err
			}
		}
		if err := p.runKubectl(ctx, "apply", "-f", projectPath); err != nil {
			return err
		}
		if err := p.waitForProjectPhase(ctx, ref, "Terminating", 120*time.Second); err != nil {
			return err
		}
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
	desiredState, err := renderedDesiredState(payload)
	if err != nil {
		phase = control.ProjectDegraded
		message = "kubernetes render drift: " + err.Error()
	}
	switch desiredState {
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
	return p.updateProjectManifest(ctx, ref, func(payload []byte) ([]byte, error) {
		return replaceProjectSpecScalar(payload, "stackVersion", version)
	})
}

func (p *Provisioner) Pause(ctx context.Context, ref string) error {
	return p.updateProjectManifest(ctx, ref, func(payload []byte) ([]byte, error) {
		return replaceProjectSpecScalar(payload, "desiredState", "paused")
	})
}

func (p *Provisioner) Resume(ctx context.Context, ref string) error {
	return p.updateProjectManifest(ctx, ref, func(payload []byte) ([]byte, error) {
		return replaceProjectSpecScalar(payload, "desiredState", "running")
	})
}

func (p *Provisioner) Scale(ctx context.Context, ref string, spec control.ProjectSpec) error {
	spec.Ref = ref
	spec.ResourceTier = control.ResourceTierCustom
	return p.updateProjectManifest(ctx, ref, func(payload []byte) ([]byte, error) {
		next, err := replaceProjectSpecScalar(payload, "resourceTier", string(control.ResourceTierCustom))
		if err != nil {
			return nil, err
		}
		if next, err = replaceProjectSpecValue(next, "cpu", spec.CPU); err != nil {
			return nil, err
		}
		if next, err = replaceProjectSpecValue(next, "ramMB", spec.RAMMB); err != nil {
			return nil, err
		}
		if next, err = replaceProjectSpecValue(next, "diskGB", spec.DiskGB); err != nil {
			return nil, err
		}
		if next, err = replaceProjectSpecValue(next, "enforceLimits", spec.EnforceLimits); err != nil {
			return nil, err
		}
		services, err := kubernetesRenderedServiceMap(spec)
		if err != nil {
			return nil, err
		}
		return replaceProjectSpecValue(next, "services", services)
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
	if err := artifact.WriteFile(path, []byte(renderReplicaCRD(ref, replicaID, name, p.namespace, opts)), 0o600); err != nil {
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
	if err := artifact.WriteFile(path, []byte(renderBranchCloneCRD(p.namespace, opts)), 0o600); err != nil {
		return control.BranchCloneResult{}, err
	}
	if p.apply {
		if err := p.runKubectl(ctx, "apply", "-f", path); err != nil {
			return control.BranchCloneResult{}, err
		}
	}
	return control.BranchCloneResult{Path: path, State: "rendered"}, nil
}

func (p *Provisioner) updateProjectManifest(ctx context.Context, ref string, mutate func([]byte) ([]byte, error)) error {
	path := filepath.Join(p.rootDir, ref, "project.yaml")
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, err := mutate(payload)
	if err != nil {
		return err
	}
	if err := artifact.WriteFile(path, next, 0o600); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runKubectl(ctx, "apply", "-f", path)
}

func (p *Provisioner) runKubectl(ctx context.Context, args ...string) error {
	_, err := p.runKubectlOutput(ctx, args...)
	return err
}

func (p *Provisioner) runKubectlOutput(ctx context.Context, args ...string) ([]byte, error) {
	parts := strings.Fields(p.command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("kubectl command is empty")
	}
	cmd := exec.CommandContext(ctx, parts[0], append(parts[1:], args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (p *Provisioner) waitForProjectPhase(ctx context.Context, ref string, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		output, err := p.runKubectlOutput(ctx, "get", "project", ref, "-n", p.namespace, "-o", "jsonpath={.status.phase}")
		if err == nil && strings.TrimSpace(string(output)) == expected {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("timed out waiting for Kubernetes Project %s phase %s: %w", ref, expected, err)
			}
			return fmt.Errorf("timed out waiting for Kubernetes Project %s phase %s, last phase %q", ref, expected, strings.TrimSpace(string(output)))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
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

func (p *Provisioner) writeRetainedResourceManifest(ref string) (string, error) {
	dir := filepath.Join(p.rootDir, "_retained")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	path := filepath.Join(dir, ref+"-"+now.Format("20060102T150405Z")+".yaml")
	manifest := retainedProjectResourcesManifest{
		APIVersion: "platform.supadupa.dev/v1alpha1",
		Kind:       "RetainedProjectResources",
		Metadata: projectManifestMeta{
			Name:      ref + "-retained",
			Namespace: p.namespace,
			Labels:    managedLabels("supadupa.dev/project-ref", ref),
		},
		Spec: retainedProjectResourcesSpec{
			ProjectRef: ref,
			RetainedAt: now.Format("2006-01-02T15:04:05Z"),
			Resources: []retainedProjectResourceRef{
				{Kind: "PersistentVolumeClaim", Name: ref + "-postgres-data"},
				{Kind: "PersistentVolumeClaim", Name: ref + "-storage-data"},
				{Kind: "PersistentVolumeClaim", Name: ref + "-logs"},
			},
			Instructions: []string{
				"These Kubernetes resources were intentionally retained by DELETE /v1/projects/{ref}?retain_volumes=true.",
				"Remove them manually after confirming the data is no longer needed.",
			},
		},
	}
	return path, artifact.WriteFile(path, []byte(encodeYAMLValue(manifest)), 0o600)
}

func markProjectVolumesRetained(payload []byte) ([]byte, error) {
	var manifest projectManifest
	if err := yaml.Unmarshal(payload, &manifest); err != nil {
		return nil, fmt.Errorf("decode project manifest for retain_volumes: %w", err)
	}
	for name, service := range manifest.Spec.Services {
		for index := range service.Volumes {
			service.Volumes[index].Retain = true
		}
		manifest.Spec.Services[name] = service
	}
	return []byte(encodeYAMLValue(manifest)), nil
}

func renderCustomResourceDefinition(definition crdDefinition) string {
	openAPIV3Schema := openAPIV3SchemaForDefinition(definition)
	return encodeYAMLValue(customResourceDefinitionManifest{
		APIVersion: "apiextensions.k8s.io/v1",
		Kind:       "CustomResourceDefinition",
		Metadata: customResourceDefinitionMeta{
			Name:   definition.Plural + ".platform.supadupa.dev",
			Labels: managedLabels(),
		},
		Spec: customResourceDefinitionSpec{
			Group: "platform.supadupa.dev",
			Scope: "Namespaced",
			Names: customResourceDefinitionNames{
				Plural:   definition.Plural,
				Singular: definition.Singular,
				Kind:     definition.Kind,
			},
			Versions: []customResourceDefinitionVersion{{
				Name:         "v1alpha1",
				Served:       true,
				Storage:      true,
				Subresources: map[string]map[string]any{"status": {}},
				Schema:       map[string]any{"openAPIV3Schema": openAPIV3Schema},
			}},
		},
	})
}

func openAPIV3SchemaForDefinition(definition crdDefinition) map[string]any {
	switch definition.Kind {
	case "Project":
		return projectOpenAPIV3Schema()
	case "ProjectConfig":
		return objectWithSpecSchema(map[string]any{
			"projectRef": stringSchema(1),
			"area":       stringSchema(1),
			"config":     stringMapSchema(),
		})
	case "ProjectAuthHooks":
		return objectWithSpecSchema(map[string]any{
			"projectRef": stringSchema(1),
			"hooks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type":          stringSchema(1),
						"enabled":       map[string]any{"type": "boolean"},
						"status":        map[string]any{"type": "string"},
						"targetURI":     map[string]any{"type": "string"},
						"edgeFunction":  map[string]any{"type": "string"},
						"secretHandle":  map[string]any{"type": "string"},
						"timeoutMS":     map[string]any{"type": "integer", "minimum": 0},
						"retryAttempts": map[string]any{"type": "integer", "minimum": 0},
						"headers":       stringMapSchema(),
					},
				},
			},
		})
	case "ProjectBranchClone":
		return objectWithSpecSchema(map[string]any{
			"sourceRef":   stringSchema(1),
			"branchRef":   stringSchema(1),
			"branchId":    map[string]any{"type": "string"},
			"displayName": map[string]any{"type": "string"},
			"expiresAt":   map[string]any{"type": "string"},
		})
	case "ProjectReplica":
		return projectReplicaOpenAPIV3Schema()
	case "RetainedProjectResources":
		return objectWithSpecSchema(map[string]any{
			"projectRef": stringSchema(1),
			"retainedAt": stringSchema(1),
			"resources": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind": stringSchema(1),
						"name": stringSchema(1),
					},
				},
			},
			"instructions": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		})
	default:
		return map[string]any{
			"type":                                 "object",
			"x-kubernetes-preserve-unknown-fields": true,
		}
	}
}

func projectOpenAPIV3Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spec": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ref":                     stringSchema(1),
					"orgId":                   map[string]any{"type": "string"},
					"displayName":             map[string]any{"type": "string"},
					"desiredState":            map[string]any{"type": "string", "enum": []string{"running", "paused", "destroying"}},
					"domain":                  map[string]any{"type": "string"},
					"stackVersion":            map[string]any{"type": "string"},
					"profile":                 map[string]any{"type": "string"},
					"resourceTier":            map[string]any{"type": "string"},
					"cpu":                     map[string]any{"type": "integer", "minimum": 0},
					"ramMB":                   map[string]any{"type": "integer", "minimum": 0},
					"diskGB":                  map[string]any{"type": "integer", "minimum": 0},
					"enforceLimits":           map[string]any{"type": "boolean"},
					"hostId":                  map[string]any{"type": "string"},
					"runtimeNamespace":        map[string]any{"type": "string"},
					"runtimeNetwork":          runtimeNetworkSchema(),
					"environment":             stringMapSchema(),
					"runtimeSecurityDefaults": runtimeSecurityDefaultsSchema(),
					"services": map[string]any{
						"type":                 "object",
						"additionalProperties": serviceSchema(),
					},
				},
			},
			"status": projectStatusSchema(),
		},
	}
}

func objectWithSpecSchema(specProperties map[string]any) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spec": map[string]any{
				"type":       "object",
				"properties": specProperties,
			},
			"status": map[string]any{
				"type":                                 "object",
				"x-kubernetes-preserve-unknown-fields": true,
			},
		},
	}
}

func runtimeNetworkSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"allowedEgressCidrs":  arraySchema(map[string]any{"type": "string"}),
			"externalEgressPorts": arraySchema(portNumberSchema()),
			"databaseService":     map[string]any{"type": "string"},
			"databasePort":        portNumberSchema(),
		},
	}
}

func runtimeSecurityDefaultsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"seccompProfile": map[string]any{"type": "string", "enum": []string{"RuntimeDefault", "Localhost", "Unconfined"}},
			"allowPrivilegeEscalation": map[string]any{
				"type": "boolean",
			},
			"dropCapabilities": map[string]any{
				"type":  "array",
				"items": stringSchema(1),
			},
		},
	}
}

func projectReplicaOpenAPIV3Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spec": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":                      stringSchema(1),
					"projectRef":              stringSchema(1),
					"name":                    stringSchema(1),
					"resourceTier":            map[string]any{"type": "string"},
					"region":                  map[string]any{"type": "string"},
					"hostId":                  map[string]any{"type": "string"},
					"readWeight":              map[string]any{"type": "integer", "minimum": 0},
					"failoverPriority":        map[string]any{"type": "integer", "minimum": 0},
					"runtimeSecurityDefaults": runtimeSecurityDefaultsSchema(),
				},
			},
			"status": map[string]any{
				"type":                                 "object",
				"x-kubernetes-preserve-unknown-fields": true,
			},
		},
	}
}

func serviceSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"enabled":     map[string]any{"type": "boolean"},
			"config":      stringMapSchema(),
			"image":       map[string]any{"type": "string"},
			"command":     arraySchema(map[string]any{"type": "string"}),
			"args":        arraySchema(map[string]any{"type": "string"}),
			"replicas":    map[string]any{"type": "integer", "minimum": 0},
			"dependsOn":   arraySchema(serviceDependencySchema()),
			"ports":       arraySchema(servicePortSchema()),
			"serviceType": map[string]any{"type": "string", "enum": []string{"ClusterIP", "NodePort", "LoadBalancer", "ExternalName"}},
			"env":         stringMapSchema(),
			"resources":   serviceResourceRequirementsSchema(),
			"volumes":     arraySchema(serviceVolumeSchema()),
			"configFiles": arraySchema(serviceConfigFileSchema()),
			"writablePaths": arraySchema(map[string]any{
				"type":     "object",
				"required": []string{"mountPath"},
				"properties": map[string]any{
					"name":      map[string]any{"type": "string"},
					"mountPath": stringSchema(1),
				},
			}),
			"runAsNonRoot":             map[string]any{"type": "boolean"},
			"allowPrivilegeEscalation": map[string]any{"type": "boolean"},
			"dropCapabilities":         arraySchema(map[string]any{"type": "string"}),
			"readOnlyRootFilesystem":   map[string]any{"type": "boolean"},
			"readinessProbe":           serviceProbeSchema(),
			"livenessProbe":            serviceProbeSchema(),
			"ingress":                  serviceIngressSchema(),
		},
	}
}

func serviceResourceRequirementsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"requests": stringMapSchema(),
			"limits":   stringMapSchema(),
		},
	}
}

func serviceDependencySchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"service", "port"},
		"properties": map[string]any{
			"service": stringSchema(1),
			"port":    portNumberSchema(),
		},
	}
}

func servicePortSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"port"},
		"properties": map[string]any{
			"name":       map[string]any{"type": "string"},
			"port":       portNumberSchema(),
			"targetPort": portNumberSchema(),
			"protocol":   map[string]any{"type": "string", "enum": []string{"TCP", "UDP", "SCTP"}},
		},
	}
}

func serviceVolumeSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"mountPath", "size"},
		"properties": map[string]any{
			"name":             map[string]any{"type": "string"},
			"mountPath":        stringSchema(1),
			"size":             stringSchema(1),
			"storageClassName": map[string]any{"type": "string"},
			"retain":           map[string]any{"type": "boolean"},
		},
	}
}

func serviceConfigFileSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"mountPath", "content"},
		"properties": map[string]any{
			"name":      map[string]any{"type": "string"},
			"mountPath": stringSchema(1),
			"content":   map[string]any{"type": "string"},
		},
	}
}

func serviceProbeSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":                map[string]any{"type": "string", "enum": []string{"http", "tcp"}},
			"path":                map[string]any{"type": "string"},
			"port":                portNumberSchema(),
			"initialDelaySeconds": map[string]any{"type": "integer", "minimum": 0},
			"periodSeconds":       map[string]any{"type": "integer", "minimum": 1},
			"timeoutSeconds":      map[string]any{"type": "integer", "minimum": 1},
			"failureThreshold":    map[string]any{"type": "integer", "minimum": 1},
		},
	}
}

func serviceIngressSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"enabled":       map[string]any{"type": "boolean"},
			"host":          stringSchema(1),
			"path":          map[string]any{"type": "string"},
			"className":     map[string]any{"type": "string"},
			"annotations":   stringMapSchema(),
			"tlsSecretName": map[string]any{"type": "string"},
		},
	}
}

func projectStatusSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"observedGeneration":      map[string]any{"type": "integer", "format": "int64"},
			"phase":                   map[string]any{"type": "string"},
			"message":                 map[string]any{"type": "string"},
			"runtimeSecurityDefaults": map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true},
			"conditions":              arraySchema(projectConditionSchema()),
			"lastReconciledAt":        map[string]any{"type": "string"},
		},
	}
}

func projectConditionSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":               map[string]any{"type": "string"},
			"status":             map[string]any{"type": "string"},
			"reason":             map[string]any{"type": "string"},
			"message":            map[string]any{"type": "string"},
			"observedGeneration": map[string]any{"type": "integer", "format": "int64"},
			"lastTransitionTime": map[string]any{"type": "string"},
		},
	}
}

func arraySchema(items map[string]any) map[string]any {
	return map[string]any{
		"type":  "array",
		"items": items,
	}
}

func portNumberSchema() map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "maximum": 65535}
}

func stringMapSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"additionalProperties": map[string]any{
			"type": "string",
		},
	}
}

func stringSchema(minLength int) map[string]any {
	schema := map[string]any{"type": "string"}
	if minLength > 0 {
		schema["minLength"] = minLength
	}
	return schema
}

func renderProjectCRD(spec control.ProjectSpec, namespace string, desiredState string, runtimeNamespace string) (string, error) {
	manifest, err := projectManifestForSpec(spec, namespace, desiredState, runtimeNamespace)
	if err != nil {
		return "", err
	}
	return encodeYAMLValue(manifest), nil
}

func projectManifestForSpec(spec control.ProjectSpec, namespace string, desiredState string, runtimeNamespace string) (projectManifest, error) {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(spec.Domain)), ".")
	if domain == "" {
		domain = "supadupa.test"
	}
	services, err := kubernetesRenderedServiceMap(spec)
	if err != nil {
		return projectManifest{}, err
	}
	return projectManifest{
		APIVersion: "platform.supadupa.dev/v1alpha1",
		Kind:       "Project",
		Metadata: projectManifestMeta{
			Name:      spec.Ref,
			Namespace: namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "supadupa"},
		},
		Spec: projectManifestSpec{
			Ref:              spec.Ref,
			OrgID:            spec.OrgID,
			DisplayName:      spec.Name,
			DesiredState:     desiredState,
			Domain:           domain,
			StackVersion:     defaultString(spec.StackVersion, "latest"),
			Profile:          defaultString(string(spec.Profile), string(control.StackProfileFull)),
			ResourceTier:     defaultString(string(spec.ResourceTier), string(control.ResourceTierCustom)),
			CPU:              spec.CPU,
			RAMMB:            spec.RAMMB,
			DiskGB:           spec.DiskGB,
			EnforceLimits:    spec.EnforceLimits,
			HostID:           strings.TrimSpace(spec.HostID),
			RuntimeNamespace: strings.TrimSpace(runtimeNamespace),
			RuntimeSecurityDefaults: kubernetesRuntimeSecurityDefaults{
				SeccompProfile:           "RuntimeDefault",
				AllowPrivilegeEscalation: false,
				DropCapabilities:         []string{"ALL"},
			},
			Environment: copyStringMap(spec.Environment),
			Services:    services,
		},
	}, nil
}

func renderBranchCloneCRD(namespace string, opts control.BranchCloneOptions) string {
	sourceRef := strings.TrimSpace(opts.SourceRef)
	branchRef := strings.TrimSpace(opts.BranchRef)
	branchID := strings.TrimSpace(opts.BranchID)
	name := strings.TrimSpace(opts.Name)
	manifest := projectBranchCloneManifest{
		APIVersion: "platform.supadupa.dev/v1alpha1",
		Kind:       "ProjectBranchClone",
		Metadata: projectManifestMeta{
			Name:      branchRef + "-clone",
			Namespace: namespace,
			Labels: managedLabels(
				"supadupa.dev/source-ref", sourceRef,
				"supadupa.dev/branch-ref", branchRef,
			),
		},
		Spec: projectBranchCloneSpec{
			SourceRef:   sourceRef,
			BranchRef:   branchRef,
			BranchID:    branchID,
			DisplayName: name,
		},
	}
	if opts.ExpiresAt != nil {
		manifest.Spec.ExpiresAt = opts.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return encodeYAMLValue(manifest)
}

func renderProjectConfigCRD(ref string, namespace string, config control.ProjectConfig) string {
	area := strings.TrimSpace(config.Area)
	return encodeYAMLValue(projectConfigManifest{
		APIVersion: "platform.supadupa.dev/v1alpha1",
		Kind:       "ProjectConfig",
		Metadata: projectManifestMeta{
			Name:      ref + "-" + area,
			Namespace: namespace,
			Labels: managedLabels(
				"supadupa.dev/project-ref", ref,
				"supadupa.dev/config-area", area,
			),
		},
		Spec: projectConfigSpec{
			ProjectRef: ref,
			Area:       area,
			Config:     copyStringMap(config.Config),
		},
	})
}

func renderProjectAuthHooksCRD(ref string, namespace string, hooks []control.ProjectAuthHook) string {
	out := append([]control.ProjectAuthHook(nil), hooks...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].HookType == out[j].HookType {
			return out[i].ID < out[j].ID
		}
		return out[i].HookType < out[j].HookType
	})

	manifest := projectAuthHooksManifest{
		APIVersion: "platform.supadupa.dev/v1alpha1",
		Kind:       "ProjectAuthHooks",
		Metadata: projectManifestMeta{
			Name:      ref + "-auth-hooks",
			Namespace: namespace,
			Labels:    managedLabels("supadupa.dev/project-ref", ref),
		},
		Spec: projectAuthHooksSpec{
			ProjectRef: ref,
			Hooks:      []projectAuthHookManifest{},
		},
	}
	for _, hook := range out {
		manifest.Spec.Hooks = append(manifest.Spec.Hooks, projectAuthHookManifest{
			Type:          hook.HookType,
			Enabled:       hook.Enabled,
			Status:        hook.Status,
			TargetURI:     hook.TargetURI,
			EdgeFunction:  hook.EdgeFunction,
			SecretHandle:  hook.SecretHandle,
			TimeoutMS:     hook.TimeoutMS,
			RetryAttempts: hook.RetryAttempts,
			Headers:       copyStringMap(hook.Headers),
		})
	}
	return encodeYAMLValue(manifest)
}

func renderedDesiredState(payload []byte) (string, error) {
	return projectSpecScalar(payload, "desiredState")
}

func renderReplicaCRD(ref string, replicaID string, name string, namespace string, opts control.ReplicaOpts) string {
	tier := defaultString(string(opts.Tier), string(control.ResourceTierSmall))
	return encodeYAMLValue(projectReplicaManifest{
		APIVersion: "platform.supadupa.dev/v1alpha1",
		Kind:       "ProjectReplica",
		Metadata: projectManifestMeta{
			Name:      ref + "-" + name,
			Namespace: namespace,
			Labels:    managedLabels("supadupa.dev/project-ref", ref),
		},
		Spec: projectReplicaSpec{
			ID:               replicaID,
			ProjectRef:       ref,
			Name:             name,
			ResourceTier:     tier,
			Region:           opts.Region,
			HostID:           opts.HostID,
			ReadWeight:       opts.ReadWeight,
			FailoverPriority: opts.FailoverPriority,
			RuntimeSecurityDefaults: kubernetesRuntimeSecurityDefaults{
				SeccompProfile:           "RuntimeDefault",
				AllowPrivilegeEscalation: false,
				DropCapabilities:         []string{"ALL"},
			},
		},
	})
}

func renderRuntimeSecurityDefaults(builder *strings.Builder) {
	builder.WriteString("  runtimeSecurityDefaults:\n")
	builder.WriteString("    seccompProfile: RuntimeDefault\n")
	builder.WriteString("    allowPrivilegeEscalation: false\n")
	builder.WriteString("    dropCapabilities:\n")
	builder.WriteString("      - ALL\n")
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

type kubernetesRenderedService struct {
	Name                     string                         `yaml:"-"`
	Enabled                  bool                           `yaml:"enabled"`
	Image                    string                         `yaml:"image,omitempty"`
	Command                  []string                       `yaml:"command,omitempty"`
	Args                     []string                       `yaml:"args,omitempty"`
	Replicas                 int                            `yaml:"replicas,omitempty"`
	DependsOn                []kubernetesRenderedDependency `yaml:"dependsOn,omitempty"`
	Ports                    []kubernetesRenderedPort       `yaml:"ports"`
	ServiceType              string                         `yaml:"serviceType,omitempty"`
	Config                   map[string]string              `yaml:"config"`
	Env                      map[string]string              `yaml:"env"`
	Resources                *kubernetesRenderedResources   `yaml:"resources,omitempty"`
	Volumes                  []kubernetesRenderedVolume     `yaml:"volumes"`
	ConfigFiles              []kubernetesRenderedConfigFile `yaml:"configFiles,omitempty"`
	WritablePaths            []kubernetesRenderedWritable   `yaml:"writablePaths,omitempty"`
	RunAsNonRoot             *bool                          `yaml:"runAsNonRoot,omitempty"`
	AllowPrivilegeEscalation *bool                          `yaml:"allowPrivilegeEscalation,omitempty"`
	DropCapabilities         []string                       `yaml:"dropCapabilities,omitempty"`
	ReadOnlyRootFilesystem   bool                           `yaml:"readOnlyRootFilesystem,omitempty"`
	ReadinessProbe           *kubernetesRenderedProbe       `yaml:"readinessProbe,omitempty"`
	LivenessProbe            *kubernetesRenderedProbe       `yaml:"livenessProbe,omitempty"`
	Ingress                  *kubernetesRenderedIngress     `yaml:"ingress"`
}

type kubernetesRenderedPort struct {
	Name       string `yaml:"name"`
	Port       int    `yaml:"port"`
	TargetPort int    `yaml:"targetPort"`
	Protocol   string `yaml:"protocol"`
}

type kubernetesRenderedDependency struct {
	Service string `yaml:"service"`
	Port    int    `yaml:"port"`
}

type kubernetesRenderedResources struct {
	Requests map[string]string `yaml:"requests,omitempty"`
	Limits   map[string]string `yaml:"limits,omitempty"`
}

type kubernetesRenderedVolume struct {
	Name             string `yaml:"name"`
	MountPath        string `yaml:"mountPath"`
	Size             string `yaml:"size"`
	StorageClassName string `yaml:"storageClassName,omitempty"`
	Retain           bool   `yaml:"retain,omitempty"`
}

type kubernetesRenderedConfigFile struct {
	Name      string `yaml:"name,omitempty"`
	MountPath string `yaml:"mountPath"`
	Content   string `yaml:"content"`
}

type kubernetesRenderedWritable struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
}

type kubernetesRenderedProbe struct {
	Type                string `yaml:"type,omitempty"`
	Path                string `yaml:"path,omitempty"`
	Port                int    `yaml:"port"`
	InitialDelaySeconds int    `yaml:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int    `yaml:"periodSeconds,omitempty"`
	TimeoutSeconds      int    `yaml:"timeoutSeconds,omitempty"`
	FailureThreshold    int    `yaml:"failureThreshold,omitempty"`
}

type kubernetesRenderedIngress struct {
	Enabled       bool   `yaml:"enabled"`
	Host          string `yaml:"host,omitempty"`
	Path          string `yaml:"path,omitempty"`
	ClassName     string `yaml:"className,omitempty"`
	TLSSecretName string `yaml:"tlsSecretName,omitempty"`
}

func renderServices(builder *strings.Builder, spec control.ProjectSpec) error {
	services, err := kubernetesRenderedServices(spec)
	if err != nil {
		return err
	}
	builder.WriteString("  services:\n")
	if len(services) == 0 {
		builder.WriteString("    {}\n")
		return nil
	}
	for _, service := range services {
		builder.WriteString(fmt.Sprintf("    %s:\n", yamlKey(service.Name)))
		builder.WriteString(fmt.Sprintf("      enabled: %t\n", service.Enabled))
		if service.Image != "" {
			builder.WriteString(fmt.Sprintf("      image: %s\n", yamlString(service.Image)))
		}
		renderServiceStringList(builder, "command", service.Command)
		renderServiceStringList(builder, "args", service.Args)
		if service.Replicas > 0 {
			builder.WriteString(fmt.Sprintf("      replicas: %d\n", service.Replicas))
		}
		if service.ServiceType != "" {
			builder.WriteString(fmt.Sprintf("      serviceType: %s\n", yamlString(service.ServiceType)))
		}
		renderNestedStringMap(builder, "config", service.Config)
		renderNestedStringMap(builder, "env", service.Env)
		if service.RunAsNonRoot != nil {
			builder.WriteString(fmt.Sprintf("      runAsNonRoot: %t\n", *service.RunAsNonRoot))
		}
		if service.AllowPrivilegeEscalation != nil {
			builder.WriteString(fmt.Sprintf("      allowPrivilegeEscalation: %t\n", *service.AllowPrivilegeEscalation))
		}
		renderServiceStringList(builder, "dropCapabilities", service.DropCapabilities)
		if service.ReadOnlyRootFilesystem {
			builder.WriteString("      readOnlyRootFilesystem: true\n")
		}
		renderServiceDependencies(builder, service.DependsOn)
		renderServicePorts(builder, service.Ports)
		renderServiceVolumes(builder, service.Volumes)
		renderServiceConfigFiles(builder, service.ConfigFiles)
		renderServiceWritablePaths(builder, service.WritablePaths)
		renderServiceProbe(builder, "readinessProbe", service.ReadinessProbe)
		renderServiceProbe(builder, "livenessProbe", service.LivenessProbe)
		renderServiceIngress(builder, service.Ingress)
	}
	return nil
}

func kubernetesRenderedServices(spec control.ProjectSpec) ([]kubernetesRenderedService, error) {
	release, err := control.ResolveStackReleaseManifestWithFallbackFromEnv(os.Getenv, spec.StackVersion)
	if err != nil {
		return nil, err
	}
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(spec.Domain)), ".")
	states := control.ProjectServiceStates(spec.Services)
	kong := kubernetesServiceFromControlSpec("kong", control.ServiceSpec{Enabled: true}, release.ImageRef("kong", "kong/kong", release.Kong), []kubernetesRenderedPort{{Name: "http", Port: 8000, TargetPort: 8000, Protocol: "TCP"}}, nil, kongKubernetesConfigFiles(spec.Ref, states), defaultIngress(spec.Ref, domain, ""), kongKubernetesEnv(), enabledRenderedDependencies([]kubernetesRenderedDependency{{Service: "auth", Port: 9999}, {Service: "rest", Port: 3000}, {Service: "realtime", Port: 4000}, {Service: "storage", Port: 5000}, {Service: "functions", Port: 9000}}, states), httpProbe("/auth/v1/health", 8000), httpProbe("/auth/v1/health", 8000), []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}})
	kong.Command = []string{"/bin/sh", "-ec"}
	kong.Args = []string{kongKubernetesStartupScript()}
	dbRunAsNonRoot := false
	upstreamImageRunAsNonRoot := false
	kong = withRunAsNonRoot(kong, &upstreamImageRunAsNonRoot)
	dbDropCapabilities := []string{"NET_RAW"}
	dbLiveness := tcpProbe(5432)
	dbLiveness.InitialDelaySeconds = 120
	dbDiskGB := spec.DiskGB
	if dbDiskGB <= 0 {
		_, _, dbDiskGB = control.EffectiveResourceSizing(spec)
	}
	if dbDiskGB <= 0 {
		dbDiskGB = 40
	}
	db := withContainerSecurity(kubernetesServiceFromControlSpec("db", control.ServiceSpec{Enabled: true, Config: map[string]string{"readOnlyRootFilesystem": "false"}}, release.ImageRef("postgres", "supabase/postgres", release.Postgres), []kubernetesRenderedPort{{Name: "postgres", Port: 5432, TargetPort: 5432, Protocol: "TCP"}}, []kubernetesRenderedVolume{{Name: "data", MountPath: "/var/lib/postgresql/data", Size: fmt.Sprintf("%dGi", dbDiskGB)}}, dbKubernetesConfigFiles(), nil, dbKubernetesEnv(), nil, tcpProbe(5432), dbLiveness, []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}}), &dbRunAsNonRoot, nil, dbDropCapabilities)
	db.Command = []string{"bash", "-lc"}
	db.Args = []string{dbKubernetesStartupScript()}
	services := []kubernetesRenderedService{
		db,
		kong,
		withRunAsNonRoot(kubernetesServiceFromControlSpec("meta", control.ServiceSpec{Enabled: true}, release.ImageRef("postgres_meta", "supabase/postgres-meta", release.PostgresMeta), []kubernetesRenderedPort{{Name: "http", Port: 8080, TargetPort: 8080, Protocol: "TCP"}}, nil, nil, nil, metaKubernetesEnv(spec.Ref), enabledRenderedDependencies([]kubernetesRenderedDependency{{Service: "db", Port: 5432}}, states), httpProbe("/", 8080), httpProbe("/", 8080), []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}}), &upstreamImageRunAsNonRoot),
	}
	known := map[string]struct{}{"db": {}, "kong": {}, "meta": {}}
	addKnown := func(name string, image string, ports []kubernetesRenderedPort, volumes []kubernetesRenderedVolume, ingress *kubernetesRenderedIngress, env map[string]string, dependencies []kubernetesRenderedDependency, readiness *kubernetesRenderedProbe, liveness *kubernetesRenderedProbe, writablePaths []kubernetesRenderedWritable) {
		known[name] = struct{}{}
		service := spec.Services[name]
		service.Enabled = states[name]
		services = append(services, kubernetesServiceFromControlSpec(name, service, image, ports, volumes, nil, ingress, env, enabledRenderedDependencies(dependencies, states), readiness, liveness, writablePaths))
	}
	authSpec := spec.Services["auth"]
	authSpec.Enabled = states["auth"]
	auth := kubernetesServiceFromControlSpec("auth", authSpec, release.ImageRef("auth", "supabase/gotrue", release.Auth), []kubernetesRenderedPort{{Name: "http", Port: 9999, TargetPort: 9999, Protocol: "TCP"}}, nil, nil, nil, authKubernetesEnv(), enabledRenderedDependencies([]kubernetesRenderedDependency{{Service: "db", Port: 5432}}, states), httpProbe("/health", 9999), httpProbe("/health", 9999), []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}})
	services = append(services, withRunAsNonRoot(auth, &upstreamImageRunAsNonRoot))
	known["auth"] = struct{}{}
	addKnown("rest", release.ImageRef("rest", "postgrest/postgrest", release.REST), []kubernetesRenderedPort{{Name: "http", Port: 3000, TargetPort: 3000, Protocol: "TCP"}}, nil, nil, nil, []kubernetesRenderedDependency{{Service: "db", Port: 5432}}, httpProbe("/", 3000), httpProbe("/", 3000), nil)
	addKnown("graphql", "", nil, nil, nil, nil, nil, nil, nil, nil)
	addKnown("realtime", release.ImageRef("realtime", "supabase/realtime", release.Realtime), []kubernetesRenderedPort{{Name: "http", Port: 4000, TargetPort: 4000, Protocol: "TCP"}}, nil, nil, realtimeKubernetesEnv(spec.Ref), []kubernetesRenderedDependency{{Service: "db", Port: 5432}}, httpProbe("/", 4000), httpProbe("/", 4000), []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}})
	addKnown("storage", release.ImageRef("storage", "supabase/storage-api", release.Storage), []kubernetesRenderedPort{{Name: "http", Port: 5000, TargetPort: 5000, Protocol: "TCP"}}, []kubernetesRenderedVolume{{Name: "data", MountPath: "/var/lib/storage", Size: "10Gi"}}, defaultIngress("storage-"+spec.Ref, domain, ""), storageKubernetesEnv(spec.Ref), []kubernetesRenderedDependency{{Service: "db", Port: 5432}, {Service: "rest", Port: 3000}}, httpProbe("/status", 5000), httpProbe("/status", 5000), []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}})
	addKnown("imgproxy", release.ImageRef("imgproxy", "darthsim/imgproxy", release.Imgproxy), []kubernetesRenderedPort{{Name: "http", Port: 5001, TargetPort: 5001, Protocol: "TCP"}}, []kubernetesRenderedVolume{{Name: "storage-data", MountPath: "/var/lib/storage", Size: "10Gi", Retain: true}}, nil, imgproxyKubernetesEnv(), nil, httpProbe("/", 5001), httpProbe("/", 5001), []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}})
	addKnown("functions", release.ImageRef("edge_runtime", "supabase/edge-runtime", release.EdgeRuntime), []kubernetesRenderedPort{{Name: "http", Port: 9000, TargetPort: 9000, Protocol: "TCP"}}, nil, nil, functionsKubernetesEnv(spec.Ref), nil, httpProbe("/", 9000), httpProbe("/", 9000), []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}})
	addKnown("pooler", release.ImageRef("pooler", "supabase/supavisor", release.Pooler), []kubernetesRenderedPort{{Name: "transaction", Port: 6543, TargetPort: 6543, Protocol: "TCP"}, {Name: "session", Port: 5432, TargetPort: 5432, Protocol: "TCP"}, {Name: "http", Port: 4000, TargetPort: 4000, Protocol: "TCP"}}, nil, nil, poolerKubernetesEnv(spec.Ref), []kubernetesRenderedDependency{{Service: "db", Port: 5432}}, tcpProbe(6543), tcpProbe(6543), []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}})
	addKnown("studio", release.ImageRef("studio", "supabase/studio", release.Studio), []kubernetesRenderedPort{{Name: "http", Port: 3000, TargetPort: 3000, Protocol: "TCP"}}, nil, defaultIngress("studio-"+spec.Ref, domain, ""), map[string]string{"HOSTNAME": "0.0.0.0"}, []kubernetesRenderedDependency{{Service: "meta", Port: 8080}, {Service: "kong", Port: 8000}}, httpProbe("/", 3000), httpProbe("/", 3000), []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}})
	addKnown("analytics", release.ImageRef("analytics", "supabase/logflare", release.Analytics), []kubernetesRenderedPort{{Name: "http", Port: 4000, TargetPort: 4000, Protocol: "TCP"}}, nil, nil, analyticsKubernetesEnv(spec.Ref), []kubernetesRenderedDependency{{Service: "db", Port: 5432}}, httpProbe("/", 4000), httpProbe("/", 4000), []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}})
	addKnown("vector", release.ImageRef("vector", "timberio/vector", release.Vector), nil, nil, nil, nil, nil, nil, nil, []kubernetesRenderedWritable{{Name: "tmp", MountPath: "/tmp"}})
	keys := make([]string, 0, len(spec.Services))
	for key := range spec.Services {
		if _, ok := known[key]; ok {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		services = append(services, kubernetesServiceFromControlSpec(key, spec.Services[key], "", nil, nil, nil, nil, nil, nil, nil, nil, nil))
	}
	return applyKubernetesResourceAllocations(services, control.ProjectServiceResourceAllocations(spec)), nil
}

func kubernetesRenderedServiceMap(spec control.ProjectSpec) (map[string]kubernetesRenderedService, error) {
	services, err := kubernetesRenderedServices(spec)
	if err != nil {
		return nil, err
	}
	out := make(map[string]kubernetesRenderedService, len(services))
	for _, service := range services {
		out[service.Name] = service
	}
	return out, nil
}

func applyKubernetesResourceAllocations(services []kubernetesRenderedService, allocations map[string]control.ProjectServiceResourceAllocation) []kubernetesRenderedService {
	for index := range services {
		allocation, ok := allocations[services[index].Name]
		if !ok || allocation.CPUMilli <= 0 || allocation.RAMMB <= 0 {
			continue
		}
		resources := kubernetesResourcesFromAllocation(allocation)
		services[index].Resources = &resources
	}
	return services
}

func kubernetesResourcesFromAllocation(allocation control.ProjectServiceResourceAllocation) kubernetesRenderedResources {
	return kubernetesRenderedResources{
		Requests: map[string]string{"cpu": kubernetesCPULimit(allocation.CPUMilli), "memory": fmt.Sprintf("%dMi", allocation.RAMMB)},
		Limits:   map[string]string{"cpu": kubernetesCPULimit(allocation.CPUMilli), "memory": fmt.Sprintf("%dMi", allocation.RAMMB)},
	}
}

func kubernetesCPULimit(milli int) string {
	if milli <= 0 {
		return "0"
	}
	if milli%1000 == 0 {
		return strconv.Itoa(milli / 1000)
	}
	return fmt.Sprintf("%dm", milli)
}

func withRunAsNonRoot(service kubernetesRenderedService, value *bool) kubernetesRenderedService {
	service.RunAsNonRoot = value
	return service
}

func withContainerSecurity(service kubernetesRenderedService, runAsNonRoot *bool, allowPrivilegeEscalation *bool, dropCapabilities []string) kubernetesRenderedService {
	service.RunAsNonRoot = runAsNonRoot
	service.AllowPrivilegeEscalation = allowPrivilegeEscalation
	service.DropCapabilities = dropCapabilities
	return service
}

func kubernetesServiceFromControlSpec(name string, service control.ServiceSpec, defaultImage string, defaultPorts []kubernetesRenderedPort, defaultVolumes []kubernetesRenderedVolume, defaultConfigFiles []kubernetesRenderedConfigFile, defaultIngress *kubernetesRenderedIngress, defaultEnv map[string]string, defaultDependencies []kubernetesRenderedDependency, defaultReadiness *kubernetesRenderedProbe, defaultLiveness *kubernetesRenderedProbe, defaultWritablePaths []kubernetesRenderedWritable) kubernetesRenderedService {
	config := copyStringMap(service.Config)
	image := strings.TrimSpace(config["image"])
	if image == "" {
		image = defaultImage
	}
	ports := defaultPorts
	if port := positiveInt(config["port"]); port > 0 {
		ports = []kubernetesRenderedPort{{Name: "http", Port: port, TargetPort: port, Protocol: "TCP"}}
	}
	volumes := defaultVolumes
	if size := strings.TrimSpace(config["storageSize"]); size != "" {
		mountPath := strings.TrimSpace(config["storageMountPath"])
		if mountPath == "" && len(defaultVolumes) > 0 {
			mountPath = defaultVolumes[0].MountPath
		}
		if mountPath != "" {
			volumes = []kubernetesRenderedVolume{{
				Name:             defaultString(config["storageName"], "data"),
				MountPath:        mountPath,
				Size:             size,
				StorageClassName: strings.TrimSpace(config["storageClassName"]),
				Retain:           strings.EqualFold(strings.TrimSpace(config["retainStorage"]), "true"),
			}}
		}
	}
	ingress := defaultIngress
	if host := strings.TrimSpace(config["ingressHost"]); host != "" {
		ingress = &kubernetesRenderedIngress{
			Enabled:       true,
			Host:          host,
			Path:          defaultString(config["ingressPath"], "/"),
			ClassName:     strings.TrimSpace(config["ingressClassName"]),
			TLSSecretName: strings.TrimSpace(config["ingressTLSSecretName"]),
		}
	}
	if ingress == nil {
		ingress = &kubernetesRenderedIngress{Enabled: false}
	}
	readinessProbe := renderedProbeFromConfig(config, "readiness", defaultReadiness)
	livenessProbe := renderedProbeFromConfig(config, "liveness", defaultLiveness)
	writablePaths := renderedWritablePathsFromConfig(config, defaultWritablePaths)
	dependencies := renderedDependenciesFromConfig(config, defaultDependencies)
	readOnlyRootFilesystem := len(writablePaths) > 0 || readinessProbe != nil || livenessProbe != nil
	if value := strings.TrimSpace(config["readOnlyRootFilesystem"]); value != "" {
		readOnlyRootFilesystem = strings.EqualFold(value, "true")
	}
	var runAsNonRoot *bool
	if value := strings.TrimSpace(config["runAsNonRoot"]); value != "" {
		parsed := strings.EqualFold(value, "true")
		runAsNonRoot = &parsed
	}
	var allowPrivilegeEscalation *bool
	if value := strings.TrimSpace(config["allowPrivilegeEscalation"]); value != "" {
		parsed := strings.EqualFold(value, "true")
		allowPrivilegeEscalation = &parsed
	}
	dropCapabilities := splitCSV(config["dropCapabilities"])
	return kubernetesRenderedService{
		Name:                     name,
		Enabled:                  service.Enabled,
		Image:                    image,
		Replicas:                 positiveInt(config["replicas"]),
		DependsOn:                dependencies,
		Ports:                    ports,
		ServiceType:              strings.TrimSpace(config["serviceType"]),
		Config:                   config,
		Env:                      renderedEnvFromConfig(config, defaultEnv),
		Volumes:                  volumes,
		ConfigFiles:              defaultConfigFiles,
		WritablePaths:            writablePaths,
		RunAsNonRoot:             runAsNonRoot,
		AllowPrivilegeEscalation: allowPrivilegeEscalation,
		DropCapabilities:         dropCapabilities,
		ReadOnlyRootFilesystem:   readOnlyRootFilesystem,
		ReadinessProbe:           readinessProbe,
		LivenessProbe:            livenessProbe,
		Ingress:                  ingress,
	}
}

func dbKubernetesEnv() map[string]string {
	return map[string]string{
		"POSTGRES_DB":   "postgres",
		"POSTGRES_HOST": "/var/run/postgresql",
		"POSTGRES_USER": "postgres",
	}
}

func dbKubernetesConfigFiles() []kubernetesRenderedConfigFile {
	return []kubernetesRenderedConfigFile{{
		Name:      "postgresql-schema-sql",
		MountPath: "/etc/postgresql.schema.sql",
		Content:   dbbootstrap.RenderDatabaseInitSQL(":'supadupa_postgres_password'"),
	}}
}

func dbKubernetesStartupScript() string {
	return `set -euo pipefail

docker-entrypoint.sh postgres -c listen_addresses='*' &
postgres_pid="$!"

forward_signal() {
  kill -TERM "$postgres_pid" 2>/dev/null || true
  wait "$postgres_pid"
}
trap forward_signal TERM INT

until pg_isready -h 127.0.0.1 -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-postgres}" >/dev/null 2>&1; do
  if ! kill -0 "$postgres_pid" 2>/dev/null; then
    wait "$postgres_pid"
    exit $?
  fi
  sleep 2
done

psql -v ON_ERROR_STOP=1 \
  -v "supadupa_postgres_password=${POSTGRES_PASSWORD:?}" \
  -U "${POSTGRES_USER:-postgres}" \
  -d "${POSTGRES_DB:-postgres}" \
  -f /etc/postgresql.schema.sql

wait "$postgres_pid"`
}

func kongKubernetesEnv() map[string]string {
	return map[string]string{
		"KONG_DATABASE":                      "off",
		"KONG_DECLARATIVE_CONFIG":            "/tmp/kong/kong.yml",
		"KONG_DNS_ORDER":                     "LAST,A,CNAME,AAAA",
		"KONG_DNS_NOT_FOUND_TTL":             "1",
		"KONG_NGINX_PROXY_PROXY_BUFFER_SIZE": "160k",
		"KONG_NGINX_PROXY_PROXY_BUFFERS":     "64 160k",
		"KONG_PLUGINS":                       "request-transformer,cors,key-auth,acl,basic-auth,post-function",
		"KONG_PREFIX":                        "/tmp/kong-prefix",
		"SUPABASE_ANON_KEY":                  "$(ANON_KEY)",
		"SUPABASE_SERVICE_KEY":               "$(SERVICE_ROLE_KEY)",
		"SUPABASE_PUBLISHABLE_KEY":           "$(SUPABASE_PUBLISHABLE_KEY)",
		"SUPABASE_SECRET_KEY":                "$(SUPABASE_SECRET_KEY)",
		"ANON_KEY_ASYMMETRIC":                "$(ANON_KEY)",
		"SERVICE_ROLE_KEY_ASYMMETRIC":        "$(SERVICE_ROLE_KEY)",
		"DASHBOARD_USERNAME":                 "$(DASHBOARD_USERNAME)",
		"DASHBOARD_PASSWORD":                 "$(DASHBOARD_PASSWORD)",
	}
}

func kongKubernetesConfigFiles(ref string, states map[string]bool) []kubernetesRenderedConfigFile {
	return []kubernetesRenderedConfigFile{{
		Name:      "kong-yml",
		MountPath: "/home/kong/kong.yml",
		Content:   kongKubernetesDeclarativeConfig(ref, states),
	}}
}

func kongKubernetesStartupScript() string {
	return `set -eu

mkdir -p "$(dirname "$KONG_DECLARATIVE_CONFIG")"

if [ -n "${SUPABASE_SECRET_KEY:-}" ] && [ -n "${SUPABASE_PUBLISHABLE_KEY:-}" ]; then
  export LUA_AUTH_EXPR="\$((headers.authorization ~= nil and headers.authorization:sub(1, 10) ~= 'Bearer sb_' and headers.authorization) or (headers.apikey == '$SUPABASE_SECRET_KEY' and 'Bearer $SERVICE_ROLE_KEY_ASYMMETRIC') or (headers.apikey == '$SUPABASE_PUBLISHABLE_KEY' and 'Bearer $ANON_KEY_ASYMMETRIC') or (headers.apikey ~= nil and 'Bearer ' .. headers.apikey))"
  export LUA_RT_WS_EXPR="\$((query_params.apikey == '$SUPABASE_SECRET_KEY' and '$SERVICE_ROLE_KEY_ASYMMETRIC') or (query_params.apikey == '$SUPABASE_PUBLISHABLE_KEY' and '$ANON_KEY_ASYMMETRIC') or query_params.apikey)"
else
  export LUA_AUTH_EXPR="\$((headers.authorization ~= nil and headers.authorization:sub(1, 10) ~= 'Bearer sb_' and headers.authorization) or (headers.apikey ~= nil and 'Bearer ' .. headers.apikey))"
  export LUA_RT_WS_EXPR="\$(query_params.apikey)"
fi

awk '{
  result = ""
  rest = $0
  while (match(rest, /\$[A-Za-z_][A-Za-z_0-9]*/)) {
    varname = substr(rest, RSTART + 1, RLENGTH - 1)
    if (varname in ENVIRON) {
      result = result substr(rest, 1, RSTART - 1) ENVIRON[varname]
    } else {
      result = result substr(rest, 1, RSTART + RLENGTH - 1)
    }
    rest = substr(rest, RSTART + RLENGTH)
  }
  print result rest
}' /home/kong/kong.yml > "$KONG_DECLARATIVE_CONFIG"

sed -i '/^[[:space:]]*- key:[[:space:]]*$/d' "$KONG_DECLARATIVE_CONFIG"

exec /entrypoint.sh kong docker-start`
}

func kongKubernetesDeclarativeConfig(ref string, states map[string]bool) string {
	serviceName := func(name string) string {
		return kubernetesServiceName(ref, name)
	}
	var builder strings.Builder
	builder.WriteString(`_format_version: "2.1"
_transform: true

consumers:
  - username: anon
    keyauth_credentials:
      - key: $SUPABASE_ANON_KEY
      - key: $SUPABASE_PUBLISHABLE_KEY
  - username: service_role
    keyauth_credentials:
      - key: $SUPABASE_SERVICE_KEY
      - key: $SUPABASE_SECRET_KEY

acls:
  - consumer: anon
    group: anon
  - consumer: service_role
    group: admin

services:
`)
	wroteService := false
	writeHTTPService := func(name string, url string, routeName string, path string) {
		wroteService = true
		builder.WriteString(fmt.Sprintf(`  - name: %s
    url: %s
    routes:
      - name: %s
        strip_path: true
        paths: [%s]
    plugins:
      - name: cors
`, name, url, routeName, path))
	}
	if states["auth"] {
		authURL := "http://" + serviceName("auth") + ":9999"
		writeHTTPService("auth-v1-health", authURL+"/health", "auth-v1-health", "/auth/v1/health")
		writeHTTPService("auth-v1-open-verify", authURL+"/verify", "auth-v1-open-verify", "/auth/v1/verify")
		writeHTTPService("auth-v1-open-callback", authURL+"/callback", "auth-v1-open-callback", "/auth/v1/callback")
		writeHTTPService("auth-v1-open-authorize", authURL+"/authorize", "auth-v1-open-authorize", "/auth/v1/authorize")
		writeHTTPService("auth-v1-open-jwks", authURL+"/.well-known/jwks.json", "auth-v1-open-jwks", "/auth/v1/.well-known/jwks.json")
		wroteService = true
		builder.WriteString(fmt.Sprintf(`  - name: auth-v1
    url: %s/
    routes:
      - name: auth-v1
        strip_path: true
        paths: [/auth/v1/]
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon
`, authURL))
	}
	if states["rest"] {
		writeProtectedHTTPService(&builder, "rest-v1", "http://"+serviceName("rest")+":3000/", "rest-v1", "/rest/v1/", []string{})
		wroteService = true
	}
	if states["graphql"] && states["rest"] {
		writeProtectedHTTPService(&builder, "graphql-v1", "http://"+serviceName("rest")+":3000/rpc/graphql", "graphql-v1", "/graphql/v1", []string{`              - "Content-Profile: graphql_public"`})
		wroteService = true
	}
	if states["realtime"] {
		realtimeURL := "http://" + serviceName("realtime") + ":4000"
		wroteService = true
		builder.WriteString(fmt.Sprintf(`  - name: realtime-v1-ws
    url: %s/socket
    protocol: ws
    routes:
      - name: realtime-v1-ws
        strip_path: true
        paths: [/realtime/v1/]
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "x-api-key:$LUA_RT_WS_EXPR"
          replace:
            querystring:
              - "apikey:$LUA_RT_WS_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon
`, realtimeURL))
		writeProtectedHTTPService(&builder, "realtime-v1-rest", realtimeURL+"/api", "realtime-v1-rest", "/realtime/v1/api", []string{})
	}
	if states["storage"] {
		wroteService = true
		builder.WriteString(fmt.Sprintf(`  - name: storage-v1
    url: http://%s:5000/
    routes:
      - name: storage-v1
        strip_path: true
        paths: [/storage/v1/]
    plugins:
      - name: cors
      - name: request-transformer
        config:
          add:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: post-function
        config:
          access:
            - |
              local auth = kong.request.get_header("authorization")
              if auth == nil or auth == "" or auth:find("^%%s*$") then
                kong.service.request.clear_header("authorization")
              end
`, serviceName("storage")))
	}
	if states["functions"] {
		writeHTTPService("functions-v1", "http://"+serviceName("functions")+":9000/", "functions-v1", "/functions/v1/")
	}
	if states["analytics"] {
		writeHTTPService("analytics", "http://"+serviceName("analytics")+":4000", "analytics", "/analytics/v1")
	}
	if !wroteService {
		return `_format_version: "2.1"
_transform: true

services: []
`
	}
	return builder.String()
}

func writeProtectedHTTPService(builder *strings.Builder, name string, url string, routeName string, path string, extraAddHeaders []string) {
	builder.WriteString(fmt.Sprintf(`  - name: %s
    url: %s
    routes:
      - name: %s
        strip_path: true
        paths: [%s]
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
`, name, url, routeName, path))
	for _, header := range extraAddHeaders {
		builder.WriteString(header)
		builder.WriteByte('\n')
	}
	builder.WriteString(`              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon
`)
}

func kubernetesServiceName(ref string, service string) string {
	ref = kubernetesDNSLabel(ref)
	service = kubernetesDNSLabel(service)
	switch {
	case ref == "" && service == "":
		return "service"
	case ref == "":
		return service
	case service == "":
		return ref
	default:
		return trimKubernetesDNSLabel(ref + "-" + service)
	}
}

func kubernetesDNSLabel(input string) string {
	out := strings.ToLower(strings.TrimSpace(input))
	var builder strings.Builder
	lastDash := false
	for _, r := range out {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return trimKubernetesDNSLabel(builder.String())
}

func trimKubernetesDNSLabel(input string) string {
	out := strings.Trim(input, "-")
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	return out
}

func metaKubernetesEnv(ref string) map[string]string {
	return map[string]string{
		"PG_META_DB_HOST":     kubernetesServiceName(ref, "db"),
		"PG_META_DB_NAME":     "$(POSTGRES_DB)",
		"PG_META_DB_PASSWORD": "$(POSTGRES_PASSWORD)",
		"PG_META_DB_PORT":     "$(POSTGRES_PORT)",
		"PG_META_DB_USER":     "$(POSTGRES_USER)",
	}
}

func authKubernetesEnv() map[string]string {
	return map[string]string{
		"GOTRUE_DB_DRIVER": "postgres",
	}
}

func realtimeKubernetesEnv(ref string) map[string]string {
	return map[string]string{
		"PORT":                        "4000",
		"DB_HOST":                     kubernetesServiceName(ref, "db"),
		"DB_PORT":                     "$(POSTGRES_PORT)",
		"DB_USER":                     "supabase_admin",
		"DB_PASSWORD":                 "$(POSTGRES_PASSWORD)",
		"DB_NAME":                     "$(POSTGRES_DB)",
		"DB_AFTER_CONNECT_QUERY":      "SET search_path TO _realtime",
		"DB_ENC_KEY":                  "$(DB_ENC_KEY)",
		"API_JWT_SECRET":              "$(JWT_SECRET)",
		"METRICS_JWT_SECRET":          "$(JWT_SECRET)",
		"SECRET_KEY_BASE":             "$(SECRET_KEY_BASE)",
		"ERL_AFLAGS":                  "-proto_dist inet_tcp",
		"DNS_NODES":                   "''",
		"RLIMIT_NOFILE":               "10000",
		"APP_NAME":                    "realtime",
		"SEED_SELF_HOST":              "true",
		"SELF_HOST_TENANT_NAME":       "$(PROJECT_REF)",
		"RUN_JANITOR":                 "true",
		"DISABLE_HEALTHCHECK_LOGGING": "true",
	}
}

func storageKubernetesEnv(ref string) map[string]string {
	dbHost := kubernetesServiceName(ref, "db")
	return map[string]string{
		"ANON_KEY":                          "$(ANON_KEY)",
		"SERVICE_KEY":                       "$(SERVICE_ROLE_KEY)",
		"POSTGREST_URL":                     "http://" + kubernetesServiceName(ref, "rest") + ":3000",
		"AUTH_JWT_SECRET":                   "$(JWT_SECRET)",
		"DATABASE_URL":                      "postgres://supabase_storage_admin:$(POSTGRES_PASSWORD)@" + dbHost + ":5432/$(POSTGRES_DB)",
		"STORAGE_PUBLIC_URL":                "$(STORAGE_PUBLIC_URL)",
		"FILE_SIZE_LIMIT":                   "$(FILE_SIZE_LIMIT)",
		"STORAGE_BACKEND":                   "$(STORAGE_BACKEND)",
		"GLOBAL_S3_BUCKET":                  "$(GLOBAL_S3_BUCKET)",
		"REQUEST_ALLOW_X_FORWARDED_PATH":    "$(REQUEST_ALLOW_X_FORWARDED_PATH)",
		"FILE_STORAGE_BACKEND_PATH":         "/var/lib/storage",
		"TENANT_ID":                         "$(STORAGE_TENANT_ID)",
		"REGION":                            "$(REGION)",
		"ENABLE_IMAGE_TRANSFORMATION":       "true",
		"IMGPROXY_URL":                      "$(STORAGE_IMGPROXY_URL)",
		"UPLOAD_FILE_SIZE_LIMIT":            "$(UPLOAD_FILE_SIZE_LIMIT)",
		"UPLOAD_FILE_SIZE_LIMIT_STANDARD":   "$(UPLOAD_FILE_SIZE_LIMIT_STANDARD)",
		"UPLOAD_SIGNED_URL_EXPIRATION_TIME": "$(UPLOAD_SIGNED_URL_EXPIRATION_TIME)",
		"TUS_URL_PATH":                      "$(TUS_URL_PATH)",
		"TUS_URL_EXPIRY_MS":                 "$(TUS_URL_EXPIRY_MS)",
		"S3_PROTOCOL_ACCESS_KEY_ID":         "$(S3_PROTOCOL_ACCESS_KEY_ID)",
		"S3_PROTOCOL_ACCESS_KEY_SECRET":     "$(S3_PROTOCOL_ACCESS_KEY_SECRET)",
	}
}

func imgproxyKubernetesEnv() map[string]string {
	return map[string]string{
		"IMGPROXY_LOCAL_FILESYSTEM_ROOT": "/",
	}
}

func functionsKubernetesEnv(ref string) map[string]string {
	return map[string]string{
		"JWT_SECRET":                     "$(JWT_SECRET)",
		"SUPABASE_URL":                   "http://" + kubernetesServiceName(ref, "kong") + ":8000",
		"SUPABASE_PUBLIC_URL":            "$(SUPABASE_PUBLIC_URL)",
		"SUPABASE_ANON_KEY":              "$(ANON_KEY)",
		"SUPABASE_SERVICE_ROLE_KEY":      "$(SERVICE_ROLE_KEY)",
		"SUPABASE_PUBLISHABLE_KEYS":      "{\"default\":\"$(SUPABASE_PUBLISHABLE_KEY)\"}",
		"SUPABASE_SECRET_KEYS":           "{\"default\":\"$(SUPABASE_SECRET_KEY)\"}",
		"SUPABASE_DB_URL":                "postgresql://postgres:$(POSTGRES_PASSWORD)@" + kubernetesServiceName(ref, "db") + ":5432/$(POSTGRES_DB)",
		"VERIFY_JWT":                     "$(FUNCTIONS_VERIFY_JWT)",
		"SUPADUPA_FUNCTION_STORAGE_ROOT": "/mnt/.supadupa-storage/$(STORAGE_TENANT_ID)/$(GLOBAL_S3_BUCKET)",
	}
}

func poolerKubernetesEnv(ref string) map[string]string {
	dbHost := kubernetesServiceName(ref, "db")
	return map[string]string{
		"PORT":                     "4000",
		"POSTGRES_PORT":            "$(POSTGRES_PORT)",
		"POSTGRES_HOST":            dbHost,
		"POSTGRES_DB":              "$(POSTGRES_DB)",
		"POSTGRES_PASSWORD":        "$(POSTGRES_PASSWORD)",
		"DATABASE_URL":             "ecto://supabase_admin:$(POSTGRES_PASSWORD)@" + dbHost + ":5432/_supabase",
		"CLUSTER_POSTGRES":         "true",
		"SECRET_KEY_BASE":          "$(SECRET_KEY_BASE)",
		"VAULT_ENC_KEY":            "$(VAULT_ENC_KEY)",
		"API_JWT_SECRET":           "$(JWT_SECRET)",
		"METRICS_JWT_SECRET":       "$(JWT_SECRET)",
		"REGION":                   "$(REGION)",
		"POOLER_TENANT_ID":         "$(PROJECT_REF)",
		"POOLER_DEFAULT_POOL_SIZE": "20",
		"POOLER_MAX_CLIENT_CONN":   "200",
		"POOLER_POOL_MODE":         "transaction",
		"DB_POOL_SIZE":             "5",
	}
}

func analyticsKubernetesEnv(ref string) map[string]string {
	dbHost := kubernetesServiceName(ref, "db")
	return map[string]string{
		"LOGFLARE_NODE_HOST":             "127.0.0.1",
		"DB_USERNAME":                    "supabase_admin",
		"DB_DATABASE":                    "$(POSTGRES_DB)",
		"DB_HOSTNAME":                    dbHost,
		"DB_PORT":                        "$(POSTGRES_PORT)",
		"DB_PASSWORD":                    "$(POSTGRES_PASSWORD)",
		"DB_SCHEMA":                      "_analytics",
		"LOGFLARE_SINGLE_TENANT":         "true",
		"LOGFLARE_SUPABASE_MODE":         "true",
		"LOGFLARE_PUBLIC_ACCESS_TOKEN":   "$(LOGFLARE_PUBLIC_ACCESS_TOKEN)",
		"LOGFLARE_PRIVATE_ACCESS_TOKEN":  "$(LOGFLARE_PRIVATE_ACCESS_TOKEN)",
		"LOGFLARE_FEATURE_FLAG_OVERRIDE": "multibackend=true",
		"POSTGRES_BACKEND_URL":           "postgresql://supabase_admin:$(POSTGRES_PASSWORD)@" + dbHost + ":5432/$(POSTGRES_DB)",
		"POSTGRES_BACKEND_SCHEMA":        "_analytics",
	}
}

func renderedEnvFromConfig(config map[string]string, fallback map[string]string) map[string]string {
	env := copyStringMap(fallback)
	for key, value := range config {
		name, ok := strings.CutPrefix(strings.TrimSpace(key), "env.")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		env[name] = value
	}
	return env
}

func httpProbe(path string, port int) *kubernetesRenderedProbe {
	return &kubernetesRenderedProbe{Type: "http", Path: defaultString(path, "/"), Port: port, PeriodSeconds: 10, TimeoutSeconds: 2, FailureThreshold: 6}
}

func tcpProbe(port int) *kubernetesRenderedProbe {
	return &kubernetesRenderedProbe{Type: "tcp", Port: port, PeriodSeconds: 10, TimeoutSeconds: 2, FailureThreshold: 6}
}

func renderedProbeFromConfig(config map[string]string, prefix string, fallback *kubernetesRenderedProbe) *kubernetesRenderedProbe {
	if strings.EqualFold(strings.TrimSpace(config[prefix+"ProbeEnabled"]), "false") {
		return nil
	}
	probe := fallback
	if probe != nil {
		copied := *probe
		probe = &copied
	}
	if path := strings.TrimSpace(config[prefix+"Path"]); path != "" {
		if probe == nil {
			probe = httpProbe(path, positiveInt(config[prefix+"Port"]))
		}
		probe.Type = "http"
		probe.Path = path
	}
	if probeType := strings.TrimSpace(config[prefix+"ProbeType"]); probeType != "" {
		if probe == nil {
			probe = &kubernetesRenderedProbe{}
		}
		probe.Type = strings.ToLower(probeType)
	}
	if port := positiveInt(config[prefix+"Port"]); port > 0 {
		if probe == nil {
			probe = tcpProbe(port)
		}
		probe.Port = port
	}
	if probe == nil || probe.Port <= 0 {
		return nil
	}
	if probe.Type == "" {
		probe.Type = "tcp"
	}
	if probe.Type == "http" && probe.Path == "" {
		probe.Path = "/"
	}
	applyPositiveInt(config[prefix+"InitialDelaySeconds"], &probe.InitialDelaySeconds)
	applyPositiveInt(config[prefix+"PeriodSeconds"], &probe.PeriodSeconds)
	applyPositiveInt(config[prefix+"TimeoutSeconds"], &probe.TimeoutSeconds)
	applyPositiveInt(config[prefix+"FailureThreshold"], &probe.FailureThreshold)
	return probe
}

func renderedWritablePathsFromConfig(config map[string]string, fallback []kubernetesRenderedWritable) []kubernetesRenderedWritable {
	if strings.EqualFold(strings.TrimSpace(config["writablePathsEnabled"]), "false") {
		return nil
	}
	paths := append([]kubernetesRenderedWritable(nil), fallback...)
	if configured := strings.TrimSpace(config["writablePaths"]); configured != "" {
		paths = nil
		for index, part := range strings.Split(configured, ",") {
			path := strings.TrimSpace(part)
			if path == "" {
				continue
			}
			paths = append(paths, kubernetesRenderedWritable{Name: fmt.Sprintf("writable-%d", index+1), MountPath: path})
		}
	}
	for index := range paths {
		if paths[index].Name == "" {
			paths[index].Name = fmt.Sprintf("writable-%d", index+1)
		}
	}
	return paths
}

func splitCSV(value string) []string {
	configured := strings.TrimSpace(value)
	if configured == "" {
		return nil
	}
	parts := strings.Split(configured, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func renderedDependenciesFromConfig(config map[string]string, fallback []kubernetesRenderedDependency) []kubernetesRenderedDependency {
	if strings.EqualFold(strings.TrimSpace(config["dependsOnEnabled"]), "false") {
		return nil
	}
	dependencies := append([]kubernetesRenderedDependency(nil), fallback...)
	if configured := strings.TrimSpace(config["dependsOn"]); configured != "" {
		dependencies = nil
		for _, part := range strings.Split(configured, ",") {
			service, portText, ok := strings.Cut(strings.TrimSpace(part), ":")
			if !ok {
				continue
			}
			port := positiveInt(portText)
			service = strings.TrimSpace(service)
			if service == "" || port <= 0 {
				continue
			}
			dependencies = append(dependencies, kubernetesRenderedDependency{Service: service, Port: port})
		}
	}
	out := make([]kubernetesRenderedDependency, 0, len(dependencies))
	seen := map[string]struct{}{}
	for _, dependency := range dependencies {
		dependency.Service = strings.TrimSpace(dependency.Service)
		if dependency.Service == "" || dependency.Port <= 0 {
			continue
		}
		key := dependency.Service + ":" + strconv.Itoa(dependency.Port)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, dependency)
	}
	return out
}

func enabledRenderedDependencies(dependencies []kubernetesRenderedDependency, states map[string]bool) []kubernetesRenderedDependency {
	if len(dependencies) == 0 {
		return nil
	}
	out := make([]kubernetesRenderedDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		service := strings.TrimSpace(dependency.Service)
		if service == "" || dependency.Port <= 0 {
			continue
		}
		switch service {
		case "db", "kong", "meta":
			out = append(out, dependency)
		default:
			if states[service] {
				out = append(out, dependency)
			}
		}
	}
	return out
}

func applyPositiveInt(value string, target *int) {
	if parsed := positiveInt(value); parsed > 0 {
		*target = parsed
	}
}

func defaultIngress(host string, domain string, path string) *kubernetesRenderedIngress {
	host = strings.Trim(host, ".")
	domain = strings.Trim(domain, ".")
	if host == "" || domain == "" {
		return nil
	}
	if path == "" {
		path = "/"
	}
	return &kubernetesRenderedIngress{Enabled: true, Host: host + "." + domain, Path: path}
}

func positiveInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func managedLabels(extra ...string) map[string]string {
	labels := map[string]string{"app.kubernetes.io/managed-by": "supadupa"}
	for index := 0; index+1 < len(extra); index += 2 {
		if extra[index] != "" && extra[index+1] != "" {
			labels[extra[index]] = extra[index+1]
		}
	}
	return labels
}

func renderServiceDependencies(builder *strings.Builder, dependencies []kubernetesRenderedDependency) {
	if len(dependencies) == 0 {
		return
	}
	builder.WriteString("      dependsOn:\n")
	for _, dependency := range dependencies {
		builder.WriteString(fmt.Sprintf("        - service: %s\n", yamlString(dependency.Service)))
		builder.WriteString(fmt.Sprintf("          port: %d\n", dependency.Port))
	}
}

func renderServicePorts(builder *strings.Builder, ports []kubernetesRenderedPort) {
	builder.WriteString("      ports:\n")
	if len(ports) == 0 {
		builder.WriteString("        []\n")
		return
	}
	for _, port := range ports {
		builder.WriteString(fmt.Sprintf("        - name: %s\n", yamlString(port.Name)))
		builder.WriteString(fmt.Sprintf("          port: %d\n", port.Port))
		builder.WriteString(fmt.Sprintf("          targetPort: %d\n", port.TargetPort))
		builder.WriteString(fmt.Sprintf("          protocol: %s\n", yamlString(defaultString(port.Protocol, "TCP"))))
	}
}

func renderServiceVolumes(builder *strings.Builder, volumes []kubernetesRenderedVolume) {
	builder.WriteString("      volumes:\n")
	if len(volumes) == 0 {
		builder.WriteString("        []\n")
		return
	}
	for _, volume := range volumes {
		builder.WriteString(fmt.Sprintf("        - name: %s\n", yamlString(volume.Name)))
		builder.WriteString(fmt.Sprintf("          mountPath: %s\n", yamlString(volume.MountPath)))
		builder.WriteString(fmt.Sprintf("          size: %s\n", yamlString(volume.Size)))
		if volume.StorageClassName != "" {
			builder.WriteString(fmt.Sprintf("          storageClassName: %s\n", yamlString(volume.StorageClassName)))
		}
		if volume.Retain {
			builder.WriteString("          retain: true\n")
		}
	}
}

func renderServiceStringList(builder *strings.Builder, name string, values []string) {
	if len(values) == 0 {
		return
	}
	builder.WriteString(fmt.Sprintf("      %s:\n", name))
	for _, value := range values {
		builder.WriteString("        - ")
		if strings.Contains(value, "\n") {
			builder.WriteString("|-\n")
			renderIndentedYAMLBlock(builder, value, 12)
			continue
		}
		builder.WriteString(yamlString(value))
		builder.WriteByte('\n')
	}
}

func renderServiceConfigFiles(builder *strings.Builder, files []kubernetesRenderedConfigFile) {
	if len(files) == 0 {
		return
	}
	builder.WriteString("      configFiles:\n")
	for _, file := range files {
		if file.Name != "" {
			builder.WriteString(fmt.Sprintf("        - name: %s\n", yamlString(file.Name)))
		} else {
			builder.WriteString("        - name: \"config\"\n")
		}
		builder.WriteString(fmt.Sprintf("          mountPath: %s\n", yamlString(file.MountPath)))
		builder.WriteString("          content: |-\n")
		renderIndentedYAMLBlock(builder, file.Content, 12)
	}
}

func renderIndentedYAMLBlock(builder *strings.Builder, value string, indent int) {
	prefix := strings.Repeat(" ", indent)
	value = strings.TrimRight(value, "\n")
	if value == "" {
		builder.WriteString(prefix + "\n")
		return
	}
	for _, line := range strings.Split(value, "\n") {
		builder.WriteString(prefix)
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
}

func renderServiceWritablePaths(builder *strings.Builder, paths []kubernetesRenderedWritable) {
	if len(paths) == 0 {
		return
	}
	builder.WriteString("      writablePaths:\n")
	for _, path := range paths {
		builder.WriteString(fmt.Sprintf("        - name: %s\n", yamlString(path.Name)))
		builder.WriteString(fmt.Sprintf("          mountPath: %s\n", yamlString(path.MountPath)))
	}
}

func renderServiceProbe(builder *strings.Builder, name string, probe *kubernetesRenderedProbe) {
	if probe == nil {
		return
	}
	builder.WriteString(fmt.Sprintf("      %s:\n", name))
	if probe.Type != "" {
		builder.WriteString(fmt.Sprintf("        type: %s\n", yamlString(probe.Type)))
	}
	if probe.Path != "" {
		builder.WriteString(fmt.Sprintf("        path: %s\n", yamlString(probe.Path)))
	}
	builder.WriteString(fmt.Sprintf("        port: %d\n", probe.Port))
	if probe.InitialDelaySeconds > 0 {
		builder.WriteString(fmt.Sprintf("        initialDelaySeconds: %d\n", probe.InitialDelaySeconds))
	}
	if probe.PeriodSeconds > 0 {
		builder.WriteString(fmt.Sprintf("        periodSeconds: %d\n", probe.PeriodSeconds))
	}
	if probe.TimeoutSeconds > 0 {
		builder.WriteString(fmt.Sprintf("        timeoutSeconds: %d\n", probe.TimeoutSeconds))
	}
	if probe.FailureThreshold > 0 {
		builder.WriteString(fmt.Sprintf("        failureThreshold: %d\n", probe.FailureThreshold))
	}
}

func renderServiceIngress(builder *strings.Builder, ingress *kubernetesRenderedIngress) {
	builder.WriteString("      ingress:\n")
	if ingress == nil || !ingress.Enabled {
		builder.WriteString("        enabled: false\n")
		return
	}
	builder.WriteString("        enabled: true\n")
	builder.WriteString(fmt.Sprintf("        host: %s\n", yamlString(ingress.Host)))
	builder.WriteString(fmt.Sprintf("        path: %s\n", yamlString(defaultString(ingress.Path, "/"))))
	if ingress.ClassName != "" {
		builder.WriteString(fmt.Sprintf("        className: %s\n", yamlString(ingress.ClassName)))
	}
	if ingress.TLSSecretName != "" {
		builder.WriteString(fmt.Sprintf("        tlsSecretName: %s\n", yamlString(ingress.TLSSecretName)))
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

func projectSpecScalar(payload []byte, key string) (string, error) {
	_, spec, err := decodeProjectManifestSpec(payload, false)
	if err != nil {
		return "", err
	}
	if spec == nil {
		return "", nil
	}
	value, ok, err := mappingValue(spec, key)
	if err != nil || !ok {
		return "", err
	}
	if value.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("spec.%s must be a scalar", key)
	}
	return value.Value, nil
}

func replaceProjectSpecScalar(payload []byte, key string, value string) ([]byte, error) {
	doc, spec, err := decodeProjectManifestSpec(payload, true)
	if err != nil {
		return nil, err
	}
	current, ok, err := mappingValue(spec, key)
	if err != nil {
		return nil, err
	}
	if !ok {
		spec.Content = append(spec.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key})
		current = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle}
		spec.Content = append(spec.Content, current)
	}
	if current.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("spec.%s must be a scalar", key)
	}
	current.Tag = "!!str"
	current.Value = value
	current.Style = yaml.DoubleQuotedStyle
	return encodeYAMLDocument(doc)
}

func replaceProjectSpecValue(payload []byte, key string, value any) ([]byte, error) {
	doc, spec, err := decodeProjectManifestSpec(payload, true)
	if err != nil {
		return nil, err
	}
	valueNode, err := yamlNodeForValue(value)
	if err != nil {
		return nil, err
	}
	_, ok, err := mappingValue(spec, key)
	if err != nil {
		return nil, err
	}
	if !ok {
		spec.Content = append(spec.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key})
		spec.Content = append(spec.Content, valueNode)
		return encodeYAMLDocument(doc)
	}
	for index := 0; index < len(spec.Content); index += 2 {
		keyNode := spec.Content[index]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == key {
			spec.Content[index+1] = valueNode
			return encodeYAMLDocument(doc)
		}
	}
	return nil, fmt.Errorf("spec.%s was not found after lookup", key)
}

func decodeProjectManifestSpec(payload []byte, createSpec bool) (*yaml.Node, *yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(payload, &doc); err != nil {
		return nil, nil, fmt.Errorf("invalid project manifest YAML: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, nil, fmt.Errorf("project manifest is empty")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("project manifest must be a YAML mapping")
	}
	spec, ok, err := mappingValue(root, "spec")
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		if !createSpec {
			return &doc, nil, nil
		}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "spec"})
		spec = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, spec)
	}
	if spec.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("spec must be a YAML mapping")
	}
	return &doc, spec, nil
}

func mappingValue(mapping *yaml.Node, key string) (*yaml.Node, bool, error) {
	if mapping.Kind != yaml.MappingNode {
		return nil, false, fmt.Errorf("expected YAML mapping while reading %s", key)
	}
	var match *yaml.Node
	for index := 0; index < len(mapping.Content); index += 2 {
		keyNode := mapping.Content[index]
		valueNode := mapping.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Value != key {
			continue
		}
		if match != nil {
			return nil, false, fmt.Errorf("duplicate YAML key %q", key)
		}
		match = valueNode
	}
	if match == nil {
		return nil, false, nil
	}
	return match, true, nil
}

func encodeYAMLDocument(doc *yaml.Node) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		_ = encoder.Close()
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func yamlNodeForValue(value any) (*yaml.Node, error) {
	payload, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(payload, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("encoded YAML value is empty")
	}
	return doc.Content[0], nil
}

func encodeYAMLValue(value any) string {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(value); err != nil {
		_ = encoder.Close()
		return ""
	}
	if err := encoder.Close(); err != nil {
		return ""
	}
	return buffer.String()
}

func yamlKey(value string) string {
	if value == "" {
		return `""`
	}
	first := value[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		return yamlString(value)
	}
	switch strings.ToLower(value) {
	case "true", "false", "null", "yes", "no", "on", "off":
		return yamlString(value)
	}
	for _, char := range value[1:] {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' {
			continue
		}
		return yamlString(value)
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
