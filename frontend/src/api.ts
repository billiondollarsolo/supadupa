import type { AdvisorFinding, AuditEvent, AuditIntegrity, AuthResponse, AuthState, Backup, BackupPolicy, BillingInvoice, CDNInvalidation, ComplianceReport, ConnectPayload, CreateBranchResponse, FleetMetrics, Host, HostCapacity, LogDrain, MFAEnrollment, MFAStatus, Membership, Org, OrgAccessReview, OrgFeatureFlags, OrgQuota, OrgUsage, PITRPolicy, PlatformDefaults, PlatformSSOConfig, PlatformSSOInitiation, Project, ProjectAccessGrant, ProjectAnalyticsBucket, ProjectAuthClient, ProjectAuthHook, ProjectBranch, ProjectCDNPolicy, ProjectCLIProfile, ProjectConfig, ProjectDatabaseCronJob, ProjectDatabaseExtension, ProjectDatabaseQueue, ProjectDatabaseRole, ProjectDatabaseSchema, ProjectDatabaseWebhook, ProjectDomain, ProjectEmbeddingJob, ProjectFunction, ProjectFunctionRegion, ProjectFunctionStorageMount, ProjectLog, ProjectMetrics, ProjectNetworkConnection, ProjectNetworkPolicy, ProjectReplica, ProjectReplicaRouting, ProjectReplicationPipeline, ProjectRoute, ProjectSecret, ProjectSecretReveal, ProjectServices, ProjectStorageBucket, ProjectVectorBucket, ProvisionerStatus, SCIMGroup, SCIMListResponse, SCIMServiceProviderConfig, SCIMUser, Team, TeamMember, UsageSnapshot, User, WALArchive } from "./types";

const apiBase = resolveApiBase();
const tokenStorageKey = "supadupa_token";

export function getApiBase() {
  return apiBase;
}

function resolveApiBase() {
  const configured = import.meta.env.VITE_API_BASE_URL?.trim();
  const runtimeOrigin = typeof window === "undefined" ? "" : window.location.origin;
  const runtimeURL = runtimeOrigin ? new URL(runtimeOrigin) : null;
  const fallbackHost = runtimeURL?.hostname || "localhost";
  const fallbackProtocol = runtimeURL?.protocol || "http:";

  if (!configured) {
    return `${fallbackProtocol}//${fallbackHost}:8080`;
  }

  if (runtimeURL && isLoopbackHost(runtimeURL.hostname)) {
    try {
      const configuredURL = new URL(configured);
      if (isLoopbackHost(configuredURL.hostname) && configuredURL.hostname !== runtimeURL.hostname) {
        configuredURL.hostname = runtimeURL.hostname;
        return configuredURL.toString().replace(/\/$/, "");
      }
    } catch {
      return configured;
    }
  }

  return configured;
}

function isLoopbackHost(hostname: string) {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "::1" || hostname === "[::1]";
}

export function getToken() {
  return window.localStorage.getItem(tokenStorageKey);
}

export function setToken(token: string) {
  window.localStorage.setItem(tokenStorageKey, token);
}

