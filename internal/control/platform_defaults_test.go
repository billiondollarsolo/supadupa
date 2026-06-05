package control

import (
	"context"
	"testing"
)

func TestPlatformDefaultsApplyToProjectCreation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpdatePlatformDefaults(ctx, PlatformDefaultsInput{
		Domain:         "apps.example.com",
		StackVersion:   "2026.06.05",
		Profile:        StackProfileEssential,
		ResourceTier:   ResourceTierMedium,
		BackupSchedule: "hourly",
		FeatureFlags: map[string]bool{
			"single_org_mode":     false,
			"kubernetes_operator": true,
			"read_replicas":       true,
		},
		SMTP: PlatformSMTP{
			Enabled:        true,
			Host:           "smtp.example.com",
			Port:           2525,
			SenderName:     "supadupa",
			SenderEmail:    "noreply@example.com",
			Username:       "apikey",
			PasswordHandle: "secret://platform/smtp-password",
			TLSMode:        "implicit",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := store.GetPlatformDefaults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !defaults.SMTP.Enabled || defaults.SMTP.Host != "smtp.example.com" || defaults.SMTP.Port != 2525 || defaults.SMTP.PasswordHandle != "secret://platform/smtp-password" || defaults.SMTP.TLSMode != "implicit" {
		t.Fatalf("expected platform smtp defaults to persist in memory store, got %#v", defaults.SMTP)
	}
	if defaults.FeatureFlags["single_org_mode"] || !defaults.FeatureFlags["kubernetes_operator"] || !defaults.FeatureFlags["read_replicas"] || !defaults.FeatureFlags["team_rbac"] || !defaults.FeatureFlags["supabase_cli_compat"] {
		t.Fatalf("expected normalized feature flags, got %#v", defaults.FeatureFlags)
	}

	project, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID: org.ID,
		Ref:   "alpha",
		Name:  "Alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	if project.Spec.Domain != "apps.example.com" || project.Spec.StackVersion != "2026.06.05" || project.Spec.Profile != StackProfileEssential || project.Spec.ResourceTier != ResourceTierMedium {
		t.Fatalf("expected project spec to use platform defaults, got %#v", project.Spec)
	}
	policy, err := store.GetBackupPolicy(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Schedule != "hourly" {
		t.Fatalf("expected hourly backup policy from defaults, got %#v", policy)
	}
}

func TestPlatformDefaultsValidateSupportedValues(t *testing.T) {
	store := NewMemoryStore()
	defaults, err := store.UpdatePlatformDefaults(context.Background(), PlatformDefaultsInput{
		Domain:         "apps.example.com",
		StackVersion:   "latest",
		Profile:        StackProfileOrioleDB,
		ResourceTier:   ResourceTierSmall,
		BackupSchedule: "daily",
	})
	if err != nil {
		t.Fatalf("expected orioledb profile defaults to be valid: %v", err)
	}
	if defaults.Profile != StackProfileOrioleDB {
		t.Fatalf("expected orioledb profile defaults, got %#v", defaults.Profile)
	}
	_, err = store.UpdatePlatformDefaults(context.Background(), PlatformDefaultsInput{
		Domain:         "apps.example.com",
		StackVersion:   "latest",
		Profile:        "tiny",
		ResourceTier:   ResourceTierSmall,
		BackupSchedule: "daily",
	})
	if err == nil {
		t.Fatal("expected invalid profile to fail")
	}
	_, err = store.UpdatePlatformDefaults(context.Background(), PlatformDefaultsInput{
		Domain:         "apps.example.com",
		StackVersion:   "latest",
		Profile:        StackProfileFull,
		ResourceTier:   ResourceTierSmall,
		BackupSchedule: "daily",
		SMTP:           PlatformSMTP{Enabled: true, Host: "smtp.example.com", Port: 587, PasswordHandle: "raw-secret", TLSMode: "starttls"},
	})
	if err == nil {
		t.Fatal("expected raw smtp password to fail")
	}
	_, err = store.UpdatePlatformDefaults(context.Background(), PlatformDefaultsInput{
		Domain:         "apps.example.com",
		StackVersion:   "latest",
		Profile:        StackProfileFull,
		ResourceTier:   ResourceTierSmall,
		BackupSchedule: "daily",
		FeatureFlags:   map[string]bool{"unknown": true},
	})
	if err == nil {
		t.Fatal("expected unsupported feature flag to fail")
	}
}

func TestOrgFeatureFlagsInheritPlatformDefaultsAndOverride(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePlatformDefaults(ctx, PlatformDefaultsInput{
		Domain:         "apps.example.com",
		StackVersion:   "latest",
		Profile:        StackProfileFull,
		ResourceTier:   ResourceTierSmall,
		BackupSchedule: "daily",
		FeatureFlags: map[string]bool{
			"billing":       true,
			"read_replicas": true,
		},
	}); err != nil {
		t.Fatal(err)
	}

	flags, err := store.GetOrgFeatureFlags(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !flags.Defaults["billing"] || !flags.Effective["billing"] || !flags.Effective["read_replicas"] {
		t.Fatalf("expected org flags to inherit platform defaults, got %#v", flags)
	}

	flags, err = store.UpdateOrgFeatureFlags(ctx, org.ID, OrgFeatureFlagsInput{
		Overrides: map[string]bool{
			"billing":         false,
			"custom_domains":  true,
			"single_org_mode": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if flags.Defaults["billing"] != true || flags.Effective["billing"] != false || flags.Effective["custom_domains"] != true || flags.Effective["single_org_mode"] != false {
		t.Fatalf("expected org overrides to win over defaults, got %#v", flags)
	}
	if len(flags.Overrides) != 3 {
		t.Fatalf("expected three explicit overrides, got %#v", flags.Overrides)
	}

	list, err := store.ListOrgs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	billingOverride, hasBillingOverride := list[0].FeatureFlagOverrides["billing"]
	if len(list) != 1 || list[0].FeatureFlags["custom_domains"] != true || !hasBillingOverride || billingOverride != false {
		t.Fatalf("expected list orgs to include effective flags and overrides, got %#v", list)
	}

	if _, err := store.UpdateOrgFeatureFlags(ctx, org.ID, OrgFeatureFlagsInput{Overrides: map[string]bool{"unknown": true}}); err == nil {
		t.Fatal("expected unsupported org feature flag to fail")
	}
}

func TestOrioleDBStackProfileSeedsDatabaseConfig(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:   org.ID,
		Ref:     "oriole-proj",
		Name:    "Oriole",
		Profile: StackProfileOrioleDB,
	})
	if err != nil {
		t.Fatal(err)
	}
	if project.Spec.Profile != StackProfileOrioleDB {
		t.Fatalf("expected orioledb profile, got %#v", project.Spec.Profile)
	}
	config, err := store.GetProjectConfig(ctx, project.Ref, "database")
	if err != nil {
		t.Fatal(err)
	}
	if config.Config["orioledb_profile"] != "preview" {
		t.Fatalf("expected orioledb profile config, got %#v", config.Config)
	}
}
