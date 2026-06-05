package terraform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectFunctionStorageMountResource struct {
	client *Client
}

type projectFunctionStorageMountResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Ref          types.String `tfsdk:"ref"`
	FunctionName types.String `tfsdk:"function_name"`
	BucketName   types.String `tfsdk:"bucket_name"`
	MountPath    types.String `tfsdk:"mount_path"`
	ReadOnly     types.Bool   `tfsdk:"read_only"`
	Prefix       types.String `tfsdk:"prefix"`
	EnvAlias     types.String `tfsdk:"env_alias"`
	Status       types.String `tfsdk:"status"`
	Message      types.String `tfsdk:"message"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

func NewProjectFunctionStorageMountResource() resource.Resource {
	return &projectFunctionStorageMountResource{}
}

func (r *projectFunctionStorageMountResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_function_storage_mount"
}

func (r *projectFunctionStorageMountResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa Edge Function persistent storage mount declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated function storage mount declaration ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replace,
			},
			"function_name": resourceschema.StringAttribute{
				Required:    true,
				Description: "Deployed Edge Function name.",
			},
			"bucket_name": resourceschema.StringAttribute{
				Required:    true,
				Description: "Storage bucket name to mount into the Edge Function runtime.",
			},
			"mount_path": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Absolute mount path under /mnt. Empty input derives /mnt/<bucket_name>.",
			},
			"read_only": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the mount should be read-only.",
			},
			"prefix": resourceschema.StringAttribute{
				Optional:    true,
				Description: "Optional bucket prefix exposed through this mount.",
			},
			"env_alias": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Environment variable alias for the mount path. Empty input derives FUNCTION_BUCKET_MOUNT.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Function storage mount status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable function storage mount status message.",
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

func (r *projectFunctionStorageMountResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *projectFunctionStorageMountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectFunctionStorageMountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	mount, err := r.client.CreateProjectFunctionStorageMount(ctx, plan.Ref.ValueString(), functionStorageMountInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project function storage mount", err.Error())
		return
	}
	setProjectFunctionStorageMountState(&plan, mount)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectFunctionStorageMountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectFunctionStorageMountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	mount, err := r.findFunctionStorageMount(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project function storage mount", err.Error())
		return
	}
	setProjectFunctionStorageMountState(&state, mount)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectFunctionStorageMountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectFunctionStorageMountResourceModel
	var state projectFunctionStorageMountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectFunctionStorageMount(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to replace Supadupa project function storage mount", err.Error())
		return
	}
	mount, err := r.client.CreateProjectFunctionStorageMount(ctx, plan.Ref.ValueString(), functionStorageMountInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to recreate Supadupa project function storage mount", err.Error())
		return
	}
	setProjectFunctionStorageMountState(&plan, mount)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectFunctionStorageMountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectFunctionStorageMountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectFunctionStorageMount(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project function storage mount", err.Error())
		return
	}
}

func (r *projectFunctionStorageMountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ref, id, ok := strings.Cut(req.ID, "/")
	if !ok {
		ref, id, ok = strings.Cut(req.ID, ":")
	}
	if !ok || strings.TrimSpace(ref) == "" || strings.TrimSpace(id) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use ref/id, for example alpha/mount_123.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ref"), strings.TrimSpace(ref))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.TrimSpace(id))...)
}

func (r *projectFunctionStorageMountResource) findFunctionStorageMount(ctx context.Context, ref string, id string) (ProjectFunctionStorageMount, error) {
	mounts, err := r.client.ListProjectFunctionStorageMounts(ctx, ref)
	if err != nil {
		return ProjectFunctionStorageMount{}, err
	}
	for _, mount := range mounts {
		if mount.ID == id {
			return mount, nil
		}
	}
	return ProjectFunctionStorageMount{}, ErrNotFound
}

func functionStorageMountInputFromModel(model projectFunctionStorageMountResourceModel) ProjectFunctionStorageMountInput {
	return ProjectFunctionStorageMountInput{
		FunctionName: model.FunctionName.ValueString(),
		BucketName:   model.BucketName.ValueString(),
		MountPath:    model.MountPath.ValueString(),
		ReadOnly:     model.ReadOnly.ValueBool(),
		Prefix:       model.Prefix.ValueString(),
		EnvAlias:     model.EnvAlias.ValueString(),
	}
}

func setProjectFunctionStorageMountState(model *projectFunctionStorageMountResourceModel, mount ProjectFunctionStorageMount) {
	model.ID = types.StringValue(mount.ID)
	model.Ref = types.StringValue(mount.ProjectRef)
	model.FunctionName = types.StringValue(mount.FunctionName)
	model.BucketName = types.StringValue(mount.BucketName)
	model.MountPath = types.StringValue(mount.MountPath)
	model.ReadOnly = types.BoolValue(mount.ReadOnly)
	model.Prefix = optionalStringValue(mount.Prefix)
	model.EnvAlias = optionalStringValue(mount.EnvAlias)
	model.Status = types.StringValue(mount.Status)
	model.Message = optionalStringValue(mount.Message)
	model.CreatedAt = optionalTimeString(mount.CreatedAt)
	model.UpdatedAt = optionalTimeString(mount.UpdatedAt)
}
