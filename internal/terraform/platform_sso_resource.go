package terraform

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type platformSSOResource struct {
	client *Client
}

type platformSSOResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	SSOProvider   types.String `tfsdk:"sso_provider"`
	IDPEntityID   types.String `tfsdk:"idp_entity_id"`
	SSOURL        types.String `tfsdk:"sso_url"`
	Certificate   types.String `tfsdk:"certificate_pem"`
	ACSURL        types.String `tfsdk:"acs_url"`
	MetadataURL   types.String `tfsdk:"metadata_url"`
	EmailDomain   types.String `tfsdk:"email_domain"`
	AutoProvision types.Bool   `tfsdk:"auto_provision"`
	DefaultRole   types.String `tfsdk:"default_role"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func NewPlatformSSOResource() resource.Resource {
	return &platformSSOResource{}
}

func (r *platformSSOResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_platform_sso"
}

func (r *platformSSOResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa platform SAML SSO configuration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Stable platform SSO ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"enabled": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether platform SAML SSO is enabled.",
			},
			"sso_provider": resourceschema.StringAttribute{
				Computed:    true,
				Description: "SSO provider. Currently saml.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"idp_entity_id": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "SAML IdP entity ID.",
			},
			"sso_url": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "SAML IdP login URL.",
			},
			"certificate_pem": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Sensitive:   true,
				Default:     stringdefault.StaticString(""),
				Description: "SAML IdP signing certificate PEM.",
			},
			"acs_url": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "SAML assertion consumer service URL.",
			},
			"metadata_url": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Optional SAML IdP metadata URL.",
			},
			"email_domain": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Optional allowed email domain for SSO users.",
			},
			"auto_provision": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether valid SAML assertions may create missing platform users.",
			},
			"default_role": resourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("developer"),
				Description: "Default role for auto-provisioned SSO users: admin, developer, or viewer.",
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

func (r *platformSSOResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *platformSSOResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan platformSSOResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	config, err := r.client.UpdatePlatformSSOConfig(ctx, platformSSOInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa platform SSO", err.Error())
		return
	}
	setPlatformSSOState(&plan, config, plan.Certificate.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *platformSSOResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state platformSSOResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	config, err := r.client.GetPlatformSSOConfig(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa platform SSO", err.Error())
		return
	}
	setPlatformSSOState(&state, config, state.Certificate.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *platformSSOResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan platformSSOResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	config, err := r.client.UpdatePlatformSSOConfig(ctx, platformSSOInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update Supadupa platform SSO", err.Error())
		return
	}
	setPlatformSSOState(&plan, config, plan.Certificate.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *platformSSOResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	_, err := r.client.UpdatePlatformSSOConfig(ctx, defaultPlatformSSOInput())
	if err != nil {
		resp.Diagnostics.AddError("Unable to reset Supadupa platform SSO", err.Error())
	}
}

func (r *platformSSOResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setOnePartImportState(ctx, req.ID, resp, "id", "Use platform-sso.")
}

func platformSSOInputFromModel(model platformSSOResourceModel) PlatformSSOConfigInput {
	return PlatformSSOConfigInput{
		Enabled:       model.Enabled.ValueBool(),
		IDPEntityID:   model.IDPEntityID.ValueString(),
		SSOURL:        model.SSOURL.ValueString(),
		Certificate:   model.Certificate.ValueString(),
		ACSURL:        model.ACSURL.ValueString(),
		MetadataURL:   model.MetadataURL.ValueString(),
		EmailDomain:   model.EmailDomain.ValueString(),
		AutoProvision: model.AutoProvision.ValueBool(),
		DefaultRole:   model.DefaultRole.ValueString(),
	}
}

func defaultPlatformSSOInput() PlatformSSOConfigInput {
	return PlatformSSOConfigInput{DefaultRole: "developer"}
}

func setPlatformSSOState(model *platformSSOResourceModel, config PlatformSSOConfig, previousCertificate string) {
	model.ID = types.StringValue("platform-sso")
	model.Enabled = types.BoolValue(config.Enabled)
	model.SSOProvider = types.StringValue(config.Provider)
	model.IDPEntityID = types.StringValue(config.IDPEntityID)
	model.SSOURL = types.StringValue(config.SSOURL)
	model.Certificate = types.StringValue(preserveMaskedSensitiveValue(config.Certificate, previousCertificate))
	model.ACSURL = types.StringValue(config.ACSURL)
	model.MetadataURL = types.StringValue(config.MetadataURL)
	model.EmailDomain = types.StringValue(config.EmailDomain)
	model.AutoProvision = types.BoolValue(config.AutoProvision)
	model.DefaultRole = types.StringValue(config.DefaultRole)
	model.UpdatedAt = optionalTimeString(config.UpdatedAt)
}
