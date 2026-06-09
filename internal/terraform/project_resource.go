package terraform

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectResource struct {
	client *Client
}

type projectResourceModel struct {
	ID            types.String `tfsdk:"id"`
	OrgID         types.String `tfsdk:"org_id"`
	Ref           types.String `tfsdk:"ref"`
	Name          types.String `tfsdk:"name"`
	HostID        types.String `tfsdk:"host_id"`
	Domain        types.String `tfsdk:"domain"`
	StackVersion  types.String `tfsdk:"stack_version"`
	Profile       types.String `tfsdk:"profile"`
	ResourceTier  types.String `tfsdk:"resource_tier"`
	CPU           types.Int64  `tfsdk:"cpu"`
	RAMMB         types.Int64  `tfsdk:"ram_mb"`
	DiskGB        types.Int64  `tfsdk:"disk_gb"`
	EnforceLimits types.Bool   `tfsdk:"enforce_limits"`
	Status        types.String `tfsdk:"status"`
}

func NewProjectResource() resource.Resource {
	return &projectResource{}
}

func (r *projectResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replaceInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	replaceBool := []planmodifier.Bool{boolplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project backed by one isolated upstream Supabase stack.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Control-plane project ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"org_id": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Organization ID that owns the project.",
				PlanModifiers: replace,
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Stable project ref used in endpoints and stack names.",
				PlanModifiers: replace,
			},
			"name": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project display name.",
				PlanModifiers: replace,
			},
			"host_id": resourceschema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Optional host placement ID. Platform defaults apply when omitted.",
				PlanModifiers: replace,
			},
			"domain": resourceschema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Base domain for the project endpoint. Platform defaults apply when omitted.",
				PlanModifiers: replace,
			},
			"stack_version": resourceschema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Supabase stack version. Platform defaults apply when omitted.",
				PlanModifiers: replace,
			},
			"profile": resourceschema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Stack profile, such as essential, full, or orioledb. Platform defaults apply when omitted.",
				PlanModifiers: replace,
			},
			"resource_tier": resourceschema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Resource tier preset, such as small, medium, or large. Platform defaults apply when omitted.",
				PlanModifiers: replace,
			},
			"cpu": resourceschema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Description:   "Exact CPU cores. 0 (or omitted) uses the tier preset.",
				PlanModifiers: replaceInt,
			},
			"ram_mb": resourceschema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Description:   "Exact RAM in MB. 0 (or omitted) uses the tier preset.",
				PlanModifiers: replaceInt,
			},
			"disk_gb": resourceschema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Description:   "Exact disk in GB. 0 (or omitted) uses the tier preset.",
				PlanModifiers: replaceInt,
			},
			"enforce_limits": resourceschema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Apply hard CPU/memory limits to the database container.",
				PlanModifiers: replaceBool,
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Current control-plane project status.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *projectResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	project, err := r.client.CreateProject(ctx, plan.OrgID.ValueString(), CreateProjectRequest{
		Ref:           plan.Ref.ValueString(),
		Name:          plan.Name.ValueString(),
		HostID:        stringValue(plan.HostID),
		Domain:        stringValue(plan.Domain),
		StackVersion:  stringValue(plan.StackVersion),
		Profile:       stringValue(plan.Profile),
		ResourceTier:  stringValue(plan.ResourceTier),
		CPU:           int(plan.CPU.ValueInt64()),
		RAMMB:         int(plan.RAMMB.ValueInt64()),
		DiskGB:        int(plan.DiskGB.ValueInt64()),
		EnforceLimits: plan.EnforceLimits.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project", err.Error())
		return
	}
	setProjectState(&plan, project)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectResourceModel
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
		resp.Diagnostics.AddError("Unable to read Supadupa project", err.Error())
		return
	}
	setProjectState(&state, project)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Supadupa project updates require replacement", "Project fields are modeled as replace-on-change until the Management API exposes project update semantics.")
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProject(ctx, state.Ref.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project", err.Error())
		return
	}
}

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setOnePartImportState(ctx, req.ID, resp, "ref", "Use project ref, for example alpha.")
}

func setProjectState(model *projectResourceModel, project Project) {
	model.ID = types.StringValue(project.ID)
	model.OrgID = types.StringValue(project.OrgID)
	model.Ref = types.StringValue(project.Ref)
	model.Name = types.StringValue(project.Name)
	model.HostID = optionalString(project.Spec.HostID)
	model.Domain = optionalString(project.Spec.Domain)
	model.StackVersion = optionalString(project.Spec.StackVersion)
	model.Profile = optionalString(project.Spec.Profile)
	model.ResourceTier = optionalString(project.Spec.ResourceTier)
	model.CPU = types.Int64Value(int64(project.Spec.CPU))
	model.RAMMB = types.Int64Value(int64(project.Spec.RAMMB))
	model.DiskGB = types.Int64Value(int64(project.Spec.DiskGB))
	model.EnforceLimits = types.BoolValue(project.Spec.EnforceLimits)
	model.Status = types.StringValue(project.Status)
}

func stringValue(value types.String) string {
	if value.IsNull() || value.IsUnknown() {
		return ""
	}
	return value.ValueString()
}

func optionalString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}
