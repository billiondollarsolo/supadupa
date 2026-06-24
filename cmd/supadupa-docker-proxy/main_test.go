package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerProxyAllowedComposeRoutes(t *testing.T) {
	allowed := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/_ping"},
		{http.MethodHead, "/_ping"},
		{http.MethodGet, "/v1.45/containers/json"},
		{http.MethodPost, "/v1.45/containers/create"},
		{http.MethodPost, "/containers/abc/start"},
		{http.MethodPost, "/containers/abc/exec"},
		{http.MethodPost, "/containers/abc/rename"},
		{http.MethodGet, "/exec/abc/json"},
		{http.MethodPost, "/exec/abc/start"},
		{http.MethodGet, "/images/json"},
		{http.MethodPost, "/images/create"},
		{http.MethodGet, "/networks"},
		{http.MethodPost, "/networks/create"},
		{http.MethodPost, "/networks/abc/connect"},
		{http.MethodGet, "/volumes"},
		{http.MethodPost, "/volumes/create"},
		{http.MethodGet, "/events"},
	}
	for _, request := range allowed {
		if !dockerProxyAllowed(request.method, request.path) {
			t.Fatalf("expected %s %s to be allowed", request.method, request.path)
		}
	}
}

func TestDockerProxyBlocksAdministrativeRoutes(t *testing.T) {
	blocked := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/secrets"},
		{http.MethodPost, "/secrets/create"},
		{http.MethodGet, "/plugins"},
		{http.MethodPost, "/plugins/pull"},
		{http.MethodGet, "/swarm"},
		{http.MethodPost, "/swarm/init"},
		{http.MethodPost, "/build"},
		{http.MethodPost, "/containers/abc/archive"},
		{http.MethodGet, "/containers/abc/archive"},
		{http.MethodPost, "/containers/abc/foo/start"},
		{http.MethodDelete, "/containers/abc/archive"},
		{http.MethodPost, "/containers/%2F/start"},
		{http.MethodGet, "/images/abc/history"},
		{http.MethodGet, "/images/search"},
		{http.MethodDelete, "/images/abc"},
		{http.MethodGet, "/exec/abc/archive"},
		{http.MethodPost, "/exec/abc/foo/start"},
		{http.MethodPost, "/networks/abc/foo/connect"},
		{http.MethodGet, "/networks/abc/foo"},
		{http.MethodDelete, "/volumes/project-data/foo"},
		{http.MethodPost, "/system/prune"},
	}
	for _, request := range blocked {
		if dockerProxyAllowed(request.method, request.path) {
			t.Fatalf("expected %s %s to be blocked", request.method, request.path)
		}
	}
}

func TestDockerContainerCreateValidationAllowsSupadupaComposePayload(t *testing.T) {
	t.Setenv("SUPADUPA_PROJECT_HOST_ROOT", "/root/supadupa/runtime/projects")
	request := newDockerCreateRequest(`{
		"Labels": {"com.docker.compose.project": "alpha"},
		"HostConfig": {
			"Binds": [
				"/root/supadupa/runtime/projects/alpha/functions:/home/deno/functions:ro",
				"/root/supadupa/runtime/projects/alpha/pg_hba.conf:/etc/postgresql/pg_hba.conf:ro",
				"alpha_db-data:/var/lib/postgresql/data",
				"./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro"
			],
			"Mounts": [
				{"Type": "volume", "Source": "alpha_storage-data", "Target": "/var/lib/storage"},
				{"Type": "tmpfs", "Target": "/tmp"}
			],
			"NetworkMode": "alpha_internal"
		}
	}`)
	if err := validateDockerProxyRequest(request, ""); err != nil {
		t.Fatalf("expected normal Supadupa Compose create payload to pass: %v", err)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read restored request body: %v", err)
	}
	if !strings.Contains(string(body), "alpha") {
		t.Fatalf("expected validation to restore request body, got %q", string(body))
	}
}

