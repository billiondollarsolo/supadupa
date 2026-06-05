package terraform

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type supadupaProvider struct {
	version string
}

type providerModel struct {
	APIURL types.String `tfsdk:"api_url"`
	Token  types.String `tfsdk:"token"`
}

func NewProvider(version string) func() provider.Provider {
	return func() provider.Provider {
		return &supadupaProvider{version: version}
	}
}

func (p *supadupaProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "supadupa"
	resp.Version = p.version
}

func (p *supadupaProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerschema.Schema{
		Attributes: map[string]providerschema.Attribute{
			"api_url": providerschema.StringAttribute{
				Optional:    true,
				Description: "Supadupa Management API base URL. Defaults to SUPADUPA_API_URL or http://localhost:8080.",
			},
			"token": providerschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Bearer token for the Management API. Defaults to SUPADUPA_TOKEN.",
			},
		},
	}
}

func (p *supadupaProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.APIURL.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_url"), "Unknown API URL", "Set api_url or SUPADUPA_API_URL before planning.")
	}
	if config.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Unknown Token", "Set token or SUPADUPA_TOKEN before planning.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	apiURL := os.Getenv("SUPADUPA_API_URL")
	if !config.APIURL.IsNull() {
		apiURL = config.APIURL.ValueString()
	}
	token := os.Getenv("SUPADUPA_TOKEN")
	if !config.Token.IsNull() {
		token = config.Token.ValueString()
	}
	client, err := NewClient(apiURL, token, nil)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Supadupa provider configuration", fmt.Sprintf("Unable to create Management API client: %s", err))
		return
	}
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *supadupaProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewPlatformDefaultsResource,
		NewPlatformSSOResource,
		NewHostResource,
		NewOrgResource,
		NewOrgQuotaResource,
		NewOrgMemberResource,
		NewOrgTeamResource,
		NewOrgTeamMemberResource,
		NewProjectResource,
		NewProjectAccessGrantResource,
		NewProjectBranchResource,
		NewProjectReplicaResource,
		NewProjectBackupPolicyResource,
		NewProjectPITRPolicyResource,
		NewProjectConfigResource,
		NewProjectAuthClientResource,
		NewProjectAuthHookResource,
		NewProjectDatabaseCronJobResource,
		NewProjectDatabaseQueueResource,
		NewProjectDatabaseWebhookResource,
		NewProjectDatabaseSchemaResource,
		NewProjectDatabaseRoleResource,
		NewProjectDatabaseExtensionResource,
		NewProjectEmbeddingJobResource,
		NewProjectFunctionResource,
		NewProjectFunctionRegionResource,
		NewProjectFunctionStorageMountResource,
		NewProjectDomainResource,
		NewProjectLogDrainResource,
		NewProjectNetworkConnectionResource,
		NewProjectStorageBucketResource,
		NewProjectVectorBucketResource,
		NewProjectAnalyticsBucketResource,
		NewProjectReplicationPipelineResource,
		NewProjectCDNPolicyResource,
	}
}

func (p *supadupaProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewOrgDataSource,
	}
}
