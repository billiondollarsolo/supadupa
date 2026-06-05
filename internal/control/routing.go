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
	baseHost := fmt.Sprintf("%s.%s", project.Ref, project.Spec.Domain)
	policy := networkPolicyFromConfig(network)
	cache := routeCachePolicyFromCDN(cdn)
	routes := []ProjectRoute{
		{
			Name:         "api",
			FQDN:         baseHost,
			UpstreamURL:  fmt.Sprintf("http://%s-kong:8000", project.Ref),
			TLS:          true,
			SSLEnforced:  policy.SSLEnforced,
			IPAllowlist:  policy.IPAllowlist,
			CacheControl: cache.CacheControl,
			SmartCDN:     cache.SmartCDN,
		},
		{
			Name:        "studio",
			FQDN:        "studio." + baseHost,
			UpstreamURL: fmt.Sprintf("http://%s-studio:3000", project.Ref),
			TLS:         true,
			SSLEnforced: policy.SSLEnforced,
			IPAllowlist: policy.IPAllowlist,
		},
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
			routes = append(routes, ProjectRoute{
				Name:        "studio-local",
				FQDN:        localHost,
				PathPrefix:  localStudioPath(project.Ref),
				StripPrefix: localStudioPath(project.Ref),
				UpstreamURL: fmt.Sprintf("http://%s-studio:3000", project.Ref),
				TLS:         false,
			})
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
		routes = append(routes, ProjectRoute{
			Name:         "custom-" + routeName(domain.FQDN),
			FQDN:         domain.FQDN,
			UpstreamURL:  fmt.Sprintf("http://%s-kong:8000", project.Ref),
			TLS:          true,
			SSLEnforced:  policy.SSLEnforced,
			IPAllowlist:  policy.IPAllowlist,
			CacheControl: cache.CacheControl,
			SmartCDN:     cache.SmartCDN,
		})
	}
	return routes
}

type routeCachePolicy struct {
	CacheControl string
	SmartCDN     bool
}

type routeNetworkPolicy struct {
	SSLEnforced bool
	IPAllowlist []string
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
	policy.IPAllowlist = splitAllowlist(config.Config["ip_allowlist"])
	return policy
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

func (s *RoutingService) RenderProject(project Project, routes []ProjectRoute) (string, error) {
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
			builder.WriteString("      tls:\n")
			builder.WriteString("        certResolver: letsencrypt\n")
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
			for _, middleware := range append(middlewares, redirectMiddlewareName(project.Ref, route.Name)) {
				builder.WriteString(fmt.Sprintf("        - %s\n", middleware))
			}
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
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func routeMiddlewares(ref string, route ProjectRoute) []string {
	middlewares := make([]string, 0, 2)
	if strings.TrimSpace(route.StripPrefix) != "" {
		middlewares = append(middlewares, stripPrefixMiddlewareName(ref, route.Name))
	}
	if len(route.IPAllowlist) > 0 {
		middlewares = append(middlewares, ipAllowlistMiddlewareName(ref, route.Name))
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

func renderMiddlewares(ref string, routes []ProjectRoute) string {
	var builder strings.Builder
	renderedStripPrefix := map[string]struct{}{}
	renderedIPAllowlist := map[string]struct{}{}
	renderedCDNHeaders := map[string]struct{}{}
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

func stripPrefixMiddlewareName(ref string, routeName string) string {
	return fmt.Sprintf("%s-%s-stripprefix", ref, routeName)
}

func redirectMiddlewareName(ref string, routeName string) string {
	return fmt.Sprintf("%s-%s-redirect-https", ref, routeName)
}

func cdnHeadersMiddlewareName(ref string, routeName string) string {
	return fmt.Sprintf("%s-%s-cdn-headers", ref, routeName)
}

func (s *RoutingService) RemoveProject(ref string) error {
	err := os.Remove(filepath.Join(s.rootDir, ref+".yaml"))
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}
