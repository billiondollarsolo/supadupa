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

type projectNetworkConnectionResource struct {
	client *Client
}

type projectNetworkConnectionResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Ref        types.String `tfsdk:"ref"`
	Name       types.String `tfsdk:"name"`
	Type       types.String `tfsdk:"type"`
	Provider   types.String `tfsdk:"network_provider"`
	Region     types.String `tfsdk:"region"`
	CIDRs      types.List   `tfsdk:"cidrs"`
	EndpointID types.String `tfsdk:"endpoint_id"`
	Config     types.Map    `tfsdk:"config"`
	Status     types.String `tfsdk:"status"`
	Message    types.String `tfsdk:"message"`
	CreatedAt  types.String `tfsdk:"created_at"`
	UpdatedAt  types.String `tfsdk:"updated_at"`
}

func NewProjectNetworkConnectionResource() resource.Resource {
	return &projectNetworkConnectionResource{}
}

func (r *projectNetworkConnectionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_network_connection"
}

func (r *projectNetworkConnectionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project private network connection declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated network connection ID.",
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
				Description: "Connection name. Must be 3-64 lowercase letters, numbers, or dashes.",
			},
			"type": resourceschema.StringAttribute{
				Required:    true,
				Description: "Connection type: privatelink, vpc_peering, private_endpoint, wireguard, or operator_network.",
			},
			"network_provider": resourceschema.StringAttribute{
				Required:    true,
				Description: "Network provider: aws, gcp, azure, custom, or operator.",
			},
			"region": resourceschema.StringAttribute{
				Optional:    true,
				Description: "Provider region for the private connectivity request.",
			},
			"cidrs": resourceschema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Allowed private or source CIDRs for the connection.",
			},
			"endpoint_id": resourceschema.StringAttribute{
				Optional:    true,
				Description: "Provider endpoint, peering, or tunnel identifier.",
			},
			"config": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "Provider-specific key/value configuration. Sensitive values must use secret:// handles.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Connection status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable connection status message.",
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

func (r *projectNetworkConnectionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectNetworkConnectionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	requireResourceReplaceOnUpdate(ctx, req, resp, "id")
}

func (r *projectNetworkConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectNetworkConnectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := networkConnectionInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	connection, err := r.client.CreateProjectNetworkConnection(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project network connection", err.Error())
		return
	}
	setProjectNetworkConnectionState(ctx, &plan, connection, input.Config, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectNetworkConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectNetworkConnectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	connection, err := r.findNetworkConnection(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project network connection", err.Error())
		return
	}
	previous, ok := optionalConfigMapFromTerraform(ctx, state.Config, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectNetworkConnectionState(ctx, &state, connection, previous, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectNetworkConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	reportUnsupportedInPlaceUpdate(resp, "Supadupa project network connection")
}

func (r *projectNetworkConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectNetworkConnectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectNetworkConnection(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project network connection", err.Error())
		return
	}
}

func (r *projectNetworkConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "id", "Use ref/id, for example alpha/net_123.")
}

func (r *projectNetworkConnectionResource) findNetworkConnection(ctx context.Context, ref string, id string) (ProjectNetworkConnection, error) {
	connections, err := r.client.ListProjectNetworkConnections(ctx, ref)
	if err != nil {
		return ProjectNetworkConnection{}, err
	}
	return findInList(connections, func(connection ProjectNetworkConnection) bool { return connection.ID == id })
}

func networkConnectionInputFromModel(ctx context.Context, model projectNetworkConnectionResourceModel, addError func(string, string)) (ProjectNetworkConnectionInput, bool) {
	cidrs, ok := stringListFromTerraform(ctx, model.CIDRs, "Invalid cidrs list", addError)
	if !ok {
		return ProjectNetworkConnectionInput{}, false
	}
	config, ok := configMapFromTerraform(ctx, model.Config, addError)
	if !ok {
		return ProjectNetworkConnectionInput{}, false
	}
	return ProjectNetworkConnectionInput{
		Name:       model.Name.ValueString(),
		Type:       model.Type.ValueString(),
		Provider:   model.Provider.ValueString(),
		Region:     model.Region.ValueString(),
		CIDRs:      cidrs,
		EndpointID: model.EndpointID.ValueString(),
		Config:     config,
	}, true
}

func stringListFromTerraform(ctx context.Context, value types.List, title string, addError func(string, string)) ([]string, bool) {
	out := []string{}
	diags := value.ElementsAs(ctx, &out, false)
	if diags.HasError() {
		addError(title, diags.Errors()[0].Detail())
		return nil, false
	}
	return out, true
}

func setProjectNetworkConnectionState(ctx context.Context, model *projectNetworkConnectionResourceModel, connection ProjectNetworkConnection, previousConfig map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(connection.ID)
	model.Ref = types.StringValue(connection.ProjectRef)
	model.Name = types.StringValue(connection.Name)
	model.Type = types.StringValue(connection.Type)
	model.Provider = types.StringValue(connection.Provider)
	model.Region = optionalStringValue(connection.Region)
	model.EndpointID = optionalStringValue(connection.EndpointID)
	model.Status = types.StringValue(connection.Status)
	model.Message = optionalStringValue(connection.Message)
	model.CreatedAt = optionalTimeString(connection.CreatedAt)
	model.UpdatedAt = optionalTimeString(connection.UpdatedAt)

	cidrs, ok := stringListStateValue(ctx, "cidrs", connection.CIDRs, addError)
	if !ok {
		return
	}
	model.CIDRs = cidrs
	config, ok := sensitiveStringMapStateValue(ctx, "config", connection.Config, previousConfig, addError)
	if !ok {
		return
	}
	model.Config = config
}
