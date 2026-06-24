package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestRouteLocalAuthzHelpersRejectStaleClaims(t *testing.T) {
	ctx := context.Background()
	store := control.NewMemoryStore()
	user, err := store.CreateUser(ctx, control.CreateUserRequest{Email: "member@example.com", Password: "super-secure", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertOrgMember(ctx, org.ID, control.MembershipInput{Email: user.Email, Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, control.CreateProjectRequest{OrgID: org.ID, Ref: "authz-stale", Name: "Authz Stale"}); err != nil {
		t.Fatal(err)
	}
	auth := control.NewAuthService("test-secret")
	token, err := auth.Issue(user, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := auth.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateUser(ctx, user.ID, control.UpdateUserRequest{Email: user.Email, Password: "new-super-secure", Role: user.Role}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/projects/authz-stale/secrets", nil)
	request = request.WithContext(context.WithValue(request.Context(), tokenClaimsKey, claims))

	orgResponse := httptest.NewRecorder()
	if requireOrgRole(orgResponse, request, store, org.ID, roleViewer) {
		t.Fatal("org role helper allowed stale claims")
	}
	if orgResponse.Code != http.StatusUnauthorized || !strings.Contains(orgResponse.Body.String(), "stale bearer token") {
		t.Fatalf("expected stale org helper rejection, got %d: %s", orgResponse.Code, orgResponse.Body.String())
	}

	projectResponse := httptest.NewRecorder()
	if _, ok := requireProjectRole(projectResponse, request, store, "authz-stale", roleViewer); ok {
		t.Fatal("project role helper allowed stale claims")
	}
	if projectResponse.Code != http.StatusUnauthorized || !strings.Contains(projectResponse.Body.String(), "stale bearer token") {
		t.Fatalf("expected stale project helper rejection, got %d: %s", projectResponse.Code, projectResponse.Body.String())
	}
}
