package control

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"supadupa2026/internal/provisioner/artifact"
)

// EnsurePlatformRouteFile rewrites runtime/routes/00-platform.yaml when missing
// (plan C6). Uses SUPADUPA_API_HOST and SUPADUPA_ADMIN_HOST; no-op when either
// host is empty so local unit tests without edge config stay quiet.
func EnsurePlatformRouteFile(routesRoot string, getenv func(string) string) (bool, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if routesRoot == "" {
		routesRoot = strings.TrimSpace(getenv("SUPADUPA_ROUTES_ROOT"))
	}
	if routesRoot == "" {
		routesRoot = "./runtime/routes"
	}
	apiHost := strings.TrimSpace(getenv("SUPADUPA_API_HOST"))
	adminHost := strings.TrimSpace(getenv("SUPADUPA_ADMIN_HOST"))
	if apiHost == "" || adminHost == "" {
		return false, nil
	}
	path := filepath.Join(routesRoot, "00-platform.yaml")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(routesRoot, 0o755); err != nil {
		return false, err
	}
	payload := renderPlatformRouteYAML(apiHost, adminHost)
	if err := artifact.WriteFile(path, []byte(payload), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func renderPlatformRouteYAML(apiHost, adminHost string) string {
	// Traefik v2/v3 Host rules use backticks: Host(`example.com`).
	return fmt.Sprintf(`http:
  routers:
    supadupa-api:
      rule: Host(`+"`%s`"+`)
      entryPoints:
        - websecure
      service: supadupa-api
      tls: {}
    supadupa-api-http:
      rule: Host(`+"`%s`"+`)
      entryPoints:
        - web
      middlewares:
        - supadupa-api-https
      service: supadupa-api
    supadupa-admin:
      rule: Host(`+"`%s`"+`)
      entryPoints:
        - websecure
      service: supadupa-admin
      tls: {}
    supadupa-admin-http:
      rule: Host(`+"`%s`"+`)
      entryPoints:
        - web
      middlewares:
        - supadupa-admin-https
      service: supadupa-admin
  middlewares:
    supadupa-api-https:
      redirectScheme:
        scheme: https
        permanent: true
    supadupa-admin-https:
      redirectScheme:
        scheme: https
        permanent: true
  services:
    supadupa-api:
      loadBalancer:
        servers:
          - url: "http://supadupavisor:8080"
    supadupa-admin:
      loadBalancer:
        servers:
          - url: "http://supadupa-admin:80"
`, apiHost, apiHost, adminHost, adminHost)
}