func TestDockerContainerCreateValidationBlocksHostTakeoverPayloads(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing compose label",
			body: `{"HostConfig": {}}`,
		},
		{
			name: "privileged",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Privileged":true}}`,
		},
		{
			name: "host network",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"NetworkMode":"host"}}`,
		},
		{
			name: "docker socket bind",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Binds":["/var/run/docker.sock:/var/run/docker.sock"]}}`,
		},
		{
			name: "root bind",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Mounts":[{"Type":"bind","Source":"/","Target":"/host"}]}}`,
		},
		{
			name: "root sibling bind",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Mounts":[{"Type":"bind","Source":"/root/supadupa/runtime/projects/beta/pg_hba.conf","Target":"/etc/postgresql/pg_hba.conf"}]}}`,
		},
		{
			name: "out-of-root absolute bind",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Binds":["/srv/supadupa/runtime/projects/alpha/functions:/home/deno/functions"]}}`,
		},
		{
			name: "docker volumes escape",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Mounts":[{"Type":"bind","Source":"/var/lib/docker/volumes","Target":"/v"}]}}`,
		},
		{
			name: "home directory escape",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Binds":["/home/ops/.ssh:/keys:ro"]}}`,
		},
		{
			name: "relative traversal bind",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Binds":["../beta/pg_hba.conf:/etc/postgresql/pg_hba.conf"]}}`,
		},
		{
			name: "bare named volume",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Binds":["db-data:/var/lib/postgresql/data"]}}`,
		},
		{
			name: "cross-project named volume",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Binds":["beta_db-data:/var/lib/postgresql/data"]}}`,
		},
		{
			name: "cross-project structured volume",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Mounts":[{"Type":"volume","Source":"beta_db-data","Target":"/var/lib/postgresql/data"}]}}`,
		},
		{
			name: "volumes from sibling container",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"VolumesFrom":["beta_db_1"]}}`,
		},
		{
			name: "device mapping",
			body: `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Devices":[{"PathOnHost":"/dev/kvm"}]}}`,
		},
		{
			name: "platform compose project",
			body: `{"Labels":{"com.docker.compose.project":"supadupa"},"HostConfig":{}}`,
		},
		{
			name: "invalid project label",
			body: `{"Labels":{"com.docker.compose.project":"../../host"},"HostConfig":{}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateDockerProxyRequest(newDockerCreateRequest(tt.body), ""); err == nil {
				t.Fatal("expected container create validation to reject payload")
			}
		})
	}
}

func TestDockerProxyRequiresScopedEventAndImageRequests(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	seen := make(chan string, 8)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen <- r.Method + " " + r.URL.EscapedPath()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	proxy := dockerProxyHandler(socketPath)
	blocked := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1.45/events", ""},
		{http.MethodGet, "/v1.45/events?" + dockerFiltersQuery("label", "com.docker.compose.project=supadupa"), ""},
		{http.MethodGet, "/v1.45/events?" + dockerFiltersQuery("label", "com.docker.compose.project=alpha", "com.docker.compose.project=supadupa"), ""},
		{http.MethodGet, "/v1.45/images/json", ""},
		{http.MethodGet, "/v1.45/images/json?" + dockerFiltersQuery("reference", "*"), ""},
		{http.MethodGet, "/v1.45/images/http:%2F%2Fevil.example%2Fimage/json", ""},
		{http.MethodGet, "/v1.45/images/public.ecr.aws%2Fsupabase%2Fpostgres:*/json", ""},
		{http.MethodPost, "/v1.45/images/create?fromSrc=https%3A%2F%2Fexample.com%2Fimage.tar&repo=alpha", ""},
		{http.MethodPost, "/v1.45/images/create?fromImage=postgres", `{"unexpected":true}`},
		{http.MethodPost, "/v1.45/images/create?fromImage=http%3A%2F%2Fevil.example%2Fimage", ""},
		{http.MethodPost, "/v1.45/images/create?fromImage=public.ecr.aws%2Fsupabase%2Fpostgres%3A*", ""},
		{http.MethodPost, "/v1.45/images/create?fromImage=public.ecr.aws%2Fsupabase%2Fpostgres&tag=bad%2Ftag", ""},
	}
	for _, request := range blocked {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
		proxy.ServeHTTP(response, req)
		if response.Code != http.StatusForbidden {
			t.Fatalf("expected %s %s to be forbidden, got %d: %s", request.method, request.path, response.Code, response.Body.String())
		}
		select {
		case got := <-seen:
			t.Fatalf("blocked scoped request reached upstream Docker socket: %s", got)
		default:
		}
	}

	allowed := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1.45/events?" + dockerFiltersQuery("label", "com.docker.compose.project=alpha")},
		{http.MethodGet, "/v1.45/images/json?" + dockerFiltersQuery("reference", "public.ecr.aws/supabase/postgres:*")},
		{http.MethodGet, "/v1.45/images/public.ecr.aws/supabase/postgres:15/json"},
		{http.MethodPost, "/v1.45/images/create?fromImage=public.ecr.aws%2Fsupabase%2Fpostgres&tag=15"},
		{http.MethodPost, "/v1.45/images/create?fromImage=registry.example.test%3A5000%2Fsupabase%2Fpostgres%40sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	for _, request := range allowed {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(request.method, request.path, nil)
		proxy.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("expected %s %s to forward, got %d: %s", request.method, request.path, response.Code, response.Body.String())
		}
		if !upstreamSaw(seen, request.method+" "+strings.SplitN(request.path, "?", 2)[0]) {
			t.Fatalf("upstream did not see %s %s", request.method, request.path)
		}
	}
}

func TestDockerImageReferenceValidation(t *testing.T) {
	for _, reference := range []string{
		"postgres",
		"supabase/postgres:15.8.1.060",
		"registry.example.test:5000/supabase/postgres@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		if !dockerImagePullReferenceAllowed(reference) {
			t.Fatalf("expected pull reference %q to be allowed", reference)
		}
	}
	if !dockerImageFilterReferenceAllowed("public.ecr.aws/supabase/postgres:*") {
		t.Fatal("expected image list wildcard reference filter to be allowed")
	}
	for _, reference := range []string{
		"",
		"http://evil.example/image",
		"public.ecr.aws/supabase/postgres:*",
		"../postgres",
		"postgres?tag=latest",
		"postgres%3Alatest",
		"registry.example.test//postgres",
	} {
		if dockerImagePullReferenceAllowed(reference) {
			t.Fatalf("expected pull reference %q to be rejected", reference)
		}
	}
	for _, reference := range []string{
		"*",
		"*:latest",
		"public.ecr.aws/*",
		"postgres:**",
		"http://evil.example/image:*",
	} {
		if dockerImageFilterReferenceAllowed(reference) {
			t.Fatalf("expected image filter reference %q to be rejected", reference)
		}
	}
}

func TestDockerProxyForwardsComposeLifecycleRoutesOverUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	seen := make(chan string, 16)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.EscapedPath() {
			case "/v1.45/containers/json":
				seen <- r.Method + " " + r.URL.EscapedPath()
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[{"Id":"abc","Labels":{"com.docker.compose.project":"alpha"}}]`))
				return
			case "/containers/abc/json":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Config":{"Labels":{"com.docker.compose.project":"alpha"}}}`))
				return
			case "/networks/abc":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Labels":{"com.docker.compose.project":"alpha"}}`))
				return
			case "/exec/abc/json":
				seen <- r.Method + " " + r.URL.EscapedPath()
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ContainerID":"abc"}`))
				return
			}
			seen <- r.Method + " " + r.URL.EscapedPath()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	proxy := dockerProxyHandler(socketPath)
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1.45/_ping", ""},
		{http.MethodGet, "/v1.45/containers/json", ""},
		{http.MethodPost, "/v1.45/containers/create", `{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"NetworkMode":"alpha_internal"}}`},
		{http.MethodPost, "/v1.45/containers/abc/start", ""},
		{http.MethodPost, "/v1.45/containers/abc/exec", `{"Cmd":["true"]}`},
		{http.MethodGet, "/v1.45/exec/abc/json", ""},
		{http.MethodPost, "/v1.45/exec/abc/start", ""},
		{http.MethodPost, "/v1.45/networks/create", `{"Labels":{"com.docker.compose.project":"alpha"}}`},
		{http.MethodPost, "/v1.45/networks/abc/connect", `{"Container":"abc"}`},
		{http.MethodPost, "/v1.45/volumes/create", `{"Labels":{"com.docker.compose.project":"alpha"}}`},
	} {
		response := httptest.NewRecorder()
		body := strings.NewReader(request.body)
		req := httptest.NewRequest(request.method, request.path, body)
		proxy.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("expected %s %s to forward, got %d: %s", request.method, request.path, response.Code, response.Body.String())
		}
		if !upstreamSaw(seen, request.method+" "+request.path) {
			t.Fatalf("upstream did not see %s %s", request.method, request.path)
		}
	}
}

