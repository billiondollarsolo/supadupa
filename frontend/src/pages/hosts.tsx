import { useRouterState } from "@tanstack/react-router";
import { useDashboardContext } from "../lib/dashboard-context";
import { HostPanel } from "./hosts/host-panel";

export function HostsPage() {
  const { hosts } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const item = pathname.match(/^\/hosts\/([^/]+)/)?.[1];
  return <HostPanel hosts={hosts.data ?? []} item={item} loading={hosts.isLoading} />;
}
