package control

import (
	"strings"
	"testing"
)

func TestPinImageAppendsDigest(t *testing.T) {
	got := PinImage("kong/kong", "3.9.1", "sha256:abc")
	if got != "kong/kong:3.9.1@sha256:abc" {
		t.Fatalf("got %q", got)
	}
	got = PinImage("kong/kong", "3.9.1", "abc")
	if got != "kong/kong:3.9.1@sha256:abc" {
		t.Fatalf("expected sha256 prefix, got %q", got)
	}
	got = PinImage("kong/kong", "3.9.1", "")
	if got != "kong/kong:3.9.1" {
		t.Fatalf("empty digest should leave tag only, got %q", got)
	}
}

func TestBuiltinStackReleaseManifestsDigestPinImages(t *testing.T) {
	manifest, ok := ResolveStackReleaseManifestFromEnv(func(string) string { return "" }, DefaultStackReleaseVersion)
	if !ok {
		t.Fatal("default release missing")
	}
	if len(manifest.Digests) == 0 {
		t.Fatal("default release must pin digests (plan B3)")
	}
	required := []string{"postgres", "kong", "auth", "rest", "storage", "vector"}
	for _, key := range required {
		if !strings.HasPrefix(manifest.Digests[key], "sha256:") {
			t.Fatalf("missing digest for %s: %#v", key, manifest.Digests[key])
		}
	}
	ref := manifest.ImageRef("kong", "kong/kong", manifest.Kong)
	if !strings.Contains(ref, "@sha256:") {
		t.Fatalf("expected pinned kong image, got %q", ref)
	}
	if !strings.HasPrefix(ref, "kong/kong:"+manifest.Kong+"@") {
		t.Fatalf("unexpected ref shape %q", ref)
	}
}
