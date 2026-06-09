package control

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var routeNamePattern = regexp.MustCompile(`[^a-z0-9-]+`)

type RoutingService struct {
	rootDir string
}

type ProjectTCPRoute struct {
	Protocol        string   `json:"protocol"`
	Name            string   `json:"name"`
	FQDN            string   `json:"fqdn"`
	EntryPoint      string   `json:"entrypoint"`
	PublicPort      int      `json:"public_port"`
	UpstreamAddress string   `json:"upstream_address"`
	TLS             bool     `json:"tls"`
	IPAllowlist     []string `json:"ip_allowlist,omitempty"`
}

type ProjectRouteManifest struct {
	ProjectRef string            `json:"project_ref"`
	HTTPRoutes []ProjectRoute    `json:"http_routes"`
	TCPRoutes  []ProjectTCPRoute `json:"tcp_routes"`
	// DatabaseIngressPublished reports whether the platform edge-router actually
	// publishes the database/pooler ports on a public interface. When false, a
	// project set to public/allowlisted still won't be reachable from outside
	// the host until the platform binds those ports — surfaced in the UI so the
	// exposure control never silently implies reachability it can't deliver.
	DatabaseIngressPublished bool `json:"database_ingress_published"`
	// DatabaseExternalAccessEnabled is the platform master switch. When false,
	// every project is forced private regardless of its own mode.
	DatabaseExternalAccessEnabled bool `json:"database_external_access_enabled"`
}

func NewRoutingService(rootDir string) *RoutingService {
	if rootDir == "" {
		rootDir = os.Getenv("SUPADUPA_ROUTES_ROOT")
	}
	if rootDir == "" {
		rootDir = "./runtime/routes"
	}
	return &RoutingService{rootDir: rootDir}
}

func RoutesForProject(project Project) []ProjectRoute {
	return RoutesForProjectWithNetwork(project, ProjectConfig{})
}

func RoutesForProjectWithNetwork(project Project, network ProjectConfig) []ProjectRoute {
	return RoutesForProjectWithNetworkAndCDN(project, network, ProjectCDNPolicy{})
}

func RoutesForProjectWithNetworkAndCDN(project Project, network ProjectConfig, cdn ProjectCDNPolicy) []ProjectRoute {
	baseHost := projectHost(project.Ref, project.Spec.Domain)
	policy := networkPolicyFromConfig(network)
	cache := routeCachePolicyFromCDN(cdn)
	services := ProjectServiceStates(project.Spec.Services)
	routes := []ProjectRoute{
		{
			Name:         "api",
			FQDN:         baseHost,
			UpstreamURL:  fmt.Sprintf("http://%s-kong:8000", project.Ref),
			TLS:          true,
			SSLEnforced:  policy.SSLEnforced,
			IPAllowlist:  policy.HTTPAllowlist,
			CacheControl: cache.CacheControl,
			SmartCDN:     cache.SmartCDN,
		},
	}
	if services["studio"] {
		routes = append(routes, ProjectRoute{
			Name:        "studio",
			FQDN:        studioHost(project.Ref, project.Spec.Domain),
			UpstreamURL: fmt.Sprintf("http://%s-studio:3000", project.Ref),
			TLS:         true,
			SSLEnforced: policy.SSLEnforced,
			IPAllowlist: policy.HTTPAllowlist,
			SSORequired: true,
		})
	}
	if services["storage"] {
		routes = append(routes, ProjectRoute{
			Name:         "storage",
			FQDN:         storageHost(project.Ref, project.Spec.Domain),
			UpstreamURL:  fmt.Sprintf("http://%s-kong:8000", project.Ref),
			TLS:          true,
			SSLEnforced:  policy.SSLEnforced,
			IPAllowlist:  policy.HTTPAllowlist,
			CacheControl: cache.CacheControl,
			SmartCDN:     cache.SmartCDN,
		})
	}
	if localRuntimeOrigin := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPADUPA_LOCAL_RUNTIME_ORIGIN")), "/"); localRuntimeOrigin != "" {
		if localHost, ok := hostFromOrigin(localRuntimeOrigin); ok {
			routes = append(routes, ProjectRoute{
				Name:        "api-local",
				FQDN:        localHost,
				PathPrefix:  localAPIPath(project.Ref),
				StripPrefix: localAPIPath(project.Ref),
				UpstreamURL: fmt.Sprintf("http://%s-kong:8000", project.Ref),
				TLS:         false,
			})
			if services["studio"] {
				routes = append(routes, ProjectRoute{
					Name:        "studio-local",
					FQDN:        localHost,
					PathPrefix:  localStudioPath(project.Ref),
					StripPrefix: localStudioPath(project.Ref),
					UpstreamURL: fmt.Sprintf("http://%s-studio:3000", project.Ref),
					TLS:         false,
					SSORequired: true,
				})
			}
		}
	}
	return routes
}

