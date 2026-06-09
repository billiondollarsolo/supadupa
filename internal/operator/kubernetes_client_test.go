package operator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKubernetesClientAppliesResourcesDeletesAndPatchesStatus(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Content-Type"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apis/platform.supadupa.dev/v1alpha1/namespaces/supadupa/projects":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha","generation":3},"spec":{"ref":"alpha","desiredState":"running"}}]}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "labelSelector="):
			switch r.URL.Path {
			case "/apis/apps/v1/namespaces/supadupa/deployments",
				"/api/v1/namespaces/supadupa/services",
				"/apis/networking.k8s.io/v1/namespaces/supadupa/ingresses",
				"/api/v1/namespaces/supadupa/persistentvolumeclaims":
				_, _ = w.Write([]byte(`{"items":[]}`))
			default:
				t.Fatalf("unexpected collection request %s", r.URL.RequestURI())
			}
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/namespaces/supadupa/configmaps/alpha-runtime":
			if r.URL.Query().Get("fieldManager") != "supadupa-operator" || r.Header.Get("Content-Type") != "application/apply-patch+yaml" {
				t.Fatalf("unexpected configmap apply request: %s %s", r.URL.RequestURI(), r.Header.Get("Content-Type"))
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["kind"] != "ConfigMap" {
				t.Fatalf("expected ConfigMap payload, got %#v", payload)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/namespaces/supadupa/secrets/alpha-environment":
			if r.Header.Get("Content-Type") != "application/apply-patch+yaml" {
				t.Fatalf("unexpected secret content type %s", r.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPatch && r.URL.Path == "/apis/platform.supadupa.dev/v1alpha1/namespaces/supadupa/projects/alpha/status":
			if r.Header.Get("Content-Type") != "application/merge-patch+json" {
				t.Fatalf("unexpected status content type %s", r.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/namespaces/supadupa/configmaps/alpha-runtime":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/namespaces/supadupa/secrets/alpha-environment":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	client := &KubernetesClient{BaseURL: server.URL, Token: "token", Client: server.Client()}
	projects, err := client.ListProjects(context.Background(), "supadupa")
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 || projects[0].Metadata.Name != "alpha" {
		t.Fatalf("unexpected projects %#v", projects)
	}
	resources, err := resourcesForProject(projects[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyProjectResources(context.Background(), "supadupa", projects[0], resources); err != nil {
		t.Fatalf("apply resources: %v", err)
	}
	if err := client.PatchProjectStatus(context.Background(), "supadupa", "alpha", ProjectStatus{Phase: "RuntimeRendered"}); err != nil {
		t.Fatalf("patch status: %v", err)
	}
	if err := client.DeleteProjectResources(context.Background(), "supadupa", projects[0], resources); err != nil {
		t.Fatalf("delete resources: %v", err)
	}

	joined := strings.Join(seen, "\n")
	for _, expected := range []string{
		"GET /apis/platform.supadupa.dev/v1alpha1/namespaces/supadupa/projects ",
		"PATCH /api/v1/namespaces/supadupa/configmaps/alpha-runtime?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /api/v1/namespaces/supadupa/secrets/alpha-environment?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /apis/platform.supadupa.dev/v1alpha1/namespaces/supadupa/projects/alpha/status application/merge-patch+json",
		"DELETE /api/v1/namespaces/supadupa/configmaps/alpha-runtime ",
		"DELETE /api/v1/namespaces/supadupa/secrets/alpha-environment ",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected request %q in:\n%s", expected, joined)
		}
	}
}

func TestKubernetesClientListsProjectReplicasAndPatchesStatus(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Content-Type"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectreplicas":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-east","generation":5},"spec":{"id":"replica-1","projectRef":"alpha","name":"east","hostId":"host-1"}}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectreplicas/alpha-east/status":
			if r.Header.Get("Content-Type") != "application/merge-patch+json" {
				t.Fatalf("unexpected replica status content type %s", r.Header.Get("Content-Type"))
			}
			var payload map[string]ReplicaStatus
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["status"].Phase != "ReplicaPending" {
				t.Fatalf("unexpected replica status payload %#v", payload)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	client := &KubernetesClient{BaseURL: server.URL, Token: "token", Client: server.Client()}
	replicas, err := client.ListProjectReplicas(context.Background(), "runtime")
	if err != nil {
		t.Fatalf("list project replicas: %v", err)
	}
	if len(replicas) != 1 || replicas[0].Metadata.Name != "alpha-east" || replicas[0].Spec.HostID != "host-1" {
		t.Fatalf("unexpected replicas %#v", replicas)
	}
	if err := client.PatchProjectReplicaStatus(context.Background(), "runtime", "alpha-east", ReplicaStatus{Phase: "ReplicaPending"}); err != nil {
		t.Fatalf("patch replica status: %v", err)
	}

	joined := strings.Join(seen, "\n")
	for _, expected := range []string{
		"GET /apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectreplicas ",
		"PATCH /apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectreplicas/alpha-east/status application/merge-patch+json",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected request %q in:\n%s", expected, joined)
		}
	}
}

func TestKubernetesClientListsAuxiliaryProjectCRDsAndPatchesStatus(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Content-Type"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectconfigs":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-auth"},"spec":{"projectRef":"alpha","area":"auth"}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectauthhooks":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-auth-hooks"},"spec":{"projectRef":"alpha","hooks":[{"type":"custom-access-token"}]}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectbranchclones":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-preview-clone"},"spec":{"sourceRef":"alpha","branchRef":"alpha-preview"}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/retainedprojectresources":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-retained"},"spec":{"projectRef":"alpha","retainedAt":"2026-06-08T00:00:00Z"}}]}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/"):
			if r.Header.Get("Content-Type") != "application/merge-patch+json" {
				t.Fatalf("unexpected auxiliary status content type %s", r.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	t.Cleanup(server.Close)

	client := &KubernetesClient{BaseURL: server.URL, Token: "token", Client: server.Client()}
	if configs, err := client.ListProjectConfigs(context.Background(), "runtime"); err != nil || len(configs) != 1 || configs[0].Spec.Area != "auth" {
		t.Fatalf("unexpected project configs %#v err=%v", configs, err)
	}
	if hooks, err := client.ListProjectAuthHooks(context.Background(), "runtime"); err != nil || len(hooks) != 1 || len(hooks[0].Spec.Hooks) != 1 {
		t.Fatalf("unexpected auth hooks %#v err=%v", hooks, err)
	}
	if clones, err := client.ListProjectBranchClones(context.Background(), "runtime"); err != nil || len(clones) != 1 || clones[0].Spec.BranchRef != "alpha-preview" {
		t.Fatalf("unexpected branch clones %#v err=%v", clones, err)
	}
	if retained, err := client.ListRetainedProjectResources(context.Background(), "runtime"); err != nil || len(retained) != 1 || retained[0].Spec.ProjectRef != "alpha" {
		t.Fatalf("unexpected retained resources %#v err=%v", retained, err)
	}
	status := ResourceStatus{Phase: "Observed"}
	if err := client.PatchProjectConfigStatus(context.Background(), "runtime", "alpha-auth", status); err != nil {
		t.Fatalf("patch project config status: %v", err)
	}
	if err := client.PatchProjectAuthHooksStatus(context.Background(), "runtime", "alpha-auth-hooks", status); err != nil {
		t.Fatalf("patch auth hooks status: %v", err)
	}
	if err := client.PatchProjectBranchCloneStatus(context.Background(), "runtime", "alpha-preview-clone", status); err != nil {
		t.Fatalf("patch branch clone status: %v", err)
	}
	if err := client.PatchRetainedProjectResourcesStatus(context.Background(), "runtime", "alpha-retained", status); err != nil {
		t.Fatalf("patch retained resources status: %v", err)
	}

	joined := strings.Join(seen, "\n")
	for _, expected := range []string{
		"GET /apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectconfigs ",
		"GET /apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectauthhooks ",
		"GET /apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectbranchclones ",
		"GET /apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/retainedprojectresources ",
		"PATCH /apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectconfigs/alpha-auth/status application/merge-patch+json",
		"PATCH /apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectauthhooks/alpha-auth-hooks/status application/merge-patch+json",
		"PATCH /apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/projectbranchclones/alpha-preview-clone/status application/merge-patch+json",
		"PATCH /apis/platform.supadupa.dev/v1alpha1/namespaces/runtime/retainedprojectresources/alpha-retained/status application/merge-patch+json",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected request %q in:\n%s", expected, joined)
		}
	}
}

func TestKubernetesClientAppliesAndDeletesWorkloadResources(t *testing.T) {
	allowPrivilegeEscalation := false
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Content-Type"))
		if r.Method == http.MethodPatch {
			if r.URL.Query().Get("fieldManager") != "supadupa-operator" || r.Header.Get("Content-Type") != "application/apply-patch+yaml" {
				t.Fatalf("unexpected apply request: %s %s", r.URL.RequestURI(), r.Header.Get("Content-Type"))
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			switch r.URL.Path {
			case "/api/v1/namespaces/runtime/configmaps/alpha-runtime":
				if payload["kind"] != "ConfigMap" {
					t.Fatalf("expected ConfigMap, got %#v", payload)
				}
				data := payload["data"].(map[string]any)
				if data["service-auth-app-conf"] != "listen=8080\n" {
					t.Fatalf("expected config file content in ConfigMap, got %#v", data)
				}
			case "/api/v1/namespaces/runtime/secrets/alpha-environment":
				if payload["kind"] != "Secret" {
					t.Fatalf("expected Secret, got %#v", payload)
				}
			case "/api/v1/namespaces/runtime/persistentvolumeclaims/alpha-auth-data":
				if payload["kind"] != "PersistentVolumeClaim" {
					t.Fatalf("expected PVC, got %#v", payload)
				}
			case "/apis/apps/v1/namespaces/runtime/deployments/alpha-auth":
				if payload["kind"] != "Deployment" {
					t.Fatalf("expected Deployment, got %#v", payload)
				}
				spec := payload["spec"].(map[string]any)
				template := spec["template"].(map[string]any)
				podSpec := template["spec"].(map[string]any)
				if podSpec["automountServiceAccountToken"] != false {
					t.Fatalf("expected service account token disabled, got %#v", podSpec)
				}
				securityContext := podSpec["securityContext"].(map[string]any)
				if securityContext["runAsNonRoot"] != true {
					t.Fatalf("expected default runAsNonRoot pod security, got %#v", securityContext)
				}
				seccompProfile := securityContext["seccompProfile"].(map[string]any)
				if seccompProfile["type"] != "RuntimeDefault" {
					t.Fatalf("expected RuntimeDefault seccomp, got %#v", securityContext)
				}
				containers := podSpec["containers"].([]any)
				container := containers[0].(map[string]any)
				containerCommand := container["command"].([]any)
				args := container["args"].([]any)
				if len(containerCommand) != 2 || containerCommand[0] != "/bin/sh" || containerCommand[1] != "-ec" || len(args) != 1 || args[0] != "echo boot && exec auth" {
					t.Fatalf("unexpected container command/args command=%#v args=%#v", containerCommand, args)
				}
				env := container["env"].([]any)
				if len(env) != 3 ||
					env[0].(map[string]any)["name"] != "A_FIRST" ||
					env[1].(map[string]any)["name"] != "MIDDLE" ||
					env[2].(map[string]any)["name"] != "Z_LAST" {
					t.Fatalf("expected deterministic sorted env, got %#v", env)
				}
				containerSecurity := container["securityContext"].(map[string]any)
				if containerSecurity["readOnlyRootFilesystem"] != true {
					t.Fatalf("expected read-only root filesystem, got %#v", containerSecurity)
				}
				readiness := container["readinessProbe"].(map[string]any)
				readinessHTTP := readiness["httpGet"].(map[string]any)
				if readinessHTTP["path"] != "/health" || readinessHTTP["port"] != float64(8080) {
					t.Fatalf("unexpected readiness probe %#v", readiness)
				}
				liveness := container["livenessProbe"].(map[string]any)
				livenessTCP := liveness["tcpSocket"].(map[string]any)
				if livenessTCP["port"] != float64(8080) {
					t.Fatalf("unexpected liveness probe %#v", liveness)
				}
				mounts := container["volumeMounts"].([]any)
				if len(mounts) != 3 {
					t.Fatalf("expected PVC, config file, and writable mounts, got %#v", mounts)
				}
				configMount := mounts[1].(map[string]any)
				if configMount["name"] != "config-app-conf" || configMount["mountPath"] != "/etc/auth/app.conf" ||
					configMount["subPath"] != "service-auth-app-conf" || configMount["readOnly"] != true {
					t.Fatalf("unexpected config file mount %#v", configMount)
				}
				volumes := podSpec["volumes"].([]any)
				if len(volumes) != 3 || volumes[2].(map[string]any)["emptyDir"] == nil {
					t.Fatalf("expected PVC, configMap, and emptyDir volumes, got %#v", volumes)
				}
				configVolume := volumes[1].(map[string]any)
				configMap := configVolume["configMap"].(map[string]any)
				items := configMap["items"].([]any)
				item := items[0].(map[string]any)
				if configVolume["name"] != "config-app-conf" || configMap["name"] != "alpha-runtime" ||
					item["key"] != "service-auth-app-conf" || item["path"] != "service-auth-app-conf" {
					t.Fatalf("unexpected configMap volume %#v", configVolume)
				}
				initContainers := podSpec["initContainers"].([]any)
				if len(initContainers) != 1 {
					t.Fatalf("expected dependency init container, got %#v", initContainers)
				}
				waitContainer := initContainers[0].(map[string]any)
				if waitContainer["name"] != "wait-db" {
					t.Fatalf("unexpected dependency init container name %#v", waitContainer)
				}
				command := waitContainer["command"].([]any)
				if len(command) != 3 || command[2] != "until nc -z alpha-db 5432; do sleep 2; done" {
					t.Fatalf("unexpected dependency wait command %#v", command)
				}
				initSecurity := waitContainer["securityContext"].(map[string]any)
				if initSecurity["readOnlyRootFilesystem"] != true || initSecurity["allowPrivilegeEscalation"] != false {
					t.Fatalf("unexpected dependency init security context %#v", initSecurity)
				}
				if initSecurity["runAsNonRoot"] != true || initSecurity["runAsUser"] != float64(65534) || initSecurity["runAsGroup"] != float64(65534) {
					t.Fatalf("dependency init container must run as explicit non-root nobody user, got %#v", initSecurity)
				}
			case "/api/v1/namespaces/runtime/services/alpha-auth":
				if payload["kind"] != "Service" {
					t.Fatalf("expected Service, got %#v", payload)
				}
			case "/apis/networking.k8s.io/v1/namespaces/runtime/ingresses/alpha-auth-ingress":
				if payload["kind"] != "Ingress" {
					t.Fatalf("expected Ingress, got %#v", payload)
				}
			default:
				t.Fatalf("unexpected patch path %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodGet {
			switch r.URL.Path {
			case "/apis/apps/v1/namespaces/runtime/deployments":
				_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-old"}}]}`))
			case "/api/v1/namespaces/runtime/services":
				_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-old"}}]}`))
			case "/apis/networking.k8s.io/v1/namespaces/runtime/ingresses":
				_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-old-ingress"}}]}`))
			case "/api/v1/namespaces/runtime/persistentvolumeclaims":
				_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-auth-data"}},{"metadata":{"name":"alpha-old-data"}},{"metadata":{"name":"alpha-retained-data","labels":{"supadupa.dev/retain":"true"}}}]}`))
			default:
				t.Fatalf("unexpected get path %s", r.URL.Path)
			}
			return
		}
		if r.Method == http.MethodDelete {
			switch r.URL.Path {
			case "/apis/networking.k8s.io/v1/namespaces/runtime/ingresses/alpha-auth-ingress",
				"/api/v1/namespaces/runtime/services/alpha-auth",
				"/apis/apps/v1/namespaces/runtime/deployments/alpha-auth",
				"/api/v1/namespaces/runtime/persistentvolumeclaims/alpha-auth-data",
				"/apis/apps/v1/namespaces/runtime/deployments/alpha-old",
				"/api/v1/namespaces/runtime/services/alpha-old",
				"/apis/networking.k8s.io/v1/namespaces/runtime/ingresses/alpha-old-ingress",
				"/api/v1/namespaces/runtime/persistentvolumeclaims/alpha-old-data",
				"/api/v1/namespaces/runtime/configmaps/alpha-runtime",
				"/api/v1/namespaces/runtime/secrets/alpha-environment":
				w.WriteHeader(http.StatusOK)
			default:
				t.Fatalf("unexpected delete path %s", r.URL.Path)
			}
			return
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
	}))
	t.Cleanup(server.Close)

	replicas := int32(1)
	project := Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec: ProjectSpec{
			Ref: "alpha",
			Services: map[string]ServiceSpec{
				"auth": {
					Enabled:  true,
					Image:    "example/auth:v1",
					Command:  []string{"/bin/sh", "-ec"},
					Args:     []string{"echo boot && exec auth"},
					Replicas: &replicas,
					DependsOn: []ServiceDependencySpec{
						{Service: "db", Port: 5432},
					},
					Ports:   []ServicePortSpec{{Name: "http", Port: 8080}},
					Env:     map[string]string{"Z_LAST": "z", "A_FIRST": "a", "MIDDLE": "m"},
					Volumes: []ServiceVolumeSpec{{Name: "data", MountPath: "/data", Size: "1Gi", Retain: true}},
					ConfigFiles: []ServiceConfigFileSpec{
						{Name: "app-conf", MountPath: "/etc/auth/app.conf", Content: "listen=8080\n"},
					},
					WritablePaths: []ServiceWritableSpec{
						{Name: "tmp", MountPath: "/tmp"},
					},
					ReadOnlyRootFilesystem: true,
					ReadinessProbe:         &ServiceProbeSpec{Type: "http", Path: "/health", Port: 8080},
					LivenessProbe:          &ServiceProbeSpec{Type: "tcp", Port: 8080},
					Ingress:                &ServiceIngressSpec{Enabled: true, Host: "auth.alpha.example.com"},
				},
			},
			RuntimeSecurityDefaults: RuntimeSecurityDefaults{
				SeccompProfile:           "RuntimeDefault",
				AllowPrivilegeEscalation: &allowPrivilegeEscalation,
				DropCapabilities:         []string{"ALL"},
			},
		},
	}
	resources, err := resourcesForProject(project)
	if err != nil {
		t.Fatal(err)
	}
	client := &KubernetesClient{BaseURL: server.URL, Token: "token", Client: server.Client()}
	if err := client.ApplyProjectResources(context.Background(), "runtime", project, resources); err != nil {
		t.Fatalf("apply resources: %v", err)
	}
	if err := client.DeleteProjectResources(context.Background(), "runtime", project, resources); err != nil {
		t.Fatalf("delete resources: %v", err)
	}

	joined := strings.Join(seen, "\n")
	for _, expected := range []string{
		"PATCH /api/v1/namespaces/runtime/configmaps/alpha-runtime?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /api/v1/namespaces/runtime/secrets/alpha-environment?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /api/v1/namespaces/runtime/persistentvolumeclaims/alpha-auth-data?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /apis/apps/v1/namespaces/runtime/deployments/alpha-auth?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /api/v1/namespaces/runtime/services/alpha-auth?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /apis/networking.k8s.io/v1/namespaces/runtime/ingresses/alpha-auth-ingress?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"DELETE /apis/networking.k8s.io/v1/namespaces/runtime/ingresses/alpha-auth-ingress ",
		"DELETE /api/v1/namespaces/runtime/services/alpha-auth ",
		"DELETE /apis/apps/v1/namespaces/runtime/deployments/alpha-auth ",
		"DELETE /apis/apps/v1/namespaces/runtime/deployments/alpha-old ",
		"DELETE /api/v1/namespaces/runtime/services/alpha-old ",
		"DELETE /apis/networking.k8s.io/v1/namespaces/runtime/ingresses/alpha-old-ingress ",
		"DELETE /api/v1/namespaces/runtime/persistentvolumeclaims/alpha-old-data ",
		"DELETE /api/v1/namespaces/runtime/configmaps/alpha-runtime ",
		"DELETE /api/v1/namespaces/runtime/secrets/alpha-environment ",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected request %q in:\n%s", expected, joined)
		}
	}
	for _, retained := range []string{"alpha-auth-data", "alpha-retained-data"} {
		if strings.Contains(joined, "DELETE /api/v1/namespaces/runtime/persistentvolumeclaims/"+retained) {
			t.Fatalf("retained PVC %s should not be deleted:\n%s", retained, joined)
		}
	}
}

func TestKubernetesClientPrunesStaleWorkloadResources(t *testing.T) {
	var deleted []string
	expectedSelector := "app.kubernetes.io/managed-by=supadupa-operator,supadupa.dev/project-ref=alpha"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("labelSelector") != expectedSelector {
				t.Fatalf("unexpected label selector %q", r.URL.Query().Get("labelSelector"))
			}
			switch r.URL.Path {
			case "/apis/apps/v1/namespaces/runtime/deployments":
				_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-auth"}},{"metadata":{"name":"alpha-rest"}}]}`))
			case "/api/v1/namespaces/runtime/services":
				_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-auth"}},{"metadata":{"name":"alpha-rest"}}]}`))
			case "/apis/networking.k8s.io/v1/namespaces/runtime/ingresses":
				_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-auth-ingress"}},{"metadata":{"name":"alpha-rest-ingress"}}]}`))
			case "/api/v1/namespaces/runtime/persistentvolumeclaims":
				_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-auth-data"}},{"metadata":{"name":"alpha-old-data","labels":{"supadupa.dev/retain":"true"}}},{"metadata":{"name":"alpha-rest-data"}}]}`))
			default:
				t.Fatalf("unexpected list path %s", r.URL.Path)
			}
			return
		}
		if r.Method == http.MethodDelete {
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
	}))
	t.Cleanup(server.Close)

	project := Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec: ProjectSpec{
			Ref: "alpha",
			Services: map[string]ServiceSpec{
				"auth": {
					Enabled: true,
					Image:   "example/auth:v1",
					Ports:   []ServicePortSpec{{Name: "http", Port: 8080}},
					Volumes: []ServiceVolumeSpec{{Name: "data", MountPath: "/data", Size: "1Gi"}},
					Ingress: &ServiceIngressSpec{Enabled: true, Host: "auth.alpha.example.com"},
				},
			},
		},
	}
	resources, err := resourcesForProject(project)
	if err != nil {
		t.Fatal(err)
	}
	client := &KubernetesClient{BaseURL: server.URL, Token: "token", Client: server.Client()}
	if err := client.PruneProjectResources(context.Background(), "runtime", project, resources); err != nil {
		t.Fatalf("prune resources: %v", err)
	}

	joined := strings.Join(deleted, "\n")
	for _, expected := range []string{
		"/apis/apps/v1/namespaces/runtime/deployments/alpha-rest",
		"/api/v1/namespaces/runtime/services/alpha-rest",
		"/apis/networking.k8s.io/v1/namespaces/runtime/ingresses/alpha-rest-ingress",
		"/api/v1/namespaces/runtime/persistentvolumeclaims/alpha-rest-data",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected delete %q in:\n%s", expected, joined)
		}
	}
	for _, unexpected := range []string{"alpha-auth", "alpha-old-data"} {
		if strings.Contains(joined, unexpected) {
			t.Fatalf("did not expect retained or desired resource %q to be deleted:\n%s", unexpected, joined)
		}
	}
}

func TestDeploymentObjectCanDisableRunAsNonRootForService(t *testing.T) {
	runAsNonRoot := false
	allowPrivilegeEscalation := true
	project := Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec: ProjectSpec{
			Ref: "alpha",
			Services: map[string]ServiceSpec{
				"db": {
					Enabled:                  true,
					Image:                    "supabase/postgres:15.8.1.060",
					RunAsNonRoot:             &runAsNonRoot,
					AllowPrivilegeEscalation: &allowPrivilegeEscalation,
					DropCapabilities:         []string{"NET_RAW"},
					Ports:                    []ServicePortSpec{{Name: "postgres", Port: 5432}},
				},
			},
			RuntimeSecurityDefaults: RuntimeSecurityDefaults{SeccompProfile: "RuntimeDefault", DropCapabilities: []string{"ALL"}},
		},
	}
	resources, err := resourcesForProject(project)
	if err != nil {
		t.Fatal(err)
	}
	deployment := deploymentObject(project, resources, resources.Workloads[0])
	spec := deployment["spec"].(map[string]any)
	template := spec["template"].(map[string]any)
	podSpec := template["spec"].(map[string]any)
	securityContext := podSpec["securityContext"].(map[string]any)
	if securityContext["runAsNonRoot"] != false {
		t.Fatalf("expected service runAsNonRoot override to be false, got %#v", securityContext)
	}
	seccompProfile := securityContext["seccompProfile"].(map[string]any)
	if seccompProfile["type"] != "RuntimeDefault" {
		t.Fatalf("expected seccomp default to remain, got %#v", securityContext)
	}
	containers := podSpec["containers"].([]map[string]any)
	containerSecurity := containers[0]["securityContext"].(map[string]any)
	if containerSecurity["allowPrivilegeEscalation"] != true {
		t.Fatalf("expected service allowPrivilegeEscalation override to be true, got %#v", containerSecurity)
	}
	drops := containerSecurity["capabilities"].(map[string]any)["drop"].([]string)
	if len(drops) != 1 || drops[0] != "NET_RAW" {
		t.Fatalf("expected service dropCapabilities override, got %#v", containerSecurity)
	}
}

