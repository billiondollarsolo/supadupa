package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"supadupa2026/internal/control"
)

func TestProjectReplicationPipelinesCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"repl-proj","name":"Replication","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/repl-proj/replication", `{"name":"bad","type":"etl","source_table":"orders","destination":"bigquery","config":{}}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid replication config 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/repl-proj/replication", `{"name":"orders-etl","type":"etl","source_schema":"public","source_table":"orders","destination":"s3","credential_handle":"secret://projects/repl-proj/etl","config":{"bucket":"analytics-lake","access_key":"secret://projects/repl-proj/s3-access-key"}}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected replication create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	pipelineID := extractString(t, createResponse.Body.String(), "id")
	for _, expected := range []string{
		`"name":"orders-etl"`,
		`"type":"etl"`,
		`"source_schema":"public"`,
		`"source_table":"orders"`,
		`"destination":"s3"`,
		`"credential_handle":"secret://projects/repl-proj/etl"`,
		`"access_key":"********"`,
	} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected create response value %s: %s", expected, createResponse.Body.String())
		}
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/repl-proj/replication", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"orders-etl"`) {
		t.Fatalf("expected replication pipeline in list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/repl-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"replication_pipelines":1`) {
		t.Fatalf("expected project replication metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"replication_pipelines":1`) {
		t.Fatalf("expected org replication usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"replication_pipelines":1`) {
		t.Fatalf("expected fleet replication metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_replication_pipelines_total 1") {
		t.Fatalf("expected prometheus replication metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/repl-proj/replication/"+pipelineID, "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected replication delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	listResponse = perform(server, http.MethodGet, "/v1/projects/repl-proj/replication", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"orders-etl"`) {
		t.Fatalf("expected empty replication list after delete: %d %s", listResponse.Code, listResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/repl-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Replication pipeline configured") || !strings.Contains(logsResponse.Body.String(), "Replication pipeline deleted") {
		t.Fatalf("expected replication project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{`"action":"project.replication_create"`, `"action":"project.replication_delete"`} {
		if !strings.Contains(auditResponse.Body.String(), action) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectVectorAIResourcesCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"ai-proj","name":"AI","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidEmbeddingResponse := perform(server, http.MethodPost, "/v1/projects/ai-proj/embeddings", `{"name":"bad","source_table":"documents","source_column":"body text"}`)
	if invalidEmbeddingResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid embedding 400, got %d: %s", invalidEmbeddingResponse.Code, invalidEmbeddingResponse.Body.String())
	}
	embeddingResponse := perform(server, http.MethodPost, "/v1/projects/ai-proj/embeddings", `{"name":"docs-embeddings","source_schema":"public","source_table":"documents","source_column":"body","primary_key_column":"id","destination_table":"document_embeddings","destination_column":"embedding","provider":"openai","model":"text-embedding-3-small","dimension":1536,"schedule":"manual","batch_size":100}`)
	if embeddingResponse.Code != http.StatusCreated {
		t.Fatalf("expected embedding create 201, got %d: %s", embeddingResponse.Code, embeddingResponse.Body.String())
	}
	embeddingID := extractString(t, embeddingResponse.Body.String(), "id")
	for _, expected := range []string{`"name":"docs-embeddings"`, `"source_table":"documents"`, `"source_column":"body"`, `"provider":"openai"`, `"dimension":1536`, `"status":"configured"`} {
		if !strings.Contains(embeddingResponse.Body.String(), expected) {
			t.Fatalf("expected embedding response value %s: %s", expected, embeddingResponse.Body.String())
		}
	}
	invalidBucketResponse := perform(server, http.MethodPost, "/v1/projects/ai-proj/vector-buckets", `{"name":"documents","storage_backend":"s3","metadata":{"access_key":"raw"}}`)
	if invalidBucketResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid vector bucket 400, got %d: %s", invalidBucketResponse.Code, invalidBucketResponse.Body.String())
	}
	bucketResponse := perform(server, http.MethodPost, "/v1/projects/ai-proj/vector-buckets", `{"name":"documents","dimension":1536,"distance":"cosine","index_method":"hnsw","storage_backend":"s3","storage_uri":"s3://vector-buckets/documents","metadata":{"purpose":"semantic-search","access_key":"secret://projects/ai-proj/vector-s3"}}`)
	if bucketResponse.Code != http.StatusCreated {
		t.Fatalf("expected vector bucket create 201, got %d: %s", bucketResponse.Code, bucketResponse.Body.String())
	}
	for _, expected := range []string{`"name":"documents"`, `"distance":"cosine"`, `"index_method":"hnsw"`, `"storage_backend":"s3"`, `"access_key":"********"`} {
		if !strings.Contains(bucketResponse.Body.String(), expected) {
			t.Fatalf("expected vector bucket response value %s: %s", expected, bucketResponse.Body.String())
		}
	}

	embeddingsList := perform(server, http.MethodGet, "/v1/projects/ai-proj/embeddings", "")
	if embeddingsList.Code != http.StatusOK || !strings.Contains(embeddingsList.Body.String(), `"name":"docs-embeddings"`) {
		t.Fatalf("expected embedding list: %d %s", embeddingsList.Code, embeddingsList.Body.String())
	}
	bucketsList := perform(server, http.MethodGet, "/v1/projects/ai-proj/vector-buckets", "")
	if bucketsList.Code != http.StatusOK || !strings.Contains(bucketsList.Body.String(), `"name":"documents"`) || !strings.Contains(bucketsList.Body.String(), `"access_key":"********"`) {
		t.Fatalf("expected vector bucket list: %d %s", bucketsList.Code, bucketsList.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/ai-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"embedding_jobs":1`) || !strings.Contains(projectMetricsResponse.Body.String(), `"vector_buckets":1`) {
		t.Fatalf("expected project vector ai metrics: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"embedding_jobs":1`) || !strings.Contains(usageResponse.Body.String(), `"vector_buckets":1`) {
		t.Fatalf("expected org vector ai usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"embedding_jobs":1`) || !strings.Contains(fleetResponse.Body.String(), `"vector_buckets":1`) {
		t.Fatalf("expected fleet vector ai metrics: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_embedding_jobs_total 1") || !strings.Contains(prometheusResponse.Body.String(), "supadupa_vector_buckets_total 1") {
		t.Fatalf("expected prometheus vector ai metrics: %s", prometheusResponse.Body.String())
	}

	deleteEmbeddingResponse := perform(server, http.MethodDelete, "/v1/projects/ai-proj/embeddings/"+embeddingID, "")
	if deleteEmbeddingResponse.Code != http.StatusNoContent {
		t.Fatalf("expected embedding delete 204, got %d: %s", deleteEmbeddingResponse.Code, deleteEmbeddingResponse.Body.String())
	}
	deleteBucketResponse := perform(server, http.MethodDelete, "/v1/projects/ai-proj/vector-buckets/documents", "")
	if deleteBucketResponse.Code != http.StatusNoContent {
		t.Fatalf("expected vector bucket delete 204, got %d: %s", deleteBucketResponse.Code, deleteBucketResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/ai-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Embedding job configured") || !strings.Contains(logsResponse.Body.String(), "Vector bucket configured") || !strings.Contains(logsResponse.Body.String(), "Embedding job deleted") || !strings.Contains(logsResponse.Body.String(), "Vector bucket deleted") {
		t.Fatalf("expected vector ai project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.embedding_create", "project.vector_bucket_create", "project.embedding_delete", "project.vector_bucket_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectAnalyticsBucketsCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"analytics-proj","name":"Analytics","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/analytics-proj/analytics-buckets", `{"name":"events","storage_uri":"http://bucket/path"}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid analytics bucket 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	rawSecretResponse := perform(server, http.MethodPost, "/v1/projects/analytics-proj/analytics-buckets", `{"name":"events","storage_uri":"s3://lakehouse/events","metadata":{"access_key":"raw"}}`)
	if rawSecretResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected raw analytics metadata secret 400, got %d: %s", rawSecretResponse.Code, rawSecretResponse.Body.String())
	}
	createResponse := perform(server, http.MethodPost, "/v1/projects/analytics-proj/analytics-buckets", `{"name":"events","storage_uri":"s3://lakehouse/events","catalog_uri":"http://iceberg-rest:8181","warehouse":"analytics","credential_handle":"secret://projects/analytics-proj/iceberg","format_version":2,"partitioning":"days(created_at)","retention_days":365,"compaction_schedule":"0 2 * * *","metadata":{"purpose":"warehouse","access_key":"secret://projects/analytics-proj/s3"}}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected analytics bucket create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"events"`, `"storage_uri":"s3://lakehouse/events"`, `"catalog_uri":"http://iceberg-rest:8181"`, `"credential_handle":"********"`, `"format_version":2`, `"partitioning":"days(created_at)"`, `"access_key":"********"`, `"status":"configured"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected analytics bucket response value %s: %s", expected, createResponse.Body.String())
		}
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/analytics-proj/analytics-buckets", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"events"`) || !strings.Contains(listResponse.Body.String(), `"credential_handle":"********"`) {
		t.Fatalf("expected analytics bucket list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/analytics-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"analytics_buckets":1`) {
		t.Fatalf("expected project analytics metrics: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"analytics_buckets":1`) {
		t.Fatalf("expected org analytics usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"analytics_buckets":1`) {
		t.Fatalf("expected fleet analytics metrics: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_analytics_buckets_total 1") {
		t.Fatalf("expected prometheus analytics metrics: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/analytics-proj/analytics-buckets/events", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected analytics bucket delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/analytics-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Analytics bucket configured") || !strings.Contains(logsResponse.Body.String(), "Analytics bucket deleted") {
		t.Fatalf("expected analytics bucket project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.analytics_bucket_create", "project.analytics_bucket_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectStorageBucketsCreateListDeleteMetricsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"storage-proj","name":"Storage","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/storage-proj/storage/buckets", `{"name":"assets","allowed_mime_types":["not-a-mime"]}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid bucket 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	createResponse := perform(server, http.MethodPost, "/v1/projects/storage-proj/storage/buckets", `{"name":"assets","public":true,"file_size_limit":1048576,"allowed_mime_types":["image/png","image/jpeg"],"cache_control":"600","avif_autodetection":true,"metadata":{"purpose":"public-assets","access_key":"secret://projects/storage-proj/storage-s3"}}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage bucket create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{`"name":"assets"`, `"public":true`, `"file_size_limit":1048576`, `"cache_control":"600"`, `"avif_autodetection":true`, `"access_key":"********"`} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected storage bucket response value %s: %s", expected, createResponse.Body.String())
		}
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/storage-proj/storage/buckets", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"assets"`) || !strings.Contains(listResponse.Body.String(), `"access_key":"********"`) {
		t.Fatalf("expected storage bucket list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	projectMetricsResponse := perform(server, http.MethodGet, "/v1/projects/storage-proj/metrics", "")
	if !strings.Contains(projectMetricsResponse.Body.String(), `"storage_buckets":1`) {
		t.Fatalf("expected project storage bucket metric: %s", projectMetricsResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"storage_buckets":1`) {
		t.Fatalf("expected org storage bucket usage: %s", usageResponse.Body.String())
	}
	fleetResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(fleetResponse.Body.String(), `"storage_buckets":1`) {
		t.Fatalf("expected fleet storage bucket metric: %s", fleetResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_storage_buckets_total 1") {
		t.Fatalf("expected prometheus storage bucket metric: %s", prometheusResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/storage-proj/storage/buckets/assets", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected storage bucket delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/storage-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Storage bucket configured") || !strings.Contains(logsResponse.Body.String(), "Storage bucket deleted") {
		t.Fatalf("expected storage bucket project logs: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.storage_bucket_create", "project.storage_bucket_delete"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected audit action %s: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectStorageBucketsApplyToStorageDataPlane(t *testing.T) {
	t.Setenv("SUPADUPA_STORAGE_APPLY", "true")
	var requestsMu sync.Mutex
	requests := []string{}
	storageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		requestsMu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("apikey") == "" {
			http.Error(w, "missing service role", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/storage/v1/bucket":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload["id"] != "assets" || payload["name"] != "assets" || payload["public"] != true {
				http.Error(w, fmt.Sprintf("unexpected payload %#v", payload), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"assets"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/storage/v1/bucket/assets":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer storageServer.Close()
	t.Setenv("SUPADUPA_STORAGE_APPLY_BASE_URL", storageServer.URL)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"storage-live","name":"Storage Live","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/storage-live/storage/buckets", `{"name":"assets","public":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage bucket create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/storage-live/storage/buckets/assets", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected storage bucket delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(requests) != 2 || !strings.HasPrefix(requests[0], "POST /storage/v1/bucket Bearer ") || !strings.HasPrefix(requests[1], "DELETE /storage/v1/bucket/assets Bearer ") {
		t.Fatalf("unexpected storage data-plane requests: %#v", requests)
	}
}

func TestProjectStorageBucketCreateRollsBackMetadataWhenDataPlaneFails(t *testing.T) {
	t.Setenv("SUPADUPA_STORAGE_APPLY", "true")
	storageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "storage unavailable", http.StatusBadGateway)
	}))
	defer storageServer.Close()
	t.Setenv("SUPADUPA_STORAGE_APPLY_BASE_URL", storageServer.URL)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"storage-fail","name":"Storage Fail","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/storage-fail/storage/buckets", `{"name":"assets","public":true}`)
	if createResponse.Code != http.StatusConflict {
		t.Fatalf("expected storage bucket create conflict, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/storage-fail/storage/buckets", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"assets"`) {
		t.Fatalf("expected failed bucket create to roll back metadata, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestProjectStorageBucketDeleteCleansMetadataWhenDataPlaneBucketAlreadyMissing(t *testing.T) {
	t.Setenv("SUPADUPA_STORAGE_APPLY", "true")
	storageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/storage/v1/bucket":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"assets"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/storage/v1/bucket/assets":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"statusCode":"404","error":"Bucket not found","message":"Bucket not found"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer storageServer.Close()
	t.Setenv("SUPADUPA_STORAGE_APPLY_BASE_URL", storageServer.URL)

	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"storage-missing","name":"Storage Missing","domain":"apps.example.test"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/storage-missing/storage/buckets", `{"name":"assets","public":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected storage bucket create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/storage-missing/storage/buckets/assets", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected storage bucket delete 204 despite missing data-plane bucket, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	listResponse := perform(server, http.MethodGet, "/v1/projects/storage-missing/storage/buckets", "")
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"name":"assets"`) {
		t.Fatalf("expected metadata cleanup after missing data-plane bucket, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestStorageDataPlaneNotFoundAcceptsSupabaseStorageShape(t *testing.T) {
	if !storageDataPlaneNotFound(http.StatusNotFound, []byte(`not found`)) {
		t.Fatalf("expected HTTP 404 to be treated as not found")
	}
	if !storageDataPlaneNotFound(http.StatusBadRequest, []byte(`{"statusCode":"404","error":"Bucket not found","message":"Bucket not found"}`)) {
		t.Fatalf("expected Supabase Storage bucket-not-found body to be treated as not found")
	}
	if storageDataPlaneNotFound(http.StatusBadRequest, []byte(`{"statusCode":"400","message":"bad request"}`)) {
		t.Fatalf("expected generic bad request to remain an error")
	}
}
