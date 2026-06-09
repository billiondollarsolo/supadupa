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

const defaultStorageBucketFileSizeLimit int64 = 50 * 1024 * 1024

type projectStorageBucketResource struct {
	client *Client
}

type projectStorageBucketResourceModel struct {
	ID                types.String `tfsdk:"id"`
	Ref               types.String `tfsdk:"ref"`
	Name              types.String `tfsdk:"name"`
	Public            types.Bool   `tfsdk:"public"`
	FileSizeLimit     types.Int64  `tfsdk:"file_size_limit"`
	AllowedMimeTypes  types.Set    `tfsdk:"allowed_mime_types"`
	CacheControl      types.String `tfsdk:"cache_control"`
	AvifAutodetection types.Bool   `tfsdk:"avif_autodetection"`
	Metadata          types.Map    `tfsdk:"metadata"`
	Status            types.String `tfsdk:"status"`
	Message           types.String `tfsdk:"message"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

func NewProjectStorageBucketResource() resource.Resource {
	return &projectStorageBucketResource{}
}

func (r *projectStorageBucketResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_storage_bucket"
}

func (r *projectStorageBucketResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project storage bucket declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated storage bucket ID.",
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
				Description: "Storage bucket name. Must be 3-64 lowercase letters, numbers, or dashes.",
			},
			"public": resourceschema.BoolAttribute{
				Required:    true,
				Description: "Whether the bucket is publicly readable.",
			},
			"file_size_limit": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(defaultStorageBucketFileSizeLimit),
				Description: "Maximum object size in bytes.",
			},
			"allowed_mime_types": resourceschema.SetAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Allowed MIME types. Use an empty set to allow all MIME types.",
			},
			"cache_control": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("3600"),
				Description: "Default Cache-Control max-age or directive for objects in this bucket.",
			},
			"avif_autodetection": resourceschema.BoolAttribute{
				Required:    true,
				Description: "Whether AVIF autodetection is enabled for image transformations.",
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

func (r *projectStorageBucketResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectStorageBucketResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	requireResourceReplaceOnUpdate(ctx, req, resp, "name")
}

func (r *projectStorageBucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectStorageBucketResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := storageBucketInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	bucket, err := r.client.CreateProjectStorageBucket(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project storage bucket", err.Error())
		return
	}
	setProjectStorageBucketState(ctx, &plan, bucket, input.Metadata, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectStorageBucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectStorageBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	bucket, err := r.findStorageBucket(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project storage bucket", err.Error())
		return
	}
	previous, ok := optionalConfigMapFromTerraform(ctx, state.Metadata, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	setProjectStorageBucketState(ctx, &state, bucket, previous, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectStorageBucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	reportUnsupportedInPlaceUpdate(resp, "Supadupa project storage bucket")
}

func (r *projectStorageBucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectStorageBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectStorageBucket(ctx, state.Ref.ValueString(), state.Name.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project storage bucket", err.Error())
		return
	}
}

func (r *projectStorageBucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "name", "Use ref/name, for example alpha/assets.")
}

func (r *projectStorageBucketResource) findStorageBucket(ctx context.Context, ref string, name string) (ProjectStorageBucket, error) {
	buckets, err := r.client.ListProjectStorageBuckets(ctx, ref)
	if err != nil {
		return ProjectStorageBucket{}, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	return findInList(buckets, func(bucket ProjectStorageBucket) bool {
		if bucket.Name == normalized {
			return true
		}
		return false
	})
}

func storageBucketInputFromModel(ctx context.Context, model projectStorageBucketResourceModel, addError func(string, string)) (ProjectStorageBucketInput, bool) {
	mimeTypes, ok := stringSetFromTerraform(ctx, model.AllowedMimeTypes, "Invalid allowed_mime_types set", addError)
	if !ok {
		return ProjectStorageBucketInput{}, false
	}
	metadata, ok := configMapFromTerraform(ctx, model.Metadata, addError)
	if !ok {
		return ProjectStorageBucketInput{}, false
	}
	return ProjectStorageBucketInput{
		Name:              model.Name.ValueString(),
		Public:            model.Public.ValueBool(),
		FileSizeLimit:     model.FileSizeLimit.ValueInt64(),
		AllowedMimeTypes:  mimeTypes,
		CacheControl:      model.CacheControl.ValueString(),
		AvifAutodetection: model.AvifAutodetection.ValueBool(),
		Metadata:          metadata,
	}, true
}

func stringSetFromTerraform(ctx context.Context, value types.Set, title string, addError func(string, string)) ([]string, bool) {
	out := []string{}
	diags := value.ElementsAs(ctx, &out, false)
	if diags.HasError() {
		addError(title, diags.Errors()[0].Detail())
		return nil, false
	}
	return out, true
}

func setProjectStorageBucketState(ctx context.Context, model *projectStorageBucketResourceModel, bucket ProjectStorageBucket, previousMetadata map[string]string, addError func(string, string)) {
	model.ID = types.StringValue(bucket.ID)
	model.Ref = types.StringValue(bucket.ProjectRef)
	model.Name = types.StringValue(bucket.Name)
	model.Public = types.BoolValue(bucket.Public)
	model.FileSizeLimit = types.Int64Value(bucket.FileSizeLimit)
	model.CacheControl = types.StringValue(bucket.CacheControl)
	model.AvifAutodetection = types.BoolValue(bucket.AvifAutodetection)
	model.Status = types.StringValue(bucket.Status)
	model.Message = optionalStringValue(bucket.Message)
	model.CreatedAt = optionalTimeString(bucket.CreatedAt)
	model.UpdatedAt = optionalTimeString(bucket.UpdatedAt)

	mimeTypes, diags := types.SetValueFrom(ctx, types.StringType, bucket.AllowedMimeTypes)
	if diags.HasError() {
		addError("Unable to encode allowed_mime_types set", diags.Errors()[0].Detail())
		return
	}
	model.AllowedMimeTypes = mimeTypes
	metadata, ok := sensitiveStringMapStateValue(ctx, "metadata", bucket.Metadata, previousMetadata, addError)
	if !ok {
		return
	}
	model.Metadata = metadata
}
