export type Org = {
  id: string;
  name: string;
  feature_flag_overrides: Record<string, boolean>;
  feature_flags: Record<string, boolean>;
  created_at: string;
};

export type OrgFeatureFlags = {
  org_id: string;
  defaults: Record<string, boolean>;
  overrides: Record<string, boolean>;
  effective: Record<string, boolean>;
};

export type User = {
  id: string;
  email: string;
  role: string;
  mfa_enabled: boolean;
  created_at: string;
};

export type AuthResponse = {
  token?: string;
  mfa_required?: boolean;
  user: User;
};

export type AuthState = {
  bootstrapped: boolean;
  auth_required: boolean;
  sso_enabled?: boolean;
  sso_provider?: string;
};

export type PlatformSSOInitiation = {
  enabled: boolean;
  provider: string;
  idp_entity_id?: string;
  login_url?: string;
  acs_url?: string;
  metadata_url?: string;
  requested_at: string;
};

export type MFAStatus = {
  user_id: string;
  email: string;
  enabled: boolean;
  pending: boolean;
  confirmed_at?: string;
  updated_at: string;
};

export type MFAEnrollment = MFAStatus & {
  secret: string;
  otpauth_url: string;
};

export type Membership = {
  user_id: string;
  org_id: string;
  email: string;
  role: "owner" | "admin" | "developer" | "viewer" | string;
  created_at: string;
};

export type Team = {
  id: string;
  org_id: string;
  name: string;
  slug: string;
  created_at: string;
};

export type TeamMember = {
  team_id: string;
  org_id: string;
  team_slug: string;
  user_id: string;
  email: string;
  created_at: string;
};

export type ProjectAccessGrant = {
  id: string;
  project_ref: string;
  org_id: string;
  subject_type: "user" | "team" | string;
  subject_id: string;
  subject_name: string;
  role: "owner" | "admin" | "developer" | "viewer" | string;
  created_at: string;
};

export type OrgAccessReview = {
  org_id: string;
  members: Membership[];
  teams: Array<{
    team: Team;
    members: TeamMember[];
  }>;
  projects: Array<{
    project_ref: string;
    project_name: string;
    grants: ProjectAccessGrant[];
    effective: Array<{
      user_id: string;
      email: string;
      role: string;
      sources: string[];
    }>;
  }>;
  generated_at: string;
};

export type HostCapacity = {
  cpu: number;
  ram_mb: number;
  disk_gb: number;
  disk_iops: number;
  projects: number;
};

export type OrgQuota = {
  org_id: string;
  max_projects: number;
  max_cpu: number;
  max_ram_mb: number;
  max_disk_gb: number;
  max_disk_iops: number;
  used: HostCapacity;
  updated_at: string;
};

export type OrgUsage = {
  org_id: string;
  resources: HostCapacity;
  projects_by_status: Record<string, number>;
  read_replicas: number;
  backup_count: number;
  backup_storage_bytes: number;
  wal_archives: number;
  wal_archive_bytes: number;
  project_log_events: number;
  custom_domains: number;
  log_drains: number;
  function_deployments: number;
  function_regions: number;
  function_storage_mounts: number;
  replication_pipelines: number;
  embedding_jobs: number;
  database_extensions: number;
  database_cron_jobs: number;
  database_queues: number;
  database_webhooks: number;
  database_schemas: number;
  auth_clients: number;
  auth_hooks: number;
  database_roles: number;
  storage_buckets: number;
  vector_buckets: number;
  analytics_buckets: number;
  cdn_enabled_projects: number;
  cdn_invalidations: number;
  network_connections: number;
  secrets: number;
  db_allocated_bytes: number;
  storage_bytes: number;
  egress_bytes: number;
  function_invocations: number;
  auth_maus: number;
  sampled_at: string;
};

export type UsageSnapshot = {
  id: string;
  org_id: string;
  metrics: OrgUsage;
  sampled_at: string;
};

export type BillingLineItem = {
  key: string;
  description: string;
  quantity: number;
  unit: string;
  unit_price_cents: number;
  amount_cents: number;
};

export type BillingInvoice = {
  id: string;
  org_id: string;
  usage_snapshot_id: string;
  number: string;
  status: string;
  currency: string;
  period_start: string;
  period_end: string;
  due_at: string;
  subtotal_cents: number;
  total_cents: number;
  line_items: BillingLineItem[];
  metrics: OrgUsage;
  created_at: string;
};

