package main

import (
	"testing"

	supadupatf "supadupa2026/internal/terraform"
)

// TestProviderFactoryMatchesMain ensures main()'s NewProvider(version) wiring
// returns a usable provider factory (same call site as providerserver.Serve).
func TestProviderFactoryMatchesMain(t *testing.T) {
	factory := supadupatf.NewProvider(version)
	if factory == nil {
		t.Fatal("NewProvider returned nil factory")
	}
	p := factory()
	if p == nil {
		t.Fatal("provider factory returned nil provider")
	}
}

// TestProviderVersionDefault matches the package-level version main serves.
func TestProviderVersionDefault(t *testing.T) {
	if version == "" {
		t.Fatal("expected non-empty provider version default")
	}
}