func TestKubernetesClientEnsuresNamespaceWithPodSecurityLabels(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Content-Type"))
		if r.Method == http.MethodPatch && r.URL.Path == "/api/v1/namespaces/supadupa-proj-alpha" {
			if r.URL.Query().Get("fieldManager") != "supadupa-operator" || r.Header.Get("Content-Type") != "application/apply-patch+yaml" {
				t.Fatalf("unexpected namespace apply request %s %s", r.URL.RequestURI(), r.Header.Get("Content-Type"))
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["kind"] != "Namespace" {
				t.Fatalf("expected Namespace, got %#v", payload)
			}
			labels := payload["metadata"].(map[string]any)["labels"].(map[string]any)
			if labels["pod-security.kubernetes.io/enforce"] != "restricted" {
				t.Fatalf("expected restricted PSA label, got %#v", labels)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
	}))
	t.Cleanup(server.Close)

	client := &KubernetesClient{BaseURL: server.URL, Token: "token", Client: server.Client()}
	labels := namespaceLabels(Project{Spec: ProjectSpec{Ref: "alpha"}}, PodSecurityLevels{Enforce: "restricted"})
	if err := client.EnsureNamespace(context.Background(), "supadupa-proj-alpha", labels); err != nil {
		t.Fatalf("ensure namespace: %v", err)
	}
	if !strings.Contains(strings.Join(seen, "\n"), "PATCH /api/v1/namespaces/supadupa-proj-alpha?fieldManager=supadupa-operator&force=true application/apply-patch+yaml") {
		t.Fatalf("expected namespace apply request, got %#v", seen)
	}
}

