import { useDashboardContext } from "../../lib/dashboard-context";
import { FunctionsConfigPanel, FunctionsPanel } from "./functions-panel";
import { ProjectPage } from "./layout";

export function ProjectFunctionsPage() {
  const { activeFeatureFlags, activeProject, functionRegions, functionStorageMounts, projectConfig, projectFunctions } = useDashboardContext();
  const enabled = Boolean(activeFeatureFlags.edge_functions);
  return (
    <ProjectPage>
      <FunctionsConfigPanel project={activeProject} config={projectConfig.data} loading={projectConfig.isLoading} enabled={enabled} />
      <FunctionsPanel project={activeProject} functions={projectFunctions.data ?? []} regions={functionRegions.data ?? []} mounts={functionStorageMounts.data ?? []} loading={projectFunctions.isLoading || functionRegions.isLoading || functionStorageMounts.isLoading} enabled={enabled} />
    </ProjectPage>
  );
}
