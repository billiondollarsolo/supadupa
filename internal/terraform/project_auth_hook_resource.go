package terraform

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectAuthHookResource struct {
	client *Client
}

type projectAuthHookResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Ref           types.String `tfsdk:"ref"`
	HookType      types.String `tfsdk:"hook_type"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	TargetURI     types.String `tfsdk:"target_uri"`
	EdgeFunction  types.String `tfsdk:"edge_function"`
	SecretHandle  types.String `tfsdk:"secret_handle"`
	Headers       types.Map    `tfsdk:"headers"`
	TimeoutMS     types.Int64  `tfsdk:"timeout_ms"`
	RetryAttempts types.Int64  `tfsdk:"retry_attempts"`
	Status        types.String `tfsdk:"status"`
	Message       types.String `tfsdk:"message"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func NewProjectAuthHookResource() resource.Resource {
	return &projectAuthHookResource{}
}

func (r *projectAuthHookResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_auth_hook"
}

func (r *projectAuthHookResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project Auth Hook declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated auth hook record ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replace,
			},
			"hook_type": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Auth Hook type, for example custom_access_token, before_user_created, send_sms, or send_email.",
				PlanModifiers: replace,
			},
			"enabled": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the Auth Hook should be active.",
			},
			"target_uri": resourceschema.StringAttribute{
				Optional:    true,
				Description: "HTTPS endpoint invoked by the Auth Hook.",
			},
			"edge_function": resourceschema.StringAttribute{
				Optional:    true,
				Description: "Project Edge Function target used instead of target_uri.",
			},
			"secret_handle": resourceschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "secret:// handle used by the Auth Hook runtime.",
			},
			"headers": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "Headers attached to outbound Auth Hook requests. Sensitive headers must use secret:// handles.",
			},
			"timeout_ms": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(5000),
				Description: "Request timeout in milliseconds, between 100 and 30000.",
			},
			"retry_attempts": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
				Description: "Retry attempts, between 0 and 5.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Auth Hook status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable Auth Hook status message.",
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
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *projectAuthHookResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectAuthHookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectAuthHookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := authHookInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	hook, err := r.client.CreateProjectAuthHook(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project Auth Hook", err.Error())
		return
	}
	setProjectAuthHookState(ctx, &plan, hook, input.SecretHandle, input.Headers, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectAuthHookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectAuthHookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	hook, err := r.findAuthHook(ctx, state.Ref.ValueString(), state.HookType.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project Auth Hook", err.Error())
		return
	}
	previousHeaders, ok := optionalConfigMapFromTerraform(ctx, state.Headers, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectAuthHookState(ctx, &state, hook, previousSensitiveString(state.SecretHandle), previousHeaders, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectAuthHookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectAuthHookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := authHookInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	hook, err := r.client.CreateProjectAuthHook(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project Auth Hook", err.Error())
		return
	}
	setProjectAuthHookState(ctx, &plan, hook, input.SecretHandle, input.Headers, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectAuthHookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectAuthHookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectAuthHook(ctx, state.Ref.ValueString(), state.HookType.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project Auth Hook", err.Error())
		return
	}
}

func (r *projectAuthHookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "hook_type", "Use ref/hook_type, for example alpha/custom_access_token.")
}

func (r *projectAuthHookResource) findAuthHook(ctx context.Context, ref string, hookType string) (ProjectAuthHook, error) {
	hooks, err := r.client.ListProjectAuthHooks(ctx, ref)
	if err != nil {
		return ProjectAuthHook{}, err
	}
	return findInList(hooks, func(hook ProjectAuthHook) bool { return hook.HookType == hookType })
}

func authHookInputFromModel(ctx context.Context, model projectAuthHookResourceModel, addError func(string, string)) (ProjectAuthHookInput, bool) {
	headers, ok := configMapFromTerraform(ctx, model.Headers, addError)
	if !ok {
		return ProjectAuthHookInput{}, false
	}
	return ProjectAuthHookInput{
		HookType:      model.HookType.ValueString(),
		Enabled:       model.Enabled.ValueBool(),
		TargetURI:     model.TargetURI.ValueString(),
		EdgeFunction:  model.EdgeFunction.ValueString(),
		SecretHandle:  model.SecretHandle.ValueString(),
		Headers:       headers,
		TimeoutMS:     int(model.TimeoutMS.ValueInt64()),
		RetryAttempts: int(model.RetryAttempts.ValueInt64()),
	}, true
}

func setProjectAuthHookState(ctx context.Context, model *projectAuthHookResourceModel, hook ProjectAuthHook, previousSecretHandle string, previousHeaders map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(hook.ID)
	model.Ref = types.StringValue(hook.ProjectRef)
	model.HookType = types.StringValue(hook.HookType)
	model.Enabled = types.BoolValue(hook.Enabled)
	model.TargetURI = optionalStringValue(hook.TargetURI)
	model.EdgeFunction = optionalStringValue(hook.EdgeFunction)
	model.SecretHandle = sensitiveStringValue(preserveMaskedSensitiveValue(hook.SecretHandle, previousSecretHandle))
	model.TimeoutMS = types.Int64Value(int64(hook.TimeoutMS))
	model.RetryAttempts = types.Int64Value(int64(hook.RetryAttempts))
	model.Status = types.StringValue(hook.Status)
	model.Message = optionalStringValue(hook.Message)
	model.CreatedAt = optionalTimeString(hook.CreatedAt)
	model.UpdatedAt = optionalTimeString(hook.UpdatedAt)

	headers, ok := sensitiveStringMapStateValue(ctx, "headers", hook.Headers, previousHeaders, addError)
	if !ok {
		return
	}
	model.Headers = headers
}
