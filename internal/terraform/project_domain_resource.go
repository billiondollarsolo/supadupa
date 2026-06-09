package terraform

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectDomainResource struct {
	client *Client
}

type projectDomainResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Ref          types.String `tfsdk:"ref"`
	FQDN         types.String `tfsdk:"fqdn"`
	CertStatus   types.String `tfsdk:"cert_status"`
	CertMode     types.String `tfsdk:"cert_mode"`
	APIURL       types.String `tfsdk:"api_url"`
	ReadyAPIURL  types.String `tfsdk:"ready_api_url"`
	RESTURL      types.String `tfsdk:"rest_url"`
	AuthURL      types.String `tfsdk:"auth_url"`
	GraphQLURL   types.String `tfsdk:"graphql_url"`
	RealtimeURL  types.String `tfsdk:"realtime_url"`
	FunctionsURL types.String `tfsdk:"functions_url"`
	StorageURL   types.String `tfsdk:"storage_url"`
}

func NewProjectDomainResource() resource.Resource {
	return &projectDomainResource{}
}

func (r *projectDomainResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_domain"
}

func (r *projectDomainResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project custom domain managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Stable domain ID in the form ref/fqdn.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replace,
			},
			"fqdn": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Custom domain FQDN.",
				PlanModifiers: replace,
			},
			"cert_status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Certificate status reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"cert_mode": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Certificate mode reported by the control plane.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"api_url": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Supabase-compatible API URL for this custom domain.",
			},
			"ready_api_url": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Supabase-compatible API URL when the custom domain has an issued or uploaded certificate; empty while pending or failed.",
			},
			"rest_url": resourceschema.StringAttribute{
				Computed:    true,
				Description: "PostgREST URL for this custom domain.",
			},
			"auth_url": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Auth URL for this custom domain.",
			},
			"graphql_url": resourceschema.StringAttribute{
				Computed:    true,
				Description: "GraphQL URL for this custom domain.",
			},
			"realtime_url": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Realtime URL for this custom domain.",
			},
			"functions_url": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Edge Functions URL for this custom domain.",
			},
			"storage_url": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Storage REST URL for this custom domain.",
			},
		},
	}
}

func (r *projectDomainResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectDomainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain, err := r.client.AddProjectDomain(ctx, plan.Ref.ValueString(), plan.FQDN.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to add Supadupa project domain", err.Error())
		return
	}
	setProjectDomainState(&plan, domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectDomainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain, err := r.findDomain(ctx, state.Ref.ValueString(), state.FQDN.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project domain", err.Error())
		return
	}
	setProjectDomainState(&state, domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectDomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Supadupa project domain updates require replacement", "Domain FQDN and project ref are replace-on-change.")
}

func (r *projectDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectDomainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectDomain(ctx, state.Ref.ValueString(), state.FQDN.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project domain", err.Error())
		return
	}
}

func (r *projectDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "fqdn", "Use ref/fqdn, for example alpha/api.example.com.")
}

func (r *projectDomainResource) findDomain(ctx context.Context, ref string, fqdn string) (ProjectDomain, error) {
	domains, err := r.client.ListProjectDomains(ctx, ref)
	if err != nil {
		return ProjectDomain{}, err
	}
	normalized := strings.Trim(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	return findInList(domains, func(domain ProjectDomain) bool { return domain.FQDN == normalized })
}

func setProjectDomainState(model *projectDomainResourceModel, domain ProjectDomain) {
	model.ID = types.StringValue(domain.ProjectRef + "/" + domain.FQDN)
	model.Ref = types.StringValue(domain.ProjectRef)
	model.FQDN = types.StringValue(domain.FQDN)
	model.CertStatus = types.StringValue(domain.CertStatus)
	model.CertMode = types.StringValue(domain.CertMode)
	apiURL := "https://" + domain.FQDN
	model.APIURL = types.StringValue(apiURL)
	if projectDomainCertificateReady(domain.CertStatus) {
		model.ReadyAPIURL = types.StringValue(apiURL)
	} else {
		model.ReadyAPIURL = types.StringValue("")
	}
	model.RESTURL = types.StringValue(apiURL + "/rest/v1")
	model.AuthURL = types.StringValue(apiURL + "/auth/v1")
	model.GraphQLURL = types.StringValue(apiURL + "/graphql/v1")
	model.RealtimeURL = types.StringValue(apiURL + "/realtime/v1")
	model.FunctionsURL = types.StringValue(apiURL + "/functions/v1")
	model.StorageURL = types.StringValue(apiURL + "/storage/v1")
}

func projectDomainCertificateReady(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "issued", "uploaded":
		return true
	default:
		return false
	}
}
