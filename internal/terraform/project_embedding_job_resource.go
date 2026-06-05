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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectEmbeddingJobResource struct {
	client *Client
}

type projectEmbeddingJobResourceModel struct {
	ID                types.String `tfsdk:"id"`
	Ref               types.String `tfsdk:"ref"`
	Name              types.String `tfsdk:"name"`
	SourceSchema      types.String `tfsdk:"source_schema"`
	SourceTable       types.String `tfsdk:"source_table"`
	SourceColumn      types.String `tfsdk:"source_column"`
	PrimaryKeyColumn  types.String `tfsdk:"primary_key_column"`
	DestinationTable  types.String `tfsdk:"destination_table"`
	DestinationColumn types.String `tfsdk:"destination_column"`
	Provider          types.String `tfsdk:"provider"`
	Model             types.String `tfsdk:"model"`
	Dimension         types.Int64  `tfsdk:"dimension"`
	Schedule          types.String `tfsdk:"schedule"`
	BatchSize         types.Int64  `tfsdk:"batch_size"`
	Status            types.String `tfsdk:"status"`
	Message           types.String `tfsdk:"message"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

func NewProjectEmbeddingJobResource() resource.Resource {
	return &projectEmbeddingJobResource{}
}

func (r *projectEmbeddingJobResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_embedding_job"
}

func (r *projectEmbeddingJobResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project automatic embedding job declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated embedding job ID.",
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
				Optional:    true,
				Computed:    true,
				Description: "Embedding job name. Empty input derives source-table-source-column-embeddings.",
			},
			"source_schema": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("public"),
				Description: "Source table schema.",
			},
			"source_table": resourceschema.StringAttribute{
				Required:    true,
				Description: "Source table.",
			},
			"source_column": resourceschema.StringAttribute{
				Required:    true,
				Description: "Source text column.",
			},
			"primary_key_column": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("id"),
				Description: "Source primary key column.",
			},
			"destination_table": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Destination table. Empty input derives source_table + _embeddings.",
			},
			"destination_column": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("embedding"),
				Description: "Destination vector column.",
			},
			"provider": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("openai"),
				Description: "Embedding provider: openai, huggingface, or local.",
			},
			"model": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("text-embedding-3-small"),
				Description: "Embedding model.",
			},
			"dimension": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1536),
				Description: "Embedding dimension, between 1 and 65535.",
			},
			"schedule": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("manual"),
				Description: "Embedding schedule or manual.",
			},
			"batch_size": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(100),
				Description: "Embedding batch size, between 1 and 10000.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Embedding job status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable embedding job status message.",
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

func (r *projectEmbeddingJobResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *projectEmbeddingJobResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectEmbeddingJobResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input := embeddingJobInputFromModel(plan)
	job, err := r.client.CreateProjectEmbeddingJob(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project embedding job", err.Error())
		return
	}
	setProjectEmbeddingJobState(&plan, job)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectEmbeddingJobResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectEmbeddingJobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	job, err := r.findEmbeddingJob(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project embedding job", err.Error())
		return
	}
	setProjectEmbeddingJobState(&state, job)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectEmbeddingJobResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectEmbeddingJobResourceModel
	var state projectEmbeddingJobResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input := embeddingJobInputFromModel(plan)
	err := r.client.DeleteProjectEmbeddingJob(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to replace Supadupa project embedding job", err.Error())
		return
	}
	job, err := r.client.CreateProjectEmbeddingJob(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to recreate Supadupa project embedding job", err.Error())
		return
	}
	setProjectEmbeddingJobState(&plan, job)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectEmbeddingJobResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectEmbeddingJobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectEmbeddingJob(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project embedding job", err.Error())
		return
	}
}

func (r *projectEmbeddingJobResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ref, id, ok := strings.Cut(req.ID, "/")
	if !ok {
		ref, id, ok = strings.Cut(req.ID, ":")
	}
	if !ok || strings.TrimSpace(ref) == "" || strings.TrimSpace(id) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use ref/id, for example alpha/emb_123.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ref"), strings.TrimSpace(ref))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.TrimSpace(id))...)
}

func (r *projectEmbeddingJobResource) findEmbeddingJob(ctx context.Context, ref string, id string) (ProjectEmbeddingJob, error) {
	jobs, err := r.client.ListProjectEmbeddingJobs(ctx, ref)
	if err != nil {
		return ProjectEmbeddingJob{}, err
	}
	for _, job := range jobs {
		if job.ID == id {
			return job, nil
		}
	}
	return ProjectEmbeddingJob{}, ErrNotFound
}

func embeddingJobInputFromModel(model projectEmbeddingJobResourceModel) ProjectEmbeddingJobInput {
	return ProjectEmbeddingJobInput{
		Name:              model.Name.ValueString(),
		SourceSchema:      model.SourceSchema.ValueString(),
		SourceTable:       model.SourceTable.ValueString(),
		SourceColumn:      model.SourceColumn.ValueString(),
		PrimaryKeyColumn:  model.PrimaryKeyColumn.ValueString(),
		DestinationTable:  model.DestinationTable.ValueString(),
		DestinationColumn: model.DestinationColumn.ValueString(),
		Provider:          model.Provider.ValueString(),
		Model:             model.Model.ValueString(),
		Dimension:         int(model.Dimension.ValueInt64()),
		Schedule:          model.Schedule.ValueString(),
		BatchSize:         int(model.BatchSize.ValueInt64()),
	}
}

func setProjectEmbeddingJobState(model *projectEmbeddingJobResourceModel, job ProjectEmbeddingJob) {
	model.ID = types.StringValue(job.ID)
	model.Ref = types.StringValue(job.ProjectRef)
	model.Name = types.StringValue(job.Name)
	model.SourceSchema = types.StringValue(job.SourceSchema)
	model.SourceTable = types.StringValue(job.SourceTable)
	model.SourceColumn = types.StringValue(job.SourceColumn)
	model.PrimaryKeyColumn = types.StringValue(job.PrimaryKeyColumn)
	model.DestinationTable = types.StringValue(job.DestinationTable)
	model.DestinationColumn = types.StringValue(job.DestinationColumn)
	model.Provider = types.StringValue(job.Provider)
	model.Model = types.StringValue(job.Model)
	model.Dimension = types.Int64Value(int64(job.Dimension))
	model.Schedule = types.StringValue(job.Schedule)
	model.BatchSize = types.Int64Value(int64(job.BatchSize))
	model.Status = types.StringValue(job.Status)
	model.Message = optionalStringValue(job.Message)
	model.CreatedAt = optionalTimeString(job.CreatedAt)
	model.UpdatedAt = optionalTimeString(job.UpdatedAt)
}
