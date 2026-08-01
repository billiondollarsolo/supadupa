package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"supadupa2026/internal/control"
	composeprovisioner "supadupa2026/internal/provisioner/compose"
)

func TestHealth(t *testing.T) {
	server := NewServer(Config{Provisioner: composeprovisioner.New()})
	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
}

func TestCORSOrigins(t *testing.T) {
	server := NewServer(Config{Provisioner: composeprovisioner.New()})
	for _, origin := range []string{"http://127.0.0.1:3001", "http://127.0.0.1:5174"} {
		request := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()

		server.Handler.ServeHTTP(response, request)

		if response.Code != http.StatusNoContent {
			t.Fatalf("expected preflight status %d, got %d", http.StatusNoContent, response.Code)
		}
		if got := response.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("expected default local admin origin %s to be allowed, got %q", origin, got)
		}
		if got := response.Header().Get("Vary"); got != "Origin" {
			t.Fatalf("expected Vary Origin header, got %q", got)
		}
		if got := response.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "PATCH") {
			t.Fatalf("expected CORS preflight to allow PATCH for SCIM updates, got %q", got)
		}
		if got := response.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-Supadupa-Browser") {
			t.Fatalf("expected CORS preflight to allow browser auth marker, got %q", got)
		}
	}
}

func TestDefaultCORSOriginsIncludeConfiguredAdminHost(t *testing.T) {
	t.Setenv("SUPADUPA_ADMIN_HOST", "admin.example.com")
	server := NewServer(Config{Provisioner: composeprovisioner.New()})

	request := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
	request.Header.Set("Origin", "https://admin.example.com")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("expected admin host origin to be allowed, got %q", got)
	}

	localRequest := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
	localRequest.Header.Set("Origin", "http://localhost:3000")
	localResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(localResponse, localRequest)
	if got := localResponse.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected configured admin host to suppress local defaults, got %q", got)
	}
}

