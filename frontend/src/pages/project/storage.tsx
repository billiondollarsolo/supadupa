import { useRouterState } from "@tanstack/react-router";
import { useDashboardContext } from "../../lib/dashboard-context";
import { projectSubrouteFromPathname } from "../../lib/routes";
import { ProjectPage } from "./layout";
import { CDNPanel, StorageBucketsPanel, StorageConfigPanel } from "./storage-panels";

export function ProjectStoragePage() {
  const { activeProject, cdnInvalidations, cdnPolicy, projectConfig, storageBuckets } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const { section: storageSection, item: storageItem } = projectSubrouteFromPathname(pathname, "storage");
  const showingBucketDetail = storageSection === "buckets" && Boolean(storageItem);

  const buckets = storageBuckets.data ?? [];

  return (
    <ProjectPage>
      <StorageBucketsPanel project={activeProject} buckets={buckets} loading={storageBuckets.isLoading} item={storageItem} />

      {showingBucketDetail ? null : (
        <>
          <StorageConfigPanel project={activeProject} config={projectConfig.data} loading={projectConfig.isLoading} />
          <CDNPanel project={activeProject} policy={cdnPolicy.data} invalidations={cdnInvalidations.data ?? []} loading={cdnPolicy.isLoading || cdnInvalidations.isLoading} />
        </>
      )}
    </ProjectPage>
  );
}
