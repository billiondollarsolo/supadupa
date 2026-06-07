import { useRouterState } from "@tanstack/react-router";
import { useDashboardContext } from "../../lib/dashboard-context";
import { ProjectPage } from "./layout";
import { CDNPanel, StorageBucketsPanel, StorageConfigPanel } from "./storage-panels";

export function ProjectStoragePage() {
  const { activeProject, cdnInvalidations, cdnPolicy, projectConfig, storageBuckets } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const storageMatch = pathname.match(/^\/projects\/[^/]+\/storage(?:\/([^/]+))?(?:\/([^/]+))?/);
  const storageSection = storageMatch?.[1];
  const storageItem = storageMatch?.[2];
  const showingBucketDetail = storageSection === "buckets" && Boolean(storageItem);

  return (
    <ProjectPage>
      {showingBucketDetail ? null : <StorageConfigPanel project={activeProject} config={projectConfig.data} loading={projectConfig.isLoading} />}
      <StorageBucketsPanel project={activeProject} buckets={storageBuckets.data ?? []} loading={storageBuckets.isLoading} item={storageItem} />
      {showingBucketDetail ? null : <CDNPanel project={activeProject} policy={cdnPolicy.data} invalidations={cdnInvalidations.data ?? []} loading={cdnPolicy.isLoading || cdnInvalidations.isLoading} />}
    </ProjectPage>
  );
}
