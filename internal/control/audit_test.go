package control

import (
	"context"
	"testing"
)

func TestAuditLogIntegrityDetectsTampering(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if _, err := store.RecordAuditEvent(ctx, AuditEventInput{Action: "org.create", Target: "org:first", Metadata: map[string]string{"name": "First"}}); err != nil {
		t.Fatalf("record first audit event: %v", err)
	}
	if _, err := store.RecordAuditEvent(ctx, AuditEventInput{Action: "project.create", Target: "project:first"}); err != nil {
		t.Fatalf("record second audit event: %v", err)
	}

	integrity, err := store.VerifyAuditLog(ctx)
	if err != nil {
		t.Fatalf("verify audit log: %v", err)
	}
	if !integrity.Verified || integrity.Events != 2 || integrity.HeadHash == "" {
		t.Fatalf("expected verified two-event chain: %+v", integrity)
	}

	store.mu.Lock()
	store.auditEvents[0].Target = "org:tampered"
	store.mu.Unlock()

	integrity, err = store.VerifyAuditLog(ctx)
	if err != nil {
		t.Fatalf("verify tampered audit log: %v", err)
	}
	if integrity.Verified || integrity.BrokenAt != 1 {
		t.Fatalf("expected tampered chain at first event: %+v", integrity)
	}
}
