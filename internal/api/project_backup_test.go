package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"supadupa2026/internal/control"
)

func TestBackupStorageTargetsAPIAndProjectPolicy(t *testing.T) {
	t.Setenv("SUPADUPA_ALLOW_UNSAFE_BACKUP_ENDPOINTS", "true")
	store := control.NewMemoryStore()
	server := NewServer(Config{
		Store:        store,
		Provisioner:  fakeProvisioner{},
		AuthRequired: true,
	})

	unauthorized := perform(server, http.MethodGet, "/v1/backup-storage-targets", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized target list, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	bootstrap := perform(server, http.MethodPost, "/v1/auth/bootstrap", `{"email":"admin@example.com","password":"super-secure"}`)
	token := extractString(t, bootstrap.Body.String(), "token")
	var s3Mu sync.Mutex
	s3Objects := map[string][]byte{}
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read fake s3 body: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			s3Mu.Lock()
			s3Objects[r.URL.Path] = body
			s3Mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			s3Mu.Lock()
			body, ok := s3Objects[r.URL.Path]
			s3Mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(body)
		case http.MethodDelete:
			s3Mu.Lock()
			delete(s3Objects, r.URL.Path)
			s3Mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3Server.Close()
	targetBody := fmt.Sprintf(`{"name":"Primary S3","type":"s3","endpoint":%q,"region":"auto","bucket":"supadupa-backups","prefix":"control","access_key_id":"access","secret_access_key":"super-secret","force_path_style":true,"default":false}`, s3Server.URL)
	created := performWithToken(server, http.MethodPost, "/v1/backup-storage-targets", targetBody, token)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected target create 201, got %d: %s", created.Code, created.Body.String())
	}
	targetID := extractString(t, created.Body.String(), "id")
	if strings.Contains(created.Body.String(), "super-secret") || !strings.Contains(created.Body.String(), `"secret_configured":true`) {
		t.Fatalf("expected redacted target response, got %s", created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"durable_off_host":false`) || !strings.Contains(created.Body.String(), `"recovery_ready":false`) || !strings.Contains(created.Body.String(), `"readiness_status":"local-or-loopback"`) || !strings.Contains(created.Body.String(), `"warnings":[`) {
		t.Fatalf("expected local target readiness metadata, got %s", created.Body.String())
	}

	listed := performWithToken(server, http.MethodGet, "/v1/backup-storage-targets", "", token)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), targetID) || !strings.Contains(listed.Body.String(), `"readiness_status":"local-or-loopback"`) || strings.Contains(listed.Body.String(), "super-secret") {
		t.Fatalf("expected redacted target list, got %d: %s", listed.Code, listed.Body.String())
	}
	tested := performWithToken(server, http.MethodPost, "/v1/backup-storage-targets/"+targetID+"/test", "", token)
	if tested.Code != http.StatusOK || !strings.Contains(tested.Body.String(), `"last_test_status":"passed"`) || !strings.Contains(tested.Body.String(), `"recovery_ready":false`) || !strings.Contains(tested.Body.String(), `"readiness_status":"local-or-loopback"`) || strings.Contains(tested.Body.String(), "super-secret") {
		t.Fatalf("expected passed redacted target test, got %d: %s", tested.Code, tested.Body.String())
	}
	listedAfterTest := performWithToken(server, http.MethodGet, "/v1/backup-storage-targets", "", token)
	if listedAfterTest.Code != http.StatusOK || !strings.Contains(listedAfterTest.Body.String(), `"last_test_status":"passed"`) || strings.Contains(listedAfterTest.Body.String(), "super-secret") {
		t.Fatalf("expected listed target test status, got %d: %s", listedAfterTest.Code, listedAfterTest.Body.String())
	}
	platformBackup := performWithToken(server, http.MethodPost, "/v1/platform/backups", "", token)
	if platformBackup.Code != http.StatusCreated || !strings.Contains(platformBackup.Body.String(), `"kind":"control-plane"`) || strings.Contains(platformBackup.Body.String(), `"storage_target_id"`) || strings.Contains(platformBackup.Body.String(), `"remote_location"`) {
		t.Fatalf("expected platform backup response, got %d: %s", platformBackup.Code, platformBackup.Body.String())
	}
	if strings.Contains(platformBackup.Body.String(), "super-secret") {
		t.Fatalf("platform backup response leaked target secret: %s", platformBackup.Body.String())
	}
	platformBackups := performWithToken(server, http.MethodGet, "/v1/platform/backups", "", token)
	if platformBackups.Code != http.StatusOK || !strings.Contains(platformBackups.Body.String(), `"kind":"control-plane"`) {
		t.Fatalf("expected platform backups list, got %d: %s", platformBackups.Code, platformBackups.Body.String())
	}

	orgResponse := performWithToken(server, http.MethodPost, "/v1/orgs", `{"name":"Backup API"}`, token)
	if orgResponse.Code != http.StatusCreated {
		t.Fatalf("expected org create 201, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := performWithToken(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"backup-target-api","name":"Backup Target API","domain":"apps.example.test"}`, token)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project create 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	policy := performWithToken(server, http.MethodPut, "/v1/projects/backup-target-api/backups/policy", `{"enabled":true,"schedule":"hourly","kind":"logical","storage_target_id":"`+targetID+`"}`, token)
	if policy.Code != http.StatusOK || !strings.Contains(policy.Body.String(), `"storage_target_id":"`+targetID+`"`) {
		t.Fatalf("expected policy target, got %d: %s", policy.Code, policy.Body.String())
	}

	audit := performWithToken(server, http.MethodGet, "/v1/audit-events", "", token)
	if strings.Contains(audit.Body.String(), "super-secret") {
		t.Fatalf("audit log leaked target secret: %s", audit.Body.String())
	}
}

func TestProjectBackupsAndLogs(t *testing.T) {
	t.Setenv("SUPADUPA_BACKUP_DRY_RUN", "true")
	t.Setenv("SUPADUPA_RESTORE_DRY_RUN", "true")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectBody := `{"ref":"backup-proj","name":"Backup","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	backupResponse := perform(server, http.MethodPost, "/v1/projects/backup-proj/backups", "")
	if backupResponse.Code != http.StatusCreated {
		t.Fatalf("expected backup status 201, got %d: %s", backupResponse.Code, backupResponse.Body.String())
	}

	backupsResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/backups", "")
	if backupsResponse.Code != http.StatusOK || !strings.Contains(backupsResponse.Body.String(), `"kind":"logical"`) {
		t.Fatalf("expected logical backup in response: %d %s", backupsResponse.Code, backupsResponse.Body.String())
	}
	hostedBackupsResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/database/backups", "")
	if hostedBackupsResponse.Code != http.StatusOK || !strings.Contains(hostedBackupsResponse.Body.String(), `"kind":"logical"`) {
		t.Fatalf("expected hosted-shaped logical backup list response: %d %s", hostedBackupsResponse.Code, hostedBackupsResponse.Body.String())
	}
	backupID := extractString(t, backupResponse.Body.String(), "id")

	recoverabilityResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/recoverability", "")
	if recoverabilityResponse.Code != http.StatusOK {
		t.Fatalf("expected recoverability status 200, got %d: %s", recoverabilityResponse.Code, recoverabilityResponse.Body.String())
	}
	for _, expected := range []string{
		`"status":"local-backup-only"`,
		`"off_host_backup_configured":false`,
		`"off_host_backup_verified":false`,
		`"restore_to_time_available":false`,
		`"restore_to_time_unavailable":"physical base backup plus WAL replay is not configured"`,
	} {
		if !strings.Contains(recoverabilityResponse.Body.String(), expected) {
			t.Fatalf("expected recoverability response to include %s: %s", expected, recoverabilityResponse.Body.String())
		}
	}

	restoreResponse := perform(server, http.MethodPost, "/v1/projects/backup-proj/restore", `{"backup_id":"`+backupID+`","confirmation":"restore project backup-proj"}`)
	if restoreResponse.Code != http.StatusAccepted {
		t.Fatalf("expected restore status 202, got %d: %s", restoreResponse.Code, restoreResponse.Body.String())
	}
	if !strings.Contains(restoreResponse.Body.String(), `"restore_state":"dry-run"`) || !strings.Contains(restoreResponse.Body.String(), backupID) || !strings.Contains(restoreResponse.Body.String(), `.sql`) {
		t.Fatalf("expected dry-run restore response: %s", restoreResponse.Body.String())
	}

	missingRestoreResponse := perform(server, http.MethodPost, "/v1/projects/backup-proj/restore", `{"backup_id":"missing","confirmation":"restore project backup-proj"}`)
	if missingRestoreResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing restore backup 404, got %d: %s", missingRestoreResponse.Code, missingRestoreResponse.Body.String())
	}
	invalidPITRRestoreResponse := perform(server, http.MethodPost, "/v1/projects/backup-proj/database/backups/restore-pitr", `{"recovery_time_target_unix":"not-a-timestamp","confirmation":"restore pitr project backup-proj"}`)
	if invalidPITRRestoreResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid PITR restore timestamp 400, got %d: %s", invalidPITRRestoreResponse.Code, invalidPITRRestoreResponse.Body.String())
	}
	unavailablePITRRestoreResponse := perform(server, http.MethodPost, "/v1/projects/backup-proj/database/backups/restore-pitr", `{"recovery_time_target_unix":"1735689600","confirmation":"restore pitr project backup-proj"}`)
	if unavailablePITRRestoreResponse.Code != http.StatusConflict || !strings.Contains(unavailablePITRRestoreResponse.Body.String(), `"restore_to_time_available":false`) || !strings.Contains(unavailablePITRRestoreResponse.Body.String(), `"status":"local-backup-only"`) {
		t.Fatalf("expected unavailable PITR restore conflict with recoverability: %d %s", unavailablePITRRestoreResponse.Code, unavailablePITRRestoreResponse.Body.String())
	}

	logsResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/logs", "")
	if logsResponse.Code != http.StatusOK || !strings.Contains(logsResponse.Body.String(), "Logical backup completed") || !strings.Contains(logsResponse.Body.String(), "Restore dry-run") {
		t.Fatalf("expected backup log in response: %d %s", logsResponse.Code, logsResponse.Body.String())
	}
	streamResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/logs/stream?follow=false", "")
	if streamResponse.Code != http.StatusOK {
		t.Fatalf("expected log stream status 200, got %d: %s", streamResponse.Code, streamResponse.Body.String())
	}
	if contentType := streamResponse.Header().Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected event stream content type, got %q", contentType)
	}
	if !strings.Contains(streamResponse.Body.String(), "event: log") || !strings.Contains(streamResponse.Body.String(), `"message":"Logical backup completed"`) {
		t.Fatalf("expected backup log stream events: %s", streamResponse.Body.String())
	}
	streamAliasResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/logs?stream=true&follow=false", "")
	if streamAliasResponse.Code != http.StatusOK {
		t.Fatalf("expected /logs stream alias status 200, got %d: %s", streamAliasResponse.Code, streamAliasResponse.Body.String())
	}
	if contentType := streamAliasResponse.Header().Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected /logs stream alias content type, got %q", contentType)
	}
	if !strings.Contains(streamAliasResponse.Body.String(), "event: log") || !strings.Contains(streamAliasResponse.Body.String(), `"message":"Restore dry-run"`) {
		t.Fatalf("expected /logs stream alias events: %s", streamAliasResponse.Body.String())
	}
	acceptStreamRequest := httptest.NewRequest(http.MethodGet, "/v1/projects/backup-proj/logs?follow=false", nil)
	acceptStreamRequest.Header.Set("Accept", "text/event-stream")
	acceptStreamResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(acceptStreamResponse, acceptStreamRequest)
	if acceptStreamResponse.Code != http.StatusOK {
		t.Fatalf("expected Accept stream status 200, got %d: %s", acceptStreamResponse.Code, acceptStreamResponse.Body.String())
	}
	if contentType := acceptStreamResponse.Header().Get("Content-Type"); !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected Accept stream content type, got %q", contentType)
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.restore"`) {
		t.Fatalf("expected project restore audit event: %s", auditResponse.Body.String())
	}

	policyResponse := perform(server, http.MethodGet, "/v1/projects/backup-proj/backups/policy", "")
	if policyResponse.Code != http.StatusOK || !strings.Contains(policyResponse.Body.String(), `"schedule":"daily"`) {
		t.Fatalf("expected default daily backup policy: %d %s", policyResponse.Code, policyResponse.Body.String())
	}

	updatePolicyResponse := perform(server, http.MethodPut, "/v1/projects/backup-proj/backups/policy", `{"enabled":false,"schedule":"hourly","kind":"logical"}`)
	if updatePolicyResponse.Code != http.StatusOK || !strings.Contains(updatePolicyResponse.Body.String(), `"enabled":false`) {
		t.Fatalf("expected updated backup policy: %d %s", updatePolicyResponse.Code, updatePolicyResponse.Body.String())
	}
	if strings.Contains(updatePolicyResponse.Body.String(), `"next_run_at"`) {
		t.Fatalf("expected disabled backup policy to omit next_run_at: %s", updatePolicyResponse.Body.String())
	}
}

func TestProjectPhysicalBackupPolicyAndTrigger(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", root)
	t.Setenv("SUPADUPA_PHYSICAL_BACKUP_COMMAND", "printf 'physical backup for %s\\n' {{ref}}")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"physical-api","name":"Physical","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	policyResponse := perform(server, http.MethodPut, "/v1/projects/physical-api/backups/policy", `{"enabled":true,"schedule":"daily","kind":"physical"}`)
	if policyResponse.Code != http.StatusOK || !strings.Contains(policyResponse.Body.String(), `"kind":"physical"`) {
		t.Fatalf("expected physical backup policy: %d %s", policyResponse.Code, policyResponse.Body.String())
	}
	backupResponse := perform(server, http.MethodPost, "/v1/projects/physical-api/backups", "")
	if backupResponse.Code != http.StatusCreated || !strings.Contains(backupResponse.Body.String(), `"kind":"physical"`) || !strings.Contains(backupResponse.Body.String(), `physical.base`) {
		t.Fatalf("expected physical backup response: %d %s", backupResponse.Code, backupResponse.Body.String())
	}
	backupPath := extractString(t, backupResponse.Body.String(), "location")
	payload, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "physical backup for physical-api") {
		t.Fatalf("expected physical backup artifact body, got:\n%s", string(payload))
	}
	logsResponse := perform(server, http.MethodGet, "/v1/projects/physical-api/logs", "")
	if !strings.Contains(logsResponse.Body.String(), "Physical backup completed") {
		t.Fatalf("expected physical backup log: %s", logsResponse.Body.String())
	}
}

