package terraform

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectVectorBucketResource struct {
	client *Client
}

type projectVectorBucketResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Ref            types.String `tfsdk:"ref"`
	Name           types.String `tfsdk:"name"`
	Dimension      types.Int64  `tfsdk:"dimension"`
	Distance       types.String `tfsdk:"distance"`
	IndexMethod    types.String `tfsdk:"index_method"`
	StorageBackend types.String `tfsdk:"storage_backend"`
	StorageURI     types.String `tfsdk:"storage_uri"`
	Metadata       types.Map    `tfsdk:"metadata"`
	Status         types.String `tfsdk:"status"`
	Message        types.String `tfsdk:"message"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

func NewProjectVectorBucketResource() resource.Resource {
	return &projectVectorBucketResource{}
}

func (r *projectVectorBucketResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_vector_bucket"
}

func (r *projectVectorBucketResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project vector bucket declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated vector bucket ID.",
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
				Description: "Vector bucket name. Must be 3-64 lowercase letters, numbers, or dashes.",
			},
			"dimension": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1536),
				Description: "Vector dimension.",
			},
			"distance": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("cosine"),
				Description: "Distance function: cosine, l2, or ip.",
			},
			"index_method": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("hnsw"),
				Description: "Index method: none, hnsw, or ivfflat.",
			},
			"storage_backend": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("postgres"),
				Description: "Storage backend: postgres or s3.",
			},
			"storage_uri": resourceschema.StringAttribute{
				Optional:    true,
				Description: "Storage URI for S3-backed vector buckets.",
			},
			"metadata": resourceschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Sensitive:   true,
				Description: "Bucket metadata. Sensitive values must use secret:// handles.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Bucket status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable bucket status message.",
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

func (r *projectVectorBucketResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectVectorBucketResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	requireResourceReplaceOnUpdate(ctx, req, resp, "name")
}

func (r *projectVectorBucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectVectorBucketResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := vectorBucketInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	bucket, err := r.client.CreateProjectVectorBucket(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project vector bucket", err.Error())
		return
	}
	setProjectVectorBucketState(ctx, &plan, bucket, input.Metadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectVectorBucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectVectorBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	bucket, err := r.findVectorBucket(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project vector bucket", err.Error())
		return
	}
	previous, ok := optionalConfigMapFromTerraform(ctx, state.Metadata, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectVectorBucketState(ctx, &state, bucket, previous, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectVectorBucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	reportUnsupportedInPlaceUpdate(resp, "Supadupa project vector bucket")
}

func (r *projectVectorBucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectVectorBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectVectorBucket(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project vector bucket", err.Error())
		return
	}
}

func (r *projectVectorBucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "name", "Use ref/name, for example alpha/documents.")
}

func (r *projectVectorBucketResource) findVectorBucket(ctx context.Context, ref string, name string) (ProjectVectorBucket, error) {
	buckets, err := r.client.ListProjectVectorBuckets(ctx, ref)
	if err != nil {
		return ProjectVectorBucket{}, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	return findInList(buckets, func(bucket ProjectVectorBucket) bool { return bucket.Name == normalized })
}

func vectorBucketInputFromModel(ctx context.Context, model projectVectorBucketResourceModel, addError func(string, string)) (ProjectVectorBucketInput, bool) {
	metadata, ok := configMapFromTerraform(ctx, model.Metadata, addError)
	if !ok {
		return ProjectVectorBucketInput{}, false
	}
	return ProjectVectorBucketInput{
		Name:           model.Name.ValueString(),
		Dimension:      int(model.Dimension.ValueInt64()),
		Distance:       model.Distance.ValueString(),
		IndexMethod:    model.IndexMethod.ValueString(),
		StorageBackend: model.StorageBackend.ValueString(),
		StorageURI:     model.StorageURI.ValueString(),
		Metadata:       metadata,
	}, true
}

func setProjectVectorBucketState(ctx context.Context, model *projectVectorBucketResourceModel, bucket ProjectVectorBucket, previousMetadata map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(bucket.ID)
	model.Ref = types.StringValue(bucket.ProjectRef)
	model.Name = types.StringValue(bucket.Name)
	model.Dimension = types.Int64Value(int64(bucket.Dimension))
	model.Distance = types.StringValue(bucket.Distance)
	model.IndexMethod = types.StringValue(bucket.IndexMethod)
	model.StorageBackend = types.StringValue(bucket.StorageBackend)
	model.StorageURI = optionalStringValue(bucket.StorageURI)
	model.Status = types.StringValue(bucket.Status)
	model.Message = optionalStringValue(bucket.Message)
	model.CreatedAt = optionalTimeString(bucket.CreatedAt)
	model.UpdatedAt = optionalTimeString(bucket.UpdatedAt)

	metadata, ok := sensitiveStringMapStateValue(ctx, "metadata", bucket.Metadata, previousMetadata, addError)
	if !ok {
		return
	}
	model.Metadata = metadata
}
