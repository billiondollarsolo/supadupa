package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Client interface {
	ListProjects(ctx context.Context, namespace string) ([]Project, error)
	ListProjectConfigs(ctx context.Context, namespace string) ([]ProjectConfig, error)
	ListProjectAuthHooks(ctx context.Context, namespace string) ([]ProjectAuthHooks, error)
	ListProjectBranchClones(ctx context.Context, namespace string) ([]ProjectBranchClone, error)
	ListProjectReplicas(ctx context.Context, namespace string) ([]ProjectReplica, error)
	ListRetainedProjectResources(ctx context.Context, namespace string) ([]RetainedProjectResources, error)
	EnsureNamespace(ctx context.Context, name string, labels map[string]string) error
	DeleteNamespace(ctx context.Context, name string) error
	ApplyProjectIsolation(ctx context.Context, namespace string, project Project, iso ProjectIsolation) error
	ApplyProjectResources(ctx context.Context, namespace string, project Project, resources ProjectResources) error
	PruneProjectResources(ctx context.Context, namespace string, project Project, resources ProjectResources) error
	ObserveProjectResources(ctx context.Context, namespace string, resources ProjectResources) (ProjectResourceObservation, error)
	DeleteProjectResources(ctx context.Context, namespace string, project Project, resources ProjectResources) error
	SetProjectFinalizers(ctx context.Context, namespace string, name string, finalizers []string) error
	PatchProjectStatus(ctx context.Context, namespace string, name string, status ProjectStatus) error
	PatchProjectConfigStatus(ctx context.Context, namespace string, name string, status ResourceStatus) error
	PatchProjectAuthHooksStatus(ctx context.Context, namespace string, name string, status ResourceStatus) error
	PatchProjectBranchCloneStatus(ctx context.Context, namespace string, name string, status ResourceStatus) error
	PatchProjectReplicaStatus(ctx context.Context, namespace string, name string, status ReplicaStatus) error
	PatchRetainedProjectResourcesStatus(ctx context.Context, namespace string, name string, status ResourceStatus) error
}

const projectFinalizer = "supadupa.dev/runtime-namespace"

type Reconciler struct {
	Client Client
	Now    func() time.Time

	// IsolationEnabled turns on namespace-per-project + default-deny isolation.
	// When false the operator behaves exactly as the legacy single-namespace
	// implementation: every runtime object lands in the control namespace and no
	// namespace/policy/quota objects are created.
	IsolationEnabled bool
	// RuntimeNamespacePrefix is prepended to kubernetesName(ref) when deriving a
	// project's runtime namespace. Defaults to "supadupa-proj-".
	RuntimeNamespacePrefix string
	// PodSecurityEnforce is the Pod Security Admission enforce level stamped on
	// each runtime namespace (privileged|baseline|restricted). Defaults to
	// "baseline" because the Supabase db pod requires root.
	PodSecurityEnforce string
	// PodSecurityAudit/PodSecurityWarn are the PSA audit and warn levels. They
	// default to "restricted" (even when enforce is "baseline") so that policy
	// violations are surfaced via the API server even where they are not blocked.
	PodSecurityAudit string
	PodSecurityWarn  string
	// PodSecurity*Version pin the policy version for each PSA mode. Empty defaults
	// to "latest".
	PodSecurityEnforceVersion string
	PodSecurityAuditVersion   string
	PodSecurityWarnVersion    string
	// NetworkPolicyEnabled gates the generation/apply of the per-project
	// NetworkPolicy set. When false the operator still provisions the namespace,
	// ServiceAccount, ResourceQuota and LimitRange but applies no NetworkPolicies.
	// Defaults to true via the operator binary.
	NetworkPolicyEnabled bool
	// IngressControllerNamespaceSelector/IngressControllerNamespace identify the
	// namespace running the ingress controller so its traffic may be allowed.
	IngressControllerNamespaceSelector map[string]string
	IngressControllerNamespace         string
	// DNSNamespace is the namespace serving cluster DNS (default kube-system).
	DNSNamespace string
	// ExtraEgressCIDRs are platform-wide CIDRs every project namespace may egress
	// to (in addition to per-project spec.runtimeNetwork.allowedEgressCidrs).
	ExtraEgressCIDRs []string
	// DefaultQuota/DefaultLimits are optional platform-wide ResourceQuota and
	// LimitRange defaults applied per runtime namespace.
	DefaultQuota  *ProjectQuotaDefaults
	DefaultLimits *ProjectLimitDefaults
}

func (r Reconciler) runtimeNamespacePrefix() string {
	if prefix := strings.TrimSpace(r.RuntimeNamespacePrefix); prefix != "" {
		return prefix
	}
	return "supadupa-proj-"
}

// runtimeNamespace resolves the namespace that holds a project's runtime
// resources. With isolation disabled this is always the control namespace.
func (r Reconciler) runtimeNamespace(project Project, controlNS string) string {
	if !r.IsolationEnabled {
		return controlNS
	}
	if explicit := strings.TrimSpace(project.Spec.RuntimeNamespace); explicit != "" {
		return explicit
	}
	ref := strings.TrimSpace(project.Spec.Ref)
	if ref == "" {
		ref = strings.TrimSpace(project.Metadata.Name)
	}
	return r.runtimeNamespacePrefix() + kubernetesName(ref)
}

// isolationOwnsNamespace reports whether the operator is responsible for the
// lifecycle of the project's runtime namespace (so it may create/delete it).
func (r Reconciler) isolationOwnsNamespace(project Project, controlNS string) bool {
	if !r.IsolationEnabled {
		return false
	}
	return r.runtimeNamespace(project, controlNS) != controlNS
}

func (r Reconciler) isolationConfig() IsolationConfig {
	return IsolationConfig{
		NetworkPolicyEnabled:               r.NetworkPolicyEnabled,
		IngressControllerNamespaceSelector: r.IngressControllerNamespaceSelector,
		IngressControllerNamespace:         r.IngressControllerNamespace,
		DNSNamespace:                       r.DNSNamespace,
		ExtraEgressCIDRs:                   r.ExtraEgressCIDRs,
		DefaultQuota:                       r.DefaultQuota,
		DefaultLimits:                      r.DefaultLimits,
	}
}

// podSecurityLevels resolves the enforce/audit/warn levels and versions stamped
// on a runtime namespace. enforce defaults to "baseline"; audit/warn default to
// "restricted" so violations are surfaced even when enforcement is relaxed.
func (r Reconciler) podSecurityLevels() PodSecurityLevels {
	return PodSecurityLevels{
		Enforce:        r.PodSecurityEnforce,
		Audit:          r.PodSecurityAudit,
		Warn:           r.PodSecurityWarn,
		EnforceVersion: r.PodSecurityEnforceVersion,
		AuditVersion:   r.PodSecurityAuditVersion,
		WarnVersion:    r.PodSecurityWarnVersion,
	}
}

