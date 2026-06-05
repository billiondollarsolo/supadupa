import { useMemo } from "react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
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
import { useDashboardContext } from "../../lib/dashboard-context";
import { databaseSections, type DatabaseSection } from "../../lib/project-config";
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
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = pathname.match(/^\/projects\/[^/]+\/database\/([^/]+)/)?.[1];
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

  return (
    <ProjectPage>
      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Database workspace</p>
            <h2>{activeProject?.ref ?? "Project database"}</h2>
            <p className="mt-1 text-xs text-faint">Use the project sidebar to drill into runtime, pooler, replicas, extensions, jobs, roles, and AI.</p>
          </div>
          <span className="pill healthy">{stats.extensions} extensions</span>
        </div>
        <div className="mt-4">
          <DatabaseSectionSummary
            activeSection={activeSection}
            onSelect={(section) => {
              if (!activeProject) return;
              void navigate({ to: section === "overview" ? `/projects/${activeProject.ref}/database` : `/projects/${activeProject.ref}/database/${section}` });
            }}
            stats={stats}
          />
        </div>
      </section>

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
      {activeSection === "ai" ? <VectorAIPanel project={activeProject} jobs={embeddingJobs.data ?? []} buckets={vectorBuckets.data ?? []} loading={embeddingJobs.isLoading || vectorBuckets.isLoading} /> : null}
    </ProjectPage>
  );
}

function DatabaseSectionSummary({
  activeSection,
  onSelect,
  stats,
}: {
  activeSection: DatabaseSection;
  stats: Record<string, number>;
  onSelect: (section: DatabaseSection) => void;
}) {
  if (activeSection !== "overview") {
    const current = databaseSections.find((section) => section.id === activeSection);
    return (
      <div className="rounded-md border border-border bg-bg p-3">
        <p className="label">{current?.label ?? "Section"}</p>
        <p className="mt-1 text-sm text-muted">{current?.description}</p>
      </div>
    );
  }

  const cards: Array<{ section: DatabaseSection; label: string; value: string; detail: string }> = [
    { section: "config", label: "Runtime", value: "Stack config", detail: "GraphQL, Vault, SSL, cron, queues" },
    { section: "pooler", label: "Connectivity", value: `${stats.replicas} replicas`, detail: "Pooler, replicas, and read routing" },
    { section: "replication", label: "Data movement", value: `${stats.replication} pipelines`, detail: `${stats.branches} branches · ${stats.analytics} analytics buckets` },
    { section: "extensions", label: "Extensions & jobs", value: `${stats.extensions} extensions`, detail: `${stats.cron} cron · ${stats.queues} queues · ${stats.webhooks} webhooks` },
    { section: "schemas", label: "Schema & access", value: `${stats.schemas} versions`, detail: `${stats.roles} database roles` },
    { section: "ai", label: "Vector / AI", value: `${stats.vectorBuckets} buckets`, detail: `${stats.embeddingJobs} embedding jobs` },
  ];

  return (
    <div className="grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
      {cards.map((card) => (
        <button className="rounded-md border border-border bg-bg p-3 text-left transition hover:border-border-strong hover:bg-surface-2" key={card.section} onClick={() => onSelect(card.section)} type="button">
          <p className="label">{card.label}</p>
          <p className="mt-2 truncate text-sm font-medium">{card.value}</p>
          <p className="mt-1 truncate text-xs text-muted">{card.detail}</p>
        </button>
      ))}
    </div>
  );
}