func TestDockerProxyFiltersContainerListToSupadupaProjects(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.EscapedPath() != "/v1.45/containers/json" {
				t.Fatalf("unexpected upstream path: %s", r.URL.EscapedPath())
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{"Id":"project","Names":["/project-api"],"Labels":{"com.docker.compose.project":"alpha"}},
				{"Id":"platform","Names":["/supadupavisor"],"Labels":{"com.docker.compose.project":"supadupa"}},
				{"Id":"unlabeled","Names":["/unrelated"],"Labels":{}}
			]`))
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1.45/containers/json", nil)
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("expected filtered container list to succeed, got %d: %s", response.Code, response.Body.String())
	}
	var containers []dockerContainerListItem
	if err := json.Unmarshal(response.Body.Bytes(), &containers); err != nil {
		t.Fatalf("decode filtered container list: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected one project container after filtering, got %#v", containers)
	}
	if containers[0].ID != "project" {
		t.Fatalf("expected project container to remain, got %#v", containers[0])
	}
}

func TestDockerProxyFiltersNetworkListToSupadupaProjects(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.EscapedPath() != "/v1.45/networks" {
				t.Fatalf("unexpected upstream path: %s", r.URL.EscapedPath())
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{"Id":"project","Name":"alpha_default","Driver":"bridge","Labels":{"com.docker.compose.project":"alpha"}},
				{"Id":"platform","Name":"supadupa_default","Driver":"bridge","Labels":{"com.docker.compose.project":"supadupa"}},
				{"Id":"host","Name":"bridge","Driver":"bridge","Labels":{}}
			]`))
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1.45/networks", nil)
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("expected filtered network list to succeed, got %d: %s", response.Code, response.Body.String())
	}
	var networks []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &networks); err != nil {
		t.Fatalf("decode filtered network list: %v", err)
	}
	if len(networks) != 1 {
		t.Fatalf("expected one project network after filtering, got %#v", networks)
	}
	if networks[0]["Name"] != "alpha_default" || networks[0]["Driver"] != "bridge" {
		t.Fatalf("expected project network fields to remain, got %#v", networks[0])
	}
}

