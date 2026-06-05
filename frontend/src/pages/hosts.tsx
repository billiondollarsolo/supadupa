import { useDashboardContext } from "../lib/dashboard-context";
import { HostPanel } from "./hosts/host-panel";

export function HostsPage() {
  const { hosts } = useDashboardContext();
  return <HostPanel hosts={hosts.data ?? []} loading={hosts.isLoading} />;
}