export type FleetMetrics = {
  orgs: number;
  users: number;
  hosts: number;
  projects: number;
  read_replicas: number;
  projects_by_status: Record<string, number>;
  host_capacity: HostCapacity;
  host_used: HostCapacity;
  observed: TelemetryRollup;
  routes: number;
  custom_domains: number;
  log_drains: number;
  function_deployments: number;
  function_regions: number;
  function_storage_mounts: number;
  replication_pipelines: number;
  embedding_jobs: number;
  database_extensions: number;
  database_cron_jobs: number;
  database_queues: number;
  database_webhooks: number;
  database_schemas: number;
  auth_clients: number;
  auth_hooks: number;
  database_roles: number;
  storage_buckets: number;
  vector_buckets: number;
  analytics_buckets: number;
  cdn_enabled_projects: number;
  cdn_invalidations: number;
  network_connections: number;
  backups: number;
  backup_storage_bytes: number;
  wal_archives: number;
  wal_archive_bytes: number;
  project_log_events: number;
  audit_events: number;
  audit_verified: boolean;
  sampled_at: string;
};

export type AdvisorFinding = {
  id: string;
  project_ref: string;
  severity: "critical" | "high" | "medium" | "low" | "info" | string;
  category: string;
  title: string;
  message: string;
  recommendation: string;
  status: string;
  created_at: string;
};

export type ComplianceReport = {
  generated_at: string;
  frameworks: string[];
  summary: {
    passed: number;
    action_needed: number;
    manual_review: number;
    total: number;
  };
  controls: ComplianceControl[];
  dpa_posture: string;
  certification: string;
};

export type ComplianceControl = {
  id: string;
  title: string;
  category: string;
  frameworks: string[];
  status: "pass" | "action_needed" | "manual_review" | string;
  evidence: string[];
  recommendation: string;
};

export type ProjectMetrics = {
  project_ref: string;
  org_id: string;
  status: ProjectPhase;
  resource_tier: string;
  resources: HostCapacity;
  observed?: TelemetrySample;
  read_replicas: number;
  routes: number;
  custom_domains: number;
  log_drains: number;
  function_deployments: number;
  function_regions: number;
  function_storage_mounts: number;
  replication_pipelines: number;
  embedding_jobs: number;
  database_extensions: number;
  database_cron_jobs: number;
  database_queues: number;
  database_webhooks: number;
  database_schemas: number;
  auth_clients: number;
  auth_hooks: number;
  database_roles: number;
  storage_buckets: number;
  vector_buckets: number;
  analytics_buckets: number;
  cdn_enabled: boolean;
  cdn_invalidations: number;
  network_connections: number;
  backups: number;
  backup_storage_bytes: number;
  wal_archives: number;
  wal_archive_bytes: number;
  project_log_events: number;
  activity_events: number;
  secrets: number;
  db_allocated_bytes: number;
  storage_bytes: number;
  egress_bytes: number;
  function_invocations: number;
  auth_maus: number;
  sampled_at: string;
};

export type TelemetrySample = {
  project_ref: string;
  source: string;
  cpu_percent: number;
  memory_bytes: number;
  memory_limit_bytes: number;
  disk_used_bytes: number;
  disk_limit_bytes: number;
  network_rx_bytes: number;
  network_tx_bytes: number;
  sampled_at: string;
};

export type TelemetryRollup = {
  projects_sampled: number;
  cpu_percent: number;
  memory_bytes: number;
  memory_limit_bytes: number;
  disk_used_bytes: number;
  disk_limit_bytes: number;
  network_rx_bytes: number;
  network_tx_bytes: number;
  latest_sampled_at?: string;
  oldest_sampled_at?: string;
  stale_projects: number;
  stale_after_seconds: number;
};

export type PlatformDefaults = {
  domain: string;
  stack_version: string;
  profile: "essential" | "full" | "orioledb" | string;
  resource_tier: "small" | "medium" | "large" | string;
  backup_schedule: "daily" | "hourly" | string;
  feature_flags: Record<string, boolean>;
  smtp: {
    enabled: boolean;
    host: string;
    port: number;
    sender_name: string;
    sender_email: string;
    username: string;
    password_handle: string;
    tls_mode: "starttls" | "implicit" | "none" | string;
  };
  updated_at: string;
};

export type ProvisionerStatus = {
  provisioner: "compose" | "kubernetes" | "unconfigured" | string;
};

