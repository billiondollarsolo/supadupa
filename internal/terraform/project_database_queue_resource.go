package terraform

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectDatabaseQueueResource struct {
	client *Client
}

type projectDatabaseQueueResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	Ref                      types.String `tfsdk:"ref"`
	Name                     types.String `tfsdk:"name"`
	Schema                   types.String `tfsdk:"schema"`
	RetentionMinutes         types.Int64  `tfsdk:"retention_minutes"`
	VisibilityTimeoutSeconds types.Int64  `tfsdk:"visibility_timeout_seconds"`
	MaxRetries               types.Int64  `tfsdk:"max_retries"`
	DeadLetterQueue          types.String `tfsdk:"dead_letter_queue"`
	Active                   types.Bool   `tfsdk:"active"`
	Metadata                 types.Map    `tfsdk:"metadata"`
	Status                   types.String `tfsdk:"status"`
	Message                  types.String `tfsdk:"message"`
	CreatedAt                types.String `tfsdk:"created_at"`
	UpdatedAt                types.String `tfsdk:"updated_at"`
}

func NewProjectDatabaseQueueResource() resource.Resource {
	return &projectDatabaseQueueResource{}
}

func (r *projectDatabaseQueueResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_database_queue"
}

func (r *projectDatabaseQueueResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project pgmq queue declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated database queue ID.",
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
				Description: "Queue name. Must be 3-64 lowercase letters, numbers, or dashes.",
			},
			"schema": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("pgmq"),
				Description: "Postgres schema that owns the pgmq queue.",
			},
			"retention_minutes": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1440),
				Description: "Queue retention window in minutes, between 1 and 525600.",
			},
			"visibility_timeout_seconds": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(30),
				Description: "Message visibility timeout in seconds, between 1 and 86400.",
			},
			"max_retries": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(5),
				Description: "Maximum retry attempts before dead-letter handling.",
			},
			"dead_letter_queue": resourceschema.StringAttribute{
				Optional:    true,
				Description: "Optional dead-letter queue name.",
			},
			"active": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the queue declaration should be active.",
			},
			"metadata": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "Queue metadata. Sensitive values must use secret:// handles.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Queue status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable queue status message.",
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

func (r *projectDatabaseQueueResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectDatabaseQueueResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	requireResourceReplaceOnUpdate(ctx, req, resp, "name")
}

func (r *projectDatabaseQueueResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectDatabaseQueueResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := databaseQueueInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	queue, err := r.client.CreateProjectDatabaseQueue(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project database queue", err.Error())
		return
	}
	setProjectDatabaseQueueState(ctx, &plan, queue, input.Metadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectDatabaseQueueResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectDatabaseQueueResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	queue, err := r.findDatabaseQueue(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project database queue", err.Error())
		return
	}
	previousMetadata, ok := optionalConfigMapFromTerraform(ctx, state.Metadata, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectDatabaseQueueState(ctx, &state, queue, previousMetadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectDatabaseQueueResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	reportUnsupportedInPlaceUpdate(resp, "Supadupa project database queue")
}

func (r *projectDatabaseQueueResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectDatabaseQueueResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectDatabaseQueue(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project database queue", err.Error())
		return
	}
}

func (r *projectDatabaseQueueResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "name", "Use ref/name, for example alpha/events.")
}

func (r *projectDatabaseQueueResource) findDatabaseQueue(ctx context.Context, ref string, name string) (ProjectDatabaseQueue, error) {
	queues, err := r.client.ListProjectDatabaseQueues(ctx, ref)
	if err != nil {
		return ProjectDatabaseQueue{}, err
	}
	return findInList(queues, func(queue ProjectDatabaseQueue) bool { return queue.Name == name })
}

func databaseQueueInputFromModel(ctx context.Context, model projectDatabaseQueueResourceModel, addError func(string, string)) (ProjectDatabaseQueueInput, bool) {
	metadata, ok := configMapFromTerraform(ctx, model.Metadata, addError)
	if !ok {
		return ProjectDatabaseQueueInput{}, false
	}
	return ProjectDatabaseQueueInput{
		Name:                     model.Name.ValueString(),
		Schema:                   model.Schema.ValueString(),
		RetentionMinutes:         int(model.RetentionMinutes.ValueInt64()),
		VisibilityTimeoutSeconds: int(model.VisibilityTimeoutSeconds.ValueInt64()),
		MaxRetries:               int(model.MaxRetries.ValueInt64()),
		DeadLetterQueue:          model.DeadLetterQueue.ValueString(),
		Active:                   model.Active.ValueBool(),
		Metadata:                 metadata,
	}, true
}

func setProjectDatabaseQueueState(ctx context.Context, model *projectDatabaseQueueResourceModel, queue ProjectDatabaseQueue, previousMetadata map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(queue.ID)
	model.Ref = types.StringValue(queue.ProjectRef)
	model.Name = types.StringValue(queue.Name)
	model.Schema = types.StringValue(queue.Schema)
	model.RetentionMinutes = types.Int64Value(int64(queue.RetentionMinutes))
	model.VisibilityTimeoutSeconds = types.Int64Value(int64(queue.VisibilityTimeoutSeconds))
	model.MaxRetries = types.Int64Value(int64(queue.MaxRetries))
	model.DeadLetterQueue = optionalStringValue(queue.DeadLetterQueue)
	model.Active = types.BoolValue(queue.Active)
	model.Status = types.StringValue(queue.Status)
	model.Message = optionalStringValue(queue.Message)
	model.CreatedAt = optionalTimeString(queue.CreatedAt)
	model.UpdatedAt = optionalTimeString(queue.UpdatedAt)

	metadata, ok := sensitiveStringMapStateValue(ctx, "metadata", queue.Metadata, previousMetadata, addError)
	if !ok {
		return
	}
	model.Metadata = metadata
}
