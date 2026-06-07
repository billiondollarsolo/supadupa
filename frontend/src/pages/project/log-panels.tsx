import type { FormEvent } from "react";
import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowLeft, Plus, X } from "lucide-react";
import { createProjectLogDrain, deleteProjectLogDrain, streamProjectLogs } from "../../api";
import { DataTable } from "../../components/data-table";
import { formatDateTime, formatTime } from "../../lib/format";
import type { LogDrain, Project, ProjectLog } from "../../types";

export function LogDrainsPanel({ project, drains, item, loading, enabled }: { project?: Project; drains: LogDrain[]; loading: boolean; enabled: boolean; item?: string }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [target, setTarget] = useState("https");
  const [destination, setDestination] = useState("");
  const [credential, setCredential] = useState("");
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["log-drains", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref, nextTarget, config }: { ref: string; nextTarget: string; config: Record<string, string> }) =>
      createProjectLogDrain(ref, { target: nextTarget, config }),
    onSuccess: (_, variables) => {
      setDestination("");
      setCredential("");
      invalidate(variables.ref);
      void navigate({ to: "/projects/$ref/logs", params: { ref: variables.ref } });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => deleteProjectLogDrain(ref, id),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const drainColumns = useMemo<ColumnDef<LogDrain>[]>(
    () => [
      {
        header: "Target",
        accessorKey: "target",
        size: 150,
        cell: ({ row }) => (
          <>
            <p className="cell-main uppercase">{row.original.target}</p>
            <p className="cell-sub font-mono">{row.original.id.slice(0, 12)}</p>
          </>
        ),
      },
      {
        header: "Destination",
        accessorKey: "config",
        size: 360,
        cell: ({ row }) => (
          <>
            <p className="truncate font-mono text-xs text-muted">{row.original.config.url ?? row.original.config.bucket ?? row.original.config.site ?? "configured"}</p>
            {row.original.config.prefix ? <p className="cell-sub font-mono">{row.original.config.prefix}</p> : null}
          </>
        ),
      },
      {
        header: "Created",
        accessorKey: "created_at",
        size: 160,
        cell: ({ row }) => formatDateTime(row.original.created_at),
      },
      {
        header: "",
        id: "actions",
        size: 56,
        cell: ({ row }) => (
          <button className="icon-button" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, id: row.original.id })} title="Delete drain" type="button">
            <X size={14} />
          </button>
        ),
      },
    ],
    [deleteMutation.isPending, project],
  );
  const destinationLabel = target === "s3" ? "Bucket" : target === "datadog" ? "Site" : "URL";
  const credentialLabel = target === "datadog" ? "API key" : target === "s3" ? "Prefix" : "Token";
  const showingCreate = item === "new";

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!enabled || !project || destination.trim().length === 0) {
      return;
    }
    const config: Record<string, string> = target === "s3" ? { bucket: destination } : target === "datadog" ? { api_key: credential, site: destination } : { url: destination };
    if (credential.trim().length > 0 && target !== "datadog" && target !== "s3") {
      config.token = credential;
    }
    if (credential.trim().length > 0 && target === "s3") {
      config.prefix = credential;
    }
    createMutation.mutate({ ref: project.ref, nextTarget: target, config });
  }

  function openCreate() {
    if (!project) return;
    void navigate({ to: "/projects/$ref/logs/$section/$item", params: { ref: project.ref, section: "drains", item: "new" } });
  }

  function closeCreate() {
    if (!project) return;
    void navigate({ to: "/projects/$ref/logs", params: { ref: project.ref } });
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Log drains</p>
          <h2>{showingCreate ? "New drain" : "External exports"}</h2>
          <p className="mt-1 text-sm text-muted">{showingCreate ? "Create one external log export." : "Project log exports to external destinations."}</p>
        </div>
        {showingCreate ? (
          <button className="icon-button" onClick={closeCreate} title="Back to drains" type="button">
            <ArrowLeft size={14} />
          </button>
        ) : (
          <button className="icon-button" disabled={!enabled || !project} onClick={openCreate} title="Add log drain" type="button">
            <Plus size={14} />
          </button>
        )}
      </div>
      {showingCreate ? (
      <form className="mt-4 grid gap-2" onSubmit={submit}>
        {!enabled ? (
          <div className="rounded-md border border-border bg-bg p-3">
            <p className="text-sm font-medium">Log drains disabled</p>
            <p className="mt-1 text-sm text-muted">Enable the log_drains feature flag for this org before adding external exports.</p>
          </div>
        ) : null}
        <div className="grid grid-cols-[120px_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
          <select className="input" disabled={!enabled} value={target} onChange={(event) => setTarget(event.target.value)}>
            <option value="https">HTTPS</option>
            <option value="loki">Loki</option>
            <option value="datadog">Datadog</option>
            <option value="sentry">Sentry</option>
            <option value="axiom">Axiom</option>
            <option value="s3">S3</option>
          </select>
          <input className="input font-mono" disabled={!enabled} placeholder={destinationLabel} value={destination} onChange={(event) => setDestination(event.target.value)} />
        </div>
        <div className="flex gap-2 max-sm:flex-col">
          <input className="input font-mono" disabled={!enabled} placeholder={credentialLabel} value={credential} onChange={(event) => setCredential(event.target.value)} />
          <button className="button secondary justify-center" disabled={!enabled || !project || createMutation.isPending || destination.trim().length === 0} type="submit">
            <Plus size={14} />
            Add
          </button>
        </div>
      </form>
      ) : (
        <div className="mt-4">
          <DataTable columns={drainColumns} data={drains} emptyText={loading ? "Loading log drains..." : "No log drains configured."} minWidth={760} />
        </div>
      )}
      <div className="mt-3 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading log drains...</p> : null}
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </section>
  );
}

