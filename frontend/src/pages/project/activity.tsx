import { useDashboardContext } from "../../lib/dashboard-context";
import { ProjectActivityPanel } from "./activity-panel";
import { ProjectPage } from "./layout";

export function ProjectActivityPage() {
  const { projectActivity } = useDashboardContext();
  return (
    <ProjectPage>
      <ProjectActivityPanel events={projectActivity.data ?? []} loading={projectActivity.isLoading} />
    </ProjectPage>
  );
}