func RoutesForProjectDomains(project Project, domains []ProjectDomain) []ProjectRoute {
	return RoutesForProjectDomainsWithNetwork(project, domains, ProjectConfig{})
}

func RoutesForProjectDomainsWithNetwork(project Project, domains []ProjectDomain, network ProjectConfig) []ProjectRoute {
	return RoutesForProjectDomainsWithNetworkAndCDN(project, domains, network, ProjectCDNPolicy{})
}

func RoutesForProjectDomainsWithNetworkAndCDN(project Project, domains []ProjectDomain, network ProjectConfig, cdn ProjectCDNPolicy) []ProjectRoute {
	routes := RoutesForProjectWithNetworkAndCDN(project, network, cdn)
	policy := networkPolicyFromConfig(network)
	cache := routeCachePolicyFromCDN(cdn)
	for _, domain := range domains {
		route := ProjectRoute{
			Name:         "custom-" + routeName(domain.FQDN),
			FQDN:         domain.FQDN,
			UpstreamURL:  fmt.Sprintf("http://%s-kong:8000", project.Ref),
			TLS:          true,
			SSLEnforced:  policy.SSLEnforced,
			IPAllowlist:  policy.HTTPAllowlist,
			CacheControl: cache.CacheControl,
			SmartCDN:     cache.SmartCDN,
			CertMode:     domain.CertMode,
		}
		if strings.EqualFold(domain.CertMode, "byo") || strings.EqualFold(domain.CertStatus, "uploaded") {
			route.CertFile, route.KeyFile = traefikCertificatePaths(project.Ref, domain.FQDN)
			route.CertMode = "byo"
		}
		routes = append(routes, route)
	}
	return routes
}

// TCPRoutesForProject renders a project's TCP routes with no network policy or
// replicas — a convenience over TCPRoutesForProjectWithNetworkReplicasAndDatabaseIngress.
func TCPRoutesForProject(project Project) []ProjectTCPRoute {
	return TCPRoutesForProjectWithNetworkReplicasAndDatabaseIngress(project, ProjectConfig{}, nil, nil)
}

func TCPRoutesForProjectWithNetworkReplicasAndDatabaseIngress(project Project, network ProjectConfig, replicas []ProjectReplica, databaseIngressAllowedCIDRs []string) []ProjectTCPRoute {
	dbHost := databaseHost(project.Ref, project.Spec.Domain)
	poolerHost := poolerHost(project.Ref, project.Spec.Domain)
	policy := networkPolicyFromConfig(network)
	publish, tcpIPAllowlist := databaseIngressDecision(policy, databaseIngressAllowedCIDRs)
	if !publish {
		// Private: this project's database is not routed through the edge-router.
		return []ProjectTCPRoute{}
	}
	routes := []ProjectTCPRoute{
		{
			Protocol:        "tcp",
			Name:            "db",
			FQDN:            dbHost,
			EntryPoint:      "postgres",
			PublicPort:      5432,
			UpstreamAddress: fmt.Sprintf("%s-db:5432", project.Ref),
			TLS:             true,
			IPAllowlist:     tcpIPAllowlist,
		},
	}
	for _, replica := range replicas {
		name := strings.TrimSpace(replica.Name)
		if name == "" {
			name = replica.ID
		}
		if name == "" {
			continue
		}
		service := replicaComposeServiceNameForRoute(name)
		routes = append(routes, ProjectTCPRoute{
			Protocol:        "tcp",
			Name:            "db-replica-" + routeName(name),
			FQDN:            replicaDatabaseHost(project.Ref, name, project.Spec.Domain),
			EntryPoint:      "postgres",
			PublicPort:      5432,
			UpstreamAddress: fmt.Sprintf("%s-%s:5432", project.Ref, service),
			TLS:             true,
			IPAllowlist:     tcpIPAllowlist,
		})
	}
	if !ProjectServiceStates(project.Spec.Services)["pooler"] {
		return routes
	}
	routes = append(routes,
		ProjectTCPRoute{
			Protocol:        "tcp",
			Name:            "pooler-transaction",
			FQDN:            poolerHost,
			EntryPoint:      "pooler",
			PublicPort:      6543,
			UpstreamAddress: fmt.Sprintf("%s-pooler:6543", project.Ref),
			TLS:             true,
			IPAllowlist:     tcpIPAllowlist,
		},
		ProjectTCPRoute{
			Protocol:        "tcp",
			Name:            "pooler-session",
			FQDN:            poolerHost,
			EntryPoint:      "postgres",
			PublicPort:      5432,
			UpstreamAddress: fmt.Sprintf("%s-pooler:5432", project.Ref),
			TLS:             true,
			IPAllowlist:     tcpIPAllowlist,
		},
	)
	return routes
}

