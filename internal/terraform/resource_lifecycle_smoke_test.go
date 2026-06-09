package terraform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestProjectResourceLifecycleSmokeAgainstFakeManagementAPI(t *testing.T) {
	ctx := context.Background()
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer smoke-token" {
			t.Fatalf("expected bearer auth, got %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/projects":
			var got CreateProjectRequest
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode create project payload: %v", err)
			}
			if got.Ref != "alpha" || got.Name != "Alpha" || got.Domain != "apps.example.test" || got.StackVersion != "2026.06.01" || got.Profile != "full" || got.ResourceTier != "medium" {
				t.Fatalf("unexpected create project payload: %#v", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"proj_1","org_id":"org_1","ref":"alpha","name":"Alpha","status":"creating","spec":{"domain":"apps.example.test","stack_version":"2026.06.01","profile":"full","resource_tier":"medium"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha":
			_, _ = w.Write([]byte(`{"id":"proj_1","org_id":"org_1","ref":"alpha","name":"Alpha","status":"healthy","spec":{"domain":"apps.example.test","stack_version":"2026.06.01","profile":"full","resource_tier":"medium"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, "smoke-token", server.Client())
	if err != nil {
		t.Fatalf("new terraform client: %v", err)
	}
	res := &projectResource{client: client}
	var schemaResp resource.SchemaResponse
	res.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	plan := tfsdk.Plan{Schema: schemaResp.Schema}
	diags := plan.Set(ctx, &projectResourceModel{
		OrgID:        types.StringValue("org_1"),
		Ref:          types.StringValue("alpha"),
		Name:         types.StringValue("Alpha"),
		HostID:       types.StringNull(),
		Domain:       types.StringValue("apps.example.test"),
		StackVersion: types.StringValue("2026.06.01"),
		Profile:      types.StringValue("full"),
		ResourceTier: types.StringValue("medium"),
		Services:     types.MapNull(types.BoolType),
	})
	if diags.HasError() {
		t.Fatalf("set plan diagnostics: %v", diags)
	}

	createResp := resource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	res.Create(ctx, resource.CreateRequest{Plan: plan}, &createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", createResp.Diagnostics)
	}
	var created projectResourceModel
	createResp.Diagnostics.Append(createResp.State.Get(ctx, &created)...)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("decode created state diagnostics: %v", createResp.Diagnostics)
	}
	if created.ID.ValueString() != "proj_1" || created.Status.ValueString() != "creating" || created.Domain.ValueString() != "apps.example.test" {
		t.Fatalf("unexpected created state: %#v", created)
	}

	readResp := resource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	res.Read(ctx, resource.ReadRequest{State: createResp.State}, &readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("read diagnostics: %v", readResp.Diagnostics)
	}
	var read projectResourceModel
	readResp.Diagnostics.Append(readResp.State.Get(ctx, &read)...)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("decode read state diagnostics: %v", readResp.Diagnostics)
	}
	if read.Status.ValueString() != "healthy" {
		t.Fatalf("expected read to refresh status to healthy, got %#v", read)
	}

	deleteResp := resource.DeleteResponse{State: readResp.State}
	res.Delete(ctx, resource.DeleteRequest{State: readResp.State}, &deleteResp)
	if deleteResp.Diagnostics.HasError() {
		t.Fatalf("delete diagnostics: %v", deleteResp.Diagnostics)
	}

	joined := strings.Join(seen, "\n")
	for _, expected := range []string{
		"POST /v1/orgs/org_1/projects",
		"GET /v1/projects/alpha",
		"DELETE /v1/projects/alpha",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %q in request sequence:\n%s", expected, joined)
		}
	}
}