func TestDockerProxyFiltersVolumeListToSupadupaProjects(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.EscapedPath() != "/v1.45/volumes" {
				t.Fatalf("unexpected upstream path: %s", r.URL.EscapedPath())
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"Volumes": [
					{"Name":"alpha_db-data","Driver":"local","Labels":{"com.docker.compose.project":"alpha"}},
					{"Name":"supadupa_meta-db-data","Driver":"local","Labels":{"com.docker.compose.project":"supadupa"}},
					{"Name":"host-data","Driver":"local","Labels":{}}
				],
				"Warnings": ["kept"]
			}`))
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1.45/volumes", nil)
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("expected filtered volume list to succeed, got %d: %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode filtered volume list: %v", err)
	}
	volumes, ok := payload["Volumes"].([]any)
	if !ok {
		t.Fatalf("expected volume array, got %#v", payload["Volumes"])
	}
	if len(volumes) != 1 {
		t.Fatalf("expected one project volume after filtering, got %#v", volumes)
	}
	volume, ok := volumes[0].(map[string]any)
	if !ok {
		t.Fatalf("expected volume object, got %#v", volumes[0])
	}
	if volume["Name"] != "alpha_db-data" || volume["Driver"] != "local" {
		t.Fatalf("expected project volume fields to remain, got %#v", volume)
	}
	if warnings, ok := payload["Warnings"].([]any); !ok || len(warnings) != 1 || warnings[0] != "kept" {
		t.Fatalf("expected volume warnings to remain, got %#v", payload["Warnings"])
	}
}

func TestDockerProxyBlocksPrivilegedExecCreateBeforeUpstreamMutation(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	mutated := make(chan struct{}, 1)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.EscapedPath() == "/containers/abc/json" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Config":{"Labels":{"com.docker.compose.project":"alpha"}}}`))
				return
			}
			mutated <- struct{}{}
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1.45/containers/abc/exec", strings.NewReader(`{"Privileged":true}`))
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected privileged exec create to be forbidden, got %d: %s", response.Code, response.Body.String())
	}
	select {
	case <-mutated:
		t.Fatal("privileged exec create reached upstream Docker socket")
	default:
	}
}

