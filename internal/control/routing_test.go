package control

import (
	"os"
	"path/filepath"
	"reflect"
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
		"Host(`studio-alpha.supadupa.test`)",
		"Host(`storage-alpha.supadupa.test`)",
		"certResolver: letsencrypt",
		`main: "*.supadupa.test"`,
		"alpha-studio-supadupa-sso",
		"forwardAuth:",
		`address: "http://host.docker.internal:8080/v1/auth/studio/verify?project_ref=alpha"`,
		"trustForwardHeader: false",
		"tcp:",
		"alpha-db:",
		"HostSNI(`db-alpha.supadupa.test`)",
		"- postgres",
		`address: "alpha-db:5432"`,
		"alpha-pooler-transaction:",
		"HostSNI(`pooler-alpha.supadupa.test`)",
		"- pooler",
		`address: "alpha-pooler:6543"`,
		"alpha-pooler-session:",
		`address: "alpha-pooler:5432"`,
		"options: alpha-postgres-alpn",
		"tls:",
		"alpha-postgres-alpn:",
		"alpnProtocols:",
		"- postgresql",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in rendered config:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "alpha-studio-redirect:\n      rule: \"Host(`studio-alpha.supadupa.test`)\"\n      service: alpha-studio\n      entryPoints:\n        - web\n      middlewares:\n        - alpha-studio-supadupa-sso") {
		t.Fatalf("studio HTTP redirect router must not require SSO before redirect:\n%s", body)
	}
}

func TestRoutingServiceOmitsCertResolverWhenDisabled(t *testing.T) {
	t.Setenv("SUPADUPA_TLS_CERT_RESOLVER", "")
	root := t.TempDir()
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "apps.supadupa.test",
		},
	}
	path, err := NewRoutingService(root).RenderProject(project, RoutesForProject(project))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	if strings.Contains(body, "certResolver:") {
		t.Fatalf("offline/local TLS should omit cert resolver:\n%s", body)
	}
	if !strings.Contains(body, `main: "*.apps.supadupa.test"`) || !strings.Contains(body, "options: alpha-postgres-alpn") {
		t.Fatalf("expected TLS domains and DB ALPN options to remain:\n%s", body)
	}
}

func TestTCPRoutesForProjectUseHostedDatabaseHosts(t *testing.T) {
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "apps.supadupa.test",
		},
	}

	routes := TCPRoutesForProject(project)
	if len(routes) != 3 {
		t.Fatalf("expected direct db plus transaction and session pooler routes, got %d", len(routes))
	}

	expected := map[string]ProjectTCPRoute{
		"db": {
			Protocol:        "tcp",
			Name:            "db",
			FQDN:            "db-alpha.apps.supadupa.test",
			EntryPoint:      "postgres",
			PublicPort:      5432,
			UpstreamAddress: "alpha-db:5432",
			TLS:             true,
			IPAllowlist:     []string{},
		},
		"pooler-transaction": {
			Protocol:        "tcp",
			Name:            "pooler-transaction",
			FQDN:            "pooler-alpha.apps.supadupa.test",
			EntryPoint:      "pooler",
			PublicPort:      6543,
			UpstreamAddress: "alpha-pooler:6543",
			TLS:             true,
			IPAllowlist:     []string{},
		},
		"pooler-session": {
			Protocol:        "tcp",
			Name:            "pooler-session",
			FQDN:            "pooler-alpha.apps.supadupa.test",
			EntryPoint:      "postgres",
			PublicPort:      5432,
			UpstreamAddress: "alpha-pooler:5432",
			TLS:             true,
			IPAllowlist:     []string{},
		},
	}
	for _, route := range routes {
		want, ok := expected[route.Name]
		if !ok {
			t.Fatalf("unexpected tcp route %#v", route)
		}
		if !reflect.DeepEqual(route, want) {
			t.Fatalf("route %s = %#v, want %#v", route.Name, route, want)
		}
	}
}

