package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"supadupa2026/internal/control"
)

func TestCreateProjectWithOrioleDBProfile(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"oriole-proj","name":"Oriole","domain":"supadupa.test","profile":"orioledb","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected orioledb project create 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if !strings.Contains(projectResponse.Body.String(), `"profile":"orioledb"`) {
		t.Fatalf("expected orioledb profile in project response: %s", projectResponse.Body.String())
	}
	configResponse := perform(server, http.MethodGet, "/v1/projects/oriole-proj/config/database", "")
	if configResponse.Code != http.StatusOK || !strings.Contains(configResponse.Body.String(), `"orioledb_profile":"preview"`) {
		t.Fatalf("expected orioledb database config: %d %s", configResponse.Code, configResponse.Body.String())
	}
}

func TestProjectBranchesCreateListRoutesAndCleanup(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "preview_branches", "custom_domains")
	projectBody := `{"ref":"branch-source","name":"Branch Source","domain":"supadupa.test","profile":"full","resource_tier":"small","services":{"storage":true},"environment":{"CUSTOM":"value"}}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	otherProjectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"branch-other","name":"Branch Other","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if otherProjectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected other project status 202, got %d: %s", otherProjectResponse.Code, otherProjectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/branch-source/branches", `{"ref":"Bad Ref","name":"Bad"}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid branch ref 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/branch-source/branches", `{"ref":"branch-preview","name":"Preview","ttl_hours":24}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected branch create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	for _, expected := range []string{
		`"source_project_ref":"branch-source"`,
		`"project_ref":"branch-preview"`,
		`"with_data":false`,
		`"status":"healthy"`,
		`"ref":"branch-preview"`,
		`"name":"Preview"`,
	} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected %s in branch create response: %s", expected, createResponse.Body.String())
		}
	}
	if strings.Contains(createResponse.Body.String(), "SUPADUPA_BRANCH_SOURCE_REF") || strings.Contains(createResponse.Body.String(), `"CUSTOM":"value"`) {
		t.Fatalf("branch create response leaked internal environment: %s", createResponse.Body.String())
	}

	listResponse := perform(server, http.MethodGet, "/v1/projects/branch-source/branches", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"project_ref":"branch-preview"`) {
		t.Fatalf("expected branch in list: %d %s", listResponse.Code, listResponse.Body.String())
	}

	connectResponse := perform(server, http.MethodGet, "/v1/projects/branch-preview/connect", "")
	if connectResponse.Code != http.StatusOK || !strings.Contains(connectResponse.Body.String(), "https://branch-preview.supadupa.test") {
		t.Fatalf("expected branch connect payload: %d %s", connectResponse.Code, connectResponse.Body.String())
	}
	routesResponse := perform(server, http.MethodGet, "/v1/projects/branch-preview/routes", "")
	if routesResponse.Code != http.StatusOK || !strings.Contains(routesResponse.Body.String(), `"fqdn":"branch-preview.supadupa.test"`) {
		t.Fatalf("expected branch routes: %d %s", routesResponse.Code, routesResponse.Body.String())
	}
	for _, reserved := range []string{
		"branch-preview.supadupa.test",
		"studio-branch-preview.supadupa.test",
		"storage-branch-preview.supadupa.test",
		"db-branch-preview.supadupa.test",
		"pooler-branch-preview.supadupa.test",
	} {
		reservedResponse := perform(server, http.MethodPost, "/v1/projects/branch-other/domains", fmt.Sprintf(`{"fqdn":%q}`, reserved))
		if reservedResponse.Code != http.StatusConflict {
			t.Fatalf("expected branch generated domain %s conflict, got %d: %s", reserved, reservedResponse.Code, reservedResponse.Body.String())
		}
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.branch_create"`) {
		t.Fatalf("expected branch create audit event: %s", auditResponse.Body.String())
	}
	if strings.Contains(auditResponse.Body.String(), `"clone_state"`) || !strings.Contains(auditResponse.Body.String(), `"with_data":"false"`) {
		t.Fatalf("expected data-less branch audit metadata without clone state: %s", auditResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/branch-source/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Branch created") {
		t.Fatalf("expected source branch log: %s", logsResponse.Body.String())
	}

	deleteMissingResponse := perform(server, http.MethodDelete, "/v1/projects/branch-source/branches/missing-preview", "")
	if deleteMissingResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing branch delete 404, got %d: %s", deleteMissingResponse.Code, deleteMissingResponse.Body.String())
	}
	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/branch-source/branches/branch-preview", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected branch project delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	listResponse = perform(server, http.MethodGet, "/v1/projects/branch-source/branches", "")
	if strings.Contains(listResponse.Body.String(), `"project_ref":"branch-preview"`) {
		t.Fatalf("expected branch metadata removed after branch delete: %s", listResponse.Body.String())
	}
	getBranchResponse := perform(server, http.MethodGet, "/v1/projects/branch-preview", "")
	if getBranchResponse.Code != http.StatusNotFound {
		t.Fatalf("expected branch project removed after branch delete, got %d: %s", getBranchResponse.Code, getBranchResponse.Body.String())
	}
	auditResponse = perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.branch_delete"`) {
		t.Fatalf("expected branch delete audit event: %s", auditResponse.Body.String())
	}
}