func TestDockerProxyBlocksUnlabeledContainerBeforeUpstreamMutation(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	mutated := make(chan struct{}, 1)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.EscapedPath() == "/containers/host-container/json" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Config":{"Labels":{"com.docker.compose.project":"supadupa"}}}`))
				return
			}
			mutated <- struct{}{}
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1.45/containers/host-container/stop", nil)
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected unlabeled container mutation to be forbidden, got %d: %s", response.Code, response.Body.String())
	}
	select {
	case <-mutated:
		t.Fatal("unlabeled container mutation reached upstream Docker socket")
	default:
	}
}

func TestDockerProxyBlocksUnlabeledNetworkBeforeUpstreamMutation(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	mutated := make(chan struct{}, 1)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.EscapedPath() == "/networks/platform_default" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Labels":{"com.docker.compose.project":"supadupa"}}`))
				return
			}
			mutated <- struct{}{}
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1.45/networks/platform_default/connect", strings.NewReader(`{"Container":"abc"}`))
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected unlabeled network mutation to be forbidden, got %d: %s", response.Code, response.Body.String())
	}
	select {
	case <-mutated:
		t.Fatal("unlabeled network mutation reached upstream Docker socket")
	default:
	}
}

func TestDockerProxyBlocksNetworkMutationWithUnlabeledContainerBeforeUpstream(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	mutated := make(chan struct{}, 1)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.EscapedPath() {
			case "/networks/alpha_default":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Labels":{"com.docker.compose.project":"alpha"}}`))
				return
			case "/containers/host-container/json":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Config":{"Labels":{"com.docker.compose.project":"supadupa"}}}`))
				return
			}
			mutated <- struct{}{}
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1.45/networks/alpha_default/connect", strings.NewReader(`{"Container":"host-container"}`))
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected network mutation with unlabeled container to be forbidden, got %d: %s", response.Code, response.Body.String())
	}
	select {
	case <-mutated:
		t.Fatal("network mutation with unlabeled container reached upstream Docker socket")
	default:
	}
}

func TestDockerProxyAllowsEdgeRouterConnectToProjectNetwork(t *testing.T) {
	t.Setenv("SUPADUPA_EDGE_ROUTER_CONTAINER", "supadupa-edge-router-1")
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	forwarded := make(chan struct{}, 1)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.EscapedPath() == "/networks/alpha-edge" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Labels":{"com.docker.compose.project":"alpha","supadupa.role":"edge"}}`))
				return
			}
			// The edge-router container is allowed without a container inspect, so
			// reaching upstream here means the connect was permitted.
			forwarded <- struct{}{}
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() { _ = upstream.Serve(listener) }()
	t.Cleanup(func() { _ = upstream.Close() })

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1.45/networks/alpha-edge/connect", strings.NewReader(`{"Container":"supadupa-edge-router-1"}`))
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("expected edge-router connect to a project network to be allowed, got %d: %s", response.Code, response.Body.String())
	}
	select {
	case <-forwarded:
	default:
		t.Fatal("edge-router connect did not reach upstream Docker socket")
	}
}

func TestDockerProxyAllowsSharedIngressNetworkReadAndProjectContainerConnect(t *testing.T) {
	t.Setenv("SUPADUPA_INGRESS_NETWORK", "supadupa-ingress")
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	seen := make(chan string, 8)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen <- r.Method + " " + r.URL.EscapedPath()
			switch r.URL.EscapedPath() {
			case "/networks/supadupa-ingress":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Name":"supadupa-ingress","Labels":{}}`))
			case "/containers/abc/json":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Config":{"Labels":{"com.docker.compose.project":"alpha"}}}`))
			default:
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			}
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1.45/networks/supadupa-ingress", nil)
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("expected shared ingress network read to pass, got %d: %s", response.Code, response.Body.String())
	}
	if !upstreamSaw(seen, "GET /v1.45/networks/supadupa-ingress") {
		t.Fatal("expected shared ingress network read to be forwarded")
	}

	response = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1.45/networks/supadupa-ingress/connect", strings.NewReader(`{"Container":"abc"}`))
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("expected shared ingress connect for labeled project container to pass, got %d: %s", response.Code, response.Body.String())
	}
	if !upstreamSaw(seen, "POST /v1.45/networks/supadupa-ingress/connect") {
		t.Fatal("expected shared ingress network connect to be forwarded")
	}
}

