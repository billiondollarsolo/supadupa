import { KeyboardEvent, useEffect, useId, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { Activity, AlertTriangle, ArrowLeft, CheckCircle2, Command, Database, Info, KeyRound, LogOut, Moon, Pause, Play, Plus, RotateCcw, Search, Server, Shield, SlidersHorizontal, Sun, UserCircle, UserPlus, XCircle, type LucideIcon } from "lucide-react";
import {
  createPlatformUser,
  getAuditIntegrity,
  getAccountMFA,
  getApiHealth,
  getAuthState,
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
  listWALArchives,
  listProjectDomains,
  listProjectBranches,
  listProjectCDNInvalidations,
  listProjectLogDrains,
  listProjectNetworkConnections,
  listProjectReplicas,
  listProjectReplicationPipelines,
  getProjectRouteManifest,
  changeAccountPassword,
  listProjectActivity,
  listProjectLogs,
  listProjects,
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
import { BuiltByFooter } from "./components/built-by-footer";
import { Breadcrumbs, buildBreadcrumbs } from "./components/breadcrumbs";
import { useAuthSession } from "./lib/auth-session";
import { DashboardContext, useDashboardContext, type DashboardContextValue } from "./lib/dashboard-context";
import { focusableElements, makeBackgroundInert } from "./lib/focus";
import { organizationSections, platformSettingsSections, projectSubnav, projectTabs, securitySections, type ConfigArea, type ProjectTab } from "./lib/project-config";
import { projectPath, projectRefFromPathname, projectRouteByTab, projectSectionFromPathname, projectTabFromPathname } from "./lib/routes";
import { useUIStore } from "./lib/ui-store";
import type { AdvisorFinding, AuditEvent, AuditIntegrity, Backup, BackupPolicy, BackupStorageTarget, BillingInvoice, CDNInvalidation, ComplianceReport, ConnectPayload, FleetMetrics, Host, LogDrain, MFAStatus, Membership, Org, OrgAccessReview, OrgFeatureFlags, OrgQuota, OrgUsage, PITRPolicy, PlatformDefaults, PlatformSSOConfig, Project, ProjectAccessGrant, ProjectAnalyticsBucket, ProjectAuthClient, ProjectAuthHook, ProjectBranch, ProjectCDNPolicy, ProjectCLIProfile, ProjectConfig, ProjectDatabaseCronJob, ProjectDatabaseExtension, ProjectDatabaseQueue, ProjectDatabaseRole, ProjectDatabaseSchema, ProjectDatabaseWebhook, ProjectDomain, ProjectEmbeddingJob, ProjectFunction, ProjectFunctionRegion, ProjectFunctionStorageMount, ProjectLog, ProjectMetrics, ProjectNetworkConnection, ProjectNetworkPolicy, ProjectReplica, ProjectReplicaRouting, ProjectReplicationPipeline, ProjectServices, ProjectStorageBucket, ProjectVectorBucket, ProvisionerStatus, RuntimeConfig, SCIMGroup, SCIMListResponse, SCIMServiceProviderConfig, SCIMUser, Team, TeamMember, UsageSnapshot, User, WALArchive } from "./types";
import { Modal } from "./components/modal";
import { StatusPill } from "./components/ui/status-pill";
import { Button } from "./components/ui/button";
import { Badge } from "./components/ui/badge";
import { Input } from "./components/ui/input";

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

function panelIdForPathname(pathname: string) {
  if (pathname === "/organizations" || pathname.startsWith("/organizations/")) return "organizations";
  if (pathname === "/projects") return "projects-list";
  if (pathname === "/projects/new") return "create-project";
  if (pathname.startsWith("/projects/")) return `project-${projectTabFromPathname(pathname)}`;
  if (pathname === "/security" || pathname.startsWith("/security/")) return "security";
  if (pathname === "/hosts") return "hosts";
  if (pathname === "/settings" || pathname.startsWith("/settings/")) return "settings";
  if (pathname === "/audit") return "audit-log";
  if (pathname === "/about") return "about";
  return "fleet-dashboard";
}

function pageTitleForPathname(pathname: string, activeProject: Project | undefined, activeProjectTab: ProjectTab, orgsEnabled: boolean) {
  if (pathname === "/organizations" || pathname.startsWith("/organizations/")) return orgsEnabled ? "Organizations" : "Access";
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
  if (pathname === "/about") return "About";
  return "Dashboard";
}

export function App() {
  const user = useAuthSession((state) => state.user);
  const setAuthenticated = useAuthSession((state) => state.setAuthenticated);
  const setUnauthenticated = useAuthSession((state) => state.setUnauthenticated);
  const logout = useAuthSession((state) => state.logout);
  const theme = useUIStore((state) => state.theme);
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const authState = useQuery({ queryKey: ["auth-state"], queryFn: ({ signal }) => getAuthState({ signal }), retry: 1, refetchInterval: 30_000 });

  useEffect(() => {
    document.title = "SUPADUPA";
  }, [pathname]);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  useEffect(() => {
    if (!authState.data) {
      return;
    }
    if (authState.data.authenticated && authState.data.user) {
      setAuthenticated(authState.data.user);
      return;
    }
    setUnauthenticated();
  }, [authState.data, setAuthenticated, setUnauthenticated]);

  useEffect(() => {
    // Drive redirects off the resolved auth-state response, NOT the derived
    // `user` store. The store is populated by a separate effect, so on a hard
    // refresh there is a render where auth-state has resolved authenticated but
    // `user` is still null — keying on `user` there would bounce a deep page to
    // /login and then to / (home). Keying on authState.data avoids that race.
    if (authState.isLoading || !authState.data) {
      return;
    }
    if (!authState.data.authenticated && pathname !== "/login") {
      void navigate({ to: "/login" });
    }
    if (authState.data.authenticated && pathname === "/login") {
      void navigate({ to: "/" });
    }
  }, [authState.isLoading, authState.data, navigate, pathname]);

  if (authState.isLoading) {
    return <main className="min-h-screen bg-bg text-text" />;
  }

  if (!user) {
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
  const user = useAuthSession((state) => state.user);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const openPalette = useUIStore((state) => state.openPalette);
  const paletteOpen = useUIStore((state) => state.paletteOpen);
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

  const routeRef = useMemo(() => projectRefFromPathname(pathname), [pathname]);
  const activeProjectTab = useMemo(() => projectTabFromPathname(pathname), [pathname]);
  const isFleetRoute = pathname === "/";
  const isOrganizationsRoute = pathname === "/organizations" || pathname.startsWith("/organizations/");
  const isProjectsRoute = pathname === "/projects" || pathname === "/projects/new";
  const isHostsRoute = pathname === "/hosts" || pathname.startsWith("/hosts/");
  const isSettingsRoute = pathname === "/settings" || pathname.startsWith("/settings/");
  const isSecurityRoute = pathname === "/security" || pathname.startsWith("/security/");
  const isAuditRoute = pathname === "/audit";
  const hasProjectRoute = routeRef.length > 0;
  const needsHosts = isFleetRoute || isProjectsRoute || isHostsRoute || isSettingsRoute || (hasProjectRoute && activeProjectTab === "database");
  const needsFleetPosture = isFleetRoute || isSecurityRoute;
  const needsAuditTrail = isAuditRoute;
  const needsPlatformSettings = isSettingsRoute;
  const needsOrgWorkspace = isOrganizationsRoute || isSecurityRoute;
  const needsProjectOverview = hasProjectRoute && activeProjectTab === "overview";
  const needsProjectConnect = hasProjectRoute && activeProjectTab === "connect";
  const needsProjectAccess = hasProjectRoute && activeProjectTab === "access";
  const needsProjectDatabase = hasProjectRoute && activeProjectTab === "database";
  const needsProjectLogs = hasProjectRoute && activeProjectTab === "logs";
  const needsProjectBackups = hasProjectRoute && activeProjectTab === "backups";
  const needsProjectConfig = hasProjectRoute && activeProjectTab === "config";
  const needsProjectActivity = hasProjectRoute && activeProjectTab === "activity";

  const apiHealth = useQuery({ queryKey: ["api-health"], queryFn: getApiHealth, refetchInterval: 30_000 });
  const orgs = useQuery({ queryKey: ["orgs"], queryFn: listOrgs });
  const hosts = useQuery({ queryKey: ["hosts"], queryFn: listHosts, enabled: needsHosts });
  const fleetMetrics = useQuery({ queryKey: ["fleet-metrics"], queryFn: getFleetMetrics, enabled: needsFleetPosture, refetchInterval: needsFleetPosture ? 15_000 : false });
  const advisorFindings = useQuery({ queryKey: ["advisor-findings"], queryFn: getAdvisorFindings, enabled: needsFleetPosture, refetchInterval: needsFleetPosture ? 30_000 : false });
  const complianceReport = useQuery({ queryKey: ["compliance-report"], queryFn: getComplianceReport, enabled: needsFleetPosture, refetchInterval: needsFleetPosture ? 30_000 : false });
  const provisionerStatus = useQuery({ queryKey: ["provisioner-status"], queryFn: getProvisionerStatus });
  const runtimeConfig = useQuery({ queryKey: ["runtime-config"], queryFn: getRuntimeConfig, refetchInterval: 30_000 });
  // Always loaded: platform feature flags gate top-level nav (orgs, SSO/SCIM).
  const platformDefaults = useQuery({ queryKey: ["platform-defaults"], queryFn: getPlatformDefaults });
  const platformFlags = platformDefaults.data?.feature_flags ?? {};
  const orgsEnabled = Boolean(platformFlags.multi_org);
  const ssoScimEnabled = Boolean(platformFlags.platform_sso_scim);
  // Resource quotas are an enterprise/scale concern (cap a tenant, or guard a
  // node from overcommit). Off by default — hidden entirely in single-node mode.
  const quotasEnabled = Boolean(platformFlags.resource_quotas);
  const platformSSO = useQuery({ queryKey: ["platform-sso"], queryFn: getPlatformSSOConfig, enabled: needsPlatformSettings });
  const backupStorageTargets = useQuery({ queryKey: ["backup-storage-targets"], queryFn: listBackupStorageTargets, enabled: needsPlatformSettings || needsProjectBackups });
  const platformBackups = useQuery({ queryKey: ["platform-backups"], queryFn: listPlatformBackups, enabled: needsPlatformSettings });
  const scimServiceProviderConfig = useQuery({ queryKey: ["scim-service-provider-config"], queryFn: getSCIMServiceProviderConfig, enabled: needsPlatformSettings });
  const scimUsers = useQuery({ queryKey: ["scim-users"], queryFn: listSCIMUsers, enabled: needsPlatformSettings });
  const scimGroups = useQuery({ queryKey: ["scim-groups"], queryFn: () => listSCIMGroups(), enabled: needsPlatformSettings });
  // The full audit page (AuditLogPage) owns its own server-paginated query; the
  // dashboard context only needs the integrity badge.
  const auditIntegrity = useQuery({ queryKey: ["audit-integrity"], queryFn: getAuditIntegrity, enabled: needsAuditTrail, refetchInterval: needsAuditTrail ? 10_000 : false });
  const users = useQuery({ queryKey: ["users"], queryFn: listUsers, enabled: needsPlatformSettings || isSecurityRoute || isOrganizationsRoute || isAuditRoute });
  const mfaStatus = useQuery({ queryKey: ["account-mfa"], queryFn: getAccountMFA, enabled: isSecurityRoute });
  const activeOrgId = selectedOrgId || orgs.data?.[0]?.id || "";
  const members = useQuery({
    queryKey: ["org-members", activeOrgId],
    queryFn: () => listOrgMembers(activeOrgId),
    enabled: needsOrgWorkspace && activeOrgId.length > 0,
  });
  const teams = useQuery({
    queryKey: ["org-teams", activeOrgId],
    queryFn: () => listOrgTeams(activeOrgId),
    enabled: (needsOrgWorkspace || needsProjectAccess) && activeOrgId.length > 0,
  });
  const activeTeamSlug = selectedTeamSlug || teams.data?.[0]?.slug || "";
  const teamMembers = useQuery({
    queryKey: ["team-members", activeOrgId, activeTeamSlug],
    queryFn: () => listTeamMembers(activeOrgId, activeTeamSlug),
    enabled: isOrganizationsRoute && activeOrgId.length > 0 && activeTeamSlug.length > 0,
  });
  const orgFeatures = useQuery({
    queryKey: ["org-features", activeOrgId],
    queryFn: () => getOrgFeatureFlags(activeOrgId),
    enabled: (needsOrgWorkspace || pathname === "/projects/new") && activeOrgId.length > 0,
  });
  const activeOrgFeatures = orgFeatures.data?.effective ?? {};
  const quota = useQuery({
    queryKey: ["org-quota", activeOrgId],
    queryFn: () => getOrgQuota(activeOrgId),
    enabled: isOrganizationsRoute && activeOrgId.length > 0,
  });
  const usage = useQuery({
    queryKey: ["org-usage", activeOrgId],
    queryFn: () => getOrgUsage(activeOrgId),
    enabled: isOrganizationsRoute && activeOrgId.length > 0,
    refetchInterval: isOrganizationsRoute ? 15_000 : false,
  });
  const usageSnapshots = useQuery({
    queryKey: ["org-usage-snapshots", activeOrgId],
    queryFn: () => listOrgUsageSnapshots(activeOrgId, 6),
    enabled: isOrganizationsRoute && activeOrgId.length > 0 && Boolean(activeOrgFeatures.usage_metering),
  });
  const billingInvoices = useQuery({
    queryKey: ["billing-invoices", activeOrgId],
    queryFn: () => listBillingInvoices(activeOrgId, 6),
    enabled: isOrganizationsRoute && activeOrgId.length > 0 && Boolean(activeOrgFeatures.billing),
  });
  const accessReview = useQuery({
    queryKey: ["org-access-review", activeOrgId],
    queryFn: () => getOrgAccessReview(activeOrgId),
    enabled: isSecurityRoute && activeOrgId.length > 0,
    refetchInterval: isSecurityRoute ? 30_000 : false,
  });
  const projects = useQuery({
    queryKey: ["projects"],
    queryFn: listProjects,
  });
  const projectList = projects.data ?? [];
  const activeConfigArea: ConfigArea = activeProjectTab === "database" ? "database" : configArea;
  const activeRef = routeRef || selectedRef || projectList[0]?.ref || "";
  const activeProjectListItem = useMemo(
    () => projectList.find((project) => project.ref === activeRef) ?? projectList[0],
    [projectList, activeRef],
  );
  const projectDetail = useQuery({
    queryKey: ["project", activeRef],
    queryFn: () => getProject(activeRef),
    enabled: hasProjectRoute && activeRef.length > 0,
    refetchInterval: hasProjectRoute ? 15_000 : false,
  });
  const activeProject = projectDetail.data ?? activeProjectListItem;
  const activeFeatureOrgId = activeProject?.org_id || activeOrgId;
  const activeProjectFeatures = useQuery({
    queryKey: ["org-features", activeFeatureOrgId],
    queryFn: () => getOrgFeatureFlags(activeFeatureOrgId),
    enabled: hasProjectRoute && activeFeatureOrgId.length > 0,
  });
  const activeFeatureFlags = activeProjectFeatures.data?.effective ?? activeOrgFeatures;
  const connect = useQuery({
    queryKey: ["connect", activeRef],
    // The overview's connection-basics panel and "Open Studio" button also need
    // the connect payload, so load it on overview as well as the connect tab.
    enabled: (needsProjectConnect || needsProjectOverview) && activeRef.length > 0,
    queryFn: () => getConnect(activeRef),
  });
  const cliProfile = useQuery({
    queryKey: ["cli-profile", activeRef],
    queryFn: () => getProjectCLIProfile(activeRef),
    enabled: needsProjectConnect && activeRef.length > 0,
  });
  const projectMetrics = useQuery({
    queryKey: ["project-metrics", activeRef],
    queryFn: () => getProjectMetrics(activeRef),
    enabled: needsProjectOverview && activeRef.length > 0,
    refetchInterval: needsProjectOverview ? 15_000 : false,
  });
  const projectAccess = useQuery({
    queryKey: ["project-access", activeRef],
    queryFn: () => listProjectAccess(activeRef),
    enabled: needsProjectAccess && activeRef.length > 0,
  });
  const routeManifest = useQuery({
    queryKey: ["project-route-manifest", activeRef],
    queryFn: () => getProjectRouteManifest(activeRef),
    enabled: (needsProjectConnect || needsProjectConfig) && activeRef.length > 0,
  });
  const domains = useQuery({
    queryKey: ["project-domains", activeRef],
    queryFn: () => listProjectDomains(activeRef),
    enabled: (needsProjectConnect || needsProjectConfig) && activeRef.length > 0,
  });
  const projectServices = useQuery({
    queryKey: ["project-services", activeRef],
    queryFn: () => getProjectServices(activeRef),
    enabled: (hasProjectRoute && activeProjectTab !== "activity") && activeRef.length > 0,
  });
  const projectConfig = useQuery({
    queryKey: ["project-config", activeRef, activeConfigArea],
    queryFn: () => getProjectConfig(activeRef, activeConfigArea),
    enabled: (needsProjectDatabase || needsProjectConfig) && activeRef.length > 0,
  });
  const databasePoolerConfig = useQuery({
    queryKey: ["project-config", activeRef, "pooler"],
    queryFn: () => getProjectConfig(activeRef, "pooler"),
    enabled: activeRef.length > 0 && activeProjectTab === "database",
  });
  const projectBranches = useQuery({
    queryKey: ["project-branches", activeRef],
    queryFn: () => listProjectBranches(activeRef),
    enabled: needsProjectDatabase && activeRef.length > 0,
  });
  const projectReplicas = useQuery({
    queryKey: ["project-replicas", activeRef],
    queryFn: () => listProjectReplicas(activeRef),
    enabled: needsProjectDatabase && activeRef.length > 0,
  });
  const projectReplicaRouting = useQuery({
    queryKey: ["project-replica-routing", activeRef],
    queryFn: () => getProjectReplicaRouting(activeRef),
    enabled: needsProjectDatabase && activeRef.length > 0,
  });
  const replicationPipelines = useQuery({
    queryKey: ["replication-pipelines", activeRef],
    queryFn: () => listProjectReplicationPipelines(activeRef),
    enabled: needsProjectDatabase && activeRef.length > 0,
  });
  const cdnPolicy = useQuery({
    queryKey: ["cdn-policy", activeRef],
    queryFn: () => getProjectCDNPolicy(activeRef),
    enabled: needsProjectConfig && activeRef.length > 0,
  });
  const cdnInvalidations = useQuery({
    queryKey: ["cdn-invalidations", activeRef],
    queryFn: () => listProjectCDNInvalidations(activeRef),
    enabled: needsProjectConfig && activeRef.length > 0,
  });
  const networkConnections = useQuery({
    queryKey: ["network-connections", activeRef],
    queryFn: () => listProjectNetworkConnections(activeRef),
    enabled: needsProjectConfig && activeRef.length > 0,
  });
  const networkPolicy = useQuery({
    queryKey: ["network-policy", activeRef],
    queryFn: () => getProjectNetwork(activeRef),
    enabled: needsProjectConfig && activeRef.length > 0,
  });
  const logDrains = useQuery({
    queryKey: ["log-drains", activeRef],
    queryFn: () => listProjectLogDrains(activeRef),
    enabled: needsProjectLogs && activeRef.length > 0,
  });
  const backups = useQuery({
    queryKey: ["backups", activeRef],
    queryFn: () => listBackups(activeRef),
    enabled: needsProjectBackups && activeRef.length > 0,
  });
  const backupPolicy = useQuery({
    queryKey: ["backup-policy", activeRef],
    queryFn: () => getBackupPolicy(activeRef),
    enabled: needsProjectBackups && activeRef.length > 0,
  });
  const recoverability = useQuery({
    queryKey: ["recoverability", activeRef],
    queryFn: () => getProjectRecoverability(activeRef),
    enabled: needsProjectBackups && activeRef.length > 0,
  });
  const pitrPolicy = useQuery({
    queryKey: ["pitr-policy", activeRef],
    queryFn: () => getPITRPolicy(activeRef),
    enabled: needsProjectBackups && activeRef.length > 0,
  });
  const walArchives = useQuery({
    queryKey: ["wal-archives", activeRef],
    queryFn: () => listWALArchives(activeRef),
    enabled: needsProjectBackups && activeRef.length > 0,
  });
  const projectLogs = useQuery({
    queryKey: ["project-logs", activeRef],
    queryFn: () => listProjectLogs(activeRef),
    enabled: needsProjectLogs && activeRef.length > 0,
    refetchInterval: needsProjectLogs ? 10_000 : false,
  });
  const projectActivity = useQuery({
    queryKey: ["project-activity", activeRef],
    queryFn: () => listProjectActivity(activeRef),
    enabled: needsProjectActivity && activeRef.length > 0,
    refetchInterval: needsProjectActivity ? 10_000 : false,
  });
  useEffect(() => {
    if (routeRef) {
      setSelectedRef(routeRef);
    }
  }, [routeRef]);
  // Redirect away from feature-gated routes once platform flags are known, so a
  // deep link / bookmark to a disabled area doesn't land on a dead page.
  useEffect(() => {
    if (!platformDefaults.data) {
      return;
    }
    // When multi-org is off, /organizations stays reachable as the single-org
    // "Access" workspace (users, teams/roles, quotas) — only billing/usage and
    // multi-org management are hidden inside it.
    if (!ssoScimEnabled && (pathname === "/settings/sso" || pathname === "/settings/scim")) {
      void navigate({ to: "/settings" });
    }
  }, [platformDefaults.data, orgsEnabled, ssoScimEnabled, isOrganizationsRoute, pathname, navigate]);
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
    void navigate({ to: projectRouteByTab[tab], params: { ref } });
  };
  const paletteActions = useMemo<PaletteAction[]>(() => {
    const actions: PaletteAction[] = [
      { id: "nav-fleet", title: "Dashboard", subtitle: "At-a-glance health, server usage, and projects", group: "Navigation", icon: Activity, run: () => routeTo("/", "fleet-dashboard") },
      { id: "nav-orgs", title: orgsEnabled ? "Organizations" : "Access", subtitle: orgsEnabled ? "Orgs, members, quotas, and usage" : "Users, teams, roles, and quotas", group: "Navigation", icon: orgsEnabled ? UserPlus : Shield, run: () => routeTo("/organizations", "organizations") },
      { id: "nav-projects", title: "Projects list", subtitle: "Browse isolated stacks", group: "Navigation", icon: Database, run: () => routeTo("/projects", "projects-list") },
      { id: "nav-create-project", title: "Create project", subtitle: "Provision a new Supabase stack", group: "Navigation", icon: Plus, run: () => routeTo("/projects/new", "create-project") },
      { id: "nav-overview", title: "Project overview", subtitle: activeProject ? `${activeProject.ref} metrics, health, and connection basics` : "Project dashboard", group: "Navigation", icon: Activity, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref) },
      { id: "nav-connect", title: "Connect surface", subtitle: activeProject ? `${activeProject.ref} credentials and links` : "Project credentials and links", group: "Navigation", icon: KeyRound, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "connect") },
      { id: "nav-project-access", title: "Project Access", subtitle: "Project RBAC teams and role grants", group: "Navigation", icon: Shield, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "access") },
      { id: "nav-project-database", title: "Project Database", subtitle: "Pooler, replicas, branches, and replication", group: "Navigation", icon: Database, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "database") },
      { id: "nav-project-logs", title: "Project Logs", subtitle: "Log tail and log drains", group: "Navigation", icon: Activity, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "logs") },
      { id: "nav-config", title: "Project settings", subtitle: "Runtime config, services, domains, network, operations", group: "Navigation", icon: SlidersHorizontal, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "config") },
      { id: "nav-backups", title: "Backups and PITR", subtitle: "Logical backups, restore runs, and WAL archive", group: "Navigation", icon: RotateCcw, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "backups") },
      { id: "nav-project-activity", title: "Project Activity", subtitle: "Per-project activity and audit trail", group: "Navigation", icon: Activity, disabled: !activeProject, run: () => activeProject && routeToProject(activeProject.ref, "activity") },
      { id: "nav-security", title: "Security", subtitle: "MFA, access review, and fleet advisor", group: "Navigation", icon: Shield, run: () => routeTo("/security", "security") },
      { id: "nav-hosts", title: "Hosts", subtitle: "Host capacity registration", group: "Navigation", icon: Server, run: () => routeTo("/hosts", "hosts") },
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
    if (hasProjectRoute && activeProject) {
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
  }, [activeProject, hasProjectRoute, orgsEnabled, projectActionBusy, projectList, routeTo, routeToProject, triggerBackupMutation.isPending]);

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
  const pageTitle = pageTitleForPathname(pathname, activeProject, activeProjectTab, orgsEnabled);
  const pageScopeLabel = routeRef ? "Project workspace" : "Control plane";
  const breadcrumbs = useMemo(() => buildBreadcrumbs(pathname, { activeProject, orgsEnabled }), [pathname, activeProject, orgsEnabled]);
  const account = useMemo(() => accountSummaryFromUser(user), [user]);
  const apiStatusLabel = apiHealth.isError ? "API offline" : apiHealth.isLoading ? "API checking" : "API online";
  const apiStatusClass = apiHealth.isError ? "bg-danger" : apiHealth.isLoading ? "bg-warning" : "bg-success";
  const contextValue: DashboardContextValue = {
    orgsEnabled,
    ssoScimEnabled,
    quotasEnabled,
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
    databasePoolerConfig,
    projectBranches,
    projectReplicas,
    projectReplicaRouting,
    replicationPipelines,
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
      <main className="min-h-screen bg-bg text-text lg:h-screen lg:overflow-hidden">
        <CommandPalette actions={paletteActions} />
        <ToastHost />
        <Modal
          description={commandCopy?.description}
          footer={
            <>
              <Button variant="secondary" disabled={projectCommandBusy} onClick={() => setProjectCommandConfirm(null)} type="button">
                Cancel
              </Button>
              <Button variant={commandCopy?.tone === "warning" ? "danger" : "default"} disabled={projectCommandBusy} onClick={runConfirmedProjectCommand} type="button">
                {projectCommandBusy ? "Working..." : commandCopy?.confirmLabel ?? "Confirm"}
              </Button>
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
        <div aria-hidden={paletteOpen ? true : undefined} className="grid min-h-screen grid-cols-[248px_1fr] max-lg:grid-cols-1 lg:h-full lg:min-h-0">
          <Sidebar projectMode={Boolean(routeRef)} />
          <section className="flex min-w-0 flex-col border-l border-border max-lg:border-l-0 lg:h-full lg:min-h-0 lg:overflow-hidden">
            <header className="flex h-14 shrink-0 items-center justify-between border-b border-border px-6">
              <div className="min-w-0">
                {breadcrumbs.length > 1 ? (
                  <Breadcrumbs crumbs={breadcrumbs} key={pathname} />
                ) : (
                  <>
                    <p className="label">{pageScopeLabel}</p>
                    <h1 className="text-[18px] font-medium">{pageTitle}</h1>
                  </>
                )}
              </div>
              <div className="flex items-center gap-2 text-sm text-muted">
                <span className={`status-dot ${apiStatusClass}`} />
                {apiStatusLabel}
                <Button className="h-8" variant="ghost" size="icon" onClick={openPalette} title="Open command palette" type="button">
                  <Command size={14} />
                </Button>
                <AccountMenu account={account} onLogout={onLogout} onRoute={routeTo} onThemeChange={setTheme} theme={theme} />
              </div>
            </header>
            {/* min-w-0 + overflow-x-clip: a single durable guard so no wide
                descendant (charts, long mono strings, dense panels) can ever
                push a page-wide horizontal scrollbar. Tables scroll internally
                via their own wrapper. */}
            <div className="grid min-w-0 gap-6 overflow-x-clip p-6 lg:min-h-0 lg:flex-1 lg:overflow-y-auto" id={panelIdForPathname(pathname)}>
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
  const [pwOpen, setPwOpen] = useState(false);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const menuID = useId();
  const buttonRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const passwordMutation = useMutation({
    mutationFn: () => changeAccountPassword({ current_password: currentPassword, new_password: newPassword }),
    onSuccess: () => {
      setPwOpen(false);
      setCurrentPassword("");
      setNewPassword("");
    },
  });

  function navigate(to: string, id: string) {
    setOpen(false);
    onRoute(to, id);
  }

  function focusMenuItem(index: number) {
    const items = menuRef.current?.querySelectorAll<HTMLButtonElement>('[role^="menuitem"]');
    if (!items?.length) {
      return;
    }
    items[(index + items.length) % items.length]?.focus();
  }

  function closeAndFocusButton() {
    setOpen(false);
    buttonRef.current?.focus();
  }

  function onButtonKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") {
      return;
    }
    event.preventDefault();
    setOpen(true);
    window.requestAnimationFrame(() => focusMenuItem(event.key === "ArrowUp" ? -1 : 0));
  }

  function onMenuKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('[role^="menuitem"]') ?? []);
    const activeIndex = items.findIndex((item) => item === document.activeElement);
    switch (event.key) {
      case "Escape":
        event.preventDefault();
        closeAndFocusButton();
        break;
      case "Tab":
        setOpen(false);
        break;
      case "ArrowDown":
        event.preventDefault();
        focusMenuItem(activeIndex + 1);
        break;
      case "ArrowUp":
        event.preventDefault();
        focusMenuItem(activeIndex - 1);
        break;
      case "Home":
        event.preventDefault();
        focusMenuItem(0);
        break;
      case "End":
        event.preventDefault();
        focusMenuItem(items.length - 1);
        break;
    }
  }

  useEffect(() => {
    if (!open) {
      return;
    }
    window.requestAnimationFrame(() => focusMenuItem(0));
    function onPointerDown(event: PointerEvent) {
      if (menuRef.current?.contains(event.target as Node) || buttonRef.current?.contains(event.target as Node)) {
        return;
      }
      setOpen(false);
    }
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  return (
    <div className="relative">
      <button aria-expanded={open} aria-haspopup="menu" aria-controls={menuID} className="nav-item h-9 gap-2 border border-border bg-surface px-2 pr-3" onClick={() => setOpen((value) => !value)} onKeyDown={onButtonKeyDown} ref={buttonRef} type="button">
        <span className="grid h-6 w-6 place-items-center rounded-full border border-border-strong bg-surface-2 font-mono text-[11px] font-semibold text-text">{account.initials}</span>
        <span className="max-w-[150px] truncate text-sm text-text">{account.shortName}</span>
      </button>
      {open ? (
        <div aria-label="Account" className="absolute right-0 top-11 z-50 w-72 rounded-lg border border-border bg-surface p-2 shadow-[0_18px_40px_rgba(0,0,0,.45)]" id={menuID} onKeyDown={onMenuKeyDown} ref={menuRef} role="menu">
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
                aria-checked={theme === "dark"}
                className={theme === "dark" ? "segmented active h-8" : "segmented h-8"}
                onClick={() => onThemeChange("dark")}
                role="menuitemradio"
                type="button"
              >
                <Moon size={14} />
                Dark
              </button>
              <button
                aria-checked={theme === "light"}
                className={theme === "light" ? "segmented active h-8" : "segmented h-8"}
                onClick={() => onThemeChange("light")}
                role="menuitemradio"
                type="button"
              >
                <Sun size={14} />
                Light
              </button>
            </div>
            <button className="nav-item h-9 justify-start" onClick={() => navigate("/security", "security")} role="menuitem" type="button">
              <UserCircle size={14} />
              Profile
            </button>
            <button className="nav-item h-9 justify-start" onClick={() => navigate("/security/mfa", "security")} role="menuitem" type="button">
              <Shield size={14} />
              Account security
            </button>
            <button className="nav-item h-9 justify-start" onClick={() => { setOpen(false); setPwOpen(true); }} role="menuitem" type="button">
              <KeyRound size={14} />
              Change password
            </button>
            <button className="nav-item h-9 justify-start" onClick={() => navigate("/settings", "settings")} role="menuitem" type="button">
              <SlidersHorizontal size={14} />
              Platform settings
            </button>
            <button className="nav-item h-9 justify-start" onClick={() => navigate("/about", "about")} role="menuitem" type="button">
              <Info size={14} />
              About
            </button>
            <button className="nav-item h-9 justify-start text-danger hover:text-danger" onClick={() => {
              setOpen(false);
              onLogout();
            }} role="menuitem" type="button">
              <LogOut size={14} />
              Logout
            </button>
          </div>
        </div>
      ) : null}
      <Modal
        open={pwOpen}
        onClose={() => !passwordMutation.isPending && setPwOpen(false)}
        title="Change password"
        description="Update the password for your own account. You'll stay signed in."
        footer={
          <>
            <Button variant="secondary" disabled={passwordMutation.isPending} onClick={() => setPwOpen(false)} type="button">Cancel</Button>
            <Button form="change-password-form" disabled={passwordMutation.isPending || currentPassword.length === 0 || newPassword.length === 0} type="submit">
              <KeyRound size={14} />
              Update password
            </Button>
          </>
        }
      >
        <form id="change-password-form" className="grid gap-3" onSubmit={(event) => { event.preventDefault(); passwordMutation.mutate(); }}>
          <label className="grid gap-1">
            <span className="label">Current password</span>
            <Input autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} type="password" />
          </label>
          <label className="grid gap-1">
            <span className="label">New password</span>
            <Input autoComplete="new-password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} type="password" />
          </label>
          {passwordMutation.error ? <p className="text-sm text-danger">{passwordMutation.error.message}</p> : null}
        </form>
      </Modal>
    </div>
  );
}