func TestProjectBranchCreatePassesGeneratedSecretsToProvisioner(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "preview_branches")
	projectBody := `{"ref":"branch-source","name":"Branch Source","domain":"supadupa.test","profile":"full","resource_tier":"small","environment":{"CUSTOM":"source-value","POSTGRES_PASSWORD":"source-should-not-win"}}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/branch-source/branches", `{"ref":"branch-secret-preview","name":"Preview","ttl_hours":24,"with_data":true}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected branch create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	if strings.Contains(createResponse.Body.String(), "source-value") || strings.Contains(createResponse.Body.String(), "source-should-not-win") {
		t.Fatalf("branch response leaked internal environment: %s", createResponse.Body.String())
	}
	if provisioner.spec.Ref != "branch-secret-preview" {
		t.Fatalf("expected branch provisioner create call, got %#v", provisioner.spec)
	}
	if provisioner.spec.Environment["CUSTOM"] != "source-value" || provisioner.spec.Environment["SUPADUPA_BRANCH_SOURCE_REF"] != "branch-source" {
		t.Fatalf("expected branch to inherit source environment markers, got %#v", provisioner.spec.Environment)
	}
	for key, prefix := range map[string]string{
		"JWT_SECRET":                       "jwt_",
		"SUPADUPA_JWT_SIGNING_KEY_CURRENT": "{",
		"SUPADUPA_JWT_SIGNING_KEY_NEXT":    "{",
		"SUPABASE_PUBLISHABLE_KEY":         "pub_",
		"SUPABASE_SECRET_KEY":              "sec_",
		"S3_ACCESS_KEY":                    "s3ak_",
		"S3_SECRET_KEY":                    "s3sk_",
	} {
		value := provisioner.spec.Environment[key]
		if !strings.HasPrefix(value, prefix) {
			t.Fatalf("expected branch provisioner env %s to have prefix %q, got %q in %#v", key, prefix, value, provisioner.spec.Environment)
		}
	}
	if value := provisioner.spec.Environment["POSTGRES_PASSWORD"]; len(value) != 48 || !isLowerHexForTest(value) {
		t.Fatalf("expected branch provisioner env POSTGRES_PASSWORD to be 48 lowercase hex chars, got %q in %#v", value, provisioner.spec.Environment)
	}
	for _, key := range []string{"ANON_KEY", "SERVICE_ROLE_KEY"} {
		if strings.Count(provisioner.spec.Environment[key], ".") != 2 {
			t.Fatalf("expected branch provisioner env %s to be a JWT, got %q in %#v", key, provisioner.spec.Environment[key], provisioner.spec.Environment)
		}
	}
	if provisioner.spec.Environment["POSTGRES_PASSWORD"] == "source-should-not-win" {
		t.Fatalf("source db password won over branch managed secret")
	}
	if provisioner.clonedBranch.SourceRef != "branch-source" || provisioner.clonedBranch.BranchRef != "branch-secret-preview" || provisioner.clonedBranch.BranchID == "" || !provisioner.clonedBranch.WithData {
		t.Fatalf("expected branch clone call, got %#v", provisioner.clonedBranch)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"clone_state":"dry-run"`) || !strings.Contains(auditResponse.Body.String(), `"clone_path":"branch-clone.sql"`) || !strings.Contains(auditResponse.Body.String(), `"with_data":"true"`) {
		t.Fatalf("expected branch clone metadata in audit: %s", auditResponse.Body.String())
	}
}

func TestProjectReplicasCreateListUsageAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host create 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	otherOrgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Replica Domain Peer"}`)
	otherOrgID := extractString(t, otherOrgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "read_replicas", "custom_domains")
	enableOrgFeaturesForTest(t, store, otherOrgID, "custom_domains")
	projectBody := `{"ref":"replica-proj","name":"Replica","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	otherProjectResponse := perform(server, http.MethodPost, "/v1/orgs/"+otherOrgID+"/projects", `{"ref":"replica-domain-other","name":"Replica Domain Other","host_id":"`+hostID+`","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if otherProjectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected other replica domain project status 202, got %d: %s", otherProjectResponse.Code, otherProjectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"bad name","tier":"small"}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid replica name 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	invalidDNSLabelResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"east-","tier":"small"}`)
	if invalidDNSLabelResponse.Code != http.StatusBadRequest || !strings.Contains(invalidDNSLabelResponse.Body.String(), "cannot start or end with a dash") {
		t.Fatalf("expected invalid replica DNS label 400, got %d: %s", invalidDNSLabelResponse.Code, invalidDNSLabelResponse.Body.String())
	}
	tooLongPublicHostResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","tier":"small"}`)
	if tooLongPublicHostResponse.Code != http.StatusBadRequest || !strings.Contains(tooLongPublicHostResponse.Body.String(), "63-character DNS label limit") {
		t.Fatalf("expected replica public DNS label 400, got %d: %s", tooLongPublicHostResponse.Code, tooLongPublicHostResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"east","host_id":"`+hostID+`","region":"us-east","tier":"small","read_weight":75,"failover_priority":2}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected replica create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	eastID := extractString(t, createResponse.Body.String(), "id")
	for _, expected := range []string{
		`"project_ref":"replica-proj"`,
		`"name":"east"`,
		`"region":"us-east"`,
		`"tier":"small"`,
		`"status":"healthy"`,
		`"role":"read"`,
		`"read_weight":75`,
		`"failover_priority":2`,
		`"read_uri":"postgres://postgres:${DB_PASSWORD}@db-replica-east-replica-proj.supadupa.test:5432/postgres?sslmode=require"`,
		`"public_read_uri":"postgres://postgres:${DB_PASSWORD}@db-replica-east-replica-proj.supadupa.test:5432/postgres?sslmode=require"`,
		`"internal_read_uri":"postgres://postgres:${DB_PASSWORD}@east.replica-proj.replica.internal:5432/postgres"`,
	} {
		if !strings.Contains(createResponse.Body.String(), expected) {
			t.Fatalf("expected %s in replica create response: %s", expected, createResponse.Body.String())
		}
	}
	enableDatabaseExposure(t, server, "replica-proj", "public", "")
	manifestResponse := perform(server, http.MethodGet, "/v1/projects/replica-proj/route-manifest", "")
	for _, expected := range []string{
		`"name":"db-replica-east"`,
		`"fqdn":"db-replica-east-replica-proj.supadupa.test"`,
		`"entrypoint":"postgres"`,
		`"upstream_address":"replica-proj-db-replica-east:5432"`,
	} {
		if !strings.Contains(manifestResponse.Body.String(), expected) {
			t.Fatalf("expected route manifest value %s: %s", expected, manifestResponse.Body.String())
		}
	}
	replicaReservedResponse := perform(server, http.MethodPost, "/v1/projects/replica-domain-other/domains", `{"fqdn":"db-replica-east-replica-proj.supadupa.test"}`)
	if replicaReservedResponse.Code != http.StatusConflict {
		t.Fatalf("expected replica generated domain conflict, got %d: %s", replicaReservedResponse.Code, replicaReservedResponse.Body.String())
	}
	westResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas", `{"name":"west","host_id":"`+hostID+`","region":"us-west","tier":"small","read_weight":125,"failover_priority":1}`)
	if westResponse.Code != http.StatusCreated {
		t.Fatalf("expected west replica create 201, got %d: %s", westResponse.Code, westResponse.Body.String())
	}
	westID := extractString(t, westResponse.Body.String(), "id")

	listResponse := perform(server, http.MethodGet, "/v1/projects/replica-proj/replicas", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"name":"east"`) || !strings.Contains(listResponse.Body.String(), `"name":"west"`) {
		t.Fatalf("expected replica in list: %d %s", listResponse.Code, listResponse.Body.String())
	}
	routingResponse := perform(server, http.MethodGet, "/v1/projects/replica-proj/replicas/routing", "")
	for _, expected := range []string{`"read_strategy":"weighted-healthy"`, `"auto_failover":true`, `"name":"west"`, `"weight":125`, `"failover_priority":1`} {
		if !strings.Contains(routingResponse.Body.String(), expected) {
			t.Fatalf("expected routing value %s: %s", expected, routingResponse.Body.String())
		}
	}
	promoteResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas/"+eastID+"/promote", `{"reason":"planned maintenance"}`)
	if promoteResponse.Code != http.StatusOK || !strings.Contains(promoteResponse.Body.String(), `"role":"primary"`) || !strings.Contains(promoteResponse.Body.String(), `"message":"planned maintenance"`) {
		t.Fatalf("expected promoted east replica: %d %s", promoteResponse.Code, promoteResponse.Body.String())
	}
	failoverResponse := perform(server, http.MethodPost, "/v1/projects/replica-proj/replicas/failover", `{"reason":"primary degraded"}`)
	if failoverResponse.Code != http.StatusOK || !strings.Contains(failoverResponse.Body.String(), `"id":"`+westID+`"`) || !strings.Contains(failoverResponse.Body.String(), `"role":"primary"`) {
		t.Fatalf("expected automatic failover to west replica: %d %s", failoverResponse.Code, failoverResponse.Body.String())
	}
	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	for _, expected := range []string{`"read_replicas":2`, `"cpu":6`, `"ram_mb":12288`, `"disk_gb":120`, `"projects":1`} {
		if !strings.Contains(usageResponse.Body.String(), expected) {
			t.Fatalf("expected usage value %s: %s", expected, usageResponse.Body.String())
		}
	}
	metricsResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if !strings.Contains(metricsResponse.Body.String(), `"read_replicas":2`) {
		t.Fatalf("expected fleet replica metric: %s", metricsResponse.Body.String())
	}
	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if !strings.Contains(prometheusResponse.Body.String(), "supadupa_read_replicas_total 2") {
		t.Fatalf("expected prometheus replica metric: %s", prometheusResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/replica-proj/logs", "")
	for _, expected := range []string{"Read replica provisioned", "Read replica promoted", "Read replica failover completed"} {
		if !strings.Contains(logsResponse.Body.String(), expected) {
			t.Fatalf("expected replica project log %s: %s", expected, logsResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{`"action":"project.replica_create"`, `"action":"project.replica_promote"`, `"action":"project.replica_failover"`} {
		if !strings.Contains(auditResponse.Body.String(), action) {
			t.Fatalf("expected replica audit action %s: %s", action, auditResponse.Body.String())
		}
	}
	routingResponse = perform(server, http.MethodGet, "/v1/projects/replica-proj/replicas/routing", "")
	if !strings.Contains(routingResponse.Body.String(), `"primary_replica_id":"`+westID+`"`) {
		t.Fatalf("expected west as routing primary: %s", routingResponse.Body.String())
	}
	if strings.Contains(routingResponse.Body.String(), `"replica_id":"`+westID+`","name":"west","uri"`) && strings.Contains(routingResponse.Body.String(), `"healthy_read_targets":[{"replica_id":"`+westID) {
		t.Fatalf("promoted primary should not remain in healthy read targets: %s", routingResponse.Body.String())
	}
	if !strings.Contains(routingResponse.Body.String(), `"replica_id":"`+eastID+`"`) {
		t.Fatalf("expected east read target after failover: %s", routingResponse.Body.String())
	}
	deletePrimaryResponse := perform(server, http.MethodDelete, "/v1/projects/replica-proj/replicas/"+westID, "")
	if deletePrimaryResponse.Code != http.StatusConflict {
		t.Fatalf("expected deleting promoted primary to be rejected, got %d: %s", deletePrimaryResponse.Code, deletePrimaryResponse.Body.String())
	}
	deleteReadResponse := perform(server, http.MethodDelete, "/v1/projects/replica-proj/replicas/"+eastID, "")
	if deleteReadResponse.Code != http.StatusNoContent {
		t.Fatalf("expected read replica delete 204, got %d: %s", deleteReadResponse.Code, deleteReadResponse.Body.String())
	}
	listResponse = perform(server, http.MethodGet, "/v1/projects/replica-proj/replicas", "")
	if strings.Contains(listResponse.Body.String(), `"id":"`+eastID+`"`) || !strings.Contains(listResponse.Body.String(), `"id":"`+westID+`"`) {
		t.Fatalf("expected only promoted west replica after delete: %d %s", listResponse.Code, listResponse.Body.String())
	}
	usageResponse = perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if !strings.Contains(usageResponse.Body.String(), `"read_replicas":1`) || !strings.Contains(usageResponse.Body.String(), `"cpu":4`) {
		t.Fatalf("expected usage to reflect deleted read replica: %s", usageResponse.Body.String())
	}
	auditResponse = perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.replica_delete"`) {
		t.Fatalf("expected replica delete audit action: %s", auditResponse.Body.String())
	}
	if logsResponse.Code != http.StatusOK {
		t.Fatalf("expected replica project log: %s", logsResponse.Body.String())
	}
}

func TestProjectScaleUpdatesResourceCapacityLimitsAndAudit(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host create 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"scale-proj","name":"Scale","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	invalidResponse := perform(server, http.MethodPost, "/v1/projects/scale-proj/scale", `{"cpu":0,"ram_mb":64,"disk_gb":0}`)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid scale resources 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	scaleResponse := perform(server, http.MethodPost, "/v1/projects/scale-proj/scale", `{"cpu":6,"ram_mb":12288,"disk_gb":120,"enforce_limits":true}`)
	if scaleResponse.Code != http.StatusOK {
		t.Fatalf("expected scale status 200, got %d: %s", scaleResponse.Code, scaleResponse.Body.String())
	}
	if !strings.Contains(scaleResponse.Body.String(), `"resource_tier":"custom"`) || !strings.Contains(scaleResponse.Body.String(), `"cpu":6`) || !strings.Contains(scaleResponse.Body.String(), `"enforce_limits":true`) || !strings.Contains(scaleResponse.Body.String(), `"message":"resource sizing updated"`) {
		t.Fatalf("expected exact resource scaled project response: %s", scaleResponse.Body.String())
	}

	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	for _, expected := range []string{`"cpu":6`, `"ram_mb":12288`, `"disk_gb":120`, `"projects":1`} {
		if !strings.Contains(usageResponse.Body.String(), expected) {
			t.Fatalf("expected scaled usage %s: %s", expected, usageResponse.Body.String())
		}
	}
	hostsResponse := perform(server, http.MethodGet, "/v1/hosts", "")
	for _, expected := range []string{`"used":{"cpu":6`, `"ram_mb":12288`, `"disk_gb":120`, `"projects":1`} {
		if !strings.Contains(hostsResponse.Body.String(), expected) {
			t.Fatalf("expected scaled host usage %s: %s", expected, hostsResponse.Body.String())
		}
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/scale-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Resource sizing updated") {
		t.Fatalf("expected scale project log: %s", logsResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.scale"`) {
		t.Fatalf("expected scale audit event: %s", auditResponse.Body.String())
	}
}

func TestCreateHostAndPlaceProject(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	getHostResponse := perform(server, http.MethodGet, "/v1/hosts/"+hostID, "")
	if getHostResponse.Code != http.StatusOK {
		t.Fatalf("expected get host status 200, got %d: %s", getHostResponse.Code, getHostResponse.Body.String())
	}

	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"hosted-proj","name":"Hosted","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	if !strings.Contains(projectResponse.Body.String(), `"host_id":"`+hostID+`"`) {
		t.Fatalf("expected project host id in response: %s", projectResponse.Body.String())
	}

	hostsResponse := perform(server, http.MethodGet, "/v1/hosts", "")
	for _, expected := range []string{`"cpu":2`, `"ram_mb":4096`, `"disk_gb":40`, `"projects":1`} {
		if !strings.Contains(hostsResponse.Body.String(), expected) {
			t.Fatalf("expected host usage %s: %s", expected, hostsResponse.Body.String())
		}
	}

	conflictResponse := perform(server, http.MethodDelete, "/v1/hosts/"+hostID, "")
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("expected host delete conflict, got %d: %s", conflictResponse.Code, conflictResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/hosted-proj", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected delete 204, got %d", deleteResponse.Code)
	}
	hostsResponse = perform(server, http.MethodGet, "/v1/hosts", "")
	for _, expected := range []string{`"cpu":0`, `"ram_mb":0`, `"disk_gb":0`, `"projects":0`} {
		if !strings.Contains(hostsResponse.Body.String(), expected) {
			t.Fatalf("expected host usage decrement %s: %s", expected, hostsResponse.Body.String())
		}
	}
	deleteHostResponse := perform(server, http.MethodDelete, "/v1/hosts/"+hostID, "")
	if deleteHostResponse.Code != http.StatusNoContent {
		t.Fatalf("expected host delete 204, got %d: %s", deleteHostResponse.Code, deleteHostResponse.Body.String())
	}
	missingHostResponse := perform(server, http.MethodGet, "/v1/hosts/"+hostID, "")
	if missingHostResponse.Code != http.StatusNotFound {
		t.Fatalf("expected deleted host 404, got %d: %s", missingHostResponse.Code, missingHostResponse.Body.String())
	}
}

