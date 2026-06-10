import type { AdvisorFinding, AuditEvent, AuditEventPage, AuditIntegrity, AuthResponse, AuthState, Backup, BackupPolicy, BackupStorageTarget, BillingInvoice, CDNInvalidation, ComplianceReport, ConnectPayload, CreateBranchResponse, FleetMetrics, Host, HostCapacity, LogDrain, MFAEnrollment, MFAStatus, Membership, Org, OrgAccessReview, OrgFeatureFlags, OrgQuota, OrgUsage, PITRPolicy, PlatformBackup, PlatformDefaults, PlatformSSOConfig, PlatformSSOInitiation, Project, ProjectAccessGrant, ProjectAnalyticsBucket, ProjectAuthClient, ProjectAuthHook, ProjectBranch, ProjectCDNPolicy, ProjectCLIProfile, ProjectConfig, ProjectDatabaseCronJob, ProjectDatabaseExtension, ProjectDatabaseQueue, ProjectDatabaseRole, ProjectDatabaseSchema, ProjectDatabaseWebhook, ProjectDomain, ProjectEmbeddingJob, ProjectFunction, ProjectFunctionRegion, ProjectFunctionStorageMount, ProjectLog, ProjectMetrics, ProjectNetworkConnection, ProjectNetworkPolicy, ProjectRecoverabilityStatus, ProjectReplica, ProjectReplicaRouting, ProjectReplicationPipeline, ProjectRoute, ProjectRouteManifest, ProjectSecret, ProjectSecretReveal, ProjectServices, ProjectStats, ProjectTraffic, FleetTraffic, ProjectStudioSession, ProjectStorageBucket, ProjectVectorBucket, ProvisionerStatus, RestoreToTimeResponse, RuntimeConfig, SCIMGroup, SCIMListResponse, SCIMServiceProviderConfig, SCIMUser, StackReleaseManifest, Team, TeamMember, UpgradeProjectResponse, UsageSnapshot, User, WALArchive } from "./types";

const apiBase = resolveApiBase();

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
    if (runtimeURL && !isLoopbackHost(runtimeURL.hostname)) {
      if (runtimeURL.hostname.startsWith("admin.")) {
        return `${fallbackProtocol}//api.${runtimeURL.hostname.slice("admin.".length)}`;
      }
      return runtimeOrigin;
    }
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

function segment(value: string) {
  return encodeURIComponent(value);
}

function queryString(params: Record<string, string | number | boolean | undefined>) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined) {
      continue;
    }
    query.set(key, String(value));
  }
  const encoded = query.toString();
  return encoded ? `?${encoded}` : "";
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("X-Supadupa-Browser", "true");
  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(`${apiBase}${path}`, {
    ...init,
    credentials: "include",
    headers,
  });
  if (!response.ok) {
    const detail = await response.text();
    throw new Error(formatErrorDetail(detail) || response.statusText);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return response.json() as Promise<T>;
}

function formatErrorDetail(detail: string) {
  if (!detail) return "";
  try {
    const payload = JSON.parse(detail) as {
      error?: string;
      backup?: { id?: string };
      previous_version?: string;
      target_version?: string;
      rollback_attempted?: boolean;
      rollback_error?: string;
    };
    if (!payload.error) return detail;
    const parts = [payload.error];
    if (payload.backup?.id) {
      parts.push(`backup ${payload.backup.id}`);
    }
    if (payload.previous_version && payload.target_version) {
      parts.push(`${payload.previous_version} -> ${payload.target_version}`);
    }
    if (payload.rollback_error) {
      parts.push(`rollback failed: ${payload.rollback_error}`);
    } else if (payload.rollback_attempted) {
      parts.push("rollback attempted");
    }
    return parts.join(" · ");
  } catch {
    return detail;
  }
}

export function getApiHealth() {
  return request<{ status: string; version?: string }>("/v1/health");
}

export function getAuthState(init?: RequestInit) {
  return request<AuthState>("/v1/auth/state", init);
}

export function getProvisionerStatus() {
  return request<ProvisionerStatus>("/v1/provisioner");
}

export function getRuntimeConfig() {
  return request<RuntimeConfig>("/v1/runtime-config");
}

