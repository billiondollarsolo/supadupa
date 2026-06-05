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

type orgTeamResource struct {
	client *Client
}

type orgTeamResourceModel struct {
	ID        types.String `tfsdk:"id"`
	OrgID     types.String `tfsdk:"org_id"`
	Name      types.String `tfsdk:"name"`
	Slug      types.String `tfsdk:"slug"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func NewOrgTeamResource() resource.Resource {
	return &orgTeamResource{}
}

func (r *orgTeamResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_team"
}

func (r *orgTeamResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa organization team managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Control-plane team ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Control-plane organization ID.",
				PlanModifiers: replace,
			},
			"name": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Team display name.",
				PlanModifiers: replace,
			},
			"slug": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Team slug used in project access grants.",
				PlanModifiers: replace,
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Team creation timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *orgTeamResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *orgTeamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgTeamResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	team, err := r.client.CreateOrgTeam(ctx, plan.OrgID.ValueString(), OrgTeamInput{Name: plan.Name.ValueString(), Slug: plan.Slug.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa org team", err.Error())
		return
	}
	setOrgTeamState(&plan, team)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgTeamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgTeamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	team, err := r.findOrgTeam(ctx, state.OrgID.ValueString(), state.Slug.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa org team", err.Error())
		return
	}
	setOrgTeamState(&state, team)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgTeamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Supadupa org team updates require replacement", "Team org_id, name, and slug are replace-on-change.")
}

func (r *orgTeamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgTeamResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteOrgTeam(ctx, state.OrgID.ValueString(), state.Slug.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa org team", err.Error())
	}
}

func (r *orgTeamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	orgID, slug, ok := strings.Cut(req.ID, "/")
	if !ok {
		orgID, slug, ok = strings.Cut(req.ID, ":")
	}
	if !ok || strings.TrimSpace(orgID) == "" || strings.TrimSpace(slug) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use org_id/slug, for example org_123/platform.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("org_id"), strings.TrimSpace(orgID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), strings.TrimSpace(slug))...)
}

func (r *orgTeamResource) findOrgTeam(ctx context.Context, orgID string, slug string) (OrgTeam, error) {
	teams, err := r.client.ListOrgTeams(ctx, orgID)
	if err != nil {
		return OrgTeam{}, err
	}
	normalized := strings.ToLower(strings.TrimSpace(slug))
	for _, team := range teams {
		if team.Slug == normalized {
			return team, nil
		}
	}
	return OrgTeam{}, ErrNotFound
}

func setOrgTeamState(model *orgTeamResourceModel, team OrgTeam) {
	model.ID = types.StringValue(team.ID)
	model.OrgID = types.StringValue(team.OrgID)
	model.Name = types.StringValue(team.Name)
	model.Slug = types.StringValue(team.Slug)
	model.CreatedAt = optionalTimeString(team.CreatedAt)
}
