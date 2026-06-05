import { useRouterState } from "@tanstack/react-router";
import { useDashboardContext } from "../lib/dashboard-context";
import { platformSettingsSections, type PlatformSettingsSection } from "../lib/project-config";
import { HostPanel } from "./hosts/host-panel";
import { SettingsPanel } from "./settings/settings-panel";

export function SettingsRoutePage() {
  const { hosts, platformDefaults, platformSSO, provisionerStatus, scimGroups, scimServiceProviderConfig, scimUsers } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = pathname.match(/^\/settings\/([^/]+)/)?.[1];
  const section: PlatformSettingsSection = platformSettingsSections.some((item) => item.id === selectedSection) ? selectedSection as PlatformSettingsSection : "overview";
  if (section === "hosts") {
    return <HostPanel hosts={hosts.data ?? []} loading={hosts.isLoading} />;
  }
  return (
    <SettingsPanel
      defaults={platformDefaults.data}
      sso={platformSSO.data}
      provisionerStatus={provisionerStatus.data}
      scimServiceProviderConfig={scimServiceProviderConfig.data}
      scimUsers={scimUsers.data}
      scimGroups={scimGroups.data}
      section={section}
      loading={platformDefaults.isLoading || platformSSO.isLoading || provisionerStatus.isLoading || scimServiceProviderConfig.isLoading || scimUsers.isLoading || scimGroups.isLoading}
    />
  );
}
