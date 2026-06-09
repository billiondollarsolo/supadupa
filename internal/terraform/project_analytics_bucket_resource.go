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

type projectAnalyticsBucketResource struct {
	client *Client
}

type projectAnalyticsBucketResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	Ref                types.String `tfsdk:"ref"`
	Name               types.String `tfsdk:"name"`
	StorageURI         types.String `tfsdk:"storage_uri"`
	CatalogURI         types.String `tfsdk:"catalog_uri"`
	Warehouse          types.String `tfsdk:"warehouse"`
	CredentialHandle   types.String `tfsdk:"credential_handle"`
	FormatVersion      types.Int64  `tfsdk:"format_version"`
	Partitioning       types.String `tfsdk:"partitioning"`
	RetentionDays      types.Int64  `tfsdk:"retention_days"`
	CompactionSchedule types.String `tfsdk:"compaction_schedule"`
	Metadata           types.Map    `tfsdk:"metadata"`
	Status             types.String `tfsdk:"status"`
	Message            types.String `tfsdk:"message"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func NewProjectAnalyticsBucketResource() resource.Resource {
	return &projectAnalyticsBucketResource{}
}

func (r *projectAnalyticsBucketResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_analytics_bucket"
}

func (r *projectAnalyticsBucketResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project Apache Iceberg analytics bucket declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated analytics bucket ID.",
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
				Description: "Analytics bucket name. Must be 3-64 lowercase letters, numbers, or dashes.",
			},
			"storage_uri": resourceschema.StringAttribute{
				Required:    true,
				Description: "Iceberg table storage URI. Supported schemes are s3, gs, az, and file.",
			},
			"catalog_uri": resourceschema.StringAttribute{
				Optional:    true,
				Description: "Optional Iceberg catalog URI.",
			},
			"warehouse": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Iceberg warehouse name. Defaults to the analytics bucket name.",
			},
			"credential_handle": resourceschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "secret:// handle for storage or catalog credentials.",
			},
			"format_version": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(2),
				Description: "Iceberg format version. Must be 1 or 2.",
			},
			"partitioning": resourceschema.StringAttribute{
				Optional:    true,
				Description: "Optional Iceberg partition spec.",
			},
			"retention_days": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(0),
				Description: "Retention period in days. 0 means indefinite.",
			},
			"compaction_schedule": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("manual"),
				Description: "Compaction schedule or manual.",
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

func (r *projectAnalyticsBucketResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectAnalyticsBucketResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	requireResourceReplaceOnUpdate(ctx, req, resp, "name")
}

func (r *projectAnalyticsBucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectAnalyticsBucketResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := analyticsBucketInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	bucket, err := r.client.CreateProjectAnalyticsBucket(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project analytics bucket", err.Error())
		return
	}
	setProjectAnalyticsBucketState(ctx, &plan, bucket, input.CredentialHandle, input.Metadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectAnalyticsBucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectAnalyticsBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	bucket, err := r.findAnalyticsBucket(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project analytics bucket", err.Error())
		return
	}
	previousMetadata, ok := optionalConfigMapFromTerraform(ctx, state.Metadata, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectAnalyticsBucketState(ctx, &state, bucket, previousSensitiveString(state.CredentialHandle), previousMetadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectAnalyticsBucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	reportUnsupportedInPlaceUpdate(resp, "Supadupa project analytics bucket")
}

func (r *projectAnalyticsBucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectAnalyticsBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectAnalyticsBucket(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project analytics bucket", err.Error())
		return
	}
}

func (r *projectAnalyticsBucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "name", "Use ref/name, for example alpha/events.")
}

func (r *projectAnalyticsBucketResource) findAnalyticsBucket(ctx context.Context, ref string, name string) (ProjectAnalyticsBucket, error) {
	buckets, err := r.client.ListProjectAnalyticsBuckets(ctx, ref)
	if err != nil {
		return ProjectAnalyticsBucket{}, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	return findInList(buckets, func(bucket ProjectAnalyticsBucket) bool { return bucket.Name == normalized })
}

func analyticsBucketInputFromModel(ctx context.Context, model projectAnalyticsBucketResourceModel, addError func(string, string)) (ProjectAnalyticsBucketInput, bool) {
	metadata, ok := configMapFromTerraform(ctx, model.Metadata, addError)
	if !ok {
		return ProjectAnalyticsBucketInput{}, false
	}
	return ProjectAnalyticsBucketInput{
		Name:               model.Name.ValueString(),
		StorageURI:         model.StorageURI.ValueString(),
		CatalogURI:         model.CatalogURI.ValueString(),
		Warehouse:          model.Warehouse.ValueString(),
		CredentialHandle:   model.CredentialHandle.ValueString(),
		FormatVersion:      int(model.FormatVersion.ValueInt64()),
		Partitioning:       model.Partitioning.ValueString(),
		RetentionDays:      int(model.RetentionDays.ValueInt64()),
		CompactionSchedule: model.CompactionSchedule.ValueString(),
		Metadata:           metadata,
	}, true
}

func setProjectAnalyticsBucketState(ctx context.Context, model *projectAnalyticsBucketResourceModel, bucket ProjectAnalyticsBucket, previousCredentialHandle string, previousMetadata map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(bucket.ID)
	model.Ref = types.StringValue(bucket.ProjectRef)
	model.Name = types.StringValue(bucket.Name)
	model.StorageURI = types.StringValue(bucket.StorageURI)
	model.CatalogURI = optionalStringValue(bucket.CatalogURI)
	model.Warehouse = types.StringValue(bucket.Warehouse)
	model.CredentialHandle = sensitiveStringValue(preserveMaskedSensitiveValue(bucket.CredentialHandle, previousCredentialHandle))
	model.FormatVersion = types.Int64Value(int64(bucket.FormatVersion))
	model.Partitioning = optionalStringValue(bucket.Partitioning)
	model.RetentionDays = types.Int64Value(int64(bucket.RetentionDays))
	model.CompactionSchedule = types.StringValue(bucket.CompactionSchedule)
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
