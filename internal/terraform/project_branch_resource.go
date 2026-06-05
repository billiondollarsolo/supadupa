package terraform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectBranchResource struct {
	client *Client
}

type projectBranchResourceModel struct {
	ID        types.String `tfsdk:"id"`
	SourceRef types.String `tfsdk:"source_ref"`
	Ref       types.String `tfsdk:"ref"`
	ProjectID types.String `tfsdk:"project_id"`
	Name      types.String `tfsdk:"name"`
	TTLHours  types.Int64  `tfsdk:"ttl_hours"`
	Status    types.String `tfsdk:"status"`
	CreatedAt types.String `tfsdk:"created_at"`
	ExpiresAt types.String `tfsdk:"expires_at"`
}

func NewProjectBranchResource() resource.Resource {
	return &projectBranchResource{}
}

func (r *projectBranchResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_branch"
}

func (r *projectBranchResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa preview branch project cloned from a source project through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Control-plane branch ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"source_ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Source project ref.",
				PlanModifiers: replace,
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Branch project ref.",
				PlanModifiers: replace,
			},
			"project_id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Control-plane project ID created for the branch.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Branch project display name.",
				PlanModifiers: replace,
			},
			"ttl_hours": resourceschema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Default:       int64default.StaticInt64(0),
				Description:   "Optional branch TTL in hours. Zero means no automatic expiry.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Current branch project status.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Branch creation timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"expires_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Branch expiry timestamp, if configured.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *projectBranchResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *projectBranchResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectBranchResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	branch, project, err := r.client.CreateProjectBranch(ctx, plan.SourceRef.ValueString(), ProjectBranchInput{
		Ref:      plan.Ref.ValueString(),
		Name:     plan.Name.ValueString(),
		TTLHours: int(plan.TTLHours.ValueInt64()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project branch", err.Error())
		return
	}
	setProjectBranchState(&plan, branch, project)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectBranchResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectBranchResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, err := r.client.GetProject(ctx, state.Ref.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa branch project", err.Error())
		return
	}
	branch, err := r.findBranch(ctx, state.SourceRef.ValueString(), state.Ref.ValueString())
	if errors.Is(err, ErrNotFound) {
		branch = ProjectBranch{
			ID:               state.ID.ValueString(),
			SourceProjectRef: state.SourceRef.ValueString(),
			ProjectRef:       project.Ref,
			Name:             project.Name,
			Status:           project.Status,
		}
	} else if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project branch", err.Error())
		return
	}
	setProjectBranchState(&state, branch, project)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectBranchResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Supadupa project branch updates require replacement", "Branch source_ref, ref, name, and TTL are replace-on-change.")
}

func (r *projectBranchResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectBranchResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectBranch(ctx, state.SourceRef.ValueString(), state.Ref.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project branch", err.Error())
	}
}

func (r *projectBranchResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	sourceRef, ref, ok := strings.Cut(req.ID, "/")
	if !ok {
		sourceRef, ref, ok = strings.Cut(req.ID, ":")
	}
	if !ok || strings.TrimSpace(sourceRef) == "" || strings.TrimSpace(ref) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use source_ref/ref, for example alpha/alpha-preview.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("source_ref"), strings.TrimSpace(sourceRef))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ref"), strings.TrimSpace(ref))...)
}

func (r *projectBranchResource) findBranch(ctx context.Context, sourceRef string, ref string) (ProjectBranch, error) {
	branches, err := r.client.ListProjectBranches(ctx, sourceRef)
	if err != nil {
		return ProjectBranch{}, err
	}
	normalized := strings.ToLower(strings.TrimSpace(ref))
	for _, branch := range branches {
		if branch.ProjectRef == normalized {
			return branch, nil
		}
	}
	return ProjectBranch{}, ErrNotFound
}

func setProjectBranchState(model *projectBranchResourceModel, branch ProjectBranch, project Project) {
	model.ID = types.StringValue(branch.ID)
	model.SourceRef = types.StringValue(branch.SourceProjectRef)
	model.Ref = types.StringValue(branch.ProjectRef)
	model.ProjectID = types.StringValue(project.ID)
	model.Name = types.StringValue(branch.Name)
	model.Status = types.StringValue(project.Status)
	model.CreatedAt = optionalTimeString(branch.CreatedAt)
	model.ExpiresAt = optionalTimePointerString(branch.ExpiresAt)
}