func TestProjectPlacementRejectsInsufficientHostCapacity(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"tiny","address":"localhost","capacity":{"cpu":2,"ram_mb":4096,"disk_gb":40,"projects":1}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host status 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")

	firstProject := `{"ref":"tiny-one","name":"Tiny One","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	firstResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", firstProject)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("expected first project status 202, got %d: %s", firstResponse.Code, firstResponse.Body.String())
	}

	secondProject := `{"ref":"tiny-two","name":"Tiny Two","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	secondResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", secondProject)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("expected second project capacity conflict, got %d: %s", secondResponse.Code, secondResponse.Body.String())
	}

	// A host with ample CPU/RAM/slots but too little disk must still reject a
	// small-tier project (which reserves 40 GiB), proving placement enforces a
	// per-dimension capacity check beyond the project-slot count.
	diskHostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"disk-tight","address":"127.0.0.2","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":10,"projects":10}}`)
	if diskHostResponse.Code != http.StatusCreated {
		t.Fatalf("expected disk host status 201, got %d: %s", diskHostResponse.Code, diskHostResponse.Body.String())
	}
	diskHostID := extractString(t, diskHostResponse.Body.String(), "id")
	diskProject := `{"ref":"disk-one","name":"Disk One","host_id":"` + diskHostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	diskResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", diskProject)
	if diskResponse.Code != http.StatusConflict {
		t.Fatalf("expected disk capacity conflict, got %d: %s", diskResponse.Code, diskResponse.Body.String())
	}
}

