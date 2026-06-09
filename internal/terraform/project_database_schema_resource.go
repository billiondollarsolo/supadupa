package terraform

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectDatabaseSchemaResource struct {
	client *Client
}

type projectDatabaseSchemaResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Ref        types.String `tfsdk:"ref"`
	Name       types.String `tfsdk:"name"`
	Version    types.String `tfsdk:"version"`
	Schema     types.String `tfsdk:"schema"`
	SQL        types.String `tfsdk:"sql"`
	Checksum   types.String `tfsdk:"checksum"`
	ApplyOrder types.Int64  `tfsdk:"apply_order"`
	Active     types.Bool   `tfsdk:"active"`
	Metadata   types.Map    `tfsdk:"metadata"`
	Status     types.String `tfsdk:"status"`
	Message    types.String `tfsdk:"message"`
	CreatedAt  types.String `tfsdk:"created_at"`
	UpdatedAt  types.String `tfsdk:"updated_at"`
}

func NewProjectDatabaseSchemaResource() resource.Resource {
	return &projectDatabaseSchemaResource{}
}

func (r *projectDatabaseSchemaResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_database_schema"
}

func (r *projectDatabaseSchemaResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project declarative database schema migration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated database schema migration ID.",
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
				Description: "Schema migration name. Must be 3-64 lowercase letters, numbers, or dashes.",
			},
			"version": resourceschema.StringAttribute{
				Required:    true,
				Description: "Migration version.",
			},
			"schema": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("public"),
				Description: "Target database schema.",
			},
			"sql": resourceschema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "SQL migration text.",
			},
			"checksum": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Server-computed SHA-256 checksum for the SQL text.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"apply_order": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
				Description: "Apply order, between 0 and 1000000.",
			},
			"active": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the schema migration declaration should be active.",
			},
			"metadata": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "Schema migration metadata. Sensitive values must use secret:// handles.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Schema migration status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable schema migration status message.",
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

func (r *projectDatabaseSchemaResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectDatabaseSchemaResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	requireResourceReplaceOnUpdate(ctx, req, resp, "name")
}

func (r *projectDatabaseSchemaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectDatabaseSchemaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := databaseSchemaInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	schema, err := r.client.CreateProjectDatabaseSchema(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project database schema", err.Error())
		return
	}
	setProjectDatabaseSchemaState(ctx, &plan, schema, input.Metadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectDatabaseSchemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectDatabaseSchemaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	schema, err := r.findDatabaseSchema(ctx, state.Ref.ValueString(), state.Name.ValueString(), state.Version.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project database schema", err.Error())
		return
	}
	previousMetadata, ok := optionalConfigMapFromTerraform(ctx, state.Metadata, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectDatabaseSchemaState(ctx, &state, schema, previousMetadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectDatabaseSchemaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	reportUnsupportedInPlaceUpdate(resp, "Supadupa project database schema")
}

func (r *projectDatabaseSchemaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectDatabaseSchemaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectDatabaseSchema(ctx, state.Ref.ValueString(), state.Name.ValueString(), state.Version.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project database schema", err.Error())
		return
	}
}

func (r *projectDatabaseSchemaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setThreePartImportState(ctx, req.ID, resp, "ref", "name", "version", "Use ref/name/version, for example alpha/app-schema/20260605_001.", true)
}

func (r *projectDatabaseSchemaResource) findDatabaseSchema(ctx context.Context, ref string, name string, version string) (ProjectDatabaseSchema, error) {
	schemas, err := r.client.ListProjectDatabaseSchemas(ctx, ref)
	if err != nil {
		return ProjectDatabaseSchema{}, err
	}
	return findInList(schemas, func(schema ProjectDatabaseSchema) bool {
		return schema.Name == name && schema.Version == version
	})
}

func databaseSchemaInputFromModel(ctx context.Context, model projectDatabaseSchemaResourceModel, addError func(string, string)) (ProjectDatabaseSchemaInput, bool) {
	metadata, ok := configMapFromTerraform(ctx, model.Metadata, addError)
	if !ok {
		return ProjectDatabaseSchemaInput{}, false
	}
	return ProjectDatabaseSchemaInput{
		Name:       model.Name.ValueString(),
		Version:    model.Version.ValueString(),
		Schema:     model.Schema.ValueString(),
		SQL:        model.SQL.ValueString(),
		ApplyOrder: int(model.ApplyOrder.ValueInt64()),
		Active:     model.Active.ValueBool(),
		Metadata:   metadata,
	}, true
}

func setProjectDatabaseSchemaState(ctx context.Context, model *projectDatabaseSchemaResourceModel, schema ProjectDatabaseSchema, previousMetadata map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(schema.ID)
	model.Ref = types.StringValue(schema.ProjectRef)
	model.Name = types.StringValue(schema.Name)
	model.Version = types.StringValue(schema.Version)
	model.Schema = types.StringValue(schema.Schema)
	model.SQL = types.StringValue(schema.SQL)
	model.Checksum = types.StringValue(schema.Checksum)
	model.ApplyOrder = types.Int64Value(int64(schema.ApplyOrder))
	model.Active = types.BoolValue(schema.Active)
	model.Status = types.StringValue(schema.Status)
	model.Message = optionalStringValue(schema.Message)
	model.CreatedAt = optionalTimeString(schema.CreatedAt)
	model.UpdatedAt = optionalTimeString(schema.UpdatedAt)

	metadata, ok := sensitiveStringMapStateValue(ctx, "metadata", schema.Metadata, previousMetadata, addError)
	if !ok {
		return
	}
	model.Metadata = metadata
}
