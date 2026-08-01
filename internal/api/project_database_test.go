package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"supadupa2026/internal/control"
)

func TestProjectDatabaseExtensionsListUpdateMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"ext-proj","name":"Extensions","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/ext-proj/database/extensions", "")
	for _, expected := range []string{`"name":"pg_graphql"`, `"schema":"graphql"`, `"name":"vector"`, `"name":"supabase_vault"`, `"enabled":true`} {
		if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), expected) {
			t.Fatalf("expected default extension %s: %d %s", expected, listResponse.Code, listResponse.Body.String())
		}
	}
	invalidResponse := perform(server, http.MethodPut, "/v1/projects/ext-proj/database/extensions/not_real", `{"enabled":true}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported extension 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	updateResponse := perform(server, http.MethodPut, "/v1/projects/ext-proj/database/extensions/pg_cron", `{"enabled":false,"schema":"extensions","version":"1.6"}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected extension update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	for _, expected := range []string{`"name":"pg_cron"`, `"schema":"extensions"`, `"version":"1.6"`, `"enabled":false`, `"status":"disabled"`} {
		if !strings.Contains(updateResponse.Body.String(), expected) {
			t.Fatalf("expected extension update value %s: %s", expected, updateResponse.Body.String())
		}
	}
	listAfterUpdate := perform(server, http.MethodGet, "/v1/projects/ext-proj/database/extensions", "")
	if !strings.Contains(listAfterUpdate.Body.String(), `"name":"pg_cron"`) || !strings.Contains(listAfterUpdate.Body.String(), `"enabled":false`) {
		t.Fatalf("expected extension override in list: %s", listAfterUpdate.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/ext-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_extensions":7`) {
		t.Fatalf("expected project enabled extension metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_extensions":7`) {
		t.Fatalf("expected org enabled extension usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_extensions":7`) {
		t.Fatalf("expected fleet enabled extension metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_extensions_enabled_total 7") {
		t.Fatalf("expected prometheus enabled extension metric: %s", prometheusResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/ext-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Database extension updated") {
		t.Fatalf("expected extension project log: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.database_extension_update"`) {
		t.Fatalf("expected extension audit action: %s", auditResponse.Body.String())
	}
}

func TestProjectDatabaseExtensionUpdateAppliesLiveDDL(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "extension-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	argsPath := filepath.Join(root, "compose.args")
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuoteForTest(argsPath) + "\ncat > " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"extension-live","name":"Extension Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	updateResponse := perform(server, http.MethodPut, "/v1/projects/extension-live/database/extensions/uuid-ossp", `{"enabled":true,"schema":"extensions"}`)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected extension update 200, got %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read fake compose args: %v", err)
	}
	for _, expected := range []string{"-p extension-live", "-f " + filepath.Join(projectDir, "compose.yaml"), "exec -T db sh -c", `PGPASSWORD="$POSTGRES_PASSWORD" exec psql`} {
		if !strings.Contains(string(args), expected) {
			t.Fatalf("expected fake compose args to contain %q, got %s", expected, string(args))
		}
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	for _, expected := range []string{`CREATE SCHEMA IF NOT EXISTS "extensions";`, `CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA "extensions";`} {
		if !strings.Contains(string(stdin), expected) {
			t.Fatalf("expected extension DDL %q, got:\n%s", expected, stdin)
		}
	}
}

func TestProjectDatabaseExtensionUpdateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "extension-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"extension-fail","name":"Extension Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	disableResponse := perform(server, http.MethodPut, "/v1/projects/extension-fail/database/extensions/pg_cron", `{"enabled":false,"schema":"extensions"}`)
	if disableResponse.Code != http.StatusOK {
		t.Fatalf("expected initial extension update 200, got %d: %s", disableResponse.Code, disableResponse.Body.String())
	}

	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\necho 'extension install failed' >&2\nexit 1\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	enableResponse := perform(server, http.MethodPut, "/v1/projects/extension-fail/database/extensions/pg_cron", `{"enabled":true,"schema":"extensions"}`)
	if enableResponse.Code != http.StatusConflict || !strings.Contains(enableResponse.Body.String(), "extension install failed") {
		t.Fatalf("expected extension apply conflict, got %d: %s", enableResponse.Code, enableResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/extension-fail/database/extensions", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"pg_cron"`) || !strings.Contains(listResponse.Body.String(), `"enabled":false`) {
		t.Fatalf("expected failed extension update to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectDatabaseCronJobsCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"cron-proj","name":"Cron","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidSchedule := perform(server, http.MethodPost, "/v1/projects/cron-proj/database/cron-jobs", `{"name":"bad-job","schedule":"* * *","command":"select 1","active":true}`)
	if invalidSchedule.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid schedule 400, got %d: %s", invalidSchedule.Code, invalidSchedule.Body.String())
	}
	invalidSecret := perform(server, http.MethodPost, "/v1/projects/cron-proj/database/cron-jobs", `{"name":"bad-secret","schedule":"*/5 * * * *","command":"select 1","active":true,"metadata":{"password":"raw"}}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret metadata 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}

	createBody := `{"name":"refresh-rollups","schedule":"*/15 * * * *","command":"select analytics.refresh_rollups();","database":"postgres","username":"postgres","active":true,"timeout_seconds":90,"max_runtime_seconds":120,"metadata":{"owner":"analytics","password":"secret://projects/cron-proj/db/cron-password"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/cron-proj/database/cron-jobs", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected cron job create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"refresh-rollups"`, `"schedule":"*/15 * * * *"`, `"command":"select analytics.refresh_rollups();"`, `"database":"postgres"`, `"username":"postgres"`, `"active":true`, `"timeout_seconds":90`, `"max_runtime_seconds":120`, `"owner":"analytics"`, `"password":"********"`, `"status":"scheduled"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected cron create value %s: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "secret://projects") {
		t.Fatalf("expected cron metadata secret to be masked: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/cron-proj/database/cron-jobs", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"refresh-rollups"`) || !strings.Contains(listResponse.Body.String(), `"password":"********"`) {
		t.Fatalf("expected masked cron job list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/cron-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_cron_jobs":1`) {
		t.Fatalf("expected project cron metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_cron_jobs":1`) {
		t.Fatalf("expected org cron usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_cron_jobs":1`) {
		t.Fatalf("expected fleet cron metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_cron_jobs_total 1") {
		t.Fatalf("expected prometheus cron metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/cron-proj/database/cron-jobs/refresh-rollups", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected cron job delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/cron-proj/logs", "")
	for _, expected := range []string{"Database cron job configured", "Database cron job deleted"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected cron project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.database_cron_create", "project.database_cron_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDatabaseCronJobCreateDeleteAppliesLiveDDL(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "cron-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\ncat >> " + shellQuoteForTest(stdinPath) + "\nprintf '\\n-- call --\\n' >> " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"cron-live","name":"Cron Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/cron-live/database/cron-jobs", `{"name":"refresh-rollups","schedule":"*/15 * * * *","command":"select public.refresh_rollups();","database":"postgres","username":"postgres","active":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected cron create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/cron-live/database/cron-jobs/refresh-rollups", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected cron delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	for _, expected := range []string{
		`CREATE EXTENSION IF NOT EXISTS pg_cron WITH SCHEMA extensions;`,
		`SELECT cron.schedule_in_database('refresh-rollups', '*/15 * * * *', 'select public.refresh_rollups();', 'postgres', 'postgres', true);`,
		`SELECT cron.unschedule(jobid) FROM cron.job WHERE jobname = 'refresh-rollups';`,
	} {
		if !strings.Contains(string(stdin), expected) {
			t.Fatalf("expected cron DDL %q, got:\n%s", expected, stdin)
		}
	}
}

func TestProjectDatabaseCronJobCreateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "cron-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\necho 'cron schedule failed' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"cron-fail","name":"Cron Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/cron-fail/database/cron-jobs", `{"name":"refresh-rollups","schedule":"*/15 * * * *","command":"select 1;","active":true}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "cron schedule failed") {
		t.Fatalf("expected cron apply conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/cron-fail/database/cron-jobs", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"refresh-rollups"`) {
		t.Fatalf("expected failed cron create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectDatabaseQueuesCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"queue-proj","name":"Queues","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidRetention := perform(server, http.MethodPost, "/v1/projects/queue-proj/database/queues", `{"name":"events","retention_minutes":0,"visibility_timeout_seconds":90000,"active":true}`)
	if invalidRetention.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid visibility timeout 400, got %d: %s", invalidRetention.Code, invalidRetention.Body.String())
	}
	invalidSecret := perform(server, http.MethodPost, "/v1/projects/queue-proj/database/queues", `{"name":"events","active":true,"metadata":{"token":"raw"}}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret metadata 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}

	createBody := `{"name":"events","schema":"pgmq","retention_minutes":10080,"visibility_timeout_seconds":45,"max_retries":7,"dead_letter_queue":"events-dlq","active":true,"metadata":{"owner":"backend","token":"secret://projects/queue-proj/db/pgmq-token"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/queue-proj/database/queues", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected database queue create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"events"`, `"schema":"pgmq"`, `"retention_minutes":10080`, `"visibility_timeout_seconds":45`, `"max_retries":7`, `"dead_letter_queue":"events-dlq"`, `"active":true`, `"owner":"backend"`, `"token":"********"`, `"status":"ready"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected queue create value %s: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "secret://projects") {
		t.Fatalf("expected queue metadata secret to be masked: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/queue-proj/database/queues", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"events"`) || !strings.Contains(listResponse.Body.String(), `"token":"********"`) {
		t.Fatalf("expected masked queue list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/queue-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_queues":1`) {
		t.Fatalf("expected project queue metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_queues":1`) {
		t.Fatalf("expected org queue usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_queues":1`) {
		t.Fatalf("expected fleet queue metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_queues_total 1") {
		t.Fatalf("expected prometheus queue metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/queue-proj/database/queues/events", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected database queue delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/queue-proj/logs", "")
	for _, expected := range []string{"Database queue configured", "Database queue deleted"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected queue project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.database_queue_create", "project.database_queue_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDatabaseQueueCreateDeleteAppliesLiveDDL(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "queue-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\ncat >> " + shellQuoteForTest(stdinPath) + "\nprintf '\\n-- call --\\n' >> " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"queue-live","name":"Queue Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/queue-live/database/queues", `{"name":"events","schema":"pgmq","dead_letter_queue":"events-dlq","active":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected queue create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/queue-live/database/queues/events", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected queue delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	for _, expected := range []string{
		`CREATE SCHEMA IF NOT EXISTS "pgmq";`,
		`CREATE EXTENSION IF NOT EXISTS pgmq WITH SCHEMA "pgmq";`,
		`SELECT pgmq.create('events-dlq');`,
		`SELECT pgmq.create('events');`,
		`SELECT pgmq.drop_queue('events');`,
		`SELECT pgmq.drop_queue('events-dlq');`,
	} {
		if !strings.Contains(string(stdin), expected) {
			t.Fatalf("expected queue DDL %q, got:\n%s", expected, stdin)
		}
	}
}

func TestProjectDatabaseQueueCreateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "queue-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\necho 'queue create failed' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"queue-fail","name":"Queue Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/queue-fail/database/queues", `{"name":"events","schema":"pgmq","active":true}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "queue create failed") {
		t.Fatalf("expected queue apply conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/queue-fail/database/queues", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"events"`) {
		t.Fatalf("expected failed queue create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectDatabaseWebhooksCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"webhook-proj","name":"Webhooks","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidEndpoint := perform(server, http.MethodPost, "/v1/projects/webhook-proj/database/webhooks", `{"name":"orders-events","schema":"public","table":"orders","endpoint":"http://hooks.example.com/orders","active":true}`)
	if invalidEndpoint.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid endpoint 400, got %d: %s", invalidEndpoint.Code, invalidEndpoint.Body.String())
	}
	invalidSecret := perform(server, http.MethodPost, "/v1/projects/webhook-proj/database/webhooks", `{"name":"orders-events","schema":"public","table":"orders","endpoint":"https://hooks.example.com/orders","active":true,"headers":{"Authorization":"Bearer raw"}}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret header 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}

	createBody := `{"name":"orders-events","schema":"public","table":"orders","events":["insert","update"],"endpoint":"https://hooks.example.com/orders","http_method":"POST","headers":{"Authorization":"secret://projects/webhook-proj/webhooks/orders-token","X-Source":"supadupa"},"timeout_seconds":15,"retry_count":5,"active":true,"metadata":{"owner":"backend","token":"secret://projects/webhook-proj/webhooks/meta-token"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/webhook-proj/database/webhooks", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected database webhook create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"orders-events"`, `"schema":"public"`, `"table":"orders"`, `"events":["insert","update"]`, `"endpoint":"https://hooks.example.com/orders"`, `"http_method":"POST"`, `"authorization":"********"`, `"x-source":"supadupa"`, `"timeout_seconds":15`, `"retry_count":5`, `"active":true`, `"owner":"backend"`, `"token":"********"`, `"status":"ready"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected webhook create value %s: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "secret://projects") {
		t.Fatalf("expected webhook secrets to be masked: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/webhook-proj/database/webhooks", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"orders-events"`) || !strings.Contains(listResponse.Body.String(), `"authorization":"********"`) {
		t.Fatalf("expected masked webhook list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/webhook-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_webhooks":1`) {
		t.Fatalf("expected project webhook metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_webhooks":1`) {
		t.Fatalf("expected org webhook usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_webhooks":1`) {
		t.Fatalf("expected fleet webhook metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_webhooks_total 1") {
		t.Fatalf("expected prometheus webhook metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/webhook-proj/database/webhooks/orders-events", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected database webhook delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/webhook-proj/logs", "")
	for _, expected := range []string{"Database webhook configured", "Database webhook deleted"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected webhook project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.database_webhook_create", "project.database_webhook_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDatabaseWebhookCreateDeleteAppliesLiveDDL(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "webhook-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\ncat >> " + shellQuoteForTest(stdinPath) + "\nprintf '\\n-- call --\\n' >> " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"webhook-live","name":"Webhook Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	seedProjectSecrets(t, store, "webhook-live", "orders-token")

	createResponse := perform(server, http.MethodPost, "/v1/projects/webhook-live/database/webhooks", `{"name":"orders-events","schema":"public","table":"orders","events":["insert","update"],"endpoint":"https://hooks.example.com/orders","http_method":"POST","headers":{"Authorization":"secret://projects/webhook-live/orders-token","X-Source":"supadupa"},"timeout_seconds":15,"active":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected webhook create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/webhook-live/database/webhooks/orders-events", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected webhook delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	sql := string(stdin)
	for _, expected := range []string{
		`CREATE SCHEMA IF NOT EXISTS supadupa;`,
		`CREATE EXTENSION IF NOT EXISTS pg_net;`,
		`CREATE OR REPLACE FUNCTION supadupa."webhook_orders_events"()`,
		`"authorization":"orders-token-value"`,
		`"x-source":"supadupa"`,
		`PERFORM net.http_post(`,
		`url := 'https://hooks.example.com/orders'`,
		`timeout_milliseconds := 15000`,
		`CREATE TRIGGER "supadupa_webhook_orders_events_insert"`,
		`AFTER INSERT ON "public"."orders"`,
		`CREATE TRIGGER "supadupa_webhook_orders_events_update"`,
		`AFTER UPDATE ON "public"."orders"`,
		`DROP TRIGGER IF EXISTS "supadupa_webhook_orders_events_insert" ON "public"."orders"`,
		`DROP FUNCTION IF EXISTS supadupa."webhook_orders_events"();`,
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("expected webhook DDL %q, got:\n%s", expected, sql)
		}
	}
	if strings.Contains(sql, "secret://projects") {
		t.Fatalf("expected live webhook DDL to contain resolved secrets, got:\n%s", sql)
	}
}

func TestProjectDatabaseWebhookCreateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "webhook-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\necho 'webhook create failed' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"webhook-fail","name":"Webhook Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/webhook-fail/database/webhooks", `{"name":"orders-events","schema":"public","table":"orders","endpoint":"https://hooks.example.com/orders","active":true}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "webhook create failed") {
		t.Fatalf("expected webhook apply conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/webhook-fail/database/webhooks", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"orders-events"`) {
		t.Fatalf("expected failed webhook create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectDatabaseWebhookCreateRequiresRevealableSecretHeadersWhenApplyEnabled(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "webhook-secret")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"webhook-secret","name":"Webhook Secret","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/webhook-secret/database/webhooks", `{"name":"orders-events","schema":"public","table":"orders","endpoint":"https://hooks.example.com/orders","headers":{"Authorization":"secret://projects/webhook-secret/webhooks/orders-token"},"active":true}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "not revealable") {
		t.Fatalf("expected unrevealable header conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/webhook-secret/database/webhooks", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"orders-events"`) {
		t.Fatalf("expected failed webhook create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectDatabaseSchemasCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"schema-proj","name":"Schemas","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidSQL := perform(server, http.MethodPost, "/v1/projects/schema-proj/database/schemas", `{"name":"app-schema","version":"20260605_001","schema":"public","sql":"","active":true}`)
	if invalidSQL.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid SQL 400, got %d: %s", invalidSQL.Code, invalidSQL.Body.String())
	}
	invalidSecret := perform(server, http.MethodPost, "/v1/projects/schema-proj/database/schemas", `{"name":"app-schema","version":"20260605_001","schema":"public","sql":"create table public.accounts(id uuid primary key);","active":true,"metadata":{"token":"raw"}}`)
	if invalidSecret.Code != http.StatusBadRequest {
		t.Fatalf("expected raw secret metadata 400, got %d: %s", invalidSecret.Code, invalidSecret.Body.String())
	}

	createBody := `{"name":"app-schema","version":"20260605_001","schema":"public","sql":"create table public.accounts(id uuid primary key);","apply_order":10,"active":true,"metadata":{"owner":"backend","token":"secret://projects/schema-proj/db/schema-token"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/schema-proj/database/schemas", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected database schema create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"app-schema"`, `"version":"20260605_001"`, `"schema":"public"`, `"sql":"create table public.accounts(id uuid primary key);"`, `"apply_order":10`, `"active":true`, `"owner":"backend"`, `"token":"********"`, `"status":"pending"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected schema create value %s: %s", expected, createResponse.Body.String())
		}
	}
	checksum := extractString(t, createResponse.Body.String(), "checksum")
	if len(checksum) != 64 {
		t.Fatalf("expected sha256 checksum, got %q", checksum)
	}
	if strings.Contains(createResponse.Body.String(), "secret://projects") {
		t.Fatalf("expected schema metadata secret to be masked: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/schema-proj/database/schemas", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"app-schema"`) || !strings.Contains(listResponse.Body.String(), `"token":"********"`) {
		t.Fatalf("expected masked schema list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/schema-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_schemas":1`) {
		t.Fatalf("expected project schema metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_schemas":1`) {
		t.Fatalf("expected org schema usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_schemas":1`) {
		t.Fatalf("expected fleet schema metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_schemas_total 1") {
		t.Fatalf("expected prometheus schema metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/schema-proj/database/schemas/app-schema/20260605_001", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected database schema delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/schema-proj/logs", "")
	for _, expected := range []string{"Declarative schema recorded", "Declarative schema deleted"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected schema project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.database_schema_create", "project.database_schema_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDatabaseSchemaCreateAppliesActiveSQLToProjectDatabase(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "schema-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	argsPath := filepath.Join(root, "compose.args")
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + shellQuoteForTest(argsPath) + "\ncat > " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"schema-live","name":"Schema Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	sql := "create table public.live_schema_probe(id uuid primary key);"
	createResponse := perform(server, http.MethodPost, "/v1/projects/schema-live/database/schemas", `{"name":"live-schema","version":"20260606_001","schema":"public","sql":"`+sql+`","active":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected schema create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read fake compose args: %v", err)
	}
	for _, expected := range []string{"-p schema-live", "-f " + filepath.Join(projectDir, "compose.yaml"), "exec -T db sh -c", `PGPASSWORD="$POSTGRES_PASSWORD" exec psql`, "-v ON_ERROR_STOP=1", "-U supabase_admin", "-d postgres"} {
		if !strings.Contains(string(args), expected) {
			t.Fatalf("expected fake compose args to contain %q, got %s", expected, string(args))
		}
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	if strings.TrimSpace(string(stdin)) != sql {
		t.Fatalf("expected SQL stdin %q, got %q", sql, strings.TrimSpace(string(stdin)))
	}
}

func TestProjectDatabaseSchemaCreateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "schema-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\necho 'syntax error near bad_sql' >&2\nexit 1\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"schema-fail","name":"Schema Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/schema-fail/database/schemas", `{"name":"bad-schema","version":"20260606_001","schema":"public","sql":"select bad_sql();","active":true}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "syntax error near bad_sql") {
		t.Fatalf("expected schema apply conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/schema-fail/database/schemas", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"bad-schema"`) {
		t.Fatalf("expected failed schema create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectDatabaseRolesCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"dbrole-proj","name":"Database Roles","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	reservedResponse := perform(server, http.MethodPost, "/v1/projects/dbrole-proj/database/roles", `{"name":"service_role","login":true,"password_secret_handle":"secret://projects/dbrole-proj/db/app-role"}`)
	if reservedResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected reserved role 400, got %d: %s", reservedResponse.Code, reservedResponse.Body.String())
	}
	invalidSecretResponse := perform(server, http.MethodPost, "/v1/projects/dbrole-proj/database/roles", `{"name":"app_writer","login":true,"password_secret_handle":"raw-password"}`)
	if invalidSecretResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected raw password handle 400, got %d: %s", invalidSecretResponse.Code, invalidSecretResponse.Body.String())
	}
	createBody := `{"name":"app_writer","login":true,"bypass_rls":false,"connection_limit":25,"password_secret_handle":"secret://projects/dbrole-proj/db/app-writer","member_of":["authenticated"],"schema_grants":{"public":"usage,select,insert,update"},"metadata":{"purpose":"application-writes","api_key":"secret://projects/dbrole-proj/db-role-api"}}`
	createResponse := perform(server, http.MethodPost, "/v1/projects/dbrole-proj/database/roles", createBody)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected database role create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"app_writer"`, `"login":true`, `"connection_limit":25`, `"password_secret_handle":"********"`, `"api_key":"********"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected database role response value %s: %s", expected, createResponse.Body.String())
		}
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/dbrole-proj/database/roles", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"app_writer"`) || !strings.Contains(listResponse.Body.String(), `"password_secret_handle":"********"`) {
		t.Fatalf("expected database role list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/dbrole-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"database_roles":1`) {
		t.Fatalf("expected project database role metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"database_roles":1`) {
		t.Fatalf("expected org database role usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"database_roles":1`) {
		t.Fatalf("expected fleet database role metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_database_roles_total 1") {
		t.Fatalf("expected prometheus database role metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/dbrole-proj/database/roles/app_writer", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected database role delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/dbrole-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Database role configured") || !strings.Contains(logsResponse.Body.String(), "Database role deleted") {
		t.Fatalf("expected database role project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.database_role_create", "project.database_role_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectDatabaseRoleCreateDeleteAppliesLiveDDL(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "role-live")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	stdinPath := filepath.Join(root, "compose.stdin")
	composePath := filepath.Join(root, "fake-compose")
	script := "#!/bin/sh\ncat >> " + shellQuoteForTest(stdinPath) + "\nprintf '\\n-- call --\\n' >> " + shellQuoteForTest(stdinPath) + "\n"
	if err := os.WriteFile(composePath, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"role-live","name":"Role Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/role-live/database/roles", `{"name":"app_reader","login":false,"bypass_rls":false,"connection_limit":25,"member_of":["authenticated"],"schema_grants":{"public":"usage,select"}}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected role create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/role-live/database/roles/app_reader", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected role delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read fake compose stdin: %v", err)
	}
	for _, expected := range []string{
		`CREATE ROLE "app_reader";`,
		`ALTER ROLE "app_reader" NOLOGIN INHERIT NOBYPASSRLS CONNECTION LIMIT 25;`,
		`GRANT "authenticated" TO "app_reader";`,
		`GRANT USAGE ON SCHEMA "public" TO "app_reader";`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA "public" TO "app_reader";`,
		`REVOKE SELECT ON ALL TABLES IN SCHEMA "public" FROM "app_reader";`,
		`REVOKE USAGE ON SCHEMA "public" FROM "app_reader";`,
		`REVOKE "authenticated" FROM "app_reader";`,
		`DROP ROLE IF EXISTS "app_reader";`,
	} {
		if !strings.Contains(string(stdin), expected) {
			t.Fatalf("expected role DDL %q, got:\n%s", expected, stdin)
		}
	}
}

func TestProjectDatabaseRoleCreateRollsBackMetadataWhenApplyFails(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "role-fail")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\necho 'role create failed' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"role-fail","name":"Role Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/role-fail/database/roles", `{"name":"app_reader","login":false,"schema_grants":{"public":"usage"}}`)
	if createResponse.Code != http.StatusConflict || !strings.Contains(createResponse.Body.String(), "role create failed") {
		t.Fatalf("expected role apply conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/role-fail/database/roles", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"app_reader"`) {
		t.Fatalf("expected failed role create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectDatabaseRoleLoginCreateRequiresRevealableProjectSecret(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "role-secret")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	composePath := filepath.Join(root, "fake-compose")
	if err := os.WriteFile(composePath, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o700); err != nil {
		t.Fatalf("write fake compose: %v", err)
	}
	t.Setenv("SUPADUPA_DATABASE_APPLY", "true")
	t.Setenv("SUPADUPA_PROJECT_ROOT", root)
	t.Setenv("SUPADUPA_COMPOSE_COMMAND", composePath)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"role-secret","name":"Role Secret","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	unrevealableResponse := perform(server, http.MethodPost, "/v1/projects/role-secret/database/roles", `{"name":"app_login","login":true,"password_secret_handle":"secret://projects/role-secret/db/app-login"}`)
	if unrevealableResponse.Code != http.StatusConflict || !strings.Contains(unrevealableResponse.Body.String(), "not revealable") {
		t.Fatalf("expected unrevealable secret conflict, got %d: %s", unrevealableResponse.Code, unrevealableResponse.Body.String())
	}
	crossProjectResponse := perform(server, http.MethodPost, "/v1/projects/role-secret/database/roles", `{"name":"other_login","login":true,"password_secret_handle":"secret://projects/other/db_password"}`)
	if crossProjectResponse.Code != http.StatusConflict || !strings.Contains(crossProjectResponse.Body.String(), "must reference project role-secret") {
		t.Fatalf("expected cross-project secret conflict, got %d: %s", crossProjectResponse.Code, crossProjectResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/role-secret/database/roles", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"login":true`) {
		t.Fatalf("expected failed login role creates to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}
