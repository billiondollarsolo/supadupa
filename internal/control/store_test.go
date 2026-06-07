package control

import (
	"context"
	"strings"
	"testing"
)

func TestCreateProjectRejectsRefsThatBreakPublicDNSLabels(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}

	valid55 := strings.Repeat("a", 55)
	if _, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: valid55, Name: "Valid"}); err != nil {
		t.Fatalf("expected 55-character ref to be valid: %v", err)
	}
	longAppsDomain := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63)
	if _, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: strings.Repeat("b", 55), Name: "Long Domain", Domain: longAppsDomain}); err == nil || !strings.Contains(err.Error(), "253-character DNS name limit") {
		t.Fatalf("expected generated project host length rejection, got %v", err)
	}

	for _, ref := range []string{
		"-bad",
		"bad-",
		strings.Repeat("a", 56),
		strings.Repeat("a", 57),
		strings.Repeat("a", 64),
	} {
		if _, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: ref, Name: "Invalid"}); err == nil || !strings.Contains(err.Error(), "ref must be 3-55") {
			t.Fatalf("expected DNS-safe ref rejection for %q, got %v", ref, err)
		}
	}
}

func TestCreateProjectBranchRejectsRefsThatBreakPublicDNSLabels(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "source", Name: "Source"})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.CreateProjectBranch(ctx, source.Ref, ProjectBranchInput{Ref: strings.Repeat("b", 56), Name: "Too Long"}); err == nil || !strings.Contains(err.Error(), "branch ref must be 3-55") {
		t.Fatalf("expected DNS-safe branch ref rejection, got %v", err)
	}
}

func TestCreateProjectRejectsGeneratedHostReservations(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "admin",
		Name:   "Admin Collision",
		Domain: "example.com",
	}); err == nil || !strings.Contains(err.Error(), "platform host topology") {
		t.Fatalf("expected generated platform host collision, got %v", err)
	}

	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "apps.example.com",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddProjectDomain(ctx, "alpha", ProjectDomainInput{FQDN: "beta.apps.example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "beta",
		Name:   "Beta",
		Domain: "apps.example.com",
	}); err == nil || !strings.Contains(err.Error(), "project custom domain alpha") {
		t.Fatalf("expected generated host collision with existing custom domain, got %v", err)
	}
}

func TestCreateProjectBranchRejectsGeneratedHostReservations(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "source",
		Name:   "Source",
		Domain: "example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.CreateProjectBranch(ctx, source.Ref, ProjectBranchInput{Ref: "api", Name: "API Collision"}); err == nil || !strings.Contains(err.Error(), "platform host") {
		t.Fatalf("expected branch generated platform host collision, got %v", err)
	}
}

func TestCreateProjectReplicaRejectsPublicDNSLabelOverflow(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}

	validName := strings.Repeat("r", 46)
	if _, err := store.CreateProjectReplica(ctx, "alpha", ProjectReplicaInput{Name: validName}); err != nil {
		t.Fatalf("expected 63-character replica DNS label to be valid: %v", err)
	}
	for _, invalidName := range []string{"-east", "east-"} {
		if _, err := store.CreateProjectReplica(ctx, "alpha", ProjectReplicaInput{Name: invalidName}); err == nil || !strings.Contains(err.Error(), "cannot start or end with a dash") {
			t.Fatalf("expected replica DNS label rejection for %q, got %v", invalidName, err)
		}
	}
	tooLongName := strings.Repeat("r", 47)
	if _, err := store.CreateProjectReplica(ctx, "alpha", ProjectReplicaInput{Name: tooLongName}); err == nil || !strings.Contains(err.Error(), "63-character DNS label limit") {
		t.Fatalf("expected replica DNS label overflow rejection, got %v", err)
	}

	longRef := strings.Repeat("b", 49)
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    longRef,
		Name:   "Long Ref",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProjectReplica(ctx, longRef, ProjectReplicaInput{Name: "abc"}); err == nil || !strings.Contains(err.Error(), "too long for public read-replica DNS labels") {
		t.Fatalf("expected long project ref replica rejection, got %v", err)
	}

	replicaLongDomain := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 47)
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "short",
		Name:   "Long Replica Domain",
		Domain: replicaLongDomain,
	}); err != nil {
		t.Fatalf("expected long domain to still fit base generated project hosts: %v", err)
	}
	if _, err := store.CreateProjectReplica(ctx, "short", ProjectReplicaInput{Name: "east"}); err == nil || !strings.Contains(err.Error(), "253-character DNS name limit") {
		t.Fatalf("expected replica full FQDN length rejection, got %v", err)
	}
}

func TestAddProjectDomainRejectsGeneratedStorageHost(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "beta",
		Name:   "Beta",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}

	_, err = store.AddProjectDomain(ctx, "beta", ProjectDomainInput{FQDN: "storage-alpha.apps.supadupa.test"})
	if err == nil || !strings.Contains(err.Error(), "project storage alpha") {
		t.Fatalf("expected generated storage host reservation conflict, got %v", err)
	}
}

func TestAddProjectDomainRejectsGeneratedReplicaDatabaseHost(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "alpha",
		Name:   "Alpha",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:  org.ID,
		Ref:    "beta",
		Name:   "Beta",
		Domain: "apps.supadupa.test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProjectReplica(ctx, "alpha", ProjectReplicaInput{Name: "east"}); err != nil {
		t.Fatal(err)
	}

	_, err = store.AddProjectDomain(ctx, "beta", ProjectDomainInput{FQDN: "db-replica-east-alpha.apps.supadupa.test"})
	if err == nil || !strings.Contains(err.Error(), "project replica alpha/east") {
		t.Fatalf("expected generated replica host reservation conflict, got %v", err)
	}
}
