import { useDashboardContext } from "../../lib/dashboard-context";
import { LogDrainsPanel, ProjectLogsPanel } from "./log-panels";
import { ProjectPage } from "./layout";

export function ProjectLogsPage() {
  const { activeFeatureFlags, activeProject, logDrains, projectLogs } = useDashboardContext();
  return (
    <ProjectPage>
      <ProjectLogsPanel project={activeProject} logs={projectLogs.data ?? []} loading={projectLogs.isLoading} />
      <LogDrainsPanel project={activeProject} drains={logDrains.data ?? []} loading={logDrains.isLoading} enabled={Boolean(activeFeatureFlags.log_drains)} />
    </ProjectPage>
  );
}
