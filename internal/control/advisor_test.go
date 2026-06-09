package control

import (
	"context"
	"testing"
)

// findingSeverity returns the severity of the first finding matching title for
// the given project ref, or "" if no such finding exists.
func findingSeverity(findings []AdvisorFinding, ref string, title string) string {
	for _, finding := range findings {
		if finding.ProjectRef == ref && finding.Title == title {
			return finding.Severity
		}
	}
	return ""
}

// The production-intent gate controls posture severity: a development project
// (the default) reports posture gaps at "info" so a greenfield fleet stays
// quiet, while a production project reports them at their real severity.
func TestAdvisorProductionGateScalesPostureSeverity(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	// Two projects with identical (insecure) posture; only the environment differs.
	for _, ref := range []string{"dev-proj", "prod-proj"} {
		if _, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: ref, Name: ref}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpdateProjectConfig(ctx, ref, "database", ProjectConfigInput{Config: map[string]string{"ssl_enforced": "false"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.UpdateProjectConfig(ctx, "prod-proj", "general", ProjectConfigInput{Config: map[string]string{"environment": "production"}}); err != nil {
		t.Fatal(err)
	}

	findings, err := FleetAdvisorFindings(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	const sslTitle = "Database SSL is not enforced"
	if got := findingSeverity(findings, "dev-proj", sslTitle); got != "info" {
		t.Fatalf("expected development posture finding to be info, got %q", got)
	}
	if got := findingSeverity(findings, "prod-proj", sslTitle); got != "high" {
		t.Fatalf("expected production posture finding to be high, got %q", got)
	}

	// The removed non-security findings must not reappear in any environment.
	for _, ref := range []string{"dev-proj", "prod-proj"} {
		if got := findingSeverity(findings, ref, "Fleet advisor mode is not enabled"); got != "" {
			t.Fatalf("did not expect fleet-advisor-mode finding for %s, got %q", ref, got)
		}
		if got := findingSeverity(findings, ref, "No log drains configured"); got != "" {
			t.Fatalf("did not expect log-drain finding for %s, got %q", ref, got)
		}
	}
}