func TestListStackReleasesReturnsConfiguredManifests(t *testing.T) {
	t.Setenv("SUPADUPA_SUPPORTED_STACK_VERSIONS", "2026.06.06")
	t.Setenv("SUPADUPA_STACK_RELEASES_JSON", `{
		"2026.06.06": {
			"postgres": "pg-tag",
			"kong": "kong-tag",
			"studio": "studio-tag",
			"postgres_meta": "meta-tag",
			"auth": "auth-tag",
			"rest": "rest-tag",
			"realtime": "realtime-tag",
			"storage": "storage-tag",
			"imgproxy": "imgproxy-tag",
			"edge_runtime": "edge-tag",
			"pooler": "pooler-tag",
			"analytics": "analytics-tag",
			"vector": "vector-tag"
		}
	}`)
	store := control.NewMemoryStore()
	user, err := store.CreateUser(context.Background(), control.CreateUserRequest{Email: "admin@example.com", Password: "super-secure", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	auth := control.NewAuthService("stack-release-test-secret")
	token, err := auth.Issue(user, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store, Auth: auth, Provisioner: fakeProvisioner{}, AuthRequired: true})

	unauthorized := perform(server, http.MethodGet, "/v1/stack-releases", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}
	response := performWithToken(server, http.MethodGet, "/v1/stack-releases", "", token)
	if response.Code != http.StatusOK {
		t.Fatalf("expected release list status 200, got %d: %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`"version":"2026.06.06"`,
		`"postgres":"pg-tag"`,
		`"kong":"kong-tag"`,
		`"studio":"studio-tag"`,
		`"postgres_meta":"meta-tag"`,
		`"auth":"auth-tag"`,
		`"rest":"rest-tag"`,
		`"realtime":"realtime-tag"`,
		`"storage":"storage-tag"`,
		`"edge_runtime":"edge-tag"`,
		`"pooler":"pooler-tag"`,
		`"analytics":"analytics-tag"`,
		`"vector":"vector-tag"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("expected release response to include %s: %s", expected, response.Body.String())
		}
	}
}

func TestConfiguredCORSOriginsOverrideDefaults(t *testing.T) {
	server := NewServer(Config{
		Provisioner: composeprovisioner.New(),
		CORSOrigins: []string{"https://admin.example.com"},
	})

	allowedRequest := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
	allowedRequest.Header.Set("Origin", "https://admin.example.com")
	allowedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(allowedResponse, allowedRequest)
	if got := allowedResponse.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("expected configured origin to be allowed, got %q", got)
	}

	defaultRequest := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
	defaultRequest.Header.Set("Origin", "http://localhost:3000")
	defaultResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(defaultResponse, defaultRequest)
	if got := defaultResponse.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected configured origins to override defaults, got %q", got)
	}
}

func TestRequestBodyLimitRejectsDeclaredOversizedBody(t *testing.T) {
	server := NewServer(Config{Store: control.NewMemoryStore(), AuthRequired: true})
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	request.ContentLength = maxRequestBodyBytes + 1
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "request body too large") {
		t.Fatalf("expected oversized body rejection, got %d: %s", response.Code, response.Body.String())
	}
}

func TestJSONBodyLimitRejectsDeclaredOversizedJSONBody(t *testing.T) {
	server := NewServer(Config{Store: control.NewMemoryStore(), AuthRequired: true})
	body := `{"email":"admin@example.com","password":"` + strings.Repeat("a", defaultJSONBodyBytes) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if len(body) >= maxRequestBodyBytes {
		t.Fatalf("test body should stay below global request cap")
	}
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "request body too large") {
		t.Fatalf("expected oversized JSON body rejection, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCORSRejectsMutatingDisallowedOrigin(t *testing.T) {
	server := NewServer(Config{Store: control.NewMemoryStore(), AuthRequired: true, CORSOrigins: []string{"https://admin.example.com"}})
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"secret"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example.com")
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "origin is not allowed") {
		t.Fatalf("expected disallowed origin rejection, got %d: %s", response.Code, response.Body.String())
	}
}

func TestRuntimeConfigRequiresAdminAndRedactsOperationalCommands(t *testing.T) {
	t.Setenv("SUPADUPA_COMPOSE_APPLY", "true")
	t.Setenv("SUPADUPA_COMPOSE_BACKUP_DEFAULTS", "true")
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "true")
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "true")
	t.Setenv("SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE", "true")
	t.Setenv("SUPADUPA_LOGICAL_BACKUP_COMMAND", "secret logical command")
	t.Setenv("SUPADUPA_PITR_RESTORE_COMMAND", "secret pitr command")

	store := control.NewMemoryStore()
	user, err := store.CreateUser(context.Background(), control.CreateUserRequest{Email: "admin@example.com", Password: "super-secure", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	auth := control.NewAuthService("runtime-config-test-secret")
	token, err := auth.Issue(user, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store, Auth: auth, Provisioner: fakeProvisioner{}, AuthRequired: true})

	unauthorized := perform(server, http.MethodGet, "/v1/runtime-config", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized runtime config status, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}
	response := performWithToken(server, http.MethodGet, "/v1/runtime-config", "", token)
	if response.Code != http.StatusOK {
		t.Fatalf("expected runtime config 200, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{
		`"provisioner":"fake"`,
		`"compose":true`,
		`"compose_defaults":true`,
		`"logical_configured":true`,
		`"pitr_restore_configured":true`,
		`"require_recovery_ready_targets":true`,
		`"require_durable_backup":true`,
		`"failure_auto_restore":true`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected runtime config to contain %s, got %s", expected, body)
		}
	}
	for _, unexpected := range []string{"secret logical command", "secret pitr command", "SUPADUPA_LOGICAL_BACKUP_COMMAND", "SUPADUPA_PITR_RESTORE_COMMAND"} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("runtime config leaked %q: %s", unexpected, body)
		}
	}
}

