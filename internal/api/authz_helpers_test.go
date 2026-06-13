package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"supadupa2026/internal/control"
)

func TestRouteLocalAuthzHelpersFailClosedWithoutClaims(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, control.CreateProjectRequest{OrgID: org.ID, Ref: "authz-helper", Name: "Authz Helper"}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/projects/authz-helper/restore", nil)
	response := httptest.NewRecorder()
	if requirePlatformAdmin(response, request, store) {
		t.Fatal("platform admin helper allowed request without claims")
	}
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected platform helper 401, got %d", response.Code)
	}

	response = httptest.NewRecorder()
	if requireOrgRole(response, request, store, org.ID, roleViewer) {
		t.Fatal("org role helper allowed request without claims")
	}
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected org helper 401, got %d", response.Code)
	}

	response = httptest.NewRecorder()
	if _, ok := requireProjectRole(response, request, store, "authz-helper", roleViewer); ok {
		t.Fatal("project role helper allowed request without claims")
	}
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected project helper 401, got %d", response.Code)
	}
}

func TestRouteLocalAuthzHelpersAllowExplicitAuthBypass(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, control.CreateProjectRequest{OrgID: org.ID, Ref: "authz-bypass", Name: "Authz Bypass"}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/projects/authz-bypass/restore", nil)
	request = request.WithContext(context.WithValue(request.Context(), authBypassKey, true))

	if !requirePlatformAdmin(httptest.NewRecorder(), request, store) {
		t.Fatal("platform admin helper rejected explicit auth bypass")
	}
	if !requireOrgRole(httptest.NewRecorder(), request, store, org.ID, roleViewer) {
		t.Fatal("org role helper rejected explicit auth bypass")
	}
	if _, ok := requireProjectRole(httptest.NewRecorder(), request, store, "authz-bypass", roleViewer); !ok {
		t.Fatal("project role helper rejected explicit auth bypass")
	}
}