// RouteManifestForProject builds a manifest with no network policy or replicas —
// a convenience over RouteManifestForProjectWithNetworkReplicasAndDatabaseIngress.
func RouteManifestForProject(project Project, httpRoutes []ProjectRoute) ProjectRouteManifest {
	return RouteManifestForProjectWithNetworkReplicasAndDatabaseIngress(project, httpRoutes, ProjectConfig{}, nil, nil)
}

func RouteManifestForProjectWithNetworkReplicasAndDatabaseIngress(project Project, httpRoutes []ProjectRoute, network ProjectConfig, replicas []ProjectReplica, databaseIngressAllowedCIDRs []string) ProjectRouteManifest {
	return ProjectRouteManifest{
		ProjectRef: project.Ref,
		HTTPRoutes: cloneProjectRoutes(httpRoutes),
		TCPRoutes:  TCPRoutesForProjectWithNetworkReplicasAndDatabaseIngress(project, network, replicas, databaseIngressAllowedCIDRs),
	}
}

type routeCachePolicy struct {
	CacheControl string
	SmartCDN     bool
}

type routeNetworkPolicy struct {
	SSLEnforced bool
	// HTTPAllowlist gates the HTTP edge routes (API/Studio/Storage/custom
	// domains). DBAllowlist independently gates the database/pooler TCP ports.
	// Both default to empty = open to all; the two surfaces are controlled
	// separately so restricting raw Postgres never blocks Studio/API.
	HTTPAllowlist []string
	DBAllowlist   []string
	// DBIngressMode is the per-project database exposure mode: "private",
	// "allowlisted", or "public". Empty means the project has not opted into
	// per-project control yet and legacy (platform-default) behavior applies.
	DBIngressMode string
}

func routeCachePolicyFromCDN(policy ProjectCDNPolicy) routeCachePolicy {
	if !policy.Enabled {
		return routeCachePolicy{}
	}
	cacheControl := strings.TrimSpace(policy.CacheControl)
	if cacheControl == "" {
		cacheControl = fmt.Sprintf("public, max-age=%d, s-maxage=%d", policy.BrowserTTLSeconds, policy.EdgeTTLSeconds)
		if policy.StaleWhileRevalidateSeconds > 0 {
			cacheControl += fmt.Sprintf(", stale-while-revalidate=%d", policy.StaleWhileRevalidateSeconds)
		}
	}
	return routeCachePolicy{CacheControl: cacheControl, SmartCDN: policy.SmartRevalidation}
}

func networkPolicyFromConfig(config ProjectConfig) routeNetworkPolicy {
	policy := routeNetworkPolicy{SSLEnforced: true}
	if strings.EqualFold(strings.TrimSpace(config.Config["ssl_enforced"]), "false") {
		policy.SSLEnforced = false
	}
	policy.HTTPAllowlist = splitAllowlist(config.Config["http_allowlist"])
	policy.DBAllowlist = splitAllowlist(config.Config["db_allowlist"])
	policy.DBIngressMode = strings.ToLower(strings.TrimSpace(config.Config["db_ingress_mode"]))
	return policy
}

