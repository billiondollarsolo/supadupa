package control

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const DefaultStackReleaseVersion = "15.8.1.085"

type StackReleaseManifest struct {
	Version      string `json:"version"`
	Postgres     string `json:"postgres"`
	Kong         string `json:"kong"`
	Studio       string `json:"studio"`
	PostgresMeta string `json:"postgres_meta"`
	Auth         string `json:"auth"`
	REST         string `json:"rest"`
	Realtime     string `json:"realtime"`
	Storage      string `json:"storage"`
	Imgproxy     string `json:"imgproxy"`
	EdgeRuntime  string `json:"edge_runtime"`
	Pooler       string `json:"pooler"`
	Analytics    string `json:"analytics"`
	Vector       string `json:"vector"`
}

var builtinStackReleaseManifests = map[string]StackReleaseManifest{
	"15.8.1.085": {
		Version:      "15.8.1.085",
		Postgres:     "15.8.1.085",
		Kong:         "3.9.1",
		Studio:       "2026.06.03-sha-0bca601",
		PostgresMeta: "v0.96.6",
		Auth:         "v2.189.0",
		REST:         "v14.12",
		Realtime:     "v2.102.3",
		Storage:      "v1.60.4",
		Imgproxy:     "v3.30.1",
		EdgeRuntime:  "v1.74.0",
		Pooler:       "2.9.5",
		Analytics:    "1.43.1",
		Vector:       "0.53.0-alpine",
	},
	"15.8.1.060": {
		Version:      "15.8.1.060",
		Postgres:     "15.8.1.060",
		Kong:         "3.9.1",
		Studio:       "2026.06.03-sha-0bca601",
		PostgresMeta: "v0.96.6",
		Auth:         "v2.189.0",
		REST:         "v14.12",
		Realtime:     "v2.102.3",
		Storage:      "v1.60.4",
		Imgproxy:     "v3.30.1",
		EdgeRuntime:  "v1.74.0",
		Pooler:       "2.9.5",
		Analytics:    "1.43.1",
		Vector:       "0.53.0-alpine",
	},
	"15.8.1.054": {
		Version:      "15.8.1.054",
		Postgres:     "15.8.1.054",
		Kong:         "3.9.1",
		Studio:       "2026.06.03-sha-0bca601",
		PostgresMeta: "v0.96.6",
		Auth:         "v2.189.0",
		REST:         "v14.12",
		Realtime:     "v2.102.3",
		Storage:      "v1.60.4",
		Imgproxy:     "v3.30.1",
		EdgeRuntime:  "v1.74.0",
		Pooler:       "2.9.5",
		Analytics:    "1.43.1",
		Vector:       "0.53.0-alpine",
	},
	"15.8.1.049": {
		Version:      "15.8.1.049",
		Postgres:     "15.8.1.049",
		Kong:         "3.9.1",
		Studio:       "2026.06.03-sha-0bca601",
		PostgresMeta: "v0.96.6",
		Auth:         "v2.189.0",
		REST:         "v14.12",
		Realtime:     "v2.102.3",
		Storage:      "v1.60.4",
		Imgproxy:     "v3.30.1",
		EdgeRuntime:  "v1.74.0",
		Pooler:       "2.9.5",
		Analytics:    "1.43.1",
		Vector:       "0.53.0-alpine",
	},
}

func SupportedStackReleaseVersionsFromEnv(getenv func(string) string) []string {
	manifests := stackReleaseManifestsFromEnv(getenv)
	versions := make([]string, 0, len(manifests))
	for version := range manifests {
		versions = append(versions, version)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(versions)))
	return versions
}

func ResolveStackReleaseManifestFromEnv(getenv func(string) string, version string) (StackReleaseManifest, bool) {
	normalized := NormalizeStackReleaseVersion(version)
	manifest, ok := stackReleaseManifestsFromEnv(getenv)[normalized]
	if ok {
		return manifest, true
	}
	if normalized == "" {
		manifest, ok = stackReleaseManifestsFromEnv(getenv)[DefaultStackReleaseVersion]
		return manifest, ok
	}
	return StackReleaseManifest{}, false
}

func NormalizeStackReleaseVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || strings.EqualFold(version, "latest") {
		return DefaultStackReleaseVersion
	}
	return version
}