func TestPlatformDefaultsAPIUpdatesAndAppliesToProjectCreate(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{
		Store:        store,
		Provisioner:  provisioner,
		AuthRequired: true,
	})

	unauthorized := perform(server, http.MethodGet, "/v1/settings/defaults", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized settings status, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap status 201, got %d: %s", bootstrap.Code, bootstrap.Body.String())
	}
	token := extractString(t, bootstrap.Body.String(), "token")

	invalidStack := performWithToken(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.example.com","stack_version":"2026.06.05","profile":"essential","resource_tier":"custom","backup_schedule":"hourly"}`, token)
	if invalidStack.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid stack defaults 400, got %d: %s", invalidStack.Code, invalidStack.Body.String())
	}

	update := performWithToken(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.example.com","stack_version":"15.8.1.060","profile":"essential","resource_tier":"custom","backup_schedule":"hourly","feature_flags":{"single_org_mode":false,"read_replicas":true,"kubernetes_operator":true},"smtp":{"enabled":true,"host":"smtp.example.com","port":2525,"sender_name":"supadupa","sender_email":"noreply@example.com","username":"apikey","password_handle":"secret://platform/smtp-password","tls_mode":"implicit"}}`, token)
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"domain":"apps.example.com"`) || !strings.Contains(update.Body.String(), `"backup_schedule":"hourly"`) || !strings.Contains(update.Body.String(), `"host":"smtp.example.com"`) || !strings.Contains(update.Body.String(), `"password_handle":"secret://platform/smtp-password"`) || !strings.Contains(update.Body.String(), `"single_org_mode":false`) || !strings.Contains(update.Body.String(), `"read_replicas":true`) || !strings.Contains(update.Body.String(), `"kubernetes_operator":true`) || !strings.Contains(update.Body.String(), `"supabase_cli_compat":true`) {
		t.Fatalf("expected updated defaults: %d %s", update.Code, update.Body.String())
	}
	invalidSMTP := performWithToken(server, http.MethodPut, "/v1/settings/defaults", `{"domain":"apps.example.com","stack_version":"15.8.1.060","profile":"essential","resource_tier":"custom","backup_schedule":"hourly","smtp":{"enabled":true,"host":"smtp.example.com","port":587,"password_handle":"raw","tls_mode":"starttls"}}`, token)
	if invalidSMTP.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid smtp defaults 400, got %d: %s", invalidSMTP.Code, invalidSMTP.Body.String())
	}

	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, token)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("expected org status 201, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"defaults-api","name":"Defaults API"}`, token)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if provisioner.spec.Domain != "apps.example.com" || provisioner.spec.StackVersion != "15.8.1.060" || provisioner.spec.Profile != control.StackProfileEssential || provisioner.spec.ResourceTier != control.ResourceTierCustom || provisioner.spec.CPU <= 0 || provisioner.spec.RAMMB <= 0 || provisioner.spec.DiskGB <= 0 {
		t.Fatalf("expected provisioner spec from defaults, got %#v", provisioner.spec)
	}
	policy, err := store.GetBackupPolicy(context.Background(), "defaults-api")
	if err != nil {
		t.Fatal(err)
	}
	if policy.Schedule != "hourly" {
		t.Fatalf("expected hourly backup policy, got %#v", policy)
	}

	auditResponse := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	if !strings.Contains(auditResponse.Body.String(), `"action":"settings.defaults_update"`) {
		t.Fatalf("expected settings audit event: %s", auditResponse.Body.String())
	}
}

