import { KeyboardEvent, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { Activity, ArrowLeft, Boxes, CheckCircle2, Command, Database, KeyRound, LogOut, Moon, Pause, Play, Plus, RadioTower, RotateCcw, Search, Server, Shield, SlidersHorizontal, Sun, UserCircle, UserPlus, type LucideIcon } from "lucide-react";
import {
  createPlatformUser,
  getAuditIntegrity,
  getAccountMFA,
  getApiHealth,
  getAdvisorFindings,
  getComplianceReport,
  getConnect,
  getBackupPolicy,
  getFleetMetrics,
  getOrgFeatureFlags,
  getOrgAccessReview,
  getOrgQuota,
  getOrgUsage,
  getPITRPolicy,
  getPlatformDefaults,
  getPlatformSSOConfig,
  getProject,
  getProjectCLIProfile,
  getProjectConfig,
  getProjectCDNPolicy,
  getProjectMetrics,
  getProjectNetwork,
  getProjectRecoverability,
  getProjectReplicaRouting,
  getProjectServices,
  getProvisionerStatus,
  getRuntimeConfig,
  getSCIMServiceProviderConfig,
  listAuditEvents,
  listBackupStorageTargets,
  listPlatformBackups,
  listBillingInvoices,
  listBackups,
  listHosts,
  listOrgs,
  listOrgMembers,
  listOrgTeams,
  listOrgUsageSnapshots,
  listProjectAccess,
  listProjectAnalyticsBuckets,
  listProjectAuthClients,
  listProjectAuthHooks,
  listWALArchives,
  listProjectDomains,
  listProjectBranches,
  listProjectCDNInvalidations,
  listProjectDatabaseExtensions,
  listProjectDatabaseCronJobs,
  listProjectDatabaseQueues,
  listProjectDatabaseWebhooks,
  listProjectDatabaseSchemas,
  listProjectDatabaseRoles,
  listProjectEmbeddingJobs,
  listProjectFunctions,
  listProjectFunctionRegions,
  listProjectFunctionStorageMounts,
  listProjectLogDrains,
  listProjectNetworkConnections,
  listProjectReplicas,
  listProjectReplicationPipelines,
  getProjectRouteManifest,
  listProjectStorageBuckets,
  listProjectVectorBuckets,
  listProjectActivity,
  listProjectLogs,
  listProjects,
  listProjectSecrets,
  listSCIMGroups,
  listSCIMUsers,
  listTeamMembers,
  listUsers,
  pauseProject,
  restartProject,
  resumeProject,
  triggerBackup,
} from "./api";
import { formatBytes, formatDateTime, formatMoney } from "./lib/format";
import { BrandLogo } from "./components/brand-logo";
import { BrandWordmark } from "./components/brand-wordmark";
import { useAuthSession } from "./lib/auth-session";
import { DashboardContext, useDashboardContext, type DashboardContextValue } from "./lib/dashboard-context";
import { organizationSections, platformSettingsSections, projectSubnav, projectTabs, securitySections, type ConfigArea, type ProjectTab } from "./lib/project-config";
import { useUIStore } from "./lib/ui-store";
import type { AdvisorFinding, AuditEvent, AuditIntegrity, Backup, BackupPolicy, BackupStorageTarget, BillingInvoice, CDNInvalidation, ComplianceReport, ConnectPayload, FleetMetrics, Host, LogDrain, MFAStatus, Membership, Org, OrgAccessReview, OrgFeatureFlags, OrgQuota, OrgUsage, PITRPolicy, PlatformDefaults, PlatformSSOConfig, Project, ProjectAccessGrant, ProjectAnalyticsBucket, ProjectAuthClient, ProjectAuthHook, ProjectBranch, ProjectCDNPolicy, ProjectCLIProfile, ProjectConfig, ProjectDatabaseCronJob, ProjectDatabaseExtension, ProjectDatabaseQueue, ProjectDatabaseRole, ProjectDatabaseSchema, ProjectDatabaseWebhook, ProjectDomain, ProjectEmbeddingJob, ProjectFunction, ProjectFunctionRegion, ProjectFunctionStorageMount, ProjectLog, ProjectMetrics, ProjectNetworkConnection, ProjectNetworkPolicy, ProjectReplica, ProjectReplicaRouting, ProjectReplicationPipeline, ProjectSecret, ProjectServices, ProjectStorageBucket, ProjectVectorBucket, ProvisionerStatus, RuntimeConfig, SCIMGroup, SCIMListResponse, SCIMServiceProviderConfig, SCIMUser, Team, TeamMember, UsageSnapshot, User, WALArchive } from "./types";
import { Modal } from "./components/modal";

type PaletteAction = {
  id: string;
  title: string;
  subtitle: string;
  group: string;
  icon: LucideIcon;
  keywords?: string[];
  disabled?: boolean;
  run: () => void;
};

type ProjectCommandConfirm = {
  action: "pause" | "resume" | "restart" | "backup";
  ref: string;
};

function projectRefFromPathname(pathname: string) {
  const match = pathname.match(/^\/projects\/([^/]+)/);
  if (!match || match[1] === "new") {
    return "";
  }
  return decodeURIComponent(match[1]);
}

function projectTabFromPathname(pathname: string): ProjectTab {
  const suffix = pathname.match(/^\/projects\/[^/]+\/([^/]+)/)?.[1] ?? "";
  return projectTabs.find((tab) => tab.suffix === suffix)?.id ?? "overview";
}

function projectSectionFromPathname(pathname: string) {
  return pathname.match(/^\/projects\/[^/]+\/[^/]+\/([^/]+)/)?.[1] ?? "overview";
}

function panelIdForPathname(pathname: string) {
  if (pathname === "/organizations" || pathname.startsWith("/organizations/")) return "organizations";
  if (pathname === "/projects") return "projects-list";
  if (pathname === "/projects/new") return "create-project";
  if (pathname.startsWith("/projects/")) return `project-${projectTabFromPathname(pathname)}`;
  if (pathname === "/security" || pathname.startsWith("/security/")) return "security";
  if (pathname === "/hosts") return "hosts";
  if (pathname === "/settings" || pathname.startsWith("/settings/")) return "settings";
  if (pathname === "/audit") return "audit-log";
  return "fleet-dashboard";
}

function pageTitleForPathname(pathname: string, activeProject: Project | undefined, activeProjectTab: ProjectTab) {
  if (pathname === "/organizations" || pathname.startsWith("/organizations/")) return "Organizations";
  if (pathname === "/projects") return "Projects";
  if (pathname === "/projects/new") return "Create project";
  if (pathname.startsWith("/projects/")) {
    const tabLabel = projectTabs.find((tab) => tab.id === activeProjectTab)?.label ?? "Overview";
    return activeProject ? tabLabel : `Project ${tabLabel}`;
  }
  if (pathname === "/security" || pathname.startsWith("/security/")) return "Security";
  if (pathname === "/hosts") return "Hosts";
  if (pathname === "/settings" || pathname.startsWith("/settings/")) return "Settings";
  if (pathname === "/audit") return "Audit log";
  return "Dashboard";
}

export function App() {
  const token = useAuthSession((state) => state.token);
  const logout = useAuthSession((state) => state.logout);
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  useEffect(() => {
    document.title = "SUPADUPA";
  }, [pathname]);

  useEffect(() => {
    if (!token && pathname !== "/login") {
      void navigate({ to: "/login" });
    }
    if (token && pathname === "/login") {
      void navigate({ to: "/" });
    }
  }, [navigate, pathname, token]);

  if (!token) {
    if (pathname === "/login") {
      return <Outlet />;
    }
    return <main className="min-h-screen bg-bg text-text" />;
  }

  if (pathname === "/login") {
    return <main className="min-h-screen bg-bg text-text" />;
  }

  return <Dashboard onLogout={logout} />;
}

function Dashboard({ onLogout }: { onLogout: () => void }) {
  const token = useAuthSession((state) => state.token);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const openPalette = useUIStore((state) => state.openPalette);
  const addToast = useUIStore((state) => state.addToast);
  const theme = useUIStore((state) => state.theme);
  const setTheme = useUIStore((state) => state.setTheme);
  const [selectedOrgId, setSelectedOrgId] = useState<string>("");
  const [selectedRef, setSelectedRef] = useState<string>("");
  const [selectedTeamSlug, setSelectedTeamSlug] = useState<string>("");
  const [configArea, setConfigArea] = useState<ConfigArea>("auth");
  const [projectCommandConfirm, setProjectCommandConfirm] = useState<ProjectCommandConfirm | null>(null);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  const apiHealth = useQuery({ queryKey: ["api-health"], queryFn: getApiHealth, refetchInterval: 30_000 });
  const orgs = useQuery({ queryKey: ["orgs"], queryFn: listOrgs });
  const hosts = useQuery({ queryKey: ["hosts"], queryFn: listHosts });
  const fleetMetrics = useQuery({ queryKey: ["fleet-metrics"], queryFn: getFleetMetrics, refetchInterval: 15_000 });
  const advisorFindings = useQuery({ queryKey: ["advisor-findings"], queryFn: getAdvisorFindings, refetchInterval: 30_000 });
  const complianceReport = useQuery({ queryKey: ["compliance-report"], queryFn: getComplianceReport, refetchInterval: 30_000 });
  const provisionerStatus = useQuery({ queryKey: ["provisioner-status"], queryFn: getProvisionerStatus });
  const runtimeConfig = useQuery({ queryKey: ["runtime-config"], queryFn: getRuntimeConfig, refetchInterval: 30_000 });
  const platformDefaults = useQuery({ queryKey: ["platform-defaults"], queryFn: getPlatformDefaults });
  const platformSSO = useQuery({ queryKey: ["platform-sso"], queryFn: getPlatformSSOConfig });
  const backupStorageTargets = useQuery({ queryKey: ["backup-storage-targets"], queryFn: listBackupStorageTargets });
  const platformBackups = useQuery({ queryKey: ["platform-backups"], queryFn: listPlatformBackups });
  const scimServiceProviderConfig = useQuery({ queryKey: ["scim-service-provider-config"], queryFn: getSCIMServiceProviderConfig });
  const scimUsers = useQuery({ queryKey: ["scim-users"], queryFn: listSCIMUsers });
  const scimGroups = useQuery({ queryKey: ["scim-groups"], queryFn: () => listSCIMGroups() });
  const auditEvents = useQuery({ queryKey: ["audit-events"], queryFn: listAuditEvents, refetchInterval: 10_000 });
  const auditIntegrity = useQuery({ queryKey: ["audit-integrity"], queryFn: getAuditIntegrity, refetchInterval: 10_000 });
  const users = useQuery({ queryKey: ["users"], queryFn: listUsers });
  const mfaStatus = useQuery({ queryKey: ["account-mfa"], queryFn: getAccountMFA });
  const activeOrgId = selectedOrgId || orgs.data?.[0]?.id || "";
  const members = useQuery({
    queryKey: ["org-members", activeOrgId],
    queryFn: () => listOrgMembers(activeOrgId),
    enabled: activeOrgId.length > 0,
  });
  const teams = useQuery({
    queryKey: ["org-teams", activeOrgId],
    queryFn: () => listOrgTeams(activeOrgId),
    enabled: activeOrgId.length > 0,
  });
  const activeTeamSlug = selectedTeamSlug || teams.data?.[0]?.slug || "";
  const teamMembers = useQuery({
    queryKey: ["team-members", activeOrgId, activeTeamSlug],
    queryFn: () => listTeamMembers(activeOrgId, activeTeamSlug),
    enabled: activeOrgId.length > 0 && activeTeamSlug.length > 0,
  });
  const orgFeatures = useQuery({
    queryKey: ["org-features", activeOrgId],
    queryFn: () => getOrgFeatureFlags(activeOrgId),
    enabled: activeOrgId.length > 0,
  });
  const activeOrgFeatures = orgFeatures.data?.effective ?? {};
  const quota = useQuery({
    queryKey: ["org-quota", activeOrgId],
    queryFn: () => getOrgQuota(activeOrgId),
    enabled: activeOrgId.length > 0,
  });
  const usage = useQuery({
    queryKey: ["org-usage", activeOrgId],
    queryFn: () => getOrgUsage(activeOrgId),
    enabled: activeOrgId.length > 0,
    refetchInterval: 15_000,
  });
  const usageSnapshots = useQuery({
    queryKey: ["org-usage-snapshots", activeOrgId],
    queryFn: () => listOrgUsageSnapshots(activeOrgId, 6),
    enabled: activeOrgId.length > 0 && Boolean(activeOrgFeatures.usage_metering),
  });
  const billingInvoices = useQuery({
    queryKey: ["billing-invoices", activeOrgId],
    queryFn: () => listBillingInvoices(activeOrgId, 6),
    enabled: activeOrgId.length > 0 && Boolean(activeOrgFeatures.billing),
  });
  const accessReview = useQuery({
    queryKey: ["org-access-review", activeOrgId],
    queryFn: () => getOrgAccessReview(activeOrgId),
    enabled: activeOrgId.length > 0,
    refetchInterval: 30_000,
  });
  const projects = useQuery({
    queryKey: ["projects"],
    queryFn: listProjects,
  });
  const projectList = projects.data ?? [];
  const routeRef = useMemo(() => projectRefFromPathname(pathname), [pathname]);
  const activeProjectTab = useMemo(() => projectTabFromPathname(pathname), [pathname]);
  const activeConfigArea: ConfigArea =
    activeProjectTab === "auth" ? "auth" :
      activeProjectTab === "database" ? "database" :
        activeProjectTab === "functions" ? "functions" :
          activeProjectTab === "realtime" ? "realtime" :
            activeProjectTab === "storage" ? "storage" :
              configArea;
  const activeRef = routeRef || selectedRef || projectList[0]?.ref || "";
  const activeProjectListItem = useMemo(
    () => projectList.find((project) => project.ref === activeRef) ?? projectList[0],
    [projectList, activeRef],
  );
  const projectDetail = useQuery({
    queryKey: ["project", activeRef],
    queryFn: () => getProject(activeRef),
    enabled: activeRef.length > 0,
    refetchInterval: 15_000,
  });
  const activeProject = projectDetail.data ?? activeProjectListItem;
  const activeFeatureOrgId = activeProject?.org_id || activeOrgId;
  const activeProjectFeatures = useQuery({
    queryKey: ["org-features", activeFeatureOrgId],
    queryFn: () => getOrgFeatureFlags(activeFeatureOrgId),
    enabled: activeFeatureOrgId.length > 0,
  });
  const activeFeatureFlags = activeProjectFeatures.data?.effective ?? activeOrgFeatures;
  const connect = useQuery({
    queryKey: ["connect", activeRef],
    queryFn: () => getConnect(activeRef),
    enabled: activeRef.length > 0,
  });
  const cliProfile = useQuery({
    queryKey: ["cli-profile", activeRef],
    queryFn: () => getProjectCLIProfile(activeRef),
    enabled: activeRef.length > 0,
  });
  const projectMetrics = useQuery({
    queryKey: ["project-metrics", activeRef],
    queryFn: () => getProjectMetrics(activeRef),
    enabled: activeRef.length > 0,
    refetchInterval: 15_000,
  });
  const projectAccess = useQuery({
    queryKey: ["project-access", activeRef],
    queryFn: () => listProjectAccess(activeRef),
    enabled: activeRef.length > 0,
  });
  const routeManifest = useQuery({
    queryKey: ["project-route-manifest", activeRef],
    queryFn: () => getProjectRouteManifest(activeRef),
    enabled: activeRef.length > 0,
  });
  const domains = useQuery({
    queryKey: ["project-domains", activeRef],
    queryFn: () => listProjectDomains(activeRef),
    enabled: activeRef.length > 0,
  });
  const projectServices = useQuery({
    queryKey: ["project-services", activeRef],
    queryFn: () => getProjectServices(activeRef),
    enabled: activeRef.length > 0,
  });
  const projectConfig = useQuery({
    queryKey: ["project-config", activeRef, activeConfigArea],
    queryFn: () => getProjectConfig(activeRef, activeConfigArea),
    enabled: activeRef.length > 0,
  });
  const authProviderConfig = useQuery({
    queryKey: ["project-config", activeRef, "auth_providers"],
    queryFn: () => getProjectConfig(activeRef, "auth_providers"),
    enabled: activeRef.length > 0 && activeProjectTab === "auth",
  });
  const authEmailTemplatesConfig = useQuery({
    queryKey: ["project-config", activeRef, "email_templates"],
    queryFn: () => getProjectConfig(activeRef, "email_templates"),
    enabled: activeRef.length > 0 && activeProjectTab === "auth",
  });
  const authSMTPConfig = useQuery({
    queryKey: ["project-config", activeRef, "smtp"],
    queryFn: () => getProjectConfig(activeRef, "smtp"),
    enabled: activeRef.length > 0 && activeProjectTab === "auth",
  });
  const databasePoolerConfig = useQuery({
    queryKey: ["project-config", activeRef, "pooler"],
    queryFn: () => getProjectConfig(activeRef, "pooler"),
    enabled: activeRef.length > 0 && activeProjectTab === "database",
  });
  const projectBranches = useQuery({
    queryKey: ["project-branches", activeRef],
    queryFn: () => listProjectBranches(activeRef),
    enabled: activeRef.length > 0,
  });
  const projectReplicas = useQuery({
    queryKey: ["project-replicas", activeRef],
    queryFn: () => listProjectReplicas(activeRef),
    enabled: activeRef.length > 0,
  });
  const projectReplicaRouting = useQuery({
    queryKey: ["project-replica-routing", activeRef],
    queryFn: () => getProjectReplicaRouting(activeRef),
    enabled: activeRef.length > 0,
  });
  const projectFunctions = useQuery({
    queryKey: ["project-functions", activeRef],
    queryFn: () => listProjectFunctions(activeRef),
    enabled: activeRef.length > 0,
  });
  const functionRegions = useQuery({
    queryKey: ["function-regions", activeRef],
    queryFn: () => listProjectFunctionRegions(activeRef),
    enabled: activeRef.length > 0,
  });
  const functionStorageMounts = useQuery({
    queryKey: ["function-storage-mounts", activeRef],
    queryFn: () => listProjectFunctionStorageMounts(activeRef),
    enabled: activeRef.length > 0,
  });
  const authClients = useQuery({
    queryKey: ["auth-clients", activeRef],
    queryFn: () => listProjectAuthClients(activeRef),
    enabled: activeRef.length > 0,
  });
  const authHooks = useQuery({
    queryKey: ["auth-hooks", activeRef],
    queryFn: () => listProjectAuthHooks(activeRef),
    enabled: activeRef.length > 0,
  });
  const replicationPipelines = useQuery({
    queryKey: ["replication-pipelines", activeRef],
    queryFn: () => listProjectReplicationPipelines(activeRef),
    enabled: activeRef.length > 0,
  });
  const embeddingJobs = useQuery({
    queryKey: ["embedding-jobs", activeRef],
    queryFn: () => listProjectEmbeddingJobs(activeRef),
    enabled: activeRef.length > 0,
  });
  const databaseExtensions = useQuery({
    queryKey: ["database-extensions", activeRef],
    queryFn: () => listProjectDatabaseExtensions(activeRef),
    enabled: activeRef.length > 0,
  });
  const databaseCronJobs = useQuery({
    queryKey: ["database-cron-jobs", activeRef],
    queryFn: () => listProjectDatabaseCronJobs(activeRef),
    enabled: activeRef.length > 0,
  });
  const databaseQueues = useQuery({
    queryKey: ["database-queues", activeRef],
    queryFn: () => listProjectDatabaseQueues(activeRef),
    enabled: activeRef.length > 0,
  });
  const databaseWebhooks = useQuery({
    queryKey: ["database-webhooks", activeRef],
    queryFn: () => listProjectDatabaseWebhooks(activeRef),
    enabled: activeRef.length > 0,
  });
  const databaseSchemas = useQuery({
    queryKey: ["database-schemas", activeRef],
    queryFn: () => listProjectDatabaseSchemas(activeRef),
    enabled: activeRef.length > 0,
  });
  const databaseRoles = useQuery({
    queryKey: ["database-roles", activeRef],
    queryFn: () => listProjectDatabaseRoles(activeRef),
    enabled: activeRef.length > 0,
  });
  const storageBuckets = useQuery({
    queryKey: ["storage-buckets", activeRef],
    queryFn: () => listProjectStorageBuckets(activeRef),
    enabled: activeRef.length > 0,
  });
  const vectorBuckets = useQuery({
    queryKey: ["vector-buckets", activeRef],
    queryFn: () => listProjectVectorBuckets(activeRef),
    enabled: activeRef.length > 0,
  });
  const analyticsBuckets = useQuery({
    queryKey: ["analytics-buckets", activeRef],
    queryFn: () => listProjectAnalyticsBuckets(activeRef),
    enabled: activeRef.length > 0,
  });
  const cdnPolicy = useQuery({
    queryKey: ["cdn-policy", activeRef],
    queryFn: () => getProjectCDNPolicy(activeRef),
    enabled: activeRef.length > 0,
  });
  const cdnInvalidations = useQuery({
    queryKey: ["cdn-invalidations", activeRef],
    queryFn: () => listProjectCDNInvalidations(activeRef),
    enabled: activeRef.length > 0,
  });
  const networkConnections = useQuery({
    queryKey: ["network-connections", activeRef],
    queryFn: () => listProjectNetworkConnections(activeRef),
    enabled: activeRef.length > 0,
  });
  const networkPolicy = useQuery({
    queryKey: ["network-policy", activeRef],
    queryFn: () => getProjectNetwork(activeRef),
    enabled: activeRef.length > 0,
  });
  const logDrains = useQuery({
    queryKey: ["log-drains", activeRef],
    queryFn: () => listProjectLogDrains(activeRef),
    enabled: activeRef.length > 0,
  });
  const backups = useQuery({
    queryKey: ["backups", activeRef],
    queryFn: () => listBackups(activeRef),
    enabled: activeRef.length > 0,
  });
  const backupPolicy = useQuery({
    queryKey: ["backup-policy", activeRef],
    queryFn: () => getBackupPolicy(activeRef),
    enabled: activeRef.length > 0,
  });
  const recoverability = useQuery({
    queryKey: ["recoverability", activeRef],
    queryFn: () => getProjectRecoverability(activeRef),
    enabled: activeRef.length > 0,
  });
  const pitrPolicy = useQuery({
    queryKey: ["pitr-policy", activeRef],
    queryFn: () => getPITRPolicy(activeRef),
    enabled: activeRef.length > 0,
  });
  const walArchives = useQuery({
    queryKey: ["wal-archives", activeRef],
    queryFn: () => listWALArchives(activeRef),
    enabled: activeRef.length > 0,
  });
  const projectLogs = useQuery({
    queryKey: ["project-logs", activeRef],
    queryFn: () => listProjectLogs(activeRef),
    enabled: activeRef.length > 0,
    refetchInterval: 10_000,
  });
  const projectActivity = useQuery({
    queryKey: ["project-activity", activeRef],
    queryFn: () => listProjectActivity(activeRef),
    enabled: activeRef.length > 0,
    refetchInterval: 10_000,
  });
  const secrets = useQuery({
    queryKey: ["project-secrets", activeRef],
    queryFn: () => listProjectSecrets(activeRef),
    enabled: activeRef.length > 0,
  });
  useEffect(() => {
    if (routeRef) {
      setSelectedRef(routeRef);
    }
  }, [routeRef]);
  useEffect(() => {
    if (selectedTeamSlug && teams.data?.some((team) => team.slug === selectedTeamSlug) === false) {
      setSelectedTeamSlug("");
    }
  }, [selectedTeamSlug, teams.data]);
  const invalidateProject = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["projects"] });
    void queryClient.invalidateQueries({ queryKey: ["project", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-services", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["org-quota", activeOrgId] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", activeOrgId] });
    void queryClient.invalidateQueries({ queryKey: ["hosts"] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const pauseMutation = useMutation({
    mutationFn: pauseProject,
    onSuccess: (updated) => {
      setProjectCommandConfirm(null);
      invalidateProject(updated.ref);
      addToast({ title: "Project paused", detail: updated.ref });
    },
    onError: (error) => {
      addToast({ title: "Pause failed", detail: error.message, kind: "danger" });
    },
  });
  const resumeMutation = useMutation({
    mutationFn: resumeProject,
    onSuccess: (updated) => {
      setProjectCommandConfirm(null);
      invalidateProject(updated.ref);
      addToast({ title: "Project resumed", detail: updated.ref });
    },
    onError: (error) => {
      addToast({ title: "Resume failed", detail: error.message, kind: "danger" });
    },
  });
  const restartMutation = useMutation({
    mutationFn: restartProject,
    onSuccess: (updated) => {
      setProjectCommandConfirm(null);
      invalidateProject(updated.ref);
      addToast({ title: "Project restarted", detail: updated.ref });
    },
    onError: (error) => {
      addToast({ title: "Restart failed", detail: error.message, kind: "danger" });
    },
  });
  const triggerBackupMutation = useMutation({
    mutationFn: triggerBackup,
    onSuccess: (_backup, ref) => {
      void queryClient.invalidateQueries({ queryKey: ["backups", ref] });
      void queryClient.invalidateQueries({ queryKey: ["backup-policy", ref] });
      void queryClient.invalidateQueries({ queryKey: ["recoverability", ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
      void queryClient.invalidateQueries({ queryKey: ["org-usage", activeOrgId] });
      void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
      setProjectCommandConfirm(null);
      addToast({ title: "Backup triggered", detail: ref });
    },
    onError: (error) => {
      addToast({ title: "Backup failed", detail: error.message, kind: "danger" });
    },
  });
  const projectActionBusy = pauseMutation.isPending || resumeMutation.isPending || restartMutation.isPending;
  const projectCommandBusy = projectActionBusy || triggerBackupMutation.isPending;

  useEffect(() => {
    function onKeyDown(event: globalThis.KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        openPalette();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [openPalette]);

  const routeTo = (to: string, id?: string) => {
    void id;
    void navigate({ to });
  };
  const routeToProject = (ref: string, tab: ProjectTab = "overview") => {
    setSelectedRef(ref);
    const routeByTab: Record<ProjectTab, string> = {
      overview: "/projects/$ref",
      connect: "/projects/$ref/connect",
      auth: "/projects/$ref/auth",
      database: "/projects/$ref/database",
      storage: "/projects/$ref/storage",
      functions: "/projects/$ref/functions",
      realtime: "/projects/$ref/realtime",
      logs: "/projects/$ref/logs",
      backups: "/projects/$ref/backups",
      config: "/projects/$ref/config",
      activity: "/projects/$ref/activity",
    };
    void navigate({ to: routeByTab[tab], params: { ref } });
  };
  const paletteActions = useMemo<PaletteAction[]>(() => {
    const actions: PaletteAction[] = [
      { id: "nav-fleet", title: "Dashboard", subtitle: "At-a-glance health, server usage, and projects", group: "Navigation", icon: Activity, run: () => routeTo("/", "fleet-dashboard") },
      { id: "nav-orgs", title: "Organizations", subtitle: "Orgs, members, quotas, and usage", group: "Navigation", icon: UserPlus, run: () => routeTo("/organizations", "organizations") },
      { id: "nav-projects", title: "Projects list", subtitle: "Browse isolated stacks", group: "Navigation", icon: Database, run: () => routeTo("/projects", "projects-list") },
      { id: "nav-create-project", title: "Create project", subtitle: "Provision a new Supabase stack", group: "Navigation", icon: Plus, run: () => routeTo("/projects/new", "create-project") },
      { id: "nav-overview", title: "Project overview", subtitle: activeProject ? `${activeProject.ref} metrics, health, and connection basics` : "Project dashboard", group: "Navigation", icon: Activity, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref) },
      { id: "nav-connect", title: "Connect surface", subtitle: activeProject ? `${activeProject.ref} credentials and links` : "Project credentials and links", group: "Navigation", icon: KeyRound, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "connect") },
      { id: "nav-project-auth", title: "Project Auth", subtitle: "Clients, hooks, and project access", group: "Navigation", icon: Shield, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "auth") },
      { id: "nav-project-database", title: "Project Database", subtitle: "Branches, replicas, extensions, queues, roles, and AI", group: "Navigation", icon: Database, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "database") },
      { id: "nav-project-storage", title: "Project Storage", subtitle: "Buckets, CDN policy, and invalidations", group: "Navigation", icon: Boxes, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "storage") },
      { id: "nav-project-functions", title: "Project Functions", subtitle: "Deployments, regions, logs, and mounts", group: "Navigation", icon: Command, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "functions") },
      { id: "nav-project-realtime", title: "Project Realtime", subtitle: "Realtime configuration and service state", group: "Navigation", icon: RadioTower, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "realtime") },
      { id: "nav-project-logs", title: "Project Logs", subtitle: "Log tail and log drains", group: "Navigation", icon: Activity, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "logs") },
      { id: "nav-config", title: "Project configuration", subtitle: "Auth, providers, templates, storage, functions, realtime, network, SMTP", group: "Navigation", icon: SlidersHorizontal, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "config") },
      { id: "nav-backups", title: "Backups and PITR", subtitle: "Logical backups, restore runs, and WAL archive", group: "Navigation", icon: RotateCcw, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "backups") },
      { id: "nav-project-activity", title: "Project Activity", subtitle: "Per-project activity and audit trail", group: "Navigation", icon: Activity, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "activity") },
      { id: "nav-security", title: "Security", subtitle: "MFA, access review, and fleet advisor", group: "Navigation", icon: Shield, run: () => routeTo("/security", "security") },
      { id: "nav-hosts", title: "Hosts", subtitle: "Fleet capacity registration", group: "Navigation", icon: Server, run: () => routeTo("/hosts", "hosts") },
      { id: "nav-settings", title: "Settings", subtitle: "Platform defaults and configuration", group: "Navigation", icon: SlidersHorizontal, run: () => routeTo("/settings", "settings") },
      { id: "nav-audit", title: "Audit log", subtitle: "Immutable control-plane action history", group: "Navigation", icon: Activity, run: () => routeTo("/audit", "audit-log") },
    ];
    for (const project of projectList) {
      actions.push({
        id: `project-${project.ref}`,
        title: project.name,
        subtitle: `${project.ref} · ${project.status} · ${project.spec.resource_tier}`,
        group: "Projects",
        icon: Database,
        keywords: [project.ref, project.status, project.spec.profile, project.spec.stack_version],
        run: () => {
          routeToProject(project.ref);
        },
      });
    }
    if (activeProject) {
      actions.push(
        {
          id: "project-pause",
          title: "Pause active project",
          subtitle: activeProject.ref,
          group: "Lifecycle",
          icon: Pause,
          disabled: projectActionBusy || activeProject.status === "paused",
          run: () => setProjectCommandConfirm({ action: "pause", ref: activeProject.ref }),
        },
        {
          id: "project-resume",
          title: "Resume active project",
          subtitle: activeProject.ref,
          group: "Lifecycle",
          icon: Play,
          disabled: projectActionBusy || activeProject.status === "healthy",
          run: () => setProjectCommandConfirm({ action: "resume", ref: activeProject.ref }),
        },
        {
          id: "project-restart",
          title: "Restart active project",
          subtitle: activeProject.ref,
          group: "Lifecycle",
          icon: RotateCcw,
          disabled: projectActionBusy,
          run: () => setProjectCommandConfirm({ action: "restart", ref: activeProject.ref }),
        },
        {
          id: "project-trigger-backup",
          title: "Trigger active project backup",
          subtitle: activeProject.ref,
          group: "Operations",
          icon: RotateCcw,
          disabled: triggerBackupMutation.isPending,
          run: () => setProjectCommandConfirm({ action: "backup", ref: activeProject.ref }),
        },
      );
    }
    return actions;
  }, [activeProject, projectActionBusy, projectList, routeTo, routeToProject, triggerBackupMutation.isPending]);

  const onOrgCreated = (orgId: string) => {
    setSelectedOrgId(orgId);
    void navigate({ to: "/organizations" });
    void queryClient.invalidateQueries({ queryKey: ["orgs"] });
    void queryClient.invalidateQueries({ queryKey: ["org-members", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["org-quota", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage-snapshots", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["billing-invoices", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const onProjectCreated = (project: Project) => {
    setSelectedRef(project.ref);
    void navigate({ to: "/projects/$ref", params: { ref: project.ref } });
    void queryClient.invalidateQueries({ queryKey: ["projects"] });
    void queryClient.invalidateQueries({ queryKey: ["project", project.ref] });
    void queryClient.invalidateQueries({ queryKey: ["org-quota", activeOrgId] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", activeOrgId] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const onProjectDestroyed = () => {
    setSelectedRef("");
    void navigate({ to: "/projects" });
    void queryClient.invalidateQueries({ queryKey: ["org-quota", activeOrgId] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", activeOrgId] });
  };
  const pageTitle = pageTitleForPathname(pathname, activeProject, activeProjectTab);
  const pageScopeLabel = routeRef ? "Project workspace" : "Control plane";
  const account = useMemo(() => accountSummaryFromToken(token), [token]);
  const apiStatusLabel = apiHealth.isError ? "API surface unreachable" : apiHealth.isLoading ? "API surface checking" : "API surface online";
  const apiStatusClass = apiHealth.isError ? "bg-danger" : apiHealth.isLoading ? "bg-warning" : "bg-success";
  const contextValue: DashboardContextValue = {
    activeOrgId,
    activeTeamSlug,
    activeRef,
    activeProject,
    activeProjectTab,
    configArea,
    projectList,
    setConfigArea,
    setSelectedOrgId,
    setSelectedTeamSlug,
    routeToProject,
    onOrgCreated,
    onProjectCreated,
    onProjectDestroyed,
    orgs,
    hosts,
    fleetMetrics,
    advisorFindings,
    complianceReport,
    provisionerStatus,
    runtimeConfig,
    platformDefaults,
    platformSSO,
    backupStorageTargets,
    platformBackups,
    scimServiceProviderConfig,
    scimUsers,
    scimGroups,
    auditEvents,
    auditIntegrity,
    users,
    mfaStatus,
    members,
    teams,
    teamMembers,
    orgFeatures,
    activeFeatureFlags,
    quota,
    usage,
    usageSnapshots,
    billingInvoices,
    accessReview,
    projects,
    connect,
    cliProfile,
    projectMetrics,
    projectAccess,
    routeManifest,
    domains,
    projectServices,
    projectConfig,
    authProviderConfig,
    authEmailTemplatesConfig,
    authSMTPConfig,
    databasePoolerConfig,
    projectBranches,
    projectReplicas,
    projectReplicaRouting,
    projectFunctions,
    functionRegions,
    functionStorageMounts,
    authClients,
    authHooks,
    replicationPipelines,
    embeddingJobs,
    databaseExtensions,
    databaseCronJobs,
    databaseQueues,
    databaseWebhooks,
    databaseSchemas,
    databaseRoles,
    storageBuckets,
    vectorBuckets,
    analyticsBuckets,
    cdnPolicy,
    cdnInvalidations,
    networkPolicy,
    networkConnections,
    logDrains,
    backups,
    backupPolicy,
    recoverability,
    pitrPolicy,
    walArchives,
    projectLogs,
    projectActivity,
    secrets,
  };
  const commandProject =
    projectCommandConfirm?.ref === activeProject?.ref
      ? activeProject
      : projectList.find((project) => project.ref === projectCommandConfirm?.ref);
  const commandCopy = projectCommandConfirm ? projectCommandConfirmCopy(projectCommandConfirm.action, commandProject) : null;

  function runConfirmedProjectCommand() {
    if (!projectCommandConfirm) return;
    switch (projectCommandConfirm.action) {
      case "pause":
        pauseMutation.mutate(projectCommandConfirm.ref);
        return;
      case "resume":
        resumeMutation.mutate(projectCommandConfirm.ref);
        return;
      case "restart":
        restartMutation.mutate(projectCommandConfirm.ref);
        return;
      case "backup":
        triggerBackupMutation.mutate(projectCommandConfirm.ref);
        return;
    }
  }

  return (
    <DashboardContext.Provider value={contextValue}>
      <main className="min-h-screen bg-bg text-text">
        <CommandPalette actions={paletteActions} />
        <ToastHost />
        <Modal
          description={commandCopy?.description}
          footer={
            <>
              <button className="button secondary" disabled={projectCommandBusy} onClick={() => setProjectCommandConfirm(null)} type="button">
                Cancel
              </button>
              <button className={commandCopy?.tone === "warning" ? "button danger" : "button"} disabled={projectCommandBusy} onClick={runConfirmedProjectCommand} type="button">
                {projectCommandBusy ? "Working..." : commandCopy?.confirmLabel ?? "Confirm"}
              </button>
            </>
          }
          onClose={() => !projectCommandBusy && setProjectCommandConfirm(null)}
          open={Boolean(projectCommandConfirm)}
          title={commandCopy?.title ?? "Confirm project command"}
        >
          <div className="grid gap-3 text-sm text-muted">
            <p>{commandCopy?.body}</p>
            <div className="rounded-md border border-border bg-bg p-3">
              <p className="label">Project</p>
              <p className="mt-1 truncate text-sm font-medium text-text">{commandProject?.name ?? projectCommandConfirm?.ref}</p>
              <p className="mt-1 truncate font-mono text-xs text-faint">{projectCommandConfirm?.ref}</p>
            </div>
          </div>
        </Modal>
        <div className="grid min-h-screen grid-cols-[248px_1fr] max-lg:grid-cols-1">
          <Sidebar projectMode={Boolean(routeRef)} />
          <section className="min-w-0 border-l border-border max-lg:border-l-0">
            <header className="flex h-14 items-center justify-between border-b border-border px-6">
              <div>
                <p className="label">{pageScopeLabel}</p>
                <h1 className="text-[18px] font-medium">{pageTitle}</h1>
              </div>
              <div className="flex items-center gap-2 text-sm text-muted">
                <span className={`status-dot ${apiStatusClass}`} />
                {apiStatusLabel}
                <button className="icon-button h-8 min-h-8 min-w-8" onClick={openPalette} title="Open command palette" type="button">
                  <Command size={14} />
                </button>
                <AccountMenu account={account} onLogout={onLogout} onRoute={routeTo} onThemeChange={setTheme} theme={theme} />
              </div>
            </header>
            <div className="grid gap-6 p-6" id={panelIdForPathname(pathname)}>
              <Outlet />
            </div>
          </section>
        </div>
      </main>
    </DashboardContext.Provider>
  );
}

type AccountSummary = {
  email: string;
  role: string;
  shortName: string;
  initials: string;
};

function AccountMenu({
  account,
  onLogout,
  onRoute,
  onThemeChange,
  theme,
}: {
  account: AccountSummary;
  onLogout: () => void;
  onRoute: (to: string, id?: string) => void;
  onThemeChange: (theme: "dark" | "light") => void;
  theme: "dark" | "light";
}) {
  const [open, setOpen] = useState(false);

  function navigate(to: string, id: string) {
    setOpen(false);
    onRoute(to, id);
  }

  return (
    <div className="relative">
      <button className="nav-item h-9 gap-2 border border-border bg-surface px-2 pr-3" onClick={() => setOpen((value) => !value)} type="button">
        <span className="grid h-6 w-6 place-items-center rounded-full border border-border-strong bg-surface-2 font-mono text-[11px] font-semibold text-text">{account.initials}</span>
        <span className="max-w-[150px] truncate text-sm text-text">{account.shortName}</span>
      </button>
      {open ? (
        <div className="absolute right-0 top-11 z-50 w-72 rounded-lg border border-border bg-surface p-2 shadow-[0_18px_40px_rgba(0,0,0,.45)]">
          <div className="border-b border-border px-2 pb-3 pt-1">
            <div className="flex items-center gap-3">
              <span className="grid h-9 w-9 place-items-center rounded-full border border-border-strong bg-surface-2 font-mono text-sm font-semibold text-text">{account.initials}</span>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{account.email}</p>
                <p className="mt-0.5 truncate text-xs text-muted">{account.role}</p>
              </div>
            </div>
          </div>
          <div className="mt-2 grid gap-1">
            <div className="grid grid-cols-2 gap-1 rounded-md border border-border bg-bg p-1">
              <button
                className={theme === "dark" ? "segmented active h-8" : "segmented h-8"}
                onClick={() => onThemeChange("dark")}
                type="button"
              >
                <Moon size={14} />
                Dark
              </button>
              <button
                className={theme === "light" ? "segmented active h-8" : "segmented h-8"}
                onClick={() => onThemeChange("light")}
                type="button"
              >
                <Sun size={14} />
                Light
              </button>
            </div>
            <button className="nav-item h-9 justify-start" onClick={() => navigate("/security", "security")} type="button">
              <UserCircle size={14} />
              Profile settings
            </button>
            <button className="nav-item h-9 justify-start" onClick={() => navigate("/security", "security")} type="button">
              <Shield size={14} />
              Account security
            </button>
            <button className="nav-item h-9 justify-start" onClick={() => navigate("/settings", "settings")} type="button">
              <SlidersHorizontal size={14} />
              Platform settings
            </button>
            <button className="nav-item h-9 justify-start text-danger hover:text-danger" onClick={() => {
              setOpen(false);
              onLogout();
            }} type="button">
              <LogOut size={14} />
              Logout
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function accountSummaryFromToken(token: string | null): AccountSummary {
  const claims = tokenClaims(token);
  const email = typeof claims.email === "string" && claims.email ? claims.email : "admin";
  const role = typeof claims.role === "string" && claims.role ? claims.role : "admin";
  const shortName = email.includes("@") ? email.split("@")[0] : email;
  const initials = shortName
    .split(/[._\-\s]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("") || "A";
  return { email, role, shortName, initials };
}

function projectCommandConfirmCopy(action: ProjectCommandConfirm["action"], project?: Project) {
  const ref = project?.ref ?? "this project";
  switch (action) {
    case "pause":
      return {
        title: "Pause project?",
        description: "This stops the project data-plane containers while preserving desired state and volumes.",
        body: `Pause ${ref}. Apps using this project will lose access until it is resumed.`,
        confirmLabel: "Pause project",
        tone: "warning" as const,
      };
    case "resume":
      return {
        title: "Resume project?",
        description: "This starts the rendered project stack and reconciles it back to the desired state.",
        body: `Resume ${ref}. Services may take a short time to report healthy after the command starts.`,
        confirmLabel: "Resume project",
        tone: "default" as const,
      };
    case "restart":
      return {
        title: "Restart project?",
        description: "This restarts the project stack without changing configuration.",
        body: `Restart ${ref}. Active database, API, realtime, storage, and function connections may briefly disconnect.`,
        confirmLabel: "Restart project",
        tone: "warning" as const,
      };
    case "backup":
      return {
        title: "Trigger backup?",
        description: "This creates a logical backup artifact through the configured backup runner.",
        body: `Trigger a new backup for ${ref}. The operation is recorded in project activity and the audit log.`,
        confirmLabel: "Trigger backup",
        tone: "default" as const,
      };
  }
}

function tokenClaims(token: string | null): Record<string, unknown> {
  if (!token) {
    return {};
  }
  const parts = token.split(".");
  for (const payload of parts.length >= 3 ? [parts[1], parts[0]] : [parts[0]]) {
    if (!payload) {
      continue;
    }
    const decoded = decodeBase64URLJSON(payload);
    if (decoded) {
      return decoded;
    }
  }
  return {};
}

function decodeBase64URLJSON(payload: string): Record<string, unknown> | null {
  try {
    const normalized = payload.replace(/-/g, "+").replace(/_/g, "/");
    const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "=");
    return JSON.parse(window.atob(padded)) as Record<string, unknown>;
  } catch {
    return null;
  }
}

function ToastHost() {
  const toasts = useUIStore((state) => state.toasts);
  const removeToast = useUIStore((state) => state.removeToast);
  return (
    <div aria-live="polite" className="toast-host">
      {toasts.map((toast) => (
        <button className={`toast ${toast.kind ?? "success"}`} key={toast.id} onClick={() => removeToast(toast.id)} type="button">
          <CheckCircle2 size={15} />
          <span className="min-w-0">
            <span className="block truncate text-sm font-medium">{toast.title}</span>
            {toast.detail ? <span className="mt-0.5 block truncate text-xs text-muted">{toast.detail}</span> : null}
          </span>
        </button>
      ))}
    </div>
  );
}

function CommandPalette({ actions }: { actions: PaletteAction[] }) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [activeIndex, setActiveIndex] = useState(0);
  const open = useUIStore((state) => state.paletteOpen);
  const query = useUIStore((state) => state.paletteQuery);
  const setQuery = useUIStore((state) => state.setPaletteQuery);
  const closePalette = useUIStore((state) => state.closePalette);
  const matches = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) {
      return actions.slice(0, 12);
    }
    return actions
      .filter((action) => {
        const haystack = [action.title, action.subtitle, action.group, ...(action.keywords ?? [])].join(" ").toLowerCase();
        return haystack.includes(normalized);
      })
      .slice(0, 12);
  }, [actions, query]);
  const runnableMatches = matches.filter((action) => !action.disabled);

  useEffect(() => {
    if (!open) {
      return;
    }
    setActiveIndex(0);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  }, [open, query]);

  if (!open) {
    return null;
  }

  function runAction(action: PaletteAction) {
    if (action.disabled) {
      return;
    }
    action.run();
    closePalette();
  }

  function onInputKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      closePalette();
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActiveIndex((index) => Math.min(index + 1, Math.max(matches.length - 1, 0)));
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setActiveIndex((index) => Math.max(index - 1, 0));
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      const selected = matches[activeIndex] ?? runnableMatches[0];
      if (selected) {
        runAction(selected);
      }
    }
  }

  return (
    <div className="palette-backdrop" onMouseDown={closePalette} role="presentation">
      <section aria-label="Command palette" aria-modal="true" className="palette-dialog" onMouseDown={(event) => event.stopPropagation()} role="dialog">
        <div className="palette-search">
          <Search size={16} />
          <input
            aria-label="Search commands"
            autoComplete="off"
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={onInputKeyDown}
            placeholder="Search commands, projects, actions"
            ref={inputRef}
            value={query}
          />
        </div>
        <div className="palette-list" role="listbox">
          {matches.length === 0 ? (
            <p className="palette-empty">No matching commands.</p>
          ) : (
            matches.map((action, index) => {
              const Icon = action.icon;
              const active = index === activeIndex;
              return (
                <button
                  aria-selected={active}
                  className={action.disabled ? "palette-item disabled" : active ? "palette-item active" : "palette-item"}
                  disabled={action.disabled}
                  key={action.id}
                  onMouseEnter={() => setActiveIndex(index)}
                  onClick={() => runAction(action)}
                  role="option"
                  type="button"
                >
                  <span className="palette-icon">
                    <Icon size={15} />
                  </span>
                  <span className="min-w-0">
                    <span className="palette-title">{action.title}</span>
                    <span className="palette-subtitle">{action.group} · {action.subtitle}</span>
                  </span>
                </button>
              );
            })
          )}
        </div>
      </section>
    </div>
  );
}

function Sidebar({ projectMode }: { projectMode: boolean }) {
  const { activeProject, activeProjectTab, activeRef } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const projectSections = useUIStore((state) => state.projectSections);
  if (projectMode) {
    const ref = activeProject?.ref ?? activeRef;
    const routeByTab: Record<ProjectTab, string> = {
      overview: "/projects/$ref",
      connect: "/projects/$ref/connect",
      auth: "/projects/$ref/auth",
      database: "/projects/$ref/database",
      storage: "/projects/$ref/storage",
      functions: "/projects/$ref/functions",
      realtime: "/projects/$ref/realtime",
      logs: "/projects/$ref/logs",
      backups: "/projects/$ref/backups",
      config: "/projects/$ref/config",
      activity: "/projects/$ref/activity",
    };

    return (
      <aside className="flex min-h-screen flex-col bg-bg px-4 py-4 max-lg:min-h-0">
        <div className="flex h-10 items-center gap-3 px-2">
          <div className="grid h-10 w-10 place-items-center rounded-md border border-border-strong bg-surface-2">
            <BrandLogo className="h-7 w-7 text-accent" />
          </div>
          <BrandWordmark />
        </div>
        <div className="mt-5 rounded-lg border border-border bg-surface p-3">
          <Link className="nav-item mb-3 h-8 px-2" to="/projects">
            <ArrowLeft size={14} />
            Projects
          </Link>
          <p className="label">Project workspace</p>
          <h2 className="mt-1 truncate text-base font-medium">{activeProject?.name ?? ref}</h2>
          <p className="mt-1 truncate font-mono text-xs text-muted">{ref}</p>
          <div className="mt-3 flex flex-wrap gap-1">
            <span className={`pill ${activeProject?.status === "healthy" ? "healthy" : activeProject?.status === "paused" ? "provisioning" : ""}`}>{activeProject?.status ?? "loading"}</span>
            {activeProject ? <span className="pill">{activeProject.spec.resource_tier}</span> : null}
            {activeProject ? <span className="pill">{activeProject.spec.profile}</span> : null}
          </div>
        </div>
        <nav className="mt-4 grid gap-1">
          {projectTabs.map((tab) => {
            const Icon = tab.icon;
            const active = activeProjectTab === tab.id;
            const subnav = active ? projectSubnav[tab.id] : undefined;
            const activeSection = active ? projectSectionFromPathname(pathname) : projectSections[tab.id] ?? "overview";
            const tabSuffix = tab.suffix;
            return (
              <div className="grid gap-1" key={tab.id}>
                <Link activeOptions={{ exact: tab.id === "overview" }} activeProps={{ className: "nav-item active" }} className={`nav-item ${active ? "active" : ""}`} params={{ ref }} to={routeByTab[tab.id]}>
                  <Icon size={15} />
                  {tab.label}
                </Link>
                {subnav ? (
                  <div className="project-subnav">
                    {subnav.map((item) => (
                      <Link activeOptions={{ exact: true }} className={`project-subnav-item ${activeSection === item.id ? "active" : ""}`} key={item.id} title={item.description} to={item.id === "overview" ? routeByTab[tab.id] : `/projects/${ref}/${tabSuffix}/${item.id}`}>
                        {item.label}
                      </Link>
                    ))}
                  </div>
                ) : null}
              </div>
            );
          })}
        </nav>
      </aside>
    );
  }

  const navItems: Array<{ label: string; icon: LucideIcon; to: string }> = [
    { label: "Dashboard", icon: Activity, to: "/" },
    { label: "Organizations", icon: UserPlus, to: "/organizations" },
    { label: "Projects", icon: Database, to: "/projects" },
    { label: "Security", icon: Shield, to: "/security" },
    { label: "Hosts", icon: Server, to: "/hosts" },
    { label: "Settings", icon: SlidersHorizontal, to: "/settings" },
    { label: "Audit", icon: Activity, to: "/audit" },
  ];

  return (
    <aside className="flex min-h-screen flex-col bg-bg px-4 py-4 max-lg:min-h-0">
      <div className="flex h-10 items-center gap-3 px-2">
        <div className="grid h-10 w-10 place-items-center rounded-md border border-border-strong bg-surface-2">
          <BrandLogo className="h-7 w-7 text-accent" />
        </div>
        <BrandWordmark />
      </div>
      <nav className="mt-6 grid gap-1">
        {navItems.map(({ label, icon: Icon, to }) => {
          const active = pathname === to ||
            (to === "/settings" && pathname.startsWith("/settings/")) ||
            (to === "/organizations" && pathname.startsWith("/organizations/")) ||
            (to === "/security" && pathname.startsWith("/security/"));
          return (
            <div className="grid gap-1" key={label}>
              <Link activeOptions={{ exact: true }} activeProps={{ className: "nav-item active" }} className={`nav-item ${active ? "active" : ""}`} to={to}>
                <Icon size={15} />
                {label}
              </Link>
              {label === "Organizations" && active ? (
                <div className="project-subnav">
                  {organizationSections.map((item) => {
                    const activeSection = pathname.match(/^\/organizations\/([^/]+)/)?.[1] ?? "overview";
                    return (
                      item.id === "overview" ? (
                        <Link activeOptions={{ exact: true }} className={`project-subnav-item ${activeSection === item.id ? "active" : ""}`} key={item.id} title={item.description} to="/organizations">
                          {item.label}
                        </Link>
                      ) : (
                        <Link activeOptions={{ exact: true }} className={`project-subnav-item ${activeSection === item.id ? "active" : ""}`} key={item.id} params={{ section: item.id }} title={item.description} to="/organizations/$section">
                          {item.label}
                        </Link>
                      )
                    );
                  })}
                </div>
              ) : null}
              {label === "Settings" && active ? (
                <div className="project-subnav">
                  {platformSettingsSections.map((item) => {
                    const activeSection = pathname.match(/^\/settings\/([^/]+)/)?.[1] ?? "overview";
                    return (
                      item.id === "overview" ? (
                        <Link activeOptions={{ exact: true }} className={`project-subnav-item ${activeSection === item.id ? "active" : ""}`} key={item.id} title={item.description} to="/settings">
                          {item.label}
                        </Link>
                      ) : (
                        <Link activeOptions={{ exact: true }} className={`project-subnav-item ${activeSection === item.id ? "active" : ""}`} key={item.id} params={{ section: item.id }} title={item.description} to="/settings/$section">
                          {item.label}
                        </Link>
                      )
                    );
                  })}
                </div>
              ) : null}
              {label === "Security" && active ? (
                <div className="project-subnav">
                  {securitySections.map((item) => {
                    const activeSection = pathname.match(/^\/security\/([^/]+)/)?.[1] ?? "overview";
                    return (
                      item.id === "overview" ? (
                        <Link activeOptions={{ exact: true }} className={`project-subnav-item ${activeSection === item.id ? "active" : ""}`} key={item.id} title={item.description} to="/security">
                          {item.label}
                        </Link>
                      ) : (
                        <Link activeOptions={{ exact: true }} className={`project-subnav-item ${activeSection === item.id ? "active" : ""}`} key={item.id} params={{ section: item.id }} title={item.description} to="/security/$section">
                          {item.label}
                        </Link>
                      )
                    );
                  })}
                </div>
              ) : null}
            </div>
          );
        })}
      </nav>
    </aside>
  );
}
