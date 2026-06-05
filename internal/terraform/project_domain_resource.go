package terraform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
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
	ID         types.String `tfsdk:"id"`
	Ref        types.String `tfsdk:"ref"`
	FQDN       types.String `tfsdk:"fqdn"`
	CertStatus types.String `tfsdk:"cert_status"`
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
		},
	}
}

func (r *projectDomainResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
	ref, fqdn, ok := strings.Cut(req.ID, "/")
	if !ok {
		ref, fqdn, ok = strings.Cut(req.ID, ":")
	}
	if !ok || strings.TrimSpace(ref) == "" || strings.TrimSpace(fqdn) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use ref/fqdn, for example alpha/api.example.com.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ref"), strings.TrimSpace(ref))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("fqdn"), strings.TrimSpace(fqdn))...)
}

func (r *projectDomainResource) findDomain(ctx context.Context, ref string, fqdn string) (ProjectDomain, error) {
	domains, err := r.client.ListProjectDomains(ctx, ref)
	if err != nil {
		return ProjectDomain{}, err
	}
	normalized := strings.Trim(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	for _, domain := range domains {
		if domain.FQDN == normalized {
			return domain, nil
		}
	}
	return ProjectDomain{}, ErrNotFound
}

func setProjectDomainState(model *projectDomainResourceModel, domain ProjectDomain) {
	model.ID = types.StringValue(domain.ProjectRef + "/" + domain.FQDN)
	model.Ref = types.StringValue(domain.ProjectRef)
	model.FQDN = types.StringValue(domain.FQDN)
	model.CertStatus = types.StringValue(domain.CertStatus)
}
