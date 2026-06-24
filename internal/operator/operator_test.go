package operator

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReconcileProjectAppliesResourcesAndPatchesStatus(t *testing.T) {
	allowPrivilegeEscalation := false
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return now }}

	err := reconciler.ReconcileProject(context.Background(), "supadupa", Project{
		Metadata: ObjectMeta{Name: "alpha", Generation: 7},
		Spec: ProjectSpec{
			Ref:          "alpha",
			DesiredState: "running",
			Domain:       "apps.example.com",
			Environment:  map[string]string{"POSTGRES_PASSWORD": "secret"},
			Services:     map[string]ServiceSpec{"auth": {Enabled: true}},
			RuntimeSecurityDefaults: RuntimeSecurityDefaults{
				SeccompProfile:           "RuntimeDefault",
				AllowPrivilegeEscalation: &allowPrivilegeEscalation,
				DropCapabilities:         []string{"ALL"},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if client.patchNamespace != "supadupa" || client.patchName != "alpha" {
		t.Fatalf("unexpected patch target %s/%s", client.patchNamespace, client.patchName)
	}
	if client.applyNamespace != "supadupa" || client.applyResources.ConfigMapName != "alpha-runtime" || client.applyResources.SecretName != "alpha-environment" {
		t.Fatalf("unexpected applied resources: namespace=%s resources=%#v", client.applyNamespace, client.applyResources)
	}
	if client.applyResources.ConfigData["domain"] != "apps.example.com" || client.applyResources.SecretData["POSTGRES_PASSWORD"] != "secret" {
		t.Fatalf("unexpected resource data %#v", client.applyResources)
	}
	if client.pruneNamespace != "supadupa" || client.pruneResources.ConfigMapName != "alpha-runtime" {
		t.Fatalf("expected resources pruned after apply, got namespace=%s resources=%#v", client.pruneNamespace, client.pruneResources)
	}
	if client.status.Phase != "RuntimeRendered" || client.status.Conditions[0].Type != "ResourcesRendered" || client.status.Conditions[0].Status != "True" {
		t.Fatalf("unexpected status %#v", client.status)
	}
	if client.status.ObservedGeneration != 7 || client.status.LastReconciledAt != "2026-06-08T12:00:00Z" {
		t.Fatalf("unexpected status timing %#v", client.status)
	}
	if client.status.RuntimeSecurityDefaults.SeccompProfile != "RuntimeDefault" || len(client.status.RuntimeSecurityDefaults.DropCapabilities) != 1 {
		t.Fatalf("runtime security defaults not reflected in status %#v", client.status.RuntimeSecurityDefaults)
	}
}

func TestReconcileProjectRendersWorkloadsForImageBackedServices(t *testing.T) {
	allowPrivilegeEscalation := false
	replicas := int32(2)
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return now }}

	err := reconciler.ReconcileProject(context.Background(), "runtime", Project{
		Metadata: ObjectMeta{Name: "alpha", Generation: 8},
		Spec: ProjectSpec{
			Ref:          "alpha",
			DesiredState: "running",
			Environment:  map[string]string{"POSTGRES_PASSWORD": "secret"},
			Services: map[string]ServiceSpec{
				"auth": {
					Enabled:  true,
					Image:    "example/auth:v1",
					Replicas: &replicas,
					DependsOn: []ServiceDependencySpec{
						{Service: "db", Port: 5432},
					},
					Ports:   []ServicePortSpec{{Name: "http", Port: 9999}},
					Volumes: []ServiceVolumeSpec{{Name: "data", MountPath: "/data", Size: "1Gi"}},
					ConfigFiles: []ServiceConfigFileSpec{
						{Name: "app-conf", MountPath: "/etc/auth/app.conf", Content: "listen=9999\n"},
					},
					WritablePaths: []ServiceWritableSpec{
						{Name: "tmp", MountPath: "/tmp"},
					},
					ReadOnlyRootFilesystem: true,
					ReadinessProbe:         &ServiceProbeSpec{Type: "http", Path: "/health", Port: 9999},
					Ingress:                &ServiceIngressSpec{Enabled: true, Host: "auth.alpha.example.com", Path: "/"},
				},
				"storage": {Enabled: true},
			},
			RuntimeSecurityDefaults: RuntimeSecurityDefaults{
				SeccompProfile:           "RuntimeDefault",
				AllowPrivilegeEscalation: &allowPrivilegeEscalation,
				DropCapabilities:         []string{"ALL"},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(client.applyResources.Workloads) != 1 {
		t.Fatalf("expected only image-backed workload rendered, got %#v", client.applyResources.Workloads)
	}
	workload := client.applyResources.Workloads[0]
	if workload.ServiceName != "auth" || workload.DeploymentName != "alpha-auth" || workload.Replicas != 2 {
		t.Fatalf("unexpected workload identity %#v", workload)
	}
	if len(workload.Ports) != 1 || workload.Ports[0].TargetPort != 9999 {
		t.Fatalf("unexpected workload ports %#v", workload.Ports)
	}
	if len(workload.Dependencies) != 1 || workload.Dependencies[0].Service != "db" || workload.Dependencies[0].Port != 5432 {
		t.Fatalf("unexpected workload dependencies %#v", workload.Dependencies)
	}
	if len(workload.Volumes) != 1 || workload.Volumes[0].MountPath != "/data" {
		t.Fatalf("unexpected workload volumes %#v", workload.Volumes)
	}
	if len(workload.ConfigFiles) != 1 || workload.ConfigFiles[0].MountPath != "/etc/auth/app.conf" || workload.ConfigFiles[0].DataKey != "service-auth-app-conf" {
		t.Fatalf("unexpected workload config files %#v", workload.ConfigFiles)
	}
	if client.applyResources.ConfigData["service-auth-app-conf"] != "listen=9999\n" {
		t.Fatalf("expected config file content in ConfigMap data, got %#v", client.applyResources.ConfigData)
	}
	if len(workload.WritablePaths) != 1 || workload.WritablePaths[0].MountPath != "/tmp" {
		t.Fatalf("unexpected workload writable paths %#v", workload.WritablePaths)
	}
	if !workload.Spec.ReadOnlyRootFilesystem || workload.Spec.ReadinessProbe == nil || workload.Spec.ReadinessProbe.Path != "/health" {
		t.Fatalf("unexpected workload probe/security spec %#v", workload.Spec)
	}
	if workload.Ingress == nil || workload.Ingress.Host != "auth.alpha.example.com" {
		t.Fatalf("expected ingress workload, got %#v", workload.Ingress)
	}
	if client.observeNamespace != "runtime" || client.observeResources.ConfigMapName != "alpha-runtime" {
		t.Fatalf("expected workload observation after apply/prune, got namespace=%s resources=%#v", client.observeNamespace, client.observeResources)
	}
	if client.status.Phase != "RuntimeReady" || client.status.Conditions[1].Type != "WorkloadsRendered" || client.status.Conditions[1].Status != "True" ||
		client.status.Conditions[2].Type != "WorkloadsAvailable" || client.status.Conditions[2].Status != "True" || client.status.Conditions[3].Status != "True" {
		t.Fatalf("unexpected workload status %#v", client.status)
	}
}

func TestReconcileProjectReportsPendingUntilWorkloadsAreAvailable(t *testing.T) {
	client := &fakeClient{observation: ProjectResourceObservation{
		Checked: true,
		Ready:   false,
		Message: "deployment/alpha-auth available 0/1; persistentvolumeclaim/alpha-auth-data phase Pending",
	}}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return time.Unix(0, 0).UTC() }}

	err := reconciler.ReconcileProject(context.Background(), "runtime", Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec: ProjectSpec{
			Ref:          "alpha",
			DesiredState: "running",
			Services: map[string]ServiceSpec{
				"auth": {
					Enabled: true,
					Image:   "example/auth:v1",
					Volumes: []ServiceVolumeSpec{
						{Name: "data", MountPath: "/data", Size: "1Gi"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if client.status.Phase != "RuntimePending" || client.status.Conditions[2].Type != "WorkloadsAvailable" || client.status.Conditions[2].Status != "False" || client.status.Conditions[3].Status != "False" {
		t.Fatalf("expected pending workload status, got %#v", client.status)
	}
	if !strings.Contains(client.status.Message, "deployment/alpha-auth available 0/1") {
		t.Fatalf("expected observation message in status, got %#v", client.status)
	}
}

func TestReconcileProjectScalesWorkloadsToZeroWhenPaused(t *testing.T) {
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return time.Unix(0, 0).UTC() }}

	err := reconciler.ReconcileProject(context.Background(), "runtime", Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec: ProjectSpec{
			DesiredState: "paused",
			Services: map[string]ServiceSpec{
				"auth": {Enabled: true, Config: map[string]string{"image": "example/auth:v1", "port": "8080"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(client.applyResources.Workloads) != 1 || client.applyResources.Workloads[0].Replicas != 0 {
		t.Fatalf("expected paused workload scaled to zero, got %#v", client.applyResources.Workloads)
	}
	if client.status.Phase != "Paused" || !strings.Contains(client.status.Message, "scaled to zero") {
		t.Fatalf("unexpected paused status %#v", client.status)
	}
}

func TestReconcileProjectDeletesResourcesWhenDestroying(t *testing.T) {
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return time.Unix(0, 0).UTC() }}

	err := reconciler.ReconcileProject(context.Background(), "supadupa", Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec:     ProjectSpec{Ref: "alpha", DesiredState: "destroying"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if client.deleteNamespace != "supadupa" || client.deleteResources.ConfigMapName != "alpha-runtime" {
		t.Fatalf("expected runtime resources deleted, got namespace=%s resources=%#v", client.deleteNamespace, client.deleteResources)
	}
	if client.status.Phase != "Terminating" || client.status.Conditions[0].Status != "False" {
		t.Fatalf("unexpected destroying status %#v", client.status)
	}
}

func TestReconcileProjectReportsUnsupportedDesiredState(t *testing.T) {
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return time.Unix(0, 0).UTC() }}

	err := reconciler.ReconcileProject(context.Background(), "supadupa", Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec:     ProjectSpec{DesiredState: "mystery"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if client.applyCount != 0 || client.pruneCount != 0 || client.deleteCount != 0 {
		t.Fatalf("unsupported desired state should not apply/prune/delete resources: apply=%d prune=%d delete=%d", client.applyCount, client.pruneCount, client.deleteCount)
	}
	if client.status.Phase != "Degraded" || client.status.Conditions[0].Reason != "UnsupportedDesiredState" {
		t.Fatalf("expected degraded status, got %#v", client.status)
	}
}

func TestReconcileProjectIsolationCreatesNamespaceAndPolicies(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	reconciler := Reconciler{
		Client:                 client,
		Now:                    func() time.Time { return now },
		IsolationEnabled:       true,
		RuntimeNamespacePrefix: "supadupa-proj-",
		PodSecurityEnforce:     "restricted",
		DefaultQuota:           &ProjectQuotaDefaults{Hard: map[string]string{"pods": "50"}},
	}

	err := reconciler.ReconcileProject(context.Background(), "supadupa", Project{
		Metadata: ObjectMeta{Name: "alpha", Generation: 1},
		Spec: ProjectSpec{
			Ref:          "alpha",
			DesiredState: "running",
			Services: map[string]ServiceSpec{
				"kong": {Enabled: true, Image: "kong/kong:3", Ports: []ServicePortSpec{{Name: "http", Port: 8000}}, Ingress: &ServiceIngressSpec{Enabled: true, Host: "alpha.example.com"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(client.ensureNamespaces) != 1 || client.ensureNamespaces[0] != "supadupa-proj-alpha" {
		t.Fatalf("expected runtime namespace ensured, got %#v", client.ensureNamespaces)
	}
	if client.ensureNamespaceLabels["pod-security.kubernetes.io/enforce"] != "restricted" {
		t.Fatalf("expected restricted PSA label on namespace, got %#v", client.ensureNamespaceLabels)
	}
	if client.isolationCount != 1 || client.isolationNamespace != "supadupa-proj-alpha" {
		t.Fatalf("expected isolation applied to runtime namespace, got count=%d ns=%s", client.isolationCount, client.isolationNamespace)
	}
	if client.isolation.ServiceAccountName != "alpha-runtime" || client.isolation.Quota == nil {
		t.Fatalf("unexpected isolation payload %#v", client.isolation)
	}
	if client.applyNamespace != "supadupa-proj-alpha" || client.pruneNamespace != "supadupa-proj-alpha" || client.observeNamespace != "supadupa-proj-alpha" {
		t.Fatalf("expected workload ops in runtime namespace, got apply=%s prune=%s observe=%s", client.applyNamespace, client.pruneNamespace, client.observeNamespace)
	}
	if client.patchNamespace != "supadupa" || client.patchName != "alpha" {
		t.Fatalf("expected status patch in control namespace, got %s/%s", client.patchNamespace, client.patchName)
	}
}

func TestReconcileProjectRejectsUnownedRuntimeNamespace(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	reconciler := Reconciler{
		Client:                 client,
		Now:                    func() time.Time { return now },
		IsolationEnabled:       true,
		RuntimeNamespacePrefix: "supadupa-proj-",
	}

	err := reconciler.ReconcileProject(context.Background(), "supadupa", Project{
		Metadata: ObjectMeta{Name: "alpha", Generation: 1},
		Spec: ProjectSpec{
			Ref:              "alpha",
			DesiredState:     "running",
			RuntimeNamespace: "kube-system",
			Services:         map[string]ServiceSpec{"auth": {Enabled: true, Image: "example/auth:v1"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `runtimeNamespace "kube-system" is not owned`) {
		t.Fatalf("expected unowned runtime namespace error, got %v", err)
	}
	if len(client.ensureNamespaces) != 0 || len(client.deleteNamespaces) != 0 || client.applyNamespace != "" || client.deleteNamespace != "" {
		t.Fatalf("unowned namespace must not be touched, ensure=%#v deleteNS=%#v apply=%q deleteResources=%q", client.ensureNamespaces, client.deleteNamespaces, client.applyNamespace, client.deleteNamespace)
	}
	if client.patchNamespace != "supadupa" || client.patchName != "alpha" {
		t.Fatalf("expected degraded status in control namespace, got %s/%s", client.patchNamespace, client.patchName)
	}
	if client.status.Phase != "Degraded" || client.status.Conditions[0].Reason != "InvalidRuntimeNamespace" {
		t.Fatalf("expected InvalidRuntimeNamespace status, got %#v", client.status)
	}
}

func TestReconcileProjectLegacyModeUnchanged(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return now }} // IsolationEnabled false

	err := reconciler.ReconcileProject(context.Background(), "supadupa", Project{
		Metadata: ObjectMeta{Name: "alpha", Generation: 1},
		Spec: ProjectSpec{
			Ref:          "alpha",
			DesiredState: "running",
			Services:     map[string]ServiceSpec{"auth": {Enabled: true}},
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(client.ensureNamespaces) != 0 || client.isolationCount != 0 {
		t.Fatalf("legacy mode must not touch namespaces/isolation, got ensure=%#v isolation=%d", client.ensureNamespaces, client.isolationCount)
	}
	if client.applyNamespace != "supadupa" || client.pruneNamespace != "supadupa" || client.patchNamespace != "supadupa" {
		t.Fatalf("expected everything in control namespace, got apply=%s prune=%s patch=%s", client.applyNamespace, client.pruneNamespace, client.patchNamespace)
	}
}

func TestReconcileProjectDestroyDeletesNamespace(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return now }, IsolationEnabled: true}

	err := reconciler.ReconcileProject(context.Background(), "supadupa", Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec:     ProjectSpec{Ref: "alpha", DesiredState: "destroying"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if client.deleteNamespace != "supadupa-proj-alpha" {
		t.Fatalf("expected runtime resources deleted in runtime namespace, got %s", client.deleteNamespace)
	}
	if len(client.deleteNamespaces) != 1 || client.deleteNamespaces[0] != "supadupa-proj-alpha" {
		t.Fatalf("expected runtime namespace deleted, got %#v", client.deleteNamespaces)
	}
	for _, ns := range client.deleteNamespaces {
		if ns == "supadupa" {
			t.Fatalf("control namespace must never be deleted")
		}
	}
	if client.status.Phase != "Terminating" {
		t.Fatalf("expected Terminating status, got %#v", client.status)
	}
}

func TestReconcileProjectAddsFinalizerAndBindsServiceAccountWhenIsolated(t *testing.T) {
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return time.Unix(0, 0).UTC() }, IsolationEnabled: true}

	err := reconciler.ReconcileProject(context.Background(), "supadupa", Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec: ProjectSpec{
			Ref:          "alpha",
			DesiredState: "running",
			Services:     map[string]ServiceSpec{"auth": {Enabled: true, Image: "example/auth:v1"}},
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if client.finalizerCount != 1 || client.finalizerNamespace != "supadupa" || client.finalizerName != "alpha" {
		t.Fatalf("expected finalizer set in control namespace, got count=%d ns=%s name=%s", client.finalizerCount, client.finalizerNamespace, client.finalizerName)
	}
	if !hasFinalizer(client.finalizers, projectFinalizer) {
		t.Fatalf("expected runtime-namespace finalizer present, got %#v", client.finalizers)
	}
	if client.applyResources.ServiceAccountName != "alpha-runtime" {
		t.Fatalf("expected workloads bound to per-project SA, got %q", client.applyResources.ServiceAccountName)
	}
}

func TestReconcileProjectDoesNotReAddFinalizerWhenPresent(t *testing.T) {
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return time.Unix(0, 0).UTC() }, IsolationEnabled: true}

	err := reconciler.ReconcileProject(context.Background(), "supadupa", Project{
		Metadata: ObjectMeta{Name: "alpha", Finalizers: []string{projectFinalizer}},
		Spec: ProjectSpec{
			Ref:          "alpha",
			DesiredState: "running",
			Services:     map[string]ServiceSpec{"auth": {Enabled: true, Image: "example/auth:v1"}},
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if client.finalizerCount != 0 {
		t.Fatalf("expected no finalizer re-patch when already present, got %d", client.finalizerCount)
	}
}

func TestReconcileProjectFinalizerDrivenTeardownOnDeletion(t *testing.T) {
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return time.Unix(0, 0).UTC() }, IsolationEnabled: true}

	err := reconciler.ReconcileProject(context.Background(), "supadupa", Project{
		Metadata: ObjectMeta{
			Name:              "alpha",
			Finalizers:        []string{projectFinalizer},
			DeletionTimestamp: "2026-06-09T00:00:00Z",
		},
		Spec: ProjectSpec{Ref: "alpha", DesiredState: "running"},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if client.deleteResources.ConfigMapName != "alpha-runtime" || client.deleteNamespace != "supadupa-proj-alpha" {
		t.Fatalf("expected runtime resources deleted, got ns=%s resources=%#v", client.deleteNamespace, client.deleteResources)
	}
	if len(client.deleteNamespaces) != 1 || client.deleteNamespaces[0] != "supadupa-proj-alpha" {
		t.Fatalf("expected runtime namespace deleted, got %#v", client.deleteNamespaces)
	}
	if client.finalizerCount != 1 || hasFinalizer(client.finalizers, projectFinalizer) {
		t.Fatalf("expected finalizer removed after teardown, got count=%d finalizers=%#v", client.finalizerCount, client.finalizers)
	}
	if client.patchCount != 0 {
		t.Fatalf("deletion path must not patch status, got %d", client.patchCount)
	}
}

func TestReconcileProjectReplicaPatchesPendingStatus(t *testing.T) {
	allowPrivilegeEscalation := false
	now := time.Date(2026, 6, 8, 13, 0, 0, 0, time.UTC)
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return now }}

	err := reconciler.ReconcileProjectReplica(context.Background(), "runtime", ProjectReplica{
		Metadata: ObjectMeta{Name: "alpha-east", Generation: 4},
		Spec: ProjectReplicaSpec{
			ID:               "replica-1",
			ProjectRef:       "alpha",
			Name:             "east",
			ResourceTier:     "small",
			Region:           "us-east-1",
			HostID:           "host-1",
			ReadWeight:       100,
			FailoverPriority: 1,
			RuntimeSecurityDefaults: RuntimeSecurityDefaults{
				SeccompProfile:           "RuntimeDefault",
				AllowPrivilegeEscalation: &allowPrivilegeEscalation,
				DropCapabilities:         []string{"ALL"},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile replica failed: %v", err)
	}
	if client.replicaPatchNamespace != "runtime" || client.replicaPatchName != "alpha-east" {
		t.Fatalf("unexpected replica patch target %s/%s", client.replicaPatchNamespace, client.replicaPatchName)
	}
	if client.replicaStatus.Phase != "ReplicaPending" || client.replicaStatus.Conditions[0].Type != "ReplicaObserved" || client.replicaStatus.Conditions[1].Reason != "ReplicaDataPlanePending" {
		t.Fatalf("unexpected replica status %#v", client.replicaStatus)
	}
	if client.replicaStatus.ObservedGeneration != 4 || client.replicaStatus.LastReconciledAt != "2026-06-08T13:00:00Z" {
		t.Fatalf("unexpected replica status timing %#v", client.replicaStatus)
	}
	if client.replicaStatus.RuntimeSecurityDefaults.SeccompProfile != "RuntimeDefault" {
		t.Fatalf("runtime security defaults not reflected in replica status %#v", client.replicaStatus.RuntimeSecurityDefaults)
	}
}

func TestReconcileProjectReplicaReportsInvalidSpec(t *testing.T) {
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return time.Unix(0, 0).UTC() }}

	err := reconciler.ReconcileProjectReplica(context.Background(), "runtime", ProjectReplica{
		Metadata: ObjectMeta{Name: "alpha-east"},
		Spec:     ProjectReplicaSpec{ProjectRef: "alpha", Name: "east"},
	})
	if err == nil || !strings.Contains(err.Error(), "spec.id is required") {
		t.Fatalf("expected invalid replica spec error, got %v", err)
	}
	if client.replicaStatus.Phase != "Degraded" || client.replicaStatus.Conditions[0].Reason != "InvalidReplicaSpec" {
		t.Fatalf("expected degraded replica status, got %#v", client.replicaStatus)
	}
}

func TestReconcileObservedProjectChildCRDPatchesStatus(t *testing.T) {
	client := &fakeClient{}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return time.Unix(0, 0).UTC() }}

	if err := reconciler.ReconcileProjectConfig(context.Background(), "runtime", ProjectConfig{
		Metadata: ObjectMeta{Name: "alpha-auth", Generation: 2},
		Spec:     ProjectConfigSpec{ProjectRef: "alpha", Area: "auth", Config: map[string]string{"site_url": "https://alpha.example.com"}},
	}); err != nil {
		t.Fatalf("reconcile project config: %v", err)
	}
	if client.configStatus.Phase != "Observed" || client.configStatus.Conditions[0].Type != "ProjectConfigObserved" {
		t.Fatalf("unexpected project config status %#v", client.configStatus)
	}

	if err := reconciler.ReconcileProjectAuthHooks(context.Background(), "runtime", ProjectAuthHooks{
		Metadata: ObjectMeta{Name: "alpha-auth-hooks"},
		Spec:     ProjectAuthHooksSpec{ProjectRef: "alpha", Hooks: []ProjectAuthHookSpec{{Type: "custom-access-token", Enabled: true}}},
	}); err != nil {
		t.Fatalf("reconcile auth hooks: %v", err)
	}
	if client.authHooksStatus.Phase != "Observed" || client.authHooksStatus.Conditions[0].Type != "ProjectAuthHooksObserved" {
		t.Fatalf("unexpected auth hooks status %#v", client.authHooksStatus)
	}

	if err := reconciler.ReconcileProjectBranchClone(context.Background(), "runtime", ProjectBranchClone{
		Metadata: ObjectMeta{Name: "branch-clone"},
		Spec:     ProjectBranchCloneSpec{SourceRef: "alpha", BranchRef: "alpha-preview"},
	}); err != nil {
		t.Fatalf("reconcile branch clone: %v", err)
	}
	if client.branchCloneStatus.Phase != "Observed" || client.branchCloneStatus.Conditions[0].Type != "ProjectBranchCloneObserved" {
		t.Fatalf("unexpected branch clone status %#v", client.branchCloneStatus)
	}

	if err := reconciler.ReconcileRetainedProjectResources(context.Background(), "runtime", RetainedProjectResources{
		Metadata: ObjectMeta{Name: "alpha-retained"},
		Spec: RetainedProjectResourcesSpec{
			ProjectRef: "alpha",
			RetainedAt: "2026-06-08T00:00:00Z",
			Resources:  []RetainedProjectResourceRef{{Kind: "PersistentVolumeClaim", Name: "alpha-db-data"}},
		},
	}); err != nil {
		t.Fatalf("reconcile retained resources: %v", err)
	}
	if client.retainedStatus.Phase != "Observed" || client.retainedStatus.Conditions[0].Type != "RetainedProjectResourcesObserved" {
		t.Fatalf("unexpected retained resources status %#v", client.retainedStatus)
	}
}

func TestReconcileNamespaceListsAndPatchesProjects(t *testing.T) {
	client := &fakeClient{
		projects: []Project{
			{Metadata: ObjectMeta{Name: "alpha"}},
			{Metadata: ObjectMeta{Name: "beta"}},
		},
		configs: []ProjectConfig{
			{Metadata: ObjectMeta{Name: "alpha-auth"}, Spec: ProjectConfigSpec{ProjectRef: "alpha", Area: "auth"}},
		},
		authHooks: []ProjectAuthHooks{
			{Metadata: ObjectMeta{Name: "alpha-auth-hooks"}, Spec: ProjectAuthHooksSpec{ProjectRef: "alpha"}},
		},
		branchClones: []ProjectBranchClone{
			{Metadata: ObjectMeta{Name: "alpha-preview-clone"}, Spec: ProjectBranchCloneSpec{SourceRef: "alpha", BranchRef: "alpha-preview"}},
		},
		replicas: []ProjectReplica{
			{Metadata: ObjectMeta{Name: "alpha-east"}, Spec: ProjectReplicaSpec{ID: "replica-1", ProjectRef: "alpha", Name: "east"}},
		},
		retained: []RetainedProjectResources{
			{Metadata: ObjectMeta{Name: "alpha-retained"}, Spec: RetainedProjectResourcesSpec{ProjectRef: "alpha", RetainedAt: "2026-06-08T00:00:00Z"}},
		},
	}
	reconciler := Reconciler{Client: client, Now: func() time.Time { return time.Unix(0, 0).UTC() }}

	if err := reconciler.ReconcileNamespace(context.Background(), "supadupa"); err != nil {
		t.Fatalf("reconcile namespace failed: %v", err)
	}
	if client.listNamespace != "supadupa" || client.configListNamespace != "supadupa" || client.authHooksListNamespace != "supadupa" ||
		client.branchCloneListNamespace != "supadupa" || client.replicaListNamespace != "supadupa" || client.retainedListNamespace != "supadupa" ||
		client.patchCount != 2 || client.configPatchCount != 1 || client.authHooksPatchCount != 1 ||
		client.branchClonePatchCount != 1 || client.replicaPatchCount != 1 || client.retainedPatchCount != 1 {
		t.Fatalf("unexpected client calls: %#v", client)
	}
}

func TestResourcesForProjectUsesDNSLengthSafeNames(t *testing.T) {
	resources, err := resourcesForProject(Project{
		Spec: ProjectSpec{Ref: "alpha-" + strings.Repeat("0123456789-", 8)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.ConfigMapName) > 63 || len(resources.SecretName) > 63 {
		t.Fatalf("expected Kubernetes-safe resource names, got configmap=%q(%d) secret=%q(%d)", resources.ConfigMapName, len(resources.ConfigMapName), resources.SecretName, len(resources.SecretName))
	}
	if resources.ConfigMapName == "" || resources.SecretName == "" {
		t.Fatalf("expected non-empty resource names: %#v", resources)
	}
}

type fakeClient struct {
	projects                  []Project
	configs                   []ProjectConfig
	authHooks                 []ProjectAuthHooks
	branchClones              []ProjectBranchClone
	replicas                  []ProjectReplica
	retained                  []RetainedProjectResources
	listNamespace             string
	configListNamespace       string
	authHooksListNamespace    string
	branchCloneListNamespace  string
	replicaListNamespace      string
	retainedListNamespace     string
	applyNamespace            string
	pruneNamespace            string
	observeNamespace          string
	deleteNamespace           string
	patchNamespace            string
	patchName                 string
	configPatchNamespace      string
	configPatchName           string
	authHooksPatchNamespace   string
	authHooksPatchName        string
	branchClonePatchNamespace string
	branchClonePatchName      string
	replicaPatchNamespace     string
	replicaPatchName          string
	retainedPatchNamespace    string
	retainedPatchName         string
	applyCount                int
	pruneCount                int
	observeCount              int
	deleteCount               int
	patchCount                int
	configPatchCount          int
	authHooksPatchCount       int
	branchClonePatchCount     int
	replicaPatchCount         int
	retainedPatchCount        int
	applyResources            ProjectResources
	pruneResources            ProjectResources
	observeResources          ProjectResources
	deleteResources           ProjectResources
	ensureNamespaces          []string
	ensureNamespaceLabels     map[string]string
	deleteNamespaces          []string
	isolationNamespace        string
	isolationCount            int
	isolation                 ProjectIsolation
	status                    ProjectStatus
	configStatus              ResourceStatus
	authHooksStatus           ResourceStatus
	branchCloneStatus         ResourceStatus
	replicaStatus             ReplicaStatus
	retainedStatus            ResourceStatus
	observation               ProjectResourceObservation
	finalizerNamespace        string
	finalizerName             string
	finalizerCount            int
	finalizers                []string
}

func (c *fakeClient) ListProjects(_ context.Context, namespace string) ([]Project, error) {
	c.listNamespace = namespace
	return c.projects, nil
}

func (c *fakeClient) ListProjectConfigs(_ context.Context, namespace string) ([]ProjectConfig, error) {
	c.configListNamespace = namespace
	return c.configs, nil
}

func (c *fakeClient) ListProjectAuthHooks(_ context.Context, namespace string) ([]ProjectAuthHooks, error) {
	c.authHooksListNamespace = namespace
	return c.authHooks, nil
}

func (c *fakeClient) ListProjectBranchClones(_ context.Context, namespace string) ([]ProjectBranchClone, error) {
	c.branchCloneListNamespace = namespace
	return c.branchClones, nil
}

func (c *fakeClient) ListProjectReplicas(_ context.Context, namespace string) ([]ProjectReplica, error) {
	c.replicaListNamespace = namespace
	return c.replicas, nil
}

func (c *fakeClient) ListRetainedProjectResources(_ context.Context, namespace string) ([]RetainedProjectResources, error) {
	c.retainedListNamespace = namespace
	return c.retained, nil
}

func (c *fakeClient) EnsureNamespace(_ context.Context, name string, labels map[string]string) error {
	c.ensureNamespaces = append(c.ensureNamespaces, name)
	c.ensureNamespaceLabels = labels
	return nil
}

func (c *fakeClient) DeleteNamespace(_ context.Context, name string) error {
	c.deleteNamespaces = append(c.deleteNamespaces, name)
	return nil
}

func (c *fakeClient) ApplyProjectIsolation(_ context.Context, namespace string, _ Project, iso ProjectIsolation) error {
	c.isolationNamespace = namespace
	c.isolationCount++
	c.isolation = iso
	return nil
}

func (c *fakeClient) ApplyProjectResources(_ context.Context, namespace string, _ Project, resources ProjectResources) error {
	c.applyNamespace = namespace
	c.applyResources = resources
	c.applyCount++
	return nil
}

func (c *fakeClient) PruneProjectResources(_ context.Context, namespace string, _ Project, resources ProjectResources) error {
	c.pruneNamespace = namespace
	c.pruneResources = resources
	c.pruneCount++
	return nil
}

func (c *fakeClient) ObserveProjectResources(_ context.Context, namespace string, resources ProjectResources) (ProjectResourceObservation, error) {
	c.observeNamespace = namespace
	c.observeResources = resources
	c.observeCount++
	if c.observation.Checked || c.observation.Message != "" {
		return c.observation, nil
	}
	if len(resources.Workloads) == 0 {
		return ProjectResourceObservation{Ready: true, Message: "no workload resources to observe"}, nil
	}
	return ProjectResourceObservation{Checked: true, Ready: true, Message: "all workload deployments are available and persistent volume claims are bound"}, nil
}

func (c *fakeClient) DeleteProjectResources(_ context.Context, namespace string, _ Project, resources ProjectResources) error {
	c.deleteNamespace = namespace
	c.deleteResources = resources
	c.deleteCount++
	return nil
}

func (c *fakeClient) SetProjectFinalizers(_ context.Context, namespace string, name string, finalizers []string) error {
	c.finalizerNamespace = namespace
	c.finalizerName = name
	c.finalizerCount++
	c.finalizers = finalizers
	return nil
}

func (c *fakeClient) PatchProjectStatus(_ context.Context, namespace string, name string, status ProjectStatus) error {
	c.patchNamespace = namespace
	c.patchName = name
	c.patchCount++
	c.status = status
	return nil
}

func (c *fakeClient) PatchProjectConfigStatus(_ context.Context, namespace string, name string, status ResourceStatus) error {
	c.configPatchNamespace = namespace
	c.configPatchName = name
	c.configPatchCount++
	c.configStatus = status
	return nil
}

func (c *fakeClient) PatchProjectAuthHooksStatus(_ context.Context, namespace string, name string, status ResourceStatus) error {
	c.authHooksPatchNamespace = namespace
	c.authHooksPatchName = name
	c.authHooksPatchCount++
	c.authHooksStatus = status
	return nil
}

func (c *fakeClient) PatchProjectBranchCloneStatus(_ context.Context, namespace string, name string, status ResourceStatus) error {
	c.branchClonePatchNamespace = namespace
	c.branchClonePatchName = name
	c.branchClonePatchCount++
	c.branchCloneStatus = status
	return nil
}

func (c *fakeClient) PatchProjectReplicaStatus(_ context.Context, namespace string, name string, status ReplicaStatus) error {
	c.replicaPatchNamespace = namespace
	c.replicaPatchName = name
	c.replicaPatchCount++
	c.replicaStatus = status
	return nil
}

func (c *fakeClient) PatchRetainedProjectResourcesStatus(_ context.Context, namespace string, name string, status ResourceStatus) error {
	c.retainedPatchNamespace = namespace
	c.retainedPatchName = name
	c.retainedPatchCount++
	c.retainedStatus = status
	return nil
}
