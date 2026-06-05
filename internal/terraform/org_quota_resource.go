package terraform

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type orgQuotaResource struct {
	client *Client
}

type orgQuotaResourceModel struct {
	ID           types.String `tfsdk:"id"`
	OrgID        types.String `tfsdk:"org_id"`
	MaxProjects  types.Int64  `tfsdk:"max_projects"`
	MaxCPU       types.Int64  `tfsdk:"max_cpu"`
	MaxRAMMB     types.Int64  `tfsdk:"max_ram_mb"`
	MaxDiskGB    types.Int64  `tfsdk:"max_disk_gb"`
	MaxDiskIOPS  types.Int64  `tfsdk:"max_disk_iops"`
	UsedCPU      types.Int64  `tfsdk:"used_cpu"`
	UsedRAMMB    types.Int64  `tfsdk:"used_ram_mb"`
	UsedDiskGB   types.Int64  `tfsdk:"used_disk_gb"`
	UsedDiskIOPS types.Int64  `tfsdk:"used_disk_iops"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

func NewOrgQuotaResource() resource.Resource {
	return &orgQuotaResource{}
}

func (r *orgQuotaResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_quota"
}

func (r *orgQuotaResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa organization quota managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Stable quota ID in the form org_id/quota.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Control-plane organization ID.",
				PlanModifiers: replace,
			},
			"max_projects":  resourceschema.Int64Attribute{Required: true, Description: "Maximum projects allowed for the organization."},
			"max_cpu":       resourceschema.Int64Attribute{Required: true, Description: "Maximum aggregate CPU units allowed for the organization."},
			"max_ram_mb":    resourceschema.Int64Attribute{Required: true, Description: "Maximum aggregate RAM in MiB allowed for the organization."},
			"max_disk_gb":   resourceschema.Int64Attribute{Required: true, Description: "Maximum aggregate disk in GiB allowed for the organization."},
			"max_disk_iops": resourceschema.Int64Attribute{Required: true, Description: "Maximum aggregate disk IOPS allowed for the organization."},
			"used_cpu": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Current CPU usage counted against the quota.",
			},
			"used_ram_mb": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Current RAM usage counted against the quota.",
			},
			"used_disk_gb": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Current disk usage counted against the quota.",
			},
			"used_disk_iops": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Current disk IOPS usage counted against the quota.",
			},
			"updated_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Quota update timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *orgQuotaResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *orgQuotaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgQuotaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	quota, err := r.client.UpdateOrgQuota(ctx, plan.OrgID.ValueString(), orgQuotaInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa org quota", err.Error())
		return
	}
	setOrgQuotaState(&plan, quota)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgQuotaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgQuotaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	quota, err := r.client.GetOrgQuota(ctx, state.OrgID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa org quota", err.Error())
		return
	}
	setOrgQuotaState(&state, quota)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgQuotaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orgQuotaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	quota, err := r.client.UpdateOrgQuota(ctx, plan.OrgID.ValueString(), orgQuotaInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa org quota", err.Error())
		return
	}
	setOrgQuotaState(&plan, quota)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgQuotaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgQuotaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.client.UpdateOrgQuota(ctx, state.OrgID.ValueString(), OrgQuotaInput{})
	if err != nil {
		resp.Diagnostics.AddError("Unable to reset Supadupa org quota", err.Error())
	}
}

func (r *orgQuotaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use organization ID, for example org_123.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("org_id"), req.ID)...)
}

func orgQuotaInputFromModel(model orgQuotaResourceModel) OrgQuotaInput {
	return OrgQuotaInput{
		MaxProjects: int(model.MaxProjects.ValueInt64()),
		MaxCPU:      int(model.MaxCPU.ValueInt64()),
		MaxRAMMB:    int(model.MaxRAMMB.ValueInt64()),
		MaxDiskGB:   int(model.MaxDiskGB.ValueInt64()),
		MaxDiskIOPS: int(model.MaxDiskIOPS.ValueInt64()),
	}
}

func setOrgQuotaState(model *orgQuotaResourceModel, quota OrgQuota) {
	model.ID = types.StringValue(quota.OrgID + "/quota")
	model.OrgID = types.StringValue(quota.OrgID)
	model.MaxProjects = types.Int64Value(int64(quota.MaxProjects))
	model.MaxCPU = types.Int64Value(int64(quota.MaxCPU))
	model.MaxRAMMB = types.Int64Value(int64(quota.MaxRAMMB))
	model.MaxDiskGB = types.Int64Value(int64(quota.MaxDiskGB))
	model.MaxDiskIOPS = types.Int64Value(int64(quota.MaxDiskIOPS))
	model.UsedCPU = types.Int64Value(int64(quota.Used.CPU))
	model.UsedRAMMB = types.Int64Value(int64(quota.Used.RAMMB))
	model.UsedDiskGB = types.Int64Value(int64(quota.Used.DiskGB))
	model.UsedDiskIOPS = types.Int64Value(int64(quota.Used.DiskIOPS))
	model.UpdatedAt = optionalTimeString(quota.UpdatedAt)
}