func TestProjectReplicasSyncRuntimeWhenProvisionerSupportsReplicaSyncer(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	hostResponse := perform(server, http.MethodPost, "/v1/hosts", `{"name":"local","address":"localhost","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10}}`)
	if hostResponse.Code != http.StatusCreated {
		t.Fatalf("expected host create 201, got %d: %s", hostResponse.Code, hostResponse.Body.String())
	}
	hostID := extractString(t, hostResponse.Body.String(), "id")
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "read_replicas")
	projectBody := `{"ref":"replica-sync","name":"Replica Sync","host_id":"` + hostID + `","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	createResponse := perform(server, http.MethodPost, "/v1/projects/replica-sync/replicas", `{"name":"east","host_id":"`+hostID+`","region":"us-east","tier":"small","read_weight":75,"failover_priority":2}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected replica create 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	eastID := extractString(t, createResponse.Body.String(), "id")
	if provisioner.syncedReplicasRef != "replica-sync" || len(provisioner.syncedReplicas) != 1 || provisioner.syncedReplicas[0].Name != "east" {
		t.Fatalf("expected replica create to sync runtime replicas, got ref=%s replicas=%#v", provisioner.syncedReplicasRef, provisioner.syncedReplicas)
	}

	promoteResponse := perform(server, http.MethodPost, "/v1/projects/replica-sync/replicas/"+eastID+"/promote", `{"reason":"planned"}`)
	if promoteResponse.Code != http.StatusOK {
		t.Fatalf("expected replica promote 200, got %d: %s", promoteResponse.Code, promoteResponse.Body.String())
	}
	if len(provisioner.syncedReplicas) != 1 || provisioner.syncedReplicas[0].Role != "primary" {
		t.Fatalf("expected promote to sync primary role, got %#v", provisioner.syncedReplicas)
	}

	westResponse := perform(server, http.MethodPost, "/v1/projects/replica-sync/replicas", `{"name":"west","host_id":"`+hostID+`","region":"us-west","tier":"small","read_weight":50,"failover_priority":3}`)
	if westResponse.Code != http.StatusCreated {
		t.Fatalf("expected west replica create 201, got %d: %s", westResponse.Code, westResponse.Body.String())
	}
	westID := extractString(t, westResponse.Body.String(), "id")
	if len(provisioner.syncedReplicas) != 2 {
		t.Fatalf("expected west create to sync two replicas, got %#v", provisioner.syncedReplicas)
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/replica-sync/replicas/"+westID, "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected replica delete 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if provisioner.syncedReplicasRef != "replica-sync" || len(provisioner.syncedReplicas) != 1 || provisioner.syncedReplicas[0].ID != eastID {
		t.Fatalf("expected delete to sync remaining replicas before metadata removal, got ref=%s replicas=%#v", provisioner.syncedReplicasRef, provisioner.syncedReplicas)
	}
}

