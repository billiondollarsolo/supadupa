import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Database, RadioTower, Save, Shield } from "lucide-react";
import { updateProjectConfig } from "../../api";
import { AppPanel } from "../../components/app/app-panel";
import { Button } from "../../components/ui/button";
import { StatusPill } from "../../components/ui/status-pill";
import { useDashboardContext } from "../../lib/dashboard-context";
import { formatRelativeTime } from "../../lib/format";
import { projectPath } from "../../lib/routes";
import type { Project, ProjectConfig } from "../../types";
import { ProjectPage } from "./layout";

const realtimeCapabilities = [
  { key: "postgres_changes_enabled", title: "Postgres changes", detail: "WAL-backed table change streams", icon: Database },
  { key: "broadcast_enabled", title: "Broadcast", detail: "Client and server channel messages", icon: RadioTower },
  { key: "presence_enabled", title: "Presence", detail: "Online state synchronization", icon: RadioTower },
  { key: "broadcast_replay", title: "Broadcast replay", detail: "Persisted replay for late subscribers", icon: RadioTower },
  { key: "broadcast_from_database", title: "Broadcast from database", detail: "Database-triggered broadcasts", icon: Database },
];

export function ProjectRealtimePage() {
  const { activeProject, projectConfig } = useDashboardContext();
  return (
    <ProjectPage>
      <RealtimePanel project={activeProject} config={projectConfig.data} loading={projectConfig.isLoading} />
    </ProjectPage>
  );
}

function RealtimePanel({ project, config, loading }: { project?: Project; config?: ProjectConfig; loading: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
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

  // Dirty when the draft diverges from the persisted config across any toggle.
  const dirty = useMemo(() => {
    const saved = config?.config ?? {};
    return realtimeCapabilities.some((capability) => (draft[capability.key] ?? "false") !== (saved[capability.key] ?? "false"));
  }, [draft, config]);

  return (
    <AppPanel eyebrow="Realtime" title="Channels and changes" actions={<RadioTower size={15} className="text-faint" />}>
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
            <div className="metric-cell">
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-2 text-faint">
                  <Shield size={14} />
                  <p className="label">Channel authorization</p>
                </div>
                <Button variant="secondary" className="h-8 min-h-8" onClick={() => void navigate({ to: projectPath(project.ref, "database", "roles") })} type="button">
                  Manage roles &amp; RLS
                </Button>
              </div>
              <p className="mt-2 text-xs text-muted" title="Broadcast and presence access are enforced by Row Level Security policies on the realtime schema. Manage the roles those policies reference in the database.">Access is enforced by RLS policies on the realtime schema.</p>
            </div>
            <div className="flex items-center justify-between gap-3">
              <span className="flex min-w-0 items-center gap-2">
                <StatusPill tone={dirty ? "warning" : "success"} label={dirty ? "unsaved changes" : "saved"} />
                <span className="truncate text-xs text-muted">{config ? `Last changed ${formatRelativeTime(config.updated_at)}` : "Loaded from project config"}</span>
              </span>
              <Button variant="secondary" disabled={!project || mutation.isPending || !dirty} onClick={() => mutation.mutate({ ref: project.ref, values: draft })} type="button">
                <Save size={14} />
                Save Realtime
              </Button>
            </div>
          </>
        ) : null}
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}