// databaseIngressDecision resolves, for a single project, whether its database
// TCP routes should be published and with which IP allowlist — derived ONLY
// from that project's own network policy once it has opted into per-project
// control. The platform-wide allowlist is used solely as legacy fallback for
// projects that have not set a mode, so one project's exposure can never be
// inferred from the fleet or from another project.
func databaseIngressDecision(policy routeNetworkPolicy, databaseIngressAllowedCIDRs []string) (publish bool, allowlist []string) {
	switch policy.DBIngressMode {
	case "private":
		return false, nil
	case "public":
		// Reachable through the edge-router with no IP restriction.
		return true, []string{}
	case "allowlisted":
		// Must list at least one CIDR; an empty allowlist is treated as private
		// rather than silently open.
		if len(policy.DBAllowlist) == 0 {
			return false, nil
		}
		return true, policy.DBAllowlist
	default:
		// Unset = legacy fallback (only reached by direct callers that pass no
		// network config). Real projects always carry the schema default
		// "private", so they stay off until explicitly enabled.
		return true, databaseIngressIPAllowlist(policy.DBAllowlist, databaseIngressAllowedCIDRs)
	}
}

// DatabaseExternalAccessFlag is the platform feature-flag key for the master
// switch that publishes project databases through the edge router.
const DatabaseExternalAccessFlag = "database_external_access"

// ApplyDatabaseExternalAccessGate enforces the platform master switch: when
// external database access is disabled, the returned network config forces the
// project private so no database TCP routes are rendered — regardless of the
// project's own mode. Shared by the API reconcile/manifest paths and the
// startup route repair so the kill switch is honored everywhere, including
// across restarts.
func ApplyDatabaseExternalAccessGate(networkConfig ProjectConfig, defaults PlatformDefaults) ProjectConfig {
	if defaults.FeatureFlags[DatabaseExternalAccessFlag] {
		return networkConfig
	}
	cfg := make(map[string]string, len(networkConfig.Config)+1)
	for k, v := range networkConfig.Config {
		cfg[k] = v
	}
	cfg["db_ingress_mode"] = "private"
	networkConfig.Config = cfg
	return networkConfig
}

func databaseIngressIPAllowlist(projectAllowlist []string, databaseIngressAllowedCIDRs []string) []string {
	platformAllowlist := splitAllowlist(strings.Join(databaseIngressAllowedCIDRs, ","))
	if len(platformAllowlist) > 0 {
		return platformAllowlist
	}
	return projectAllowlist
}

func splitAllowlist(input string) []string {
	parts := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func routeName(input string) string {
	name := strings.Trim(routeNamePattern.ReplaceAllString(strings.ToLower(input), "-"), "-")
	if name == "" {
		return "domain"
	}
	return name
}

func projectHost(ref string, domain string) string {
	return fmt.Sprintf("%s.%s", ref, domain)
}

func studioHost(ref string, domain string) string {
	return fmt.Sprintf("studio-%s.%s", ref, domain)
}

func storageHost(ref string, domain string) string {
	return fmt.Sprintf("storage-%s.%s", ref, domain)
}

func databaseHost(ref string, domain string) string {
	return fmt.Sprintf("db-%s.%s", ref, domain)
}

func replicaDatabaseHost(ref string, replicaName string, domain string) string {
	return fmt.Sprintf("db-replica-%s-%s.%s", routeName(replicaName), ref, domain)
}

func replicaComposeServiceNameForRoute(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "replica"
	}
	var builder strings.Builder
	builder.WriteString("db-replica-")
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune('-')
	}
	out := strings.TrimRight(builder.String(), "-")
	if out == "db-replica" {
		return "db-replica-replica"
	}
	return out
}

func poolerHost(ref string, domain string) string {
	return fmt.Sprintf("pooler-%s.%s", ref, domain)
}

func localAPIPath(ref string) string {
	return "/projects/" + ref + "/api"
}

func localStudioPath(ref string) string {
	return "/projects/" + ref + "/studio"
}