func TestDockerProxyBlocksUnlabeledNetworkAndVolumeReadsBeforeForward(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	seen := make(chan string, 4)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen <- r.URL.EscapedPath()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"Labels":{"com.docker.compose.project":"supadupa"}}`))
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	for _, path := range []string{"/v1.45/networks/platform_default", "/v1.45/volumes/platform-data"} {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		dockerProxyHandler(socketPath).ServeHTTP(response, req)
		if response.Code != http.StatusForbidden {
			t.Fatalf("expected unlabeled object read %s to be forbidden, got %d: %s", path, response.Code, response.Body.String())
		}
		if !upstreamSaw(seen, strings.TrimPrefix(path, "/v1.45")) {
			t.Fatalf("expected inspect preflight for %s", path)
		}
		select {
		case extra := <-seen:
			t.Fatalf("unlabeled object read was forwarded after preflight: %s", extra)
		default:
		}
	}
}

func TestDockerProxyAllowsMissingObjectReadBeforeComposeCreate(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	seen := 0
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || (r.URL.EscapedPath() != "/volumes/alpha_db-data" && r.URL.EscapedPath() != "/v1.45/volumes/alpha_db-data") {
				t.Fatalf("unexpected upstream request %s %s", r.Method, r.URL.EscapedPath())
			}
			seen++
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1.45/volumes/alpha_db-data", nil)
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected missing volume read to be forwarded as 404, got %d: %s", response.Code, response.Body.String())
	}
	if seen < 2 {
		t.Fatalf("expected inspect and forwarded read attempts, got %d", seen)
	}
}

func TestDockerProxyRetriesTransientInspectNotFound(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	inspectAttempts := 0
	seen := make(chan string, 4)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.EscapedPath() == "/networks/alpha_internal" {
				inspectAttempts++
				if inspectAttempts == 1 {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"Labels":{"com.docker.compose.project":"alpha"}}`))
				return
			}
			seen <- r.Method + " " + r.URL.EscapedPath()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1.45/networks/alpha_internal", nil)
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("expected transient inspect 404 to retry and forward, got %d: %s", response.Code, response.Body.String())
	}
	if inspectAttempts != 2 {
		t.Fatalf("expected two inspect attempts, got %d", inspectAttempts)
	}
	if !upstreamSaw(seen, "GET /v1.45/networks/alpha_internal") {
		t.Fatal("expected network read to be forwarded after successful inspect retry")
	}
}

func upstreamSaw(seen <-chan string, expected string) bool {
	for {
		select {
		case got := <-seen:
			if got == expected {
				return true
			}
		default:
			return false
		}
	}
}

func TestDockerProxyBlocksForbiddenRoutesBeforeUpstream(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake docker socket: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
	})

	seen := make(chan struct{}, 1)
	upstream := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen <- struct{}{}
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() {
		_ = upstream.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = upstream.Close()
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1.45/containers/create", strings.NewReader(`{"Labels":{"com.docker.compose.project":"alpha"},"HostConfig":{"Privileged":true}}`))
	dockerProxyHandler(socketPath).ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden route to be blocked, got %d: %s", response.Code, response.Body.String())
	}
	select {
	case <-seen:
		t.Fatal("forbidden request reached upstream Docker socket")
	default:
	}
}

func newDockerCreateRequest(body string) *http.Request {
	request, err := http.NewRequest(http.MethodPost, "/v1.45/containers/create", strings.NewReader(body))
	if err != nil {
		return nil
	}
	return request
}

func TestNormalizeDockerAPIPath(t *testing.T) {
	for input, expected := range map[string]string{
		"/v1.45/containers/json": "/containers/json",
		"/v1/containers/json":    "/containers/json",
		"/containers/json":       "/containers/json",
		"":                       "/",
	} {
		if got := normalizeDockerAPIPath(input); got != expected {
			t.Fatalf("normalizeDockerAPIPath(%q) = %q, want %q", input, got, expected)
		}
	}
}

func dockerFiltersQuery(name string, values ...string) string {
	payload, _ := json.Marshal(map[string][]string{name: values})
	return "filters=" + url.QueryEscape(string(payload))
}
