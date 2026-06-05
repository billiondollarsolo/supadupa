package terraform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectAuthClientResource struct {
	client *Client
}

type projectAuthClientResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	Ref                types.String `tfsdk:"ref"`
	Name               types.String `tfsdk:"name"`
	ClientID           types.String `tfsdk:"client_id"`
	ClientSecretHandle types.String `tfsdk:"client_secret_handle"`
	RedirectURIs       types.List   `tfsdk:"redirect_uris"`
	GrantTypes         types.List   `tfsdk:"grant_types"`
	Scopes             types.List   `tfsdk:"scopes"`
	Confidential       types.Bool   `tfsdk:"confidential"`
	Status             types.String `tfsdk:"status"`
	Message            types.String `tfsdk:"message"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func NewProjectAuthClientResource() resource.Resource {
	return &projectAuthClientResource{}
}

func (r *projectAuthClientResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_auth_client"
}

func (r *projectAuthClientResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	defaultGrantTypes := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("authorization_code"), types.StringValue("refresh_token")})
	defaultScopes := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("openid"), types.StringValue("profile"), types.StringValue("email")})
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project OAuth 2.1 client registration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated auth client record ID.",
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
				Description: "OAuth client display name.",
			},
			"client_id": resourceschema.StringAttribute{
				Required:    true,
				Description: "OAuth client ID.",
			},
			"client_secret_handle": resourceschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "secret:// handle for confidential OAuth client secret material.",
			},
			"redirect_uris": resourceschema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Allowed OAuth redirect URIs.",
			},
			"grant_types": resourceschema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     listdefault.StaticValue(defaultGrantTypes),
				Description: "Allowed OAuth grant types.",
			},
			"scopes": resourceschema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     listdefault.StaticValue(defaultScopes),
				Description: "Allowed OAuth scopes.",
			},
			"confidential": resourceschema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether this OAuth client is confidential.",
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Auth client registration status.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable auth client status message.",
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

func (r *projectAuthClientResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *projectAuthClientResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectAuthClientResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := authClientInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	client, err := r.client.CreateProjectAuthClient(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project auth client", err.Error())
		return
	}
	setProjectAuthClientState(ctx, &plan, client, input.ClientSecretHandle, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectAuthClientResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectAuthClientResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client, err := r.findAuthClient(ctx, state.Ref.ValueString(), state.ClientID.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project auth client", err.Error())
		return
	}
	setProjectAuthClientState(ctx, &state, client, previousSensitiveString(state.ClientSecretHandle), resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectAuthClientResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan projectAuthClientResourceModel
	var state projectAuthClientResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input, ok := authClientInputFromModel(ctx, plan, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	err := r.client.DeleteProjectAuthClient(ctx, state.Ref.ValueString(), state.ClientID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to replace Supadupa project auth client", err.Error())
		return
	}
	client, err := r.client.CreateProjectAuthClient(ctx, plan.Ref.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to recreate Supadupa project auth client", err.Error())
		return
	}
	setProjectAuthClientState(ctx, &plan, client, input.ClientSecretHandle, resp.Diagnostics.AddError)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectAuthClientResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectAuthClientResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectAuthClient(ctx, state.Ref.ValueString(), state.ClientID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project auth client", err.Error())
		return
	}
}

func (r *projectAuthClientResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ref, clientID, ok := strings.Cut(req.ID, "/")
	if !ok {
		ref, clientID, ok = strings.Cut(req.ID, ":")
	}
	if !ok || strings.TrimSpace(ref) == "" || strings.TrimSpace(clientID) == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use ref/client_id, for example alpha/dashboard_app.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ref"), strings.TrimSpace(ref))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("client_id"), strings.TrimSpace(clientID))...)
}

func (r *projectAuthClientResource) findAuthClient(ctx context.Context, ref string, clientID string) (ProjectAuthClient, error) {
	clients, err := r.client.ListProjectAuthClients(ctx, ref)
	if err != nil {
		return ProjectAuthClient{}, err
	}
	for _, client := range clients {
		if client.ClientID == clientID {
			return client, nil
		}
	}
	return ProjectAuthClient{}, ErrNotFound
}

func authClientInputFromModel(ctx context.Context, model projectAuthClientResourceModel, addError func(string, string)) (ProjectAuthClientInput, bool) {
	redirectURIs, ok := stringListFromTerraform(ctx, model.RedirectURIs, "Invalid redirect_uris list", addError)
	if !ok {
		return ProjectAuthClientInput{}, false
	}
	grantTypes, ok := stringListFromTerraform(ctx, model.GrantTypes, "Invalid grant_types list", addError)
	if !ok {
		return ProjectAuthClientInput{}, false
	}
	scopes, ok := stringListFromTerraform(ctx, model.Scopes, "Invalid scopes list", addError)
	if !ok {
		return ProjectAuthClientInput{}, false
	}
	return ProjectAuthClientInput{
		Name:               model.Name.ValueString(),
		ClientID:           model.ClientID.ValueString(),
		ClientSecretHandle: model.ClientSecretHandle.ValueString(),
		RedirectURIs:       redirectURIs,
		GrantTypes:         grantTypes,
		Scopes:             scopes,
		Confidential:       model.Confidential.ValueBool(),
	}, true
}

func setProjectAuthClientState(ctx context.Context, model *projectAuthClientResourceModel, client ProjectAuthClient, previousSecretHandle string, addError func(string, string)) {
	model.ID = types.StringValue(client.ID)
	model.Ref = types.StringValue(client.ProjectRef)
	model.Name = types.StringValue(client.Name)
	model.ClientID = types.StringValue(client.ClientID)
	model.ClientSecretHandle = sensitiveStringValue(preserveMaskedSensitiveValue(client.ClientSecretHandle, previousSecretHandle))
	model.Confidential = types.BoolValue(client.Confidential)
	model.Status = types.StringValue(client.Status)
	model.Message = optionalStringValue(client.Message)
	model.CreatedAt = optionalTimeString(client.CreatedAt)
	model.UpdatedAt = optionalTimeString(client.UpdatedAt)

	redirectURIs, diags := types.ListValueFrom(ctx, types.StringType, client.RedirectURIs)
	if diags.HasError() {
		addError("Unable to encode redirect_uris list", diags.Errors()[0].Detail())
		return
	}
	model.RedirectURIs = redirectURIs
	grantTypes, diags := types.ListValueFrom(ctx, types.StringType, client.GrantTypes)
	if diags.HasError() {
		addError("Unable to encode grant_types list", diags.Errors()[0].Detail())
		return
	}
	model.GrantTypes = grantTypes
	scopes, diags := types.ListValueFrom(ctx, types.StringType, client.Scopes)
	if diags.HasError() {
		addError("Unable to encode scopes list", diags.Errors()[0].Detail())
		return
	}
	model.Scopes = scopes
}
