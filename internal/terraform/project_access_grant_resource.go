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

type projectAccessGrantResource struct {
	client *Client
}

type projectAccessGrantResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Ref         types.String `tfsdk:"ref"`
	OrgID       types.String `tfsdk:"org_id"`
	SubjectType types.String `tfsdk:"subject_type"`
	SubjectID   types.String `tfsdk:"subject_id"`
	SubjectName types.String `tfsdk:"subject_name"`
	Role        types.String `tfsdk:"role"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func NewProjectAccessGrantResource() resource.Resource {
	return &projectAccessGrantResource{}
}

func (r *projectAccessGrantResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_access_grant"
}

func (r *projectAccessGrantResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project access grant for users or teams managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Control-plane grant ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replace,
			},
			"org_id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Organization ID owning the project.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"subject_type": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Grant subject type: user or team.",
				PlanModifiers: replace,
			},
			"subject_id": resourceschema.StringAttribute{
				Required:      true,
				Description:   "User email/user ID or team slug/team ID, depending on subject_type.",
				PlanModifiers: replace,
			},
			"subject_name": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Resolved subject display name reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role": resourceschema.StringAttribute{
				Required:    true,
				Description: "Project role: viewer, developer, admin, or owner.",
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Grant creation timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *projectAccessGrantResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectAccessGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectAccessGrantResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	grant, err := r.client.UpsertProjectAccess(ctx, plan.Ref.ValueString(), ProjectAccessGrantInput{
		SubjectType: plan.SubjectType.ValueString(),
		SubjectID:   plan.SubjectID.ValueString(),
		Role:        plan.Role.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to upsert Supadupa project access grant", err.Error())
		return
	}
	setProjectAccessGrantState(&plan, grant)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectAccessGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectAccessGrantResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	grant, err := r.findProjectAccessGrant(ctx, state.Ref.ValueString(), state.SubjectType.ValueString(), state.SubjectID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project access grant", err.Error())
		return
	}
	setProjectAccessGrantState(&state, grant)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectAccessGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectAccessGrantResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	grant, err := r.client.UpsertProjectAccess(ctx, plan.Ref.ValueString(), ProjectAccessGrantInput{
		SubjectType: plan.SubjectType.ValueString(),
		SubjectID:   plan.SubjectID.ValueString(),
		Role:        plan.Role.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project access grant", err.Error())
		return
	}
	setProjectAccessGrantState(&plan, grant)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectAccessGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectAccessGrantResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectAccess(ctx, state.Ref.ValueString(), state.SubjectType.ValueString(), state.SubjectID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project access grant", err.Error())
	}
}

func (r *projectAccessGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setThreePartImportState(ctx, req.ID, resp, "ref", "subject_type", "subject_id", "Use ref/subject_type/subject_id, for example alpha/team/platform.", false)
}

func (r *projectAccessGrantResource) findProjectAccessGrant(ctx context.Context, ref string, subjectType string, subjectID string) (ProjectAccessGrant, error) {
	grants, err := r.client.ListProjectAccess(ctx, ref)
	if err != nil {
		return ProjectAccessGrant{}, err
	}
	normalizedType := strings.ToLower(strings.TrimSpace(subjectType))
	normalizedID := strings.ToLower(strings.TrimSpace(subjectID))
	return findInList(grants, func(grant ProjectAccessGrant) bool {
		return grant.SubjectType == normalizedType && strings.ToLower(grant.SubjectID) == normalizedID
	})
}

func setProjectAccessGrantState(model *projectAccessGrantResourceModel, grant ProjectAccessGrant) {
	model.ID = types.StringValue(grant.ID)
	model.Ref = types.StringValue(grant.ProjectRef)
	model.OrgID = types.StringValue(grant.OrgID)
	model.SubjectType = types.StringValue(grant.SubjectType)
	model.SubjectID = types.StringValue(grant.SubjectID)
	model.SubjectName = types.StringValue(grant.SubjectName)
	model.Role = types.StringValue(grant.Role)
	model.CreatedAt = optionalTimeString(grant.CreatedAt)
}
