package terraform

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectDatabaseRoleResource struct {
	client *Client
}

type projectDatabaseRoleResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Ref                  types.String `tfsdk:"ref"`
	Name                 types.String `tfsdk:"name"`
	Login                types.Bool   `tfsdk:"login"`
	Inherit              types.Bool   `tfsdk:"inherit"`
	BypassRLS            types.Bool   `tfsdk:"bypass_rls"`
	ConnectionLimit      types.Int64  `tfsdk:"connection_limit"`
	PasswordSecretHandle types.String `tfsdk:"password_secret_handle"`
	MemberOf             types.List   `tfsdk:"member_of"`
	SchemaGrants         types.Map    `tfsdk:"schema_grants"`
	Metadata             types.Map    `tfsdk:"metadata"`
	Status               types.String `tfsdk:"status"`
	Message              types.String `tfsdk:"message"`
	CreatedAt            types.String `tfsdk:"created_at"`
	UpdatedAt            types.String `tfsdk:"updated_at"`
}

func NewProjectDatabaseRoleResource() resource.Resource {
	return &projectDatabaseRoleResource{}
}

func (r *projectDatabaseRoleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_database_role"
}

func (r *projectDatabaseRoleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	emptyStringList := types.ListValueMust(types.StringType, []attr.Value{})
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project Postgres role declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated database role ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replace,
			},
			"name": resourceschema.StringAttribute{
				Required:    true,
				Description: "Postgres role name.",
			},
			"login": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether this is a login role.",
			},
			"inherit": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the role inherits privileges from parent roles.",
			},
			"bypass_rls": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the role may bypass row-level security.",
			},
			"connection_limit": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
				Description: "Connection limit; use -1 for unlimited.",
			},
			"password_secret_handle": resourceschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "secret:// handle for login role password material.",
			},
			"member_of": resourceschema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     listdefault.StaticValue(emptyStringList),
				Description: "Parent roles this role should be a member of.",
			},
			"schema_grants": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Schema grants as schema = comma-separated privileges.",
			},
			"metadata": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "Role metadata. Sensitive values must use secret:// handles.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Role status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable role status message.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Update timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *projectDatabaseRoleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectDatabaseRoleResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	requireResourceReplaceOnUpdate(ctx, req, resp, "name")
}

func (r *projectDatabaseRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectDatabaseRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := databaseRoleInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	role, err := r.client.CreateProjectDatabaseRole(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project database role", err.Error())
		return
	}
	setProjectDatabaseRoleState(ctx, &plan, role, input.PasswordSecretHandle, input.Metadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectDatabaseRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectDatabaseRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	role, err := r.findDatabaseRole(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project database role", err.Error())
		return
	}
	previousMetadata, ok := optionalConfigMapFromTerraform(ctx, state.Metadata, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectDatabaseRoleState(ctx, &state, role, previousSensitiveString(state.PasswordSecretHandle), previousMetadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectDatabaseRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	reportUnsupportedInPlaceUpdate(resp, "Supadupa project database role")
}

func (r *projectDatabaseRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectDatabaseRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectDatabaseRole(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project database role", err.Error())
		return
	}
}

func (r *projectDatabaseRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "name", "Use ref/name, for example alpha/app_writer.")
}

func (r *projectDatabaseRoleResource) findDatabaseRole(ctx context.Context, ref string, name string) (ProjectDatabaseRole, error) {
	roles, err := r.client.ListProjectDatabaseRoles(ctx, ref)
	if err != nil {
		return ProjectDatabaseRole{}, err
	}
	return findInList(roles, func(role ProjectDatabaseRole) bool { return role.Name == name })
}

func databaseRoleInputFromModel(ctx context.Context, model projectDatabaseRoleResourceModel, addError func(string, string)) (ProjectDatabaseRoleInput, bool) {
	memberOf, ok := stringListFromTerraform(ctx, model.MemberOf, "Invalid member_of list", addError)
	if !ok {
		return ProjectDatabaseRoleInput{}, false
	}
	schemaGrants, ok := configMapFromTerraform(ctx, model.SchemaGrants, addError)
	if !ok {
		return ProjectDatabaseRoleInput{}, false
	}
	metadata, ok := configMapFromTerraform(ctx, model.Metadata, addError)
	if !ok {
		return ProjectDatabaseRoleInput{}, false
	}
	inherit := model.Inherit.ValueBool()
	return ProjectDatabaseRoleInput{
		Name:                 model.Name.ValueString(),
		Login:                model.Login.ValueBool(),
		Inherit:              &inherit,
		BypassRLS:            model.BypassRLS.ValueBool(),
		ConnectionLimit:      int(model.ConnectionLimit.ValueInt64()),
		PasswordSecretHandle: model.PasswordSecretHandle.ValueString(),
		MemberOf:             memberOf,
		SchemaGrants:         schemaGrants,
		Metadata:             metadata,
	}, true
}

func setProjectDatabaseRoleState(ctx context.Context, model *projectDatabaseRoleResourceModel, role ProjectDatabaseRole, previousPasswordHandle string, previousMetadata map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(role.ID)
	model.Ref = types.StringValue(role.ProjectRef)
	model.Name = types.StringValue(role.Name)
	model.Login = types.BoolValue(role.Login)
	model.Inherit = types.BoolValue(role.Inherit)
	model.BypassRLS = types.BoolValue(role.BypassRLS)
	model.ConnectionLimit = types.Int64Value(int64(role.ConnectionLimit))
	model.PasswordSecretHandle = sensitiveStringValue(preserveMaskedSensitiveValue(role.PasswordSecretHandle, previousPasswordHandle))
	model.Status = types.StringValue(role.Status)
	model.Message = optionalStringValue(role.Message)
	model.CreatedAt = optionalTimeString(role.CreatedAt)
	model.UpdatedAt = optionalTimeString(role.UpdatedAt)

	memberOf, ok := stringListStateValue(ctx, "member_of", role.MemberOf, addError)
	if !ok {
		return
	}
	model.MemberOf = memberOf
	schemaGrants, ok := stringMapStateValue(ctx, "schema_grants", role.SchemaGrants, addError)
	if !ok {
		return
	}
	model.SchemaGrants = schemaGrants
	metadata, ok := sensitiveStringMapStateValue(ctx, "metadata", role.Metadata, previousMetadata, addError)
	if !ok {
		return
	}
	model.Metadata = metadata
}
