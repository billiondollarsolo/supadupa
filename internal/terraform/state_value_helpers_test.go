package terraform

import (
	"context"
	"testing"
	"time"
)

func TestOptionalStringValue(t *testing.T) {
	if !optionalStringValue("").IsNull() {
		t.Fatal("expected empty string to become null")
	}
	if !optionalStringValue("   ").IsNull() {
		t.Fatal("expected whitespace string to become null")
	}
	if got := optionalStringValue("value"); got.ValueString() != "value" {
		t.Fatalf("expected value, got %q", got.ValueString())
	}
}

func TestOptionalTimeString(t *testing.T) {
	if got := optionalTimeString(time.Time{}); got.ValueString() != "" {
		t.Fatalf("expected zero time to become empty string, got %q", got.ValueString())
	}
	value := time.Date(2026, 6, 8, 12, 34, 56, 0, time.UTC)
	if got := optionalTimeString(value); got.ValueString() != "2026-06-08T12:34:56Z" {
		t.Fatalf("unexpected formatted time %q", got.ValueString())
	}
}

func TestOptionalTimePointerString(t *testing.T) {
	if got := optionalTimePointerString(nil); got.ValueString() != "" {
		t.Fatalf("expected nil time pointer to become empty string, got %q", got.ValueString())
	}
	value := time.Date(2026, 6, 8, 12, 34, 56, 0, time.UTC)
	if got := optionalTimePointerString(&value); got.ValueString() != "2026-06-08T12:34:56Z" {
		t.Fatalf("unexpected formatted pointer time %q", got.ValueString())
	}
}

func TestStringListStateValue(t *testing.T) {
	var errors []string
	value, ok := stringListStateValue(context.Background(), "cidrs", []string{"10.0.0.0/8", "192.0.2.10"}, func(summary string, detail string) {
		errors = append(errors, summary+": "+detail)
	})
	if !ok || len(errors) != 0 {
		t.Fatalf("expected successful list encoding, ok=%v errors=%v", ok, errors)
	}
	var out []string
	if diags := value.ElementsAs(context.Background(), &out, false); diags.HasError() {
		t.Fatalf("decode list state value: %v", diags.Errors())
	}
	if len(out) != 2 || out[0] != "10.0.0.0/8" || out[1] != "192.0.2.10" {
		t.Fatalf("unexpected list state value %#v", out)
	}
}

func TestSensitiveStringMapStateValuePreservesMasks(t *testing.T) {
	var errors []string
	value, ok := sensitiveStringMapStateValue(
		context.Background(),
		"headers",
		map[string]string{"authorization": "********", "x-trace": "remote"},
		map[string]string{"authorization": "secret://old", "x-trace": "previous"},
		func(summary string, detail string) {
			errors = append(errors, summary+": "+detail)
		},
	)
	if !ok || len(errors) != 0 {
		t.Fatalf("expected successful map encoding, ok=%v errors=%v", ok, errors)
	}
	var out map[string]string
	if diags := value.ElementsAs(context.Background(), &out, false); diags.HasError() {
		t.Fatalf("decode map state value: %v", diags.Errors())
	}
	if out["authorization"] != "secret://old" || out["x-trace"] != "remote" {
		t.Fatalf("unexpected map state value %#v", out)
	}
}
