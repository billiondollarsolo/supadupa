package control

import (
	"strings"
	"testing"
)

func TestSupportedStackReleaseVersionsUseBuiltins(t *testing.T) {
	versions := SupportedStackReleaseVersionsFromEnv(func(string) string { return "" })
	expected := []string{"15.8.1.085", "15.8.1.060", "15.8.1.054", "15.8.1.049"}
	if len(versions) != len(expected) {
		t.Fatalf("expected built-in stable versions, got %#v", versions)
	}
	for index, version := range expected {
		if versions[index] != version {
			t.Fatalf("expected built-in stable versions %#v, got %#v", expected, versions)
		}
	}
	for _, version := range expected {
		manifest, ok := ResolveStackReleaseManifestFromEnv(func(string) string { return "" }, version)
		if !ok {
			t.Fatalf("expected built-in manifest for %s", version)
		}
		if missing := missingStackReleaseTags(manifest); len(missing) > 0 {
			t.Fatalf("built-in manifest %s is missing tags: %#v", version, missing)
		}
	}
}

func TestConfiguredStackReleaseCatalogCoversSeveralCompleteStableVersions(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "SUPADUPA_SUPPORTED_STACK_VERSIONS":
			return "2026.06.08,2026.06.07,2026.06.06"
		case "SUPADUPA_STACK_RELEASES_JSON":
			return `[
				{"version":"2026.06.08","postgres":"pg-08","kong":"kong-08","studio":"studio-08","postgres_meta":"meta-08","auth":"auth-08","rest":"rest-08","realtime":"realtime-08","storage":"storage-08","imgproxy":"imgproxy-08","edge_runtime":"edge-08","pooler":"pooler-08","analytics":"analytics-08","vector":"vector-08"},
				{"version":"2026.06.07","postgres":"pg-07","kong":"kong-07","studio":"studio-07","postgres_meta":"meta-07","auth":"auth-07","rest":"rest-07","realtime":"realtime-07","storage":"storage-07","imgproxy":"imgproxy-07","edge_runtime":"edge-07","pooler":"pooler-07","analytics":"analytics-07","vector":"vector-07"},
				{"version":"2026.06.06","postgres":"pg-06","kong":"kong-06","studio":"studio-06","postgres_meta":"meta-06","auth":"auth-06","rest":"rest-06","realtime":"realtime-06","storage":"storage-06","imgproxy":"imgproxy-06","edge_runtime":"edge-06","pooler":"pooler-06","analytics":"analytics-06","vector":"vector-06"}
			]`
		default:
			return ""
		}
	}

	versions := SupportedStackReleaseVersionsFromEnv(getenv)
	if len(versions) < 3 {
		t.Fatalf("expected at least three configured stable stack releases for upgrade validation, got %#v", versions)
	}
	if versions[0] != "2026.06.08" || versions[1] != "2026.06.07" || versions[2] != "2026.06.06" {
		t.Fatalf("expected configured releases to sort newest first, got %#v", versions)
	}
	for _, version := range versions {
		manifest, ok := ResolveStackReleaseManifestFromEnv(getenv, version)
		if !ok {
			t.Fatalf("expected configured manifest for %s", version)
		}
		if missing := missingStackReleaseTags(manifest); len(missing) > 0 {
			t.Fatalf("configured manifest %s is missing tags: %#v", version, missing)
		}
	}
}

func TestStackReleaseManifestOverridesEveryServiceTag(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "SUPADUPA_STACK_RELEASES_JSON":
			return `{
				"2026.06.06": {
					"postgres": "pg-tag",
					"kong": "kong-tag",
					"studio": "studio-tag",
					"postgres_meta": "meta-tag",
					"auth": "auth-tag",
					"rest": "rest-tag",
					"realtime": "realtime-tag",
					"storage": "storage-tag",
					"imgproxy": "imgproxy-tag",
					"edge_runtime": "edge-tag",
					"pooler": "pooler-tag",
					"analytics": "analytics-tag",
					"vector": "vector-tag"
				}
			}`
		case "SUPADUPA_SUPPORTED_STACK_VERSIONS":
			return "2026.06.06"
		default:
			return ""
		}
	}

	versions := SupportedStackReleaseVersionsFromEnv(getenv)
	if len(versions) != 1 || versions[0] != "2026.06.06" {
		t.Fatalf("expected configured version, got %#v", versions)
	}
	manifest, ok := ResolveStackReleaseManifestFromEnv(getenv, "2026.06.06")
	if !ok {
		t.Fatal("expected configured manifest")
	}
	if manifest.Postgres != "pg-tag" ||
		manifest.Kong != "kong-tag" ||
		manifest.Studio != "studio-tag" ||
		manifest.PostgresMeta != "meta-tag" ||
		manifest.Auth != "auth-tag" ||
		manifest.REST != "rest-tag" ||
		manifest.Realtime != "realtime-tag" ||
		manifest.Storage != "storage-tag" ||
		manifest.Imgproxy != "imgproxy-tag" ||
		manifest.EdgeRuntime != "edge-tag" ||
		manifest.Pooler != "pooler-tag" ||
		manifest.Analytics != "analytics-tag" ||
		manifest.Vector != "vector-tag" {
		t.Fatalf("manifest did not preserve service tags: %#v", manifest)
	}
}

func missingStackReleaseTags(manifest StackReleaseManifest) []string {
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
		if value == "" {
			missing = append(missing, field)
		}
	}
	return missing
}

