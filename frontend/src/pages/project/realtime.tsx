import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Database, RadioTower, Save, Shield } from "lucide-react";
import { updateProjectConfig } from "../../api";
import { useDashboardContext } from "../../lib/dashboard-context";
import { formatDateTime } from "../../lib/format";
import type { Project, ProjectConfig } from "../../types";
import { ServicesPanel } from "./config-panels";
import { ProjectPage } from "./layout";

const realtimeCapabilities = [
  { key: "postgres_changes_enabled", title: "Postgres changes", detail: "WAL-backed table change streams", icon: Database },
  { key: "broadcast_enabled", title: "Broadcast", detail: "Client and server channel messages", icon: RadioTower },
  { key: "presence_enabled", title: "Presence", detail: "Online state synchronization", icon: RadioTower },
  { key: "broadcast_replay", title: "Broadcast replay", detail: "Persisted replay for late subscribers", icon: RadioTower },
  { key: "broadcast_from_database", title: "Broadcast from database", detail: "Database-triggered broadcasts", icon: Database },
];

export function ProjectRealtimePage() {
  const { activeProject, projectConfig, projectServices } = useDashboardContext();
  return (
    <ProjectPage>
      <RealtimePanel project={activeProject} config={projectConfig.data} loading={projectConfig.isLoading} />
      <ServicesPanel project={activeProject} services={projectServices.data} loading={projectServices.isLoading} />
    </ProjectPage>
  );
}

function RealtimePanel({ project, config, loading }: { project?: Project; config?: ProjectConfig; loading: boolean }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const configKey = `${project?.ref ?? ""}:${config?.updated_at ?? ""}`;
  useEffect(() => {
    setDraft(config?.config ?? {});
  }, [configKey, config]);
  const mutation = useMutation({
    mutationFn: ({ ref, values }: { ref: string; values: Record<string, string> }) => updateProjectConfig(ref, "realtime", values),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-config", variables.ref, "realtime"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const updateValue = (key: string, enabled: boolean) => {
    setDraft((current) => ({ ...current, [key]: enabled ? "true" : "false" }));
  };

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Realtime</p>
          <h2>Channels and changes</h2>
        </div>
        <RadioTower size={15} className="text-faint" />
      </div>
      <div className="mt-4 grid gap-3">
        {loading ? <p className="text-sm text-muted">Loading Realtime configuration...</p> : null}
        {!project ? <p className="text-sm text-muted">Select a project to manage Realtime.</p> : null}
        {project && !loading ? (
          <>
            <div className="grid grid-cols-2 gap-2 max-md:grid-cols-1">
              {realtimeCapabilities.map((capability) => {
                const Icon = capability.icon;
                const enabled = (draft[capability.key] ?? "false") === "true";
                return (
                  <label className="config-toggle" key={capability.key}>
                    <span className="flex min-w-0 items-center gap-3">
                      <Icon size={15} className="text-faint" />
                      <span className="min-w-0">
                        <span className="block truncate text-sm font-medium">{capability.title}</span>
                        <span className="block truncate text-xs text-faint">{capability.detail}</span>
                      </span>
                    </span>
                    <input checked={enabled} onChange={(event) => updateValue(capability.key, event.target.checked)} type="checkbox" />
                  </label>
                );
              })}
            </div>
            <div className="grid grid-cols-2 gap-2 max-md:grid-cols-1">
              <div className="metric-cell">
                <div className="flex items-center gap-2 text-faint">
                  <Shield size={14} />
                  <p className="label">Broadcast authorization</p>
                </div>
                <p className="mt-2 text-sm font-medium">RLS-backed</p>
                <p className="mt-1 text-xs text-muted">Realtime channel authorization is available through upstream policies.</p>
              </div>
              <div className="metric-cell">
                <div className="flex items-center gap-2 text-faint">
                  <Shield size={14} />
                  <p className="label">Presence authorization</p>
                </div>
                <p className="mt-2 text-sm font-medium">RLS-backed</p>
                <p className="mt-1 text-xs text-muted">Presence authorization follows the same channel policy model.</p>
              </div>
            </div>
            <div className="flex items-center justify-between gap-3">
              <p className="truncate text-xs text-muted">{config ? `Last changed ${formatDateTime(config.updated_at)}` : "Realtime desired state is loaded from project config."}</p>
              <button className="button secondary" disabled={!project || mutation.isPending} onClick={() => mutation.mutate({ ref: project.ref, values: draft })} type="button">
                <Save size={14} />
                Save Realtime
              </button>
            </div>
          </>
        ) : null}
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </div>
    </section>
  );
}
