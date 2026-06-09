package terraform

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectCDNPolicyResource struct {
	client *Client
}

type projectCDNPolicyResourceModel struct {
	ID                          types.String `tfsdk:"id"`
	Ref                         types.String `tfsdk:"ref"`
	Enabled                     types.Bool   `tfsdk:"enabled"`
	BrowserTTLSeconds           types.Int64  `tfsdk:"browser_ttl_seconds"`
	EdgeTTLSeconds              types.Int64  `tfsdk:"edge_ttl_seconds"`
	StaleWhileRevalidateSeconds types.Int64  `tfsdk:"stale_while_revalidate_seconds"`
	IncludedPaths               types.Set    `tfsdk:"included_paths"`
	ExcludedPaths               types.Set    `tfsdk:"excluded_paths"`
	SmartRevalidation           types.Bool   `tfsdk:"smart_revalidation"`
	CacheControl                types.String `tfsdk:"cache_control"`
	EffectiveCacheControl       types.String `tfsdk:"effective_cache_control"`
	UpdatedAt                   types.String `tfsdk:"updated_at"`
}

func NewProjectCDNPolicyResource() resource.Resource {
	return &projectCDNPolicyResource{}
}

func (r *projectCDNPolicyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_cdn_policy"
}

func (r *projectCDNPolicyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	defaultIncludedPaths := types.SetValueMust(types.StringType, []attr.Value{types.StringValue("/storage/v1/object/public/*")})
	defaultExcludedPaths := types.SetValueMust(types.StringType, []attr.Value{})
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project CDN and Smart CDN policy managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Stable CDN policy ID in the form ref/cdn-policy.",
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
				Description: "Whether CDN caching is enabled for the project.",
			},
			"browser_ttl_seconds": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(3600),
				Description: "Browser max-age in seconds.",
			},
			"edge_ttl_seconds": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(3600),
				Description: "Edge s-maxage in seconds.",
			},
			"stale_while_revalidate_seconds": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(60),
				Description: "stale-while-revalidate duration in seconds.",
			},
			"included_paths": resourceschema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     setdefault.StaticValue(defaultIncludedPaths),
				Description: "Path patterns included in the CDN policy.",
			},
			"excluded_paths": resourceschema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     setdefault.StaticValue(defaultExcludedPaths),
				Description: "Path patterns excluded from the CDN policy.",
			},
			"smart_revalidation": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether Smart CDN storage object event revalidation is enabled.",
			},
			"cache_control": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("public, max-age=3600, s-maxage=3600, stale-while-revalidate=60"),
				Description: "Explicit Cache-Control header to render at the edge.",
			},
			"effective_cache_control": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Cache-Control header currently reported by the control plane.",
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

func (r *projectCDNPolicyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectCDNPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectCDNPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := cdnPolicyInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	policy, err := r.client.UpdateProjectCDNPolicy(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project CDN policy", err.Error())
		return
	}
	setProjectCDNPolicyState(ctx, &plan, policy, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectCDNPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectCDNPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := r.client.GetProjectCDNPolicy(ctx, state.Ref.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project CDN policy", err.Error())
		return
	}
	setProjectCDNPolicyState(ctx, &state, policy, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectCDNPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectCDNPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := cdnPolicyInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	policy, err := r.client.UpdateProjectCDNPolicy(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa project CDN policy", err.Error())
		return
	}
	setProjectCDNPolicyState(ctx, &plan, policy, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectCDNPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectCDNPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.client.UpdateProjectCDNPolicy(ctx, state.Ref.ValueString(), defaultProjectCDNPolicyInput())
	if err != nil {
		resp.Diagnostics.AddError("Unable to reset Supadupa project CDN policy", err.Error())
		return
	}
}

func (r *projectCDNPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setOnePartImportState(ctx, req.ID, resp, "ref", "Use project ref, for example alpha.")
}

func cdnPolicyInputFromModel(ctx context.Context, model projectCDNPolicyResourceModel, addError func(string, string)) (ProjectCDNPolicyInput, bool) {
	included, ok := stringSetFromTerraform(ctx, model.IncludedPaths, "Invalid included_paths set", addError)
	if !ok {
		return ProjectCDNPolicyInput{}, false
	}
	excluded, ok := stringSetFromTerraform(ctx, model.ExcludedPaths, "Invalid excluded_paths set", addError)
	if !ok {
		return ProjectCDNPolicyInput{}, false
	}
	return ProjectCDNPolicyInput{
		Enabled:                     model.Enabled.ValueBool(),
		BrowserTTLSeconds:           int(model.BrowserTTLSeconds.ValueInt64()),
		EdgeTTLSeconds:              int(model.EdgeTTLSeconds.ValueInt64()),
		StaleWhileRevalidateSeconds: int(model.StaleWhileRevalidateSeconds.ValueInt64()),
		IncludedPaths:               included,
		ExcludedPaths:               excluded,
		SmartRevalidation:           model.SmartRevalidation.ValueBool(),
		CacheControl:                model.CacheControl.ValueString(),
	}, true
}

func setProjectCDNPolicyState(ctx context.Context, model *projectCDNPolicyResourceModel, policy ProjectCDNPolicy, addError func(string, string)) {
	model.ID = types.StringValue(policy.ProjectRef + "/cdn-policy")
	model.Ref = types.StringValue(policy.ProjectRef)
	model.Enabled = types.BoolValue(policy.Enabled)
	model.BrowserTTLSeconds = types.Int64Value(int64(policy.BrowserTTLSeconds))
	model.EdgeTTLSeconds = types.Int64Value(int64(policy.EdgeTTLSeconds))
	model.StaleWhileRevalidateSeconds = types.Int64Value(int64(policy.StaleWhileRevalidateSeconds))
	model.SmartRevalidation = types.BoolValue(policy.SmartRevalidation)
	model.CacheControl = types.StringValue(policy.CacheControl)
	model.EffectiveCacheControl = types.StringValue(policy.CacheControl)
	model.UpdatedAt = optionalTimeString(policy.UpdatedAt)

	included, diags := types.SetValueFrom(ctx, types.StringType, policy.IncludedPaths)
	if diags.HasError() {
		addError("Unable to encode included_paths set", diags.Errors()[0].Detail())
		return
	}
	model.IncludedPaths = included
	excluded, diags := types.SetValueFrom(ctx, types.StringType, policy.ExcludedPaths)
	if diags.HasError() {
		addError("Unable to encode excluded_paths set", diags.Errors()[0].Detail())
		return
	}
	model.ExcludedPaths = excluded
}

func defaultProjectCDNPolicyInput() ProjectCDNPolicyInput {
	return ProjectCDNPolicyInput{
		Enabled:                     false,
		BrowserTTLSeconds:           3600,
		EdgeTTLSeconds:              3600,
		StaleWhileRevalidateSeconds: 60,
		IncludedPaths:               []string{"/storage/v1/object/public/*"},
		ExcludedPaths:               []string{},
		SmartRevalidation:           false,
		CacheControl:                "public, max-age=3600, s-maxage=3600, stale-while-revalidate=60",
	}
}
