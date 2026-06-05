import { useDashboardContext } from "../../lib/dashboard-context";
import { BackupPanel, PITRPanel } from "./backups-panels";
import { ProjectPage } from "./layout";

export function ProjectBackupsPage() {
  const { activeFeatureFlags, activeProject, backupPolicy, backups, pitrPolicy, walArchives } = useDashboardContext();
  return (
    <ProjectPage>
      <BackupPanel project={activeProject} backups={backups.data ?? []} policy={backupPolicy.data} loading={backups.isLoading || backupPolicy.isLoading} />
      <PITRPanel project={activeProject} policy={pitrPolicy.data} archives={walArchives.data ?? []} loading={pitrPolicy.isLoading || walArchives.isLoading} enabled={Boolean(activeFeatureFlags.pitr)} />
    </ProjectPage>
  );
}
