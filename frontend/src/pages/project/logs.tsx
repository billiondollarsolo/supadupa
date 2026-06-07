import { useRouterState } from "@tanstack/react-router";
import { useDashboardContext } from "../../lib/dashboard-context";
import { LogDrainsPanel, ProjectLogsPanel } from "./log-panels";
import { ProjectPage } from "./layout";

export function ProjectLogsPage() {
  const { activeFeatureFlags, activeProject, logDrains, projectLogs } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const logsMatch = pathname.match(/^\/projects\/[^/]+\/logs(?:\/([^/]+))?(?:\/([^/]+))?/);
  const section = logsMatch?.[1];
  const item = logsMatch?.[2];
  const showingDrainDetail = section === "drains" && Boolean(item);
  return (
    <ProjectPage>
      {showingDrainDetail ? null : <ProjectLogsPanel project={activeProject} logs={projectLogs.data ?? []} loading={projectLogs.isLoading} />}
      <LogDrainsPanel project={activeProject} drains={logDrains.data ?? []} loading={logDrains.isLoading} enabled={Boolean(activeFeatureFlags.log_drains)} item={item} />
    </ProjectPage>
  );
}