func TestFleetMetricsJSONAndPrometheus(t *testing.T) {
	t.Setenv("SUPADUPA_POSTGRES_ADDR", "127.0.0.1:5432")
	t.Setenv("SUPADUPA_POOLER_ADDR", "127.0.0.1:6543")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	if _, err := store.RecordNodeTelemetry(context.Background(), hostID, control.NodeTelemetrySampleInput{
		Source:             "compose-local-node",
		CPUPercent:         12.5,
		CPUUsedCores:       1,
		CPUCapacityCores:   8,
		MemoryUsedBytes:    4294967296,
		MemoryTotalBytes:   34359738368,
		DiskUsedBytes:      85899345920,
		DiskTotalBytes:     536870912000,
		DiskAvailableBytes: 450971566080,
		NetworkSampled:     true,
		NetworkRxBytes:     1234,
		NetworkTxBytes:     5678,
		SampledAt:          time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record node telemetry: %v", err)
	}
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"metrics-proj","name":"Metrics","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if _, err := store.CreateBackup(context.Background(), control.BackupInput{ProjectRef: "metrics-proj", Kind: "logical", Location: "memory://metrics", SizeBytes: 2048, Status: "completed"}); err != nil {
		t.Fatalf("create metrics backup: %v", err)
	}
	functionResponse := perform(server, http.MethodPost, "/v1/projects/metrics-proj/functions", `{"name":"hello-api","source":"Deno.serve(() => new Response('ok'))"}`)
	if functionResponse.Code != http.StatusCreated {
		t.Fatalf("expected function deploy 201, got %d: %s", functionResponse.Code, functionResponse.Body.String())
	}
	telemetryPayload := `{"source":"compose","cpu_percent":18.5,"memory_bytes":536870912,"memory_limit_bytes":2147483648,"disk_used_bytes":7516192768,"disk_limit_bytes":21474836480,"network_rx_bytes":123,"network_tx_bytes":456,"sampled_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	telemetryResponse := perform(server, http.MethodPost, "/v1/projects/metrics-proj/telemetry", telemetryPayload)
	if telemetryResponse.Code != http.StatusCreated {
		t.Fatalf("expected telemetry status 201, got %d: %s", telemetryResponse.Code, telemetryResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/metrics-proj/metrics", "")
	if projectMetricsResponse.Code != http.StatusOK {
		t.Fatalf("expected project metrics 200, got %d: %s", projectMetricsResponse.Code, projectMetricsResponse.Body.String())
	}
	for _, expected := range []string{
		`"project_ref":"metrics-proj"`,
		`"status":"healthy"`,
		`"function_deployments":1`,
		`"backups":1`,
		`"backup_storage_bytes":2048`,
		`"observed":{"project_ref":"metrics-proj","source":"compose","cpu_percent":18.5`,
		`"memory_bytes":536870912`,
	} {
		if !strings.Contains(projectMetricsResponse.Body.String(), expected) {
			t.Fatalf("expected project metrics value %s: %s", expected, projectMetricsResponse.Body.String())
		}
	}

	historyResponse := perform(server, http.MethodGet, "/v1/projects/metrics-proj/telemetry/history?range=1h&step=15s", "")
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("expected project telemetry history 200, got %d: %s", historyResponse.Code, historyResponse.Body.String())
	}
	for _, expected := range []string{
		`"project_ref":"metrics-proj"`,
		`"step_seconds":15`,
		`"points":[`,
		`"cpu_reservation_percent"`,
		`"memory_reservation_percent"`,
	} {
		if !strings.Contains(historyResponse.Body.String(), expected) {
			t.Fatalf("expected project telemetry history value %s: %s", expected, historyResponse.Body.String())
		}
	}

	jsonResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if jsonResponse.Code != http.StatusOK {
		t.Fatalf("expected metrics json 200, got %d: %s", jsonResponse.Code, jsonResponse.Body.String())
	}
	for _, expected := range []string{
		`"orgs":1`,
		`"hosts":1`,
		`"projects":1`,
		`"healthy":1`,
		`"host_capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}`,
		`"host_used":{"cpu":2,"ram_mb":4096,"disk_gb":40,"projects":1}`,
		`"database_ingress":{"mode":"private","public":false,"postgres_addr":"127.0.0.1:5432","pooler_addr":"127.0.0.1:6543","postgres_public":false,"pooler_public":false`,
		`"node_observed":[{"host_id":"` + hostID + `","source":"compose-local-node","cpu_percent":12.5`,
		`"network_sampled":true,"network_rx_bytes":1234,"network_tx_bytes":5678`,
		`"observed":{"projects_sampled":1,"cpu_percent":18.5,"memory_bytes":536870912`,
		`"function_deployments":1`,
		`"backups":1`,
		`"backup_storage_bytes":2048`,
		`"audit_verified":true`,
	} {
		if !strings.Contains(jsonResponse.Body.String(), expected) {
			t.Fatalf("expected metrics value %s: %s", expected, jsonResponse.Body.String())
		}
	}

	promResponse := perform(server, http.MethodGet, "/metrics", "")
	if promResponse.Code != http.StatusOK {
		t.Fatalf("expected prometheus metrics 200, got %d: %s", promResponse.Code, promResponse.Body.String())
	}
	for _, expected := range []string{
		"supadupa_projects_total 1",
		"supadupa_projects_by_status{status=\"healthy\"} 1",
		"supadupa_host_capacity_cpu 8",
		"supadupa_host_used_cpu 2",
		"supadupa_node_cpu_percent{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 12.5",
		"supadupa_node_memory_used_bytes{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 4294967296",
		"supadupa_node_disk_used_bytes{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 85899345920",
		"supadupa_node_network_sampled{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 1",
		"supadupa_node_network_rx_bytes{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 1234",
		"supadupa_node_network_tx_bytes{host_id=\"" + hostID + "\",source=\"compose-local-node\"} 5678",
		"supadupa_observed_projects 1",
		"supadupa_observed_cpu_percent 18.5",
		"supadupa_observed_memory_bytes 536870912",
		"supadupa_function_deployments_total 1",
		"supadupa_backup_storage_bytes 2048",
		"supadupa_audit_verified 1",
		"supadupa_project_resource_cpu{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",status=\"healthy\"} 2",
		"supadupa_project_logs_total{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",status=\"healthy\"}",
		"supadupa_project_backups_total{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",status=\"healthy\"} 1",
		"supadupa_project_backup_storage_bytes{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",status=\"healthy\"} 2048",
		"supadupa_project_observed_cpu_percent{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",source=\"compose\",status=\"healthy\"} 18.5",
		"supadupa_project_observed_memory_bytes{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",source=\"compose\",status=\"healthy\"} 536870912",
		"supadupa_project_telemetry_sampled_at_unix{org_id=\"" + orgID + "\",project_ref=\"metrics-proj\",resource_tier=\"small\",source=\"compose\",status=\"healthy\"}",
	} {
		if !strings.Contains(promResponse.Body.String(), expected) {
			t.Fatalf("expected prometheus metric %s: %s", expected, promResponse.Body.String())
		}
	}
	if strings.Count(promResponse.Body.String(), "# HELP supadupa_project_resource_cpu ") != 1 {
		t.Fatalf("expected one HELP line for project CPU metric: %s", promResponse.Body.String())
	}
}

func TestProjectTelemetryHistoryRequiresProjectViewer(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}, AuthRequired: true})

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"owner@example.com","password":"super-secure"}`)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", bootstrap.Code, bootstrap.Body.String())
	}
	token := extractString(t, bootstrap.Body.String(), "token")
	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`, token)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("create org failed: %d %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"history-auth","name":"History Auth","domain":"supadupa.test","profile":"full"}`, token)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("create project failed: %d %s", projectResponse.Code, projectResponse.Body.String())
	}

	unauthorized := perform(server, http.MethodGet, "/v1/projects/history-auth/telemetry/history?range=1h", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated history request to be unauthorized, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}
	authorized := performWithToken(server, http.MethodGet, "/v1/projects/history-auth/telemetry/history?range=1h", "", token)
	if authorized.Code != http.StatusOK || !strings.Contains(authorized.Body.String(), `"project_ref":"history-auth"`) {
		t.Fatalf("expected authorized history response, got %d: %s", authorized.Code, authorized.Body.String())
	}
}

