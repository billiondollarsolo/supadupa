package main

import (
	"supadupa2026/internal/env"
	"testing"
	"time"
)

func TestBoolEnvOrDefault(t *testing.T) {
	if !boolEnvOrDefault("SUPADUPA_OPERATOR_ISOLATION_UNSET", true) {
		t.Fatalf("expected default true for unset isolation env")
	}
	t.Setenv("SUPADUPA_OPERATOR_ISOLATION_SET", "false")
	if boolEnvOrDefault("SUPADUPA_OPERATOR_ISOLATION_SET", true) {
		t.Fatalf("expected false override")
	}
	t.Setenv("SUPADUPA_OPERATOR_ISOLATION_SET", "garbage")
	if !boolEnvOrDefault("SUPADUPA_OPERATOR_ISOLATION_SET", true) {
		t.Fatalf("expected fallback to default for unparseable bool")
	}
}

func TestEnvOrDefault(t *testing.T) {
	if got := env.OrDefault("SUPADUPA_OPERATOR_PREFIX_UNSET", "supadupa-proj-"); got != "supadupa-proj-" {
		t.Fatalf("expected default prefix, got %q", got)
	}
	t.Setenv("SUPADUPA_OPERATOR_PREFIX_SET", "tenant-")
	if got := env.OrDefault("SUPADUPA_OPERATOR_PREFIX_SET", "supadupa-proj-"); got != "tenant-" {
		t.Fatalf("expected override prefix, got %q", got)
	}
}

func TestDurationEnvOrDefault(t *testing.T) {
	if got := durationEnvOrDefault("SUPADUPA_OPERATOR_INTERVAL_UNSET", 15*time.Second); got != 15*time.Second {
		t.Fatalf("expected default interval, got %v", got)
	}
	t.Setenv("SUPADUPA_OPERATOR_INTERVAL_SET", "30s")
	if got := durationEnvOrDefault("SUPADUPA_OPERATOR_INTERVAL_SET", 15*time.Second); got != 30*time.Second {
		t.Fatalf("expected override interval, got %v", got)
	}
}

func TestQuotaFromEnvNilWhenUnset(t *testing.T) {
	if quotaFromEnv() != nil {
		t.Fatalf("expected nil quota when no quota env vars set")
	}
}

func TestQuotaFromEnvParsesValues(t *testing.T) {
	t.Setenv("SUPADUPA_OPERATOR_QUOTA_PODS", "50")
	t.Setenv("SUPADUPA_OPERATOR_QUOTA_REQUESTS_CPU", "4")
	quota := quotaFromEnv()
	if quota == nil || quota.Hard["pods"] != "50" || quota.Hard["requests.cpu"] != "4" {
		t.Fatalf("unexpected quota %#v", quota)
	}
}

func TestLimitsFromEnvNilWhenUnset(t *testing.T) {
	if limitsFromEnv() != nil {
		t.Fatalf("expected nil limits when no limit env vars set")
	}
}

func TestLimitsFromEnvParsesValues(t *testing.T) {
	t.Setenv("SUPADUPA_OPERATOR_LIMIT_DEFAULT_CPU", "500m")
	t.Setenv("SUPADUPA_OPERATOR_LIMIT_REQUEST_CPU", "100m")
	limits := limitsFromEnv()
	if limits == nil || limits.Default["cpu"] != "500m" || limits.DefaultRequest["cpu"] != "100m" {
		t.Fatalf("unexpected limits %#v", limits)
	}
}