func hostFromOrigin(origin string) (string, bool) {
	withoutScheme := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	host := strings.TrimSpace(strings.Split(withoutScheme, "/")[0])
	if host == "" {
		return "", false
	}
	return strings.Split(host, ":")[0], true
}

func traefikCertificatePaths(ref string, fqdn string) (string, string) {
	root := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPADUPA_CERTS_TRAEFIK_DIR")), "/")
	if root == "" {
		root = "/certs"
	}
	return root + "/" + ref + "/" + fqdn + ".crt", root + "/" + ref + "/" + fqdn + ".key"
}

func (s *RoutingService) RenderProject(project Project, routes []ProjectRoute) (string, error) {
	return s.RenderProjectWithTCPRoutes(project, routes, tcpRoutesForProjectFromHTTPRoutes(project, routes))
}

func (s *RoutingService) RenderProjectWithTCPRoutes(project Project, routes []ProjectRoute, tcpRoutes []ProjectTCPRoute) (string, error) {
	if err := os.MkdirAll(s.rootDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(s.rootDir, project.Ref+".yaml")
	var builder strings.Builder
	builder.WriteString("http:\n")
	builder.WriteString("  routers:\n")
	for _, route := range routes {
		routerName := fmt.Sprintf("%s-%s", project.Ref, route.Name)
		builder.WriteString(fmt.Sprintf("    %s:\n", routerName))
		builder.WriteString(fmt.Sprintf("      rule: %q\n", routeRule(route)))
		builder.WriteString(fmt.Sprintf("      service: %s\n", routerName))
		builder.WriteString("      entryPoints:\n")
		if route.TLS {
			builder.WriteString("        - websecure\n")
		} else {
			builder.WriteString("        - web\n")
		}
		if route.TLS {
			if strings.TrimSpace(route.CertFile) != "" && strings.TrimSpace(route.KeyFile) != "" {
				builder.WriteString("      tls: {}\n")
			} else {
				builder.WriteString("      tls:\n")
				if resolver := routeTLSCertResolver(); resolver != "" {
					builder.WriteString(fmt.Sprintf("        certResolver: %s\n", resolver))
				}
				if wildcard := wildcardCertDomain(project.Spec.Domain, route.FQDN); wildcard != "" {
					builder.WriteString("        domains:\n")
					builder.WriteString(fmt.Sprintf("          - main: %q\n", wildcard))
				}
			}
		}
		middlewares := routeMiddlewares(project.Ref, route)
		if len(middlewares) > 0 {
			builder.WriteString("      middlewares:\n")
			for _, middleware := range middlewares {
				builder.WriteString(fmt.Sprintf("        - %s\n", middleware))
			}
		}
		if route.SSLEnforced {
			redirectRouter := routerName + "-redirect"
			builder.WriteString(fmt.Sprintf("    %s:\n", redirectRouter))
			builder.WriteString(fmt.Sprintf("      rule: %q\n", routeRule(route)))
			builder.WriteString(fmt.Sprintf("      service: %s\n", routerName))
			builder.WriteString("      entryPoints:\n")
			builder.WriteString("        - web\n")
			builder.WriteString("      middlewares:\n")
			builder.WriteString(fmt.Sprintf("        - %s\n", redirectMiddlewareName(project.Ref, route.Name)))
		}
	}
	builder.WriteString("  services:\n")
	for _, route := range routes {
		serviceName := fmt.Sprintf("%s-%s", project.Ref, route.Name)
		builder.WriteString(fmt.Sprintf("    %s:\n", serviceName))
		builder.WriteString("      loadBalancer:\n")
		builder.WriteString("        servers:\n")
		builder.WriteString(fmt.Sprintf("          - url: \"%s\"\n", route.UpstreamURL))
	}
	middlewarePayload := renderMiddlewares(project.Ref, routes)
	if middlewarePayload != "" {
		builder.WriteString("  middlewares:\n")
		builder.WriteString(middlewarePayload)
	}
	// Only emit the tcp section when there are TCP routes. An empty
	// "tcp:\n  routers:\n  services:" block is rejected by Traefik ("tcp cannot
	// be a standalone element"), which makes the file invalid and causes the
	// file provider to drop ALL dynamic config — taking down unrelated routing.
	if len(tcpRoutes) > 0 {
		builder.WriteString("tcp:\n")
		builder.WriteString("  routers:\n")
	}
	for _, route := range tcpRoutes {
		routerName := fmt.Sprintf("%s-%s", project.Ref, route.Name)
		builder.WriteString(fmt.Sprintf("    %s:\n", routerName))
		builder.WriteString(fmt.Sprintf("      rule: %q\n", tcpRouteRule(route)))
		builder.WriteString(fmt.Sprintf("      service: %s\n", routerName))
		builder.WriteString("      entryPoints:\n")
		builder.WriteString(fmt.Sprintf("        - %s\n", route.EntryPoint))
		if route.TLS {
			builder.WriteString("      tls:\n")
			if resolver := routeTLSCertResolver(); resolver != "" {
				builder.WriteString(fmt.Sprintf("        certResolver: %s\n", resolver))
			}
			builder.WriteString(fmt.Sprintf("        options: %s\n", postgresALPNTLSOptionName(project.Ref)))
			if wildcard := wildcardCertDomain(project.Spec.Domain, route.FQDN); wildcard != "" {
				builder.WriteString("        domains:\n")
				builder.WriteString(fmt.Sprintf("          - main: %q\n", wildcard))
			}
		}
		if len(route.IPAllowlist) > 0 {
			builder.WriteString("      middlewares:\n")
			builder.WriteString(fmt.Sprintf("        - %s\n", tcpIPAllowlistMiddlewareName(project.Ref, route.Name)))
		}
	}
	if len(tcpRoutes) > 0 {
		builder.WriteString("  services:\n")
	}
	for _, route := range tcpRoutes {
		serviceName := fmt.Sprintf("%s-%s", project.Ref, route.Name)
		builder.WriteString(fmt.Sprintf("    %s:\n", serviceName))
		builder.WriteString("      loadBalancer:\n")
		builder.WriteString("        servers:\n")
		builder.WriteString(fmt.Sprintf("          - address: \"%s\"\n", route.UpstreamAddress))
	}
	explicitCertificates := explicitRouteCertificates(routes)
	if len(tcpRoutes) > 0 {
		if middlewarePayload := renderTCPMiddlewares(project.Ref, tcpRoutes); middlewarePayload != "" {
			builder.WriteString("  middlewares:\n")
			builder.WriteString(middlewarePayload)
		}
	}
	if len(tcpRoutes) > 0 || len(explicitCertificates) > 0 {
		builder.WriteString("tls:\n")
		if len(explicitCertificates) > 0 {
			builder.WriteString("  certificates:\n")
			for _, cert := range explicitCertificates {
				builder.WriteString("    - certFile: " + quoteYAML(cert.certFile) + "\n")
				builder.WriteString("      keyFile: " + quoteYAML(cert.keyFile) + "\n")
			}
		}
		if len(tcpRoutes) > 0 {
			builder.WriteString("  options:\n")
			builder.WriteString(fmt.Sprintf("    %s:\n", postgresALPNTLSOptionName(project.Ref)))
			builder.WriteString("      alpnProtocols:\n")
			builder.WriteString("        - postgresql\n")
		}
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

type explicitRouteCertificate struct {
	certFile string
	keyFile  string
}

func explicitRouteCertificates(routes []ProjectRoute) []explicitRouteCertificate {
	out := []explicitRouteCertificate{}
	seen := map[string]struct{}{}
	for _, route := range routes {
		certFile := strings.TrimSpace(route.CertFile)
		keyFile := strings.TrimSpace(route.KeyFile)
		if certFile == "" || keyFile == "" {
			continue
		}
		key := certFile + "\x00" + keyFile
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, explicitRouteCertificate{certFile: certFile, keyFile: keyFile})
	}
	return out
}

func quoteYAML(value string) string {
	return fmt.Sprintf("%q", value)
}

func routeTLSCertResolver() string {
	if value, ok := os.LookupEnv("SUPADUPA_TLS_CERT_RESOLVER"); ok {
		return strings.TrimSpace(value)
	}
	return "letsencrypt"
}

func tcpRoutesForProjectFromHTTPRoutes(project Project, routes []ProjectRoute) []ProjectTCPRoute {
	network := ProjectConfig{}
	for _, route := range routes {
		if route.Name == "api" && len(route.IPAllowlist) > 0 {
			network.Config = map[string]string{"db_allowlist": strings.Join(route.IPAllowlist, ",")}
			break
		}
	}
	return TCPRoutesForProjectWithNetworkReplicasAndDatabaseIngress(project, network, nil, nil)
}

func routeMiddlewares(ref string, route ProjectRoute) []string {
	middlewares := make([]string, 0, 2)
	if strings.TrimSpace(route.StripPrefix) != "" {
		middlewares = append(middlewares, stripPrefixMiddlewareName(ref, route.Name))
	}
	if len(route.IPAllowlist) > 0 {
		middlewares = append(middlewares, ipAllowlistMiddlewareName(ref, route.Name))
	}
	if route.SSORequired {
		middlewares = append(middlewares, studioSSOMiddlewareName(ref, route.Name))
	}
	if strings.TrimSpace(route.CacheControl) != "" {
		middlewares = append(middlewares, cdnHeadersMiddlewareName(ref, route.Name))
	}
	return middlewares
}

func routeRule(route ProjectRoute) string {
	rule := fmt.Sprintf("Host(`%s`)", route.FQDN)
	if prefix := strings.TrimSpace(route.PathPrefix); prefix != "" {
		rule += fmt.Sprintf(" && PathPrefix(`%s`)", prefix)
	}
	return rule
}

func tcpRouteRule(route ProjectTCPRoute) string {
	return fmt.Sprintf("HostSNI(`%s`)", route.FQDN)
}

func renderTCPMiddlewares(ref string, routes []ProjectTCPRoute) string {
	var builder strings.Builder
	renderedIPAllowlist := map[string]struct{}{}
	for _, route := range routes {
		if len(route.IPAllowlist) == 0 {
			continue
		}
		name := tcpIPAllowlistMiddlewareName(ref, route.Name)
		if _, ok := renderedIPAllowlist[name]; ok {
			continue
		}
		renderedIPAllowlist[name] = struct{}{}
		builder.WriteString(fmt.Sprintf("    %s:\n", name))
		builder.WriteString("      ipAllowList:\n")
		builder.WriteString("        sourceRange:\n")
		for _, source := range route.IPAllowlist {
			builder.WriteString(fmt.Sprintf("          - \"%s\"\n", source))
		}
	}
	return builder.String()
}

func postgresALPNTLSOptionName(ref string) string {
	return ref + "-postgres-alpn"
}

func wildcardCertDomain(baseDomain string, fqdn string) string {
	baseDomain = strings.TrimSpace(baseDomain)
	fqdn = strings.TrimSpace(fqdn)
	if baseDomain == "" || fqdn == "" {
		return ""
	}
	if fqdn == baseDomain {
		return ""
	}
	if fqdn == "*."+baseDomain || strings.HasSuffix(fqdn, "."+baseDomain) {
		return "*." + baseDomain
	}
	return ""
}

func renderMiddlewares(ref string, routes []ProjectRoute) string {
	var builder strings.Builder
	renderedStripPrefix := map[string]struct{}{}
	renderedIPAllowlist := map[string]struct{}{}
	renderedCDNHeaders := map[string]struct{}{}
	renderedStudioSSO := map[string]struct{}{}
	renderedRedirect := map[string]struct{}{}
	for _, route := range routes {
		if strings.TrimSpace(route.StripPrefix) != "" {
			name := stripPrefixMiddlewareName(ref, route.Name)
			if _, ok := renderedStripPrefix[name]; !ok {
				renderedStripPrefix[name] = struct{}{}
				builder.WriteString(fmt.Sprintf("    %s:\n", name))
				builder.WriteString("      stripPrefix:\n")
				builder.WriteString("        prefixes:\n")
				builder.WriteString(fmt.Sprintf("          - %q\n", route.StripPrefix))
			}
		}
		if len(route.IPAllowlist) > 0 {
			name := ipAllowlistMiddlewareName(ref, route.Name)
			if _, ok := renderedIPAllowlist[name]; !ok {
				renderedIPAllowlist[name] = struct{}{}
				builder.WriteString(fmt.Sprintf("    %s:\n", name))
				builder.WriteString("      ipAllowList:\n")
				builder.WriteString("        sourceRange:\n")
				for _, source := range route.IPAllowlist {
					builder.WriteString(fmt.Sprintf("          - \"%s\"\n", source))
				}
			}
		}
		if strings.TrimSpace(route.CacheControl) != "" {
			name := cdnHeadersMiddlewareName(ref, route.Name)
			if _, ok := renderedCDNHeaders[name]; !ok {
				renderedCDNHeaders[name] = struct{}{}
				builder.WriteString(fmt.Sprintf("    %s:\n", name))
				builder.WriteString("      headers:\n")
				builder.WriteString("        customResponseHeaders:\n")
				builder.WriteString(fmt.Sprintf("          Cache-Control: %q\n", route.CacheControl))
				builder.WriteString("          X-Supadupa-CDN: \"enabled\"\n")
				if route.SmartCDN {
					builder.WriteString("          X-Supadupa-CDN-Smart: \"enabled\"\n")
				}
			}
		}
		if route.SSORequired {
			name := studioSSOMiddlewareName(ref, route.Name)
			if _, ok := renderedStudioSSO[name]; !ok {
				renderedStudioSSO[name] = struct{}{}
				builder.WriteString(fmt.Sprintf("    %s:\n", name))
				builder.WriteString("      forwardAuth:\n")
				builder.WriteString(fmt.Sprintf("        address: %q\n", studioForwardAuthURL(ref)))
				builder.WriteString("        trustForwardHeader: false\n")
				builder.WriteString("        authResponseHeaders:\n")
				builder.WriteString("          - X-Supadupa-User\n")
				builder.WriteString("          - X-Supadupa-Project\n")
				builder.WriteString("        addAuthCookiesToResponse:\n")
				builder.WriteString("          - supadupa_session\n")
			}
		}
		if route.SSLEnforced {
			name := redirectMiddlewareName(ref, route.Name)
			if _, ok := renderedRedirect[name]; !ok {
				renderedRedirect[name] = struct{}{}
				builder.WriteString(fmt.Sprintf("    %s:\n", name))
				builder.WriteString("      redirectScheme:\n")
				builder.WriteString("        scheme: https\n")
				builder.WriteString("        permanent: true\n")
			}
		}
	}
	return builder.String()
}

func ipAllowlistMiddlewareName(ref string, routeName string) string {
	return fmt.Sprintf("%s-%s-ipallowlist", ref, routeName)
}

func tcpIPAllowlistMiddlewareName(ref string, routeName string) string {
	return fmt.Sprintf("%s-%s-tcp-ipallowlist", ref, routeName)
}

func stripPrefixMiddlewareName(ref string, routeName string) string {
	return fmt.Sprintf("%s-%s-stripprefix", ref, routeName)
}

func redirectMiddlewareName(ref string, routeName string) string {
	return fmt.Sprintf("%s-%s-redirect-https", ref, routeName)
}

func cdnHeadersMiddlewareName(ref string, routeName string) string {
	return fmt.Sprintf("%s-%s-cdn-headers", ref, routeName)
}

func studioSSOMiddlewareName(ref string, routeName string) string {
	return fmt.Sprintf("%s-%s-supadupa-sso", ref, routeName)
}

func studioForwardAuthURL(ref ...string) string {
	if configured := strings.TrimSpace(os.Getenv("SUPADUPA_STUDIO_FORWARD_AUTH_URL")); configured != "" {
		if len(ref) > 0 && strings.TrimSpace(ref[0]) != "" && !strings.Contains(configured, "project_ref=") {
			separator := "?"
			if strings.Contains(configured, "?") {
				separator = "&"
			}
			return configured + separator + "project_ref=" + strings.TrimSpace(ref[0])
		}
		return configured
	}
	base := "http://supadupavisor:8080/v1/auth/studio/verify"
	if len(ref) > 0 && strings.TrimSpace(ref[0]) != "" {
		return base + "?project_ref=" + strings.TrimSpace(ref[0])
	}
	return base
}

func (s *RoutingService) RemoveProject(ref string) error {
	err := os.Remove(filepath.Join(s.rootDir, ref+".yaml"))
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}
