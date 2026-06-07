import { useDashboardContext } from "../../lib/dashboard-context";
import { BackupPanel, PITRPanel } from "./backups-panels";
import { ProjectPage } from "./layout";

export function ProjectBackupsPage() {
  const { activeFeatureFlags, activeProject, backupPolicy, backups, backupStorageTargets, pitrPolicy, recoverability, walArchives } = useDashboardContext();
  return (
    <ProjectPage>
      <BackupPanel project={activeProject} backups={backups.data ?? []} policy={backupPolicy.data} recoverability={recoverability.data} storageTargets={backupStorageTargets.data ?? []} loading={backups.isLoading || backupPolicy.isLoading || recoverability.isLoading || backupStorageTargets.isLoading} />
      <PITRPanel project={activeProject} policy={pitrPolicy.data} recoverability={recoverability.data} archives={walArchives.data ?? []} loading={pitrPolicy.isLoading || walArchives.isLoading || recoverability.isLoading} enabled={Boolean(activeFeatureFlags.pitr)} />
    </ProjectPage>
  );
}