type Project struct {
	APIVersion string        `json:"apiVersion,omitempty"`
	Kind       string        `json:"kind,omitempty"`
	Metadata   ObjectMeta    `json:"metadata,omitempty"`
	Spec       ProjectSpec   `json:"spec,omitempty"`
	Status     ProjectStatus `json:"status,omitempty"`
}

type ProjectReplica struct {
	APIVersion string             `json:"apiVersion,omitempty"`
	Kind       string             `json:"kind,omitempty"`
	Metadata   ObjectMeta         `json:"metadata,omitempty"`
	Spec       ProjectReplicaSpec `json:"spec,omitempty"`
	Status     ReplicaStatus      `json:"status,omitempty"`
}

type ProjectConfig struct {
	APIVersion string            `json:"apiVersion,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Metadata   ObjectMeta        `json:"metadata,omitempty"`
	Spec       ProjectConfigSpec `json:"spec,omitempty"`
	Status     ResourceStatus    `json:"status,omitempty"`
}

type ProjectAuthHooks struct {
	APIVersion string               `json:"apiVersion,omitempty"`
	Kind       string               `json:"kind,omitempty"`
	Metadata   ObjectMeta           `json:"metadata,omitempty"`
	Spec       ProjectAuthHooksSpec `json:"spec,omitempty"`
	Status     ResourceStatus       `json:"status,omitempty"`
}

type ProjectBranchClone struct {
	APIVersion string                 `json:"apiVersion,omitempty"`
	Kind       string                 `json:"kind,omitempty"`
	Metadata   ObjectMeta             `json:"metadata,omitempty"`
	Spec       ProjectBranchCloneSpec `json:"spec,omitempty"`
	Status     ResourceStatus         `json:"status,omitempty"`
}

type RetainedProjectResources struct {
	APIVersion string                       `json:"apiVersion,omitempty"`
	Kind       string                       `json:"kind,omitempty"`
	Metadata   ObjectMeta                   `json:"metadata,omitempty"`
	Spec       RetainedProjectResourcesSpec `json:"spec,omitempty"`
	Status     ResourceStatus               `json:"status,omitempty"`
}

type ObjectMeta struct {
	Name              string            `json:"name,omitempty"`
	Namespace         string            `json:"namespace,omitempty"`
	Generation        int64             `json:"generation,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Finalizers        []string          `json:"finalizers,omitempty"`
	DeletionTimestamp string            `json:"deletionTimestamp,omitempty"`
}

type ProjectSpec struct {
	Ref                     string                  `json:"ref,omitempty"`
	DesiredState            string                  `json:"desiredState,omitempty"`
	Domain                  string                  `json:"domain,omitempty"`
	StackVersion            string                  `json:"stackVersion,omitempty"`
	Profile                 string                  `json:"profile,omitempty"`
	ResourceTier            string                  `json:"resourceTier,omitempty"`
	Environment             map[string]string       `json:"environment,omitempty"`
	Services                map[string]ServiceSpec  `json:"services,omitempty"`
	RuntimeSecurityDefaults RuntimeSecurityDefaults `json:"runtimeSecurityDefaults,omitempty"`
	RuntimeNamespace        string                  `json:"runtimeNamespace,omitempty"`
	RuntimeNetwork          *ProjectNetworkSpec     `json:"runtimeNetwork,omitempty"`
}

type ProjectNetworkSpec struct {
	AllowedEgressCIDRs  []string `json:"allowedEgressCidrs,omitempty"`
	ExternalEgressPorts []int    `json:"externalEgressPorts,omitempty"`
	DatabaseService     string   `json:"databaseService,omitempty"`
	DatabasePort        int      `json:"databasePort,omitempty"`
}

type ProjectReplicaSpec struct {
	ID                      string                  `json:"id,omitempty"`
	ProjectRef              string                  `json:"projectRef,omitempty"`
	Name                    string                  `json:"name,omitempty"`
	ResourceTier            string                  `json:"resourceTier,omitempty"`
	Region                  string                  `json:"region,omitempty"`
	HostID                  string                  `json:"hostId,omitempty"`
	ReadWeight              int                     `json:"readWeight,omitempty"`
	FailoverPriority        int                     `json:"failoverPriority,omitempty"`
	RuntimeSecurityDefaults RuntimeSecurityDefaults `json:"runtimeSecurityDefaults,omitempty"`
}

type ProjectConfigSpec struct {
	ProjectRef string            `json:"projectRef,omitempty"`
	Area       string            `json:"area,omitempty"`
	Config     map[string]string `json:"config,omitempty"`
}

type ProjectAuthHooksSpec struct {
	ProjectRef string                `json:"projectRef,omitempty"`
	Hooks      []ProjectAuthHookSpec `json:"hooks,omitempty"`
}

type ProjectAuthHookSpec struct {
	Type          string            `json:"type,omitempty"`
	Enabled       bool              `json:"enabled,omitempty"`
	Status        string            `json:"status,omitempty"`
	TargetURI     string            `json:"targetURI,omitempty"`
	EdgeFunction  string            `json:"edgeFunction,omitempty"`
	SecretHandle  string            `json:"secretHandle,omitempty"`
	TimeoutMS     int               `json:"timeoutMS,omitempty"`
	RetryAttempts int               `json:"retryAttempts,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
}

type ProjectBranchCloneSpec struct {
	SourceRef   string `json:"sourceRef,omitempty"`
	BranchRef   string `json:"branchRef,omitempty"`
	BranchID    string `json:"branchId,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
}

type RetainedProjectResourcesSpec struct {
	ProjectRef   string                       `json:"projectRef,omitempty"`
	RetainedAt   string                       `json:"retainedAt,omitempty"`
	Resources    []RetainedProjectResourceRef `json:"resources,omitempty"`
	Instructions []string                     `json:"instructions,omitempty"`
}

type RetainedProjectResourceRef struct {
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
}

