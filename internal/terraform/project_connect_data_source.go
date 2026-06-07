package terraform

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectConnectDataSource struct {
	client *Client
}

type projectConnectDataSourceModel struct {
	Ref                        types.String `tfsdk:"ref"`
	APIURL                     types.String `tfsdk:"api_url"`
	StudioURL                  types.String `tfsdk:"studio_url"`
	RESTURL                    types.String `tfsdk:"rest_url"`
	AuthURL                    types.String `tfsdk:"auth_url"`
	GraphQLURL                 types.String `tfsdk:"graphql_url"`
	RealtimeURL                types.String `tfsdk:"realtime_url"`
	FunctionsURL               types.String `tfsdk:"functions_url"`
	StorageURL                 types.String `tfsdk:"storage_url"`
	StorageS3URL               types.String `tfsdk:"storage_s3_url"`
	CustomAPIURLs              types.List   `tfsdk:"custom_api_urls"`
	AnonKeyHandle              types.String `tfsdk:"anon_key_handle"`
	ServiceRoleKeyHandle       types.String `tfsdk:"service_role_key_handle"`
	PublicDatabaseURL          types.String `tfsdk:"public_database_url"`
	PublicPoolerTransactionURL types.String `tfsdk:"public_pooler_transaction_url"`
	PublicPoolerSessionURL     types.String `tfsdk:"public_pooler_session_url"`
}

func NewProjectConnectDataSource() datasource.DataSource {
	return &projectConnectDataSource{}
}

func (d *projectConnectDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_connect"
}

func (d *projectConnectDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		Description: "Fetch non-secret Supabase-compatible remote connection metadata for a Supadupa project.",
		Attributes: map[string]datasourceschema.Attribute{
			"ref": datasourceschema.StringAttribute{
				Required:    true,
				Description: "Project ref.",
			},
			"api_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Canonical generated project API URL.",
			},
			"studio_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Project Studio URL.",
			},
			"rest_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "PostgREST URL.",
			},
			"auth_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Auth URL.",
			},
			"graphql_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "GraphQL URL.",
			},
			"realtime_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Realtime URL.",
			},
			"functions_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Edge Functions URL.",
			},
			"storage_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Storage REST URL.",
			},
			"storage_s3_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Storage S3 protocol URL.",
			},
			"custom_api_urls": datasourceschema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Ready custom API URLs with issued or uploaded certificates.",
			},
			"anon_key_handle": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Secret handle for the project anon key.",
			},
			"service_role_key_handle": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Secret handle for the project service-role key.",
			},
			"public_database_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Public direct Postgres URL template.",
			},
			"public_pooler_transaction_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Public transaction pooler URL template.",
			},
			"public_pooler_session_url": datasourceschema.StringAttribute{
				Computed:    true,
				Description: "Public session pooler URL template.",
			},
		},
	}
}

func (d *projectConnectDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *terraform.Client, got %T.", req.ProviderData))
		return
	}
	d.client = client
}

func (d *projectConnectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config projectConnectDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ref := stringValue(config.Ref)
	if ref == "" {
		resp.Diagnostics.AddError("Missing project ref", "Set ref for the supadupa_project_connect data source.")
		return
	}
	connect, err := d.client.GetProjectConnect(ctx, ref)
	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch Supadupa project connect metadata", err.Error())
		return
	}
	setProjectConnectDataSourceState(ctx, &config, ref, connect, func(title string, detail string) {
		resp.Diagnostics.AddError(title, detail)
	})
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

func setProjectConnectDataSourceState(ctx context.Context, model *projectConnectDataSourceModel, ref string, connect ProjectConnect, addError func(string, string)) {
	model.Ref = types.StringValue(ref)
	model.APIURL = types.StringValue(connect.APIURL)
	model.StudioURL = types.StringValue(connect.StudioURL)
	model.RESTURL = types.StringValue(connect.RESTURL)
	model.AuthURL = types.StringValue(connect.AuthURL)
	model.GraphQLURL = types.StringValue(connect.GraphQLURL)
	model.RealtimeURL = types.StringValue(connect.RealtimeURL)
	model.FunctionsURL = types.StringValue(connect.FunctionsURL)
	model.StorageURL = types.StringValue(connect.StorageURL)
	model.StorageS3URL = types.StringValue(connect.StorageS3URL)
	customAPIURLs, diags := types.ListValueFrom(ctx, types.StringType, connect.CustomAPIURLs)
	if diags.HasError() {
		addError("Unable to encode custom API URLs", diags.Errors()[0].Detail())
		customAPIURLs = types.ListNull(types.StringType)
	}
	model.CustomAPIURLs = customAPIURLs
	model.AnonKeyHandle = types.StringValue(connect.APIKeys["anon"])
	model.ServiceRoleKeyHandle = types.StringValue(connect.APIKeys["service_role"])
	model.PublicDatabaseURL = types.StringValue(connect.Postgres["public_direct"])
	model.PublicPoolerTransactionURL = types.StringValue(connect.Postgres["public_transaction"])
	model.PublicPoolerSessionURL = types.StringValue(connect.Postgres["public_session"])
}
