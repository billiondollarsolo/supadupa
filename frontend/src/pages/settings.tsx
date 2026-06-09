import { Navigate, useRouterState } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { listStackReleases } from "../api";
import { useDashboardContext } from "../lib/dashboard-context";
import { platformSettingsSections, type PlatformSettingsSection } from "../lib/project-config";
import { SettingsPanel } from "./settings/settings-panel";

export function SettingsRoutePage() {
  const { backupStorageTargets, fleetMetrics, platformBackups, platformDefaults, platformSSO, provisionerStatus, runtimeConfig, scimGroups, scimServiceProviderConfig, scimUsers } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const stackReleases = useQuery({ queryKey: ["stack-releases"], queryFn: listStackReleases });
  const selectedSection = pathname.match(/^\/settings\/([^/]+)/)?.[1];
  const selectedItem = pathname.match(/^\/settings\/[^/]+\/([^/]+)/)?.[1];
  const section: PlatformSettingsSection = platformSettingsSections.some((item) => item.id === selectedSection) ? selectedSection as PlatformSettingsSection : "overview";
  // "Hosts" has a single, top-level page (and nav entry). The settings entry
  // defers there instead of rendering a second, divergent HostPanel inline, so
  // there's one Hosts UI rather than two doors that drift apart.
  if (section === "hosts") {
    return selectedItem
      ? <Navigate to="/hosts/$item" params={{ item: selectedItem }} replace />
      : <Navigate to="/hosts" replace />;
  }
  return (
    <SettingsPanel
      defaults={platformDefaults.data}
      sso={platformSSO.data}
      backupStorageTargets={backupStorageTargets.data ?? []}
      fleetMetrics={fleetMetrics.data}
      platformBackups={platformBackups.data ?? []}
      stackReleases={stackReleases.data ?? []}
      provisionerStatus={provisionerStatus.data}
      runtimeConfig={runtimeConfig.data}
      scimServiceProviderConfig={scimServiceProviderConfig.data}
      scimUsers={scimUsers.data}
      scimGroups={scimGroups.data}
      item={selectedItem}
      section={section}
      loading={platformDefaults.isLoading || platformSSO.isLoading || backupStorageTargets.isLoading || platformBackups.isLoading || stackReleases.isLoading || provisionerStatus.isLoading || runtimeConfig.isLoading || scimServiceProviderConfig.isLoading || scimUsers.isLoading || scimGroups.isLoading}
    />
  );
}