func TestTCPRoutesForProjectIncludeReplicaHosts(t *testing.T) {
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "apps.supadupa.test",
		},
	}

	routes := TCPRoutesForProjectWithNetworkAndReplicas(project, ProjectConfig{}, []ProjectReplica{
		{ID: "replica-one", Name: "east", Status: "healthy", Role: "read"},
	})
	seen := false
	for _, route := range routes {
		if route.Name != "db-replica-east" {
			continue
		}
		seen = true
		if route.FQDN != "db-replica-east-alpha.apps.supadupa.test" {
			t.Fatalf("unexpected replica fqdn: %#v", route)
		}
		if route.UpstreamAddress != "alpha-db-replica-east:5432" {
			t.Fatalf("unexpected replica upstream: %#v", route)
		}
		if route.EntryPoint != "postgres" || !route.TLS {
			t.Fatalf("unexpected replica tcp route flags: %#v", route)
		}
	}
	if !seen {
		t.Fatalf("expected replica tcp route in %#v", routes)
	}

	path, err := NewRoutingService(t.TempDir()).RenderProjectWithTCPRoutes(project, RoutesForProject(project), routes)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, expected := range []string{
		"alpha-db-replica-east:",
		"HostSNI(`db-replica-east-alpha.apps.supadupa.test`)",
		`address: "alpha-db-replica-east:5432"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in rendered replica tcp route:\n%s", expected, body)
		}
	}
}

func TestRoutesHonorDisabledStudioStorageAndPoolerServices(t *testing.T) {
	root := t.TempDir()
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "apps.supadupa.test",
			Services: map[string]ServiceSpec{
				"studio":  {Enabled: false},
				"storage": {Enabled: false},
				"pooler":  {Enabled: false},
			},
		},
	}

	httpRoutes := RoutesForProject(project)
	for _, route := range httpRoutes {
		if strings.Contains(route.Name, "studio") || strings.Contains(route.FQDN, "studio-alpha") || strings.Contains(route.UpstreamURL, "alpha-studio") {
			t.Fatalf("disabled studio service should not render a public route: %#v", route)
		}
		if strings.Contains(route.Name, "storage") || strings.Contains(route.FQDN, "storage-alpha") {
			t.Fatalf("disabled storage service should not render a public route: %#v", route)
		}
	}

	tcpRoutes := TCPRoutesForProject(project)
	if len(tcpRoutes) != 1 || tcpRoutes[0].Name != "db" {
		t.Fatalf("disabled pooler should leave only direct db tcp route, got %#v", tcpRoutes)
	}

	manifest := RouteManifestForProject(project, httpRoutes)
	for _, route := range manifest.TCPRoutes {
		if strings.Contains(route.Name, "pooler") || strings.Contains(route.FQDN, "pooler-alpha") || strings.Contains(route.UpstreamAddress, "alpha-pooler") {
			t.Fatalf("disabled pooler service should not appear in route manifest: %#v", route)
		}
	}

	path, err := NewRoutingService(root).RenderProject(project, httpRoutes)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, unexpected := range []string{
		"studio-alpha.apps.supadupa.test",
		"alpha-studio",
		"storage-alpha.apps.supadupa.test",
		"pooler-alpha.apps.supadupa.test",
		"alpha-pooler",
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("disabled service route leaked %q in rendered config:\n%s", unexpected, body)
		}
	}
	if !strings.Contains(body, "Host(`alpha.apps.supadupa.test`)") || !strings.Contains(body, "HostSNI(`db-alpha.apps.supadupa.test`)") {
		t.Fatalf("expected API and direct DB routes to remain:\n%s", body)
	}
}

func TestRouteManifestForProjectIncludesHTTPAndTCPRoutes(t *testing.T) {
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "apps.supadupa.test",
		},
	}
	httpRoutes := []ProjectRoute{
		{Name: "api", FQDN: "alpha.apps.supadupa.test", UpstreamURL: "http://alpha-kong:8000", TLS: true},
		{Name: "studio", FQDN: "studio-alpha.apps.supadupa.test", UpstreamURL: "http://alpha-studio:3000", TLS: true},
	}

	manifest := RouteManifestForProject(project, httpRoutes)
	if manifest.ProjectRef != "alpha" {
		t.Fatalf("manifest project ref = %q", manifest.ProjectRef)
	}
	if len(manifest.HTTPRoutes) != 2 {
		t.Fatalf("expected 2 http routes, got %d", len(manifest.HTTPRoutes))
	}
	if len(manifest.TCPRoutes) != 3 {
		t.Fatalf("expected db and pooler tcp routes, got %d", len(manifest.TCPRoutes))
	}
	for _, route := range manifest.TCPRoutes {
		if route.Protocol != "tcp" || route.FQDN == "" || route.EntryPoint == "" || route.PublicPort == 0 || route.UpstreamAddress == "" || !route.TLS {
			t.Fatalf("incomplete tcp route metadata: %#v", route)
		}
	}
}

func TestWildcardCertDomainOnlyCoversProjectDomainChildren(t *testing.T) {
	for _, tt := range []struct {
		name       string
		baseDomain string
		fqdn       string
		want       string
	}{
		{name: "child", baseDomain: "apps.supadupa.test", fqdn: "alpha.apps.supadupa.test", want: "*.apps.supadupa.test"},
		{name: "studio", baseDomain: "apps.supadupa.test", fqdn: "studio-alpha.apps.supadupa.test", want: "*.apps.supadupa.test"},
		{name: "database", baseDomain: "apps.supadupa.test", fqdn: "db-alpha.apps.supadupa.test", want: "*.apps.supadupa.test"},
		{name: "apex", baseDomain: "apps.supadupa.test", fqdn: "apps.supadupa.test"},
		{name: "custom", baseDomain: "apps.supadupa.test", fqdn: "api.example.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := wildcardCertDomain(tt.baseDomain, tt.fqdn); got != tt.want {
				t.Fatalf("wildcardCertDomain(%q, %q) = %q, want %q", tt.baseDomain, tt.fqdn, got, tt.want)
			}
		})
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
	if len(routes) != 4 {
		t.Fatalf("expected base routes plus custom domain, got %d", len(routes))
	}
	custom := routes[3]
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

func TestRoutingServiceRendersBYOCustomDomainCertificate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUPADUPA_CERTS_TRAEFIK_DIR", "/edge-certs")
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "supadupa.test",
		},
	}
	routes := RoutesForProjectDomains(project, []ProjectDomain{{
		ProjectRef: "alpha",
		FQDN:       "api.example.com",
		CertStatus: "uploaded",
		CertMode:   "byo",
	}})
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
		"      tls: {}",
		"certFile: \"/edge-certs/alpha/api.example.com.crt\"",
		"keyFile: \"/edge-certs/alpha/api.example.com.key\"",
		"alpha-postgres-alpn",
		"alpnProtocols:",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected rendered config to contain %q, got:\n%s", expected, body)
		}
	}
	customRouter := body[strings.Index(body, "alpha-custom-api-example-com:"):]
	customRouter = strings.Split(customRouter, "alpha-custom-api-example-com-redirect:")[0]
	if strings.Contains(customRouter, "certResolver: letsencrypt") {
		t.Fatalf("expected BYO custom router to omit cert resolver, got:\n%s", customRouter)
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
		"alpha-db-tcp-ipallowlist",
		"alpha-pooler-transaction-tcp-ipallowlist",
		"alpha-pooler-session-tcp-ipallowlist",
		"alpha-api-redirect",
		"redirectScheme:",
		"scheme: https",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected %q in rendered network policy:\n%s", expected, body)
		}
	}
}

func TestTCPRoutesForProjectHonorNetworkPolicy(t *testing.T) {
	project := Project{
		Ref: "alpha",
		Spec: ProjectSpec{
			Ref:    "alpha",
			Domain: "apps.supadupa.test",
		},
	}
	routes := TCPRoutesForProjectWithNetwork(project, ProjectConfig{
		Config: map[string]string{"ip_allowlist": "10.0.0.0/8, 2001:db8::/32"},
	})
	if len(routes) != 3 {
		t.Fatalf("expected db and pooler routes, got %d", len(routes))
	}
	for _, route := range routes {
		if strings.Join(route.IPAllowlist, ",") != "10.0.0.0/8,2001:db8::/32" {
			t.Fatalf("route %s allowlist = %#v", route.Name, route.IPAllowlist)
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
