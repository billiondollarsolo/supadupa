package terraform

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestRequireResourceReplaceOnUpdate(t *testing.T) {
	resourceValue := tftypes.NewValue(
		tftypes.Object{AttributeTypes: map[string]tftypes.Type{"name": tftypes.String}},
		map[string]tftypes.Value{"name": tftypes.NewValue(tftypes.String, "primary")},
	)

	var resp resource.ModifyPlanResponse
	requireResourceReplaceOnUpdate(context.Background(), resource.ModifyPlanRequest{
		State: tfsdk.State{Raw: resourceValue},
		Plan:  tfsdk.Plan{Raw: resourceValue},
	}, &resp, "name")

	if len(resp.RequiresReplace) != 1 {
		t.Fatalf("expected one replacement path, got %d", len(resp.RequiresReplace))
	}
	if got := resp.RequiresReplace[0].String(); got != "name" {
		t.Fatalf("expected replacement path name, got %q", got)
	}
}

func TestRequireResourceReplaceOnUpdateSkipsCreateAndDestroy(t *testing.T) {
	resourceType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"name": tftypes.String}}
	resourceValue := tftypes.NewValue(resourceType, map[string]tftypes.Value{"name": tftypes.NewValue(tftypes.String, "primary")})
	nullResource := tftypes.NewValue(resourceType, nil)

	tests := map[string]resource.ModifyPlanRequest{
		"create": {
			State: tfsdk.State{Raw: nullResource},
			Plan:  tfsdk.Plan{Raw: resourceValue},
		},
		"destroy": {
			State: tfsdk.State{Raw: resourceValue},
			Plan:  tfsdk.Plan{Raw: nullResource},
		},
	}

	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			var resp resource.ModifyPlanResponse
			requireResourceReplaceOnUpdate(context.Background(), req, &resp, "name")
			if len(resp.RequiresReplace) != 0 {
				t.Fatalf("expected no replacement paths, got %d", len(resp.RequiresReplace))
			}
		})
	}
}

func TestReplaceOnlyResourcesDeclareReplacementPlanning(t *testing.T) {
	replaceOnlyResources := []struct {
		name string
		new  func() resource.Resource
	}{
		{name: "host", new: NewHostResource},
		{name: "org_team", new: NewOrgTeamResource},
		{name: "org_team_member", new: NewOrgTeamMemberResource},
		{name: "project", new: NewProjectResource},
		{name: "project_auth_client", new: NewProjectAuthClientResource},
		{name: "project_branch", new: NewProjectBranchResource},
		{name: "project_database_cron_job", new: NewProjectDatabaseCronJobResource},
		{name: "project_database_queue", new: NewProjectDatabaseQueueResource},
		{name: "project_database_schema", new: NewProjectDatabaseSchemaResource},
		{name: "project_database_role", new: NewProjectDatabaseRoleResource},
		{name: "project_database_webhook", new: NewProjectDatabaseWebhookResource},
		{name: "project_domain", new: NewProjectDomainResource},
		{name: "project_embedding_job", new: NewProjectEmbeddingJobResource},
		{name: "project_function_region", new: NewProjectFunctionRegionResource},
		{name: "project_function_storage_mount", new: NewProjectFunctionStorageMountResource},
		{name: "project_network_connection", new: NewProjectNetworkConnectionResource},
		{name: "project_replica", new: NewProjectReplicaResource},
		{name: "project_replication_pipeline", new: NewProjectReplicationPipelineResource},
		{name: "project_storage_bucket", new: NewProjectStorageBucketResource},
		{name: "project_vector_bucket", new: NewProjectVectorBucketResource},
		{name: "project_analytics_bucket", new: NewProjectAnalyticsBucketResource},
	}

	for _, tc := range replaceOnlyResources {
		t.Run(tc.name, func(t *testing.T) {
			instance := tc.new()
			if hasResourceModifyPlan(instance) {
				return
			}

			schema := resourceSchemaForTest(t, instance)
			var missing []string
			for name, attr := range schema.Attributes {
				if !isPractitionerConfigurable(attr) {
					continue
				}
				if !hasRequiresReplacePlanModifier(attr) {
					missing = append(missing, name)
				}
			}
			if len(missing) > 0 {
				t.Fatalf("replace-only resource must add ModifyPlan replacement hook or RequiresReplace on configurable attributes, missing: %s", strings.Join(missing, ", "))
			}
		})
	}
}

func hasResourceModifyPlan(instance resource.Resource) bool {
	_, ok := instance.(interface {
		ModifyPlan(context.Context, resource.ModifyPlanRequest, *resource.ModifyPlanResponse)
	})
	return ok
}

func resourceSchemaForTest(t *testing.T, instance resource.Resource) resourceschema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	instance.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func isPractitionerConfigurable(attr resourceschema.Attribute) bool {
	value := reflect.ValueOf(attr)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return false
	}
	required := boolField(value, "Required")
	optional := boolField(value, "Optional")
	writeOnly := boolField(value, "WriteOnly")
	return required || optional || writeOnly
}

func hasRequiresReplacePlanModifier(attr resourceschema.Attribute) bool {
	value := reflect.ValueOf(attr)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return false
	}
	modifiers := value.FieldByName("PlanModifiers")
	if !modifiers.IsValid() {
		return false
	}
	for i := 0; i < modifiers.Len(); i++ {
		if strings.Contains(strings.ToLower(fmt.Sprintf("%T", modifiers.Index(i).Interface())), "requiresreplace") {
			return true
		}
	}
	return false
}

func boolField(value reflect.Value, name string) bool {
	field := value.FieldByName(name)
	return field.IsValid() && field.Kind() == reflect.Bool && field.Bool()
}