export type PlatformSSOConfig = {
  enabled: boolean;
  provider: "saml" | string;
  idp_entity_id: string;
  sso_url: string;
  certificate_pem: string;
  acs_url: string;
  metadata_url: string;
  email_domain: string;
  auto_provision: boolean;
  default_role: "admin" | "developer" | "viewer" | string;
  updated_at: string;
};

export type ProjectPhase =
  | "provisioning"
  | "healthy"
  | "degraded"
  | "paused"
  | "error"
  | "destroying";

export type ProjectRuntimeStatus = {
  ref: string;
  phase: ProjectPhase | string;
  message: string;
  endpoints?: Record<string, string>;
};

export type Project = {
  id: string;
  ref: string;
  org_id: string;
  name: string;
  status: ProjectPhase;
  message?: string;
  spec: {
    domain: string;
    host_id?: string;
    stack_version: string;
    profile: string;
    resource_tier: string;
  };
  runtime_status?: ProjectRuntimeStatus;
  created_at: string;
  updated_at: string;
};

export type ProjectServices = {
  project_ref: string;
  services: Record<string, boolean>;
  updated_at: string;
};

export type SCIMServiceProviderConfig = {
  schemas: string[];
  patch: { supported: boolean };
  bulk: { supported: boolean };
  filter: { supported: boolean };
  changePassword: { supported: boolean };
  sort: { supported: boolean };
  etag: { supported: boolean };
  authenticationSchemes: Array<{ type: string; name: string }>;
};

export type SCIMMeta = {
  resourceType: string;
  created?: string;
  location?: string;
};

export type SCIMUser = {
  schemas: string[];
  id: string;
  externalId?: string;
  userName: string;
  displayName?: string;
  active: boolean;
  emails?: Array<{ value: string; primary?: boolean }>;
  meta: SCIMMeta;
  "urn:supadupa:params:scim:schemas:extension:User"?: {
    role?: string;
  };
};

export type SCIMGroup = {
  schemas: string[];
  id: string;
  externalId?: string;
  displayName: string;
  members?: Array<{ value: string; display?: string }>;
  meta: SCIMMeta;
  "urn:supadupa:params:scim:schemas:extension:Group"?: {
    org_id?: string;
    slug?: string;
  };
};

export type SCIMListResponse<T> = {
  schemas: string[];
  totalResults: number;
  startIndex: number;
  itemsPerPage: number;
  Resources: T[];
};

export type Host = {
  id: string;
  name: string;
  address: string;
  capacity: HostCapacity;
  used: HostCapacity;
  created_at: string;
};

export type ConnectPayload = {
  api_url: string;
  local_api_url?: string;
  studio_url: string;
  local_studio_url?: string;
  rest_url: string;
  auth_url: string;
  graphql_url: string;
  realtime_url: string;
  functions_url: string;
  storage_url: string;
  storage_s3_url: string;
  api_keys: Record<string, string>;
  jwt: Record<string, string>;
  postgres: Record<string, string>;
  postgres_parts: Record<string, Record<string, string>>;
  pooler: Record<string, string>;
  storage: Record<string, string>;
  links: Record<string, string>;
  connection_snippets: Record<string, string>;
  sdk_snippets: Record<string, string>;
  secret_handles: Record<string, string>;
  jwt_signing_keys: Array<{
    kind: string;
    kid: string;
    alg: string;
    status: string;
    public_key: string;
    handle: string;
    created_at: string;
    rotated_at?: string;
  }>;
};

export type ProjectCLIProfile = {
  project_ref: string;
  project_name: string;
  api_url: string;
  local_api_url?: string;
  studio_url: string;
  local_studio_url?: string;
  rest_url: string;
  auth_url: string;
  graphql_url: string;
  realtime_url: string;
  functions_url: string;
  storage_url: string;
  storage_s3_url: string;
  database_url: string;
  pooler_transaction_url: string;
  pooler_session_url: string;
  env: Record<string, string>;
  supabase_config_toml: string;
  commands: Record<string, string>;
  secret_handles: Record<string, string>;
  compatibility_contracts: Record<string, string>;
};

export type AuditEvent = {
  id: string;
  actor_id?: string;
  chain_index: number;
  previous_hash: string;
  hash: string;
  action: string;
  target: string;
  metadata: Record<string, string>;
  created_at: string;
};

export type AuditIntegrity = {
  verified: boolean;
  events: number;
  head_hash: string;
  broken_at?: number;
  checked_at: string;
};

export type Backup = {
  id: string;
  project_ref: string;
  kind: string;
  location: string;
  size_bytes: number;
  checksum_sha256: string;
  status: string;
  created_at: string;
  verified_at?: string;
};

