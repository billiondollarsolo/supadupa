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

type orgTeamMemberResource struct {
	client *Client
}

type orgTeamMemberResourceModel struct {
	ID        types.String `tfsdk:"id"`
	OrgID     types.String `tfsdk:"org_id"`
	TeamID    types.String `tfsdk:"team_id"`
	TeamSlug  types.String `tfsdk:"team_slug"`
	UserID    types.String `tfsdk:"user_id"`
	Email     types.String `tfsdk:"email"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func NewOrgTeamMemberResource() resource.Resource {
	return &orgTeamMemberResource{}
}

func (r *orgTeamMemberResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_team_member"
}

func (r *orgTeamMemberResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa organization team member managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Stable team member ID in the form org_id/team_slug/email.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Control-plane organization ID.",
				PlanModifiers: replace,
			},
			"team_id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Control-plane team ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"team_slug": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Team slug.",
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
				Description:   "Team member email address.",
				PlanModifiers: replace,
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Team membership creation timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *orgTeamMemberResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *orgTeamMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgTeamMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	member, err := r.client.UpsertOrgTeamMember(ctx, plan.OrgID.ValueString(), plan.TeamSlug.ValueString(), OrgTeamMemberInput{Email: plan.Email.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to upsert Supadupa org team member", err.Error())
		return
	}
	setOrgTeamMemberState(&plan, member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgTeamMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgTeamMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	member, err := r.findOrgTeamMember(ctx, state.OrgID.ValueString(), state.TeamSlug.ValueString(), state.Email.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa org team member", err.Error())
		return
	}
	setOrgTeamMemberState(&state, member)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgTeamMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Supadupa org team member updates require replacement", "Team memberships are replace-on-change.")
}

func (r *orgTeamMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgTeamMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteOrgTeamMember(ctx, state.OrgID.ValueString(), state.TeamSlug.ValueString(), state.Email.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa org team member", err.Error())
	}
}

func (r *orgTeamMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use org_id/team_slug/email, for example org_123/platform/dev@example.com.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("org_id"), strings.TrimSpace(parts[0]))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("team_slug"), strings.TrimSpace(parts[1]))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("email"), strings.TrimSpace(parts[2]))...)
}

func (r *orgTeamMemberResource) findOrgTeamMember(ctx context.Context, orgID string, slug string, email string) (OrgTeamMember, error) {
	members, err := r.client.ListOrgTeamMembers(ctx, orgID, slug)
	if err != nil {
		return OrgTeamMember{}, err
	}
	normalized := strings.ToLower(strings.TrimSpace(email))
	for _, member := range members {
		if member.Email == normalized {
			return member, nil
		}
	}
	return OrgTeamMember{}, ErrNotFound
}

func setOrgTeamMemberState(model *orgTeamMemberResourceModel, member OrgTeamMember) {
	model.ID = types.StringValue(member.OrgID + "/" + member.TeamSlug + "/" + member.Email)
	model.OrgID = types.StringValue(member.OrgID)
	model.TeamID = types.StringValue(member.TeamID)
	model.TeamSlug = types.StringValue(member.TeamSlug)
	model.UserID = types.StringValue(member.UserID)
	model.Email = types.StringValue(member.Email)
	model.CreatedAt = optionalTimeString(member.CreatedAt)
}