func TestProjectPITRPolicyAndWALArchives(t *testing.T) {
	t.Setenv("SUPADUPA_WAL_ARCHIVE_DRY_RUN", "true")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	enableOrgFeaturesForTest(t, store, orgID, "pitr")
	projectBody := `{"ref":"pitr-proj","name":"PITR","domain":"supadupa.test","profile":"full","resource_tier":"small"}`
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", projectBody)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}

	policyResponse := perform(server, http.MethodGet, "/v1/projects/pitr-proj/pitr/policy", "")
	if policyResponse.Code != http.StatusOK || !strings.Contains(policyResponse.Body.String(), `"enabled":false`) || !strings.Contains(policyResponse.Body.String(), `"retention_days":7`) {
		t.Fatalf("expected default disabled PITR policy: %d %s", policyResponse.Code, policyResponse.Body.String())
	}

	disabledArchiveResponse := perform(server, http.MethodPost, "/v1/projects/pitr-proj/pitr/wal", "")
	if disabledArchiveResponse.Code != http.StatusConflict {
		t.Fatalf("expected disabled PITR archive 409, got %d: %s", disabledArchiveResponse.Code, disabledArchiveResponse.Body.String())
	}

	invalidPolicyResponse := perform(server, http.MethodPut, "/v1/projects/pitr-proj/pitr/policy", `{"enabled":true,"archive_bucket":"","retention_days":7}`)
	if invalidPolicyResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected missing archive bucket 400, got %d: %s", invalidPolicyResponse.Code, invalidPolicyResponse.Body.String())
	}

	target, err := store.CreateBackupStorageTarget(context.Background(), control.BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupPolicy(context.Background(), "pitr-proj", control.BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	derivedPolicyResponse := perform(server, http.MethodPut, "/v1/projects/pitr-proj/pitr/policy", `{"enabled":true,"archive_bucket":"","retention_days":7}`)
	if derivedPolicyResponse.Code != http.StatusOK || !strings.Contains(derivedPolicyResponse.Body.String(), `"archive_bucket":"s3://backups/supadupa/projects/pitr-proj/wal"`) {
		t.Fatalf("expected derived PITR archive bucket from backup target: %d %s", derivedPolicyResponse.Code, derivedPolicyResponse.Body.String())
	}
	if err := store.DeleteBackupStorageTarget(context.Background(), target.ID); err != nil {
		t.Fatal(err)
	}

	updatePolicyResponse := perform(server, http.MethodPut, "/v1/projects/pitr-proj/pitr/policy", `{"enabled":true,"archive_bucket":"s3://archive/pitr-proj","retention_days":14}`)
	if updatePolicyResponse.Code != http.StatusOK || !strings.Contains(updatePolicyResponse.Body.String(), `"enabled":true`) || !strings.Contains(updatePolicyResponse.Body.String(), `"retention_days":14`) || !strings.Contains(updatePolicyResponse.Body.String(), `"archive_bucket":"s3://archive/pitr-proj"`) {
		t.Fatalf("expected enabled PITR policy: %d %s", updatePolicyResponse.Code, updatePolicyResponse.Body.String())
	}

	archiveResponse := perform(server, http.MethodPost, "/v1/projects/pitr-proj/pitr/wal", "")
	if archiveResponse.Code != http.StatusCreated || !strings.Contains(archiveResponse.Body.String(), `"status":"archived"`) || !strings.Contains(archiveResponse.Body.String(), `"segment":"`) || !strings.Contains(archiveResponse.Body.String(), `"verified_at":"`) || !strings.Contains(archiveResponse.Body.String(), `.wal`) || strings.Contains(archiveResponse.Body.String(), `.wal.json`) {
		t.Fatalf("expected archived WAL segment: %d %s", archiveResponse.Code, archiveResponse.Body.String())
	}
	archiveID := extractString(t, archiveResponse.Body.String(), "id")

	archivesResponse := perform(server, http.MethodGet, "/v1/projects/pitr-proj/pitr/wal", "")
	if archivesResponse.Code != http.StatusOK || !strings.Contains(archivesResponse.Body.String(), archiveID) {
		t.Fatalf("expected WAL archive in list: %d %s", archivesResponse.Code, archivesResponse.Body.String())
	}

	recoverabilityResponse := perform(server, http.MethodGet, "/v1/projects/pitr-proj/recoverability", "")
	if recoverabilityResponse.Code != http.StatusOK {
		t.Fatalf("expected recoverability status 200, got %d: %s", recoverabilityResponse.Code, recoverabilityResponse.Body.String())
	}
	for _, expected := range []string{
		`"pitr_enabled":true`,
		`"latest_wal_archive"`,
		`"wal_archive_off_host_verified":false`,
		`"restore_to_time_configured":false`,
		`"restore_to_time_available":false`,
		`"verified WAL archives exist only on local disk"`,
		`"no verified physical base backup is available for PITR restore"`,
	} {
		if !strings.Contains(recoverabilityResponse.Body.String(), expected) {
			t.Fatalf("expected recoverability response to include %s: %s", expected, recoverabilityResponse.Body.String())
		}
	}
	unavailablePITRRestoreResponse := perform(server, http.MethodPost, "/v1/projects/pitr-proj/database/backups/restore-pitr", `{"recovery_time_target_unix":"1735689600","confirmation":"restore pitr project pitr-proj"}`)
	if unavailablePITRRestoreResponse.Code != http.StatusConflict || !strings.Contains(unavailablePITRRestoreResponse.Body.String(), `"recoverability"`) || !strings.Contains(unavailablePITRRestoreResponse.Body.String(), `"pitr_enabled":true`) {
		t.Fatalf("expected PITR restore conflict with recoverability: %d %s", unavailablePITRRestoreResponse.Code, unavailablePITRRestoreResponse.Body.String())
	}

	policyResponse = perform(server, http.MethodGet, "/v1/projects/pitr-proj/pitr/policy", "")
	if policyResponse.Code != http.StatusOK || !strings.Contains(policyResponse.Body.String(), `"last_archive_at":"`) {
		t.Fatalf("expected PITR policy last archive timestamp: %d %s", policyResponse.Code, policyResponse.Body.String())
	}

	usageResponse := perform(server, http.MethodGet, "/v1/orgs/"+orgID+"/usage", "")
	if usageResponse.Code != http.StatusOK || !strings.Contains(usageResponse.Body.String(), `"wal_archives":1`) || !strings.Contains(usageResponse.Body.String(), `"wal_archive_bytes":`) {
		t.Fatalf("expected WAL usage metering: %d %s", usageResponse.Code, usageResponse.Body.String())
	}

	metricsResponse := perform(server, http.MethodGet, "/v1/metrics", "")
	if metricsResponse.Code != http.StatusOK || !strings.Contains(metricsResponse.Body.String(), `"wal_archives":1`) || !strings.Contains(metricsResponse.Body.String(), `"wal_archive_bytes":`) {
		t.Fatalf("expected WAL fleet metrics: %d %s", metricsResponse.Code, metricsResponse.Body.String())
	}

	prometheusResponse := perform(server, http.MethodGet, "/metrics", "")
	if prometheusResponse.Code != http.StatusOK || !strings.Contains(prometheusResponse.Body.String(), "supadupa_wal_archives_total 1") {
		t.Fatalf("expected WAL prometheus metric: %d %s", prometheusResponse.Code, prometheusResponse.Body.String())
	}

	logsResponse := perform(server, http.MethodGet, "/v1/projects/pitr-proj/logs", "")
	for _, message := range []string{"PITR policy updated", "WAL archive failed", "WAL segment archived"} {
		if !strings.Contains(logsResponse.Body.String(), message) {
			t.Fatalf("expected log message %q: %s", message, logsResponse.Body.String())
		}
	}

	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	for _, action := range []string{"project.pitr_policy_update", "project.wal_archive"} {
		if !strings.Contains(auditResponse.Body.String(), `"action":"`+action+`"`) {
			t.Fatalf("expected %s audit event: %s", action, auditResponse.Body.String())
		}
	}
}

func TestProjectPITRRestoreAPI(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUPADUPA_BACKUP_ROOT", root)
	t.Setenv("SUPADUPA_PITR_RESTORE_COMMAND", "printf 'pitr restore %s %s %s\\n' {{recovery_time_target_unix}} {{backup_path}} {{wal_segment}}")
	store := control.NewMemoryStore()
	server := NewServer(Config{Store: store, Provisioner: fakeProvisioner{}})
	orgResponse := perform(server, http.MethodPost, "/v1/orgs", `{"name":"Platform"}`)
	orgID := extractString(t, orgResponse.Body.String(), "id")
	projectResponse := perform(server, http.MethodPost, "/v1/orgs/"+orgID+"/projects", `{"ref":"pitr-restore","name":"PITR Restore","domain":"supadupa.test","profile":"full","resource_tier":"small"}`)
	if projectResponse.Code != http.StatusAccepted {
		t.Fatalf("expected project status 202, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
	target, err := store.CreateBackupStorageTarget(context.Background(), control.BackupStorageTargetInput{
		Name:            "Archive",
		Type:            "s3",
		Endpoint:        "https://s3.example.test",
		Region:          "us-east-1",
		Bucket:          "backups",
		Prefix:          "supadupa",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Default:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupStorageTargetTestResult(context.Background(), target.ID, time.Now().UTC(), "passed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateBackupPolicy(context.Background(), "pitr-restore", control.BackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical", StorageTargetID: target.ID}); err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(root, "base.tar")
	basePayload := []byte("physical base backup")
	if err := os.WriteFile(basePath, basePayload, 0o600); err != nil {
		t.Fatal(err)
	}
	baseHash := sha256.Sum256(basePayload)
	now := time.Now().UTC()
	if _, err := store.CreateBackup(context.Background(), control.BackupInput{
		ProjectRef:      "pitr-restore",
		Kind:            "physical",
		Location:        basePath,
		RemoteLocation:  "s3://backups/supadupa/projects/pitr-restore/backups/base.tar",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len(basePayload)),
		ChecksumSHA256:  hex.EncodeToString(baseHash[:]),
		Status:          "completed",
		VerifiedAt:      &now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePITRPolicy(context.Background(), "pitr-restore", control.PITRPolicyInput{Enabled: true, ArchiveBucket: "s3://archive/pitr-restore", RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	walPath := filepath.Join(root, "wal")
	walPayload := []byte("wal archive")
	if err := os.WriteFile(walPath, walPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	walHash := sha256.Sum256(walPayload)
	archive, err := store.CreateWALArchive(context.Background(), control.WALArchiveInput{
		ProjectRef:      "pitr-restore",
		Segment:         "000000010000000000000001",
		SegmentSource:   "postgres",
		Location:        walPath,
		RemoteLocation:  "s3://backups/supadupa/projects/pitr-restore/wal/000000010000000000000001.wal",
		StorageTargetID: target.ID,
		SizeBytes:       int64(len(walPayload)),
		ChecksumSHA256:  hex.EncodeToString(walHash[:]),
		Status:          "archived",
		VerifiedAt:      &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	restoreResponse := perform(server, http.MethodPost, "/v1/projects/pitr-restore/database/backups/restore-pitr", `{"recovery_time_target_unix":"`+fmt.Sprintf("%d", archive.CreatedAt.Unix())+`","confirmation":"restore pitr project pitr-restore"}`)
	if restoreResponse.Code != http.StatusCreated || !strings.Contains(restoreResponse.Body.String(), `"restore_state":"completed"`) || !strings.Contains(restoreResponse.Body.String(), `"recovery_time_target_unix":`+fmt.Sprintf("%d", archive.CreatedAt.Unix())) {
		t.Fatalf("expected created PITR restore response: %d %s", restoreResponse.Code, restoreResponse.Body.String())
	}
	restorePath := extractString(t, restoreResponse.Body.String(), "restore_path")
	transcript, err := os.ReadFile(restorePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "pitr restore "+fmt.Sprintf("%d", archive.CreatedAt.Unix())) || !strings.Contains(string(transcript), basePath) || !strings.Contains(string(transcript), archive.Segment) {
		t.Fatalf("expected PITR restore transcript, got:\n%s", string(transcript))
	}
	auditResponse := perform(server, http.MethodGet, "/v1/audit-events", "")
	if !strings.Contains(auditResponse.Body.String(), `"action":"project.restore_pitr"`) {
		t.Fatalf("expected PITR restore audit event: %s", auditResponse.Body.String())
	}
}
