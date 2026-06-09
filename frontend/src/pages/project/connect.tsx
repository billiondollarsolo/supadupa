import { useDashboardContext } from "../../lib/dashboard-context";
import { ConnectPanel } from "./connect-panels";
import { ProjectPage } from "./layout";

export function ProjectConnectPage() {
  const { activeProject, cliProfile, connect } = useDashboardContext();
  return (
    <ProjectPage>
      <ConnectPanel
        cliProfile={cliProfile.data}
        cliProfileLoading={cliProfile.isLoading}
        project={activeProject}
        payload={connect.data}
        loading={connect.isLoading}
      />
    </ProjectPage>
  );
}
