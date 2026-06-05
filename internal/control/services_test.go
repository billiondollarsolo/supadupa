package control

import (
	"context"
	"testing"
)

func TestProjectServicesDefaultAndUpdate(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	project, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID: org.ID,
		Ref:   "svc-proj",
		Name:  "Services",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if got := ProjectServiceStates(project.Spec.Services); !got["auth"] || !got["storage"] || !got["functions"] {
		t.Fatalf("expected default services enabled, got %#v", got)
	}

	updated, err := store.UpdateProjectServices(ctx, "svc-proj", ProjectServicesInput{
		Services: map[string]bool{
			"storage":      false,
			"edge-runtime": false,
			"supavisor":    true,
		},
	})
	if err != nil {
		t.Fatalf("update services: %v", err)
	}
	services := ProjectServiceStates(updated.Spec.Services)
	if services["storage"] || services["functions"] || !services["pooler"] {
		t.Fatalf("expected normalized service update, got %#v", services)
	}
}

func TestProjectServicesRejectUnsupportedName(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	org, err := store.CreateOrg(ctx, "Platform")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := store.CreateProject(ctx, CreateProjectRequest{
		OrgID:    org.ID,
		Ref:      "bad-svc",
		Name:     "Bad Services",
		Services: map[string]bool{"billing": true},
	}); err == nil {
		t.Fatalf("expected unsupported service error")
	}
}
