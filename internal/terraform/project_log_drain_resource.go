package terraform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectLogDrainResource struct {
	client *Client
}

type projectLogDrainResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Ref       types.String `tfsdk:"ref"`
	Target    types.String `tfsdk:"target"`
	Config    types.Map    `tfsdk:"config"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func NewProjectLogDrainResource() resource.Resource {
	return &projectLogDrainResource{}
}

func (r *projectLogDrainResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_log_drain"
}

func (r *projectLogDrainResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project log drain managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated log drain ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replace,
			},
			"target": resourceschema.StringAttribute{
				Required:    true,
				Description: "Log drain target such as https, loki, datadog, axiom, s3, or sentry.",
			},
			"config": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "Target-specific key/value configuration. Use secret:// handles for sensitive downstream credentials.",
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *projectLogDrainResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *projectLogDrainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectLogDrainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	config, ok := configMapFromTerraform(ctx, plan.Config, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	drain, err := r.client.CreateProjectLogDrain(ctx, plan.Ref.ValueString(), ProjectLogDrainInput{
		Target: plan.Target.ValueString(),
		Config: config,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project log drain", err.Error())
		return
	}
	setProjectLogDrainState(ctx, &plan, drain, config, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectLogDrainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectLogDrainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	drain, err := r.findLogDrain(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project log drain", err.Error())
		return
	}
	previous, ok := optionalConfigMapFromTerraform(ctx, state.Config, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectLogDrainState(ctx, &state, drain, previous, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectLogDrainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectLogDrainResourceModel
	var state projectLogDrainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	config, ok := configMapFromTerraform(ctx, plan.Config, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	err := r.client.DeleteProjectLogDrain(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to replace Supadupa project log drain", err.Error())
		return
	}
	drain, err := r.client.CreateProjectLogDrain(ctx, plan.Ref.ValueString(), ProjectLogDrainInput{
		Target: plan.Target.ValueString(),
		Config: config,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to recreate Supadupa project log drain", err.Error())
		return
	}
	setProjectLogDrainState(ctx, &plan, drain, config, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectLogDrainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectLogDrainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectLogDrain(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project log drain", err.Error())
		return
	}
}

func (r *projectLogDrainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ref, id, ok := strings.Cut(req.ID, "/")
	if !ok {
		ref, id, ok = strings.Cut(req.ID, ":")
	}
	if !ok || strings.TrimSpace(ref) == "" || strings.TrimSpace(id) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use ref/id, for example alpha/01HXYZ.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ref"), strings.TrimSpace(ref))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.TrimSpace(id))...)
}

func (r *projectLogDrainResource) findLogDrain(ctx context.Context, ref string, id string) (ProjectLogDrain, error) {
	drains, err := r.client.ListProjectLogDrains(ctx, ref)
	if err != nil {
		return ProjectLogDrain{}, err
	}
	for _, drain := range drains {
		if drain.ID == id {
			return drain, nil
		}
	}
	return ProjectLogDrain{}, ErrNotFound
}

func optionalConfigMapFromTerraform(ctx context.Context, value types.Map, addError func(string, string)) (map[string]string, bool) {
	if value.IsNull() || value.IsUnknown() {
		return map[string]string{}, true
	}
	return configMapFromTerraform(ctx, value, addError)
}

func setProjectLogDrainState(ctx context.Context, model *projectLogDrainResourceModel, drain ProjectLogDrain, previousConfig map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(drain.ID)
	model.Ref = types.StringValue(drain.ProjectRef)
	model.Target = types.StringValue(drain.Target)
	if drain.CreatedAt.IsZero() {
		model.CreatedAt = types.StringValue("")
	} else {
		model.CreatedAt = types.StringValue(drain.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	value, diags := types.MapValueFrom(ctx, types.StringType, preserveMaskedConfigValues(drain.Config, previousConfig))
	if diags.HasError() {
		addError("Unable to encode config map", diags.Errors()[0].Detail())
		return
	}
	model.Config = value
}

func preserveMaskedConfigValues(remote map[string]string, previous map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range remote {
		if value == "********" {
			if previousValue, ok := previous[key]; ok && strings.TrimSpace(previousValue) != "" {
				merged[key] = previousValue
				continue
			}
		}
		merged[key] = value
	}
	return merged
}