func TestProjectHostMustExist(t *testing.T) {
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"missing-host","name":"Missing","host_id":"missing","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing host 404, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
}

func TestProjectLifecycleActions(t *testing.T) {
	routesRoot := t.TempDir()
	certRoot := t.TempDir()
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", routesRoot)
	t.Setenv("SUPADUPA_CERT_ROOT", certRoot)
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "custom_domains")
	projectBody := `{"ref":"life-proj","name":"Life","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	routePath := filepath.Join(routesRoot, "life-proj.yaml")
	if _, err := os.Stat(routePath); err != nil {
		t.Fatalf("expected project route artifact: %v", err)
	}
	domainResponse := perform(server, http.MethodPost, "/v1/projects/life-proj/domains", `{"fqdn":"life.example.com"}`)
	if domainResponse.Code != http.StatusCreated {
		t.Fatalf("expected domain status 201, got %d: %s", domainResponse.Code, domainResponse.Body.String())
	}
	certPath := filepath.Join(certRoot, "life-proj", "life.example.com.json")
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("expected project certificate artifact: %v", err)
	}

	pauseResponse := perform(server, http.MethodPost, "/v1/projects/life-proj/pause", "")
	if pauseResponse.Code != http.StatusOK || !strings.Contains(pauseResponse.Body.String(), `"status":"paused"`) {
		t.Fatalf("expected paused project: %d %s", pauseResponse.Code, pauseResponse.Body.String())
	}

	resumeResponse := perform(server, http.MethodPost, "/v1/projects/life-proj/resume", "")
	if resumeResponse.Code != http.StatusOK || !strings.Contains(resumeResponse.Body.String(), `"status":"healthy"`) {
		t.Fatalf("expected resumed project: %d %s", resumeResponse.Code, resumeResponse.Body.String())
	}

	restartResponse := perform(server, http.MethodPost, "/v1/projects/life-proj/restart", "")
	if restartResponse.Code != http.StatusOK || !strings.Contains(restartResponse.Body.String(), `"message":"restarted"`) {
		t.Fatalf("expected restarted project: %d %s", restartResponse.Code, restartResponse.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/life-proj/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusOK || !strings.Contains(upgradeResponse.Body.String(), `"stack_version":"15.8.1.085"`) {
		t.Fatalf("expected upgraded project: %d %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}

	logsResponse := perform(server, http.MethodGet, "/v1/projects/life-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Project restarted") || !strings.Contains(logsResponse.Body.String(), "Stack upgraded") {
		t.Fatalf("expected lifecycle logs: %s", logsResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/life-proj", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected delete status 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, err := os.Stat(routePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected route artifact removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(certRoot, "life-proj")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected certificate directory removed, got err=%v", err)
	}

	missingResponse := perform(server, http.MethodGet, "/v1/projects/life-proj", "")
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing project after delete, got %d: %s", missingResponse.Code, missingResponse.Body.String())
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.paused", "project.restart", "project.upgrade", "project.destroy"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectUpgradeCreatesPreUpgradeBackupAndReturnsRollbackMetadata(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-safe","name":"Upgrade Safe","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-safe/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusOK {
		t.Fatalf("expected upgrade status 200, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	for _, expected := range []string{
		`"previous_version":"15.8.1.060"`,
		`"target_version":"15.8.1.085"`,
		`"rollback_available":true`,
		`"backup":{`,
		`"status":"completed"`,
		`"stack_version":"15.8.1.085"`,
	} {
		if !strings.Contains(upgradeResponse.Body.String(), expected) {
			t.Fatalf("expected %s in upgrade response: %s", expected, upgradeResponse.Body.String())
		}
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085" {
		t.Fatalf("expected one provisioner upgrade to target version, got %#v", provisioner.upgradeVersions)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-safe")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].Status != "completed" {
		t.Fatalf("expected completed pre-upgrade backup, got %#v", backups)
	}
	if _, err := os.Stat(backups[0].Location); err != nil {
		t.Fatalf("expected backup artifact to exist: %v", err)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.upgrade_backup", "project.upgrade"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectUpgradeRejectsUnsupportedStackVersionBeforeBackup(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-reject","name":"Upgrade Reject","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-reject/upgrade", `{"version":"nightly"}`)
	if upgradeResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected upgrade status 400, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), "unsupported stack version") {
		t.Fatalf("expected unsupported version error: %s", upgradeResponse.Body.String())
	}
	if len(provisioner.upgradeVersions) != 0 {
		t.Fatalf("provisioner should not run for unsupported version, got %#v", provisioner.upgradeVersions)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-reject")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("backup should not run for unsupported version, got %#v", backups)
	}
}

func TestProjectUpgradeRejectsDowngradeBeforeBackup(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-downgrade","name":"Upgrade Downgrade","domain":"supadupa.test","stack_version":"15.8.1.085","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-downgrade/upgrade", `{"version":"15.8.1.060"}`)
	if upgradeResponse.Code != http.StatusBadRequest || !strings.Contains(upgradeResponse.Body.String(), "not newer") {
		t.Fatalf("expected downgrade rejection, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if len(provisioner.upgradeVersions) != 0 {
		t.Fatalf("provisioner should not run for downgrade, got %#v", provisioner.upgradeVersions)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-downgrade")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("backup should not run for downgrade, got %#v", backups)
	}
}

func TestProjectUpgradeUsesVerifiedBackupID(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-with-backup","name":"Upgrade With Backup","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}

	artifact := filepath.Join(backupRoot, "verified.sql")
	body := []byte("-- verified backup\n")
	if err := os.WriteFile(artifact, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	now := time.Now().UTC()
	backup, err := store.CreateBackup(context.Background(), control.BackupInput{
		ProjectRef:     "upgrade-with-backup",
		Kind:           "logical",
		Location:       artifact,
		SizeBytes:      int64(len(body)),
		ChecksumSHA256: hex.EncodeToString(sum[:]),
		Status:         "completed",
		VerifiedAt:     &now,
	})
	if err != nil {
		t.Fatal(err)
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-with-backup/upgrade", `{"version":"15.8.1.085","backup_id":"`+backup.ID+`"}`)
	if upgradeResponse.Code != http.StatusOK {
		t.Fatalf("expected upgrade status 200, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), `"id":"`+backup.ID+`"`) {
		t.Fatalf("expected supplied backup in response: %s", upgradeResponse.Body.String())
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085" {
		t.Fatalf("expected one provisioner upgrade to target version, got %#v", provisioner.upgradeVersions)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-with-backup")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected no extra pre-upgrade backup when backup_id is supplied, got %#v", backups)
	}
}

func TestProjectUpgradeRequiresDurableBackupWhenConfigured(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "true")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-durable-required","name":"Upgrade Durable Required","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-durable-required/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), "local-only") || !strings.Contains(upgradeResponse.Body.String(), "SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP") {
		t.Fatalf("expected durable backup requirement error: %s", upgradeResponse.Body.String())
	}
	if len(provisioner.upgradeVersions) != 0 {
		t.Fatalf("provisioner should not run without durable backup, got %#v", provisioner.upgradeVersions)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-durable-required")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].RemoteLocation != "" {
		t.Fatalf("expected one rejected local pre-upgrade backup artifact, got %#v", backups)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.upgrade_backup_failed"`) || !strings.Contains(auditResponse.Body.String(), `"durable_required":"true"`) {
		t.Fatalf("expected durable backup failure audit: %s", auditResponse.Body.String())
	}
}

