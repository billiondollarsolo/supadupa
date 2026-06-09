import { createContext, useContext } from "react";
import type { ConfigArea, ProjectTab } from "./project-config";
import type { AdvisorFinding, AuditEvent, AuditIntegrity, Backup, BackupPolicy, BackupStorageTarget, BillingInvoice, CDNInvalidation, ComplianceReport, ConnectPayload, FleetMetrics, Host, LogDrain, MFAStatus, Membership, Org, OrgAccessReview, OrgFeatureFlags, OrgQuota, OrgUsage, PITRPolicy, PlatformBackup, PlatformDefaults, PlatformSSOConfig, Project, ProjectAccessGrant, ProjectAnalyticsBucket, ProjectAuthClient, ProjectAuthHook, ProjectBranch, ProjectCDNPolicy, ProjectCLIProfile, ProjectConfig, ProjectDatabaseCronJob, ProjectDatabaseExtension, ProjectDatabaseQueue, ProjectDatabaseRole, ProjectDatabaseSchema, ProjectDatabaseWebhook, ProjectDomain, ProjectEmbeddingJob, ProjectFunction, ProjectFunctionRegion, ProjectFunctionStorageMount, ProjectLog, ProjectMetrics, ProjectNetworkConnection, ProjectNetworkPolicy, ProjectRecoverabilityStatus, ProjectReplica, ProjectReplicaRouting, ProjectReplicationPipeline, ProjectRouteManifest, ProjectServices, ProjectStorageBucket, ProjectVectorBucket, ProvisionerStatus, RuntimeConfig, SCIMGroup, SCIMListResponse, SCIMServiceProviderConfig, SCIMUser, Team, TeamMember, UsageSnapshot, User, WALArchive } from "../types";

export type QueryState<T> = {
  data: T | undefined;
  isLoading: boolean;
};

export type DashboardContextValue = {
  orgsEnabled: boolean;
  ssoScimEnabled: boolean;
  activeOrgId: string;
  activeTeamSlug: string;
  activeRef: string;
  activeProject?: Project;
  activeProjectTab: ProjectTab;
  configArea: ConfigArea;
  projectList: Project[];
  setConfigArea: (area: ConfigArea) => void;
  setSelectedOrgId: (orgId: string) => void;
  setSelectedTeamSlug: (slug: string) => void;
  routeToProject: (ref: string, tab?: ProjectTab) => void;
  onOrgCreated: (orgId: string) => void;
  onProjectCreated: (project: Project) => void;
  onProjectDestroyed: () => void;
  orgs: QueryState<Org[]>;
  hosts: QueryState<Host[]>;
  fleetMetrics: QueryState<FleetMetrics>;
  advisorFindings: QueryState<AdvisorFinding[]>;
  complianceReport: QueryState<ComplianceReport>;
  provisionerStatus: QueryState<ProvisionerStatus>;
  runtimeConfig: QueryState<RuntimeConfig>;
  platformDefaults: QueryState<PlatformDefaults>;
  platformSSO: QueryState<PlatformSSOConfig>;
  backupStorageTargets: QueryState<BackupStorageTarget[]>;
  platformBackups: QueryState<PlatformBackup[]>;
  scimServiceProviderConfig: QueryState<SCIMServiceProviderConfig>;
  scimUsers: QueryState<SCIMListResponse<SCIMUser>>;
  scimGroups: QueryState<SCIMListResponse<SCIMGroup>>;
  auditEvents: QueryState<AuditEvent[]>;
  auditIntegrity: QueryState<AuditIntegrity>;
  users: QueryState<User[]>;
  mfaStatus: QueryState<MFAStatus>;
  members: QueryState<Membership[]>;
  teams: QueryState<Team[]>;
  teamMembers: QueryState<TeamMember[]>;
  orgFeatures: QueryState<OrgFeatureFlags>;
  activeFeatureFlags: Record<string, boolean>;
  quota: QueryState<OrgQuota>;
  usage: QueryState<OrgUsage>;
  usageSnapshots: QueryState<UsageSnapshot[]>;
  billingInvoices: QueryState<BillingInvoice[]>;
  accessReview: QueryState<OrgAccessReview>;
  projects: QueryState<Project[]>;
  connect: QueryState<ConnectPayload>;
  cliProfile: QueryState<ProjectCLIProfile>;
  projectMetrics: QueryState<ProjectMetrics>;
  projectAccess: QueryState<ProjectAccessGrant[]>;
  routeManifest: QueryState<ProjectRouteManifest>;
  domains: QueryState<ProjectDomain[]>;
  projectServices: QueryState<ProjectServices>;
  projectConfig: QueryState<ProjectConfig>;
  authProviderConfig: QueryState<ProjectConfig>;
  authEmailTemplatesConfig: QueryState<ProjectConfig>;
  authSMTPConfig: QueryState<ProjectConfig>;
  databasePoolerConfig: QueryState<ProjectConfig>;
  projectBranches: QueryState<ProjectBranch[]>;
  projectReplicas: QueryState<ProjectReplica[]>;
  projectReplicaRouting: QueryState<ProjectReplicaRouting>;
  projectFunctions: QueryState<ProjectFunction[]>;
  functionRegions: QueryState<ProjectFunctionRegion[]>;
  functionStorageMounts: QueryState<ProjectFunctionStorageMount[]>;
  authClients: QueryState<ProjectAuthClient[]>;
  authHooks: QueryState<ProjectAuthHook[]>;
  replicationPipelines: QueryState<ProjectReplicationPipeline[]>;
  embeddingJobs: QueryState<ProjectEmbeddingJob[]>;
  databaseExtensions: QueryState<ProjectDatabaseExtension[]>;
  databaseCronJobs: QueryState<ProjectDatabaseCronJob[]>;
  databaseQueues: QueryState<ProjectDatabaseQueue[]>;
  databaseWebhooks: QueryState<ProjectDatabaseWebhook[]>;
  databaseSchemas: QueryState<ProjectDatabaseSchema[]>;
  databaseRoles: QueryState<ProjectDatabaseRole[]>;
  storageBuckets: QueryState<ProjectStorageBucket[]>;
  vectorBuckets: QueryState<ProjectVectorBucket[]>;
  analyticsBuckets: QueryState<ProjectAnalyticsBucket[]>;
  cdnPolicy: QueryState<ProjectCDNPolicy>;
  cdnInvalidations: QueryState<CDNInvalidation[]>;
  networkPolicy: QueryState<ProjectNetworkPolicy>;
  networkConnections: QueryState<ProjectNetworkConnection[]>;
  logDrains: QueryState<LogDrain[]>;
  backups: QueryState<Backup[]>;
  backupPolicy: QueryState<BackupPolicy>;
  recoverability: QueryState<ProjectRecoverabilityStatus>;
  pitrPolicy: QueryState<PITRPolicy>;
  walArchives: QueryState<WALArchive[]>;
  projectLogs: QueryState<ProjectLog[]>;
  projectActivity: QueryState<AuditEvent[]>;
};

export const DashboardContext = createContext<DashboardContextValue | null>(null);

export function useDashboardContext() {
  const context = useContext(DashboardContext);
  if (!context) {
    throw new Error("useDashboardContext must be used inside Dashboard");
  }
  return context;
}
