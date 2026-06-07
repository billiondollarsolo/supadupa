import { useRouterState } from "@tanstack/react-router";
import { useDashboardContext } from "../../lib/dashboard-context";
import { FunctionsConfigPanel, FunctionsPanel } from "./functions-panel";
import { ProjectPage } from "./layout";

export function ProjectFunctionsPage() {
  const { activeFeatureFlags, activeProject, functionRegions, functionStorageMounts, projectConfig, projectFunctions } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const functionsMatch = pathname.match(/^\/projects\/[^/]+\/functions(?:\/([^/]+))?(?:\/([^/]+))?/);
  const section = functionsMatch?.[1];
  const item = functionsMatch?.[2];
  const showingFunctionDetail = section === "deployments" && Boolean(item);
  const enabled = Boolean(activeFeatureFlags.edge_functions);
  return (
    <ProjectPage>
      {showingFunctionDetail ? null : <FunctionsConfigPanel project={activeProject} config={projectConfig.data} loading={projectConfig.isLoading} enabled={enabled} />}
      <FunctionsPanel project={activeProject} functions={projectFunctions.data ?? []} regions={functionRegions.data ?? []} mounts={functionStorageMounts.data ?? []} loading={projectFunctions.isLoading || functionRegions.isLoading || functionStorageMounts.isLoading} enabled={enabled} item={item} />
    </ProjectPage>
  );
}
