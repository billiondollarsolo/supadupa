package terraform

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPreserveMaskedSensitiveValue(t *testing.T) {
	if got := preserveMaskedSensitiveValue("********", "secret://projects/alpha/token"); got != "secret://projects/alpha/token" {
		t.Fatalf("expected previous secret, got %q", got)
	}
	if got := preserveMaskedSensitiveValue("********", "   "); got != "********" {
		t.Fatalf("expected mask to remain without previous secret, got %q", got)
	}
	if got := preserveMaskedSensitiveValue("secret://new", "secret://old"); got != "secret://new" {
		t.Fatalf("expected remote value, got %q", got)
	}
}

func TestPreviousSensitiveString(t *testing.T) {
	if got := previousSensitiveString(types.StringNull()); got != "" {
		t.Fatalf("expected empty null value, got %q", got)
	}
	if got := previousSensitiveString(types.StringUnknown()); got != "" {
		t.Fatalf("expected empty unknown value, got %q", got)
	}
	if got := previousSensitiveString(types.StringValue("secret://projects/alpha/token")); got != "secret://projects/alpha/token" {
		t.Fatalf("expected prior value, got %q", got)
	}
}