export function listStackReleases() {
  return request<StackReleaseManifest[]>("/v1/stack-releases");
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
  return request<Membership[]>(`/v1/orgs/${segment(orgId)}/members`);
}

export function upsertOrgMember(orgId: string, input: { email: string; role: string }) {
  return request<Membership>(`/v1/orgs/${segment(orgId)}/members`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteOrgMember(orgId: string, email: string) {
  return request<void>(`/v1/orgs/${segment(orgId)}/members/${segment(email)}`, {
    method: "DELETE",
  });
}

export function listOrgTeams(orgId: string) {
  return request<Team[]>(`/v1/orgs/${segment(orgId)}/teams`);
}

export function createOrgTeam(orgId: string, input: { name: string; slug: string }) {
  return request<Team>(`/v1/orgs/${segment(orgId)}/teams`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteOrgTeam(orgId: string, slug: string) {
  return request<void>(`/v1/orgs/${segment(orgId)}/teams/${segment(slug)}`, {
    method: "DELETE",
  });
}

export function listTeamMembers(orgId: string, slug: string) {
  return request<TeamMember[]>(`/v1/orgs/${segment(orgId)}/teams/${segment(slug)}/members`);
}

export function upsertTeamMember(orgId: string, slug: string, email: string) {
  return request<TeamMember>(`/v1/orgs/${segment(orgId)}/teams/${segment(slug)}/members`, {
    method: "POST",
    body: JSON.stringify({ email }),
  });
}

export function deleteTeamMember(orgId: string, slug: string, email: string) {
  return request<void>(`/v1/orgs/${segment(orgId)}/teams/${segment(slug)}/members/${segment(email)}`, {
    method: "DELETE",
  });
}

export function getOrgQuota(orgId: string) {
  return request<OrgQuota>(`/v1/orgs/${segment(orgId)}/quotas`);
}

export function getOrgFeatureFlags(orgId: string) {
  return request<OrgFeatureFlags>(`/v1/orgs/${segment(orgId)}/features`);
}

export function updateOrgFeatureFlags(orgId: string, overrides: Record<string, boolean>) {
  return request<OrgFeatureFlags>(`/v1/orgs/${segment(orgId)}/features`, {
    method: "PUT",
    body: JSON.stringify({ overrides }),
  });
}

export function updateOrgQuota(orgId: string, input: { max_projects: number; max_cpu: number; max_ram_mb: number; max_disk_gb: number }) {
  return request<OrgQuota>(`/v1/orgs/${segment(orgId)}/quotas`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function getOrgUsage(orgId: string) {
  return request<OrgUsage>(`/v1/orgs/${segment(orgId)}/usage`);
}

export function listOrgUsageSnapshots(orgId: string, limit = 10) {
  return request<UsageSnapshot[]>(`/v1/orgs/${segment(orgId)}/usage/snapshots${queryString({ limit })}`);
}

export function createOrgUsageSnapshot(orgId: string) {
  return request<UsageSnapshot>(`/v1/orgs/${segment(orgId)}/usage/snapshots`, {
    method: "POST",
  });
}

export function listBillingInvoices(orgId: string, limit = 10) {
  return request<BillingInvoice[]>(`/v1/orgs/${segment(orgId)}/billing/invoices${queryString({ limit })}`);
}

export function createBillingInvoice(orgId: string, input: { usage_snapshot_id?: string; currency?: string; status?: string; due_days?: number }) {
  return request<BillingInvoice>(`/v1/orgs/${segment(orgId)}/billing/invoices`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function getBillingInvoice(orgId: string, invoiceId: string) {
  return request<BillingInvoice>(`/v1/orgs/${segment(orgId)}/billing/invoices/${segment(invoiceId)}`);
}

export function getOrgAccessReview(orgId: string) {
  return request<OrgAccessReview>(`/v1/orgs/${segment(orgId)}/access-review`);
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
  return request<ProjectMetrics>(`/v1/projects/${segment(ref)}/metrics`);
}

export function getProjectStats(ref: string) {
  return request<ProjectStats>(`/v1/projects/${segment(ref)}/stats`);
}

export function getProjectTraffic(ref: string) {
  return request<ProjectTraffic>(`/v1/projects/${segment(ref)}/traffic`);
}

export function getFleetTraffic() {
  return request<FleetTraffic>(`/v1/metrics/traffic`);
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

export type PlatformSSOConfigInput = Omit<PlatformSSOConfig, "provider" | "updated_at" | "scim_token_configured"> & {
  scim_token?: string;
};

export function updatePlatformSSOConfig(input: PlatformSSOConfigInput) {
  return request<PlatformSSOConfig>("/v1/settings/sso", {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export type BackupStorageTargetInput = {
  name: string;
  type?: string;
  endpoint: string;
  region: string;
  bucket: string;
  prefix: string;
  access_key_id: string;
  secret_access_key?: string;
  force_path_style: boolean;
  default: boolean;
};

export function listBackupStorageTargets() {
  return request<BackupStorageTarget[]>("/v1/backup-storage-targets");
}

export function createBackupStorageTarget(input: BackupStorageTargetInput) {
  return request<BackupStorageTarget>("/v1/backup-storage-targets", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function updateBackupStorageTarget(id: string, input: BackupStorageTargetInput) {
  return request<BackupStorageTarget>(`/v1/backup-storage-targets/${segment(id)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function testBackupStorageTarget(id: string) {
  return request<BackupStorageTarget>(`/v1/backup-storage-targets/${segment(id)}/test`, {
    method: "POST",
  });
}

export function deleteBackupStorageTarget(id: string) {
  return request<void>(`/v1/backup-storage-targets/${segment(id)}`, {
    method: "DELETE",
  });
}

export function listPlatformBackups() {
  return request<PlatformBackup[]>("/v1/platform/backups");
}

export function triggerPlatformBackup() {
  return request<PlatformBackup>("/v1/platform/backups", {
    method: "POST",
  });
}

export function restorePlatformBackup(id: string) {
  return request<{ backup: PlatformBackup; restore_path: string; restore_state: string; runtime_reconciled: number; runtime_destroyed: number; runtime_errors?: string[] }>(`/v1/platform/backups/${segment(id)}/restore`, {
    method: "POST",
    body: JSON.stringify({ confirm: "restore-control-plane" }),
  });
}

export function getSCIMServiceProviderConfig() {
  return request<SCIMServiceProviderConfig>("/v1/scim/v2/ServiceProviderConfig");
}

export function listSCIMUsers() {
  return request<SCIMListResponse<SCIMUser>>("/v1/scim/v2/Users");
}

export function listSCIMGroups(orgId?: string) {
  return request<SCIMListResponse<SCIMGroup>>(`/v1/scim/v2/Groups${queryString({ org_id: orgId })}`);
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

export function logoutSession() {
  return request<void>("/v1/auth/logout", {
    method: "POST",
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

export function updateUser(id: string, input: { email: string; role: string; password?: string }) {
  return request<User>(`/v1/users/${segment(id)}`, {
    method: "PUT",
    body: JSON.stringify({ email: input.email, role: input.role, password: input.password ?? "" }),
  });
}

export function deleteUser(id: string) {
  return request<void>(`/v1/users/${segment(id)}`, { method: "DELETE" });
}

export function changeAccountPassword(input: { current_password: string; new_password: string }) {
  return request<void>("/v1/account/password", {
    method: "POST",
    body: JSON.stringify(input),
  });
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
  return request<void>(`/v1/hosts/${segment(id)}`, {
    method: "DELETE",
  });
}

export function listProjects() {
  return request<Project[]>("/v1/projects");
}

export function getProject(ref: string) {
  return request<Project>(`/v1/projects/${segment(ref)}`);
}

export function listOrgProjects(orgId: string) {
  return request<Project[]>(`/v1/orgs/${segment(orgId)}/projects`);
}

export type CreateProjectInput = {
  orgId: string;
  ref: string;
  name: string;
  host_id: string;
  domain: string;
  stack_version: string;
  profile: "essential" | "full" | "orioledb";
  resource_tier: "small" | "medium" | "large";
  // Optional exact-size overrides. 0/undefined means "use the tier preset".
  cpu?: number;
  ram_mb?: number;
  disk_gb?: number;
  // Opt-in: apply real container CPU/memory limits to the database service.
  enforce_limits?: boolean;
  // Per-service enable map (subset of the supported Supabase services).
  services?: Record<string, boolean>;
};

export function createProject(input: CreateProjectInput) {
  const { orgId, ...payload } = input;
  return request<Project>(`/v1/orgs/${segment(orgId)}/projects`, {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function getConnect(ref: string) {
  return request<ConnectPayload>(`/v1/projects/${segment(ref)}/connect`);
}

export function getProjectCLIProfile(ref: string) {
  return request<ProjectCLIProfile>(`/v1/projects/${segment(ref)}/connect/cli`);
}

export function createProjectStudioSession(ref: string) {
  return request<ProjectStudioSession>(`/v1/projects/${segment(ref)}/studio-session`);
}

export function listProjectAccess(ref: string) {
  return request<ProjectAccessGrant[]>(`/v1/projects/${segment(ref)}/access`);
}

export function upsertProjectAccess(ref: string, input: { subject_type: string; subject_id: string; role: string }) {
  return request<ProjectAccessGrant>(`/v1/projects/${segment(ref)}/access`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function deleteProjectAccess(ref: string, subjectType: string, subjectId: string) {
  return request<void>(`/v1/projects/${segment(ref)}/access/${segment(subjectType)}/${segment(subjectId)}`, {
    method: "DELETE",
  });
}

export function listProjectRoutes(ref: string) {
  return request<ProjectRoute[]>(`/v1/projects/${segment(ref)}/routes`);
}

export function getProjectRouteManifest(ref: string) {
  return request<ProjectRouteManifest>(`/v1/projects/${segment(ref)}/route-manifest`);
}

export function listProjectDomains(ref: string) {
  return request<ProjectDomain[]>(`/v1/projects/${segment(ref)}/domains`);
}

export function getProjectServices(ref: string) {
  return request<ProjectServices>(`/v1/projects/${segment(ref)}/services`);
}

export function updateProjectServices(ref: string, services: Record<string, boolean>) {
  return request<ProjectServices>(`/v1/projects/${segment(ref)}/services`, {
    method: "PUT",
    body: JSON.stringify({ services }),
  });
}

export function addProjectDomain(ref: string, fqdn: string) {
  return request<ProjectDomain>(`/v1/projects/${segment(ref)}/domains`, {
    method: "POST",
    body: JSON.stringify({ fqdn }),
  });
}

export function deleteProjectDomain(ref: string, fqdn: string) {
  return request<void>(`/v1/projects/${segment(ref)}/domains/${segment(fqdn)}`, {
    method: "DELETE",
  });
}

export function uploadProjectDomainCertificate(ref: string, fqdn: string, certificatePEM: string, privateKeyPEM: string) {
  return request<ProjectDomain>(`/v1/projects/${segment(ref)}/domains/${segment(fqdn)}/certificate`, {
    method: "PUT",
    body: JSON.stringify({ certificate_pem: certificatePEM, private_key_pem: privateKeyPEM }),
  });
}

export function resetProjectDomainCertificate(ref: string, fqdn: string) {
  return request<ProjectDomain>(`/v1/projects/${segment(ref)}/domains/${segment(fqdn)}/certificate`, {
    method: "DELETE",
  });
}

export function getProjectConfig(ref: string, area: string) {
  return request<ProjectConfig>(`/v1/projects/${segment(ref)}/config/${segment(area)}`);
}

export function updateProjectConfig(ref: string, area: string, config: Record<string, string>) {
  return request<ProjectConfig>(`/v1/projects/${segment(ref)}/config/${segment(area)}`, {
    method: "PUT",
    body: JSON.stringify({ config }),
  });
}

export function listProjectBranches(ref: string) {
  return request<ProjectBranch[]>(`/v1/projects/${segment(ref)}/branches`);
}

export function createProjectBranch(ref: string, input: { ref: string; name: string; ttl_hours: number; with_data: boolean }) {
  return request<CreateBranchResponse>(`/v1/projects/${segment(ref)}/branches`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectBranch(ref: string, branchRef: string) {
  return request<void>(`/v1/projects/${segment(ref)}/branches/${segment(branchRef)}`, {
    method: "DELETE",
  });
}

export function listProjectReplicas(ref: string) {
  return request<ProjectReplica[]>(`/v1/projects/${segment(ref)}/replicas`);
}

export function getProjectReplicaRouting(ref: string) {
  return request<ProjectReplicaRouting>(`/v1/projects/${segment(ref)}/replicas/routing`);
}

export function createProjectReplica(ref: string, input: { name: string; host_id: string; region: string; tier: "small" | "medium" | "large" | string; read_weight: number; failover_priority: number }) {
  return request<ProjectReplica>(`/v1/projects/${segment(ref)}/replicas`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectReplica(ref: string, id: string) {
  return request<void>(`/v1/projects/${segment(ref)}/replicas/${segment(id)}`, {
    method: "DELETE",
  });
}

export function promoteProjectReplica(ref: string, id: string, reason: string) {
  return request<ProjectReplica>(`/v1/projects/${segment(ref)}/replicas/${segment(id)}/promote`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });
}

export function failoverProjectReplica(ref: string, reason: string) {
  return request<ProjectReplica>(`/v1/projects/${segment(ref)}/replicas/failover`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });
}

export function listProjectFunctions(ref: string) {
  return request<ProjectFunction[]>(`/v1/projects/${segment(ref)}/functions`);
}

export function deployProjectFunction(ref: string, input: { name: string; entrypoint: string; verify_jwt: boolean; source: string; secrets: Record<string, string> }) {
  return request<ProjectFunction>(`/v1/projects/${segment(ref)}/functions`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectFunction(ref: string, name: string) {
  return request<void>(`/v1/projects/${segment(ref)}/functions/${segment(name)}`, {
    method: "DELETE",
  });
}

export function listProjectFunctionRegions(ref: string) {
  return request<ProjectFunctionRegion[]>(`/v1/projects/${segment(ref)}/functions/regions`);
}

export function createProjectFunctionRegion(ref: string, input: {
  function_name: string;
  host_id: string;
  region: string;
  routing_policy: string;
}) {
  return request<ProjectFunctionRegion>(`/v1/projects/${segment(ref)}/functions/regions`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectFunctionRegion(ref: string, id: string) {
  return request<void>(`/v1/projects/${segment(ref)}/functions/regions/${segment(id)}`, {
    method: "DELETE",
  });
}

export function listProjectFunctionStorageMounts(ref: string) {
  return request<ProjectFunctionStorageMount[]>(`/v1/projects/${segment(ref)}/functions/storage-mounts`);
}

export function createProjectFunctionStorageMount(ref: string, input: {
  function_name: string;
  bucket_name: string;
  mount_path: string;
  read_only: boolean;
  prefix: string;
  env_alias: string;
}) {
  return request<ProjectFunctionStorageMount>(`/v1/projects/${segment(ref)}/functions/storage-mounts`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectFunctionStorageMount(ref: string, id: string) {
  return request<void>(`/v1/projects/${segment(ref)}/functions/storage-mounts/${segment(id)}`, {
    method: "DELETE",
  });
}

export function listProjectAuthClients(ref: string) {
  return request<ProjectAuthClient[]>(`/v1/projects/${segment(ref)}/auth/clients`);
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
  return request<ProjectAuthClient>(`/v1/projects/${segment(ref)}/auth/clients`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectAuthClient(ref: string, clientId: string) {
  return request<void>(`/v1/projects/${segment(ref)}/auth/clients/${segment(clientId)}`, {
    method: "DELETE",
  });
}

export function listProjectAuthHooks(ref: string) {
  return request<ProjectAuthHook[]>(`/v1/projects/${segment(ref)}/auth/hooks`);
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
  return request<ProjectAuthHook>(`/v1/projects/${segment(ref)}/auth/hooks`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectAuthHook(ref: string, hookType: string) {
  return request<void>(`/v1/projects/${segment(ref)}/auth/hooks/${segment(hookType)}`, {
    method: "DELETE",
  });
}

export function listProjectReplicationPipelines(ref: string) {
  return request<ProjectReplicationPipeline[]>(`/v1/projects/${segment(ref)}/replication`);
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
  return request<ProjectReplicationPipeline>(`/v1/projects/${segment(ref)}/replication`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectReplicationPipeline(ref: string, id: string) {
  return request<void>(`/v1/projects/${segment(ref)}/replication/${segment(id)}`, {
    method: "DELETE",
  });
}

export function listProjectEmbeddingJobs(ref: string) {
  return request<ProjectEmbeddingJob[]>(`/v1/projects/${segment(ref)}/embeddings`);
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
  return request<ProjectEmbeddingJob>(`/v1/projects/${segment(ref)}/embeddings`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectEmbeddingJob(ref: string, id: string) {
  return request<void>(`/v1/projects/${segment(ref)}/embeddings/${segment(id)}`, {
    method: "DELETE",
  });
}

export function listProjectDatabaseExtensions(ref: string) {
  return request<ProjectDatabaseExtension[]>(`/v1/projects/${segment(ref)}/database/extensions`);
}

export function updateProjectDatabaseExtension(ref: string, name: string, input: {
  schema: string;
  version: string;
  enabled: boolean;
}) {
  return request<ProjectDatabaseExtension>(`/v1/projects/${segment(ref)}/database/extensions/${segment(name)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function listProjectDatabaseCronJobs(ref: string) {
  return request<ProjectDatabaseCronJob[]>(`/v1/projects/${segment(ref)}/database/cron-jobs`);
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
  return request<ProjectDatabaseCronJob>(`/v1/projects/${segment(ref)}/database/cron-jobs`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectDatabaseCronJob(ref: string, name: string) {
  return request<void>(`/v1/projects/${segment(ref)}/database/cron-jobs/${segment(name)}`, {
    method: "DELETE",
  });
}

export function listProjectDatabaseQueues(ref: string) {
  return request<ProjectDatabaseQueue[]>(`/v1/projects/${segment(ref)}/database/queues`);
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
  return request<ProjectDatabaseQueue>(`/v1/projects/${segment(ref)}/database/queues`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectDatabaseQueue(ref: string, name: string) {
  return request<void>(`/v1/projects/${segment(ref)}/database/queues/${segment(name)}`, {
    method: "DELETE",
  });
}

export function listProjectDatabaseWebhooks(ref: string) {
  return request<ProjectDatabaseWebhook[]>(`/v1/projects/${segment(ref)}/database/webhooks`);
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
  return request<ProjectDatabaseWebhook>(`/v1/projects/${segment(ref)}/database/webhooks`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectDatabaseWebhook(ref: string, name: string) {
  return request<void>(`/v1/projects/${segment(ref)}/database/webhooks/${segment(name)}`, {
    method: "DELETE",
  });
}

export function listProjectDatabaseSchemas(ref: string) {
  return request<ProjectDatabaseSchema[]>(`/v1/projects/${segment(ref)}/database/schemas`);
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
  return request<ProjectDatabaseSchema>(`/v1/projects/${segment(ref)}/database/schemas`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectDatabaseSchema(ref: string, name: string, version: string) {
  return request<void>(`/v1/projects/${segment(ref)}/database/schemas/${segment(name)}/${segment(version)}`, {
    method: "DELETE",
  });
}

export function listProjectDatabaseRoles(ref: string) {
  return request<ProjectDatabaseRole[]>(`/v1/projects/${segment(ref)}/database/roles`);
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
  return request<ProjectDatabaseRole>(`/v1/projects/${segment(ref)}/database/roles`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectDatabaseRole(ref: string, name: string) {
  return request<void>(`/v1/projects/${segment(ref)}/database/roles/${segment(name)}`, {
    method: "DELETE",
  });
}

export function listProjectStorageBuckets(ref: string) {
  return request<ProjectStorageBucket[]>(`/v1/projects/${segment(ref)}/storage/buckets`);
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
  return request<ProjectStorageBucket>(`/v1/projects/${segment(ref)}/storage/buckets`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectStorageBucket(ref: string, name: string) {
  return request<void>(`/v1/projects/${segment(ref)}/storage/buckets/${segment(name)}`, {
    method: "DELETE",
  });
}

export function listProjectVectorBuckets(ref: string) {
  return request<ProjectVectorBucket[]>(`/v1/projects/${segment(ref)}/vector-buckets`);
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
  return request<ProjectVectorBucket>(`/v1/projects/${segment(ref)}/vector-buckets`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectVectorBucket(ref: string, name: string) {
  return request<void>(`/v1/projects/${segment(ref)}/vector-buckets/${segment(name)}`, {
    method: "DELETE",
  });
}

export function listProjectAnalyticsBuckets(ref: string) {
  return request<ProjectAnalyticsBucket[]>(`/v1/projects/${segment(ref)}/analytics-buckets`);
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
  return request<ProjectAnalyticsBucket>(`/v1/projects/${segment(ref)}/analytics-buckets`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectAnalyticsBucket(ref: string, name: string) {
  return request<void>(`/v1/projects/${segment(ref)}/analytics-buckets/${segment(name)}`, {
    method: "DELETE",
  });
}

export function getProjectCDNPolicy(ref: string) {
  return request<ProjectCDNPolicy>(`/v1/projects/${segment(ref)}/cdn/policy`);
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
  return request<ProjectCDNPolicy>(`/v1/projects/${segment(ref)}/cdn/policy`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function listProjectCDNInvalidations(ref: string) {
  return request<CDNInvalidation[]>(`/v1/projects/${segment(ref)}/cdn/invalidations`);
}

export function createProjectCDNInvalidation(ref: string, paths: string[]) {
  return request<CDNInvalidation>(`/v1/projects/${segment(ref)}/cdn/invalidations`, {
    method: "POST",
    body: JSON.stringify({ paths }),
  });
}

export function createProjectCDNObjectEvent(ref: string, input: { event_id: string; bucket: string; object_path: string; event_type: string }) {
  return request<CDNInvalidation>(`/v1/projects/${segment(ref)}/cdn/object-events`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function listProjectNetworkConnections(ref: string) {
  return request<ProjectNetworkConnection[]>(`/v1/projects/${segment(ref)}/network-connections`);
}

export function getProjectNetwork(ref: string) {
  return request<ProjectNetworkPolicy>(`/v1/projects/${segment(ref)}/network`);
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
  return request<ProjectNetworkConnection>(`/v1/projects/${segment(ref)}/network-connections`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectNetworkConnection(ref: string, id: string) {
  return request<void>(`/v1/projects/${segment(ref)}/network-connections/${segment(id)}`, {
    method: "DELETE",
  });
}

export function listProjectLogDrains(ref: string) {
  return request<LogDrain[]>(`/v1/projects/${segment(ref)}/log-drains`);
}

export function createProjectLogDrain(ref: string, input: { target: string; config: Record<string, string> }) {
  return request<LogDrain>(`/v1/projects/${segment(ref)}/log-drains`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteProjectLogDrain(ref: string, id: string) {
  return request<void>(`/v1/projects/${segment(ref)}/log-drains/${segment(id)}`, {
    method: "DELETE",
  });
}

export function listAuditEvents(params?: { limit?: number; offset?: number; action?: string; actor?: string; since?: string; until?: string }) {
  const qs = new URLSearchParams();
  if (params?.limit != null) qs.set("limit", String(params.limit));
  if (params?.offset != null) qs.set("offset", String(params.offset));
  if (params?.action) qs.set("action", params.action);
  if (params?.actor) qs.set("actor", params.actor);
  if (params?.since) qs.set("since", params.since);
  if (params?.until) qs.set("until", params.until);
  const query = qs.toString();
  return request<AuditEventPage>(`/v1/audit-events${query ? `?${query}` : ""}`);
}

export function getAuditIntegrity() {
  return request<AuditIntegrity>("/v1/audit-events/integrity");
}

export function listBackups(ref: string) {
  return request<Backup[]>(`/v1/projects/${segment(ref)}/backups`);
}

export function triggerBackup(ref: string) {
  return request<Backup>(`/v1/projects/${segment(ref)}/backups`, {
    method: "POST",
  });
}

export function restoreBackup(ref: string, backupId: string) {
  return request<{ backup: Backup; restore_path: string; restore_state: string }>(`/v1/projects/${segment(ref)}/restore`, {
    method: "POST",
    body: JSON.stringify({ backup_id: backupId }),
  });
}

export function restoreToTime(ref: string, recoveryTimeTargetUnix: number) {
  return request<RestoreToTimeResponse>(`/v1/projects/${segment(ref)}/database/backups/restore-pitr`, {
    method: "POST",
    body: JSON.stringify({ recovery_time_target_unix: String(recoveryTimeTargetUnix) }),
  });
}

export function getBackupPolicy(ref: string) {
  return request<BackupPolicy>(`/v1/projects/${segment(ref)}/backups/policy`);
}

export function getProjectRecoverability(ref: string) {
  return request<ProjectRecoverabilityStatus>(`/v1/projects/${segment(ref)}/recoverability`);
}

export function updateBackupPolicy(ref: string, input: { enabled: boolean; schedule: string; kind: string; storage_target_id?: string }) {
  return request<BackupPolicy>(`/v1/projects/${segment(ref)}/backups/policy`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function getPITRPolicy(ref: string) {
  return request<PITRPolicy>(`/v1/projects/${segment(ref)}/pitr/policy`);
}

export function updatePITRPolicy(ref: string, input: { enabled: boolean; archive_bucket: string; retention_days: number }) {
  return request<PITRPolicy>(`/v1/projects/${segment(ref)}/pitr/policy`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function listWALArchives(ref: string) {
  return request<WALArchive[]>(`/v1/projects/${segment(ref)}/pitr/wal`);
}

export function archiveWAL(ref: string) {
  return request<WALArchive>(`/v1/projects/${segment(ref)}/pitr/wal`, {
    method: "POST",
  });
}

export function listProjectLogs(ref: string) {
  return request<ProjectLog[]>(`/v1/projects/${segment(ref)}/logs`);
}

export async function streamProjectLogs(ref: string, onLog: (entry: ProjectLog) => void, signal?: AbortSignal) {
  const response = await fetch(`${apiBase}/v1/projects/${segment(ref)}/logs?stream=true`, {
    headers: {
      Accept: "text/event-stream",
    },
    credentials: "include",
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
  return request<AuditEvent[]>(`/v1/projects/${segment(ref)}/activity`);
}

export function listProjectSecrets(ref: string) {
  return request<ProjectSecret[]>(`/v1/projects/${segment(ref)}/secrets`);
}

export function revealProjectSecret(ref: string, kind: string) {
  return request<ProjectSecretReveal>(`/v1/projects/${segment(ref)}/secrets/${segment(kind)}/reveal`);
}

export function auditProjectSecretCopy(ref: string, kind: string) {
  return request<void>(`/v1/projects/${segment(ref)}/secrets/${segment(kind)}/copy`, {
    method: "POST",
  });
}

export function rotateProjectSecret(ref: string, kind: string) {
  return request<ProjectSecret>(`/v1/projects/${segment(ref)}/keys/rotate`, {
    method: "POST",
    body: JSON.stringify({ kind }),
  });
}

export function pauseProject(ref: string) {
  return request<Project>(`/v1/projects/${segment(ref)}/pause`, {
    method: "POST",
  });
}

export function resumeProject(ref: string) {
  return request<Project>(`/v1/projects/${segment(ref)}/resume`, {
    method: "POST",
  });
}

export function restartProject(ref: string) {
  return request<Project>(`/v1/projects/${segment(ref)}/restart`, {
    method: "POST",
  });
}

export function upgradeProject(ref: string, version: string) {
  return request<UpgradeProjectResponse>(`/v1/projects/${segment(ref)}/upgrade`, {
    method: "POST",
    body: JSON.stringify({ version }),
  });
}

export function scaleProject(ref: string, resourceTier: "small" | "medium" | "large" | string) {
  return request<Project>(`/v1/projects/${segment(ref)}/scale`, {
    method: "POST",
    body: JSON.stringify({ resource_tier: resourceTier }),
  });
}

export function destroyProject(ref: string, opts?: { retainVolumes?: boolean }) {
  return request<void>(`/v1/projects/${segment(ref)}${queryString({ retain_volumes: opts?.retainVolumes ? true : undefined })}`, {
    method: "DELETE",
  });
}
