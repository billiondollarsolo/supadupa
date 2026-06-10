package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestAPIRouteRegistrationSnapshot(t *testing.T) {
	file := parseAPIRoutesSource(t)

	const expectedRegisteredRouteCount = 201
	patterns := registeredRoutePatterns(t, file)
	if got := len(patterns); got != expectedRegisteredRouteCount {
		t.Fatalf("expected %d registered API route patterns, got %d", expectedRegisteredRouteCount, got)
	}

	assertOrderedCalls(t, routeRegistryMethodCalls(t, file, "registerAPIRoutes"), []string{
		"registerCoreRoutes",
		"registerAuthRoutes",
		"registerAccountRoutes",
		"registerUserRoutes",
		"registerSCIMRoutes",
		"registerPlatformRoutes",
		"registerOrgRoutes",
		"registerProjectRoutes",
	})
	assertOrderedCalls(t, routeRegistryMethodCalls(t, file, "registerProjectRoutes"), []string{
		"registerProjectOverviewRoutes",
		"registerProjectAccessAndRoutingRoutes",
		"registerProjectConfigRoutes",
		"registerProjectAuthRoutes",
		"registerProjectBranchAndReplicaRoutes",
		"registerProjectFunctionRoutes",
		"registerProjectDataRoutes",
		"registerProjectStorageAndEdgeRoutes",
		"registerProjectSecretRoutes",
		"registerProjectBackupAndOpsRoutes",
	})
	assertAllRouteGroupsAreWired(t, file)

	for _, critical := range []string{
		"GET /healthz",
		"GET /v1/health",
		"GET /metrics",
		"GET /v1/metrics",
		"GET /v1/runtime-config",
		"GET /v1/auth/state",
		"POST /v1/auth/bootstrap",
		"POST /v1/auth/login",
		"POST /v1/auth/logout",
		"GET /v1/auth/studio/verify",
		"GET /v1/auth/sso/saml/start",
		"POST /v1/auth/sso/saml/callback",
		"GET /v1/account/mfa",
		"POST /v1/account/mfa/enroll",
		"POST /v1/account/mfa/verify",
		"DELETE /v1/account/mfa",
		"GET /v1/scim/v2/ServiceProviderConfig",
		"GET /v1/scim/v2/Users",
		"POST /v1/scim/v2/Users",
		"PATCH /v1/scim/v2/Users/{id}",
		"GET /v1/scim/v2/Groups",
		"POST /v1/scim/v2/Groups",
		"GET /v1/settings/defaults",
		"PUT /v1/settings/defaults",
		"GET /v1/settings/sso",
		"PUT /v1/settings/sso",
		"GET /v1/backup-storage-targets",
		"POST /v1/backup-storage-targets",
		"GET /v1/platform/backups",
		"POST /v1/platform/backups",
		"GET /v1/audit-events",
		"GET /v1/audit-events/integrity",
		"GET /v1/hosts",
		"POST /v1/hosts",
		"GET /v1/orgs",
		"POST /v1/orgs",
		"GET /v1/projects",
		"GET /v1/orgs/{id}/members",
		"POST /v1/orgs/{id}/members",
		"GET /v1/orgs/{id}/projects",
		"POST /v1/orgs/{id}/projects",
		"GET /v1/projects/{ref}",
		"DELETE /v1/projects/{ref}",
		"GET /v1/projects/{ref}/metrics",
		"POST /v1/projects/{ref}/telemetry",
		"GET /v1/projects/{ref}/connect",
		"GET /v1/projects/{ref}/studio-session",
		"GET /v1/projects/{ref}/services",
		"PUT /v1/projects/{ref}/services",
		"GET /v1/projects/{ref}/config/{area}",
		"PUT /v1/projects/{ref}/config/{area}",
		"GET /v1/projects/{ref}/branches",
		"POST /v1/projects/{ref}/branches",
		"GET /v1/projects/{ref}/replicas",
		"POST /v1/projects/{ref}/replicas",
		"GET /v1/projects/{ref}/secrets",
		"GET /v1/projects/{ref}/secrets/{kind}/reveal",
		"POST /v1/projects/{ref}/secrets/{kind}/copy",
		"POST /v1/projects/{ref}/keys/rotate",
		"GET /v1/projects/{ref}/backups",
		"POST /v1/projects/{ref}/backups",
		"POST /v1/projects/{ref}/restore",
		"POST /v1/projects/{ref}/database/backups/restore-pitr",
		"GET /v1/projects/{ref}/logs",
		"GET /v1/projects/{ref}/logs/stream",
		"POST /v1/projects/{ref}/pause",
		"POST /v1/projects/{ref}/resume",
		"POST /v1/projects/{ref}/restart",
		"POST /v1/projects/{ref}/upgrade",
		"POST /v1/projects/{ref}/scale",
	} {
		if !patterns[critical] {
			t.Fatalf("critical API route is not registered: %s", critical)
		}
	}
}

func parseAPIRoutesSource(t *testing.T) *ast.File {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating current test file")
	}
	routesFile := strings.TrimSuffix(currentFile, "_test.go") + ".go"

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, routesFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", routesFile, err)
	}
	return file
}

func registeredRoutePatterns(t *testing.T, file *ast.File) map[string]bool {
	t.Helper()
	patterns := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "HandleFunc" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		pattern, err := parseStringLiteral(literal.Value)
		if err != nil {
			t.Fatalf("parse route pattern literal %s: %v", literal.Value, err)
		}
		if patterns[pattern] {
			t.Fatalf("duplicate API route registration: %s", pattern)
		}
		patterns[pattern] = true
		return true
	})
	return patterns
}

func routeRegistryMethodCalls(t *testing.T, file *ast.File, methodName string) []string {
	t.Helper()
	fn := findRouteRegistryMethod(t, file, methodName)
	var calls []string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !strings.HasPrefix(selector.Sel.Name, "register") || !strings.HasSuffix(selector.Sel.Name, "Routes") {
			return true
		}
		calls = append(calls, selector.Sel.Name)
		return true
	})
	return calls
}

func assertAllRouteGroupsAreWired(t *testing.T, file *ast.File) {
	t.Helper()
	wired := map[string]bool{"registerAPIRoutes": true}
	for _, name := range routeRegistryMethodCalls(t, file, "registerAPIRoutes") {
		wired[name] = true
	}
	for _, name := range routeRegistryMethodCalls(t, file, "registerProjectRoutes") {
		wired[name] = true
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || !strings.HasPrefix(fn.Name.Name, "register") || !strings.HasSuffix(fn.Name.Name, "Routes") {
			continue
		}
		if !wired[fn.Name.Name] {
			t.Fatalf("route group %s is defined but not called by registerAPIRoutes or registerProjectRoutes", fn.Name.Name)
		}
	}
}

func findRouteRegistryMethod(t *testing.T, file *ast.File, methodName string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == methodName {
			return fn
		}
	}
	t.Fatalf("route registration method not found: %s", methodName)
	return nil
}

func assertOrderedCalls(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("route group calls changed\nwant: %v\n got: %v", want, got)
	}
}

func parseStringLiteral(value string) (string, error) {
	return strconv.Unquote(value)
}