func TestKubernetesClientRefusesToDeleteControlNamespace(t *testing.T) {
	client := &KubernetesClient{BaseURL: "http://example.invalid", ControlNamespace: "supadupa"}
	if err := client.DeleteNamespace(context.Background(), "supadupa"); err == nil {
		t.Fatalf("expected refusal to delete control namespace")
	}
}

func TestKubernetesClientAppliesProjectIsolation(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Content-Type"))
		if r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "labelSelector=") {
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}
		if r.Method == http.MethodPatch {
			if r.URL.Query().Get("fieldManager") != "supadupa-operator" || r.Header.Get("Content-Type") != "application/apply-patch+yaml" {
				t.Fatalf("unexpected isolation apply request %s %s", r.URL.RequestURI(), r.Header.Get("Content-Type"))
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			switch r.URL.Path {
			case "/api/v1/namespaces/supadupa-proj-alpha/serviceaccounts/alpha-runtime":
				if payload["kind"] != "ServiceAccount" || payload["automountServiceAccountToken"] != false {
					t.Fatalf("expected ServiceAccount with automount disabled, got %#v", payload)
				}
			case "/apis/networking.k8s.io/v1/namespaces/supadupa-proj-alpha/networkpolicies/alpha-default-deny",
				"/apis/networking.k8s.io/v1/namespaces/supadupa-proj-alpha/networkpolicies/alpha-allow-intra",
				"/apis/networking.k8s.io/v1/namespaces/supadupa-proj-alpha/networkpolicies/alpha-allow-ingress-controller",
				"/apis/networking.k8s.io/v1/namespaces/supadupa-proj-alpha/networkpolicies/alpha-allow-egress":
				if payload["kind"] != "NetworkPolicy" {
					t.Fatalf("expected NetworkPolicy, got %#v", payload)
				}
			case "/api/v1/namespaces/supadupa-proj-alpha/resourcequotas/alpha-quota":
				if payload["kind"] != "ResourceQuota" {
					t.Fatalf("expected ResourceQuota, got %#v", payload)
				}
			case "/api/v1/namespaces/supadupa-proj-alpha/limitranges/alpha-limits":
				if payload["kind"] != "LimitRange" {
					t.Fatalf("expected LimitRange, got %#v", payload)
				}
			default:
				t.Fatalf("unexpected isolation apply path %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
	}))
	t.Cleanup(server.Close)

	project := Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec: ProjectSpec{
			Ref: "alpha",
			Services: map[string]ServiceSpec{
				"db":   {Enabled: true, Image: "supabase/postgres:15", Ports: []ServicePortSpec{{Name: "postgres", Port: 5432}}},
				"kong": {Enabled: true, Image: "kong/kong:3", Ports: []ServicePortSpec{{Name: "http", Port: 8000}}, Ingress: &ServiceIngressSpec{Enabled: true, Host: "alpha.example.com"}},
			},
		},
	}
	iso := isolationForProject(project, "alpha", IsolationConfig{
		NetworkPolicyEnabled: true,
		DefaultQuota:         &ProjectQuotaDefaults{Hard: map[string]string{"pods": "50"}},
		DefaultLimits:        &ProjectLimitDefaults{Default: map[string]string{"cpu": "500m"}},
	})
	client := &KubernetesClient{BaseURL: server.URL, Token: "token", Client: server.Client()}
	if err := client.ApplyProjectIsolation(context.Background(), "supadupa-proj-alpha", project, iso); err != nil {
		t.Fatalf("apply isolation: %v", err)
	}

	joined := strings.Join(seen, "\n")
	for _, expected := range []string{
		"PATCH /api/v1/namespaces/supadupa-proj-alpha/serviceaccounts/alpha-runtime?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /apis/networking.k8s.io/v1/namespaces/supadupa-proj-alpha/networkpolicies/alpha-default-deny?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /apis/networking.k8s.io/v1/namespaces/supadupa-proj-alpha/networkpolicies/alpha-allow-egress?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /api/v1/namespaces/supadupa-proj-alpha/resourcequotas/alpha-quota?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
		"PATCH /api/v1/namespaces/supadupa-proj-alpha/limitranges/alpha-limits?fieldManager=supadupa-operator&force=true application/apply-patch+yaml",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected request %q in:\n%s", expected, joined)
		}
	}
}