type ServiceSpec struct {
	Enabled                  bool                    `json:"enabled,omitempty"`
	Config                   map[string]string       `json:"config,omitempty"`
	Image                    string                  `json:"image,omitempty"`
	Command                  []string                `json:"command,omitempty"`
	Args                     []string                `json:"args,omitempty"`
	Replicas                 *int32                  `json:"replicas,omitempty"`
	DependsOn                []ServiceDependencySpec `json:"dependsOn,omitempty"`
	Ports                    []ServicePortSpec       `json:"ports,omitempty"`
	ServiceType              string                  `json:"serviceType,omitempty"`
	Env                      map[string]string       `json:"env,omitempty"`
	Volumes                  []ServiceVolumeSpec     `json:"volumes,omitempty"`
	ConfigFiles              []ServiceConfigFileSpec `json:"configFiles,omitempty"`
	WritablePaths            []ServiceWritableSpec   `json:"writablePaths,omitempty"`
	RunAsNonRoot             *bool                   `json:"runAsNonRoot,omitempty"`
	AllowPrivilegeEscalation *bool                   `json:"allowPrivilegeEscalation,omitempty"`
	DropCapabilities         []string                `json:"dropCapabilities,omitempty"`
	ReadOnlyRootFilesystem   bool                    `json:"readOnlyRootFilesystem,omitempty"`
	ReadinessProbe           *ServiceProbeSpec       `json:"readinessProbe,omitempty"`
	LivenessProbe            *ServiceProbeSpec       `json:"livenessProbe,omitempty"`
	Ingress                  *ServiceIngressSpec     `json:"ingress,omitempty"`
}

type ServicePortSpec struct {
	Name       string `json:"name,omitempty"`
	Port       int    `json:"port,omitempty"`
	TargetPort int    `json:"targetPort,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
}

type ServiceDependencySpec struct {
	Service string `json:"service,omitempty"`
	Port    int    `json:"port,omitempty"`
}

type ServiceVolumeSpec struct {
	Name             string `json:"name,omitempty"`
	MountPath        string `json:"mountPath,omitempty"`
	Size             string `json:"size,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	Retain           bool   `json:"retain,omitempty"`
}

type ServiceConfigFileSpec struct {
	Name      string `json:"name,omitempty"`
	MountPath string `json:"mountPath,omitempty"`
	Content   string `json:"content,omitempty"`
}

type ServiceWritableSpec struct {
	Name      string `json:"name,omitempty"`
	MountPath string `json:"mountPath,omitempty"`
}

type ServiceProbeSpec struct {
	Type                string `json:"type,omitempty"`
	Path                string `json:"path,omitempty"`
	Port                int    `json:"port,omitempty"`
	InitialDelaySeconds int    `json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int    `json:"periodSeconds,omitempty"`
	TimeoutSeconds      int    `json:"timeoutSeconds,omitempty"`
	FailureThreshold    int    `json:"failureThreshold,omitempty"`
}

type ServiceIngressSpec struct {
	Enabled       bool              `json:"enabled,omitempty"`
	Host          string            `json:"host,omitempty"`
	Path          string            `json:"path,omitempty"`
	ClassName     string            `json:"className,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	TLSSecretName string            `json:"tlsSecretName,omitempty"`
}

type RuntimeSecurityDefaults struct {
	SeccompProfile           string   `json:"seccompProfile,omitempty"`
	AllowPrivilegeEscalation *bool    `json:"allowPrivilegeEscalation,omitempty"`
	DropCapabilities         []string `json:"dropCapabilities,omitempty"`
}

type ProjectStatus struct {
	ObservedGeneration      int64                   `json:"observedGeneration,omitempty"`
	Phase                   string                  `json:"phase,omitempty"`
	Message                 string                  `json:"message,omitempty"`
	RuntimeSecurityDefaults RuntimeSecurityDefaults `json:"runtimeSecurityDefaults,omitempty"`
	Conditions              []Condition             `json:"conditions,omitempty"`
	LastReconciledAt        string                  `json:"lastReconciledAt,omitempty"`
}

type ReplicaStatus struct {
	ObservedGeneration      int64                   `json:"observedGeneration,omitempty"`
	Phase                   string                  `json:"phase,omitempty"`
	Message                 string                  `json:"message,omitempty"`
	RuntimeSecurityDefaults RuntimeSecurityDefaults `json:"runtimeSecurityDefaults,omitempty"`
	Conditions              []Condition             `json:"conditions,omitempty"`
	LastReconciledAt        string                  `json:"lastReconciledAt,omitempty"`
}

type ResourceStatus = ReplicaStatus

type Condition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

type ProjectResources struct {
	ConfigMapName string `json:"configMapName"`
	SecretName    string `json:"secretName"`
	// ServiceAccountName, when set, binds every workload pod to the dedicated
	// tokenless per-project ServiceAccount (isolation mode). Empty in legacy
	// single-namespace mode so pods fall back to the namespace default SA.
	ServiceAccountName string             `json:"serviceAccountName,omitempty"`
	Labels             map[string]string  `json:"labels"`
	ConfigData         map[string]string  `json:"configData"`
	SecretData         map[string]string  `json:"secretData"`
	Workloads          []ServiceResources `json:"workloads,omitempty"`
}

type ProjectResourceObservation struct {
	Checked bool
	Ready   bool
	Message string
}

type ServiceResources struct {
	ServiceName     string
	DeploymentName  string
	KubeServiceName string
	IngressName     string
	Labels          map[string]string
	Spec            ServiceSpec
	Replicas        int32
	Dependencies    []ServiceDependencySpec
	Ports           []ServicePortSpec
	Volumes         []ServiceVolumeSpec
	ConfigFiles     []ServiceConfigFileResources
	WritablePaths   []ServiceWritableSpec
	Ingress         *ServiceIngressSpec
}

type ServiceConfigFileResources struct {
	Name      string
	MountPath string
	Content   string
	DataKey   string
}

func (r Reconciler) ReconcileNamespace(ctx context.Context, namespace string) error {
	if r.Client == nil {
		return fmt.Errorf("operator client is required")
	}
	var combined error
	projects, err := r.Client.ListProjects(ctx, namespace)
	if err != nil {
		combined = appendError(combined, err)
	} else {
		for _, project := range projects {
			if err := r.ReconcileProject(ctx, namespace, project); err != nil {
				combined = appendError(combined, err)
			}
		}
	}

	configs, err := r.Client.ListProjectConfigs(ctx, namespace)
	if err != nil {
		combined = appendError(combined, err)
	} else {
		for _, config := range configs {
			if err := r.ReconcileProjectConfig(ctx, namespace, config); err != nil {
				combined = appendError(combined, err)
			}
		}
	}

	authHooks, err := r.Client.ListProjectAuthHooks(ctx, namespace)
	if err != nil {
		combined = appendError(combined, err)
	} else {
		for _, hooks := range authHooks {
			if err := r.ReconcileProjectAuthHooks(ctx, namespace, hooks); err != nil {
				combined = appendError(combined, err)
			}
		}
	}

	branchClones, err := r.Client.ListProjectBranchClones(ctx, namespace)
	if err != nil {
		combined = appendError(combined, err)
	} else {
		for _, clone := range branchClones {
			if err := r.ReconcileProjectBranchClone(ctx, namespace, clone); err != nil {
				combined = appendError(combined, err)
			}
		}
	}

	replicas, err := r.Client.ListProjectReplicas(ctx, namespace)
	if err != nil {
		combined = appendError(combined, err)
	} else {
		for _, replica := range replicas {
			if err := r.ReconcileProjectReplica(ctx, namespace, replica); err != nil {
				combined = appendError(combined, err)
			}
		}
	}

	retainedResources, err := r.Client.ListRetainedProjectResources(ctx, namespace)
	if err != nil {
		combined = appendError(combined, err)
	} else {
		for _, retained := range retainedResources {
			if err := r.ReconcileRetainedProjectResources(ctx, namespace, retained); err != nil {
				combined = appendError(combined, err)
			}
		}
	}
	return combined
}

