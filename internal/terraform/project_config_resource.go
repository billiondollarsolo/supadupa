package terraform

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectConfigResource struct {
	client *Client
}

type projectConfigResourceModel struct {
	ID     types.String `tfsdk:"id"`
	Ref    types.String `tfsdk:"ref"`
	Area   types.String `tfsdk:"area"`
	Config types.Map    `tfsdk:"config"`
}

func NewProjectConfigResource() resource.Resource {
	return &projectConfigResource{}
}

func (r *projectConfigResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_config"
}

func (r *projectConfigResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project config area managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Stable config ID in the form ref/area.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replace,
			},
			"area": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project config area such as auth, auth_providers, email_templates, storage, functions, realtime, pooler, network, smtp, or ai.",
				PlanModifiers: replace,
			},
			"config": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Config key/value pairs for the area. Values should use secret:// handles for sensitive provider credentials.",
			},
		},
	}
}

func (r *projectConfigResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectConfigResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	config, ok := configMapFromTerraform(ctx, plan.Config, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	updated, err := r.client.UpdateProjectConfig(ctx, plan.Ref.ValueString(), plan.Area.ValueString(), config)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project config", err.Error())
		return
	}
	setProjectConfigState(ctx, &plan, updated, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	config, err := r.client.GetProjectConfig(ctx, state.Ref.ValueString(), state.Area.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project config", err.Error())
		return
	}
	setProjectConfigState(ctx, &state, config, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectConfigResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	config, ok := configMapFromTerraform(ctx, plan.Config, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	updated, err := r.client.UpdateProjectConfig(ctx, plan.Ref.ValueString(), plan.Area.ValueString(), config)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project config", err.Error())
		return
	}
	setProjectConfigState(ctx, &plan, updated, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectConfigResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectConfigResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.client.UpdateProjectConfig(ctx, state.Ref.ValueString(), state.Area.ValueString(), map[string]string{})
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to reset Supadupa project config", err.Error())
		return
	}
}

func (r *projectConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "area", "Use ref/area, for example alpha/auth.")
}

func configMapFromTerraform(ctx context.Context, value types.Map, addError func(string, string)) (map[string]string, bool) {
	config := map[string]string{}
	diags := value.ElementsAs(ctx, &config, false)
	if diags.HasError() {
		addError("Invalid config map", diags.Errors()[0].Detail())
		return nil, false
	}
	return config, true
}

func setProjectConfigState(ctx context.Context, model *projectConfigResourceModel, config ProjectConfig, addError func(string, string)) {
	model.ID = types.StringValue(config.ProjectRef + "/" + config.Area)
	model.Ref = types.StringValue(config.ProjectRef)
	model.Area = types.StringValue(config.Area)
	value, ok := stringMapStateValue(ctx, "config", config.Config, addError)
	if !ok {
		return
	}
	model.Config = value
}
