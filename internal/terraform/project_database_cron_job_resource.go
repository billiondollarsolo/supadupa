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

type projectDatabaseCronJobResource struct {
	client *Client
}

type projectDatabaseCronJobResourceModel struct {
	ID                types.String `tfsdk:"id"`
	Ref               types.String `tfsdk:"ref"`
	Name              types.String `tfsdk:"name"`
	Schedule          types.String `tfsdk:"schedule"`
	Command           types.String `tfsdk:"command"`
	Database          types.String `tfsdk:"database"`
	Username          types.String `tfsdk:"username"`
	Active            types.Bool   `tfsdk:"active"`
	TimeoutSeconds    types.Int64  `tfsdk:"timeout_seconds"`
	MaxRuntimeSeconds types.Int64  `tfsdk:"max_runtime_seconds"`
	Metadata          types.Map    `tfsdk:"metadata"`
	Status            types.String `tfsdk:"status"`
	Message           types.String `tfsdk:"message"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

func NewProjectDatabaseCronJobResource() resource.Resource {
	return &projectDatabaseCronJobResource{}
}

func (r *projectDatabaseCronJobResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_database_cron_job"
}

func (r *projectDatabaseCronJobResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project pg_cron job declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated database cron job ID.",
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
				Description: "Cron job name. Must be 3-64 lowercase letters, numbers, or dashes.",
			},
			"schedule": resourceschema.StringAttribute{
				Required:    true,
				Description: "Five-field cron schedule.",
			},
			"command": resourceschema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "SQL command executed by pg_cron.",
			},
			"database": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("postgres"),
				Description: "Target database name.",
			},
			"username": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("postgres"),
				Description: "Database role used to run the job.",
			},
			"active": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the cron job should be active.",
			},
			"timeout_seconds": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(60),
				Description: "Statement timeout in seconds, between 1 and 86400.",
			},
			"max_runtime_seconds": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(60),
				Description: "Maximum runtime in seconds, between 1 and 86400.",
			},
			"metadata": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "Cron job metadata. Sensitive values must use secret:// handles.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Cron job status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable cron job status message.",
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

func (r *projectDatabaseCronJobResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectDatabaseCronJobResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	requireResourceReplaceOnUpdate(ctx, req, resp, "name")
}

func (r *projectDatabaseCronJobResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectDatabaseCronJobResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := databaseCronJobInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	job, err := r.client.CreateProjectDatabaseCronJob(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project database cron job", err.Error())
		return
	}
	setProjectDatabaseCronJobState(ctx, &plan, job, input.Metadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectDatabaseCronJobResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectDatabaseCronJobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	job, err := r.findDatabaseCronJob(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project database cron job", err.Error())
		return
	}
	previousMetadata, ok := optionalConfigMapFromTerraform(ctx, state.Metadata, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectDatabaseCronJobState(ctx, &state, job, previousMetadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectDatabaseCronJobResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	reportUnsupportedInPlaceUpdate(resp, "Supadupa project database cron job")
}

func (r *projectDatabaseCronJobResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectDatabaseCronJobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectDatabaseCronJob(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project database cron job", err.Error())
		return
	}
}

func (r *projectDatabaseCronJobResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "name", "Use ref/name, for example alpha/refresh-rollups.")
}

func (r *projectDatabaseCronJobResource) findDatabaseCronJob(ctx context.Context, ref string, name string) (ProjectDatabaseCronJob, error) {
	jobs, err := r.client.ListProjectDatabaseCronJobs(ctx, ref)
	if err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	return findInList(jobs, func(job ProjectDatabaseCronJob) bool { return job.Name == name })
}

func databaseCronJobInputFromModel(ctx context.Context, model projectDatabaseCronJobResourceModel, addError func(string, string)) (ProjectDatabaseCronJobInput, bool) {
	metadata, ok := configMapFromTerraform(ctx, model.Metadata, addError)
	if !ok {
		return ProjectDatabaseCronJobInput{}, false
	}
	return ProjectDatabaseCronJobInput{
		Name:              model.Name.ValueString(),
		Schedule:          model.Schedule.ValueString(),
		Command:           model.Command.ValueString(),
		Database:          model.Database.ValueString(),
		Username:          model.Username.ValueString(),
		Active:            model.Active.ValueBool(),
		TimeoutSeconds:    int(model.TimeoutSeconds.ValueInt64()),
		MaxRuntimeSeconds: int(model.MaxRuntimeSeconds.ValueInt64()),
		Metadata:          metadata,
	}, true
}

func setProjectDatabaseCronJobState(ctx context.Context, model *projectDatabaseCronJobResourceModel, job ProjectDatabaseCronJob, previousMetadata map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(job.ID)
	model.Ref = types.StringValue(job.ProjectRef)
	model.Name = types.StringValue(job.Name)
	model.Schedule = types.StringValue(job.Schedule)
	model.Command = types.StringValue(job.Command)
	model.Database = types.StringValue(job.Database)
	model.Username = types.StringValue(job.Username)
	model.Active = types.BoolValue(job.Active)
	model.TimeoutSeconds = types.Int64Value(int64(job.TimeoutSeconds))
	model.MaxRuntimeSeconds = types.Int64Value(int64(job.MaxRuntimeSeconds))
	model.Status = types.StringValue(job.Status)
	model.Message = optionalStringValue(job.Message)
	model.CreatedAt = optionalTimeString(job.CreatedAt)
	model.UpdatedAt = optionalTimeString(job.UpdatedAt)

	metadata, ok := sensitiveStringMapStateValue(ctx, "metadata", job.Metadata, previousMetadata, addError)
	if !ok {
		return
	}
	model.Metadata = metadata
}
