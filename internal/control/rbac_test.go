package control

import (
	"context"
	"testing"
)

func TestTeamProjectAccessResolvesHighestRole(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "team-rbac", Name: "Team RBAC"})
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateOrgTeam(ctx, org.ID, TeamInput{Name: "Backend Team", Slug: "backend"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertTeamMember(ctx, org.ID, team.Slug, TeamMemberInput{Email: "dev@example.com"}); err != nil {
		t.Fatal(err)
	}
	grant, err := store.UpsertProjectAccess(ctx, project.Ref, ProjectAccessInput{SubjectType: "team", SubjectID: team.Slug, Role: "developer"})
	if err != nil {
		t.Fatal(err)
	}
	if grant.SubjectType != "team" || grant.SubjectID != team.ID || grant.Role != "developer" {
		t.Fatalf("unexpected team grant %#v", grant)
	}
	role, err := store.ResolveProjectRole(ctx, project.Ref, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if role != "developer" {
		t.Fatalf("expected developer role, got %q", role)
	}

	if _, err := store.UpsertProjectAccess(ctx, project.Ref, ProjectAccessInput{SubjectType: "user", SubjectID: "dev@example.com", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	role, err = store.ResolveProjectRole(ctx, project.Ref, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if role != "admin" {
		t.Fatalf("expected highest admin role, got %q", role)
	}
}

func TestDeletingTeamRemovesProjectAccessGrant(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "team-cleanup", Name: "Team Cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateOrgTeam(ctx, org.ID, TeamInput{Name: "Cleanup", Slug: "cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertProjectAccess(ctx, project.Ref, ProjectAccessInput{SubjectType: "team", SubjectID: team.Slug, Role: "viewer"}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteOrgTeam(ctx, org.ID, team.Slug); err != nil {
		t.Fatal(err)
	}
	grants, err := store.ListProjectAccess(ctx, project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 {
		t.Fatalf("expected team grants to be removed, got %#v", grants)
	}
}

func TestOrgAccessReviewIncludesEffectiveProjectRoles(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{OrgID: org.ID, Ref: "review-proj", Name: "Review"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertOrgMember(ctx, org.ID, MembershipInput{Email: "viewer@example.com", Role: "viewer"}); err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateOrgTeam(ctx, org.ID, TeamInput{Name: "Reviewers", Slug: "reviewers"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertTeamMember(ctx, org.ID, team.Slug, TeamMemberInput{Email: "viewer@example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertProjectAccess(ctx, project.Ref, ProjectAccessInput{SubjectType: "team", SubjectID: team.Slug, Role: "admin"}); err != nil {
		t.Fatal(err)
	}

	review, err := store.GetOrgAccessReview(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Members) != 1 || len(review.Teams) != 1 || len(review.Projects) != 1 {
		t.Fatalf("unexpected review shape %#v", review)
	}
	effective := review.Projects[0].Effective
	if len(effective) != 1 || effective[0].Email != "viewer@example.com" || effective[0].Role != "admin" {
		t.Fatalf("expected effective admin via team grant, got %#v", effective)
	}
	if len(effective[0].Sources) < 2 {
		t.Fatalf("expected org and team sources, got %#v", effective[0].Sources)
	}
}