func (r Reconciler) ReconcileProject(ctx context.Context, namespace string, project Project) error {
	name := strings.TrimSpace(project.Metadata.Name)
	if name == "" {
		return fmt.Errorf("project metadata.name is required")
	}
	if namespace == "" {
		namespace = strings.TrimSpace(project.Metadata.Namespace)
	}
	if namespace == "" {
		return fmt.Errorf("namespace is required for project %s", name)
	}
	// controlNS holds the Project CR + status; runtimeNS holds workload objects.
	controlNS := namespace
	runtimeNS := r.runtimeNamespace(project, controlNS)
	ownsNamespace := r.isolationOwnsNamespace(project, controlNS)
	now := r.now()
	desired := normalizedDesiredState(project.Spec.DesiredState)

	// Finalizer-driven teardown: a direct deletion of the Project CR (kubectl
	// delete, GitOps prune, namespace delete) sets metadata.deletionTimestamp.
	// Run the same teardown as the destroying branch and then drop the finalizer
	// so the API server can garbage-collect the CR. This makes runtime-namespace
	// cleanup robust to deletions that bypass the provisioner-driven flow.
	if strings.TrimSpace(project.Metadata.DeletionTimestamp) != "" {
		if !hasFinalizer(project.Metadata.Finalizers, projectFinalizer) {
			return nil
		}
		resources, resErr := resourcesForProject(project)
		if resErr == nil {
			if err := r.Client.DeleteProjectResources(ctx, runtimeNS, project, resources); err != nil {
				return err
			}
		}
		if ownsNamespace {
			if err := r.Client.DeleteNamespace(ctx, runtimeNS); err != nil {
				return err
			}
		}
		return r.Client.SetProjectFinalizers(ctx, controlNS, name, removeFinalizer(project.Metadata.Finalizers, projectFinalizer))
	}

	resources, err := resourcesForProject(project)
	if err != nil {
		status := degradedStatusForProject(project, now, "InvalidProjectSpec", err.Error())
		if patchErr := r.Client.PatchProjectStatus(ctx, controlNS, name, status); patchErr != nil {
			return appendError(err, patchErr)
		}
		return err
	}
	if desired == "destroying" {
		if err := r.Client.DeleteProjectResources(ctx, runtimeNS, project, resources); err != nil {
			status := degradedStatusForProject(project, now, "ResourceDeleteFailed", err.Error())
			if patchErr := r.Client.PatchProjectStatus(ctx, controlNS, name, status); patchErr != nil {
				return appendError(err, patchErr)
			}
			return err
		}
		if ownsNamespace {
			if err := r.Client.DeleteNamespace(ctx, runtimeNS); err != nil {
				status := degradedStatusForProject(project, now, "NamespaceDeleteFailed", err.Error())
				if patchErr := r.Client.PatchProjectStatus(ctx, controlNS, name, status); patchErr != nil {
					return appendError(err, patchErr)
				}
				return err
			}
		}
	} else if desired == "running" || desired == "paused" {
		// Ensure the runtime-namespace finalizer is present on live projects so a
		// later direct CR deletion still triggers operator teardown.
		if ownsNamespace && !hasFinalizer(project.Metadata.Finalizers, projectFinalizer) {
			if err := r.Client.SetProjectFinalizers(ctx, controlNS, name, append(append([]string{}, project.Metadata.Finalizers...), projectFinalizer)); err != nil {
				status := degradedStatusForProject(project, now, "FinalizerPatchFailed", err.Error())
				if patchErr := r.Client.PatchProjectStatus(ctx, controlNS, name, status); patchErr != nil {
					return appendError(err, patchErr)
				}
				return err
			}
		}
		if ownsNamespace {
			if err := r.Client.EnsureNamespace(ctx, runtimeNS, namespaceLabels(project, r.podSecurityLevels())); err != nil {
				status := degradedStatusForProject(project, now, "NamespaceProvisionFailed", err.Error())
				if patchErr := r.Client.PatchProjectStatus(ctx, controlNS, name, status); patchErr != nil {
					return appendError(err, patchErr)
				}
				return err
			}
			iso := isolationForProject(project, kubernetesName(projectRef(project)), r.isolationConfig())
			if err := r.Client.ApplyProjectIsolation(ctx, runtimeNS, project, iso); err != nil {
				status := degradedStatusForProject(project, now, "IsolationApplyFailed", err.Error())
				if patchErr := r.Client.PatchProjectStatus(ctx, controlNS, name, status); patchErr != nil {
					return appendError(err, patchErr)
				}
				return err
			}
			// Bind workload pods to the dedicated tokenless per-project SA.
			resources.ServiceAccountName = iso.ServiceAccountName
		}
		var observation ProjectResourceObservation
		if err := r.Client.ApplyProjectResources(ctx, runtimeNS, project, resources); err != nil {
			status := degradedStatusForProject(project, now, "ResourceApplyFailed", err.Error())
			if patchErr := r.Client.PatchProjectStatus(ctx, controlNS, name, status); patchErr != nil {
				return appendError(err, patchErr)
			}
			return err
		}
		if err := r.Client.PruneProjectResources(ctx, runtimeNS, project, resources); err != nil {
			status := degradedStatusForProject(project, now, "ResourcePruneFailed", err.Error())
			if patchErr := r.Client.PatchProjectStatus(ctx, controlNS, name, status); patchErr != nil {
				return appendError(err, patchErr)
			}
			return err
		}
		if desired == "running" {
			observation, err = r.Client.ObserveProjectResources(ctx, runtimeNS, resources)
			if err != nil {
				status := degradedStatusForProject(project, now, "ResourceObserveFailed", err.Error())
				if patchErr := r.Client.PatchProjectStatus(ctx, controlNS, name, status); patchErr != nil {
					return appendError(err, patchErr)
				}
				return err
			}
		}
		status := statusForProject(project, now, resources, observation)
		return r.Client.PatchProjectStatus(ctx, controlNS, name, status)
	}
	status := statusForProject(project, now, resources, ProjectResourceObservation{})
	return r.Client.PatchProjectStatus(ctx, controlNS, name, status)
}