func stackReleaseManifestsFromEnv(getenv func(string) string) map[string]StackReleaseManifest {
	if getenv == nil {
		getenv = os.Getenv
	}
	manifests := cloneStackReleaseManifestMap(builtinStackReleaseManifests)
	if payload := strings.TrimSpace(getenv("SUPADUPA_STACK_RELEASES_JSON")); payload != "" {
		overrides, err := parseStackReleaseManifestJSON(payload)
		if err == nil {
			for version, manifest := range overrides {
				version = NormalizeStackReleaseVersion(version)
				if builtin, ok := manifests[version]; ok {
					manifests[version] = mergeStackReleaseManifestDefaults(builtin, manifest)
					continue
				}
				if missing := missingStackReleaseManifestFields(manifest); len(missing) == 0 {
					manifests[version] = manifest
				}
			}
		}
	}
	if configured := strings.TrimSpace(getenv("SUPADUPA_SUPPORTED_STACK_VERSIONS")); configured != "" {
		filtered := make(map[string]StackReleaseManifest)
		for _, version := range splitConfiguredStackVersions(configured) {
			if manifest, ok := manifests[version]; ok {
				filtered[version] = manifest
			}
		}
		return filtered
	}
	return manifests
}

func parseStackReleaseManifestJSON(payload string) (map[string]StackReleaseManifest, error) {
	var list []StackReleaseManifest
	if err := json.Unmarshal([]byte(payload), &list); err == nil {
		return stackReleaseListToMap(list)
	}
	var keyed map[string]StackReleaseManifest
	if err := json.Unmarshal([]byte(payload), &keyed); err != nil {
		return nil, err
	}
	for version, manifest := range keyed {
		if strings.TrimSpace(manifest.Version) == "" {
			manifest.Version = version
		}
		normalized := NormalizeStackReleaseVersion(version)
		manifest.Version = NormalizeStackReleaseVersion(manifest.Version)
		keyed[normalized] = manifest
		if normalized != version {
			delete(keyed, version)
		}
	}
	return keyed, nil
}

func stackReleaseListToMap(list []StackReleaseManifest) (map[string]StackReleaseManifest, error) {
	out := make(map[string]StackReleaseManifest, len(list))
	for _, manifest := range list {
		version := strings.TrimSpace(manifest.Version)
		if version == "" {
			return nil, fmt.Errorf("stack release manifest version is required")
		}
		version = NormalizeStackReleaseVersion(version)
		manifest.Version = version
		out[version] = manifest
	}
	return out, nil
}

func mergeStackReleaseManifestDefaults(defaults StackReleaseManifest, manifest StackReleaseManifest) StackReleaseManifest {
	if manifest.Version == "" {
		manifest.Version = defaults.Version
	}
	if manifest.Postgres == "" {
		manifest.Postgres = manifest.Version
	}
	if manifest.Kong == "" {
		manifest.Kong = defaults.Kong
	}
	if manifest.Studio == "" {
		manifest.Studio = defaults.Studio
	}
	if manifest.PostgresMeta == "" {
		manifest.PostgresMeta = defaults.PostgresMeta
	}
	if manifest.Auth == "" {
		manifest.Auth = defaults.Auth
	}
	if manifest.REST == "" {
		manifest.REST = defaults.REST
	}
	if manifest.Realtime == "" {
		manifest.Realtime = defaults.Realtime
	}
	if manifest.Storage == "" {
		manifest.Storage = defaults.Storage
	}
	if manifest.Imgproxy == "" {
		manifest.Imgproxy = defaults.Imgproxy
	}
	if manifest.EdgeRuntime == "" {
		manifest.EdgeRuntime = defaults.EdgeRuntime
	}
	if manifest.Pooler == "" {
		manifest.Pooler = defaults.Pooler
	}
	if manifest.Analytics == "" {
		manifest.Analytics = defaults.Analytics
	}
	if manifest.Vector == "" {
		manifest.Vector = defaults.Vector
	}
	return manifest
}

func splitConfiguredStackVersions(configured string) []string {
	versions := make([]string, 0)
	for _, part := range strings.FieldsFunc(configured, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t'
	}) {
		version := NormalizeStackReleaseVersion(part)
		if version != "" {
			versions = append(versions, version)
		}
	}
	return versions
}

func missingStackReleaseManifestFields(manifest StackReleaseManifest) []string {
	missing := make([]string, 0)
	fields := map[string]string{
		"version":       manifest.Version,
		"postgres":      manifest.Postgres,
		"kong":          manifest.Kong,
		"studio":        manifest.Studio,
		"postgres_meta": manifest.PostgresMeta,
		"auth":          manifest.Auth,
		"rest":          manifest.REST,
		"realtime":      manifest.Realtime,
		"storage":       manifest.Storage,
		"imgproxy":      manifest.Imgproxy,
		"edge_runtime":  manifest.EdgeRuntime,
		"pooler":        manifest.Pooler,
		"analytics":     manifest.Analytics,
		"vector":        manifest.Vector,
	}
	for field, value := range fields {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, field)
		}
	}
	sort.Strings(missing)
	return missing
}

func cloneStackReleaseManifestMap(input map[string]StackReleaseManifest) map[string]StackReleaseManifest {
	out := make(map[string]StackReleaseManifest, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