func TestDatabaseIngressStatusFromEnv(t *testing.T) {
	private := databaseIngressStatusFromEnv(func(key string) string {
		return map[string]string{
			"SUPADUPA_POSTGRES_ADDR": "127.0.0.1:5432",
			"SUPADUPA_POOLER_ADDR":   "localhost:6543",
		}[key]
	})
	if private.Mode != "private" || private.Public || private.PostgresPublic || private.PoolerPublic || len(private.Warnings) != 0 {
		t.Fatalf("expected private database ingress, got %#v", private)
	}

	public := databaseIngressStatusFromEnv(func(key string) string {
		return map[string]string{
			"SUPADUPA_POSTGRES_ADDR":            "0.0.0.0:5432",
			"SUPADUPA_POOLER_ADDR":              "[::]:6543",
			"SUPADUPA_DB_INGRESS_ALLOWED_CIDRS": "203.0.113.0/24, 2001:db8::/32",
		}[key]
	})
	if public.Mode != "public" || !public.Public || !public.PostgresPublic || !public.PoolerPublic || !public.AllowlistConfigured || len(public.AllowedCIDRs) != 2 {
		t.Fatalf("expected public allowlisted database ingress, got %#v", public)
	}
	for _, warning := range public.Warnings {
		if strings.Contains(warning, "no database ingress CIDR allowlist") {
			t.Fatalf("allowlisted public ingress should not warn about missing allowlist: %#v", public)
		}
	}

	unrestricted := databaseIngressStatusFromEnv(func(key string) string {
		return map[string]string{
			"SUPADUPA_POSTGRES_ADDR": "198.51.100.10:5432",
			"SUPADUPA_POOLER_ADDR":   "127.0.0.1:6543",
		}[key]
	})
	if unrestricted.Mode != "public" || !unrestricted.PostgresPublic || unrestricted.PoolerPublic || len(unrestricted.Warnings) != 1 {
		t.Fatalf("expected public ingress with a single informational warning, got %#v", unrestricted)
	}
}