func projectRef(project Project) string {
	ref := strings.TrimSpace(project.Spec.Ref)
	if ref == "" {
		ref = strings.TrimSpace(project.Metadata.Name)
	}
	return ref
}

func (r Reconciler) ReconcileProjectReplica(ctx context.Context, namespace string, replica ProjectReplica) error {
	name := strings.TrimSpace(replica.Metadata.Name)
	if name == "" {
		return fmt.Errorf("projectreplica metadata.name is required")
	}
	if namespace == "" {
		namespace = strings.TrimSpace(replica.Metadata.Namespace)
	}
	if namespace == "" {
		return fmt.Errorf("namespace is required for projectreplica %s", name)
	}
	now := r.now()
	if err := validateProjectReplicaSpec(replica.Spec); err != nil {
		status := degradedStatusForReplica(replica, now, "InvalidReplicaSpec", err.Error())
		if patchErr := r.Client.PatchProjectReplicaStatus(ctx, namespace, name, status); patchErr != nil {
			return appendError(err, patchErr)
		}
		return err
	}
	return r.Client.PatchProjectReplicaStatus(ctx, namespace, name, statusForReplica(replica, now))
}

func (r Reconciler) ReconcileProjectConfig(ctx context.Context, namespace string, config ProjectConfig) error {
	return r.reconcileObservedResource(ctx, namespace, "ProjectConfig", config.Metadata, func() error {
		if strings.TrimSpace(config.Spec.ProjectRef) == "" {
			return fmt.Errorf("spec.projectRef is required")
		}
		if strings.TrimSpace(config.Spec.Area) == "" {
			return fmt.Errorf("spec.area is required")
		}
		return nil
	}, func(ctx context.Context, namespace string, name string, status ResourceStatus) error {
		return r.Client.PatchProjectConfigStatus(ctx, namespace, name, status)
	}, observedResourceMessage("ProjectConfig", config.Spec.ProjectRef, config.Spec.Area))
}

func (r Reconciler) ReconcileProjectAuthHooks(ctx context.Context, namespace string, hooks ProjectAuthHooks) error {
	return r.reconcileObservedResource(ctx, namespace, "ProjectAuthHooks", hooks.Metadata, func() error {
		if strings.TrimSpace(hooks.Spec.ProjectRef) == "" {
			return fmt.Errorf("spec.projectRef is required")
		}
		return nil
	}, func(ctx context.Context, namespace string, name string, status ResourceStatus) error {
		return r.Client.PatchProjectAuthHooksStatus(ctx, namespace, name, status)
	}, observedResourceMessage("ProjectAuthHooks", hooks.Spec.ProjectRef, fmt.Sprintf("%d hook(s)", len(hooks.Spec.Hooks))))
}

func (r Reconciler) ReconcileProjectBranchClone(ctx context.Context, namespace string, clone ProjectBranchClone) error {
	return r.reconcileObservedResource(ctx, namespace, "ProjectBranchClone", clone.Metadata, func() error {
		if strings.TrimSpace(clone.Spec.SourceRef) == "" {
			return fmt.Errorf("spec.sourceRef is required")
		}
		if strings.TrimSpace(clone.Spec.BranchRef) == "" {
			return fmt.Errorf("spec.branchRef is required")
		}
		return nil
	}, func(ctx context.Context, namespace string, name string, status ResourceStatus) error {
		return r.Client.PatchProjectBranchCloneStatus(ctx, namespace, name, status)
	}, observedResourceMessage("ProjectBranchClone", clone.Spec.SourceRef, clone.Spec.BranchRef))
}

func (r Reconciler) ReconcileRetainedProjectResources(ctx context.Context, namespace string, retained RetainedProjectResources) error {
	return r.reconcileObservedResource(ctx, namespace, "RetainedProjectResources", retained.Metadata, func() error {
		if strings.TrimSpace(retained.Spec.ProjectRef) == "" {
			return fmt.Errorf("spec.projectRef is required")
		}
		if strings.TrimSpace(retained.Spec.RetainedAt) == "" {
			return fmt.Errorf("spec.retainedAt is required")
		}
		return nil
	}, func(ctx context.Context, namespace string, name string, status ResourceStatus) error {
		return r.Client.PatchRetainedProjectResourcesStatus(ctx, namespace, name, status)
	}, observedResourceMessage("RetainedProjectResources", retained.Spec.ProjectRef, fmt.Sprintf("%d retained resource(s)", len(retained.Spec.Resources))))
}

func (r Reconciler) reconcileObservedResource(ctx context.Context, namespace string, kind string, metadata ObjectMeta, validate func() error, patch func(context.Context, string, string, ResourceStatus) error, detail string) error {
	name := strings.TrimSpace(metadata.Name)
	if name == "" {
		return fmt.Errorf("%s metadata.name is required", strings.ToLower(kind))
	}
	if namespace == "" {
		namespace = strings.TrimSpace(metadata.Namespace)
	}
	if namespace == "" {
		return fmt.Errorf("namespace is required for %s %s", strings.ToLower(kind), name)
	}
	now := r.now()
	if err := validate(); err != nil {
		status := degradedStatusForObservedResource(kind, metadata.Generation, now, "Invalid"+kind+"Spec", err.Error())
		if patchErr := patch(ctx, namespace, name, status); patchErr != nil {
			return appendError(err, patchErr)
		}
		return err
	}
	return patch(ctx, namespace, name, statusForObservedResource(kind, metadata.Generation, now, detail))
}

