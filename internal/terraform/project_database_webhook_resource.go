package terraform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectDatabaseWebhookResource struct {
	client *Client
}

type projectDatabaseWebhookResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Ref            types.String `tfsdk:"ref"`
	Name           types.String `tfsdk:"name"`
	Schema         types.String `tfsdk:"schema"`
	Table          types.String `tfsdk:"table"`
	Events         types.List   `tfsdk:"events"`
	Endpoint       types.String `tfsdk:"endpoint"`
	HTTPMethod     types.String `tfsdk:"http_method"`
	Headers        types.Map    `tfsdk:"headers"`
	TimeoutSeconds types.Int64  `tfsdk:"timeout_seconds"`
	RetryCount     types.Int64  `tfsdk:"retry_count"`
	Active         types.Bool   `tfsdk:"active"`
	Metadata       types.Map    `tfsdk:"metadata"`
	Status         types.String `tfsdk:"status"`
	Message        types.String `tfsdk:"message"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

func NewProjectDatabaseWebhookResource() resource.Resource {
	return &projectDatabaseWebhookResource{}
}

func (r *projectDatabaseWebhookResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_database_webhook"
}

func (r *projectDatabaseWebhookResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	defaultEvents := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("delete"), types.StringValue("insert"), types.StringValue("update")})
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project database webhook declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated database webhook ID.",
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
				Required:    true,
				Description: "Webhook name. Must be 3-64 lowercase letters, numbers, or dashes.",
			},
			"schema": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("public"),
				Description: "Table schema.",
			},
			"table": resourceschema.StringAttribute{
				Required:    true,
				Description: "Table name.",
			},
			"events": resourceschema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     listdefault.StaticValue(defaultEvents),
				Description: "Database events that trigger delivery: insert, update, and/or delete.",
			},
			"endpoint": resourceschema.StringAttribute{
				Required:    true,
				Description: "HTTPS endpoint that receives webhook deliveries.",
			},
			"http_method": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("POST"),
				Description: "HTTP method: POST, PUT, or PATCH.",
			},
			"headers": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "HTTP headers for webhook delivery. Sensitive values must use secret:// handles.",
			},
			"timeout_seconds": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(10),
				Description: "Delivery timeout in seconds, between 1 and 300.",
			},
			"retry_count": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(3),
				Description: "Delivery retry count, between 0 and 25.",
			},
			"active": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the webhook declaration should be active.",
			},
			"metadata": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "Webhook metadata. Sensitive values must use secret:// handles.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Webhook status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable webhook status message.",
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

func (r *projectDatabaseWebhookResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *projectDatabaseWebhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectDatabaseWebhookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := databaseWebhookInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	webhook, err := r.client.CreateProjectDatabaseWebhook(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project database webhook", err.Error())
		return
	}
	setProjectDatabaseWebhookState(ctx, &plan, webhook, input.Headers, input.Metadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectDatabaseWebhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectDatabaseWebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	webhook, err := r.findDatabaseWebhook(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project database webhook", err.Error())
		return
	}
	previousHeaders, ok := optionalConfigMapFromTerraform(ctx, state.Headers, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	previousMetadata, ok := optionalConfigMapFromTerraform(ctx, state.Metadata, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectDatabaseWebhookState(ctx, &state, webhook, previousHeaders, previousMetadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectDatabaseWebhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectDatabaseWebhookResourceModel
	var state projectDatabaseWebhookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := databaseWebhookInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	err := r.client.DeleteProjectDatabaseWebhook(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to replace Supadupa project database webhook", err.Error())
		return
	}
	webhook, err := r.client.CreateProjectDatabaseWebhook(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to recreate Supadupa project database webhook", err.Error())
		return
	}
	setProjectDatabaseWebhookState(ctx, &plan, webhook, input.Headers, input.Metadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectDatabaseWebhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectDatabaseWebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectDatabaseWebhook(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project database webhook", err.Error())
		return
	}
}

func (r *projectDatabaseWebhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ref, name, ok := strings.Cut(req.ID, "/")
	if !ok {
		ref, name, ok = strings.Cut(req.ID, ":")
	}
	if !ok || strings.TrimSpace(ref) == "" || strings.TrimSpace(name) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use ref/name, for example alpha/orders-events.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ref"), strings.TrimSpace(ref))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), strings.TrimSpace(name))...)
}

func (r *projectDatabaseWebhookResource) findDatabaseWebhook(ctx context.Context, ref string, name string) (ProjectDatabaseWebhook, error) {
	webhooks, err := r.client.ListProjectDatabaseWebhooks(ctx, ref)
	if err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	for _, webhook := range webhooks {
		if webhook.Name == name {
			return webhook, nil
		}
	}
	return ProjectDatabaseWebhook{}, ErrNotFound
}

func databaseWebhookInputFromModel(ctx context.Context, model projectDatabaseWebhookResourceModel, addError func(string, string)) (ProjectDatabaseWebhookInput, bool) {
	events, ok := stringListFromTerraform(ctx, model.Events, "Invalid events list", addError)
	if !ok {
		return ProjectDatabaseWebhookInput{}, false
	}
	headers, ok := configMapFromTerraform(ctx, model.Headers, addError)
	if !ok {
		return ProjectDatabaseWebhookInput{}, false
	}
	metadata, ok := configMapFromTerraform(ctx, model.Metadata, addError)
	if !ok {
		return ProjectDatabaseWebhookInput{}, false
	}
	return ProjectDatabaseWebhookInput{
		Name:           model.Name.ValueString(),
		Schema:         model.Schema.ValueString(),
		Table:          model.Table.ValueString(),
		Events:         events,
		Endpoint:       model.Endpoint.ValueString(),
		HTTPMethod:     model.HTTPMethod.ValueString(),
		Headers:        headers,
		TimeoutSeconds: int(model.TimeoutSeconds.ValueInt64()),
		RetryCount:     int(model.RetryCount.ValueInt64()),
		Active:         model.Active.ValueBool(),
		Metadata:       metadata,
	}, true
}

func setProjectDatabaseWebhookState(ctx context.Context, model *projectDatabaseWebhookResourceModel, webhook ProjectDatabaseWebhook, previousHeaders map[string]string, previousMetadata map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(webhook.ID)
	model.Ref = types.StringValue(webhook.ProjectRef)
	model.Name = types.StringValue(webhook.Name)
	model.Schema = types.StringValue(webhook.Schema)
	model.Table = types.StringValue(webhook.Table)
	model.Endpoint = types.StringValue(webhook.Endpoint)
	model.HTTPMethod = types.StringValue(webhook.HTTPMethod)
	model.TimeoutSeconds = types.Int64Value(int64(webhook.TimeoutSeconds))
	model.RetryCount = types.Int64Value(int64(webhook.RetryCount))
	model.Active = types.BoolValue(webhook.Active)
	model.Status = types.StringValue(webhook.Status)
	model.Message = optionalStringValue(webhook.Message)
	model.CreatedAt = optionalTimeString(webhook.CreatedAt)
	model.UpdatedAt = optionalTimeString(webhook.UpdatedAt)

	events, diags := types.ListValueFrom(ctx, types.StringType, webhook.Events)
	if diags.HasError() {
		addError("Unable to encode events list", diags.Errors()[0].Detail())
		return
	}
	model.Events = events
	headers, diags := types.MapValueFrom(ctx, types.StringType, preserveMaskedConfigValues(webhook.Headers, previousHeaders))
	if diags.HasError() {
		addError("Unable to encode headers map", diags.Errors()[0].Detail())
		return
	}
	model.Headers = headers
	metadata, diags := types.MapValueFrom(ctx, types.StringType, preserveMaskedConfigValues(webhook.Metadata, previousMetadata))
	if diags.HasError() {
		addError("Unable to encode metadata map", diags.Errors()[0].Detail())
		return
	}
	model.Metadata = metadata
}
