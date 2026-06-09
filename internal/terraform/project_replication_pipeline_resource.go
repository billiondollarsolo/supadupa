package terraform

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectReplicationPipelineResource struct {
	client *Client
}

type projectReplicationPipelineResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Ref              types.String `tfsdk:"ref"`
	Name             types.String `tfsdk:"name"`
	Type             types.String `tfsdk:"type"`
	SourceSchema     types.String `tfsdk:"source_schema"`
	SourceTable      types.String `tfsdk:"source_table"`
	Destination      types.String `tfsdk:"destination"`
	DestinationURI   types.String `tfsdk:"destination_uri"`
	CredentialHandle types.String `tfsdk:"credential_handle"`
	Config           types.Map    `tfsdk:"config"`
	Status           types.String `tfsdk:"status"`
	Message          types.String `tfsdk:"message"`
	CreatedAt        types.String `tfsdk:"created_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
}

func NewProjectReplicationPipelineResource() resource.Resource {
	return &projectReplicationPipelineResource{}
}

func (r *projectReplicationPipelineResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_replication_pipeline"
}

func (r *projectReplicationPipelineResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project logical replication, ETL, or analytics export pipeline declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated replication pipeline ID.",
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
				Description: "Pipeline name. Must be 3-64 lowercase letters, numbers, or dashes.",
			},
			"type": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("logical"),
				Description: "Pipeline type: logical, etl, or analytics_bucket.",
			},
			"source_schema": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("public"),
				Description: "Source Postgres schema.",
			},
			"source_table": resourceschema.StringAttribute{
				Required:    true,
				Description: "Source Postgres table.",
			},
			"destination": resourceschema.StringAttribute{
				Required:    true,
				Description: "Destination type: postgres, webhook, s3, iceberg, bigquery, snowflake, or redshift.",
			},
			"destination_uri": resourceschema.StringAttribute{
				Optional:    true,
				Description: "Optional destination URI.",
			},
			"credential_handle": resourceschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "secret:// credential handle for the destination.",
			},
			"config": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "Destination-specific key/value config. Sensitive values must use secret:// handles.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Pipeline status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable pipeline status message.",
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

func (r *projectReplicationPipelineResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectReplicationPipelineResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	requireResourceReplaceOnUpdate(ctx, req, resp, "id")
}

func (r *projectReplicationPipelineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectReplicationPipelineResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := replicationPipelineInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	pipeline, err := r.client.CreateProjectReplicationPipeline(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project replication pipeline", err.Error())
		return
	}
	setProjectReplicationPipelineState(ctx, &plan, pipeline, input.CredentialHandle, input.Config, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectReplicationPipelineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectReplicationPipelineResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pipeline, err := r.findReplicationPipeline(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project replication pipeline", err.Error())
		return
	}
	previousConfig, ok := optionalConfigMapFromTerraform(ctx, state.Config, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectReplicationPipelineState(ctx, &state, pipeline, previousSensitiveString(state.CredentialHandle), previousConfig, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectReplicationPipelineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	reportUnsupportedInPlaceUpdate(resp, "Supadupa project replication pipeline")
}

func (r *projectReplicationPipelineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectReplicationPipelineResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectReplicationPipeline(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project replication pipeline", err.Error())
		return
	}
}

func (r *projectReplicationPipelineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "id", "Use ref/id, for example alpha/pipe_123.")
}

func (r *projectReplicationPipelineResource) findReplicationPipeline(ctx context.Context, ref string, id string) (ProjectReplicationPipeline, error) {
	pipelines, err := r.client.ListProjectReplicationPipelines(ctx, ref)
	if err != nil {
		return ProjectReplicationPipeline{}, err
	}
	return findInList(pipelines, func(pipeline ProjectReplicationPipeline) bool { return pipeline.ID == id })
}

func replicationPipelineInputFromModel(ctx context.Context, model projectReplicationPipelineResourceModel, addError func(string, string)) (ProjectReplicationPipelineInput, bool) {
	config, ok := configMapFromTerraform(ctx, model.Config, addError)
	if !ok {
		return ProjectReplicationPipelineInput{}, false
	}
	return ProjectReplicationPipelineInput{
		Name:             model.Name.ValueString(),
		Type:             model.Type.ValueString(),
		SourceSchema:     model.SourceSchema.ValueString(),
		SourceTable:      model.SourceTable.ValueString(),
		Destination:      model.Destination.ValueString(),
		DestinationURI:   model.DestinationURI.ValueString(),
		CredentialHandle: model.CredentialHandle.ValueString(),
		Config:           config,
	}, true
}

func setProjectReplicationPipelineState(ctx context.Context, model *projectReplicationPipelineResourceModel, pipeline ProjectReplicationPipeline, previousCredentialHandle string, previousConfig map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(pipeline.ID)
	model.Ref = types.StringValue(pipeline.ProjectRef)
	model.Name = types.StringValue(pipeline.Name)
	model.Type = types.StringValue(pipeline.Type)
	model.SourceSchema = types.StringValue(pipeline.SourceSchema)
	model.SourceTable = types.StringValue(pipeline.SourceTable)
	model.Destination = types.StringValue(pipeline.Destination)
	model.DestinationURI = optionalStringValue(pipeline.DestinationURI)
	model.CredentialHandle = sensitiveStringValue(preserveMaskedSensitiveValue(pipeline.CredentialHandle, previousCredentialHandle))
	model.Status = types.StringValue(pipeline.Status)
	model.Message = optionalStringValue(pipeline.Message)
	model.CreatedAt = optionalTimeString(pipeline.CreatedAt)
	model.UpdatedAt = optionalTimeString(pipeline.UpdatedAt)

	config, ok := sensitiveStringMapStateValue(ctx, "config", pipeline.Config, previousConfig, addError)
	if !ok {
		return
	}
	model.Config = config
}
