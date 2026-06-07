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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type backupStorageTargetResource struct {
	client *Client
}

type backupStorageTargetResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Type             types.String `tfsdk:"type"`
	Endpoint         types.String `tfsdk:"endpoint"`
	Region           types.String `tfsdk:"region"`
	Bucket           types.String `tfsdk:"bucket"`
	Prefix           types.String `tfsdk:"prefix"`
	AccessKeyID      types.String `tfsdk:"access_key_id"`
	SecretAccessKey  types.String `tfsdk:"secret_access_key"`
	SecretConfigured types.Bool   `tfsdk:"secret_configured"`
	ForcePathStyle   types.Bool   `tfsdk:"force_path_style"`
	Default          types.Bool   `tfsdk:"default"`
	DurableOffHost   types.Bool   `tfsdk:"durable_off_host"`
	RecoveryReady    types.Bool   `tfsdk:"recovery_ready"`
	ReadinessStatus  types.String `tfsdk:"readiness_status"`
	ReadinessMessage types.String `tfsdk:"readiness_message"`
	LastTestedAt     types.String `tfsdk:"last_tested_at"`
	LastTestStatus   types.String `tfsdk:"last_test_status"`
	LastTestError    types.String `tfsdk:"last_test_error"`
	CreatedAt        types.String `tfsdk:"created_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
}

func NewBackupStorageTargetResource() resource.Resource {
	return &backupStorageTargetResource{}
}

func (r *backupStorageTargetResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_backup_storage_target"
}

func (r *backupStorageTargetResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa S3-compatible backup storage target for off-host project and platform recovery artifacts.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Backup storage target ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": resourceschema.StringAttribute{
				Required:    true,
				Description: "Target display name.",
			},
			"type": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("s3"),
				Description: "Target type. Currently s3-compatible targets are supported.",
			},
			"endpoint": resourceschema.StringAttribute{
				Required:    true,
				Description: "S3-compatible endpoint URL.",
			},
			"region": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("auto"),
				Description: "S3 region.",
			},
			"bucket": resourceschema.StringAttribute{
				Required:    true,
				Description: "S3 bucket name.",
			},
			"prefix": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Object key prefix.",
			},
			"access_key_id": resourceschema.StringAttribute{
				Required:    true,
				Description: "S3 access key ID.",
			},
			"secret_access_key": resourceschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "S3 secret access key. Required on create; omit on update/import to preserve the stored secret.",
			},
			"secret_configured": resourceschema.BoolAttribute{
				Computed:    true,
				Description: "Whether a secret access key is configured on the control plane.",
			},
			"force_path_style": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Use S3 path-style addressing.",
			},
			"default": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether this target is the platform default backup storage target.",
			},
			"durable_off_host": resourceschema.BoolAttribute{
				Computed:    true,
				Description: "Whether the endpoint is considered durable and off-host by recoverability gates.",
			},
			"recovery_ready": resourceschema.BoolAttribute{
				Computed:    true,
				Description: "Whether the target has passed all recovery-readiness gates.",
			},
			"readiness_status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Readiness classifier such as off-host-ready, validation-pending, validation-failed, local-or-loopback, or missing-secret.",
			},
			"readiness_message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable readiness message.",
			},
			"last_tested_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Last target probe timestamp.",
			},
			"last_test_status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Last target probe status.",
			},
			"last_test_error": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Last target probe error, if any.",
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp.",
			},
			"updated_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Update timestamp.",
			},
		},
	}
}

func (r *backupStorageTargetResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *backupStorageTargetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan backupStorageTargetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if strings.TrimSpace(stringValue(plan.SecretAccessKey)) == "" {
		resp.Diagnostics.AddError("Missing secret access key", "secret_access_key is required when creating a backup storage target.")
		return
	}
	target, err := r.client.CreateBackupStorageTarget(ctx, backupStorageTargetInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa backup storage target", err.Error())
		return
	}
	setBackupStorageTargetState(&plan, target)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *backupStorageTargetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state backupStorageTargetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	target, err := r.findTarget(ctx, state.ID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa backup storage target", err.Error())
		return
	}
	setBackupStorageTargetState(&state, target)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *backupStorageTargetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan backupStorageTargetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	target, err := r.client.UpdateBackupStorageTarget(ctx, plan.ID.ValueString(), backupStorageTargetInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa backup storage target", err.Error())
		return
	}
	setBackupStorageTargetState(&plan, target)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *backupStorageTargetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state backupStorageTargetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteBackupStorageTarget(ctx, state.ID.ValueString()); err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa backup storage target", err.Error())
	}
}

func (r *backupStorageTargetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if strings.TrimSpace(req.ID) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use the backup storage target ID.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.TrimSpace(req.ID))...)
}

func (r *backupStorageTargetResource) findTarget(ctx context.Context, id string) (BackupStorageTarget, error) {
	targets, err := r.client.ListBackupStorageTargets(ctx)
	if err != nil {
		return BackupStorageTarget{}, err
	}
	for _, target := range targets {
		if target.ID == id {
			return target, nil
		}
	}
	return BackupStorageTarget{}, ErrNotFound
}

func backupStorageTargetInputFromModel(model backupStorageTargetResourceModel) BackupStorageTargetInput {
	return BackupStorageTargetInput{
		Name:            model.Name.ValueString(),
		Type:            model.Type.ValueString(),
		Endpoint:        model.Endpoint.ValueString(),
		Region:          model.Region.ValueString(),
		Bucket:          model.Bucket.ValueString(),
		Prefix:          stringValue(model.Prefix),
		AccessKeyID:     model.AccessKeyID.ValueString(),
		SecretAccessKey: stringValue(model.SecretAccessKey),
		ForcePathStyle:  model.ForcePathStyle.ValueBool(),
		Default:         model.Default.ValueBool(),
	}
}

func setBackupStorageTargetState(model *backupStorageTargetResourceModel, target BackupStorageTarget) {
	model.ID = types.StringValue(target.ID)
	model.Name = types.StringValue(target.Name)
	model.Type = types.StringValue(target.Type)
	model.Endpoint = types.StringValue(target.Endpoint)
	model.Region = types.StringValue(target.Region)
	model.Bucket = types.StringValue(target.Bucket)
	model.Prefix = types.StringValue(target.Prefix)
	model.AccessKeyID = optionalStringValue(target.AccessKeyID)
	model.SecretConfigured = types.BoolValue(target.SecretConfigured)
	model.ForcePathStyle = types.BoolValue(target.ForcePathStyle)
	model.Default = types.BoolValue(target.Default)
	model.DurableOffHost = types.BoolValue(target.DurableOffHost)
	model.RecoveryReady = types.BoolValue(target.RecoveryReady)
	model.ReadinessStatus = types.StringValue(target.ReadinessStatus)
	model.ReadinessMessage = optionalStringValue(target.ReadinessMessage)
	model.LastTestedAt = optionalTimePointerString(target.LastTestedAt)
	model.LastTestStatus = optionalStringValue(target.LastTestStatus)
	model.LastTestError = optionalStringValue(target.LastTestError)
	model.CreatedAt = optionalTimeString(target.CreatedAt)
	model.UpdatedAt = optionalTimeString(target.UpdatedAt)
}