func TestProjectUpgradeRejectsUntestedRemoteBackupWhenDurableRequired(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "true")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-untested-target","name":"Upgrade Untested Target","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}
	target, err := store.CreateBackupStorageTarget(context.Background(), control.BackupStorageTargetInput{
		Name:            "Off-host",
		Type:            "s3",
		Region:          "us-east-1",
		Bucket:          "supadupa-upgrade-backups",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	backup := createVerifiedUpgradeBackup(t, store, backupRoot, "upgrade-untested-target", target.ID, "s3://supadupa-upgrade-backups/projects/upgrade-untested-target/backups/verified.sql")

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-untested-target/upgrade", `{"version":"15.8.1.085","backup_id":"`+backup.ID+`"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), "validation-pending") {
		t.Fatalf("expected target validation-pending error: %s", upgradeResponse.Body.String())
	}
	if len(provisioner.upgradeVersions) != 0 {
		t.Fatalf("provisioner should not run without tested durable target, got %#v", provisioner.upgradeVersions)
	}
}

func TestProjectUpgradeAllowsTestedRemoteBackupWhenDurableRequired(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP", "true")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-durable-ok","name":"Upgrade Durable OK","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}
	target, err := store.CreateBackupStorageTarget(context.Background(), control.BackupStorageTargetInput{
		Name:            "Off-host",
		Type:            "s3",
		Region:          "us-east-1",
		Bucket:          "supadupa-upgrade-backups",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupStorageTargetTestResult(context.Background(), target.ID, time.Now().UTC(), "passed", "ok"); err != nil {
		t.Fatal(err)
	}
	backup := createVerifiedUpgradeBackup(t, store, backupRoot, "upgrade-durable-ok", target.ID, "s3://supadupa-upgrade-backups/projects/upgrade-durable-ok/backups/verified.sql")

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-durable-ok/upgrade", `{"version":"15.8.1.085","backup_id":"`+backup.ID+`"}`)
	if upgradeResponse.Code != http.StatusOK {
		t.Fatalf("expected upgrade status 200, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), `"id":"`+backup.ID+`"`) || !strings.Contains(upgradeResponse.Body.String(), `"remote_location":"s3://supadupa-upgrade-backups/projects/upgrade-durable-ok/backups/verified.sql"`) {
		t.Fatalf("expected durable supplied backup in response: %s", upgradeResponse.Body.String())
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085" {
		t.Fatalf("expected one provisioner upgrade to target version, got %#v", provisioner.upgradeVersions)
	}
}

func TestProjectUpgradeRejectsInvalidBackupIDArtifact(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-bad-backup","name":"Upgrade Bad Backup","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}

	artifact := filepath.Join(backupRoot, "corrupt.sql")
	if err := os.WriteFile(artifact, []byte("-- corrupt backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	backup, err := store.CreateBackup(context.Background(), control.BackupInput{
		ProjectRef:     "upgrade-bad-backup",
		Kind:           "logical",
		Location:       artifact,
		SizeBytes:      18,
		ChecksumSHA256: strings.Repeat("0", 64),
		Status:         "completed",
		VerifiedAt:     &now,
	})
	if err != nil {
		t.Fatal(err)
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-bad-backup/upgrade", `{"version":"15.8.1.085","backup_id":"`+backup.ID+`"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	if !strings.Contains(upgradeResponse.Body.String(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error: %s", upgradeResponse.Body.String())
	}
	if len(provisioner.upgradeVersions) != 0 {
		t.Fatalf("provisioner should not run for invalid backup, got %#v", provisioner.upgradeVersions)
	}
}

func TestProjectUpgradeFailureAttemptsRollbackToPreviousVersion(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{upgradeErr: errors.New("apply failed")}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-fail","name":"Upgrade Fail","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-fail/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	for _, expected := range []string{
		`"error":"apply failed"`,
		`"backup":{`,
		`"previous_version":"15.8.1.060"`,
		`"target_version":"15.8.1.085"`,
		`"rollback_available":true`,
		`"rollback_attempted":true`,
	} {
		if !strings.Contains(upgradeResponse.Body.String(), expected) {
			t.Fatalf("expected %s in failed upgrade response: %s", expected, upgradeResponse.Body.String())
		}
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085,15.8.1.060" {
		t.Fatalf("expected target upgrade then rollback to previous version, got %#v", provisioner.upgradeVersions)
	}
	project, err := store.GetProject(context.Background(), "upgrade-fail")
	if err != nil {
		t.Fatal(err)
	}
	if project.Spec.StackVersion != "15.8.1.060" {
		t.Fatalf("store version should remain previous after failed upgrade, got %q", project.Spec.StackVersion)
	}
	backups, err := store.ListBackups(context.Background(), "upgrade-fail")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].Status != "completed" {
		t.Fatalf("expected pre-upgrade backup retained after failure, got %#v", backups)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.upgrade_failed"`) || !strings.Contains(auditResponse.Body.String(), `"rollback":"attempted"`) {
		t.Fatalf("expected failed upgrade audit with rollback metadata: %s", auditResponse.Body.String())
	}
}

