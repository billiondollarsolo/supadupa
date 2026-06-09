import { useMemo } from "react";
import { useRouterState } from "@tanstack/react-router";
import {
  AnalyticsBucketsPanel,
  BranchesPanel,
  DatabaseCronPanel,
  DatabaseExtensionsPanel,
  DatabaseConfigPanel,
  DatabasePoolerPanel,
  DatabaseQueuesPanel,
  DatabaseRolesPanel,
  DatabaseSchemasPanel,
  DatabaseWebhooksPanel,
  ReplicasPanel,
  ReplicationPanel,
  VectorAIPanel,
} from "./database-panels";
import { Sparkles } from "lucide-react";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { EmptyState } from "../../components/ui/empty-state";
import { StatusPill } from "../../components/ui/status-pill";
import { useDashboardContext } from "../../lib/dashboard-context";
import { databaseSections, type DatabaseSection } from "../../lib/project-config";
import { projectSectionFromPathname } from "../../lib/routes";
import { ProjectPage } from "./layout";

export function ProjectDatabasePage() {
  const {
    activeProject,
    activeFeatureFlags,
    analyticsBuckets,
    databaseCronJobs,
    databaseExtensions,
    databasePoolerConfig,
    databaseQueues,
    databaseRoles,
    databaseSchemas,
    databaseWebhooks,
    embeddingJobs,
    hosts,
    projectConfig,
    projectBranches,
    projectReplicaRouting,
    projectReplicas,
    replicationPipelines,
    routeToProject,
    vectorBuckets,
  } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = projectSectionFromPathname(pathname, "database");
  const activeSection: DatabaseSection = databaseSections.some((section) => section.id === selectedSection) ? selectedSection as DatabaseSection : "overview";
  const stats = useMemo(() => ({
    branches: projectBranches.data?.length ?? 0,
    replicas: projectReplicas.data?.length ?? 0,
    replication: replicationPipelines.data?.length ?? 0,
    analytics: analyticsBuckets.data?.length ?? 0,
    extensions: databaseExtensions.data?.filter((extension) => extension.enabled).length ?? 0,
    cron: databaseCronJobs.data?.length ?? 0,
    queues: databaseQueues.data?.length ?? 0,
    webhooks: databaseWebhooks.data?.length ?? 0,
    schemas: databaseSchemas.data?.length ?? 0,
    roles: databaseRoles.data?.length ?? 0,
    embeddingJobs: embeddingJobs.data?.length ?? 0,
    vectorBuckets: vectorBuckets.data?.length ?? 0,
  }), [
    analyticsBuckets.data,
    databaseCronJobs.data,
    databaseExtensions.data,
    databaseQueues.data,
    databaseRoles.data,
    databaseSchemas.data,
    databaseWebhooks.data,
    embeddingJobs.data,
    projectBranches.data,
    projectReplicas.data,
    replicationPipelines.data,
    vectorBuckets.data,
  ]);

  // Truthful rollup: count the resource surfaces that actually have something configured.
  const configuredSurfaces = useMemo(
    () =>
      [
        stats.branches,
        stats.replicas,
        stats.replication,
        stats.analytics,
        stats.extensions,
        stats.cron,
        stats.queues,
        stats.webhooks,
        stats.schemas,
        stats.roles,
        stats.embeddingJobs + stats.vectorBuckets,
      ].filter((count) => count > 0).length,
    [stats],
  );

  return (
    <ProjectPage>
      {activeSection === "overview" ? (
        <AppPanel
          eyebrow="Database workspace"
          title={activeProject?.ref ?? "Project database"}
          description="Posture across configured database surfaces. Use the sidebar to drill into each area."
          actions={<StatusPill tone={configuredSurfaces > 0 ? "info" : "neutral"} label={`${configuredSurfaces} / 11 surfaces configured`} />}
        >
          <div className="mt-4 grid grid-cols-4 gap-2 max-xl:grid-cols-3 max-sm:grid-cols-2">
            <MetricCard label="Replicas" value={stats.replicas} detail="read scaling" />
            <MetricCard label="Branches" value={stats.branches} detail="preview stacks" />
            <MetricCard label="Replication" value={stats.replication} detail="pipelines" />
            <MetricCard label="Analytics" value={stats.analytics} detail="Iceberg buckets" />
            <MetricCard label="Extensions" value={stats.extensions} detail="enabled" />
            <MetricCard label="Cron" value={stats.cron} detail="jobs" />
            <MetricCard label="Queues" value={stats.queues} detail="pgmq" />
            <MetricCard label="Webhooks" value={stats.webhooks} detail="db change hooks" />
            <MetricCard label="Schemas" value={stats.schemas} detail="versions" />
            <MetricCard label="Roles" value={stats.roles} detail="with grants" />
            <MetricCard label="Vector buckets" value={stats.vectorBuckets} detail="similarity stores" />
            <MetricCard label="Embedding jobs" value={stats.embeddingJobs} detail="scheduled" />
          </div>
        </AppPanel>
      ) : null}

      {activeSection === "config" ? <DatabaseConfigPanel project={activeProject} config={projectConfig.data} loading={projectConfig.isLoading} /> : null}
      {activeSection === "pooler" ? <DatabasePoolerPanel project={activeProject} config={databasePoolerConfig.data} loading={databasePoolerConfig.isLoading} /> : null}
      {activeSection === "branches" ? <BranchesPanel project={activeProject} branches={projectBranches.data ?? []} loading={projectBranches.isLoading} onSelect={(ref) => routeToProject(ref)} enabled={Boolean(activeFeatureFlags.preview_branches)} /> : null}
      {activeSection === "replicas" ? <ReplicasPanel project={activeProject} hosts={hosts.data ?? []} replicas={projectReplicas.data ?? []} routing={projectReplicaRouting.data} loading={projectReplicas.isLoading || projectReplicaRouting.isLoading} enabled={Boolean(activeFeatureFlags.read_replicas)} /> : null}
      {activeSection === "replication" ? <ReplicationPanel project={activeProject} pipelines={replicationPipelines.data ?? []} loading={replicationPipelines.isLoading} /> : null}
      {activeSection === "analytics" ? <AnalyticsBucketsPanel project={activeProject} buckets={analyticsBuckets.data ?? []} loading={analyticsBuckets.isLoading} /> : null}
      {activeSection === "extensions" ? <DatabaseExtensionsPanel project={activeProject} extensions={databaseExtensions.data ?? []} loading={databaseExtensions.isLoading} /> : null}
      {activeSection === "cron" ? <DatabaseCronPanel project={activeProject} jobs={databaseCronJobs.data ?? []} loading={databaseCronJobs.isLoading} /> : null}
      {activeSection === "queues" ? <DatabaseQueuesPanel project={activeProject} queues={databaseQueues.data ?? []} loading={databaseQueues.isLoading} /> : null}
      {activeSection === "webhooks" ? <DatabaseWebhooksPanel project={activeProject} webhooks={databaseWebhooks.data ?? []} loading={databaseWebhooks.isLoading} /> : null}
      {activeSection === "schemas" ? <DatabaseSchemasPanel project={activeProject} schemas={databaseSchemas.data ?? []} loading={databaseSchemas.isLoading} /> : null}
      {activeSection === "roles" ? <DatabaseRolesPanel project={activeProject} roles={databaseRoles.data ?? []} loading={databaseRoles.isLoading} /> : null}
      {activeSection === "ai" ? (
        activeFeatureFlags.ai_integrations ? (
          <VectorAIPanel project={activeProject} jobs={embeddingJobs.data ?? []} buckets={vectorBuckets.data ?? []} loading={embeddingJobs.isLoading || vectorBuckets.isLoading} />
        ) : (
          <AppPanel eyebrow="Database" title="Vector / AI">
            <div className="mt-4">
              <EmptyState icon={Sparkles} title="AI integrations are off" description="Enable “AI integrations” in Settings → Features to use vector buckets and embedding jobs." />
            </div>
          </AppPanel>
        )
      ) : null}
    </ProjectPage>
  );
}
