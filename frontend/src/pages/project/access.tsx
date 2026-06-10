import { useMemo } from "react";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { StatusPill } from "../../components/ui/status-pill";
import { useDashboardContext } from "../../lib/dashboard-context";
import { AccessScopePanel, ProjectAccessPanel } from "./auth-panels";
import { ProjectPage } from "./layout";

// Project access control (Supadupa RBAC: teams + role grants). This is a
// control-plane concern Studio does not provide — Studio manages end-user auth
// (the project's own users/providers), while this governs who in the platform
// can operate the project.
export function ProjectAccessPage() {
  const { activeProject, projectAccess, teams, orgsEnabled } = useDashboardContext();
  const stats = useMemo(
    () => ({ grants: projectAccess.data?.length ?? 0, teams: teams.data?.length ?? 0 }),
    [projectAccess.data, teams.data],
  );

  return (
    <ProjectPage>
      <AppPanel
        eyebrow="Project access"
        title={activeProject?.name ?? "Project access"}
        actions={<StatusPill label={`${stats.grants} grant${stats.grants === 1 ? "" : "s"}`} tone={stats.grants > 0 ? "success" : "neutral"} />}
      >
        {activeProject ? <p className="-mt-2 truncate font-mono text-xs text-faint">{activeProject.ref}</p> : null}
        <div className="mt-4 grid grid-cols-2 gap-2 max-sm:grid-cols-1">
          <MetricCard label="Project grants" value={stats.grants} detail="team/user role bindings" tone={stats.grants > 0 ? "success" : "default"} />
          <MetricCard label="Teams available" value={stats.teams} detail="scoped RBAC teams" />
        </div>
        <p className="mt-3 text-xs leading-5 text-muted">
          This governs who in the platform can operate this project. End-user authentication for the
          project's own application (users, providers, policies) is managed in Studio.
        </p>
      </AppPanel>

      <AccessScopePanel project={activeProject} teams={teams.data ?? []} grants={projectAccess.data ?? []} />
      <ProjectAccessPanel project={activeProject} teams={teams.data ?? []} grants={projectAccess.data ?? []} loading={projectAccess.isLoading} orgsEnabled={orgsEnabled} />
    </ProjectPage>
  );
}
