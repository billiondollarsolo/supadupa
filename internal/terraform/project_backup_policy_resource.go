package terraform

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectBackupPolicyResource struct {
	client *Client
}

type projectBackupPolicyResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Ref       types.String `tfsdk:"ref"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	Schedule  types.String `tfsdk:"schedule"`
	Kind      types.String `tfsdk:"kind"`
	LastRunAt types.String `tfsdk:"last_run_at"`
	NextRunAt types.String `tfsdk:"next_run_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func NewProjectBackupPolicyResource() resource.Resource {
	return &projectBackupPolicyResource{}
}

func (r *projectBackupPolicyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_backup_policy"
}

func (r *projectBackupPolicyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project scheduled backup policy managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Stable backup policy ID in the form ref/backup-policy.",
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
				Default:     booldefault.StaticBool(true),
				Description: "Whether scheduled logical backups are enabled.",
			},
			"schedule": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("daily"),
				Description: "Backup schedule: daily or hourly.",
			},
			"kind": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("logical"),
				Description: "Backup kind. The MVP supports logical backups.",
			},
			"last_run_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Last successful scheduled backup timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"next_run_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Next scheduled backup timestamp.",
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

func (r *projectBackupPolicyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *projectBackupPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectBackupPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := r.client.UpdateProjectBackupPolicy(ctx, plan.Ref.ValueString(), projectBackupPolicyInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project backup policy", err.Error())
		return
	}
	setProjectBackupPolicyState(&plan, policy)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectBackupPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectBackupPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := r.client.GetProjectBackupPolicy(ctx, state.Ref.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project backup policy", err.Error())
		return
	}
	setProjectBackupPolicyState(&state, policy)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectBackupPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectBackupPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := r.client.UpdateProjectBackupPolicy(ctx, plan.Ref.ValueString(), projectBackupPolicyInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project backup policy", err.Error())
		return
	}
	setProjectBackupPolicyState(&plan, policy)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectBackupPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectBackupPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.client.UpdateProjectBackupPolicy(ctx, state.Ref.ValueString(), defaultProjectBackupPolicyInput())
	if err != nil {
		resp.Diagnostics.AddError("Unable to reset Supadupa project backup policy", err.Error())
	}
}

func (r *projectBackupPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use project ref, for example alpha.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ref"), req.ID)...)
}

func projectBackupPolicyInputFromModel(model projectBackupPolicyResourceModel) ProjectBackupPolicyInput {
	return ProjectBackupPolicyInput{
		Enabled:  model.Enabled.ValueBool(),
		Schedule: model.Schedule.ValueString(),
		Kind:     model.Kind.ValueString(),
	}
}

func defaultProjectBackupPolicyInput() ProjectBackupPolicyInput {
	return ProjectBackupPolicyInput{Enabled: true, Schedule: "daily", Kind: "logical"}
}

func setProjectBackupPolicyState(model *projectBackupPolicyResourceModel, policy ProjectBackupPolicy) {
	model.ID = types.StringValue(policy.ProjectRef + "/backup-policy")
	model.Ref = types.StringValue(policy.ProjectRef)
	model.Enabled = types.BoolValue(policy.Enabled)
	model.Schedule = types.StringValue(policy.Schedule)
	model.Kind = types.StringValue(policy.Kind)
	model.LastRunAt = optionalTimePointerString(policy.LastRunAt)
	model.NextRunAt = optionalTimePointerString(policy.NextRunAt)
	model.UpdatedAt = optionalTimeString(policy.UpdatedAt)
}