export type BackupPolicy = {
  project_ref: string;
  enabled: boolean;
  schedule: "daily" | "hourly" | string;
  kind: string;
  last_run_at?: string;
  next_run_at?: string;
  updated_at: string;
};

export type PITRPolicy = {
  project_ref: string;
  enabled: boolean;
  archive_bucket: string;
  retention_days: number;
  last_archive_at?: string;
  updated_at: string;
};

export type WALArchive = {
  id: string;
  project_ref: string;
  segment: string;
  location: string;
  size_bytes: number;
  checksum_sha256: string;
  status: string;
  created_at: string;
  verified_at?: string;
};

export type ProjectLog = {
  id: string;
  project_ref: string;
  level: "info" | "warning" | "error" | string;
  message: string;
  metadata: Record<string, string>;
  created_at: string;
};

export type ProjectRoute = {
  id: string;
  project_ref: string;
  name: string;
  fqdn: string;
  path_prefix?: string;
  strip_prefix?: string;
  upstream_url: string;
  tls: boolean;
  ssl_enforced: boolean;
  ip_allowlist?: string[];
  cache_control?: string;
  smart_cdn?: boolean;
  created_at: string;
};

export type ProjectDomain = {
  project_ref: string;
  fqdn: string;
  cert_status: "pending" | "issued" | "failed" | string;
  created_at: string;
  updated_at: string;
};

export type ProjectConfig = {
  project_ref: string;
  area: string;
  config: Record<string, string>;
  updated_at: string;
};

export type ProjectBranch = {
  id: string;
  source_project_ref: string;
  project_ref: string;
  name: string;
  status: string;
  created_at: string;
  expires_at?: string;
};

export type CreateBranchResponse = {
  branch: ProjectBranch;
  project: Project;
};

