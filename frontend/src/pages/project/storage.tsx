import { useDashboardContext } from "../../lib/dashboard-context";
import { ProjectPage } from "./layout";
import { CDNPanel, StorageBucketsPanel, StorageConfigPanel } from "./storage-panels";

export function ProjectStoragePage() {
  const { activeProject, cdnInvalidations, cdnPolicy, projectConfig, storageBuckets } = useDashboardContext();
  return (
    <ProjectPage>
      <StorageConfigPanel project={activeProject} config={projectConfig.data} loading={projectConfig.isLoading} />
      <StorageBucketsPanel project={activeProject} buckets={storageBuckets.data ?? []} loading={storageBuckets.isLoading} />
      <CDNPanel project={activeProject} policy={cdnPolicy.data} invalidations={cdnInvalidations.data ?? []} loading={cdnPolicy.isLoading || cdnInvalidations.isLoading} />
    </ProjectPage>
  );
}
