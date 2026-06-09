package terraform

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type orgMemberResource struct {
	client *Client
}

type orgMemberResourceModel struct {
	ID        types.String `tfsdk:"id"`
	OrgID     types.String `tfsdk:"org_id"`
	UserID    types.String `tfsdk:"user_id"`
	Email     types.String `tfsdk:"email"`
	Role      types.String `tfsdk:"role"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func NewOrgMemberResource() resource.Resource {
	return &orgMemberResource{}
}

func (r *orgMemberResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_member"
}

func (r *orgMemberResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa organization member and platform role managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Stable member ID in the form org_id/email.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Control-plane organization ID.",
				PlanModifiers: replace,
			},
			"user_id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Control-plane user ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"email": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Member email address.",
				PlanModifiers: replace,
			},
			"role": resourceschema.StringAttribute{
				Required:    true,
				Description: "Organization role: viewer, developer, admin, or owner.",
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Membership creation timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *orgMemberResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *orgMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	member, err := r.client.UpsertOrgMember(ctx, plan.OrgID.ValueString(), OrgMemberInput{Email: plan.Email.ValueString(), Role: plan.Role.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to upsert Supadupa org member", err.Error())
		return
	}
	setOrgMemberState(&plan, member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	member, err := r.findOrgMember(ctx, state.OrgID.ValueString(), state.Email.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa org member", err.Error())
		return
	}
	setOrgMemberState(&state, member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orgMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	member, err := r.client.UpsertOrgMember(ctx, plan.OrgID.ValueString(), OrgMemberInput{Email: plan.Email.ValueString(), Role: plan.Role.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa org member", err.Error())
		return
	}
	setOrgMemberState(&plan, member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteOrgMember(ctx, state.OrgID.ValueString(), state.Email.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa org member", err.Error())
	}
}

func (r *orgMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "org_id", "email", "Use org_id/email, for example org_123/admin@example.com.")
}

func (r *orgMemberResource) findOrgMember(ctx context.Context, orgID string, email string) (OrgMember, error) {
	members, err := r.client.ListOrgMembers(ctx, orgID)
	if err != nil {
		return OrgMember{}, err
	}
	normalized := strings.ToLower(strings.TrimSpace(email))
	return findInList(members, func(member OrgMember) bool { return member.Email == normalized })
}

func setOrgMemberState(model *orgMemberResourceModel, member OrgMember) {
	model.ID = types.StringValue(member.OrgID + "/" + member.Email)
	model.OrgID = types.StringValue(member.OrgID)
	model.UserID = types.StringValue(member.UserID)
	model.Email = types.StringValue(member.Email)
	model.Role = types.StringValue(member.Role)
	model.CreatedAt = optionalTimeString(member.CreatedAt)
}