func statusForProject(project Project, now time.Time, resources ProjectResources, observation ProjectResourceObservation) ProjectStatus {
	desired := normalizedDesiredState(project.Spec.DesiredState)
	phase := "RuntimeRendered"
	reason := "RuntimeResourcesRendered"
	message := "project runtime configuration resources rendered; workload reconciliation is pending"
	ready := "False"
	workloadsRendered := len(resources.Workloads) > 0
	workloadsAvailable := false
	workloadsAvailableReason := "NoWorkloadsRendered"
	workloadsAvailableMessage := "no enabled services declare workload images"
	switch desired {
	case "running":
		if workloadsRendered {
			workloadsAvailableReason = "RuntimeWorkloadsObserved"
			workloadsAvailableMessage = observation.Message
			if workloadsAvailableMessage == "" {
				workloadsAvailableMessage = "project workload availability has not been observed yet"
			}
			if observation.Checked && observation.Ready {
				phase = "RuntimeReady"
				reason = "RuntimeWorkloadsAvailable"
				message = fmt.Sprintf("project runtime resources and %d workload(s) are available", len(resources.Workloads))
				ready = "True"
				workloadsAvailable = true
			} else {
				phase = "RuntimePending"
				reason = "RuntimeWorkloadsPending"
				message = workloadsAvailableMessage
			}
		}
	case "paused":
		phase = "Paused"
		reason = "DesiredStatePaused"
		if workloadsRendered {
			message = fmt.Sprintf("project is paused by desired state; %d workload(s) scaled to zero", len(resources.Workloads))
		} else {
			message = "project is paused by desired state"
		}
	case "destroying":
		phase = "Terminating"
		reason = "RuntimeResourcesDeleted"
		message = "project runtime configuration resources removed"
	default:
		phase = "Degraded"
		reason = "UnsupportedDesiredState"
		message = fmt.Sprintf("unsupported desiredState %q", project.Spec.DesiredState)
	}
	timestamp := now.UTC().Format(time.RFC3339)
	return ProjectStatus{
		ObservedGeneration:      project.Metadata.Generation,
		Phase:                   phase,
		Message:                 message,
		RuntimeSecurityDefaults: project.Spec.RuntimeSecurityDefaults,
		LastReconciledAt:        timestamp,
		Conditions: []Condition{
			{
				Type:               "ResourcesRendered",
				Status:             conditionStatus(desired == "running" || desired == "paused"),
				Reason:             reason,
				Message:            resourcesMessage(desired, resources),
				ObservedGeneration: project.Metadata.Generation,
				LastTransitionTime: timestamp,
			},
			{
				Type:               "WorkloadsRendered",
				Status:             conditionStatus((desired == "running" || desired == "paused") && workloadsRendered),
				Reason:             reason,
				Message:            workloadsMessage(desired, resources),
				ObservedGeneration: project.Metadata.Generation,
				LastTransitionTime: timestamp,
			},
			{
				Type:               "WorkloadsAvailable",
				Status:             conditionStatus(desired == "running" && workloadsAvailable),
				Reason:             workloadsAvailableReason,
				Message:            workloadsAvailableMessage,
				ObservedGeneration: project.Metadata.Generation,
				LastTransitionTime: timestamp,
			},
			{
				Type:               "Ready",
				Status:             ready,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: project.Metadata.Generation,
				LastTransitionTime: timestamp,
			},
		},
	}
}

func degradedStatusForProject(project Project, now time.Time, reason string, message string) ProjectStatus {
	timestamp := now.UTC().Format(time.RFC3339)
	return ProjectStatus{
		ObservedGeneration:      project.Metadata.Generation,
		Phase:                   "Degraded",
		Message:                 message,
		RuntimeSecurityDefaults: project.Spec.RuntimeSecurityDefaults,
		LastReconciledAt:        timestamp,
		Conditions: []Condition{
			{
				Type:               "Ready",
				Status:             "False",
				Reason:             reason,
				Message:            message,
				ObservedGeneration: project.Metadata.Generation,
				LastTransitionTime: timestamp,
			},
		},
	}
}

func statusForReplica(replica ProjectReplica, now time.Time) ReplicaStatus {
	timestamp := now.UTC().Format(time.RFC3339)
	message := "project replica desired state observed; replica data-plane reconciliation is pending"
	return ReplicaStatus{
		ObservedGeneration:      replica.Metadata.Generation,
		Phase:                   "ReplicaPending",
		Message:                 message,
		RuntimeSecurityDefaults: replica.Spec.RuntimeSecurityDefaults,
		LastReconciledAt:        timestamp,
		Conditions: []Condition{
			{
				Type:               "ReplicaObserved",
				Status:             "True",
				Reason:             "ReplicaSpecObserved",
				Message:            fmt.Sprintf("observed replica %s for project %s", replica.Spec.Name, replica.Spec.ProjectRef),
				ObservedGeneration: replica.Metadata.Generation,
				LastTransitionTime: timestamp,
			},
			{
				Type:               "Ready",
				Status:             "False",
				Reason:             "ReplicaDataPlanePending",
				Message:            message,
				ObservedGeneration: replica.Metadata.Generation,
				LastTransitionTime: timestamp,
			},
		},
	}
}

func degradedStatusForReplica(replica ProjectReplica, now time.Time, reason string, message string) ReplicaStatus {
	timestamp := now.UTC().Format(time.RFC3339)
	return ReplicaStatus{
		ObservedGeneration:      replica.Metadata.Generation,
		Phase:                   "Degraded",
		Message:                 message,
		RuntimeSecurityDefaults: replica.Spec.RuntimeSecurityDefaults,
		LastReconciledAt:        timestamp,
		Conditions: []Condition{
			{
				Type:               "Ready",
				Status:             "False",
				Reason:             reason,
				Message:            message,
				ObservedGeneration: replica.Metadata.Generation,
				LastTransitionTime: timestamp,
			},
		},
	}
}

func statusForObservedResource(kind string, generation int64, now time.Time, detail string) ResourceStatus {
	timestamp := now.UTC().Format(time.RFC3339)
	message := strings.TrimSpace(detail)
	if message == "" {
		message = fmt.Sprintf("%s desired state observed", kind)
	}
	return ResourceStatus{
		ObservedGeneration: generation,
		Phase:              "Observed",
		Message:            message,
		LastReconciledAt:   timestamp,
		Conditions: []Condition{
			{
				Type:               kind + "Observed",
				Status:             "True",
				Reason:             kind + "SpecObserved",
				Message:            message,
				ObservedGeneration: generation,
				LastTransitionTime: timestamp,
			},
			{
				Type:               "Ready",
				Status:             "False",
				Reason:             kind + "DataPlanePending",
				Message:            fmt.Sprintf("%s data-plane reconciliation is pending", kind),
				ObservedGeneration: generation,
				LastTransitionTime: timestamp,
			},
		},
	}
}

func degradedStatusForObservedResource(kind string, generation int64, now time.Time, reason string, message string) ResourceStatus {
	timestamp := now.UTC().Format(time.RFC3339)
	return ResourceStatus{
		ObservedGeneration: generation,
		Phase:              "Degraded",
		Message:            message,
		LastReconciledAt:   timestamp,
		Conditions: []Condition{
			{
				Type:               "Ready",
				Status:             "False",
				Reason:             reason,
				Message:            message,
				ObservedGeneration: generation,
				LastTransitionTime: timestamp,
			},
		},
	}
}

func observedResourceMessage(kind string, projectRef string, detail string) string {
	parts := []string{kind + " desired state observed"}
	if projectRef = strings.TrimSpace(projectRef); projectRef != "" {
		parts = append(parts, "project="+projectRef)
	}
	if detail = strings.TrimSpace(detail); detail != "" {
		parts = append(parts, "detail="+detail)
	}
	return strings.Join(parts, "; ")
}

