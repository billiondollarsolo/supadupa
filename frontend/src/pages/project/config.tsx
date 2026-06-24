import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";
import { listProjectSecrets } from "../../api";
import { useDashboardContext } from "../../lib/dashboard-context";
import { projectSettingsSections, type ProjectSettingsSection } from "../../lib/project-config";
import { projectPath, projectSectionFromPathname } from "../../lib/routes";
import { statusTone } from "../../lib/status";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { StatusPill } from "../../components/ui/status-pill";
import { ConfigPanel, DangerZonePanel, DatabaseExposurePanel, DomainsPanel, NetworkConnectionsPanel, ServicesPanel } from "./config-panels";
import { RoutesPanel } from "./connect-panels";
import { CDNPanel } from "./cdn-panel";
import { ProjectPage } from "./layout";
import { LifecyclePanel, RuntimeStatusPanel, SecretsPanel } from "./side-panels";

export function ProjectConfigPage() {
  const { activeFeatureFlags, activeOrgId, activeProject, cdnInvalidations, cdnPolicy, configArea, domains, networkConnections, networkPolicy, onProjectDestroyed, projectConfig, projectServices, routeManifest, setConfigArea } = useDashboardContext();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = projectSectionFromPathname(pathname, "config");
  const activeSection: ProjectSettingsSection = projectSettingsSections.some((section) => section.id === selectedSection) ? selectedSection as ProjectSettingsSection : "overview";
  const projectRef = activeProject?.ref ?? "";
  const projectSecrets = useQuery({
    queryKey: ["project-secrets", projectRef],
    queryFn: () => listProjectSecrets(projectRef),
    enabled: activeSection === "operations" && Boolean(projectRef),
  });
  const stats = useMemo(() => {
    const services = projectServices.data?.services ?? {};
    const serviceValues = Object.values(services);
    const allowlist = networkPolicy.data?.allowlist?.split("\n").filter((item) => item.trim().length > 0).length ?? 0;
    return {
      servicesLoaded: !projectServices.isLoading && projectServices.data !== undefined,
      servicesEnabled: serviceValues.filter(Boolean).length,
      servicesTotal: serviceValues.length,
      domains: domains.data?.length ?? 0,
      routes: (routeManifest.data?.http_routes.length ?? 0) + (routeManifest.data?.tcp_routes.length ?? 0),
      network: networkConnections.data?.length ?? 0,
      allowlist,
      status: activeProject?.status ?? "loading",
    };
  }, [activeProject?.status, domains.data, networkConnections.data, networkPolicy.data, projectServices.data, projectServices.isLoading, routeManifest.data]);

  function goToSection(section: ProjectSettingsSection) {
    if (!activeProject) return;
    void navigate({ to: section === "overview" ? projectPath(activeProject.ref, "config") : projectPath(activeProject.ref, "config", section) });
  }

  const current = projectSettingsSections.find((section) => section.id === activeSection);
  const statusTextTone = (() => {
    const tone = statusTone(stats.status);
    return tone === "success" ? "success" : tone === "danger" ? "danger" : tone === "warning" ? "warning" : "default";
  })();

  return (
    <ProjectPage>
      <AppPanel
        actions={<StatusPill status={stats.status} />}
        eyebrow="Project settings"
        title={activeProject?.ref ?? "Project settings"}
        description={activeSection === "overview" ? "Configuration posture for this project. Use the sidebar to drill into each area." : undefined}
      >
        {activeSection === "overview" ? (
          <div className="mt-4 grid grid-cols-3 gap-2 max-md:grid-cols-2 max-sm:grid-cols-1">
            <MetricCard label="Status" value={stats.status} tone={statusTextTone} />
            <MetricCard label="Services enabled" value={stats.servicesLoaded ? `${stats.servicesEnabled}/${stats.servicesTotal}` : "—"} />
            <MetricCard label="Custom domains" value={stats.domains} />
            <MetricCard label="Routes" value={stats.routes} />
            <MetricCard label="Private connections" value={stats.network} />
            <MetricCard label="Allowlist entries" value={stats.allowlist} />
          </div>
        ) : (
          <nav className="mt-3 flex items-center gap-1.5 text-sm text-muted" aria-label="Breadcrumb">
            <button className="text-muted transition hover:text-text" onClick={() => goToSection("overview")} type="button">Settings</button>
            <ChevronRight size={14} className="text-faint" />
            <span className="font-medium text-text">{current?.label ?? "Section"}</span>
          </nav>
        )}
      </AppPanel>

      {activeSection === "runtime" ? <ConfigPanel project={activeProject} area={configArea} config={projectConfig.data} loading={projectConfig.isLoading} onAreaChange={setConfigArea} /> : null}
      {activeSection === "services" ? <ServicesPanel project={activeProject} services={projectServices.data} loading={projectServices.isLoading} /> : null}
      {activeSection === "domains" ? <DomainsPanel project={activeProject} domains={domains.data ?? []} loading={domains.isLoading} enabled={Boolean(activeFeatureFlags.custom_domains)} /> : null}
      {activeSection === "network" ? (
        <div className="grid gap-4">
          <DatabaseExposurePanel project={activeProject} hostPublished={routeManifest.data?.database_ingress_published} masterEnabled={routeManifest.data?.database_external_access_enabled} />
          <RoutesPanel manifest={routeManifest.data} loading={routeManifest.isLoading} />
          <NetworkConnectionsPanel project={activeProject} policy={networkPolicy.data} connections={networkConnections.data ?? []} loading={networkConnections.isLoading || networkPolicy.isLoading} enabled={Boolean(activeFeatureFlags.network_restrictions)} />
          <CDNPanel project={activeProject} policy={cdnPolicy.data} invalidations={cdnInvalidations.data ?? []} loading={cdnPolicy.isLoading || cdnInvalidations.isLoading} />
        </div>
      ) : null}
      {activeSection === "operations" ? (
        <div className="grid gap-4">
          <LifecyclePanel orgId={activeOrgId} project={activeProject} />
          <SecretsPanel project={activeProject} secrets={projectSecrets.data ?? []} loading={projectSecrets.isLoading} />
          <RuntimeStatusPanel project={activeProject} />
        </div>
      ) : null}
      {activeSection === "danger" ? <DangerZonePanel project={activeProject} onDestroyed={onProjectDestroyed} /> : null}
    </ProjectPage>
  );
}
