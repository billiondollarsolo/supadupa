package terraform

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type platformDefaultsResource struct {
	client *Client
}

type platformDefaultsResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	Domain             types.String `tfsdk:"domain"`
	StackVersion       types.String `tfsdk:"stack_version"`
	Profile            types.String `tfsdk:"profile"`
	ResourceTier       types.String `tfsdk:"resource_tier"`
	BackupSchedule     types.String `tfsdk:"backup_schedule"`
	SMTPEnabled        types.Bool   `tfsdk:"smtp_enabled"`
	SMTPHost           types.String `tfsdk:"smtp_host"`
	SMTPPort           types.Int64  `tfsdk:"smtp_port"`
	SMTPSenderName     types.String `tfsdk:"smtp_sender_name"`
	SMTPSenderEmail    types.String `tfsdk:"smtp_sender_email"`
	SMTPUsername       types.String `tfsdk:"smtp_username"`
	SMTPPasswordHandle types.String `tfsdk:"smtp_password_handle"`
	SMTPTLSMode        types.String `tfsdk:"smtp_tls_mode"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func NewPlatformDefaultsResource() resource.Resource {
	return &platformDefaultsResource{}
}

func (r *platformDefaultsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_platform_defaults"
}

func (r *platformDefaultsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa platform project-creation defaults managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Stable platform defaults ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"domain": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("supadupa.test"),
				Description: "Default base domain for new projects.",
			},
			"stack_version": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("latest"),
				Description: "Default upstream Supabase stack version for new projects.",
			},
			"profile": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("full"),
				Description: "Default stack profile: full, essential, or orioledb.",
			},
			"resource_tier": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("small"),
				Description: "Default resource tier: small, medium, or large.",
			},
			"backup_schedule": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("daily"),
				Description: "Default scheduled backup cadence: daily or hourly.",
			},
			"smtp_enabled": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether platform SMTP defaults are enabled for platform mail and integrations.",
			},
			"smtp_host": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Platform SMTP host.",
			},
			"smtp_port": resourceschema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(587),
				Description: "Platform SMTP port.",
			},
			"smtp_sender_name": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Platform SMTP sender display name.",
			},
			"smtp_sender_email": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Platform SMTP sender email.",
			},
			"smtp_username": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Platform SMTP username.",
			},
			"smtp_password_handle": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Platform SMTP password secret:// handle. Raw SMTP passwords are rejected by the API.",
				Sensitive:   true,
			},
			"smtp_tls_mode": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("starttls"),
				Description: "Platform SMTP TLS mode: starttls, implicit, or none.",
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

func (r *platformDefaultsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *platformDefaultsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan platformDefaultsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	defaults, err := r.client.UpdatePlatformDefaults(ctx, platformDefaultsInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa platform defaults", err.Error())
		return
	}
	setPlatformDefaultsState(&plan, defaults)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *platformDefaultsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state platformDefaultsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	defaults, err := r.client.GetPlatformDefaults(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa platform defaults", err.Error())
		return
	}
	setPlatformDefaultsState(&state, defaults)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *platformDefaultsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan platformDefaultsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	defaults, err := r.client.UpdatePlatformDefaults(ctx, platformDefaultsInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa platform defaults", err.Error())
		return
	}
	setPlatformDefaultsState(&plan, defaults)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *platformDefaultsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	_, err := r.client.UpdatePlatformDefaults(ctx, defaultPlatformDefaultsInput())
	if err != nil {
		resp.Diagnostics.AddError("Unable to reset Supadupa platform defaults", err.Error())
	}
}

func (r *platformDefaultsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func platformDefaultsInputFromModel(model platformDefaultsResourceModel) PlatformDefaultsInput {
	return PlatformDefaultsInput{
		Domain:         model.Domain.ValueString(),
		StackVersion:   model.StackVersion.ValueString(),
		Profile:        model.Profile.ValueString(),
		ResourceTier:   model.ResourceTier.ValueString(),
		BackupSchedule: model.BackupSchedule.ValueString(),
		SMTP: PlatformSMTP{
			Enabled:        model.SMTPEnabled.ValueBool(),
			Host:           model.SMTPHost.ValueString(),
			Port:           int(model.SMTPPort.ValueInt64()),
			SenderName:     model.SMTPSenderName.ValueString(),
			SenderEmail:    model.SMTPSenderEmail.ValueString(),
			Username:       model.SMTPUsername.ValueString(),
			PasswordHandle: model.SMTPPasswordHandle.ValueString(),
			TLSMode:        model.SMTPTLSMode.ValueString(),
		},
	}
}

func defaultPlatformDefaultsInput() PlatformDefaultsInput {
	return PlatformDefaultsInput{
		Domain:         "supadupa.test",
		StackVersion:   "latest",
		Profile:        "full",
		ResourceTier:   "small",
		BackupSchedule: "daily",
		SMTP: PlatformSMTP{
			Port:    587,
			TLSMode: "starttls",
		},
	}
}

func setPlatformDefaultsState(model *platformDefaultsResourceModel, defaults PlatformDefaults) {
	model.ID = types.StringValue("platform-defaults")
	model.Domain = types.StringValue(defaults.Domain)
	model.StackVersion = types.StringValue(defaults.StackVersion)
	model.Profile = types.StringValue(defaults.Profile)
	model.ResourceTier = types.StringValue(defaults.ResourceTier)
	model.BackupSchedule = types.StringValue(defaults.BackupSchedule)
	model.SMTPEnabled = types.BoolValue(defaults.SMTP.Enabled)
	model.SMTPHost = types.StringValue(defaults.SMTP.Host)
	model.SMTPPort = types.Int64Value(int64(defaults.SMTP.Port))
	model.SMTPSenderName = types.StringValue(defaults.SMTP.SenderName)
	model.SMTPSenderEmail = types.StringValue(defaults.SMTP.SenderEmail)
	model.SMTPUsername = types.StringValue(defaults.SMTP.Username)
	model.SMTPPasswordHandle = types.StringValue(defaults.SMTP.PasswordHandle)
	model.SMTPTLSMode = types.StringValue(defaults.SMTP.TLSMode)
	model.UpdatedAt = optionalTimeString(defaults.UpdatedAt)
}
