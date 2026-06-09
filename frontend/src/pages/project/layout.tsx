import { type ReactNode } from "react";
import { useDashboardContext } from "../../lib/dashboard-context";
import { LifecyclePanel, RuntimeStatusPanel } from "./side-panels";

export function ProjectPage({ children, side }: { children: ReactNode; side?: ReactNode }) {
  return (
    <div className="grid min-w-0 content-start gap-6">
      {side ? (
        <div className="grid items-start gap-6 grid-cols-[minmax(0,1fr)_360px] max-xl:grid-cols-1">
          <div className="grid min-w-0 content-start gap-6">{children}</div>
          <div className="min-w-0">{side}</div>
        </div>
      ) : (
        <div className="grid min-w-0 content-start gap-6">{children}</div>
      )}
    </div>
  );
}

export function ProjectSidePanel() {
  const { activeOrgId, activeProject } = useDashboardContext();
  return (
    <div className="grid gap-6 content-start">
      <RuntimeStatusPanel project={activeProject} />
      <LifecyclePanel orgId={activeOrgId} project={activeProject} />
    </div>
  );
}
