import type { FormEvent } from "react";
import { Fragment, useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { ChevronDown, ChevronRight, Plus, Search, X } from "lucide-react";
import { createProjectLogDrain, deleteProjectLogDrain, streamProjectLogs } from "../../api";
import { AppPanel } from "../../components/app/app-panel";
import { DataTable } from "../../components/data-table";
import { Button } from "../../components/ui/button";
import { EmptyState } from "../../components/ui/empty-state";
import { Field } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import { formatDateTime, formatRelativeTime, formatTime } from "../../lib/format";
import { statusTone } from "../../lib/status";
import type { LogDrain, Project, ProjectLog } from "../../types";

// Per-target labels so the generic destination/credential pair reads correctly
// for the selected target instead of a vague "URL"/"Token".
const drainTargetFields: Record<string, { destination: { label: string; hint: string; placeholder: string }; credential: { label: string; hint: string; placeholder: string } | null }> = {
  https: { destination: { label: "Endpoint URL", hint: "HTTPS endpoint that receives log batches", placeholder: "https://logs.example.com/ingest" }, credential: { label: "Bearer token", hint: "Sent as the Authorization header", placeholder: "token" } },
  loki: { destination: { label: "Push URL", hint: "Loki push API URL", placeholder: "https://loki.example.com/loki/api/v1/push" }, credential: { label: "Token", hint: "Optional bearer token", placeholder: "token" } },
  datadog: { destination: { label: "Datadog site", hint: "e.g. datadoghq.com or us5.datadoghq.com", placeholder: "datadoghq.com" }, credential: { label: "API key", hint: "Datadog API key", placeholder: "api key" } },
  sentry: { destination: { label: "DSN", hint: "Sentry ingest DSN", placeholder: "https://key@o0.ingest.sentry.io/0" }, credential: { label: "Token", hint: "Optional auth token", placeholder: "token" } },
  axiom: { destination: { label: "Ingest URL", hint: "Axiom dataset ingest URL", placeholder: "https://api.axiom.co/v1/datasets/logs/ingest" }, credential: { label: "API token", hint: "Axiom API token", placeholder: "token" } },
  s3: { destination: { label: "Bucket", hint: "S3 bucket name for archived logs", placeholder: "my-log-bucket" }, credential: { label: "Key prefix", hint: "Optional object key prefix", placeholder: "logs/" } },
};

// Turn a stored drain config into a human destination summary instead of the
// opaque "configured" fallback.
function drainDestination(drain: LogDrain): string {
  const config = drain.config ?? {};
  return config.url || config.bucket || config.site || config.dsn || config.endpoint || `${drain.target} export`;
}

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
            <p className="truncate font-mono text-xs text-muted">{drainDestination(row.original)}</p>
            {row.original.config.prefix ? <p className="cell-sub font-mono">{row.original.config.prefix}</p> : null}
          </>
        ),
      },
      {
        header: "Created",
        accessorKey: "created_at",
        size: 160,
        cell: ({ row }) => formatRelativeTime(row.original.created_at),
      },
      {
        header: "",
        id: "actions",
        size: 56,
        cell: ({ row }) => (
          <Button variant="ghost" size="icon" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, id: row.original.id })} title="Delete drain" type="button">
            <X size={14} />
          </Button>
        ),
      },
    ],
    [deleteMutation.isPending, project],
  );
  const fields = drainTargetFields[target] ?? drainTargetFields.https;
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
    <AppPanel
      eyebrow="Log drains"
      title="External exports"
      description="Ship this project's logs to external destinations like Datadog, Loki, or an S3 bucket."
      actions={
        <Button variant="secondary" disabled={!enabled || !project} onClick={() => (showingCreate ? closeCreate() : openCreate())} type="button">
          {showingCreate ? <ChevronDown size={14} /> : <Plus size={14} />}
          Add drain
        </Button>
      }
    >
      {/* Inline disclosure form so existing drains stay visible while adding. */}
      {showingCreate ? (
        <form className="mt-4 grid gap-2 rounded-md border border-border bg-bg p-3" onSubmit={submit}>
          {!enabled ? (
            <div className="rounded-md border border-border bg-surface p-3">
              <p className="text-sm font-medium">Log drains disabled</p>
              <p className="mt-1 text-sm text-muted">Enable the log_drains feature flag for this org before adding external exports.</p>
            </div>
          ) : null}
          <div className="grid grid-cols-[160px_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
            <Field label="Target type" hint="Where logs are shipped">
              <NativeSelect disabled={!enabled} value={target} onChange={(event) => setTarget(event.target.value)}>
                <option value="https">HTTPS</option>
                <option value="loki">Loki</option>
                <option value="datadog">Datadog</option>
                <option value="sentry">Sentry</option>
                <option value="axiom">Axiom</option>
                <option value="s3">S3</option>
              </NativeSelect>
            </Field>
            <Field label={fields.destination.label} hint={fields.destination.hint}>
              <Input className="font-mono" disabled={!enabled} placeholder={fields.destination.placeholder} value={destination} onChange={(event) => setDestination(event.target.value)} />
            </Field>
          </div>
          {fields.credential ? (
            <Field label={fields.credential.label} hint={fields.credential.hint}>
              <Input className="font-mono" disabled={!enabled} placeholder={fields.credential.placeholder} value={credential} onChange={(event) => setCredential(event.target.value)} />
            </Field>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" disabled={createMutation.isPending} onClick={closeCreate} type="button">Cancel</Button>
            <Button disabled={!enabled || !project || createMutation.isPending || destination.trim().length === 0} type="submit">
              <Plus size={14} />
              Add drain
            </Button>
          </div>
        </form>
      ) : null}

      <div className="mt-4 grid gap-2">
        {!loading && drains.length === 0 ? (
          <EmptyState
            title="No log drains configured"
            description="Ship this project's logs to an external destination like Datadog, Loki, or an S3 bucket."
            action={
              <Button variant="secondary" disabled={!enabled || !project} onClick={openCreate} type="button">
                <Plus size={14} />
                Add drain
              </Button>
            }
          />
        ) : (
          <DataTable columns={drainColumns} data={drains} emptyText={loading ? "Loading log drains..." : ""} minWidth={760} />
        )}
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

const LOG_LEVELS = ["error", "warning", "info"] as const;

function streamPill(state: "idle" | "connecting" | "live" | "error"): { tone: "success" | "info" | "danger" | "neutral"; label: string } {
  switch (state) {
    case "live":
      return { tone: "success", label: "streaming live" };
    case "connecting":
      return { tone: "info", label: "connecting" };
    case "error":
      return { tone: "danger", label: "stream offline" };
    default:
      return { tone: "neutral", label: "idle" };
  }
}

export function ProjectLogsPanel({ project, logs, loading }: { project?: Project; logs: ProjectLog[]; loading: boolean }) {
  const [liveLogs, setLiveLogs] = useState<ProjectLog[]>([]);
  const [streamState, setStreamState] = useState<"idle" | "connecting" | "live" | "error">("idle");
  const [streamError, setStreamError] = useState("");
  const [levelFilter, setLevelFilter] = useState("all");
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
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
        return [entry, ...current].slice(0, 200);
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

  // Merge live + historical, dedupe, keep a usable scrollback rather than 20 rows.
  const mergedLogs = useMemo(() => {
    const seen = new Set<string>();
    const next: ProjectLog[] = [];
    for (const entry of [...liveLogs, ...logs]) {
      if (seen.has(entry.id)) continue;
      seen.add(entry.id);
      next.push(entry);
    }
    return next.slice(0, 200);
  }, [liveLogs, logs]);

  const filtered = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    return mergedLogs.filter((entry) => {
      if (levelFilter !== "all" && entry.level !== levelFilter) return false;
      if (!normalized) return true;
      const haystack = [entry.message, ...Object.entries(entry.metadata ?? {}).flatMap(([key, value]) => [key, value])];
      return haystack.some((value) => value.toLowerCase().includes(normalized));
    });
  }, [mergedLogs, levelFilter, query]);

  const pill = streamPill(streamState);

  return (
    <AppPanel eyebrow="Logs" title="Project events" actions={<StatusPill tone={pill.tone} label={pill.label} />}>
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" size={14} />
          <Input aria-label="Search logs" className="pl-8" onChange={(event) => setQuery(event.target.value)} placeholder="Search message or metadata" value={query} />
        </div>
        <NativeSelect aria-label="Filter by level" className="w-auto" onChange={(event) => setLevelFilter(event.target.value)} value={levelFilter}>
          <option value="all">All levels</option>
          {LOG_LEVELS.map((level) => (
            <option key={level} value={level}>{level}</option>
          ))}
        </NativeSelect>
        <span className="ml-auto text-xs text-faint">Showing {filtered.length} of {mergedLogs.length} events</span>
      </div>

      <div className="mt-3 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading project logs...</p> : null}
        {streamError ? <p className="text-sm text-danger">{streamError}</p> : null}
        {filtered.length === 0 ? (
          mergedLogs.length === 0 ? (
            <EmptyState title="No project logs yet" description="Function invocations, config changes, and backup jobs will stream here as they happen." />
          ) : (
            <p className="text-sm text-muted">No logs match the filter.</p>
          )
        ) : (
          <div className="data-table-wrap max-h-[520px] overflow-auto">
            <table className="data-table w-full" style={{ minWidth: 640 }}>
              <thead>
                <tr>
                  <th style={{ width: 36 }} />
                  <th style={{ width: 110 }}>Level</th>
                  <th style={{ width: 420 }}>Event</th>
                  <th style={{ width: 120 }}>Time</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((entry) => {
                  const metaEntries = Object.entries(entry.metadata ?? {});
                  const hasMeta = metaEntries.length > 0;
                  const open = Boolean(expanded[entry.id]);
                  const rowClass = entry.level === "error" ? "table-row-error" : entry.level === "warning" ? "table-row-warning" : "";
                  return (
                    <Fragment key={entry.id}>
                      <tr className={rowClass}>
                        <td>
                          {hasMeta ? (
                            <Button aria-expanded={open} aria-label={open ? "Hide metadata" : "Show metadata"} variant="ghost" size="icon" onClick={() => setExpanded((prev) => ({ ...prev, [entry.id]: !prev[entry.id] }))} type="button">
                              {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                            </Button>
                          ) : null}
                        </td>
                        <td>
                          <span className="inline-flex items-center gap-2">
                            <span className={`level-dot ${entry.level}`} />
                            <StatusPill tone={statusTone(entry.level)} label={entry.level} />
                          </span>
                        </td>
                        <td>
                          <p className="cell-main truncate">{entry.message}</p>
                          {hasMeta && !open ? <p className="cell-sub truncate font-mono">{metaEntries.length} metadata {metaEntries.length === 1 ? "field" : "fields"}</p> : null}
                        </td>
                        <td><time className="text-xs text-faint" title={formatDateTime(entry.created_at)}>{formatTime(entry.created_at)}</time></td>
                      </tr>
                      {open && hasMeta ? (
                        <tr className={rowClass}>
                          <td />
                          <td colSpan={3}>
                            <div className="grid grid-cols-[160px_minmax(0,1fr)] gap-x-3 gap-y-1 rounded-md border border-border bg-bg p-3 text-xs">
                              {metaEntries.map(([key, value]) => (
                                <Fragment key={key}>
                                  <span className="text-faint">{key}</span>
                                  <span className="font-mono break-all text-muted">{value}</span>
                                </Fragment>
                              ))}
                            </div>
                          </td>
                        </tr>
                      ) : null}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </AppPanel>
  );
}