func validateProjectReplicaSpec(spec ProjectReplicaSpec) error {
	if strings.TrimSpace(spec.ID) == "" {
		return fmt.Errorf("spec.id is required")
	}
	if strings.TrimSpace(spec.ProjectRef) == "" {
		return fmt.Errorf("spec.projectRef is required")
	}
	if strings.TrimSpace(spec.Name) == "" {
		return fmt.Errorf("spec.name is required")
	}
	return nil
}

func resourcesForProject(project Project) (ProjectResources, error) {
	ref := strings.TrimSpace(project.Spec.Ref)
	if ref == "" {
		ref = strings.TrimSpace(project.Metadata.Name)
	}
	if ref == "" {
		return ProjectResources{}, fmt.Errorf("project ref is required")
	}
	name := kubernetesName(ref)
	services, err := json.Marshal(project.Spec.Services)
	if err != nil {
		return ProjectResources{}, fmt.Errorf("encode project services: %w", err)
	}
	runtimeSecurityDefaults, err := json.Marshal(project.Spec.RuntimeSecurityDefaults)
	if err != nil {
		return ProjectResources{}, fmt.Errorf("encode runtime security defaults: %w", err)
	}
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "supadupa-operator",
		"supadupa.dev/project-ref":     ref,
	}
	workloads := workloadsForProject(project, labels, name)
	configData := map[string]string{
		"ref":                     ref,
		"domain":                  strings.TrimSpace(project.Spec.Domain),
		"stackVersion":            strings.TrimSpace(project.Spec.StackVersion),
		"profile":                 strings.TrimSpace(project.Spec.Profile),
		"resourceTier":            strings.TrimSpace(project.Spec.ResourceTier),
		"desiredState":            normalizedDesiredState(project.Spec.DesiredState),
		"services.json":           string(services),
		"runtimeSecurityDefaults": string(runtimeSecurityDefaults),
	}
	for _, workload := range workloads {
		for _, file := range workload.ConfigFiles {
			configData[file.DataKey] = file.Content
		}
	}
	return ProjectResources{
		ConfigMapName: name + "-runtime",
		SecretName:    name + "-environment",
		Labels:        labels,
		ConfigData:    configData,
		SecretData:    copyStringMap(project.Spec.Environment),
		Workloads:     workloads,
	}, nil
}

func normalizedDesiredState(input string) string {
	desired := strings.ToLower(strings.TrimSpace(input))
	if desired == "" {
		return "running"
	}
	return desired
}

func resourcesMessage(desired string, resources ProjectResources) string {
	if desired == "destroying" {
		return "project runtime configuration resources removed"
	}
	if desired != "running" && desired != "paused" {
		return "project runtime configuration resources were not rendered"
	}
	if len(resources.Workloads) == 0 {
		return fmt.Sprintf("rendered ConfigMap %s and Secret %s", resources.ConfigMapName, resources.SecretName)
	}
	return fmt.Sprintf("rendered ConfigMap %s, Secret %s, and %d workload(s)", resources.ConfigMapName, resources.SecretName, len(resources.Workloads))
}

func workloadsMessage(desired string, resources ProjectResources) string {
	if desired == "destroying" {
		return "project workload resources removed"
	}
	if desired != "running" && desired != "paused" {
		return "project workload resources were not rendered"
	}
	if len(resources.Workloads) == 0 {
		return "no enabled services declare workload images"
	}
	return fmt.Sprintf("rendered %d service workload(s)", len(resources.Workloads))
}

func conditionStatus(ok bool) string {
	if ok {
		return "True"
	}
	return "False"
}

func kubernetesName(input string) string {
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
	out = strings.Trim(builder.String(), "-")
	if out == "" {
		return "project"
	}
	if len(out) > 51 {
		out = strings.TrimRight(out[:51], "-")
	}
	return out
}

func kubernetesJoinedName(parts ...string) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := kubernetesName(part); name != "" {
			segments = append(segments, name)
		}
	}
	out := strings.Join(segments, "-")
	if out == "" {
		return "resource"
	}
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	if out == "" {
		return "resource"
	}
	return out
}