export function clearToken() {
  window.localStorage.removeItem(tokenStorageKey);
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getToken();
  const headers = new Headers(init?.headers);
  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  const response = await fetch(`${apiBase}${path}`, {
    ...init,
    headers,
  });
  if (!response.ok) {
    const detail = await response.text();
    throw new Error(detail || response.statusText);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return response.json() as Promise<T>;
}

export function getApiHealth() {
  return request<{ status: string }>("/v1/health");
}

export function getAuthState() {
  return request<AuthState>("/v1/auth/state");
}

export function getProvisionerStatus() {
  return request<ProvisionerStatus>("/v1/provisioner");
}

export function listOrgs() {
  return request<Org[]>("/v1/orgs");
}

export function createOrg(name: string) {
  return request<Org>("/v1/orgs", {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export function listOrgMembers(orgId: string) {
  return request<Membership[]>(`/v1/orgs/${orgId}/members`);
}

export function upsertOrgMember(orgId: string, input: { email: string; role: string }) {
  return request<Membership>(`/v1/orgs/${orgId}/members`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteOrgMember(orgId: string, email: string) {
  return request<void>(`/v1/orgs/${orgId}/members/${encodeURIComponent(email)}`, {
    method: "DELETE",
  });
}

export function listOrgTeams(orgId: string) {
  return request<Team[]>(`/v1/orgs/${orgId}/teams`);
}

export function createOrgTeam(orgId: string, input: { name: string; slug: string }) {
  return request<Team>(`/v1/orgs/${orgId}/teams`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteOrgTeam(orgId: string, slug: string) {
  return request<void>(`/v1/orgs/${orgId}/teams/${encodeURIComponent(slug)}`, {
    method: "DELETE",
  });
}

export function listTeamMembers(orgId: string, slug: string) {
  return request<TeamMember[]>(`/v1/orgs/${orgId}/teams/${encodeURIComponent(slug)}/members`);
}

export function upsertTeamMember(orgId: string, slug: string, email: string) {
  return request<TeamMember>(`/v1/orgs/${orgId}/teams/${encodeURIComponent(slug)}/members`, {
    method: "POST",
    body: JSON.stringify({ email }),
  });
}

export function deleteTeamMember(orgId: string, slug: string, email: string) {
  return request<void>(`/v1/orgs/${orgId}/teams/${encodeURIComponent(slug)}/members/${encodeURIComponent(email)}`, {
    method: "DELETE",
  });
}

export function getOrgQuota(orgId: string) {
  return request<OrgQuota>(`/v1/orgs/${orgId}/quotas`);
}

export function getOrgFeatureFlags(orgId: string) {
  return request<OrgFeatureFlags>(`/v1/orgs/${orgId}/features`);
}

export function updateOrgFeatureFlags(orgId: string, overrides: Record<string, boolean>) {
  return request<OrgFeatureFlags>(`/v1/orgs/${orgId}/features`, {
    method: "PUT",
    body: JSON.stringify({ overrides }),
  });
}

export function updateOrgQuota(orgId: string, input: { max_projects: number; max_cpu: number; max_ram_mb: number; max_disk_gb: number; max_disk_iops: number }) {
  return request<OrgQuota>(`/v1/orgs/${orgId}/quotas`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function getOrgUsage(orgId: string) {
  return request<OrgUsage>(`/v1/orgs/${orgId}/usage`);
}

export function listOrgUsageSnapshots(orgId: string, limit = 10) {
  return request<UsageSnapshot[]>(`/v1/orgs/${orgId}/usage/snapshots?limit=${limit}`);
}

export function createOrgUsageSnapshot(orgId: string) {
  return request<UsageSnapshot>(`/v1/orgs/${orgId}/usage/snapshots`, {
    method: "POST",
  });
}

export function listBillingInvoices(orgId: string, limit = 10) {
  return request<BillingInvoice[]>(`/v1/orgs/${orgId}/billing/invoices?limit=${limit}`);
}

export function createBillingInvoice(orgId: string, input: { usage_snapshot_id?: string; currency?: string; status?: string; due_days?: number }) {
  return request<BillingInvoice>(`/v1/orgs/${orgId}/billing/invoices`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function getBillingInvoice(orgId: string, invoiceId: string) {
  return request<BillingInvoice>(`/v1/orgs/${orgId}/billing/invoices/${encodeURIComponent(invoiceId)}`);
}

export function getOrgAccessReview(orgId: string) {
  return request<OrgAccessReview>(`/v1/orgs/${orgId}/access-review`);
}

export function getFleetMetrics() {
  return request<FleetMetrics>("/v1/metrics");
}

export function getAdvisorFindings() {
  return request<AdvisorFinding[]>("/v1/advisor");
}

export function getComplianceReport() {
  return request<ComplianceReport>("/v1/compliance/report");
}

export function getProjectMetrics(ref: string) {
  return request<ProjectMetrics>(`/v1/projects/${ref}/metrics`);
}

export function getPlatformDefaults() {
  return request<PlatformDefaults>("/v1/settings/defaults");
}

export function updatePlatformDefaults(input: Omit<PlatformDefaults, "updated_at">) {
  return request<PlatformDefaults>("/v1/settings/defaults", {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function getPlatformSSOConfig() {
  return request<PlatformSSOConfig>("/v1/settings/sso");
}

export function updatePlatformSSOConfig(input: Omit<PlatformSSOConfig, "provider" | "updated_at">) {
  return request<PlatformSSOConfig>("/v1/settings/sso", {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function getSCIMServiceProviderConfig() {
  return request<SCIMServiceProviderConfig>("/v1/scim/v2/ServiceProviderConfig");
}

export function listSCIMUsers() {
  return request<SCIMListResponse<SCIMUser>>("/v1/scim/v2/Users");
}

export function listSCIMGroups(orgId?: string) {
  const suffix = orgId ? `?org_id=${encodeURIComponent(orgId)}` : "";
  return request<SCIMListResponse<SCIMGroup>>(`/v1/scim/v2/Groups${suffix}`);
}

export function bootstrapAdmin(input: { email: string; password: string }) {
  return request<AuthResponse>("/v1/auth/bootstrap", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function login(input: { email: string; password: string; totp_code?: string }) {
  return request<AuthResponse>("/v1/auth/login", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function startPlatformSSO() {
  return request<PlatformSSOInitiation>("/v1/auth/sso/saml/start");
}

export function getAccountMFA() {
  return request<MFAStatus>("/v1/account/mfa");
}

export function enrollAccountMFA() {
  return request<MFAEnrollment>("/v1/account/mfa/enroll", {
    method: "POST",
  });
}

export function verifyAccountMFA(code: string) {
  return request<MFAStatus>("/v1/account/mfa/verify", {
    method: "POST",
    body: JSON.stringify({ code }),
  });
}

export function disableAccountMFA(code: string) {
  return request<MFAStatus>("/v1/account/mfa", {
    method: "DELETE",
    body: JSON.stringify({ code }),
  });
}

export function createPlatformUser(input: { email: string; password: string; role: string }) {
  return request<User>("/v1/users", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function listUsers() {
  return request<User[]>("/v1/users");
}

export function listHosts() {
  return request<Host[]>("/v1/hosts");
}

export function createHost(input: { name: string; address: string; capacity: Partial<HostCapacity> }) {
  return request<Host>("/v1/hosts", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteHost(id: string) {
  return request<void>(`/v1/hosts/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function listProjects() {
  return request<Project[]>("/v1/projects");
}

export function getProject(ref: string) {
  return request<Project>(`/v1/projects/${ref}`);
}

export function listOrgProjects(orgId: string) {
  return request<Project[]>(`/v1/orgs/${orgId}/projects`);
}

export type CreateProjectInput = {
  orgId: string;
  ref: string;
  name: string;
  host_id: string;
  domain: string;
  profile: "essential" | "full" | "orioledb";
  resource_tier: "small" | "medium" | "large";
};

export function createProject(input: CreateProjectInput) {
  const { orgId, ...payload } = input;
  return request<Project>(`/v1/orgs/${orgId}/projects`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getConnect(ref: string) {
  return request<ConnectPayload>(`/v1/projects/${ref}/connect`);
}

export function getProjectCLIProfile(ref: string) {
  return request<ProjectCLIProfile>(`/v1/projects/${ref}/connect/cli`);
}

export function listProjectAccess(ref: string) {
  return request<ProjectAccessGrant[]>(`/v1/projects/${ref}/access`);
}

export function upsertProjectAccess(ref: string, input: { subject_type: string; subject_id: string; role: string }) {
  return request<ProjectAccessGrant>(`/v1/projects/${ref}/access`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function deleteProjectAccess(ref: string, subjectType: string, subjectId: string) {
  return request<void>(`/v1/projects/${ref}/access/${encodeURIComponent(subjectType)}/${encodeURIComponent(subjectId)}`, {
    method: "DELETE",
  });
}

export function listProjectRoutes(ref: string) {
  return request<ProjectRoute[]>(`/v1/projects/${ref}/routes`);
}

export function listProjectDomains(ref: string) {
  return request<ProjectDomain[]>(`/v1/projects/${ref}/domains`);
}

export function getProjectServices(ref: string) {
  return request<ProjectServices>(`/v1/projects/${ref}/services`);
}

export function updateProjectServices(ref: string, services: Record<string, boolean>) {
  return request<ProjectServices>(`/v1/projects/${ref}/services`, {
    method: "PUT",
    body: JSON.stringify({ services }),
  });
}

export function addProjectDomain(ref: string, fqdn: string) {
  return request<ProjectDomain>(`/v1/projects/${ref}/domains`, {
    method: "POST",
    body: JSON.stringify({ fqdn }),
  });
}

export function deleteProjectDomain(ref: string, fqdn: string) {
  return request<void>(`/v1/projects/${ref}/domains/${encodeURIComponent(fqdn)}`, {
    method: "DELETE",
  });
}

export function getProjectConfig(ref: string, area: string) {
  return request<ProjectConfig>(`/v1/projects/${ref}/config/${area}`);
}

export function updateProjectConfig(ref: string, area: string, config: Record<string, string>) {
  return request<ProjectConfig>(`/v1/projects/${ref}/config/${area}`, {
    method: "PUT",
    body: JSON.stringify({ config }),
  });
}

export function listProjectBranches(ref: string) {
  return request<ProjectBranch[]>(`/v1/projects/${ref}/branches`);
}

export function createProjectBranch(ref: string, input: { ref: string; name: string; ttl_hours: number }) {
  return request<CreateBranchResponse>(`/v1/projects/${ref}/branches`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectBranch(ref: string, branchRef: string) {
  return request<void>(`/v1/projects/${ref}/branches/${encodeURIComponent(branchRef)}`, {
    method: "DELETE",
  });
}

export function listProjectReplicas(ref: string) {
  return request<ProjectReplica[]>(`/v1/projects/${ref}/replicas`);
}

export function getProjectReplicaRouting(ref: string) {
  return request<ProjectReplicaRouting>(`/v1/projects/${ref}/replicas/routing`);
}

export function createProjectReplica(ref: string, input: { name: string; host_id: string; region: string; tier: "small" | "medium" | "large" | string; read_weight: number; failover_priority: number }) {
  return request<ProjectReplica>(`/v1/projects/${ref}/replicas`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectReplica(ref: string, id: string) {
  return request<void>(`/v1/projects/${ref}/replicas/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function promoteProjectReplica(ref: string, id: string, reason: string) {
  return request<ProjectReplica>(`/v1/projects/${ref}/replicas/${encodeURIComponent(id)}/promote`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });
}

export function failoverProjectReplica(ref: string, reason: string) {
  return request<ProjectReplica>(`/v1/projects/${ref}/replicas/failover`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });
}

export function listProjectFunctions(ref: string) {
  return request<ProjectFunction[]>(`/v1/projects/${ref}/functions`);
}

export function deployProjectFunction(ref: string, input: { name: string; entrypoint: string; verify_jwt: boolean; source: string; secrets: Record<string, string> }) {
  return request<ProjectFunction>(`/v1/projects/${ref}/functions`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectFunction(ref: string, name: string) {
  return request<void>(`/v1/projects/${ref}/functions/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export function listProjectFunctionRegions(ref: string) {
  return request<ProjectFunctionRegion[]>(`/v1/projects/${ref}/functions/regions`);
}

export function createProjectFunctionRegion(ref: string, input: {
  function_name: string;
  host_id: string;
  region: string;
  routing_policy: string;
}) {
  return request<ProjectFunctionRegion>(`/v1/projects/${ref}/functions/regions`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectFunctionRegion(ref: string, id: string) {
  return request<void>(`/v1/projects/${ref}/functions/regions/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function listProjectFunctionStorageMounts(ref: string) {
  return request<ProjectFunctionStorageMount[]>(`/v1/projects/${ref}/functions/storage-mounts`);
}

export function createProjectFunctionStorageMount(ref: string, input: {
  function_name: string;
  bucket_name: string;
  mount_path: string;
  read_only: boolean;
  prefix: string;
  env_alias: string;
}) {
  return request<ProjectFunctionStorageMount>(`/v1/projects/${ref}/functions/storage-mounts`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectFunctionStorageMount(ref: string, id: string) {
  return request<void>(`/v1/projects/${ref}/functions/storage-mounts/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function listProjectAuthClients(ref: string) {
  return request<ProjectAuthClient[]>(`/v1/projects/${ref}/auth/clients`);
}

export function createProjectAuthClient(ref: string, input: {
  name: string;
  client_id: string;
  client_secret_handle: string;
  redirect_uris: string[];
  grant_types: string[];
  scopes: string[];
  confidential: boolean;
}) {
  return request<ProjectAuthClient>(`/v1/projects/${ref}/auth/clients`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectAuthClient(ref: string, clientId: string) {
  return request<void>(`/v1/projects/${ref}/auth/clients/${encodeURIComponent(clientId)}`, {
    method: "DELETE",
  });
}

export function listProjectAuthHooks(ref: string) {
  return request<ProjectAuthHook[]>(`/v1/projects/${ref}/auth/hooks`);
}

export function setProjectAuthHook(ref: string, input: {
  hook_type: string;
  enabled: boolean;
  target_uri: string;
  edge_function: string;
  secret_handle: string;
  headers: Record<string, string>;
  timeout_ms: number;
  retry_attempts: number;
}) {
  return request<ProjectAuthHook>(`/v1/projects/${ref}/auth/hooks`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectAuthHook(ref: string, hookType: string) {
  return request<void>(`/v1/projects/${ref}/auth/hooks/${encodeURIComponent(hookType)}`, {
    method: "DELETE",
  });
}

export function listProjectReplicationPipelines(ref: string) {
  return request<ProjectReplicationPipeline[]>(`/v1/projects/${ref}/replication`);
}

export function createProjectReplicationPipeline(ref: string, input: {
  name: string;
  type: string;
  source_schema: string;
  source_table: string;
  destination: string;
  destination_uri: string;
  credential_handle: string;
  config: Record<string, string>;
}) {
  return request<ProjectReplicationPipeline>(`/v1/projects/${ref}/replication`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectReplicationPipeline(ref: string, id: string) {
  return request<void>(`/v1/projects/${ref}/replication/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function listProjectEmbeddingJobs(ref: string) {
  return request<ProjectEmbeddingJob[]>(`/v1/projects/${ref}/embeddings`);
}

export function createProjectEmbeddingJob(ref: string, input: {
  name: string;
  source_schema: string;
  source_table: string;
  source_column: string;
  primary_key_column: string;
  destination_table: string;
  destination_column: string;
  provider: string;
  model: string;
  dimension: number;
  schedule: string;
  batch_size: number;
}) {
  return request<ProjectEmbeddingJob>(`/v1/projects/${ref}/embeddings`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectEmbeddingJob(ref: string, id: string) {
  return request<void>(`/v1/projects/${ref}/embeddings/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function listProjectDatabaseExtensions(ref: string) {
  return request<ProjectDatabaseExtension[]>(`/v1/projects/${ref}/database/extensions`);
}

export function updateProjectDatabaseExtension(ref: string, name: string, input: {
  schema: string;
  version: string;
  enabled: boolean;
}) {
  return request<ProjectDatabaseExtension>(`/v1/projects/${ref}/database/extensions/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function listProjectDatabaseCronJobs(ref: string) {
  return request<ProjectDatabaseCronJob[]>(`/v1/projects/${ref}/database/cron-jobs`);
}

export function createProjectDatabaseCronJob(ref: string, input: {
  name: string;
  schedule: string;
  command: string;
  database: string;
  username: string;
  active: boolean;
  timeout_seconds: number;
  max_runtime_seconds: number;
  metadata: Record<string, string>;
}) {
  return request<ProjectDatabaseCronJob>(`/v1/projects/${ref}/database/cron-jobs`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectDatabaseCronJob(ref: string, name: string) {
  return request<void>(`/v1/projects/${ref}/database/cron-jobs/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export function listProjectDatabaseQueues(ref: string) {
  return request<ProjectDatabaseQueue[]>(`/v1/projects/${ref}/database/queues`);
}

export function createProjectDatabaseQueue(ref: string, input: {
  name: string;
  schema: string;
  retention_minutes: number;
  visibility_timeout_seconds: number;
  max_retries: number;
  dead_letter_queue: string;
  active: boolean;
  metadata: Record<string, string>;
}) {
  return request<ProjectDatabaseQueue>(`/v1/projects/${ref}/database/queues`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectDatabaseQueue(ref: string, name: string) {
  return request<void>(`/v1/projects/${ref}/database/queues/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export function listProjectDatabaseWebhooks(ref: string) {
  return request<ProjectDatabaseWebhook[]>(`/v1/projects/${ref}/database/webhooks`);
}

export function createProjectDatabaseWebhook(ref: string, input: {
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
}) {
  return request<ProjectDatabaseWebhook>(`/v1/projects/${ref}/database/webhooks`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectDatabaseWebhook(ref: string, name: string) {
  return request<void>(`/v1/projects/${ref}/database/webhooks/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export function listProjectDatabaseSchemas(ref: string) {
  return request<ProjectDatabaseSchema[]>(`/v1/projects/${ref}/database/schemas`);
}

export function createProjectDatabaseSchema(ref: string, input: {
  name: string;
  version: string;
  schema: string;
  sql: string;
  apply_order: number;
  active: boolean;
  metadata: Record<string, string>;
}) {
  return request<ProjectDatabaseSchema>(`/v1/projects/${ref}/database/schemas`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectDatabaseSchema(ref: string, name: string, version: string) {
  return request<void>(`/v1/projects/${ref}/database/schemas/${encodeURIComponent(name)}/${encodeURIComponent(version)}`, {
    method: "DELETE",
  });
}

export function listProjectDatabaseRoles(ref: string) {
  return request<ProjectDatabaseRole[]>(`/v1/projects/${ref}/database/roles`);
}

export function createProjectDatabaseRole(ref: string, input: {
  name: string;
  login: boolean;
  inherit: boolean;
  bypass_rls: boolean;
  connection_limit: number;
  password_secret_handle: string;
  member_of: string[];
  schema_grants: Record<string, string>;
  metadata: Record<string, string>;
}) {
  return request<ProjectDatabaseRole>(`/v1/projects/${ref}/database/roles`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectDatabaseRole(ref: string, name: string) {
  return request<void>(`/v1/projects/${ref}/database/roles/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export function listProjectStorageBuckets(ref: string) {
  return request<ProjectStorageBucket[]>(`/v1/projects/${ref}/storage/buckets`);
}

export function createProjectStorageBucket(ref: string, input: {
  name: string;
  public: boolean;
  file_size_limit: number;
  allowed_mime_types: string[];
  cache_control: string;
  avif_autodetection: boolean;
  metadata: Record<string, string>;
}) {
  return request<ProjectStorageBucket>(`/v1/projects/${ref}/storage/buckets`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectStorageBucket(ref: string, name: string) {
  return request<void>(`/v1/projects/${ref}/storage/buckets/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export function listProjectVectorBuckets(ref: string) {
  return request<ProjectVectorBucket[]>(`/v1/projects/${ref}/vector-buckets`);
}

export function createProjectVectorBucket(ref: string, input: {
  name: string;
  dimension: number;
  distance: string;
  index_method: string;
  storage_backend: string;
  storage_uri: string;
  metadata: Record<string, string>;
}) {
  return request<ProjectVectorBucket>(`/v1/projects/${ref}/vector-buckets`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectVectorBucket(ref: string, name: string) {
  return request<void>(`/v1/projects/${ref}/vector-buckets/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export function listProjectAnalyticsBuckets(ref: string) {
  return request<ProjectAnalyticsBucket[]>(`/v1/projects/${ref}/analytics-buckets`);
}

export function createProjectAnalyticsBucket(ref: string, input: {
  name: string;
  storage_uri: string;
  catalog_uri: string;
  warehouse: string;
  credential_handle: string;
  format_version: number;
  partitioning: string;
  retention_days: number;
  compaction_schedule: string;
  metadata: Record<string, string>;
}) {
  return request<ProjectAnalyticsBucket>(`/v1/projects/${ref}/analytics-buckets`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectAnalyticsBucket(ref: string, name: string) {
  return request<void>(`/v1/projects/${ref}/analytics-buckets/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export function getProjectCDNPolicy(ref: string) {
  return request<ProjectCDNPolicy>(`/v1/projects/${ref}/cdn/policy`);
}

export function updateProjectCDNPolicy(ref: string, input: {
  enabled: boolean;
  browser_ttl_seconds: number;
  edge_ttl_seconds: number;
  stale_while_revalidate_seconds: number;
  included_paths: string[];
  excluded_paths: string[];
  smart_revalidation: boolean;
  cache_control: string;
}) {
  return request<ProjectCDNPolicy>(`/v1/projects/${ref}/cdn/policy`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function listProjectCDNInvalidations(ref: string) {
  return request<CDNInvalidation[]>(`/v1/projects/${ref}/cdn/invalidations`);
}

export function createProjectCDNInvalidation(ref: string, paths: string[]) {
  return request<CDNInvalidation>(`/v1/projects/${ref}/cdn/invalidations`, {
    method: "POST",
    body: JSON.stringify({ paths }),
  });
}

export function createProjectCDNObjectEvent(ref: string, input: { event_id: string; bucket: string; object_path: string; event_type: string }) {
  return request<CDNInvalidation>(`/v1/projects/${ref}/cdn/object-events`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function listProjectNetworkConnections(ref: string) {
  return request<ProjectNetworkConnection[]>(`/v1/projects/${ref}/network-connections`);
}

export function getProjectNetwork(ref: string) {
  return request<ProjectNetworkPolicy>(`/v1/projects/${ref}/network`);
}

export function createProjectNetworkConnection(ref: string, input: {
  name: string;
  type: string;
  provider: string;
  region: string;
  cidrs: string[];
  endpoint_id: string;
  config: Record<string, string>;
}) {
  return request<ProjectNetworkConnection>(`/v1/projects/${ref}/network-connections`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectNetworkConnection(ref: string, id: string) {
  return request<void>(`/v1/projects/${ref}/network-connections/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function listProjectLogDrains(ref: string) {
  return request<LogDrain[]>(`/v1/projects/${ref}/log-drains`);
}

export function createProjectLogDrain(ref: string, input: { target: string; config: Record<string, string> }) {
  return request<LogDrain>(`/v1/projects/${ref}/log-drains`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectLogDrain(ref: string, id: string) {
  return request<void>(`/v1/projects/${ref}/log-drains/${id}`, {
    method: "DELETE",
  });
}

export function listAuditEvents() {
  return request<AuditEvent[]>("/v1/audit-events");
}

export function getAuditIntegrity() {
  return request<AuditIntegrity>("/v1/audit-events/integrity");
}

export function listBackups(ref: string) {
  return request<Backup[]>(`/v1/projects/${ref}/backups`);
}

export function triggerBackup(ref: string) {
  return request<Backup>(`/v1/projects/${ref}/backups`, {
    method: "POST",
  });
}

export function restoreBackup(ref: string, backupId: string) {
  return request<{ backup: Backup; restore_path: string; restore_state: string }>(`/v1/projects/${ref}/restore`, {
    method: "POST",
    body: JSON.stringify({ backup_id: backupId }),
  });
}

export function getBackupPolicy(ref: string) {
  return request<BackupPolicy>(`/v1/projects/${ref}/backups/policy`);
}

export function updateBackupPolicy(ref: string, input: { enabled: boolean; schedule: string; kind: string }) {
  return request<BackupPolicy>(`/v1/projects/${ref}/backups/policy`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function getPITRPolicy(ref: string) {
  return request<PITRPolicy>(`/v1/projects/${ref}/pitr/policy`);
}

export function updatePITRPolicy(ref: string, input: { enabled: boolean; archive_bucket: string; retention_days: number }) {
  return request<PITRPolicy>(`/v1/projects/${ref}/pitr/policy`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function listWALArchives(ref: string) {
  return request<WALArchive[]>(`/v1/projects/${ref}/pitr/wal`);
}

export function archiveWAL(ref: string) {
  return request<WALArchive>(`/v1/projects/${ref}/pitr/wal`, {
    method: "POST",
  });
}

export function listProjectLogs(ref: string) {
  return request<ProjectLog[]>(`/v1/projects/${ref}/logs`);
}

export async function streamProjectLogs(ref: string, onLog: (entry: ProjectLog) => void, signal?: AbortSignal) {
  const token = getToken();
  const response = await fetch(`${apiBase}/v1/projects/${encodeURIComponent(ref)}/logs?stream=true`, {
    headers: {
      Accept: "text/event-stream",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    signal,
  });
  if (!response.ok) {
    const detail = await response.text();
    throw new Error(detail || response.statusText);
  }
  const reader = response.body?.getReader();
  if (!reader) {
    throw new Error("Log stream is not readable");
  }
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      return;
    }
    buffer += decoder.decode(value, { stream: true });
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const eventBlock = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      const parsed = parseServerSentEvent(eventBlock);
      if (parsed.event === "log" && parsed.data) {
        onLog(JSON.parse(parsed.data) as ProjectLog);
      }
      if (parsed.event === "error" && parsed.data) {
        throw new Error(parsed.data.replace(/^"|"$/g, ""));
      }
      boundary = buffer.indexOf("\n\n");
    }
  }
}

function parseServerSentEvent(block: string) {
  let event = "message";
  const data: string[] = [];
  for (const line of block.split(/\r?\n/)) {
    if (line.startsWith(":")) continue;
    if (line.startsWith("event:")) event = line.slice("event:".length).trim();
    if (line.startsWith("data:")) data.push(line.slice("data:".length).trimStart());
  }
  return { event, data: data.join("\n") };
}

export function listProjectActivity(ref: string) {
  return request<AuditEvent[]>(`/v1/projects/${ref}/activity`);
}

export function listProjectSecrets(ref: string) {
  return request<ProjectSecret[]>(`/v1/projects/${ref}/secrets`);
}

export function revealProjectSecret(ref: string, kind: string) {
  return request<ProjectSecretReveal>(`/v1/projects/${ref}/secrets/${encodeURIComponent(kind)}/reveal`);
}

export function auditProjectSecretCopy(ref: string, kind: string) {
  return request<void>(`/v1/projects/${ref}/secrets/${encodeURIComponent(kind)}/copy`, {
    method: "POST",
  });
}

export function rotateProjectSecret(ref: string, kind: string) {
  return request<ProjectSecret>(`/v1/projects/${ref}/keys/rotate`, {
    method: "POST",
    body: JSON.stringify({ kind }),
  });
}

export function pauseProject(ref: string) {
  return request<Project>(`/v1/projects/${ref}/pause`, {
    method: "POST",
  });
}

export function resumeProject(ref: string) {
  return request<Project>(`/v1/projects/${ref}/resume`, {
    method: "POST",
  });
}

export function restartProject(ref: string) {
  return request<Project>(`/v1/projects/${ref}/restart`, {
    method: "POST",
  });
}

export function upgradeProject(ref: string, version: string) {
  return request<Project>(`/v1/projects/${ref}/upgrade`, {
    method: "POST",
    body: JSON.stringify({ version }),
  });
}

export function scaleProject(ref: string, resourceTier: "small" | "medium" | "large" | string) {
  return request<Project>(`/v1/projects/${ref}/scale`, {
    method: "POST",
    body: JSON.stringify({ resource_tier: resourceTier }),
  });
}

export function destroyProject(ref: string, opts?: { retainVolumes?: boolean }) {
  const query = opts?.retainVolumes ? "?retain_volumes=true" : "";
  return request<void>(`/v1/projects/${ref}${query}`, {
    method: "DELETE",
  });
}
