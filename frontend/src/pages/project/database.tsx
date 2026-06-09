import { useMemo } from "react";
import { useRouterState } from "@tanstack/react-router";
import { BranchesPanel, DatabasePoolerPanel, ReplicasPanel, ReplicationPanel } from "./database-panels";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { StatusPill } from "../../components/ui/status-pill";
import { useDashboardContext } from "../../lib/dashboard-context";
import { databaseSections, type DatabaseSection } from "../../lib/project-config";
import { projectSectionFromPathname } from "../../lib/routes";
import { ProjectPage } from "./layout";

// Database tab is infrastructure-only: connection pooling, read replicas,
// preview branches, and logical replication. Schema, roles, extensions, cron,
// queues, webhooks, and vector/AI are data-plane surfaces owned by Studio.
export function ProjectDatabasePage() {
  const {
    activeProject,
    activeFeatureFlags,
    databasePoolerConfig,
    hosts,
    projectBranches,
    projectReplicaRouting,
    projectReplicas,
    replicationPipelines,
    routeToProject,
  } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = projectSectionFromPathname(pathname, "database");
  const activeSection: DatabaseSection = databaseSections.some((section) => section.id === selectedSection) ? (selectedSection as DatabaseSection) : "overview";
  const stats = useMemo(
    () => ({
      replicas: projectReplicas.data?.length ?? 0,
      branches: projectBranches.data?.length ?? 0,
      replication: replicationPipelines.data?.length ?? 0,
    }),
    [projectBranches.data, projectReplicas.data, replicationPipelines.data],
  );
  const configuredSurfaces = [stats.replicas, stats.branches, stats.replication].filter((count) => count > 0).length;

  return (
    <ProjectPage>
      {activeSection === "overview" ? (
        <AppPanel
          eyebrow="Database infrastructure"
          title={activeProject?.ref ?? "Project database"}
          description="Connection pooling, read replicas, preview branches, and replication. Tables, SQL, roles, and extensions live in Studio."
          actions={<StatusPill tone={configuredSurfaces > 0 ? "info" : "neutral"} label={`${configuredSurfaces} / 3 surfaces configured`} />}
        >
          <div className="mt-4 grid grid-cols-3 gap-2 max-sm:grid-cols-1">
            <MetricCard label="Replicas" value={stats.replicas} detail="read scaling" />
            <MetricCard label="Branches" value={stats.branches} detail="preview stacks" />
            <MetricCard label="Replication" value={stats.replication} detail="pipelines" />
          </div>
        </AppPanel>
      ) : null}

      {activeSection === "pooler" ? <DatabasePoolerPanel project={activeProject} config={databasePoolerConfig.data} loading={databasePoolerConfig.isLoading} /> : null}
      {activeSection === "replicas" ? <ReplicasPanel project={activeProject} hosts={hosts.data ?? []} replicas={projectReplicas.data ?? []} routing={projectReplicaRouting.data} loading={projectReplicas.isLoading || projectReplicaRouting.isLoading} enabled={Boolean(activeFeatureFlags.read_replicas)} /> : null}
      {activeSection === "branches" ? <BranchesPanel project={activeProject} branches={projectBranches.data ?? []} loading={projectBranches.isLoading} onSelect={(ref) => routeToProject(ref)} enabled={Boolean(activeFeatureFlags.preview_branches)} /> : null}
      {activeSection === "replication" ? <ReplicationPanel project={activeProject} pipelines={replicationPipelines.data ?? []} loading={replicationPipelines.isLoading} /> : null}
    </ProjectPage>
  );
}
