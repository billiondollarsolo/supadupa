import { useRouterState } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { listStackReleases } from "../api";
import { useDashboardContext } from "../lib/dashboard-context";
import { platformSettingsSections, type PlatformSettingsSection } from "../lib/project-config";
import { HostPanel } from "./hosts/host-panel";
import { SettingsPanel } from "./settings/settings-panel";

export function SettingsRoutePage() {
  const { backupStorageTargets, hosts, platformBackups, platformDefaults, platformSSO, provisionerStatus, runtimeConfig, scimGroups, scimServiceProviderConfig, scimUsers } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const stackReleases = useQuery({ queryKey: ["stack-releases"], queryFn: listStackReleases });
  const selectedSection = pathname.match(/^\/settings\/([^/]+)/)?.[1];
  const selectedItem = pathname.match(/^\/settings\/[^/]+\/([^/]+)/)?.[1];
  const section: PlatformSettingsSection = platformSettingsSections.some((item) => item.id === selectedSection) ? selectedSection as PlatformSettingsSection : "overview";
  if (section === "hosts") {
    return <HostPanel hosts={hosts.data ?? []} item={selectedItem} loading={hosts.isLoading} scope="settings" />;
  }
  return (
    <SettingsPanel
      defaults={platformDefaults.data}
      sso={platformSSO.data}
      backupStorageTargets={backupStorageTargets.data ?? []}
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