func TestFleetAdvisorFindingsEndpoint(t *testing.T) {
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "false")
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "false")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"advisor-proj","name":"Advisor","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if _, err := store.UpdateProjectStatus(context.Background(), "advisor-proj", control.ProjectDegraded, "db health check failed"); err != nil {
		t.Fatalf("update project status: %v", err)
	}
	// Mark the project production so posture findings keep their full severity;
	// development projects intentionally downgrade those to info.
	generalConfigResponse := perform(server, http.MethodPut, "/v1/projects/advisor-proj/config/general", `{"config":{"environment":"production"}}`)
	if generalConfigResponse.Code != http.StatusOK {
		t.Fatalf("expected general config update 200, got %d: %s", generalConfigResponse.Code, generalConfigResponse.Body.String())
	}
	databaseConfigResponse := perform(server, http.MethodPut, "/v1/projects/advisor-proj/config/database", `{"config":{"ssl_enforced":"false"}}`)
	if databaseConfigResponse.Code != http.StatusOK {
		t.Fatalf("expected database config update 200, got %d: %s", databaseConfigResponse.Code, databaseConfigResponse.Body.String())
	}
	backupPolicyResponse := perform(server, http.MethodPut, "/v1/projects/advisor-proj/backups/policy", `{"enabled":false,"schedule":"daily","kind":"logical"}`)
	if backupPolicyResponse.Code != http.StatusOK {
		t.Fatalf("expected backup policy update 200, got %d: %s", backupPolicyResponse.Code, backupPolicyResponse.Body.String())
	}
	bucketResponse := perform(server, http.MethodPost, "/v1/projects/advisor-proj/storage/buckets", `{"name":"public-assets","public":true,"file_size_limit":10485760,"cache_control":"3600"}`)
	if bucketResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage bucket create 201, got %d: %s", bucketResponse.Code, bucketResponse.Body.String())
	}

	response := perform(server, http.MethodGet, "/v1/advisor", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected advisor 200, got %d: %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`"project_ref":"advisor-proj"`,
		`"project_ref":"platform"`,
		`"severity":"critical"`,
		`"title":"Recovery-ready target guard is disabled"`,
		`"title":"Durable upgrade backup guard is disabled"`,
		`"title":"No recovery-ready backup target"`,
		`"title":"Project is not healthy"`,
		`"title":"Backups are disabled"`,
		`"title":"PITR is disabled"`,
		`"title":"Database ports are open to all IPs"`,
		`"title":"Database SSL is not enforced"`,
		`"title":"Public storage bucket"`,
		`"recommendation":"Inspect project logs and reconcile the project until it returns to healthy."`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("expected advisor value %s: %s", expected, response.Body.String())
		}
	}
}

