package terraform

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type hostResource struct {
	client *Client
}

type hostResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Address          types.String `tfsdk:"address"`
	CapacityCPU      types.Int64  `tfsdk:"capacity_cpu"`
	CapacityRAMMB    types.Int64  `tfsdk:"capacity_ram_mb"`
	CapacityDiskGB   types.Int64  `tfsdk:"capacity_disk_gb"`
	CapacityDiskIOPS types.Int64  `tfsdk:"capacity_disk_iops"`
	CapacityProjects types.Int64  `tfsdk:"capacity_projects"`
	UsedCPU          types.Int64  `tfsdk:"used_cpu"`
	UsedRAMMB        types.Int64  `tfsdk:"used_ram_mb"`
	UsedDiskGB       types.Int64  `tfsdk:"used_disk_gb"`
	UsedDiskIOPS     types.Int64  `tfsdk:"used_disk_iops"`
	UsedProjects     types.Int64  `tfsdk:"used_projects"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func NewHostResource() resource.Resource {
	return &hostResource{}
}

func (r *hostResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_host"
}

func (r *hostResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replaceInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa control-plane host capacity target.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Control-plane host ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Operator-facing host name.",
				PlanModifiers: replaceString,
			},
			"address": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Host address used by the control plane.",
				PlanModifiers: replaceString,
			},
			"capacity_cpu": resourceschema.Int64Attribute{
				Required:      true,
				Description:   "CPU units available for project and replica placement.",
				PlanModifiers: replaceInt,
			},
			"capacity_ram_mb": resourceschema.Int64Attribute{
				Required:      true,
				Description:   "RAM capacity in MiB.",
				PlanModifiers: replaceInt,
			},
			"capacity_disk_gb": resourceschema.Int64Attribute{
				Required:      true,
				Description:   "Disk capacity in GiB.",
				PlanModifiers: replaceInt,
			},
			"capacity_disk_iops": resourceschema.Int64Attribute{
				Required:      true,
				Description:   "Disk IOPS capacity.",
				PlanModifiers: replaceInt,
			},
			"capacity_projects": resourceschema.Int64Attribute{
				Required:      true,
				Description:   "Number of project or replica slots available on the host.",
				PlanModifiers: replaceInt,
			},
			"used_cpu": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "CPU units currently reserved on the host.",
			},
			"used_ram_mb": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "RAM currently reserved on the host in MiB.",
			},
			"used_disk_gb": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Disk currently reserved on the host in GiB.",
			},
			"used_disk_iops": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Disk IOPS currently reserved on the host.",
			},
			"used_projects": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Project or replica slots currently reserved on the host.",
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Host creation timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *hostResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *hostResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hostResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	host, err := r.client.CreateHost(ctx, hostInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa host", err.Error())
		return
	}
	setHostState(&plan, host)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *hostResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state hostResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	host, err := r.client.GetHost(ctx, state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa host", err.Error())
		return
	}
	setHostState(&state, host)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *hostResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unable to update Supadupa host", "Host fields are immutable in the current Management API; Terraform will replace the resource when they change.")
}

func (r *hostResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state hostResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteHost(ctx, state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa host", err.Error())
	}
}

func (r *hostResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func hostInputFromModel(model hostResourceModel) HostInput {
	return HostInput{
		Name:    model.Name.ValueString(),
		Address: model.Address.ValueString(),
		Capacity: HostCapacity{
			CPU:      int(model.CapacityCPU.ValueInt64()),
			RAMMB:    int(model.CapacityRAMMB.ValueInt64()),
			DiskGB:   int(model.CapacityDiskGB.ValueInt64()),
			DiskIOPS: int(model.CapacityDiskIOPS.ValueInt64()),
			Project:  int(model.CapacityProjects.ValueInt64()),
		},
	}
}

func setHostState(model *hostResourceModel, host Host) {
	model.ID = types.StringValue(host.ID)
	model.Name = types.StringValue(host.Name)
	model.Address = types.StringValue(host.Address)
	model.CapacityCPU = types.Int64Value(int64(host.Capacity.CPU))
	model.CapacityRAMMB = types.Int64Value(int64(host.Capacity.RAMMB))
	model.CapacityDiskGB = types.Int64Value(int64(host.Capacity.DiskGB))
	model.CapacityDiskIOPS = types.Int64Value(int64(host.Capacity.DiskIOPS))
	model.CapacityProjects = types.Int64Value(int64(host.Capacity.Project))
	model.UsedCPU = types.Int64Value(int64(host.Used.CPU))
	model.UsedRAMMB = types.Int64Value(int64(host.Used.RAMMB))
	model.UsedDiskGB = types.Int64Value(int64(host.Used.DiskGB))
	model.UsedDiskIOPS = types.Int64Value(int64(host.Used.DiskIOPS))
	model.UsedProjects = types.Int64Value(int64(host.Used.Project))
	model.CreatedAt = optionalTimeString(host.CreatedAt)
}
