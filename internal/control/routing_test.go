package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoutingServiceRendersTraefikDynamicConfig(t *testing.T) {
	root := t.TempDir()
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
		},
	}
	routes := RoutesForProject(project)
	path, err := NewRoutingService(root).RenderProject(project, routes)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, "alpha.yaml") {
		t.Fatalf("unexpected route path: %s", path)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, expected := range []string{
		"Host(`alpha.supadupa.test`)",
		"Host(`studio.alpha.supadupa.test`)",
		"certResolver: letsencrypt",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in rendered config:\n%s", expected, body)
		}
	}
}

func TestRoutingServiceRendersLocalStudioRouteWhenConfigured(t *testing.T) {
	t.Setenv("SUPADUPA_LOCAL_RUNTIME_ORIGIN", "https://runtime.supadupa.test")
	root := t.TempDir()
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
		},
	}

	routes := RoutesForProject(project)
	path, err := NewRoutingService(root).RenderProject(project, routes)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, expected := range []string{
		"alpha-api-local:",
		"Host(`runtime.supadupa.test`) && PathPrefix(`/projects/alpha/api`)",
		"alpha-api-local:\n      rule: \"Host(`runtime.supadupa.test`) && PathPrefix(`/projects/alpha/api`)\"\n      service: alpha-api-local\n      entryPoints:\n        - web\n",
		"alpha-api-local-stripprefix",
		`- "/projects/alpha/api"`,
		`url: "http://alpha-kong:8000"`,
		"alpha-studio-local:",
		"Host(`runtime.supadupa.test`) && PathPrefix(`/projects/alpha/studio`)",
		"alpha-studio-local:\n      rule: \"Host(`runtime.supadupa.test`) && PathPrefix(`/projects/alpha/studio`)\"\n      service: alpha-studio-local\n      entryPoints:\n        - web\n",
		"alpha-studio-local-stripprefix",
		`- "/projects/alpha/studio"`,
		`url: "http://alpha-studio:3000"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in rendered config:\n%s", expected, body)
		}
	}
}

func TestRoutesForProjectDomainsAddsCustomDomainRoute(t *testing.T) {
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
		},
	}
	routes := RoutesForProjectDomains(project, []ProjectDomain{{ProjectRef: "alpha", FQDN: "api.example.com"}})
	if len(routes) != 3 {
		t.Fatalf("expected base routes plus custom domain, got %d", len(routes))
	}
	custom := routes[2]
	if custom.Name != "custom-api-example-com" {
		t.Fatalf("unexpected custom route name: %s", custom.Name)
	}
	if custom.FQDN != "api.example.com" {
		t.Fatalf("unexpected custom route fqdn: %s", custom.FQDN)
	}
	if custom.UpstreamURL != "http://alpha-kong:8000" {
		t.Fatalf("unexpected custom route upstream: %s", custom.UpstreamURL)
	}
}

func TestRoutingServiceRendersNetworkPolicy(t *testing.T) {
	root := t.TempDir()
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
		},
	}
	routes := RoutesForProjectWithNetwork(project, ProjectConfig{
		Config: map[string]string{
			"ip_allowlist": "10.0.0.0/8, 2001:db8::/32",
			"ssl_enforced": "true",
		},
	})
	path, err := NewRoutingService(root).RenderProject(project, routes)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, expected := range []string{
		"alpha-api-ipallowlist",
		"ipAllowList:",
		"- \"10.0.0.0/8\"",
		"- \"2001:db8::/32\"",
		"alpha-api-redirect",
		"redirectScheme:",
		"scheme: https",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in rendered network policy:\n%s", expected, body)
		}
	}
}

func TestRoutingServiceRendersCDNHeaders(t *testing.T) {
	root := t.TempDir()
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
		},
	}
	routes := RoutesForProjectDomainsWithNetworkAndCDN(project, []ProjectDomain{{ProjectRef: "alpha", FQDN: "cdn.example.com"}}, ProjectConfig{}, ProjectCDNPolicy{
		ProjectRef:                  "alpha",
		Enabled:                     true,
		BrowserTTLSeconds:           300,
		EdgeTTLSeconds:              600,
		StaleWhileRevalidateSeconds: 30,
		SmartRevalidation:           true,
		CacheControl:                "public, max-age=300, s-maxage=600, stale-while-revalidate=30",
	})
	path, err := NewRoutingService(root).RenderProject(project, routes)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, expected := range []string{
		"alpha-api-cdn-headers",
		"alpha-custom-cdn-example-com-cdn-headers",
		"headers:",
		"Cache-Control: \"public, max-age=300, s-maxage=600, stale-while-revalidate=30\"",
		"X-Supadupa-CDN: \"enabled\"",
		"X-Supadupa-CDN-Smart: \"enabled\"",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in rendered cdn policy:\n%s", expected, body)
		}
	}
}

func TestRoutingServiceUsesEnvRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUPADUPA_ROUTES_ROOT", root)
	project := Project{
		Ref: "envroot",
		Spec: ProjectSpec{
			Ref:    "envroot",
			Domain: "supadupa.test",
		},
	}
	path, err := NewRoutingService("").RenderProject(project, RoutesForProject(project))
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, "envroot.yaml") {
		t.Fatalf("expected env route root, got %s", path)
	}
}
