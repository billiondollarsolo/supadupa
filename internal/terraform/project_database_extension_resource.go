package terraform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectDatabaseExtensionResource struct {
	client *Client
}

type projectDatabaseExtensionResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Ref       types.String `tfsdk:"ref"`
	Name      types.String `tfsdk:"name"`
	Schema    types.String `tfsdk:"schema"`
	Version   types.String `tfsdk:"version"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	Status    types.String `tfsdk:"status"`
	Message   types.String `tfsdk:"message"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func NewProjectDatabaseExtensionResource() resource.Resource {
	return &projectDatabaseExtensionResource{}
}

func (r *projectDatabaseExtensionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_database_extension"
}

func (r *projectDatabaseExtensionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project Postgres extension override managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated or default extension record ID.",
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
				Required:      true,
				Description:   "Extension name from the supported upstream catalog.",
				PlanModifiers: replace,
			},
			"schema": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Schema where the extension should be installed. Empty input uses the upstream catalog default.",
			},
			"version": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Optional pinned extension version.",
			},
			"enabled": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the extension should be enabled.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Extension status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable extension status message.",
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

func (r *projectDatabaseExtensionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *terraform.Client, got %T.", req.ProviderData))
		return
	}
	r.client = client
}

func (r *projectDatabaseExtensionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectDatabaseExtensionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input := databaseExtensionInputFromModel(plan)
	extension, err := r.client.UpdateProjectDatabaseExtension(ctx, plan.Ref.ValueString(), plan.Name.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project database extension", err.Error())
		return
	}
	setProjectDatabaseExtensionState(&plan, extension)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectDatabaseExtensionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectDatabaseExtensionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	extension, err := r.findDatabaseExtension(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project database extension", err.Error())
		return
	}
	setProjectDatabaseExtensionState(&state, extension)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectDatabaseExtensionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectDatabaseExtensionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input := databaseExtensionInputFromModel(plan)
	extension, err := r.client.UpdateProjectDatabaseExtension(ctx, plan.Ref.ValueString(), plan.Name.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project database extension", err.Error())
		return
	}
	setProjectDatabaseExtensionState(&plan, extension)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectDatabaseExtensionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectDatabaseExtensionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.client.UpdateProjectDatabaseExtension(ctx, state.Ref.ValueString(), state.Name.ValueString(), ProjectDatabaseExtensionInput{})
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to reset Supadupa project database extension", err.Error())
		return
	}
}

func (r *projectDatabaseExtensionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ref, name, ok := strings.Cut(req.ID, "/")
	if !ok {
		ref, name, ok = strings.Cut(req.ID, ":")
	}
	if !ok || strings.TrimSpace(ref) == "" || strings.TrimSpace(name) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use ref/name, for example alpha/pg_cron.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ref"), strings.TrimSpace(ref))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), strings.TrimSpace(name))...)
}

func (r *projectDatabaseExtensionResource) findDatabaseExtension(ctx context.Context, ref string, name string) (ProjectDatabaseExtension, error) {
	extensions, err := r.client.ListProjectDatabaseExtensions(ctx, ref)
	if err != nil {
		return ProjectDatabaseExtension{}, err
	}
	for _, extension := range extensions {
		if extension.Name == name {
			return extension, nil
		}
	}
	return ProjectDatabaseExtension{}, ErrNotFound
}

func databaseExtensionInputFromModel(model projectDatabaseExtensionResourceModel) ProjectDatabaseExtensionInput {
	enabled := model.Enabled.ValueBool()
	return ProjectDatabaseExtensionInput{
		Schema:  model.Schema.ValueString(),
		Version: model.Version.ValueString(),
		Enabled: &enabled,
	}
}

func setProjectDatabaseExtensionState(model *projectDatabaseExtensionResourceModel, extension ProjectDatabaseExtension) {
	model.ID = types.StringValue(extension.ID)
	model.Ref = types.StringValue(extension.ProjectRef)
	model.Name = types.StringValue(extension.Name)
	model.Schema = optionalStringValue(extension.Schema)
	model.Version = optionalStringValue(extension.Version)
	model.Enabled = types.BoolValue(extension.Enabled)
	model.Status = types.StringValue(extension.Status)
	model.Message = optionalStringValue(extension.Message)
	model.CreatedAt = optionalTimeString(extension.CreatedAt)
	model.UpdatedAt = optionalTimeString(extension.UpdatedAt)
}
