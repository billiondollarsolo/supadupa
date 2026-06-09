package terraform

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectReplicaResource struct {
	client *Client
}

type projectReplicaResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	Ref                   types.String `tfsdk:"ref"`
	Name                  types.String `tfsdk:"name"`
	HostID                types.String `tfsdk:"host_id"`
	Region                types.String `tfsdk:"region"`
	Tier                  types.String `tfsdk:"tier"`
	Status                types.String `tfsdk:"status"`
	Role                  types.String `tfsdk:"role"`
	Message               types.String `tfsdk:"message"`
	ReadURI               types.String `tfsdk:"read_uri"`
	ReadWeight            types.Int64  `tfsdk:"read_weight"`
	FailoverPriority      types.Int64  `tfsdk:"failover_priority"`
	ReplicationLagBytes   types.Int64  `tfsdk:"replication_lag_bytes"`
	ReplicationLagSeconds types.Int64  `tfsdk:"replication_lag_seconds"`
	PromotedAt            types.String `tfsdk:"promoted_at"`
	CreatedAt             types.String `tfsdk:"created_at"`
	UpdatedAt             types.String `tfsdk:"updated_at"`
}

func NewProjectReplicaResource() resource.Resource {
	return &projectReplicaResource{}
}

func (r *projectReplicaResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_replica"
}

func (r *projectReplicaResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replaceInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	resp.Schema = resourceschema.Schema{
		Description: "Supadupa project read replica declaration managed through the Management API.",
		Attributes: map[string]resourceschema.Attribute{
			"id": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Generated replica ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ref": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Project ref.",
				PlanModifiers: replaceString,
			},
			"name": resourceschema.StringAttribute{
				Required:      true,
				Description:   "Replica name. Must be unique within the project.",
				PlanModifiers: replaceString,
			},
			"host_id": resourceschema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString(""),
				Description:   "Optional host placement ID.",
				PlanModifiers: replaceString,
			},
			"region": resourceschema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString(""),
				Description:   "Replica region label.",
				PlanModifiers: replaceString,
			},
			"tier": resourceschema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString(""),
				Description:   "Replica resource tier. Empty inherits the source project tier.",
				PlanModifiers: replaceString,
			},
			"status": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Replica status.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Replica role, usually read or primary after promotion.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"message": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Human-readable replica status message.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"read_uri": resourceschema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Replica read connection URI.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"read_weight": resourceschema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Default:       int64default.StaticInt64(100),
				Description:   "Weighted read routing share.",
				PlanModifiers: replaceInt,
			},
			"failover_priority": resourceschema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Description:   "Failover priority. Lower values are preferred; zero lets the control plane assign one.",
				PlanModifiers: replaceInt,
			},
			"replication_lag_bytes": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Current replication lag in bytes.",
			},
			"replication_lag_seconds": resourceschema.Int64Attribute{
				Computed:    true,
				Description: "Current replication lag in seconds.",
			},
			"promoted_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Timestamp if this replica was promoted.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Replica creation timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": resourceschema.StringAttribute{
				Computed:    true,
				Description: "Replica update timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *projectReplicaResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, resp.Diagnostics.AddError)
	if !ok {
		return
	}
	r.client = client
}

func (r *projectReplicaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectReplicaResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	replica, err := r.client.CreateProjectReplica(ctx, plan.Ref.ValueString(), projectReplicaInputFromModel(plan))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Supadupa project replica", err.Error())
		return
	}
	setProjectReplicaState(&plan, replica)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectReplicaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectReplicaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	replica, err := r.findReplica(ctx, state.Ref.ValueString(), state.ID.ValueString(), state.Name.ValueString())
	if errors.Is(err, ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Supadupa project replica", err.Error())
		return
	}
	setProjectReplicaState(&state, replica)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *projectReplicaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Supadupa project replica updates require replacement", "Replica placement, tier, and routing fields are replace-on-change.")
}

func (r *projectReplicaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectReplicaResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteProjectReplica(ctx, state.Ref.ValueString(), state.ID.ValueString())
	if err != nil && !errors.Is(err, ErrNotFound) {
		resp.Diagnostics.AddError("Unable to delete Supadupa project replica", err.Error())
	}
}

func (r *projectReplicaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setTwoPartImportState(ctx, req.ID, resp, "ref", "id", "Use ref/id, for example alpha/replica_123.")
}

func (r *projectReplicaResource) findReplica(ctx context.Context, ref string, id string, name string) (ProjectReplica, error) {
	replicas, err := r.client.ListProjectReplicas(ctx, ref)
	if err != nil {
		return ProjectReplica{}, err
	}
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	return findInList(replicas, func(replica ProjectReplica) bool {
		return replica.ID == id || (normalizedName != "" && replica.Name == normalizedName)
	})
}

func projectReplicaInputFromModel(model projectReplicaResourceModel) ProjectReplicaInput {
	return ProjectReplicaInput{
		Name:             model.Name.ValueString(),
		HostID:           model.HostID.ValueString(),
		Region:           model.Region.ValueString(),
		Tier:             model.Tier.ValueString(),
		ReadWeight:       int(model.ReadWeight.ValueInt64()),
		FailoverPriority: int(model.FailoverPriority.ValueInt64()),
	}
}

func setProjectReplicaState(model *projectReplicaResourceModel, replica ProjectReplica) {
	model.ID = types.StringValue(replica.ID)
	model.Ref = types.StringValue(replica.ProjectRef)
	model.Name = types.StringValue(replica.Name)
	model.HostID = types.StringValue(replica.HostID)
	model.Region = types.StringValue(replica.Region)
	model.Tier = types.StringValue(replica.Tier)
	model.Status = types.StringValue(replica.Status)
	model.Role = types.StringValue(replica.Role)
	model.Message = optionalStringValue(replica.Message)
	model.ReadURI = types.StringValue(replica.ReadURI)
	model.ReadWeight = types.Int64Value(int64(replica.ReadWeight))
	model.FailoverPriority = types.Int64Value(int64(replica.FailoverPriority))
	model.ReplicationLagBytes = types.Int64Value(replica.ReplicationLagBytes)
	model.ReplicationLagSeconds = types.Int64Value(int64(replica.ReplicationLagSeconds))
	model.PromotedAt = optionalTimePointerString(replica.PromotedAt)
	model.CreatedAt = optionalTimeString(replica.CreatedAt)
	model.UpdatedAt = optionalTimeString(replica.UpdatedAt)
}
