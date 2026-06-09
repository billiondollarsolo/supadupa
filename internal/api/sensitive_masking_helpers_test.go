package api

import "testing"

func TestMaskSensitiveStringMapMasksOnlySensitiveNonEmptyValues(t *testing.T) {
	input := map[string]string{
		"token":       "secret-token",
		"password":    "",
		"access_key":  "access",
		"description": "public",
	}
	masked := maskSensitiveStringMap(input, isSensitiveMetadataKey)

	if masked["token"] != maskedSensitiveValue {
		t.Fatalf("expected token masked, got %#v", masked)
	}
	if masked["access_key"] != maskedSensitiveValue {
		t.Fatalf("expected access_key masked, got %#v", masked)
	}
	if masked["password"] != "" {
		t.Fatalf("expected empty sensitive values preserved, got %#v", masked)
	}
	if masked["description"] != "public" {
		t.Fatalf("expected non-sensitive values preserved, got %#v", masked)
	}
}

func TestMaskSensitiveStringMapPreservesEmptyMapShape(t *testing.T) {
	masked := maskSensitiveStringMap(nil, isSensitiveMetadataKey)
	if masked == nil {
		t.Fatal("expected nil input to return an empty map, not nil")
	}
	if len(masked) != 0 {
		t.Fatalf("expected empty map, got %#v", masked)
	}
}

func TestSensitiveAuthHookHeaderKeyIncludesStrictHeaderNames(t *testing.T) {
	for _, key := range []string{"authorization", "proxy-authorization", "x-api-key", "x-auth-token", "secret_key"} {
		if !isSensitiveAuthHookHeaderKey(key) {
			t.Fatalf("expected %q to be sensitive", key)
		}
	}
	if isSensitiveAuthHookHeaderKey("x-trace-id") {
		t.Fatal("did not expect x-trace-id to be sensitive")
	}
}