function accountSummaryFromUser(user: User | null): AccountSummary {
  const email = user?.email || "admin";
  const role = user?.role || "admin";
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

type ToastMessage = { id: string; title: string; detail?: string; kind?: "success" | "warning" | "danger" };

const toastIconByKind = {
  success: CheckCircle2,
  warning: AlertTriangle,
  danger: XCircle,
} as const;

const toastDismissMs = {
  success: 6000,
  warning: 6000,
  danger: 10000,
} as const;

function ToastItem({ toast, onDismiss }: { toast: ToastMessage; onDismiss: (id: string) => void }) {
  const kind = toast.kind ?? "success";
  const Icon = toastIconByKind[kind];
  useEffect(() => {
    const timer = window.setTimeout(() => onDismiss(toast.id), toastDismissMs[kind]);
    return () => window.clearTimeout(timer);
  }, [toast.id, kind, onDismiss]);
  return (
    <button className={`toast ${kind}`} onClick={() => onDismiss(toast.id)} type="button">
      <Icon size={15} />
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium">{toast.title}</span>
        {toast.detail ? <span className="mt-0.5 block truncate text-xs text-muted">{toast.detail}</span> : null}
      </span>
    </button>
  );
}

function ToastHost() {
  const toasts = useUIStore((state) => state.toasts);
  const removeToast = useUIStore((state) => state.removeToast);
  return (
    <div aria-live="polite" className="toast-host">
      {toasts.map((toast) => (
        <ToastItem key={toast.id} onDismiss={removeToast} toast={toast} />
      ))}
    </div>
  );
}

function CommandPalette({ actions }: { actions: PaletteAction[] }) {
  const inputRef = useRef<HTMLInputElement>(null);
  const backdropRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
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
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const restoreBackground = makeBackgroundInert(backdropRef.current);
    setActiveIndex(0);
    window.setTimeout(() => inputRef.current?.focus(), 0);
    return () => {
      restoreBackground();
      if (previousFocusRef.current?.isConnected) {
        previousFocusRef.current.focus();
      }
      previousFocusRef.current = null;
    };
  }, [open]);

  useEffect(() => {
    if (open) {
      setActiveIndex(0);
    }
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

  function onDialogKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      closePalette();
      return;
    }
    if (event.key !== "Tab") {
      return;
    }
    const elements = focusableElements(dialogRef.current);
    if (elements.length === 0) {
      event.preventDefault();
      return;
    }
    const first = elements[0];
    const last = elements[elements.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
      return;
    }
    if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  return (
    <div className="palette-backdrop" onMouseDown={closePalette} ref={backdropRef} role="presentation">
      <section aria-label="Command palette" aria-modal="true" className="palette-dialog" onKeyDown={onDialogKeyDown} onMouseDown={(event) => event.stopPropagation()} ref={dialogRef} role="dialog" tabIndex={-1}>
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
              const showHeader = index === 0 || matches[index - 1]?.group !== action.group;
              return (
                <div key={action.id}>
                  {showHeader ? (
                    <div className="label" role="presentation" style={{ padding: "10px 12px 4px" }}>{action.group}</div>
                  ) : null}
                  <button
                    aria-selected={active}
                    className={action.disabled ? "palette-item disabled" : active ? "palette-item active" : "palette-item"}
                    disabled={action.disabled}
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
                      <span className="palette-subtitle">{action.subtitle}</span>
                    </span>
                  </button>
                </div>
              );
            })
          )}
        </div>
      </section>
    </div>
  );
}

