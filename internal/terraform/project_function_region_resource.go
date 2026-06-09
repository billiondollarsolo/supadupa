package terraform

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectFunctionRegionResource struct {
	client *Client
}

type projectFunctionRegionResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Ref           types.String `tfsdk:"ref"`
	FunctionName  types.String `tfsdk:"function_name"`
	HostID        types.String `tfsdk:"host_id"`
	Region        types.String `tfsdk:"region"`
	RoutingPolicy types.String `tfsdk:"routing_policy"`
	InvocationURL types.String `tfsdk:"invocation_url"`
	Status        types.String `tfsdk:"status"`
	Message       types.String `tfsdk:"message"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func NewProjectFunctionRegionResource() resource.Resource {
	return &projectFunctionRegionResource{}
}

func (r *projectFunctionRegionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_function_region"
}

func (r *projectFunctionRegionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa Edge Function regional invocation declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated function region declaration ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replace,
			},
			"function_name": resourceschema.StringAttribute{
				Required:    true,
				Description: "Deployed Edge Function name.",
			},
			"host_id": resourceschema.StringAttribute{
				Optional:    true,
				Description: "Optional host ID that should serve this regional function target.",
			},
			"region": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("local"),
				Description: "Region label for the function target.",
			},
			"routing_policy": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("nearest"),
				Description: "Routing policy: nearest, primary, or weighted.",
			},
			"invocation_url": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated internal invocation URL for this regional target.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Function region status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable function region status message.",
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

func (r *projectFunctionRegionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectFunctionRegionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	requireResourceReplaceOnUpdate(ctx, req, resp, "id")
}

func (r *projectFunctionRegionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectFunctionRegionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	region, err := r.client.CreateProjectFunctionRegion(ctx, plan.Ref.ValueString(), functionRegionInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project function region", err.Error())
		return
	}
	setProjectFunctionRegionState(&plan, region)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectFunctionRegionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectFunctionRegionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	region, err := r.findFunctionRegion(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project function region", err.Error())
		return
	}
	setProjectFunctionRegionState(&state, region)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectFunctionRegionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	reportUnsupportedInPlaceUpdate(resp, "Supadupa project function region")
}

func (r *projectFunctionRegionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectFunctionRegionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectFunctionRegion(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project function region", err.Error())
		return
	}
}

func (r *projectFunctionRegionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "id", "Use ref/id, for example alpha/region_123.")
}

func (r *projectFunctionRegionResource) findFunctionRegion(ctx context.Context, ref string, id string) (ProjectFunctionRegion, error) {
	regions, err := r.client.ListProjectFunctionRegions(ctx, ref)
	if err != nil {
		return ProjectFunctionRegion{}, err
	}
	return findInList(regions, func(region ProjectFunctionRegion) bool { return region.ID == id })
}

func functionRegionInputFromModel(model projectFunctionRegionResourceModel) ProjectFunctionRegionInput {
	return ProjectFunctionRegionInput{
		FunctionName:  model.FunctionName.ValueString(),
		HostID:        model.HostID.ValueString(),
		Region:        model.Region.ValueString(),
		RoutingPolicy: model.RoutingPolicy.ValueString(),
	}
}

func setProjectFunctionRegionState(model *projectFunctionRegionResourceModel, region ProjectFunctionRegion) {
	model.ID = types.StringValue(region.ID)
	model.Ref = types.StringValue(region.ProjectRef)
	model.FunctionName = types.StringValue(region.FunctionName)
	model.HostID = optionalStringValue(region.HostID)
	model.Region = types.StringValue(region.Region)
	model.RoutingPolicy = types.StringValue(region.RoutingPolicy)
	model.InvocationURL = types.StringValue(region.InvocationURL)
	model.Status = types.StringValue(region.Status)
	model.Message = optionalStringValue(region.Message)
	model.CreatedAt = optionalTimeString(region.CreatedAt)
	model.UpdatedAt = optionalTimeString(region.UpdatedAt)
}