func TestProjectUpgradeFailureAutoRestoresPreUpgradeBackupWhenEnabled(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_LOGICAL_BACKUP_COMMAND", "printf 'backup for %s\\n' {{ref}}")
	t.Setenv("SUPADUPA_LOGICAL_RESTORE_COMMAND", "printf 'restored %s from %s\\n' {{ref}} {{backup_id}}")
	t.Setenv("SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE", "true")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{upgradeErr: errors.New("apply failed")}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-autorestore","name":"Upgrade Auto Restore","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-autorestore/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	for _, expected := range []string{
		`"error":"apply failed"`,
		`"rollback_attempted":true`,
		`"restore_attempted":true`,
		`"restore_state":"completed"`,
	} {
		if !strings.Contains(upgradeResponse.Body.String(), expected) {
			t.Fatalf("expected %s in failed upgrade response: %s", expected, upgradeResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.upgrade_failed"`) ||
		!strings.Contains(auditResponse.Body.String(), `"restore":"attempted"`) ||
		!strings.Contains(auditResponse.Body.String(), `"restore_state":"completed"`) {
		t.Fatalf("expected failed upgrade audit with restore metadata: %s", auditResponse.Body.String())
	}
}

func TestProjectUpgradeFailureAutoRestoreReportsDryRunAsRestoreError(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_LOGICAL_BACKUP_COMMAND", "printf 'backup for %s\\n' {{ref}}")
	t.Setenv("SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE", "true")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{upgradeErr: errors.New("apply failed")}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-autorestore-dry","name":"Upgrade Auto Restore Dry","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-autorestore-dry/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	for _, expected := range []string{
		`"restore_attempted":true`,
		`"restore_state":"dry-run"`,
		`"restore_error":"logical restore returned state \"dry-run\"`,
	} {
		if !strings.Contains(upgradeResponse.Body.String(), expected) {
			t.Fatalf("expected %s in failed upgrade response: %s", expected, upgradeResponse.Body.String())
		}
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"restore_state":"dry-run"`) ||
		!strings.Contains(auditResponse.Body.String(), `"restore_error":"logical restore returned state \"dry-run\"`) {
		t.Fatalf("expected failed upgrade audit with dry-run restore error metadata: %s", auditResponse.Body.String())
	}
}

func TestProjectUpgradeFailureReportsRollbackAttemptEvenWhenRollbackFails(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{
		upgradeErr:  errors.New("apply failed"),
		rollbackErr: errors.New("rollback failed"),
	}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-rollback-fail","name":"Upgrade Rollback Fail","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}

	upgradeResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-rollback-fail/upgrade", `{"version":"15.8.1.085"}`)
	if upgradeResponse.Code != http.StatusConflict {
		t.Fatalf("expected upgrade status 409, got %d: %s", upgradeResponse.Code, upgradeResponse.Body.String())
	}
	for _, expected := range []string{
		`"error":"apply failed"`,
		`"rollback_available":true`,
		`"rollback_attempted":true`,
		`"rollback_error":"rollback failed"`,
		`"previous_version":"15.8.1.060"`,
		`"target_version":"15.8.1.085"`,
	} {
		if !strings.Contains(upgradeResponse.Body.String(), expected) {
			t.Fatalf("expected %s in failed rollback response: %s", expected, upgradeResponse.Body.String())
		}
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085,15.8.1.060" {
		t.Fatalf("expected target upgrade then rollback attempt, got %#v", provisioner.upgradeVersions)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.upgrade_failed"`) || !strings.Contains(auditResponse.Body.String(), `"rollback_error":"rollback failed"`) {
		t.Fatalf("expected failed upgrade audit with rollback error: %s", auditResponse.Body.String())
	}
}

func TestProjectUpgradeCompatFailureInjectionRequiresHeader(t *testing.T) {
	backupRoot := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", backupRoot)
	t.Setenv("SUPADUPA_COMPAT_UPGRADE_FAILURE_TARGETS", "15.8.1.085")
	store := control.NewMemoryStore()
	provisioner := &capturingProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	if response := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"upgrade-inject","name":"Upgrade Inject","domain":"supadupa.test","stack_version":"15.8.1.060","profile":"full","resource_tier":"small"}`); response.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", response.Code, response.Body.String())
	}

	normalResponse := perform(server, http.MethodPost, "/v1/projects/upgrade-inject/upgrade", `{"version":"15.8.1.085"}`)
	if normalResponse.Code != http.StatusOK {
		t.Fatalf("expected normal upgrade status 200, got %d: %s", normalResponse.Code, normalResponse.Body.String())
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085" {
		t.Fatalf("expected normal upgrade to run once, got %#v", provisioner.upgradeVersions)
	}

	if _, err := store.UpdateProjectStackVersion(context.Background(), "upgrade-inject", "15.8.1.060"); err != nil {
		t.Fatal(err)
	}
	injectedResponse := performWithHeader(server, http.MethodPost, "/v1/projects/upgrade-inject/upgrade", `{"version":"15.8.1.085"}`, "X-Supadupa-Compat-Inject-Upgrade-Failure", "true")
	if injectedResponse.Code != http.StatusConflict {
		t.Fatalf("expected injected upgrade status 409, got %d: %s", injectedResponse.Code, injectedResponse.Body.String())
	}
	for _, expected := range []string{
		`"error":"compat upgrade failure injection for 15.8.1.085"`,
		`"backup":{`,
		`"previous_version":"15.8.1.060"`,
		`"target_version":"15.8.1.085"`,
		`"rollback_attempted":true`,
	} {
		if !strings.Contains(injectedResponse.Body.String(), expected) {
			t.Fatalf("expected %s in injected response: %s", expected, injectedResponse.Body.String())
		}
	}
	if strings.Join(provisioner.upgradeVersions, ",") != "15.8.1.085,15.8.1.060" {
		t.Fatalf("expected rollback after injected failure, got %#v", provisioner.upgradeVersions)
	}
}

func TestProjectDestroySurfacesRouteCleanupFailure(t *testing.T) {
	routesRoot := t.TempDir()
	certRoot := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", routesRoot)
	t.Setenv("SUPADUPA_CERT_ROOT", certRoot)
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"route-cleanup-proj","name":"Routes","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	badRouteRoot := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badRouteRoot, []byte("blocks cleanup"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUPADUPA_ROUTES_ROOT", badRouteRoot)

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/route-cleanup-proj", "")
	if deleteResponse.Code != http.StatusConflict {
		t.Fatalf("expected route cleanup conflict, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	projectResponse = perform(server, http.MethodGet, "/v1/projects/route-cleanup-proj", "")
	if projectResponse.Code != http.StatusOK {
		t.Fatalf("expected project metadata retained after route cleanup failure, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.route_cleanup_failed"`) {
		t.Fatalf("expected route cleanup failure audit event: %s", auditResponse.Body.String())
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/route-cleanup-proj/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Project route cleanup failed") {
		t.Fatalf("expected route cleanup failure project log: %s", logsResponse.Body.String())
	}
}

func TestProjectDestroyPassesRetainVolumesOption(t *testing.T) {
	store := control.NewMemoryStore()
	provisioner := &retainDestroyProvisioner{}
	server := NewServer(Config{Store: store, Provisioner: provisioner})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"retain-proj","name":"Retain","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	deleteResponse := perform(server, http.MethodDelete, "/v1/projects/retain-proj?retain_volumes=true", "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected destroy 204, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if provisioner.destroyedRef != "retain-proj" || !provisioner.destroyOpts.RetainVolumes {
		t.Fatalf("expected retain destroy options, ref=%q opts=%#v", provisioner.destroyedRef, provisioner.destroyOpts)
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"retain_volumes":"true"`) {
		t.Fatalf("expected retain_volumes audit metadata: %s", auditResponse.Body.String())
	}
}

func TestReconcilePlatformRestoreRuntimeRecreatesAndStopsProjects(t *testing.T) {
	ctx := context.Background()
	routesRoot := t.TempDir()
	certRoot := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", routesRoot)
	t.Setenv("SUPADUPA_CERT_ROOT", certRoot)
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := store.CreateProject(ctx, control.CreateProjectRequest{OrgID: org.ID, Ref: "restored-proj", Name: "Restored", Domain: "apps.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	provisioner := &restoreRuntimeProvisioner{}
	beforeProjects := []control.Project{
		restored,
		{
			Ref: "stale-proj",
			Spec: control.ProjectSpec{
				Ref:    "stale-proj",
				OrgID:  org.ID,
				Name:   "Stale",
				Domain: "apps.example.test",
			},
		},
	}
	staleRoutePath := filepath.Join(routesRoot, "stale-proj.yaml")
	if err := os.WriteFile(staleRoutePath, []byte("project_ref: stale-proj\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	staleCertDir := filepath.Join(certRoot, "stale-proj")
	if err := os.MkdirAll(staleCertDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleCertDir, "stale.example.test.json"), []byte(`{"state":"ready"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	summary := reconcilePlatformRestoreRuntime(ctx, store, provisioner, beforeProjects)

	if summary.Reconciled != 1 || summary.Destroyed != 1 || len(summary.Errors) != 0 {
		t.Fatalf("unexpected restore runtime summary: %#v", summary)
	}
	if len(provisioner.createdRefs) != 1 || provisioner.createdRefs[0] != "restored-proj" {
		t.Fatalf("expected restored project reconcile, got %#v", provisioner.createdRefs)
	}
	if provisioner.destroyedRef != "stale-proj" || !provisioner.destroyOpts.RetainVolumes {
		t.Fatalf("expected stale project retained destroy, ref=%q opts=%#v", provisioner.destroyedRef, provisioner.destroyOpts)
	}
	if _, err := os.Stat(staleRoutePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale route artifact removed, got err=%v", err)
	}
	if _, err := os.Stat(staleCertDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale certificate directory removed, got err=%v", err)
	}
	logs, err := store.ListProjectLogs(ctx, "restored-proj", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 || !strings.Contains(logs[0].Message, "Runtime reconciled after control-plane restore") {
		t.Fatalf("expected restored project reconcile log, got %#v", logs)
	}
	audit, err := store.ListAuditEvents(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	seenStaleDestroy := false
	for _, event := range audit {
		if event.Action == "project.restore_stale_destroyed" && event.Target == "project:stale-proj" && event.Metadata["retain_volumes"] == "true" {
			seenStaleDestroy = true
			break
		}
	}
	if !seenStaleDestroy {
		t.Fatalf("expected stale destroy audit event, got %#v", audit)
	}
}