func TestConfiguredStackVersionWithoutManifestIsNotSupported(t *testing.T) {
	getenv := func(key string) string {
		if key == "SUPADUPA_SUPPORTED_STACK_VERSIONS" {
			return "2026.06.07"
		}
		return ""
	}

	versions := SupportedStackReleaseVersionsFromEnv(getenv)
	if len(versions) != 0 {
		t.Fatalf("expected unknown configured version to be unsupported, got %#v", versions)
	}
	_, ok := ResolveStackReleaseManifestFromEnv(getenv, "2026.06.07")
	if ok {
		t.Fatal("expected unknown configured version without manifest to be unsupported")
	}
}

func TestConfiguredStackVersionWithPartialNewManifestIsNotSupported(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "SUPADUPA_SUPPORTED_STACK_VERSIONS":
			return "2026.06.07"
		case "SUPADUPA_STACK_RELEASES_JSON":
			return `[{"version":"2026.06.07","postgres":"15.8.1.087","auth":"v2.190.0"}]`
		default:
			return ""
		}
	}

	_, ok := ResolveStackReleaseManifestFromEnv(getenv, "2026.06.07")
	if ok {
		t.Fatal("expected partial new manifest to be unsupported")
	}
	versions := SupportedStackReleaseVersionsFromEnv(getenv)
	if len(versions) != 0 {
		t.Fatalf("expected partial new manifest to be hidden, got %#v", versions)
	}
}

func TestConfiguredStackVersionWithCompleteManifestIsSupported(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "SUPADUPA_SUPPORTED_STACK_VERSIONS":
			return "2026.06.07"
		case "SUPADUPA_STACK_RELEASES_JSON":
			return `[{"version":"2026.06.07","postgres":"15.8.1.087","kong":"kong-07","studio":"studio-07","postgres_meta":"meta-07","auth":"v2.190.0","rest":"rest-07","realtime":"realtime-07","storage":"storage-07","imgproxy":"imgproxy-07","edge_runtime":"edge-07","pooler":"pooler-07","analytics":"analytics-07","vector":"vector-07"}]`
		default:
			return ""
		}
	}

	manifest, ok := ResolveStackReleaseManifestFromEnv(getenv, "2026.06.07")
	if !ok {
		t.Fatal("expected configured version with complete manifest")
	}
	if manifest.Version != "2026.06.07" || manifest.Postgres != "15.8.1.087" || manifest.Auth != "v2.190.0" {
		t.Fatalf("expected configured manifest tags, got %#v", manifest)
	}
	if missing := missingStackReleaseTags(manifest); len(missing) > 0 {
		t.Fatalf("expected complete manifest, missing %#v", missing)
	}
}

func TestConfiguredBuiltinStackReleaseCanBePartiallyOverridden(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "SUPADUPA_SUPPORTED_STACK_VERSIONS":
			return "15.8.1.085"
		case "SUPADUPA_STACK_RELEASES_JSON":
			return `[{"version":"15.8.1.085","auth":"v2.190.0"}]`
		default:
			return ""
		}
	}

	manifest, ok := ResolveStackReleaseManifestFromEnv(getenv, "15.8.1.085")
	if !ok {
		t.Fatal("expected built-in version override")
	}
	if manifest.Auth != "v2.190.0" {
		t.Fatalf("expected auth override, got %#v", manifest)
	}
	if manifest.Storage == "" || manifest.Pooler == "" || manifest.Postgres != "15.8.1.085" {
		t.Fatalf("expected built-in fallback service tags, got %#v", manifest)
	}
}

func TestResolveStackReleaseManifestWithFallbackUsesRequestedVersion(t *testing.T) {
	manifest, err := ResolveStackReleaseManifestWithFallbackFromEnv(func(string) string { return "" }, "15.8.1.060")
	if err != nil {
		t.Fatalf("expected resolve success: %v", err)
	}
	if manifest.Version != "15.8.1.060" || manifest.Postgres != "15.8.1.060" {
		t.Fatalf("expected requested version manifest, got %#v", manifest)
	}
}

func TestResolveStackReleaseManifestWithFallbackUsesDefaultWhenMissing(t *testing.T) {
	manifest, err := ResolveStackReleaseManifestWithFallbackFromEnv(func(string) string { return "" }, "not-a-real-version")
	if err != nil {
		t.Fatalf("expected default fallback success: %v", err)
	}
	if manifest.Version != DefaultStackReleaseVersion {
		t.Fatalf("expected default %s, got %#v", DefaultStackReleaseVersion, manifest)
	}
}

func TestResolveStackReleaseManifestWithFallbackErrorsWhenCatalogEmpty(t *testing.T) {
	// Filter the catalog to only an unknown version so neither requested nor default resolve.
	getenv := func(key string) string {
		if key == "SUPADUPA_SUPPORTED_STACK_VERSIONS" {
			return "does-not-exist-in-catalog"
		}
		return ""
	}
	_, err := ResolveStackReleaseManifestWithFallbackFromEnv(getenv, "15.8.1.060")
	if err == nil {
		t.Fatal("expected error when active catalog cannot resolve requested or default version")
	}
	if !strings.Contains(err.Error(), "not available in the active catalog") {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = ResolveStackReleaseManifestWithFallbackFromEnv(getenv, DefaultStackReleaseVersion)
	if err == nil {
		t.Fatal("expected error when default is also filtered out of the catalog")
	}
}