export function ProjectLogsPanel({ project, logs, loading }: { project?: Project; logs: ProjectLog[]; loading: boolean }) {
  const [liveLogs, setLiveLogs] = useState<ProjectLog[]>([]);
  const [streamState, setStreamState] = useState<"idle" | "connecting" | "live" | "error">("idle");
  const [streamError, setStreamError] = useState("");
  const projectRef = project?.ref ?? "";
  useEffect(() => {
    setLiveLogs([]);
    setStreamError("");
    if (!projectRef) {
      setStreamState("idle");
      return;
    }
    const controller = new AbortController();
    setStreamState("connecting");
    void streamProjectLogs(projectRef, (entry) => {
      setStreamState("live");
      setLiveLogs((current) => {
        if (current.some((existing) => existing.id === entry.id)) {
          return current;
        }
        return [entry, ...current].slice(0, 100);
      });
    }, controller.signal).catch((error: Error) => {
      if (error.name === "AbortError") {
        return;
      }
      setStreamState("error");
      setStreamError(error.message);
    });
    return () => controller.abort();
  }, [projectRef]);
  const mergedLogs = useMemo(() => {
    const seen = new Set<string>();
    const next: ProjectLog[] = [];
    for (const entry of [...liveLogs, ...logs]) {
      if (seen.has(entry.id)) {
        continue;
      }
      seen.add(entry.id);
      next.push(entry);
    }
    return next.slice(0, 20);
  }, [liveLogs, logs]);
  const streamLabel = streamState === "live" ? "live" : streamState === "connecting" ? "connecting" : streamState === "error" ? "stream offline" : "idle";
  const logColumns = useMemo<ColumnDef<ProjectLog>[]>(
    () => [
      {
        header: "Level",
        accessorKey: "level",
        size: 110,
        cell: ({ row }) => (
          <span className="inline-flex items-center gap-2">
            <span className={`level-dot ${row.original.level}`} />
            <span className={`pill ${row.original.level === "error" ? "error" : row.original.level === "warning" ? "warning" : "healthy"}`}>{row.original.level}</span>
          </span>
        ),
      },
      {
        header: "Event",
        accessorKey: "message",
        size: 420,
        cell: ({ row }) => (
          <>
            <p className="cell-main truncate">{row.original.message}</p>
            {Object.keys(row.original.metadata ?? {}).length > 0 ? (
              <p className="cell-sub truncate font-mono">{formatMetadata(row.original.metadata)}</p>
            ) : null}
          </>
        ),
      },
      {
        header: "Project",
        accessorKey: "project_ref",
        size: 130,
        cell: ({ row }) => <p className="font-mono text-xs text-muted">{row.original.project_ref}</p>,
      },
      {
        header: "Time",
        accessorKey: "created_at",
        size: 120,
        cell: ({ row }) => <time className="text-xs text-faint">{formatTime(row.original.created_at)}</time>,
      },
    ],
    [],
  );

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Logs</p>
          <h2>Project events</h2>
        </div>
        <span className={`pill ${streamState === "live" ? "healthy" : streamState === "error" ? "error" : "provisioning"}`}>{streamLabel}</span>
      </div>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading project logs...</p> : null}
        {streamError ? <p className="text-sm text-danger">{streamError}</p> : null}
        <DataTable columns={logColumns} data={mergedLogs} emptyText={loading ? "Loading project logs..." : "No project logs yet."} minWidth={780} rowClassName={(entry) => entry.level === "error" ? "table-row-error" : entry.level === "warning" ? "table-row-warning" : ""} />
      </div>
    </section>
  );
}

function formatMetadata(metadata: Record<string, string>) {
  return Object.entries(metadata).map(([key, value]) => `${key}=${value}`).join(" · ");
}