func workloadsForProject(project Project, projectLabels map[string]string, baseName string) []ServiceResources {
	desired := normalizedDesiredState(project.Spec.DesiredState)
	keys := make([]string, 0, len(project.Spec.Services))
	for key := range project.Spec.Services {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	workloads := make([]ServiceResources, 0, len(keys))
	for _, key := range keys {
		service := project.Spec.Services[key]
		if !service.Enabled {
			continue
		}
		image := serviceImage(service)
		if image == "" {
			continue
		}
		name := kubernetesName(key)
		labels := copyStringMap(projectLabels)
		labels["app.kubernetes.io/component"] = name
		labels["supadupa.dev/project-service"] = name
		replicas := serviceReplicas(service)
		if desired == "paused" {
			replicas = 0
		}
		workloads = append(workloads, ServiceResources{
			ServiceName:     name,
			DeploymentName:  kubernetesJoinedName(baseName, name),
			KubeServiceName: kubernetesJoinedName(baseName, name),
			IngressName:     kubernetesJoinedName(baseName, name, "ingress"),
			Labels:          labels,
			Spec:            service,
			Replicas:        replicas,
			Dependencies:    serviceDependencies(service),
			Ports:           servicePorts(service),
			Volumes:         serviceVolumes(service),
			ConfigFiles:     serviceConfigFiles(name, service),
			WritablePaths:   serviceWritablePaths(service),
			Ingress:         serviceIngress(service),
		})
	}
	return workloads
}

func serviceImage(service ServiceSpec) string {
	if image := strings.TrimSpace(service.Image); image != "" {
		return image
	}
	return strings.TrimSpace(service.Config["image"])
}

func serviceReplicas(service ServiceSpec) int32 {
	if service.Replicas != nil {
		if *service.Replicas < 0 {
			return 0
		}
		return *service.Replicas
	}
	if replicas := strings.TrimSpace(service.Config["replicas"]); replicas != "" {
		parsed, err := strconv.Atoi(replicas)
		if err == nil && parsed >= 0 {
			return int32(parsed)
		}
	}
	return 1
}

func servicePorts(service ServiceSpec) []ServicePortSpec {
	if len(service.Ports) > 0 {
		return normalizePorts(service.Ports)
	}
	if port := strings.TrimSpace(service.Config["port"]); port != "" {
		parsed, err := strconv.Atoi(port)
		if err == nil && parsed > 0 && parsed <= 65535 {
			return []ServicePortSpec{{Name: "http", Port: parsed, TargetPort: parsed, Protocol: "TCP"}}
		}
	}
	return nil
}

func serviceDependencies(service ServiceSpec) []ServiceDependencySpec {
	input := append([]ServiceDependencySpec(nil), service.DependsOn...)
	if configured := strings.TrimSpace(service.Config["dependsOn"]); configured != "" && !strings.EqualFold(strings.TrimSpace(service.Config["dependsOnEnabled"]), "false") {
		input = nil
		for _, part := range strings.Split(configured, ",") {
			serviceName, portText, ok := strings.Cut(strings.TrimSpace(part), ":")
			if !ok {
				continue
			}
			port, err := strconv.Atoi(strings.TrimSpace(portText))
			if err != nil {
				continue
			}
			input = append(input, ServiceDependencySpec{Service: strings.TrimSpace(serviceName), Port: port})
		}
	}
	if strings.EqualFold(strings.TrimSpace(service.Config["dependsOnEnabled"]), "false") {
		return nil
	}
	out := make([]ServiceDependencySpec, 0, len(input))
	seen := map[string]struct{}{}
	for _, dependency := range input {
		dependency.Service = kubernetesName(dependency.Service)
		if dependency.Service == "" || dependency.Port <= 0 || dependency.Port > 65535 {
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

func normalizePorts(input []ServicePortSpec) []ServicePortSpec {
	out := make([]ServicePortSpec, 0, len(input))
	for index, port := range input {
		if port.Port <= 0 || port.Port > 65535 {
			continue
		}
		if port.TargetPort <= 0 {
			port.TargetPort = port.Port
		}
		if port.Protocol == "" {
			port.Protocol = "TCP"
		}
		if port.Name == "" {
			port.Name = fmt.Sprintf("port-%d", index+1)
		}
		port.Name = kubernetesName(port.Name)
		out = append(out, port)
	}
	return out
}

func serviceVolumes(service ServiceSpec) []ServiceVolumeSpec {
	if len(service.Volumes) > 0 {
		return normalizeVolumes(service.Volumes)
	}
	size := strings.TrimSpace(service.Config["storageSize"])
	mountPath := strings.TrimSpace(service.Config["storageMountPath"])
	if size == "" || mountPath == "" {
		return nil
	}
	return normalizeVolumes([]ServiceVolumeSpec{{
		Name:             strings.TrimSpace(service.Config["storageName"]),
		MountPath:        mountPath,
		Size:             size,
		StorageClassName: strings.TrimSpace(service.Config["storageClassName"]),
		Retain:           strings.EqualFold(strings.TrimSpace(service.Config["retainStorage"]), "true"),
	}})
}

func normalizeVolumes(input []ServiceVolumeSpec) []ServiceVolumeSpec {
	out := make([]ServiceVolumeSpec, 0, len(input))
	for index, volume := range input {
		volume.Name = kubernetesName(volume.Name)
		if volume.Name == "" || volume.Name == "project" {
			volume.Name = fmt.Sprintf("data-%d", index+1)
		}
		volume.MountPath = strings.TrimSpace(volume.MountPath)
		volume.Size = strings.TrimSpace(volume.Size)
		volume.StorageClassName = strings.TrimSpace(volume.StorageClassName)
		if volume.MountPath == "" || volume.Size == "" {
			continue
		}
		out = append(out, volume)
	}
	return out
}

func serviceConfigFiles(serviceName string, service ServiceSpec) []ServiceConfigFileResources {
	out := make([]ServiceConfigFileResources, 0, len(service.ConfigFiles))
	for index, file := range service.ConfigFiles {
		name := kubernetesName(file.Name)
		if name == "" || name == "project" {
			name = fmt.Sprintf("config-%d", index+1)
		}
		mountPath := strings.TrimSpace(file.MountPath)
		if mountPath == "" {
			continue
		}
		out = append(out, ServiceConfigFileResources{
			Name:      name,
			MountPath: mountPath,
			Content:   file.Content,
			DataKey:   kubernetesJoinedName("service", serviceName, name),
		})
	}
	return out
}

func serviceWritablePaths(service ServiceSpec) []ServiceWritableSpec {
	input := append([]ServiceWritableSpec(nil), service.WritablePaths...)
	if configured := strings.TrimSpace(service.Config["writablePaths"]); configured != "" && !strings.EqualFold(strings.TrimSpace(service.Config["writablePathsEnabled"]), "false") {
		input = nil
		for index, part := range strings.Split(configured, ",") {
			path := strings.TrimSpace(part)
			if path == "" {
				continue
			}
			input = append(input, ServiceWritableSpec{Name: fmt.Sprintf("writable-%d", index+1), MountPath: path})
		}
	}
	if strings.EqualFold(strings.TrimSpace(service.Config["writablePathsEnabled"]), "false") {
		return nil
	}
	out := make([]ServiceWritableSpec, 0, len(input))
	for index, writable := range input {
		writable.Name = kubernetesName(writable.Name)
		if writable.Name == "" || writable.Name == "project" {
			writable.Name = fmt.Sprintf("writable-%d", index+1)
		}
		writable.MountPath = strings.TrimSpace(writable.MountPath)
		if writable.MountPath == "" {
			continue
		}
		out = append(out, writable)
	}
	return out
}

func serviceIngress(service ServiceSpec) *ServiceIngressSpec {
	if service.Ingress != nil {
		ingress := *service.Ingress
		ingress.Host = strings.TrimSpace(ingress.Host)
		ingress.Path = strings.TrimSpace(ingress.Path)
		if ingress.Path == "" {
			ingress.Path = "/"
		}
		ingress.ClassName = strings.TrimSpace(ingress.ClassName)
		ingress.TLSSecretName = strings.TrimSpace(ingress.TLSSecretName)
		ingress.Annotations = copyStringMap(ingress.Annotations)
		if ingress.Enabled && ingress.Host != "" {
			return &ingress
		}
		return nil
	}
	host := strings.TrimSpace(service.Config["ingressHost"])
	if host == "" {
		return nil
	}
	return &ServiceIngressSpec{
		Enabled:       true,
		Host:          host,
		Path:          strings.TrimSpace(service.Config["ingressPath"]),
		ClassName:     strings.TrimSpace(service.Config["ingressClassName"]),
		TLSSecretName: strings.TrimSpace(service.Config["ingressTLSSecretName"]),
	}
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

func (r Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func hasFinalizer(finalizers []string, target string) bool {
	for _, f := range finalizers {
		if f == target {
			return true
		}
	}
	return false
}

func removeFinalizer(finalizers []string, target string) []string {
	out := make([]string, 0, len(finalizers))
	for _, f := range finalizers {
		if f == target {
			continue
		}
		out = append(out, f)
	}
	return out
}

func appendError(existing error, next error) error {
	if next == nil {
		return existing
	}
	if existing == nil {
		return next
	}
	return fmt.Errorf("%v; %w", existing, next)
}
