package terraform

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectPITRPolicyResource struct {
	client *Client
}

type projectPITRPolicyResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Ref           types.String `tfsdk:"ref"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	ArchiveBucket types.String `tfsdk:"archive_bucket"`
	RetentionDays types.Int64  `tfsdk:"retention_days"`
	LastArchiveAt types.String `tfsdk:"last_archive_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func NewProjectPITRPolicyResource() resource.Resource {
	return &projectPITRPolicyResource{}
}

func (r *projectPITRPolicyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_pitr_policy"
}

func (r *projectPITRPolicyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project point-in-time recovery policy managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Stable PITR policy ID in the form ref/pitr-policy.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replace,
			},
			"enabled": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether WAL archiving for PITR is enabled.",
			},
			"archive_bucket": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Object storage URI for WAL archives. Required when enabled is true.",
			},
			"retention_days": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(7),
				Description: "WAL retention in days, from 1 to 35.",
			},
			"last_archive_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Last WAL archive timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Policy update timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *projectPITRPolicyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *projectPITRPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectPITRPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := r.client.UpdateProjectPITRPolicy(ctx, plan.Ref.ValueString(), projectPITRPolicyInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project PITR policy", err.Error())
		return
	}
	setProjectPITRPolicyState(&plan, policy)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectPITRPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectPITRPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := r.client.GetProjectPITRPolicy(ctx, state.Ref.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project PITR policy", err.Error())
		return
	}
	setProjectPITRPolicyState(&state, policy)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectPITRPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectPITRPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := r.client.UpdateProjectPITRPolicy(ctx, plan.Ref.ValueString(), projectPITRPolicyInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project PITR policy", err.Error())
		return
	}
	setProjectPITRPolicyState(&plan, policy)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectPITRPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectPITRPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.client.UpdateProjectPITRPolicy(ctx, state.Ref.ValueString(), defaultProjectPITRPolicyInput())
	if err != nil {
		resp.Diagnostics.AddError("Unable to reset Supadupa project PITR policy", err.Error())
	}
}

func (r *projectPITRPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use project ref, for example alpha.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ref"), req.ID)...)
}

func projectPITRPolicyInputFromModel(model projectPITRPolicyResourceModel) ProjectPITRPolicyInput {
	return ProjectPITRPolicyInput{
		Enabled:       model.Enabled.ValueBool(),
		ArchiveBucket: model.ArchiveBucket.ValueString(),
		RetentionDays: int(model.RetentionDays.ValueInt64()),
	}
}

func defaultProjectPITRPolicyInput() ProjectPITRPolicyInput {
	return ProjectPITRPolicyInput{Enabled: false, ArchiveBucket: "", RetentionDays: 7}
}

func setProjectPITRPolicyState(model *projectPITRPolicyResourceModel, policy ProjectPITRPolicy) {
	model.ID = types.StringValue(policy.ProjectRef + "/pitr-policy")
	model.Ref = types.StringValue(policy.ProjectRef)
	model.Enabled = types.BoolValue(policy.Enabled)
	model.ArchiveBucket = types.StringValue(policy.ArchiveBucket)
	model.RetentionDays = types.Int64Value(int64(policy.RetentionDays))
	model.LastArchiveAt = optionalTimePointerString(policy.LastArchiveAt)
	model.UpdatedAt = optionalTimeString(policy.UpdatedAt)
}

func optionalTimePointerString(value *time.Time) types.String {
	if value == nil || value.IsZero() {
		return types.StringValue("")
	}
	return types.StringValue(value.Format("2006-01-02T15:04:05Z07:00"))
}
