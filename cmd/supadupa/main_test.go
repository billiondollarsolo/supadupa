package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"supadupa2026/internal/control"
)

func TestBootstrapInitialAdminNoEnvDoesNothing(t *testing.T) {
	t.Setenv("SUPADUPA_BOOTSTRAP_EMAIL", "")
	t.Setenv("SUPADUPA_BOOTSTRAP_PASSWORD", "")
	store := control.NewMemoryStore()

	if err := bootstrapInitialAdmin(context.Background(), store, discardLogger()); err != nil {
		t.Fatalf("bootstrapInitialAdmin returned error: %v", err)
	}
	if store.HasUsers(context.Background()) {
		t.Fatal("expected no users without bootstrap env")
	}
}

func TestBootstrapInitialAdminCreatesAdminFromEnv(t *testing.T) {
	t.Setenv("SUPADUPA_BOOTSTRAP_EMAIL", "Admin@SUPADUPA.local")
	t.Setenv("SUPADUPA_BOOTSTRAP_PASSWORD", "supadupa2026")
	store := control.NewMemoryStore()
	ctx := context.Background()

	if err := bootstrapInitialAdmin(ctx, store, discardLogger()); err != nil {
		t.Fatalf("bootstrapInitialAdmin returned error: %v", err)
	}
	user, err := store.AuthenticateUser(ctx, "admin@supadupa.local", "supadupa2026")
	if err != nil {
		t.Fatalf("expected seeded admin to authenticate: %v", err)
	}
	if user.Role != "admin" {
		t.Fatalf("expected admin role, got %q", user.Role)
	}
	events, err := store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatalf("expected audit events: %v", err)
	}
	if len(events) != 1 || events[0].Action != "user.bootstrap_env" {
		t.Fatalf("expected bootstrap audit event, got %#v", events)
	}

	if err := bootstrapInitialAdmin(ctx, store, discardLogger()); err != nil {
		t.Fatalf("second bootstrapInitialAdmin returned error: %v", err)
	}
	events, err = store.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatalf("expected audit events after second bootstrap: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected second bootstrap to skip existing users, got %d audit events", len(events))
	}
}

func TestBootstrapInitialAdminRequiresEmailAndPasswordTogether(t *testing.T) {
	t.Setenv("SUPADUPA_BOOTSTRAP_EMAIL", "admin@supadupa.local")
	t.Setenv("SUPADUPA_BOOTSTRAP_PASSWORD", "")

	err := bootstrapInitialAdmin(context.Background(), control.NewMemoryStore(), discardLogger())
	if err == nil || !strings.Contains(err.Error(), "must be set together") {
		t.Fatalf("expected paired-env error, got %v", err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
