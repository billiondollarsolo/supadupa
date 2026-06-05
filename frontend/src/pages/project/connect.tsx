import { useDashboardContext } from "../../lib/dashboard-context";
import { ConnectPanel } from "./connect-panels";
import { ProjectPage } from "./layout";

export function ProjectConnectPage() {
  const { activeProject, cliProfile, connect, routeToProject, setConfigArea } = useDashboardContext();
  return (
    <ProjectPage>
      <ConnectPanel
        cliProfile={cliProfile.data}
        cliProfileLoading={cliProfile.isLoading}
        project={activeProject}
        payload={connect.data}
        loading={connect.isLoading}
        onOpenProjectTab={(tab) => activeProject && routeToProject(activeProject.ref, tab)}
        onOpenConfigArea={(area) => {
          setConfigArea(area);
          if (activeProject) {
            routeToProject(activeProject.ref, "config");
          }
        }}
      />
    </ProjectPage>
  );
}
