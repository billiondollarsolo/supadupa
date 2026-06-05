import { useMemo } from "react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { useDashboardContext } from "../../lib/dashboard-context";
import { projectSettingsSections, type ProjectSettingsSection } from "../../lib/project-config";
import { ConfigPanel, DangerZonePanel, DomainsPanel, NetworkConnectionsPanel, ServicesPanel } from "./config-panels";
import { RoutesPanel } from "./connect-panels";
import { ProjectPage } from "./layout";
import { LifecyclePanel, RuntimeStatusPanel } from "./side-panels";

export function ProjectConfigPage() {
  const { activeFeatureFlags, activeOrgId, activeProject, configArea, domains, networkConnections, networkPolicy, onProjectDestroyed, projectConfig, projectServices, routes, setConfigArea } = useDashboardContext();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = pathname.match(/^\/projects\/[^/]+\/config\/([^/]+)/)?.[1];
  const activeSection: ProjectSettingsSection = projectSettingsSections.some((section) => section.id === selectedSection) ? selectedSection as ProjectSettingsSection : "overview";
  const stats = useMemo(() => {
    const services = projectServices.data?.services ?? {};
    const serviceValues = Object.values(services);
    const enabledServices = serviceValues.length > 0 ? serviceValues.filter(Boolean).length : 11;
    const allowlist = networkPolicy.data?.allowlist?.split("\n").filter((item) => item.trim().length > 0).length ?? 0;
    return {
      configArea,
      services: enabledServices,
      domains: domains.data?.length ?? 0,
      routes: routes.data?.length ?? 0,
      network: networkConnections.data?.length ?? 0,
      allowlist,
      status: activeProject?.status ?? "loading",
    };
  }, [activeProject?.status, configArea, domains.data, networkConnections.data, networkPolicy.data, projectServices.data, routes.data]);

  return (
    <ProjectPage>
      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Project settings</p>
            <h2>{activeProject?.ref ?? "Project settings"}</h2>
            <p className="mt-1 text-xs text-faint">Use the project sidebar to drill into runtime config, services, domains, network, operations, and destructive actions.</p>
          </div>
          <span className={`pill ${stats.status === "healthy" ? "healthy" : stats.status === "paused" ? "provisioning" : ""}`}>{stats.status}</span>
        </div>
        <div className="mt-4">
          <SettingsSectionSummary
            activeSection={activeSection}
            onSelect={(section) => {
              if (!activeProject) return;
              void navigate({ to: section === "overview" ? `/projects/${activeProject.ref}/config` : `/projects/${activeProject.ref}/config/${section}` });
            }}
            stats={stats}
          />
        </div>
      </section>

      {activeSection === "runtime" ? <ConfigPanel project={activeProject} area={configArea} config={projectConfig.data} loading={projectConfig.isLoading} onAreaChange={setConfigArea} /> : null}
      {activeSection === "services" ? <ServicesPanel project={activeProject} services={projectServices.data} loading={projectServices.isLoading} /> : null}
      {activeSection === "domains" ? <DomainsPanel project={activeProject} domains={domains.data ?? []} loading={domains.isLoading} enabled={Boolean(activeFeatureFlags.custom_domains)} /> : null}
      {activeSection === "network" ? (
        <div className="grid gap-4">
          <RoutesPanel routes={routes.data ?? []} loading={routes.isLoading} />
          <NetworkConnectionsPanel project={activeProject} policy={networkPolicy.data} connections={networkConnections.data ?? []} loading={networkConnections.isLoading || networkPolicy.isLoading} enabled={Boolean(activeFeatureFlags.network_restrictions)} />
        </div>
      ) : null}
      {activeSection === "operations" ? (
        <div className="grid grid-cols-2 gap-6 max-xl:grid-cols-1">
          <RuntimeStatusPanel project={activeProject} />
          <LifecyclePanel orgId={activeOrgId} project={activeProject} />
        </div>
      ) : null}
      {activeSection === "danger" ? <DangerZonePanel project={activeProject} onDestroyed={onProjectDestroyed} /> : null}
    </ProjectPage>
  );
}

function SettingsSectionSummary({
  activeSection,
  onSelect,
  stats,
}: {
  activeSection: ProjectSettingsSection;
  stats: {
    configArea: string;
    services: number;
    domains: number;
    routes: number;
    network: number;
    allowlist: number;
    status: string;
  };
  onSelect: (section: ProjectSettingsSection) => void;
}) {
  if (activeSection !== "overview") {
    const current = projectSettingsSections.find((section) => section.id === activeSection);
    return (
      <div className="rounded-md border border-border bg-bg p-3">
        <p className="label">{current?.label ?? "Section"}</p>
        <p className="mt-1 text-sm text-muted">{current?.description}</p>
      </div>
    );
  }

  const cards: Array<{ section: ProjectSettingsSection; label: string; value: string; detail: string }> = [
    { section: "runtime", label: "Runtime config", value: stats.configArea, detail: "Project config area" },
    { section: "services", label: "Services", value: `${stats.services} enabled`, detail: "Managed stack services" },
    { section: "domains", label: "Domains", value: `${stats.domains} custom`, detail: "Ingress and certificates" },
    { section: "network", label: "Network", value: `${stats.routes} routes`, detail: `${stats.network} private · ${stats.allowlist} allowlist entries` },
    { section: "operations", label: "Operations", value: stats.status, detail: "Pause, resume, restart, upgrade" },
    { section: "danger", label: "Danger zone", value: "Gated", detail: "Destroy requires typed confirmation" },
  ];

  return (
    <div className="grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
      {cards.map((card) => (
        <button className="rounded-md border border-border bg-bg p-3 text-left transition hover:border-border-strong hover:bg-surface-2" key={card.section} onClick={() => onSelect(card.section)} type="button">
          <p className="label">{card.label}</p>
          <p className="mt-2 truncate text-sm font-medium">{card.value}</p>
          <p className="mt-1 truncate text-xs text-muted">{card.detail}</p>
        </button>
      ))}
    </div>
  );
}