func TestKubernetesClientObservesUnavailableWorkloadResources(t *testing.T) {
	expectedSelector := "app.kubernetes.io/managed-by=supadupa-operator,supadupa.dev/project-ref=alpha"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		if r.URL.Query().Get("labelSelector") != expectedSelector {
			t.Fatalf("unexpected label selector %q", r.URL.Query().Get("labelSelector"))
		}
		switch r.URL.Path {
		case "/apis/apps/v1/namespaces/runtime/deployments":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-auth"},"spec":{"replicas":2},"status":{"availableReplicas":1,"readyReplicas":1,"updatedReplicas":2}}]}`))
		case "/api/v1/namespaces/runtime/persistentvolumeclaims":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"alpha-auth-data"},"status":{"phase":"Pending"}}]}`))
		default:
			t.Fatalf("unexpected observe path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	replicas := int32(2)
	project := Project{
		Metadata: ObjectMeta{Name: "alpha"},
		Spec: ProjectSpec{
			Ref: "alpha",
			Services: map[string]ServiceSpec{
				"auth": {
					Enabled:  true,
					Image:    "example/auth:v1",
					Replicas: &replicas,
					Volumes:  []ServiceVolumeSpec{{Name: "data", MountPath: "/data", Size: "1Gi"}},
				},
			},
		},
	}
	resources, err := resourcesForProject(project)
	if err != nil {
		t.Fatal(err)
	}
	client := &KubernetesClient{BaseURL: server.URL, Token: "token", Client: server.Client()}
	observation, err := client.ObserveProjectResources(context.Background(), "runtime", resources)
	if err != nil {
		t.Fatalf("observe resources: %v", err)
	}
	if !observation.Checked || observation.Ready {
		t.Fatalf("expected checked unavailable observation, got %#v", observation)
	}
	if !strings.Contains(observation.Message, "deployment/alpha-auth available 1/2") ||
		!strings.Contains(observation.Message, "persistentvolumeclaim/alpha-auth-data phase Pending") {
		t.Fatalf("unexpected observation message %#v", observation)
	}
}
