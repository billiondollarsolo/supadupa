package terraform

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type orgDataSource struct {
	client *Client
}

type orgDataSourceModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func NewOrgDataSource() datasource.DataSource {
	return &orgDataSource{}
}

func (d *orgDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org"
}

func (d *orgDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = datasourceschema.Schema{
		Description: "Look up a Supadupa organization by ID or name.",
		Attributes: map[string]datasourceschema.Attribute{
			"id": datasourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Organization ID.",
			},
			"name": datasourceschema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Organization display name.",
			},
		},
	}
}

func (d *orgDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *orgDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config orgDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if (config.ID.IsNull() || config.ID.IsUnknown()) && (config.Name.IsNull() || config.Name.IsUnknown()) {
		resp.Diagnostics.AddError("Missing organization selector", "Set id or name for the supadupa_org data source.")
		return
	}
	orgs, err := d.client.ListOrgs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Supadupa orgs", err.Error())
		return
	}
	id := stringValue(config.ID)
	name := stringValue(config.Name)
	var matches []Org
	for _, org := range orgs {
		if id != "" && org.ID == id {
			matches = append(matches, org)
			continue
		}
		if id == "" && name != "" && org.Name == name {
			matches = append(matches, org)
		}
	}
	if len(matches) == 0 {
		resp.Diagnostics.AddError("Organization not found", "No Supadupa organization matched the configured selector.")
		return
	}
	if len(matches) > 1 {
		resp.Diagnostics.AddError("Organization selector is ambiguous", "Multiple Supadupa organizations matched; use id instead of name.")
		return
	}
	config.ID = types.StringValue(matches[0].ID)
	config.Name = types.StringValue(matches[0].Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
