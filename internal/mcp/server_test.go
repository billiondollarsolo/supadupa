package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRunnerInitializeListToolsAndCallManagementAPI(t *testing.T) {
	var requests []string
	var authHeader string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		authHeader = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/v1/metrics":
			_, _ = w.Write([]byte(`{"projects":1,"analytics_buckets":1}`))
		case "/v1/orgs/org_1/usage":
			_, _ = w.Write([]byte(`{"org_id":"org_1","resources":{"projects":1},"analytics_buckets":1}`))
		case "/v1/orgs/org_1/usage/snapshots":
			if r.URL.Query().Get("limit") != "2" {
				t.Fatalf("expected snapshot limit 2, got %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"id":"snap_1","org_id":"org_1"}]`))
		case "/v1/orgs/org_1/billing/invoices":
			if r.URL.Query().Get("limit") != "2" {
				t.Fatalf("expected invoice limit 2, got %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"id":"inv_1","number":"SDP-202606-0001","total_cents":2600}]`))
		case "/v1/projects":
			_, _ = w.Write([]byte(`[{"ref":"alpha","name":"Alpha","status":"healthy"}]`))
		case "/v1/projects/alpha/connect":
			_, _ = w.Write([]byte(`{"api_url":"https://alpha.example.test","custom_api_urls":["https://api.example.com"],"custom_domains":[{"project_ref":"alpha","fqdn":"api.example.com","cert_status":"issued","cert_mode":"acme"}],"studio_url":"https://studio.alpha.example.test"}`))
		case "/v1/projects/alpha/metrics":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","status":"healthy","analytics_buckets":1}`))
		case "/v1/projects/alpha/telemetry/history":
			if r.URL.Query().Get("range") != "24h" || r.URL.Query().Get("step") != "5m" {
				t.Fatalf("unexpected telemetry history query %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","step_seconds":300,"points":[]}`))
		case "/v1/projects/alpha/config/ai":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","area":"ai","config":{"studio_assistant_enabled":"true"}}`))
		case "/v1/projects/alpha/logs":
			_, _ = w.Write([]byte(`[{"level":"info","message":"Project healthy"}]`))
		case "/v1/projects/alpha/analytics-buckets":
			_, _ = w.Write([]byte(`[{"name":"events","storage_uri":"s3://lake/events"}]`))
		case "/v1/projects/alpha/auth/clients":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"id":"auth_client_1","name":"Dashboard App","client_id":"dashboard_app"}]`))
			case http.MethodPost:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				redirectURIs, _ := got["redirect_uris"].([]any)
				grantTypes, _ := got["grant_types"].([]any)
				scopes, _ := got["scopes"].([]any)
				if got["name"] != "Dashboard App" || got["client_id"] != "dashboard_app" || got["client_secret_handle"] != "secret://projects/alpha/auth/dashboard" || got["confidential"] != true || len(redirectURIs) != 1 || redirectURIs[0] != "https://app.example.com/auth/callback" || len(grantTypes) != 2 || grantTypes[0] != "authorization_code" || grantTypes[1] != "refresh_token" || len(scopes) != 2 || scopes[0] != "openid" || scopes[1] != "email" {
					t.Fatalf("unexpected auth client payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"auth_client_1","name":"Dashboard App","client_id":"dashboard_app","client_secret_handle":"********","confidential":true}`))
			default:
				t.Fatalf("unexpected auth-clients method %s", r.Method)
			}
		case "/v1/projects/alpha/auth/clients/dashboard_app":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected auth client delete method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/projects/alpha/auth/hooks":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"id":"hook_1","hook_type":"custom_access_token","enabled":true}]`))
			case http.MethodPost:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				headers, ok := got["headers"].(map[string]any)
				if !ok || got["hook_type"] != "custom_access_token" || got["enabled"] != true || got["target_uri"] != "https://hooks.example.com/token" || got["edge_function"] != "token-hook" || got["secret_handle"] != "secret://projects/alpha/auth/hook" || headers["authorization"] != "secret://projects/alpha/auth/header" || got["timeout_ms"] != float64(7000) || got["retry_attempts"] != float64(2) {
					t.Fatalf("unexpected auth hook payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"hook_1","hook_type":"custom_access_token","enabled":true,"secret_handle":"********","headers":{"authorization":"********"}}`))
			default:
				t.Fatalf("unexpected auth-hooks method %s", r.Method)
			}
		case "/v1/projects/alpha/auth/hooks/custom_access_token":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected auth hook delete method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/projects/alpha/domains":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"project_ref":"alpha","fqdn":"api.example.com","cert_status":"issued"}]`))
			case http.MethodPost:
				var got map[string]string
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				if got["fqdn"] != "api.example.com" {
					t.Fatalf("unexpected domain payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"project_ref":"alpha","fqdn":"api.example.com","cert_status":"pending"}`))
			default:
				t.Fatalf("unexpected domains method %s", r.Method)
			}
		case "/v1/projects/alpha/domains/api.example.com":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected domain delete method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/projects/alpha/log-drains":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"id":"drain_1","target":"https","config":{"url":"https://logs.example.com"}}]`))
			case http.MethodPost:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				config, ok := got["config"].(map[string]any)
				if !ok || got["target"] != "https" || config["url"] != "https://logs.example.com/ingest" || config["token"] != "secret://projects/alpha/logs" {
					t.Fatalf("unexpected log drain payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"drain_1","target":"https","config":{"url":"https://logs.example.com/ingest","token":"********"}}`))
			default:
				t.Fatalf("unexpected log-drains method %s", r.Method)
			}
		case "/v1/projects/alpha/log-drains/drain_1":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected log drain delete method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/projects/alpha/network-connections":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"id":"net_1","name":"aws-prod","type":"privatelink","provider":"aws"}]`))
			case http.MethodPost:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				cidrs, _ := got["cidrs"].([]any)
				config, ok := got["config"].(map[string]any)
				if !ok || got["name"] != "aws-prod" || got["type"] != "privatelink" || got["provider"] != "aws" || got["region"] != "us-east-1" || got["endpoint_id"] != "vpce-123" || len(cidrs) != 1 || cidrs[0] != "10.0.0.0/16" || config["account_id"] != "123456789012" {
					t.Fatalf("unexpected network connection payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"net_1","name":"aws-prod","type":"privatelink","provider":"aws","status":"requested"}`))
			default:
				t.Fatalf("unexpected network-connections method %s", r.Method)
			}
		case "/v1/projects/alpha/network-connections/net_1":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected network connection delete method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/projects/alpha/replication":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"id":"pipe_1","name":"orders-etl","destination":"s3"}]`))
			case http.MethodPost:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				config, ok := got["config"].(map[string]any)
				if !ok || got["name"] != "orders-etl" || got["type"] != "etl" || got["source_schema"] != "public" || got["source_table"] != "orders" || got["destination"] != "s3" || got["destination_uri"] != "s3://lake/orders" || got["credential_handle"] != "secret://projects/alpha/etl" || config["bucket"] != "lake" {
					t.Fatalf("unexpected replication payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"pipe_1","name":"orders-etl","destination":"s3"}`))
			default:
				t.Fatalf("unexpected replication method %s", r.Method)
			}
		case "/v1/projects/alpha/replication/pipe_1":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected replication delete method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/projects/alpha/embeddings":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"id":"emb_1","name":"docs-embeddings","source_table":"documents"}]`))
			case http.MethodPost:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				if got["source_schema"] != "public" || got["source_table"] != "documents" || got["source_column"] != "body" || got["primary_key_column"] != "id" || got["destination_column"] != "embedding" || got["provider"] != "openai" || got["model"] != "text-embedding-3-small" || got["dimension"] != float64(1536) || got["schedule"] != "manual" || got["batch_size"] != float64(100) {
					t.Fatalf("unexpected embedding payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"emb_1","name":"docs-embeddings","source_table":"documents"}`))
			default:
				t.Fatalf("unexpected embedding method %s", r.Method)
			}
		case "/v1/projects/alpha/embeddings/emb_1":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected embedding delete method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/projects/alpha/functions":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"id":"fn_1","name":"hello-api","version":1,"status":"deployed"}]`))
			case http.MethodPost:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				if got["name"] != "hello-api" || got["entrypoint"] != "index.ts" || got["verify_jwt"] != true || got["source"] != "Deno.serve(() => new Response('ok'))" {
					t.Fatalf("unexpected function deploy payload %#v", got)
				}
				secrets, ok := got["secrets"].(map[string]any)
				if !ok || secrets["API_KEY"] != "secret://projects/alpha/functions/api-key" {
					t.Fatalf("unexpected function secrets payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"fn_1","name":"hello-api","version":2,"status":"deployed","secrets":{"API_KEY":"********"}}`))
			default:
				t.Fatalf("unexpected function method %s", r.Method)
			}
		case "/v1/projects/alpha/functions/hello-api":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected function delete method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/projects/alpha/functions/regions":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"id":"region_1","function_name":"hello-api","region":"us-east-1"}]`))
			case http.MethodPost:
				var got map[string]string
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				if got["function_name"] != "hello-api" || got["host_id"] != "host-1" || got["region"] != "us-east-1" || got["routing_policy"] != "primary" {
					t.Fatalf("unexpected function region payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"region_1","function_name":"hello-api","region":"us-east-1","routing_policy":"primary"}`))
			default:
				t.Fatalf("unexpected function region method %s", r.Method)
			}
		case "/v1/projects/alpha/functions/regions/region_1":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected function region delete method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/projects/alpha/functions/storage-mounts":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"id":"mount_1","function_name":"hello-api","bucket_name":"assets","mount_path":"/mnt/assets"}]`))
			case http.MethodPost:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				if got["function_name"] != "hello-api" || got["bucket_name"] != "assets" || got["mount_path"] != "/mnt/assets" || got["read_only"] != true || got["prefix"] != "public" || got["env_alias"] != "ASSETS_MOUNT" {
					t.Fatalf("unexpected function storage mount payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"mount_1","function_name":"hello-api","bucket_name":"assets","mount_path":"/mnt/assets","read_only":true}`))
			default:
				t.Fatalf("unexpected function storage mount method %s", r.Method)
			}
		case "/v1/projects/alpha/functions/storage-mounts/mount_1":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected function storage mount delete method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_list_projects","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_project_connect","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"supadupa_get_fleet_metrics","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"supadupa_get_project_metrics","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"supadupa_get_project_telemetry_history","arguments":{"ref":"alpha","range":"24h","step":"5m"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"supadupa_get_project_config","arguments":{"ref":"alpha","area":"ai"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"supadupa_list_project_logs","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"supadupa_list_project_analytics_buckets","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"supadupa_get_org_usage","arguments":{"org_id":"org_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"supadupa_list_org_usage_snapshots","arguments":{"org_id":"org_1","limit":2}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"supadupa_list_billing_invoices","arguments":{"org_id":"org_1","limit":2}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"supadupa_list_project_domains","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"supadupa_add_project_domain","arguments":{"ref":"alpha","fqdn":"API.EXAMPLE.COM."}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"supadupa_delete_project_domain","arguments":{"ref":"alpha","fqdn":"api.example.com"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"supadupa_list_project_log_drains","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":18,"method":"tools/call","params":{"name":"supadupa_create_project_log_drain","arguments":{"ref":"alpha","target":"https","config":{"url":"https://logs.example.com/ingest","token":"secret://projects/alpha/logs"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":19,"method":"tools/call","params":{"name":"supadupa_delete_project_log_drain","arguments":{"ref":"alpha","id":"drain_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"supadupa_list_project_network_connections","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"supadupa_create_project_network_connection","arguments":{"ref":"alpha","name":"aws-prod","type":"privatelink","provider":"aws","region":"us-east-1","cidrs":["10.0.0.0/16"],"endpoint_id":"vpce-123","config":{"account_id":"123456789012"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":22,"method":"tools/call","params":{"name":"supadupa_delete_project_network_connection","arguments":{"ref":"alpha","id":"net_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":23,"method":"tools/call","params":{"name":"supadupa_list_project_replication_pipelines","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":24,"method":"tools/call","params":{"name":"supadupa_create_project_replication_pipeline","arguments":{"ref":"alpha","name":"orders-etl","type":"etl","source_table":"orders","destination":"s3","destination_uri":"s3://lake/orders","credential_handle":"secret://projects/alpha/etl","config":{"bucket":"lake"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":25,"method":"tools/call","params":{"name":"supadupa_delete_project_replication_pipeline","arguments":{"ref":"alpha","id":"pipe_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":26,"method":"tools/call","params":{"name":"supadupa_list_project_embedding_jobs","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":27,"method":"tools/call","params":{"name":"supadupa_create_project_embedding_job","arguments":{"ref":"alpha","name":"docs-embeddings","source_table":"documents","source_column":"body"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":28,"method":"tools/call","params":{"name":"supadupa_delete_project_embedding_job","arguments":{"ref":"alpha","id":"emb_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":29,"method":"tools/call","params":{"name":"supadupa_list_project_functions","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":30,"method":"tools/call","params":{"name":"supadupa_deploy_project_function","arguments":{"ref":"alpha","name":"hello-api","source":"Deno.serve(() => new Response('ok'))","secrets":{"API_KEY":"secret://projects/alpha/functions/api-key"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":31,"method":"tools/call","params":{"name":"supadupa_list_project_function_regions","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":32,"method":"tools/call","params":{"name":"supadupa_create_project_function_region","arguments":{"ref":"alpha","function_name":"hello-api","host_id":"host-1","region":"us-east-1","routing_policy":"primary"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":33,"method":"tools/call","params":{"name":"supadupa_delete_project_function_region","arguments":{"ref":"alpha","id":"region_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":34,"method":"tools/call","params":{"name":"supadupa_list_project_function_storage_mounts","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":35,"method":"tools/call","params":{"name":"supadupa_create_project_function_storage_mount","arguments":{"ref":"alpha","function_name":"hello-api","bucket_name":"assets","mount_path":"/mnt/assets","read_only":true,"prefix":"public","env_alias":"ASSETS_MOUNT"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":36,"method":"tools/call","params":{"name":"supadupa_delete_project_function_storage_mount","arguments":{"ref":"alpha","id":"mount_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":37,"method":"tools/call","params":{"name":"supadupa_delete_project_function","arguments":{"ref":"alpha","name":"hello-api"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":38,"method":"tools/call","params":{"name":"supadupa_list_project_auth_clients","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":39,"method":"tools/call","params":{"name":"supadupa_create_project_auth_client","arguments":{"ref":"alpha","name":"Dashboard App","client_id":"dashboard_app","client_secret_handle":"secret://projects/alpha/auth/dashboard","redirect_uris":["https://app.example.com/auth/callback"],"grant_types":["authorization_code","refresh_token"],"scopes":["openid","email"]}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":40,"method":"tools/call","params":{"name":"supadupa_delete_project_auth_client","arguments":{"ref":"alpha","client_id":"dashboard_app"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":41,"method":"tools/call","params":{"name":"supadupa_list_project_auth_hooks","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"supadupa_set_project_auth_hook","arguments":{"ref":"alpha","hook_type":"custom_access_token","target_uri":"https://hooks.example.com/token","edge_function":"token-hook","secret_handle":"secret://projects/alpha/auth/hook","headers":{"authorization":"secret://projects/alpha/auth/header"},"timeout_ms":7000,"retry_attempts":2}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":43,"method":"tools/call","params":{"name":"supadupa_delete_project_auth_hook","arguments":{"ref":"alpha","hook_type":"custom_access_token"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env: map[string]string{
			"SUPADUPA_API_URL": api.URL,
			"SUPADUPA_TOKEN":   "test-token",
		},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	responses := readTestFrames(t, output)
	if len(responses) != 43 {
		t.Fatalf("expected forty-three responses, got %d: %s", len(responses), output.String())
	}
	if !strings.Contains(responses[0], `"serverInfo":{"name":"supadupa-mcp"`) {
		t.Fatalf("expected initialize server info: %s", responses[0])
	}
	if !strings.Contains(responses[1], `"name":"supadupa_project_connect"`) || !strings.Contains(responses[1], `"name":"supadupa_get_project_telemetry_history"`) || !strings.Contains(responses[1], `"name":"supadupa_trigger_backup"`) || !strings.Contains(responses[1], `"name":"supadupa_list_project_analytics_buckets"`) || !strings.Contains(responses[1], `"name":"supadupa_list_project_auth_clients"`) || !strings.Contains(responses[1], `"name":"supadupa_deploy_project_function"`) || !strings.Contains(responses[1], `"name":"supadupa_create_project_embedding_job"`) || !strings.Contains(responses[1], `"name":"supadupa_add_project_domain"`) || !strings.Contains(responses[1], `"name":"supadupa_create_host"`) {
		t.Fatalf("expected tool list: %s", responses[1])
	}
	if !strings.Contains(responses[2], `"structuredContent":[{"name":"Alpha","ref":"alpha","status":"healthy"}]`) {
		t.Fatalf("expected structured project list: %s", responses[2])
	}
	if !strings.Contains(responses[3], `"api_url":"https://alpha.example.test"`) || !strings.Contains(responses[3], `"custom_api_urls":["https://api.example.com"]`) || !strings.Contains(responses[3], `"fqdn":"api.example.com"`) {
		t.Fatalf("expected connect payload: %s", responses[3])
	}
	if !strings.Contains(responses[4], `"analytics_buckets":1`) || !strings.Contains(responses[6], `"project_ref":"alpha"`) || !strings.Contains(responses[7], `"studio_assistant_enabled":"true"`) || !strings.Contains(responses[9], `"storage_uri":"s3://lake/events"`) {
		t.Fatalf("expected expanded MCP structured results: %#v", responses)
	}
	if !strings.Contains(responses[10], `"org_id":"org_1"`) || !strings.Contains(responses[11], `"snap_1"`) || !strings.Contains(responses[12], `"SDP-202606-0001"`) {
		t.Fatalf("expected org usage and billing MCP results: %#v", responses)
	}
	if !strings.Contains(responses[13], `"api.example.com"`) || !strings.Contains(responses[17], `"token":"********"`) || !strings.Contains(responses[20], `"status":"requested"`) {
		t.Fatalf("expected platform MCP results: %#v", responses)
	}
	if !strings.Contains(responses[22], `"orders-etl"`) || !strings.Contains(responses[23], `"destination":"s3"`) || !strings.Contains(responses[25], `"docs-embeddings"`) || !strings.Contains(responses[26], `"source_table":"documents"`) {
		t.Fatalf("expected replication and embedding MCP results: %#v", responses)
	}
	if !strings.Contains(responses[28], `"name":"hello-api"`) || !strings.Contains(responses[29], `"API_KEY":"********"`) || !strings.Contains(responses[31], `"routing_policy":"primary"`) || !strings.Contains(responses[34], `"mount_path":"/mnt/assets"`) {
		t.Fatalf("expected Edge Function MCP results: %#v", responses)
	}
	if !strings.Contains(responses[37], `"client_id":"dashboard_app"`) || !strings.Contains(responses[38], `"client_secret_handle":"********"`) || !strings.Contains(responses[40], `"hook_type":"custom_access_token"`) || !strings.Contains(responses[41], `"authorization":"********"`) {
		t.Fatalf("expected auth MCP results: %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/projects",
		"GET /v1/projects/alpha/connect",
		"GET /v1/metrics",
		"GET /v1/projects/alpha/metrics",
		"GET /v1/projects/alpha/telemetry/history",
		"GET /v1/projects/alpha/config/ai",
		"GET /v1/projects/alpha/logs",
		"GET /v1/projects/alpha/analytics-buckets",
		"GET /v1/orgs/org_1/usage",
		"GET /v1/orgs/org_1/usage/snapshots",
		"GET /v1/orgs/org_1/billing/invoices",
		"GET /v1/projects/alpha/domains",
		"POST /v1/projects/alpha/domains",
		"DELETE /v1/projects/alpha/domains/api.example.com",
		"GET /v1/projects/alpha/log-drains",
		"POST /v1/projects/alpha/log-drains",
		"DELETE /v1/projects/alpha/log-drains/drain_1",
		"GET /v1/projects/alpha/network-connections",
		"POST /v1/projects/alpha/network-connections",
		"DELETE /v1/projects/alpha/network-connections/net_1",
		"GET /v1/projects/alpha/replication",
		"POST /v1/projects/alpha/replication",
		"DELETE /v1/projects/alpha/replication/pipe_1",
		"GET /v1/projects/alpha/embeddings",
		"POST /v1/projects/alpha/embeddings",
		"DELETE /v1/projects/alpha/embeddings/emb_1",
		"GET /v1/projects/alpha/functions",
		"POST /v1/projects/alpha/functions",
		"GET /v1/projects/alpha/functions/regions",
		"POST /v1/projects/alpha/functions/regions",
		"DELETE /v1/projects/alpha/functions/regions/region_1",
		"GET /v1/projects/alpha/functions/storage-mounts",
		"POST /v1/projects/alpha/functions/storage-mounts",
		"DELETE /v1/projects/alpha/functions/storage-mounts/mount_1",
		"DELETE /v1/projects/alpha/functions/hello-api",
		"GET /v1/projects/alpha/auth/clients",
		"POST /v1/projects/alpha/auth/clients",
		"DELETE /v1/projects/alpha/auth/clients/dashboard_app",
		"GET /v1/projects/alpha/auth/hooks",
		"POST /v1/projects/alpha/auth/hooks",
		"DELETE /v1/projects/alpha/auth/hooks/custom_access_token",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected management API requests %#v", requests)
	}
	if authHeader != "Bearer test-token" {
		t.Fatalf("expected bearer token forwarding, got %q", authHeader)
	}
}

func TestRunnerReturnsToolError(t *testing.T) {
	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":"bad","method":"tools/call","params":{"name":"supadupa_get_project","arguments":{}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": "http://127.0.0.1:1"},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0 for handled RPC error, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 1 || !strings.Contains(responses[0], `"error"`) || !strings.Contains(responses[0], `ref is required`) {
		t.Fatalf("expected RPC error response, got %#v", responses)
	}
}

func TestPlatformDefaultsToolsUseSettingsEndpoint(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/settings/defaults":
			_, _ = w.Write([]byte(`{"domain":"supadupa.test","stack_version":"latest","profile":"full","resource_tier":"custom","backup_schedule":"daily","smtp":{"enabled":false,"host":"","port":587,"sender_name":"","sender_email":"","username":"","password_handle":"","tls_mode":"starttls"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/settings/defaults":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			smtp, ok := got["smtp"].(map[string]any)
			if !ok || got["domain"] != "apps.example.com" || got["stack_version"] != "2026.06.05" || got["profile"] != "orioledb" || got["resource_tier"] != "custom" || got["backup_schedule"] != "hourly" || smtp["enabled"] != true || smtp["host"] != "smtp.example.com" || smtp["port"] != float64(2525) || smtp["sender_email"] != "noreply@example.com" || smtp["password_handle"] != "secret://platform/smtp-password" || smtp["tls_mode"] != "implicit" {
				t.Fatalf("unexpected platform defaults payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"domain":"apps.example.com","stack_version":"2026.06.05","profile":"orioledb","resource_tier":"custom","backup_schedule":"hourly","smtp":{"enabled":true,"host":"smtp.example.com","port":2525,"sender_name":"supadupa","sender_email":"noreply@example.com","username":"apikey","password_handle":"secret://platform/smtp-password","tls_mode":"implicit"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_get_platform_defaults","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_set_platform_defaults","arguments":{"domain":"apps.example.com","stack_version":"2026.06.05","profile":"orioledb","resource_tier":"custom","backup_schedule":"hourly","smtp_enabled":true,"smtp_host":"smtp.example.com","smtp_port":2525,"smtp_sender_name":"supadupa","smtp_sender_email":"noreply@example.com","smtp_username":"apikey","smtp_password_handle":"secret://platform/smtp-password","smtp_tls_mode":"implicit"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 3 {
		t.Fatalf("expected three responses, got %d: %s", len(responses), output.String())
	}
	if !strings.Contains(responses[0], `"name":"supadupa_get_platform_defaults"`) || !strings.Contains(responses[0], `"name":"supadupa_set_platform_defaults"`) {
		t.Fatalf("expected platform defaults tools in list: %s", responses[0])
	}
	if !strings.Contains(responses[1], `"smtp":{"enabled":false`) || !strings.Contains(responses[2], `"password_handle":"secret://platform/smtp-password"`) {
		t.Fatalf("expected platform defaults tool results, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/settings/defaults",
		"PUT /v1/settings/defaults",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestPlatformAccountToolsUseManagementEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/provisioner":
			_, _ = w.Write([]byte(`{"provisioner":"compose"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/users":
			_, _ = w.Write([]byte(`[{"id":"usr_1","email":"admin@example.com","role":"admin","mfa_enabled":true}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/users":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["email"] != "operator@example.com" || got["password"] != "initial-secret" || got["role"] != "admin" {
				t.Fatalf("unexpected user payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"usr_2","email":"operator@example.com","role":"admin","mfa_enabled":false}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/settings/sso":
			_, _ = w.Write([]byte(`{"enabled":false,"provider":"saml","idp_entity_id":"","sso_url":"","certificate_pem":"","acs_url":"","metadata_url":"","email_domain":"","auto_provision":false,"default_role":"developer"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/settings/sso":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["enabled"] != true ||
				got["idp_entity_id"] != "https://idp.example.com/saml" ||
				got["sso_url"] != "https://idp.example.com/login" ||
				got["certificate_pem"] != "-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----" ||
				got["acs_url"] != "https://supadupa.example.com/v1/auth/sso/saml/callback" ||
				got["metadata_url"] != "https://idp.example.com/metadata" ||
				got["email_domain"] != "example.com" ||
				got["auto_provision"] != true ||
				got["default_role"] != "viewer" {
				t.Fatalf("unexpected sso payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"enabled":true,"provider":"saml","idp_entity_id":"https://idp.example.com/saml","sso_url":"https://idp.example.com/login","certificate_pem":"-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----","acs_url":"https://supadupa.example.com/v1/auth/sso/saml/callback","metadata_url":"https://idp.example.com/metadata","email_domain":"example.com","auto_provision":true,"default_role":"viewer"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/account/mfa":
			_, _ = w.Write([]byte(`{"user_id":"usr_1","email":"admin@example.com","enabled":false,"pending":false}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/account/mfa/enroll":
			_, _ = w.Write([]byte(`{"user_id":"usr_1","email":"admin@example.com","enabled":false,"pending":true,"otpauth_url":"otpauth://totp/supadupa:admin@example.com","secret":"ABCD1234"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/account/mfa/verify":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["code"] != "123456" {
				t.Fatalf("unexpected mfa verify payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"user_id":"usr_1","email":"admin@example.com","enabled":true,"pending":false}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/account/mfa":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["code"] != "123456" {
				t.Fatalf("unexpected mfa disable payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"user_id":"usr_1","email":"admin@example.com","enabled":false,"pending":false}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_get_provisioner","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_list_users","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_create_user","arguments":{"email":" Operator@Example.com ","password":" initial-secret ","role":"admin"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"supadupa_get_platform_sso","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"supadupa_set_platform_sso","arguments":{"enabled":true,"idp_entity_id":"https://idp.example.com/saml","sso_url":"https://idp.example.com/login","certificate_pem":"-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----","acs_url":"https://supadupa.example.com/v1/auth/sso/saml/callback","metadata_url":"https://idp.example.com/metadata","email_domain":" Example.com ","auto_provision":true,"default_role":"viewer"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"supadupa_get_account_mfa","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"supadupa_enroll_account_mfa","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"supadupa_verify_account_mfa","arguments":{"code":" 123456 "}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"supadupa_disable_account_mfa","arguments":{"code":"123456"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 10 ||
		!strings.Contains(responses[0], `"name":"supadupa_list_users"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_create_user"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_get_provisioner"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_get_platform_sso"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_set_platform_sso"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_get_account_mfa"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_enroll_account_mfa"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_verify_account_mfa"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_disable_account_mfa"`) ||
		!strings.Contains(responses[1], `"provisioner":"compose"`) ||
		!strings.Contains(responses[2], `"email":"admin@example.com"`) ||
		!strings.Contains(responses[3], `"email":"operator@example.com"`) ||
		!strings.Contains(responses[5], `"default_role":"viewer"`) ||
		!strings.Contains(responses[6], `"enabled":false`) ||
		!strings.Contains(responses[7], `"pending":true`) ||
		!strings.Contains(responses[8], `"enabled":true`) ||
		!strings.Contains(responses[9], `"enabled":false`) {
		t.Fatalf("expected platform account tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/provisioner",
		"GET /v1/users",
		"POST /v1/users",
		"GET /v1/settings/sso",
		"PUT /v1/settings/sso",
		"GET /v1/account/mfa",
		"POST /v1/account/mfa/enroll",
		"POST /v1/account/mfa/verify",
		"DELETE /v1/account/mfa",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestBillingInvoiceToolsUseOrgBillingEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/billing/invoices":
			if r.URL.Query().Get("limit") != "2" {
				t.Fatalf("expected invoice limit 2, got %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"id":"inv_1","number":"SDP-202606-0001","total_cents":2600}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/billing/invoices":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["usage_snapshot_id"] != "snap_1" || got["currency"] != "USD" || got["status"] != "draft" || got["due_days"] != float64(14) {
				t.Fatalf("unexpected billing invoice payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"inv_1","number":"SDP-202606-0001","usage_snapshot_id":"snap_1","status":"draft","currency":"USD","total_cents":2600}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/billing/invoices/inv_1":
			_, _ = w.Write([]byte(`{"id":"inv_1","number":"SDP-202606-0001","status":"draft","currency":"USD","total_cents":2600}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_list_billing_invoices","arguments":{"org_id":"org_1","limit":2}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_create_billing_invoice","arguments":{"org_id":"org_1","usage_snapshot_id":"snap_1","currency":"usd","status":"draft","due_days":14}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_get_billing_invoice","arguments":{"org_id":"org_1","invoice_id":"inv_1"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	responses := readTestFrames(t, output)
	if len(responses) != 4 ||
		!strings.Contains(responses[0], `"name":"supadupa_create_billing_invoice"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_get_billing_invoice"`) ||
		!strings.Contains(responses[1], `"SDP-202606-0001"`) ||
		!strings.Contains(responses[2], `"usage_snapshot_id":"snap_1"`) ||
		!strings.Contains(responses[3], `"total_cents":2600`) {
		t.Fatalf("expected billing invoice tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/orgs/org_1/billing/invoices?limit=2",
		"POST /v1/orgs/org_1/billing/invoices",
		"GET /v1/orgs/org_1/billing/invoices/inv_1",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestOrgUsageSnapshotToolsUseOrgUsageEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/usage/snapshots":
			if r.URL.Query().Get("limit") != "2" {
				t.Fatalf("expected snapshot limit 2, got %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"id":"snap_1","org_id":"org_1","totals":{"projects":2}}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/usage/snapshots":
			_, _ = w.Write([]byte(`{"id":"snap_2","org_id":"org_1","totals":{"projects":3}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_list_org_usage_snapshots","arguments":{"org_id":"org_1","limit":2}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_create_org_usage_snapshot","arguments":{"org_id":"org_1"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	responses := readTestFrames(t, output)
	if len(responses) != 3 ||
		!strings.Contains(responses[0], `"name":"supadupa_list_org_usage_snapshots"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_create_org_usage_snapshot"`) ||
		!strings.Contains(responses[1], `"snap_1"`) ||
		!strings.Contains(responses[2], `"snap_2"`) {
		t.Fatalf("expected usage snapshot tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/orgs/org_1/usage/snapshots?limit=2",
		"POST /v1/orgs/org_1/usage/snapshots",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestSCIMToolsUseSCIMEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/scim/v2/ServiceProviderConfig":
			_, _ = w.Write([]byte(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"],"patch":{"supported":true}}`))
		case "/v1/scim/v2/Users":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`{"Resources":[{"id":"usr_1","userName":"admin@example.com"}],"totalResults":1}`))
			case http.MethodPost:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				extension, ok := got["urn:supadupa:params:scim:schemas:extension:User"].(map[string]any)
				if !ok || got["userName"] != "dev@example.com" || got["displayName"] != "Dev User" || got["active"] != true || extension["role"] != "developer" {
					t.Fatalf("unexpected SCIM user create payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"usr_2","userName":"dev@example.com"}`))
			default:
				t.Fatalf("unexpected SCIM users method %s", r.Method)
			}
		case "/v1/scim/v2/Users/usr_1":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`{"id":"usr_1","userName":"admin@example.com"}`))
			case http.MethodPut:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				extension, ok := got["urn:supadupa:params:scim:schemas:extension:User"].(map[string]any)
				if !ok || got["userName"] != "admin@example.com" || got["active"] != false || extension["role"] != "admin" {
					t.Fatalf("unexpected SCIM user replace payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"usr_1","userName":"admin@example.com"}`))
			case http.MethodPatch:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				operations, _ := got["Operations"].([]any)
				if len(operations) != 1 {
					t.Fatalf("unexpected SCIM user deprovision payload %#v", got)
				}
				w.WriteHeader(http.StatusNoContent)
			case http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			default:
				t.Fatalf("unexpected SCIM user method %s", r.Method)
			}
		case "/v1/scim/v2/Groups":
			switch r.Method {
			case http.MethodGet:
				if r.URL.Query().Get("org_id") != "org_1" {
					t.Fatalf("expected org_id filter, got %s", r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(`{"Resources":[{"id":"team_1","displayName":"Developers"}],"totalResults":1}`))
			case http.MethodPost:
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				extension, ok := got["urn:supadupa:params:scim:schemas:extension:Group"].(map[string]any)
				members, _ := got["members"].([]any)
				if !ok || got["externalId"] != "org_1" || got["displayName"] != "Developers" || extension["slug"] != "developers" || len(members) != 2 {
					t.Fatalf("unexpected SCIM group create payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"team_1","displayName":"Developers"}`))
			default:
				t.Fatalf("unexpected SCIM groups method %s", r.Method)
			}
		case "/v1/scim/v2/Groups/team_1":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`{"id":"team_1","displayName":"Developers"}`))
			case http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			default:
				t.Fatalf("unexpected SCIM group method %s", r.Method)
			}
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_get_scim_service_provider_config","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_list_scim_users","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_get_scim_user","arguments":{"id":"usr_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"supadupa_create_scim_user","arguments":{"user_name":"dev@example.com","display_name":"Dev User","password":"initial-secret","role":"developer"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"supadupa_replace_scim_user","arguments":{"id":"usr_1","email":"admin@example.com","role":"admin","active":false}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"supadupa_deprovision_scim_user","arguments":{"id":"usr_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"supadupa_delete_scim_user","arguments":{"id":"usr_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"supadupa_list_scim_groups","arguments":{"org_id":"org_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"supadupa_get_scim_group","arguments":{"id":"team_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"supadupa_create_scim_group","arguments":{"org_id":"org_1","display_name":"Developers","slug":"developers","members":["dev@example.com","admin@example.com"]}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"supadupa_delete_scim_group","arguments":{"id":"team_1"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	responses := readTestFrames(t, output)
	if len(responses) != 12 ||
		!strings.Contains(responses[0], `"name":"supadupa_get_scim_service_provider_config"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_list_scim_users"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_get_scim_user"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_create_scim_user"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_replace_scim_user"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_deprovision_scim_user"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_delete_scim_user"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_list_scim_groups"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_get_scim_group"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_create_scim_group"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_delete_scim_group"`) ||
		!strings.Contains(responses[1], `"supported":true`) ||
		!strings.Contains(responses[2], `"userName":"admin@example.com"`) ||
		!strings.Contains(responses[4], `"userName":"dev@example.com"`) ||
		!strings.Contains(responses[8], `"displayName":"Developers"`) ||
		!strings.Contains(responses[10], `"displayName":"Developers"`) {
		t.Fatalf("expected SCIM tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/scim/v2/ServiceProviderConfig",
		"GET /v1/scim/v2/Users",
		"GET /v1/scim/v2/Users/usr_1",
		"POST /v1/scim/v2/Users",
		"PUT /v1/scim/v2/Users/usr_1",
		"PATCH /v1/scim/v2/Users/usr_1",
		"DELETE /v1/scim/v2/Users/usr_1",
		"GET /v1/scim/v2/Groups?org_id=org_1",
		"GET /v1/scim/v2/Groups/team_1",
		"POST /v1/scim/v2/Groups",
		"DELETE /v1/scim/v2/Groups/team_1",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestProjectLifecycleToolsUseProjectEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/upgrade":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["version"] != "2026.06.05" {
				t.Fatalf("unexpected upgrade payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"ref":"alpha","status":"healthy","spec":{"stack_version":"2026.06.05"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/scale":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["cpu"] != float64(6) || got["ram_mb"] != float64(12288) || got["disk_gb"] != float64(120) || got["enforce_limits"] != true {
				t.Fatalf("unexpected scale payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"ref":"alpha","status":"healthy","spec":{"resource_tier":"custom","cpu":6,"ram_mb":12288,"disk_gb":120,"enforce_limits":true}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha":
			if r.URL.Query().Get("retain_volumes") != "true" {
				t.Fatalf("expected retain_volumes=true, got %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_upgrade_project","arguments":{"ref":"alpha","version":"2026.06.05"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_scale_project","arguments":{"ref":"alpha","cpu":6,"ram_mb":12288,"disk_gb":120,"enforce_limits":true}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_destroy_project","arguments":{"ref":"alpha","retain_volumes":true}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"supadupa_destroy_project","arguments":{"ref":"alpha","retain_volumes":true,"confirmation":"destroy project alpha"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 5 {
		t.Fatalf("expected five responses, got %d: %s", len(responses), output.String())
	}
	if !strings.Contains(responses[0], `"name":"supadupa_upgrade_project"`) || !strings.Contains(responses[0], `"name":"supadupa_scale_project"`) || !strings.Contains(responses[0], `"name":"supadupa_destroy_project"`) {
		t.Fatalf("expected lifecycle tools in list: %s", responses[0])
	}
	if !strings.Contains(responses[1], `"stack_version":"2026.06.05"`) || !strings.Contains(responses[2], `"cpu":6`) || !strings.Contains(responses[2], `"enforce_limits":true`) {
		t.Fatalf("expected lifecycle responses, got %#v", responses)
	}
	if !strings.Contains(responses[3], `confirmation must be exactly`) {
		t.Fatalf("expected unconfirmed destroy to be rejected, got %#v", responses[3])
	}
	expectedRequests := strings.Join([]string{
		"POST /v1/projects/alpha/upgrade",
		"POST /v1/projects/alpha/scale",
		"DELETE /v1/projects/alpha?retain_volumes=true",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestProjectConfigToolsUseProjectEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/config/ai":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","area":"ai","config":{"studio_assistant_enabled":"true"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/config/ai":
			var got struct {
				Config map[string]string `json:"config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Config["studio_assistant_enabled"] != "false" || got.Config["studio_assistant_provider"] != "openai" {
				t.Fatalf("unexpected config payload %#v", got.Config)
			}
			if _, ok := got.Config[" "]; ok {
				t.Fatalf("expected blank config key to be dropped: %#v", got.Config)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","area":"ai","config":{"studio_assistant_enabled":"false","studio_assistant_provider":"openai"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_get_project_config","arguments":{"ref":"alpha","area":"ai"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_set_project_config","arguments":{"ref":"alpha","area":"ai","config":{" studio_assistant_enabled ":"false","studio_assistant_provider":"openai"," ":"ignored"}}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	responses := readTestFrames(t, output)
	if len(responses) != 3 ||
		!strings.Contains(responses[0], `"name":"supadupa_get_project_config"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_set_project_config"`) ||
		!strings.Contains(responses[1], `"studio_assistant_enabled":"true"`) ||
		!strings.Contains(responses[2], `"studio_assistant_provider":"openai"`) {
		t.Fatalf("expected project config tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/projects/alpha/config/ai",
		"PUT /v1/projects/alpha/config/ai",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestProjectServicesToolsUseProjectEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/services":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","services":{"storage":true,"functions":true,"studio":true}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/services":
			var got struct {
				Services map[string]bool `json:"services"`
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Services["storage"] != false || got.Services["functions"] != true || got.Services["studio"] != false {
				t.Fatalf("unexpected services payload %#v", got.Services)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","services":{"storage":false,"functions":true,"studio":false}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_get_project_services","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_set_project_services","arguments":{"ref":"alpha","services":{" storage ":false,"functions":true,"studio":false}}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	responses := readTestFrames(t, output)
	if len(responses) != 3 ||
		!strings.Contains(responses[0], `"name":"supadupa_get_project_services"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_set_project_services"`) ||
		!strings.Contains(responses[1], `"storage":true`) ||
		!strings.Contains(responses[2], `"storage":false`) {
		t.Fatalf("expected project services tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/projects/alpha/services",
		"PUT /v1/projects/alpha/services",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestProjectNetworkToolUsesProjectNetworkEndpoint(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/network" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"project_ref":"alpha","allowlist":"10.0.0.0/8","ssl_enforced":"true","connections":[{"id":"net_1","name":"aws-prod"}]}`))
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_get_project_network","arguments":{"ref":"alpha"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	responses := readTestFrames(t, output)
	if len(responses) != 2 ||
		!strings.Contains(responses[0], `"name":"supadupa_get_project_network"`) ||
		!strings.Contains(responses[1], `"allowlist":"10.0.0.0/8"`) ||
		!strings.Contains(responses[1], `"ssl_enforced":"true"`) {
		t.Fatalf("expected project network tool responses, got %#v", responses)
	}
	if strings.Join(requests, "\n") != "GET /v1/projects/alpha/network" {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestBucketToolsUseProjectEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/storage/buckets":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			mimeTypes, _ := got["allowed_mime_types"].([]any)
			metadata, ok := got["metadata"].(map[string]any)
			if !ok || got["name"] != "assets" || got["public"] != true || got["file_size_limit"] != float64(52428800) || len(mimeTypes) != 2 || mimeTypes[0] != "image/png" || got["cache_control"] != "3600" || got["avif_autodetection"] != true || metadata["purpose"] != "public-assets" {
				t.Fatalf("unexpected storage bucket payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"bucket_1","name":"assets","metadata":{"purpose":"public-assets"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/storage/buckets/assets":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/vector-buckets":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			metadata, ok := got["metadata"].(map[string]any)
			if !ok || got["name"] != "documents" || got["dimension"] != float64(1536) || got["distance"] != "cosine" || got["index_method"] != "hnsw" || got["storage_backend"] != "s3" || got["storage_uri"] != "s3://vectors/documents" || metadata["access_key"] != "secret://projects/alpha/vector-s3" {
				t.Fatalf("unexpected vector bucket payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"vb_1","name":"documents","metadata":{"access_key":"********"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/vector-buckets/documents":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/analytics-buckets":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			metadata, ok := got["metadata"].(map[string]any)
			if !ok || got["name"] != "events" || got["storage_uri"] != "s3://lakehouse/events" || got["catalog_uri"] != "http://iceberg-rest:8181" || got["warehouse"] != "analytics" || got["credential_handle"] != "secret://projects/alpha/iceberg" || got["format_version"] != float64(2) || got["partitioning"] != "days(created_at)" || got["retention_days"] != float64(365) || got["compaction_schedule"] != "manual" || metadata["purpose"] != "warehouse" {
				t.Fatalf("unexpected analytics bucket payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"ab_1","name":"events","credential_handle":"********","metadata":{"purpose":"warehouse"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/analytics-buckets/events":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"supadupa_create_project_storage_bucket","arguments":{"ref":"alpha","name":"assets","public":true,"file_size_limit":52428800,"allowed_mime_types":["image/png","image/jpeg"],"cache_control":"3600","avif_autodetection":true,"metadata":{"purpose":"public-assets"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_delete_project_storage_bucket","arguments":{"ref":"alpha","name":"assets"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_create_project_vector_bucket","arguments":{"ref":"alpha","name":"documents","storage_backend":"s3","storage_uri":"s3://vectors/documents","metadata":{"access_key":"secret://projects/alpha/vector-s3"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_delete_project_vector_bucket","arguments":{"ref":"alpha","name":"documents"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"supadupa_create_project_analytics_bucket","arguments":{"ref":"alpha","name":"events","storage_uri":"s3://lakehouse/events","catalog_uri":"http://iceberg-rest:8181","warehouse":"analytics","credential_handle":"secret://projects/alpha/iceberg","partitioning":"days(created_at)","retention_days":365,"metadata":{"purpose":"warehouse"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"supadupa_delete_project_analytics_bucket","arguments":{"ref":"alpha","name":"events"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 6 || !strings.Contains(responses[2], `"access_key":"********"`) || !strings.Contains(responses[4], `"credential_handle":"********"`) {
		t.Fatalf("expected bucket tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"POST /v1/projects/alpha/storage/buckets",
		"DELETE /v1/projects/alpha/storage/buckets/assets",
		"POST /v1/projects/alpha/vector-buckets",
		"DELETE /v1/projects/alpha/vector-buckets/documents",
		"POST /v1/projects/alpha/analytics-buckets",
		"DELETE /v1/projects/alpha/analytics-buckets/events",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestCDNToolsUseProjectEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/cdn/policy":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"edge_ttl_seconds":600}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/cdn/policy":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			included, _ := got["included_paths"].([]any)
			excluded, _ := got["excluded_paths"].([]any)
			if got["enabled"] != true || got["browser_ttl_seconds"] != float64(300) || got["edge_ttl_seconds"] != float64(600) || got["stale_while_revalidate_seconds"] != float64(30) || got["smart_revalidation"] != true || got["cache_control"] != "public, max-age=300, s-maxage=600" || len(included) != 1 || included[0] != "/storage/*" || len(excluded) != 1 || excluded[0] != "/storage/private/*" {
				t.Fatalf("unexpected cdn policy payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"smart_revalidation":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/cdn/invalidations":
			_, _ = w.Write([]byte(`[{"id":"inv_1","status":"completed"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/cdn/invalidations":
			var got map[string][]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if strings.Join(got["paths"], ",") != "/storage/avatar.png,/storage/*" {
				t.Fatalf("unexpected cdn invalidation payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"inv_2","status":"queued"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/cdn/object-events":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["event_id"] != "evt-1" || got["bucket"] != "assets" || got["object_path"] != "avatars/user.png" || got["event_type"] != "object_updated" {
				t.Fatalf("unexpected cdn object event payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"inv_3","source":"storage_object_event","status":"completed"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"supadupa_get_project_cdn_policy","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_set_project_cdn_policy","arguments":{"ref":"alpha","enabled":true,"browser_ttl_seconds":300,"edge_ttl_seconds":600,"stale_while_revalidate_seconds":30,"included_paths":["/storage/*"],"excluded_paths":["/storage/private/*"],"smart_revalidation":true,"cache_control":"public, max-age=300, s-maxage=600"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_list_project_cdn_invalidations","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_create_project_cdn_invalidation","arguments":{"ref":"alpha","paths":["/storage/avatar.png","/storage/*"]}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"supadupa_create_project_cdn_object_event","arguments":{"ref":"alpha","event_id":"evt-1","bucket":"assets","object_path":"avatars/user.png","event_type":"object_updated"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 5 || !strings.Contains(responses[0], `"edge_ttl_seconds":600`) || !strings.Contains(responses[4], `"source":"storage_object_event"`) {
		t.Fatalf("expected cdn tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/projects/alpha/cdn/policy",
		"PUT /v1/projects/alpha/cdn/policy",
		"GET /v1/projects/alpha/cdn/invalidations",
		"POST /v1/projects/alpha/cdn/invalidations",
		"POST /v1/projects/alpha/cdn/object-events",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestDatabaseToolsUseProjectEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/extensions":
			_, _ = w.Write([]byte(`[{"id":"default:pg_cron","name":"pg_cron","enabled":true}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/database/extensions/pg_cron":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["schema"] != "extensions" || got["version"] != "1.6" || got["enabled"] != false {
				t.Fatalf("unexpected database extension payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"ext_1","name":"pg_cron","enabled":false}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/cron-jobs":
			_, _ = w.Write([]byte(`[{"id":"cron_1","name":"refresh-rollups"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/cron-jobs":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "refresh-rollups" || got["schedule"] != "*/15 * * * *" || got["command"] != "select analytics.refresh_rollups();" || got["database"] != "postgres" || got["username"] != "postgres" || got["active"] != true || got["timeout_seconds"] != float64(90) || got["max_runtime_seconds"] != float64(120) || metadata["owner"] != "analytics" {
				t.Fatalf("unexpected database cron payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"cron_1","name":"refresh-rollups"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/cron-jobs/refresh-rollups":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/queues":
			_, _ = w.Write([]byte(`[{"id":"queue_1","name":"events"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/queues":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "events" || got["schema"] != "pgmq" || got["retention_minutes"] != float64(10080) || got["visibility_timeout_seconds"] != float64(45) || got["max_retries"] != float64(7) || got["dead_letter_queue"] != "events-dlq" || got["active"] != true || metadata["owner"] != "backend" {
				t.Fatalf("unexpected database queue payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"queue_1","name":"events"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/queues/events":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/webhooks":
			_, _ = w.Write([]byte(`[{"id":"webhook_1","name":"orders-events"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/webhooks":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			events, _ := got["events"].([]any)
			headers, _ := got["headers"].(map[string]any)
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "orders-events" || got["schema"] != "public" || got["table"] != "orders" || len(events) != 2 || events[0] != "insert" || events[1] != "update" || got["endpoint"] != "https://hooks.example.com/orders" || got["http_method"] != "POST" || got["timeout_seconds"] != float64(15) || got["retry_count"] != float64(5) || got["active"] != true || headers["Authorization"] != "secret://projects/alpha/webhooks/orders-token" || metadata["owner"] != "backend" {
				t.Fatalf("unexpected database webhook payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"webhook_1","name":"orders-events"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/webhooks/orders-events":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/schemas":
			_, _ = w.Write([]byte(`[{"id":"schema_1","name":"app-schema","version":"20260605_001"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/schemas":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "app-schema" || got["version"] != "20260605_001" || got["schema"] != "public" || got["sql"] != "create table public.accounts(id uuid primary key);" || got["apply_order"] != float64(10) || got["active"] != true || metadata["owner"] != "backend" {
				t.Fatalf("unexpected database schema payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"schema_1","name":"app-schema","version":"20260605_001"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/schemas/app-schema/20260605_001":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/roles":
			_, _ = w.Write([]byte(`[{"id":"role_1","name":"app_writer","password_secret_handle":"********"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/roles":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			memberOf, _ := got["member_of"].([]any)
			grants, _ := got["schema_grants"].(map[string]any)
			metadata, _ := got["metadata"].(map[string]any)
			if got["name"] != "app_writer" || got["login"] != true || got["inherit"] != false || got["bypass_rls"] != true || got["connection_limit"] != float64(25) || got["password_secret_handle"] != "secret://projects/alpha/db/app-writer" || len(memberOf) != 1 || memberOf[0] != "authenticated" || grants["public"] != "usage,select,insert" || metadata["purpose"] != "app" {
				t.Fatalf("unexpected database role payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"role_1","name":"app_writer","password_secret_handle":"********"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/roles/app_writer":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"supadupa_list_project_database_extensions","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_set_project_database_extension","arguments":{"ref":"alpha","name":"pg_cron","schema":"extensions","version":"1.6","enabled":false}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_list_project_database_cron_jobs","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_create_project_database_cron_job","arguments":{"ref":"alpha","name":"refresh-rollups","schedule":"*/15 * * * *","command":"select analytics.refresh_rollups();","database":"postgres","username":"postgres","timeout_seconds":90,"max_runtime_seconds":120,"metadata":{"owner":"analytics"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"supadupa_delete_project_database_cron_job","arguments":{"ref":"alpha","name":"refresh-rollups"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"supadupa_list_project_database_queues","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"supadupa_create_project_database_queue","arguments":{"ref":"alpha","name":"events","schema":"pgmq","retention_minutes":10080,"visibility_timeout_seconds":45,"max_retries":7,"dead_letter_queue":"events-dlq","metadata":{"owner":"backend"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"supadupa_delete_project_database_queue","arguments":{"ref":"alpha","name":"events"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"supadupa_list_project_database_webhooks","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"supadupa_create_project_database_webhook","arguments":{"ref":"alpha","name":"orders-events","schema":"public","table":"orders","events":["insert","update"],"endpoint":"https://hooks.example.com/orders","http_method":"POST","headers":{"Authorization":"secret://projects/alpha/webhooks/orders-token"},"timeout_seconds":15,"retry_count":5,"metadata":{"owner":"backend"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"supadupa_delete_project_database_webhook","arguments":{"ref":"alpha","name":"orders-events"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"supadupa_list_project_database_schemas","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"supadupa_create_project_database_schema","arguments":{"ref":"alpha","name":"app-schema","version":"20260605_001","schema":"public","sql":"create table public.accounts(id uuid primary key);","apply_order":10,"metadata":{"owner":"backend"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"supadupa_delete_project_database_schema","arguments":{"ref":"alpha","name":"app-schema","version":"20260605_001"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"supadupa_list_project_database_roles","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"supadupa_create_project_database_role","arguments":{"ref":"alpha","name":"app_writer","login":true,"inherit":false,"bypass_rls":true,"connection_limit":25,"password_secret_handle":"secret://projects/alpha/db/app-writer","member_of":["authenticated"],"schema_grants":{"public":"usage,select,insert"},"metadata":{"purpose":"app"}}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"supadupa_delete_project_database_role","arguments":{"ref":"alpha","name":"app_writer"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 17 || !strings.Contains(responses[1], `"enabled":false`) || !strings.Contains(responses[15], `"password_secret_handle":"********"`) {
		t.Fatalf("expected database tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/projects/alpha/database/extensions",
		"PUT /v1/projects/alpha/database/extensions/pg_cron",
		"GET /v1/projects/alpha/database/cron-jobs",
		"POST /v1/projects/alpha/database/cron-jobs",
		"DELETE /v1/projects/alpha/database/cron-jobs/refresh-rollups",
		"GET /v1/projects/alpha/database/queues",
		"POST /v1/projects/alpha/database/queues",
		"DELETE /v1/projects/alpha/database/queues/events",
		"GET /v1/projects/alpha/database/webhooks",
		"POST /v1/projects/alpha/database/webhooks",
		"DELETE /v1/projects/alpha/database/webhooks/orders-events",
		"GET /v1/projects/alpha/database/schemas",
		"POST /v1/projects/alpha/database/schemas",
		"DELETE /v1/projects/alpha/database/schemas/app-schema/20260605_001",
		"GET /v1/projects/alpha/database/roles",
		"POST /v1/projects/alpha/database/roles",
		"DELETE /v1/projects/alpha/database/roles/app_writer",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestOperationalToolsUseProjectEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/backups/policy":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["enabled"] != true || got["schedule"] != "0 2 * * *" || got["kind"] != "physical" {
				t.Fatalf("unexpected backup policy payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"schedule":"0 2 * * *","kind":"physical"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/restore":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["backup_id"] != "bkp_123" || got["confirmation"] != "restore project alpha" {
				t.Fatalf("unexpected restore payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"restore_state":"completed","backup":{"id":"bkp_123"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/pitr/policy":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["enabled"] != true || got["archive_bucket"] != "s3://archive/alpha" || got["retention_days"] != float64(14) {
				t.Fatalf("unexpected pitr policy payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"archive_bucket":"s3://archive/alpha","retention_days":14}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/pitr/wal":
			_, _ = w.Write([]byte(`[{"id":"wal_1","status":"archived"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/pitr/wal":
			_, _ = w.Write([]byte(`{"id":"wal_2","status":"archived"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/branches":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["ref"] != "alpha-preview" || got["name"] != "Alpha Preview" || got["ttl_hours"] != float64(24) || got["with_data"] != true {
				t.Fatalf("unexpected branch payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"branch":{"project_ref":"alpha-preview"},"project":{"ref":"alpha-preview"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/branches/alpha-preview":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/replicas":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["name"] != "east" || got["host_id"] != "host-one" || got["region"] != "us-east" || got["tier"] != "medium" || got["read_weight"] != float64(75) || got["failover_priority"] != float64(2) {
				t.Fatalf("unexpected replica payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"replica_1","name":"east","status":"healthy"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/replicas/replica_1/promote":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["reason"] != "planned maintenance" {
				t.Fatalf("unexpected promote payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"replica_1","role":"primary"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/replicas/failover":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["reason"] != "primary degraded" {
				t.Fatalf("unexpected failover payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"replica_1","role":"primary"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/replicas/replica_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"supadupa_set_project_backup_policy","arguments":{"ref":"alpha","enabled":true,"schedule":"0 2 * * *","kind":"physical"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_restore_project_backup","arguments":{"ref":"alpha","backup_id":"bkp_123","confirmation":"restore project alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_set_project_pitr_policy","arguments":{"ref":"alpha","enabled":true,"archive_bucket":"s3://archive/alpha","retention_days":14}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_list_project_wal_archives","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"supadupa_archive_project_wal","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"supadupa_create_project_branch","arguments":{"ref":"alpha","branch_ref":"alpha-preview","name":"Alpha Preview","ttl_hours":24,"with_data":true}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"supadupa_delete_project_branch","arguments":{"ref":"alpha","branch_ref":"alpha-preview"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"supadupa_create_project_replica","arguments":{"ref":"alpha","name":"east","host_id":"host-one","region":"us-east","tier":"medium","read_weight":75,"failover_priority":2}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"supadupa_promote_project_replica","arguments":{"ref":"alpha","id":"replica_1","reason":"planned maintenance"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"supadupa_failover_project_replica","arguments":{"ref":"alpha","reason":"primary degraded"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"supadupa_delete_project_replica","arguments":{"ref":"alpha","id":"replica_1"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 11 || !strings.Contains(responses[1], `"restore_state":"completed"`) || !strings.Contains(responses[5], `"alpha-preview"`) || !strings.Contains(responses[9], `"role":"primary"`) {
		t.Fatalf("expected operational tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"PUT /v1/projects/alpha/backups/policy",
		"POST /v1/projects/alpha/restore",
		"PUT /v1/projects/alpha/pitr/policy",
		"GET /v1/projects/alpha/pitr/wal",
		"POST /v1/projects/alpha/pitr/wal",
		"POST /v1/projects/alpha/branches",
		"DELETE /v1/projects/alpha/branches/alpha-preview",
		"POST /v1/projects/alpha/replicas",
		"POST /v1/projects/alpha/replicas/replica_1/promote",
		"POST /v1/projects/alpha/replicas/failover",
		"DELETE /v1/projects/alpha/replicas/replica_1",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestSecretToolsUseAuditedProjectEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/secrets":
			_, _ = w.Write([]byte(`[{"kind":"service_role","masked_value":"********","rotated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/secrets/service_role/reveal":
			_, _ = w.Write([]byte(`{"kind":"service_role","value":"svc_secret_value"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/secrets/service_role/copy":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/keys/rotate":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["kind"] != "service_role" {
				t.Fatalf("unexpected rotate payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"kind":"service_role","masked_value":"********","rotated_at":"2026-06-05T12:30:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":0,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"supadupa_list_project_secrets","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_reveal_project_secret","arguments":{"ref":"alpha","kind":"service_role"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_record_project_secret_copy","arguments":{"ref":"alpha","kind":"service_role"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_rotate_project_secret","arguments":{"ref":"alpha","kind":"service_role"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 5 {
		t.Fatalf("expected secret tool responses, got %#v", responses)
	}
	if strings.Contains(responses[0], `"name":"supadupa_reveal_project_secret"`) || !strings.Contains(responses[0], `"name":"supadupa_record_project_secret_copy"`) {
		t.Fatalf("expected reveal tool to be hidden by default, got %s", responses[0])
	}
	if !strings.Contains(responses[1], `"masked_value":"********"`) || !strings.Contains(responses[2], "supadupa_reveal_project_secret is disabled") || strings.Contains(responses[2], "svc_secret_value") || strings.Contains(responses[3], `"value"`) || !strings.Contains(responses[4], `"rotated_at":"2026-06-05T12:30:00Z"`) {
		t.Fatalf("expected default secret tool responses to stay redacted, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/projects/alpha/secrets",
		"POST /v1/projects/alpha/secrets/service_role/copy",
		"POST /v1/projects/alpha/keys/rotate",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}

	requests = nil
	revealInput := bytes.NewBuffer(nil)
	writeTestFrame(t, revealInput, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"supadupa_reveal_project_secret","arguments":{"ref":"alpha","kind":"service_role"}}}`)
	revealOutput := bytes.NewBuffer(nil)
	exit = Runner{
		Stdin:  revealInput,
		Stdout: revealOutput,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL, "SUPADUPA_MCP_ALLOW_SECRET_REVEAL": "true"},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected opt-in reveal exit 0, got %d", exit)
	}
	revealResponses := readTestFrames(t, revealOutput)
	if len(revealResponses) != 1 || !strings.Contains(revealResponses[0], `"value":"svc_secret_value"`) {
		t.Fatalf("expected opt-in reveal response, got %#v", revealResponses)
	}
	if strings.Join(requests, "\n") != "GET /v1/projects/alpha/secrets/service_role/reveal" {
		t.Fatalf("unexpected opt-in reveal requests %#v", requests)
	}
}

func TestOperatorToolsUsePlatformEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/advisor":
			_, _ = w.Write([]byte(`[{"id":"finding_1","severity":"warning","message":"PITR is disabled"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/compliance/report":
			_, _ = w.Write([]byte(`{"status":"action_required","controls":[{"id":"audit-log","status":"implemented"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/audit-events":
			_, _ = w.Write([]byte(`[{"id":"audit_1","action":"project.secret_reveal"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/audit-events/integrity":
			_, _ = w.Write([]byte(`{"verified":true,"event_count":42}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"supadupa_get_advisor_findings","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_get_compliance_report","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_list_audit_events","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_get_audit_integrity","arguments":{}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 4 || !strings.Contains(responses[0], `"finding_1"`) || !strings.Contains(responses[1], `"audit-log"`) || !strings.Contains(responses[2], `"project.secret_reveal"`) || !strings.Contains(responses[3], `"verified":true`) {
		t.Fatalf("expected operator tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/advisor",
		"GET /v1/compliance/report",
		"GET /v1/audit-events",
		"GET /v1/audit-events/integrity",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestHostToolsUsePlatformEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/hosts":
			_, _ = w.Write([]byte(`[{"id":"host_1","name":"east-1a","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/hosts":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			capacity, ok := got["capacity"].(map[string]any)
			if !ok || got["name"] != "east-1a" || got["address"] != "10.0.0.12" || capacity["cpu"] != float64(8) || capacity["ram_mb"] != float64(32768) || capacity["disk_gb"] != float64(500) || capacity["projects"] != float64(10) {
				t.Fatalf("unexpected host payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"host_1","name":"east-1a","address":"10.0.0.12"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/hosts/host_1":
			_, _ = w.Write([]byte(`{"id":"host_1","name":"east-1a","used":{"projects":1}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/hosts/host_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"supadupa_list_hosts","arguments":{}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_create_host","arguments":{"name":"east-1a","address":"10.0.0.12","capacity_cpu":8,"capacity_ram_mb":32768,"capacity_disk_gb":500,"capacity_projects":10}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_get_host","arguments":{"id":"host_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_delete_host","arguments":{"id":"host_1"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 4 || !strings.Contains(responses[0], `"id":"host_1"`) || !strings.Contains(responses[1], `"address":"10.0.0.12"`) || !strings.Contains(responses[2], `"projects":1`) {
		t.Fatalf("expected host tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/hosts",
		"POST /v1/hosts",
		"GET /v1/hosts/host_1",
		"DELETE /v1/hosts/host_1",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestOrgAccessToolsUsePlatformEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/quotas":
			_, _ = w.Write([]byte(`{"org_id":"org_1","max_projects":4}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/orgs/org_1/quotas":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["max_projects"] != float64(4) || got["max_cpu"] != float64(16) || got["max_ram_mb"] != float64(65536) || got["max_disk_gb"] != float64(1000) {
				t.Fatalf("unexpected quota payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"org_id":"org_1","max_projects":4,"max_cpu":16}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/members":
			_, _ = w.Write([]byte(`[{"email":"dev@example.com","role":"developer"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/members":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["email"] != "dev@example.com" || got["role"] != "admin" {
				t.Fatalf("unexpected member payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"email":"dev@example.com","role":"admin"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/orgs/org_1/members/dev@example.com":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/teams":
			_, _ = w.Write([]byte(`[{"slug":"developers","name":"Developers"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/teams":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["name"] != "Developers" || got["slug"] != "developers" {
				t.Fatalf("unexpected team payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"slug":"developers","name":"Developers"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/teams/developers/members":
			_, _ = w.Write([]byte(`[{"email":"dev@example.com"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/teams/developers/members":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["email"] != "dev@example.com" {
				t.Fatalf("unexpected team member payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"email":"dev@example.com"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/orgs/org_1/teams/developers/members/dev@example.com":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/orgs/org_1/teams/developers":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/access-review":
			_, _ = w.Write([]byte(`{"org_id":"org_1","projects":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/access":
			_, _ = w.Write([]byte(`[{"subject_type":"team","subject_id":"developers","role":"developer"}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/access":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["subject_type"] != "team" || got["subject_id"] != "developers" || got["role"] != "developer" {
				t.Fatalf("unexpected project access payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","subject_type":"team","subject_id":"developers","role":"developer"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/access/team/developers":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"supadupa_get_org_quota","arguments":{"org_id":"org_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_set_org_quota","arguments":{"org_id":"org_1","max_projects":4,"max_cpu":16,"max_ram_mb":65536,"max_disk_gb":1000}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_list_org_members","arguments":{"org_id":"org_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_upsert_org_member","arguments":{"org_id":"org_1","email":"dev@example.com","role":"admin"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"supadupa_delete_org_member","arguments":{"org_id":"org_1","email":"dev@example.com"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"supadupa_list_org_teams","arguments":{"org_id":"org_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"supadupa_create_org_team","arguments":{"org_id":"org_1","name":"Developers","slug":"developers"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"supadupa_list_org_team_members","arguments":{"org_id":"org_1","slug":"developers"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"supadupa_add_org_team_member","arguments":{"org_id":"org_1","slug":"developers","email":"dev@example.com"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"supadupa_delete_org_team_member","arguments":{"org_id":"org_1","slug":"developers","email":"dev@example.com"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"supadupa_delete_org_team","arguments":{"org_id":"org_1","slug":"developers"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"supadupa_get_org_access_review","arguments":{"org_id":"org_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"supadupa_list_project_access","arguments":{"ref":"alpha"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"supadupa_grant_project_access","arguments":{"ref":"alpha","subject_type":"team","subject_id":"developers","role":"developer"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"supadupa_revoke_project_access","arguments":{"ref":"alpha","subject_type":"team","subject_id":"developers"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}
	responses := readTestFrames(t, output)
	if len(responses) != 15 || !strings.Contains(responses[1], `"max_cpu":16`) || !strings.Contains(responses[6], `"developers"`) || !strings.Contains(responses[11], `"org_id":"org_1"`) || !strings.Contains(responses[13], `"role":"developer"`) {
		t.Fatalf("expected org access tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/orgs/org_1/quotas",
		"PUT /v1/orgs/org_1/quotas",
		"GET /v1/orgs/org_1/members",
		"POST /v1/orgs/org_1/members",
		"DELETE /v1/orgs/org_1/members/dev@example.com",
		"GET /v1/orgs/org_1/teams",
		"POST /v1/orgs/org_1/teams",
		"GET /v1/orgs/org_1/teams/developers/members",
		"POST /v1/orgs/org_1/teams/developers/members",
		"DELETE /v1/orgs/org_1/teams/developers/members/dev@example.com",
		"DELETE /v1/orgs/org_1/teams/developers",
		"GET /v1/orgs/org_1/access-review",
		"GET /v1/projects/alpha/access",
		"PUT /v1/projects/alpha/access",
		"DELETE /v1/projects/alpha/access/team/developers",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestCreateOrgAndProjectToolsUseManagementEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["name"] != "Platform" {
				t.Fatalf("unexpected org payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"org_1","name":"Platform"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/projects":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			services, _ := got["services"].(map[string]any)
			environment, _ := got["environment"].(map[string]any)
			if got["ref"] != "alpha" ||
				got["name"] != "Alpha" ||
				got["host_id"] != "host-1" ||
				got["domain"] != "apps.example.test" ||
				got["stack_version"] != "2026.06.01" ||
				got["profile"] != "full" ||
				got["resource_tier"] != "medium" ||
				services["storage"] != false ||
				services["realtime"] != true ||
				environment["SUPADUPA_TEST"] != "1" {
				t.Fatalf("unexpected project payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"ref":"alpha","name":"Alpha","status":"healthy"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_create_org","arguments":{"name":" Platform "}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_create_project","arguments":{"org_id":"org_1","ref":" alpha ","name":" Alpha ","host_id":"host-1","domain":"apps.example.test","stack_version":"2026.06.01","profile":"full","resource_tier":"medium","services":{"storage":false,"realtime":true},"environment":{" SUPADUPA_TEST ":" 1 "}}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	responses := readTestFrames(t, output)
	if len(responses) != 3 ||
		!strings.Contains(responses[0], `"name":"supadupa_create_org"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_create_project"`) ||
		!strings.Contains(responses[1], `"id":"org_1"`) ||
		!strings.Contains(responses[2], `"ref":"alpha"`) {
		t.Fatalf("expected create org/project tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"POST /v1/orgs",
		"POST /v1/orgs/org_1/projects",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestOrgCRUDToolsUseManagementEndpoints(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1":
			_, _ = w.Write([]byte(`{"id":"org_1","name":"Platform"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/orgs/org_1":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["name"] != "Core Platform" {
				t.Fatalf("unexpected org update payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"org_1","name":"Core Platform"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/projects":
			_, _ = w.Write([]byte(`[{"ref":"alpha","name":"Alpha","org_id":"org_1"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/orgs/org_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	input := bytes.NewBuffer(nil)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"supadupa_get_org","arguments":{"org_id":"org_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"supadupa_update_org","arguments":{"org_id":"org_1","name":" Core Platform "}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supadupa_list_org_projects","arguments":{"org_id":"org_1"}}}`)
	writeTestFrame(t, input, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"supadupa_delete_org","arguments":{"org_id":"org_1"}}}`)
	output := bytes.NewBuffer(nil)

	exit := Runner{
		Stdin:  input,
		Stdout: output,
		Stderr: io.Discard,
		Env:    map[string]string{"SUPADUPA_API_URL": api.URL},
	}.Run(context.Background(), nil)
	if exit != 0 {
		t.Fatalf("expected exit 0, got %d", exit)
	}

	responses := readTestFrames(t, output)
	if len(responses) != 5 ||
		!strings.Contains(responses[0], `"name":"supadupa_get_org"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_update_org"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_delete_org"`) ||
		!strings.Contains(responses[0], `"name":"supadupa_list_org_projects"`) ||
		!strings.Contains(responses[2], `"name":"Core Platform"`) ||
		!strings.Contains(responses[3], `"ref":"alpha"`) {
		t.Fatalf("expected org CRUD tool responses, got %#v", responses)
	}
	expectedRequests := strings.Join([]string{
		"GET /v1/orgs/org_1",
		"PUT /v1/orgs/org_1",
		"GET /v1/orgs/org_1/projects",
		"DELETE /v1/orgs/org_1",
	}, "\n")
	if strings.Join(requests, "\n") != expectedRequests {
		t.Fatalf("unexpected requests %#v", requests)
	}
}

func TestReadMessageRejectsOversizedContentLength(t *testing.T) {
	input := bytes.NewBufferString("Content-Length: " + strconv.Itoa(maxMCPMessageBytes+1) + "\r\n\r\n")
	_, err := readMessage(bufio.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "MCP message exceeded") {
		t.Fatalf("expected oversized MCP frame rejection, got %v", err)
	}
}

func TestReadMessageRejectsOversizedBareJSONLine(t *testing.T) {
	input := bytes.NewBufferString("{" + strings.Repeat("x", maxMCPMessageBytes+1))
	_, err := readMessage(bufio.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "MCP message exceeded") {
		t.Fatalf("expected oversized MCP line rejection, got %v", err)
	}
}

func TestReadMessageRejectsOversizedHeaderLine(t *testing.T) {
	input := bytes.NewBufferString("X-Header: " + strings.Repeat("x", maxMCPHeaderLineBytes+1) + "\r\nContent-Length: 2\r\n\r\n{}")
	_, err := readMessage(bufio.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "MCP header exceeded") {
		t.Fatalf("expected oversized MCP header rejection, got %v", err)
	}
}

func TestReadMessageRejectsOversizedBareJSONLineBeforeReadingWholeInput(t *testing.T) {
	input := &countingReader{r: io.MultiReader(
		strings.NewReader("{"),
		io.LimitReader(repeatByteReader('x'), int64(4*maxMCPMessageBytes)),
		strings.NewReader("\n"),
	)}
	_, err := readMessage(bufio.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "MCP message exceeded") {
		t.Fatalf("expected oversized MCP line rejection, got %v", err)
	}
	if input.n >= 2*maxMCPMessageBytes {
		t.Fatalf("expected bounded read near %d bytes, read %d bytes", maxMCPMessageBytes, input.n)
	}
}

func TestReadMessageRejectsOversizedHeaderLineBeforeReadingWholeInput(t *testing.T) {
	input := &countingReader{r: io.MultiReader(
		strings.NewReader("X-Header: "),
		io.LimitReader(repeatByteReader('x'), int64(4*maxMCPMessageBytes)),
		strings.NewReader("\r\nContent-Length: 2\r\n\r\n{}"),
	)}
	_, err := readMessage(bufio.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "MCP header exceeded") {
		t.Fatalf("expected oversized MCP header rejection, got %v", err)
	}
	if input.n >= maxMCPMessageBytes {
		t.Fatalf("expected header read bounded near %d bytes, read %d bytes", maxMCPHeaderLineBytes, input.n)
	}
}

func TestAPIClientRejectsOversizedResponses(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, io.LimitReader(strings.NewReader(strings.Repeat("x", maxManagementAPIBytes+1)), maxManagementAPIBytes+1))
	}))
	t.Cleanup(api.Close)

	client := apiClient{baseURL: api.URL, client: api.Client()}
	_, _, err := client.do(context.Background(), http.MethodGet, "/v1/projects", nil)
	if err == nil || !strings.Contains(err.Error(), "management API response exceeded") {
		t.Fatalf("expected oversized API response rejection, got %v", err)
	}
}

func writeTestFrame(t *testing.T, out *bytes.Buffer, payload string) {
	t.Helper()
	if !json.Valid([]byte(payload)) {
		t.Fatalf("invalid JSON test payload: %s", payload)
	}
	_, _ = out.WriteString("Content-Length: ")
	_, _ = out.WriteString(strconv.Itoa(len(payload)))
	_, _ = out.WriteString("\r\n\r\n")
	_, _ = out.WriteString(payload)
}

func readTestFrames(t *testing.T, input *bytes.Buffer) []string {
	t.Helper()
	reader := bufio.NewReader(input)
	var frames []string
	for {
		payload, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read test frame: %v", err)
		}
		frames = append(frames, string(payload))
	}
	return frames
}

type countingReader struct {
	r io.Reader
	n int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += n
	return n, err
}

type repeatByteReader byte

func (r repeatByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}