export type ProjectReplica = {
  id: string;
  project_ref: string;
  name: string;
  host_id?: string;
  region?: string;
  tier: "small" | "medium" | "large" | string;
  status: string;
  role: "read" | "primary" | string;
  message?: string;
  read_uri: string;
  read_weight: number;
  failover_priority: number;
  replication_lag_bytes: number;
  replication_lag_seconds: number;
  promoted_at?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectReplicaRouteTarget = {
  replica_id: string;
  name: string;
  uri: string;
  region?: string;
  weight: number;
  failover_priority: number;
  replication_lag_bytes: number;
  replication_lag_seconds: number;
  role: string;
  status: string;
};

export type ProjectReplicaRouting = {
  project_ref: string;
  primary_uri: string;
  read_strategy: string;
  auto_failover: boolean;
  primary_replica_id?: string;
  failover_candidate?: ProjectReplicaRouteTarget;
  healthy_read_targets: ProjectReplicaRouteTarget[];
  all_targets: ProjectReplicaRouteTarget[];
};

export type ProjectFunction = {
  id: string;
  project_ref: string;
  name: string;
  version: number;
  entrypoint: string;
  verify_jwt: boolean;
  status: string;
  source_hash: string;
  source_bytes: number;
  secrets: Record<string, string>;
  created_at: string;
  updated_at: string;
};

export type ProjectFunctionRegion = {
  id: string;
  project_ref: string;
  function_name: string;
  host_id?: string;
  region: string;
  routing_policy: string;
  invocation_url: string;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectFunctionStorageMount = {
  id: string;
  project_ref: string;
  function_name: string;
  bucket_name: string;
  mount_path: string;
  read_only: boolean;
  prefix?: string;
  env_alias?: string;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectAuthClient = {
  id: string;
  project_ref: string;
  name: string;
  client_id: string;
  client_secret_handle?: string;
  redirect_uris: string[];
  grant_types: string[];
  scopes: string[];
  confidential: boolean;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectAuthHook = {
  id: string;
  project_ref: string;
  hook_type: string;
  enabled: boolean;
  target_uri?: string;
  edge_function?: string;
  secret_handle?: string;
  headers: Record<string, string>;
  timeout_ms: number;
  retry_attempts: number;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectReplicationPipeline = {
  id: string;
  project_ref: string;
  name: string;
  type: "logical" | "etl" | "analytics_bucket" | string;
  source_schema: string;
  source_table: string;
  destination: "postgres" | "webhook" | "s3" | "iceberg" | "bigquery" | "snowflake" | "redshift" | string;
  destination_uri: string;
  credential_handle?: string;
  config: Record<string, string>;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectEmbeddingJob = {
  id: string;
  project_ref: string;
  name: string;
  source_schema: string;
  source_table: string;
  source_column: string;
  primary_key_column: string;
  destination_table: string;
  destination_column: string;
  provider: "openai" | "huggingface" | "local" | string;
  model: string;
  dimension: number;
  schedule: string;
  batch_size: number;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectDatabaseExtension = {
  id: string;
  project_ref: string;
  name: string;
  schema: string;
  version?: string;
  enabled: boolean;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectDatabaseCronJob = {
  id: string;
  project_ref: string;
  name: string;
  schedule: string;
  command: string;
  database: string;
  username: string;
  active: boolean;
  timeout_seconds: number;
  max_runtime_seconds: number;
  metadata: Record<string, string>;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectDatabaseQueue = {
  id: string;
  project_ref: string;
  name: string;
  schema: string;
  retention_minutes: number;
  visibility_timeout_seconds: number;
  max_retries: number;
  dead_letter_queue?: string;
  active: boolean;
  metadata: Record<string, string>;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectDatabaseWebhook = {
  id: string;
  project_ref: string;
  name: string;
  schema: string;
  table: string;
  events: string[];
  endpoint: string;
  http_method: string;
  headers: Record<string, string>;
  timeout_seconds: number;
  retry_count: number;
  active: boolean;
  metadata: Record<string, string>;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectDatabaseSchema = {
  id: string;
  project_ref: string;
  name: string;
  version: string;
  schema: string;
  sql: string;
  checksum: string;
  apply_order: number;
  active: boolean;
  metadata: Record<string, string>;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectDatabaseRole = {
  id: string;
  project_ref: string;
  name: string;
  login: boolean;
  inherit: boolean;
  bypass_rls: boolean;
  connection_limit: number;
  password_secret_handle?: string;
  member_of: string[];
  schema_grants: Record<string, string>;
  metadata: Record<string, string>;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectVectorBucket = {
  id: string;
  project_ref: string;
  name: string;
  dimension: number;
  distance: "cosine" | "l2" | "ip" | string;
  index_method: "none" | "hnsw" | "ivfflat" | string;
  storage_backend: "postgres" | "s3" | string;
  storage_uri?: string;
  metadata: Record<string, string>;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectAnalyticsBucket = {
  id: string;
  project_ref: string;
  name: string;
  storage_uri: string;
  catalog_uri?: string;
  warehouse: string;
  credential_handle?: string;
  format_version: number;
  partitioning?: string;
  retention_days: number;
  compaction_schedule: string;
  metadata: Record<string, string>;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectStorageBucket = {
  id: string;
  project_ref: string;
  name: string;
  public: boolean;
  file_size_limit: number;
  allowed_mime_types: string[];
  cache_control: string;
  avif_autodetection: boolean;
  metadata: Record<string, string>;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectCDNPolicy = {
  project_ref: string;
  enabled: boolean;
  browser_ttl_seconds: number;
  edge_ttl_seconds: number;
  stale_while_revalidate_seconds: number;
  included_paths: string[];
  excluded_paths: string[];
  smart_revalidation: boolean;
  cache_control: string;
  updated_at: string;
};

export type CDNInvalidation = {
  id: string;
  project_ref: string;
  paths: string[];
  source: string;
  event_id?: string;
  status: string;
  message?: string;
  created_at: string;
  completed_at?: string;
};

export type ProjectNetworkConnection = {
  id: string;
  project_ref: string;
  name: string;
  type: "privatelink" | "vpc_peering" | "private_endpoint" | "wireguard" | "operator_network" | string;
  provider: "aws" | "gcp" | "azure" | "custom" | "operator" | string;
  region?: string;
  cidrs: string[];
  endpoint_id?: string;
  config: Record<string, string>;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
};

export type ProjectNetworkPolicy = {
  project_ref: string;
  config: ProjectConfig;
  connections: ProjectNetworkConnection[];
  allowlist: string;
  ssl_enforced: string;
};

export type LogDrain = {
  id: string;
  project_ref: string;
  target: "https" | "loki" | "datadog" | "sentry" | "axiom" | "s3" | string;
  config: Record<string, string>;
  created_at: string;
};

export type ProjectSecret = {
  id: string;
  project_ref: string;
  kind: string;
  masked: string;
  created_at: string;
  rotated_at?: string;
};

export type ProjectSecretReveal = {
  kind: string;
  value: string;
  created_at: string;
  rotated_at?: string;
};