function SidebarFooter() {
  const health = useQuery({ queryKey: ["api-health"], queryFn: getApiHealth, refetchInterval: 30_000 });
  const version = health.data?.version;
  return (
    <div className="mt-auto px-1 pt-6">
      <Link
        activeProps={{ className: "flex items-center justify-between text-xs text-text" }}
        className="flex items-center justify-between text-xs text-faint hover:text-text"
        to="/about"
      >
        <span className="inline-flex items-center gap-1.5">
          <Info size={12} />
          About
        </span>
        {version ? <span className="font-mono text-faint">v{version}</span> : null}
      </Link>
      <BuiltByFooter className="mt-2" />
    </div>
  );
}

function Sidebar({ projectMode }: { projectMode: boolean }) {
  const { activeProject, activeProjectTab, activeRef, orgsEnabled, ssoScimEnabled, quotasEnabled } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const projectSections = useUIStore((state) => state.projectSections);
  if (projectMode) {
    const ref = activeProject?.ref ?? activeRef;

    return (
      <aside className="flex min-h-screen flex-col bg-bg px-4 py-4 max-lg:min-h-0 lg:h-full lg:min-h-0 lg:overflow-y-auto">
        <div className="flex h-10 items-center gap-2.5 px-1">
          <BrandLogo className="h-9 w-9" />
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
            <StatusPill status={activeProject?.status ?? "loading"} />
            {activeProject ? <Badge variant="muted">{activeProject.spec.resource_tier}</Badge> : null}
            {activeProject ? <Badge variant="muted">{activeProject.spec.profile}</Badge> : null}
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
                <Link activeOptions={{ exact: tab.id === "overview" }} activeProps={{ className: "nav-item active" }} className={`nav-item ${active ? "active" : ""}`} params={{ ref }} to={projectRouteByTab[tab.id]}>
                  <Icon size={15} />
                  {tab.label}
                </Link>
                {subnav ? (
                  <div className="project-subnav">
                    {subnav.map((item, index) => {
                      const group = item.group;
                      // Render a small cluster header whenever a new group begins.
                      // Tabs whose items have no group skip headers entirely.
                      const showGroup = group ? group !== subnav[index - 1]?.group : false;
                      return (
                        <div className="contents" key={item.id}>
                          {showGroup ? (
                            <div className="label" role="presentation" style={{ padding: "10px 12px 4px" }}>{group}</div>
                          ) : null}
                          <Link activeOptions={{ exact: true }} className={`project-subnav-item ${activeSection === item.id ? "active" : ""}`} title={item.description} to={item.id === "overview" ? projectRouteByTab[tab.id] : projectPath(ref, tabSuffix, item.id)}>
                            {item.label}
                          </Link>
                        </div>
                      );
                    })}
                  </div>
                ) : null}
              </div>
            );
          })}
        </nav>
        <SidebarFooter />
      </aside>
    );
  }

  const navItems: Array<{ label: string; icon: LucideIcon; to: string }> = [
    { label: "Dashboard", icon: Activity, to: "/" },
    // Same workspace, framed for the mode: full "Organizations" when multi-org is
    // on; a single-org "Access" (users, teams/roles, quotas) when it's off.
    { label: orgsEnabled ? "Organizations" : "Access", icon: orgsEnabled ? UserPlus : Shield, to: "/organizations" },
    { label: "Projects", icon: Database, to: "/projects" },
    { label: "Security", icon: Shield, to: "/security" },
    { label: "Hosts", icon: Server, to: "/hosts" },
    { label: "Settings", icon: SlidersHorizontal, to: "/settings" },
    { label: "Audit", icon: Activity, to: "/audit" },
  ];
  // Hide the SSO/SCIM settings sub-sections unless platform SSO/SCIM is enabled.
  const visibleSettingsSections = platformSettingsSections.filter((item) => ssoScimEnabled || (item.id !== "sso" && item.id !== "scim"));
  // Hide the Quotas sub-section unless resource quotas are enabled for the platform.
  const visibleOrganizationSections = organizationSections.filter((item) => quotasEnabled || item.id !== "quotas");

  return (
    <aside className="flex min-h-screen flex-col bg-bg px-4 py-4 max-lg:min-h-0">
      <div className="flex h-10 items-center gap-2.5 px-1">
        <BrandLogo className="h-9 w-9" />
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
                  {visibleOrganizationSections.map((item) => {
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
                  {visibleSettingsSections.map((item) => {
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
      <SidebarFooter />
    </aside>
  );
}
