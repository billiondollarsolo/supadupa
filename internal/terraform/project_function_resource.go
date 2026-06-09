package terraform

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectFunctionResource struct {
	client *Client
}

type projectFunctionResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Ref         types.String `tfsdk:"ref"`
	Name        types.String `tfsdk:"name"`
	Version     types.Int64  `tfsdk:"version"`
	Entrypoint  types.String `tfsdk:"entrypoint"`
	VerifyJWT   types.Bool   `tfsdk:"verify_jwt"`
	Source      types.String `tfsdk:"source"`
	SourceHash  types.String `tfsdk:"source_hash"`
	SourceBytes types.Int64  `tfsdk:"source_bytes"`
	Secrets     types.Map    `tfsdk:"secrets"`
	Status      types.String `tfsdk:"status"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func NewProjectFunctionResource() resource.Resource {
	return &projectFunctionResource{}
}

func (r *projectFunctionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_function"
}

func (r *projectFunctionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa Edge Function deployment managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated Edge Function deployment record ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replace,
			},
			"name": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Edge Function name. Must be 3-64 lowercase letters, numbers, or dashes.",
				PlanModifiers: replace,
			},
			"version": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Monotonic function deployment version reported by the control plane.",
			},
			"entrypoint": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("index.ts"),
				Description: "Function source entrypoint.",
			},
			"verify_jwt": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the Edge Runtime should require JWT verification for invocations.",
			},
			"source": resourceschema.StringAttribute{
				Required:    true,
				Description: "Function source text deployed to the Edge Runtime.",
			},
			"source_hash": resourceschema.StringAttribute{
				Computed:    true,
				Description: "SHA-256 hash of the deployed function source reported by the control plane.",
			},
			"source_bytes": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Deployed function source size in bytes reported by the control plane.",
			},
			"secrets": resourceschema.MapAttribute{
				Optional:    true,
				Computed:    true,
				Sensitive:   true,
				ElementType: types.StringType,
				Description: "Function secret environment values. Responses are masked; Terraform preserves configured values in state.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Function deployment status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Update timestamp reported by the control plane.",
			},
		},
	}
}

func (r *projectFunctionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectFunctionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectFunctionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := functionInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	function, err := r.client.DeployProjectFunction(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to deploy Supadupa project function", err.Error())
		return
	}
	setProjectFunctionState(ctx, &plan, function, input.Source, input.Secrets, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectFunctionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectFunctionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	function, err := r.findFunction(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project function", err.Error())
		return
	}
	previousSecrets, ok := optionalConfigMapFromTerraform(ctx, state.Secrets, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectFunctionState(ctx, &state, function, previousSensitiveString(state.Source), previousSecrets, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectFunctionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectFunctionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := functionInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	function, err := r.client.DeployProjectFunction(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to redeploy Supadupa project function", err.Error())
		return
	}
	setProjectFunctionState(ctx, &plan, function, input.Source, input.Secrets, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectFunctionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectFunctionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectFunction(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project function", err.Error())
		return
	}
}

func (r *projectFunctionResource) findFunction(ctx context.Context, ref string, name string) (ProjectFunction, error) {
	functions, err := r.client.ListProjectFunctions(ctx, ref)
	if err != nil {
		return ProjectFunction{}, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	return findInList(functions, func(function ProjectFunction) bool { return function.Name == normalized })
}

func functionInputFromModel(ctx context.Context, model projectFunctionResourceModel, addError func(string, string)) (ProjectFunctionInput, bool) {
	secrets, ok := optionalConfigMapFromTerraform(ctx, model.Secrets, addError)
	if !ok {
		return ProjectFunctionInput{}, false
	}
	return ProjectFunctionInput{
		Name:       model.Name.ValueString(),
		Entrypoint: model.Entrypoint.ValueString(),
		VerifyJWT:  model.VerifyJWT.ValueBool(),
		Source:     model.Source.ValueString(),
		Secrets:    secrets,
	}, true
}

func setProjectFunctionState(ctx context.Context, model *projectFunctionResourceModel, function ProjectFunction, source string, previousSecrets map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(function.ID)
	model.Ref = types.StringValue(function.ProjectRef)
	model.Name = types.StringValue(function.Name)
	model.Version = types.Int64Value(int64(function.Version))
	model.Entrypoint = types.StringValue(function.Entrypoint)
	model.VerifyJWT = types.BoolValue(function.VerifyJWT)
	model.Source = types.StringValue(source)
	model.SourceHash = types.StringValue(function.SourceHash)
	model.SourceBytes = types.Int64Value(int64(function.SourceBytes))
	model.Status = types.StringValue(function.Status)
	model.CreatedAt = optionalTimeString(function.CreatedAt)
	model.UpdatedAt = optionalTimeString(function.UpdatedAt)

	secrets, ok := sensitiveStringMapStateValue(ctx, "function secrets", function.Secrets, previousSecrets, addError)
	if !ok {
		return
	}
	model.Secrets = secrets
}