func TestComplianceReportEndpoint(t *testing.T) {
	t.Setenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS", "false")
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "false")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "pitr", "log_drains")
	projectBody := `{"ref":"compliance-proj","name":"Compliance","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	networkResponse := perform(server, http.MethodPut, "/v1/projects/compliance-proj/config/network", `{"config":{"db_allowlist":"10.0.0.0/8","ssl_enforced":"true"}}`)
	if networkResponse.Code != http.StatusOK {
		t.Fatalf("expected network config 200, got %d: %s", networkResponse.Code, networkResponse.Body.String())
	}
	pitrResponse := perform(server, http.MethodPut, "/v1/projects/compliance-proj/pitr/policy", `{"enabled":true,"archive_bucket":"s3://archive/compliance-proj","retention_days":14}`)
	if pitrResponse.Code != http.StatusOK {
		t.Fatalf("expected pitr policy 200, got %d: %s", pitrResponse.Code, pitrResponse.Body.String())
	}
	drainResponse := perform(server, http.MethodPost, "/v1/projects/compliance-proj/log-drains", `{"target":"https","config":{"url":"https://logs.example.com/ingest"}}`)
	if drainResponse.Code != http.StatusCreated {
		t.Fatalf("expected log drain create 201, got %d: %s", drainResponse.Code, drainResponse.Body.String())
	}
	rotateResponse := perform(server, http.MethodPost, "/v1/projects/compliance-proj/keys/rotate", `{"kind":"service_role"}`)
	if rotateResponse.Code != http.StatusOK {
		t.Fatalf("expected secret rotation 200, got %d: %s", rotateResponse.Code, rotateResponse.Body.String())
	}

	response := perform(server, http.MethodGet, "/v1/compliance/report", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected compliance report 200, got %d: %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`"frameworks":["SOC 2","HIPAA"]`,
		`"id":"COM-001"`,
		`"title":"Immutable audit chain"`,
		`"status":"pass"`,
		`"id":"COM-009"`,
		`"title":"Hosted-grade recovery guards"`,
		`"recovery-ready target guard enabled: false"`,
		`"durable upgrade backup guard enabled: false"`,
		`"id":"COM-010"`,
		`"title":"Off-host recovery target readiness"`,
		`"0 recovery-ready backup targets"`,
		`"id":"COM-011"`,
		`"status":"manual_review"`,
		`"dpa_posture":"operator-owned: use these controls as evidence for the deploying organization's DPA and BAA posture"`,
		`"certification":"not certified by supadupa; certification remains the operator's responsibility"`,
		`"1/1 projects have backups enabled"`,
		`"1/1 projects have PITR enabled"`,
		`"1/1 projects export logs to a drain"`,
		`"1/1 projects have rotated at least one secret"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("expected compliance report value %s: %s", expected, response.Body.String())
		}
	}
}

func TestAuditEventsRecorded(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	_ = perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)

	response := perform(server, http.MethodGet, "/v1/audit-events", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"action":"org.create"`) {
		t.Fatalf("expected org.create audit event: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"chain_index":1`) || !strings.Contains(response.Body.String(), `"hash":"`) {
		t.Fatalf("expected chained audit fields: %s", response.Body.String())
	}

	integrityResponse := perform(server, http.MethodGet, "/v1/audit-events/integrity", "")
	if integrityResponse.Code != http.StatusOK {
		t.Fatalf("expected audit integrity status 200, got %d: %s", integrityResponse.Code, integrityResponse.Body.String())
	}
	if !strings.Contains(integrityResponse.Body.String(), `"verified":true`) || !strings.Contains(integrityResponse.Body.String(), `"events":1`) {
		t.Fatalf("expected verified audit chain: %s", integrityResponse.Body.String())
	}
}

func TestProjectActivityFiltersAuditEvents(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	alphaResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"activity-alpha","name":"Alpha","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if alphaResponse.Code != http.StatusAccepted {
		t.Fatalf("expected alpha create 202, got %d: %s", alphaResponse.Code, alphaResponse.Body.String())
	}
	betaResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"activity-beta","name":"Beta","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if betaResponse.Code != http.StatusAccepted {
		t.Fatalf("expected beta create 202, got %d: %s", betaResponse.Code, betaResponse.Body.String())
	}
	pauseResponse := perform(server, http.MethodPost, "/v1/projects/activity-alpha/pause", "")
	if pauseResponse.Code != http.StatusOK {
		t.Fatalf("expected alpha pause 200, got %d: %s", pauseResponse.Code, pauseResponse.Body.String())
	}

	activityResponse := perform(server, http.MethodGet, "/v1/projects/activity-alpha/activity", "")
	if activityResponse.Code != http.StatusOK {
		t.Fatalf("expected activity status 200, got %d: %s", activityResponse.Code, activityResponse.Body.String())
	}
	body := activityResponse.Body.String()
	if !strings.Contains(body, `"target":"project:activity-alpha"`) || !strings.Contains(body, `"action":"project.paused"`) {
		t.Fatalf("expected alpha project activity: %s", body)
	}
	if strings.Contains(body, "activity-beta") || strings.Contains(body, `"target":"org:`) {
		t.Fatalf("expected only alpha project activity: %s", body)
	}
}

func TestProvisionerEndpoint(t *testing.T) {
	server := NewServer(Config{Provisioner: composeprovisioner.New()})
	request := httptest.NewRequest(http.MethodGet, "/v1/provisioner", nil)
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if body := response.Body.String(); body != "{\"provisioner\":\"compose\"}\n" {
		t.Fatalf("unexpected body: %s", body)
	}
}
