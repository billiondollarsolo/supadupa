import { FormEvent, useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Boxes, Globe2, RadioTower, Save, SlidersHorizontal, X } from "lucide-react";
import {
  createProjectFunctionRegion,
  createProjectFunctionStorageMount,
  deleteProjectFunction,
  deleteProjectFunctionRegion,
  deleteProjectFunctionStorageMount,
  deployProjectFunction,
  updateProjectConfig,
} from "../../api";
import { DataTable } from "../../components/data-table";
import { formatBytes, formatDateTime } from "../../lib/format";
import { parseKeyValueLines } from "../../lib/parse";
import type { Project, ProjectConfig, ProjectFunction, ProjectFunctionRegion, ProjectFunctionStorageMount } from "../../types";

const functionRuntimeFlags = [
  { key: "runtime_enabled", label: "Runtime", detail: "Deno edge runtime" },
  { key: "verify_jwt_by_default", label: "Verify JWT", detail: "default auth guard" },
  { key: "secret_sync_enabled", label: "Secret sync", detail: "function secrets" },
] as const;

export function FunctionsConfigPanel({ project, config, loading, enabled }: { project?: Project; config?: ProjectConfig; loading: boolean; enabled: boolean }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const configKey = `${project?.ref ?? ""}:${config?.updated_at ?? ""}`;

  useEffect(() => {
    setDraft(config?.config ?? {});
  }, [configKey, config]);

  const mutation = useMutation({
    mutationFn: ({ ref, values }: { ref: string; values: Record<string, string> }) => updateProjectConfig(ref, "functions", values),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-config", variables.ref, "functions"] });
      void queryClient.invalidateQueries({ queryKey: ["project-functions", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!enabled || !project) {
      return;
    }
    mutation.mutate({
      ref: project.ref,
      values: {
        runtime_enabled: draft.runtime_enabled || "true",
        verify_jwt_by_default: draft.verify_jwt_by_default || "true",
        worker_timeout_ms: draft.worker_timeout_ms || "60000",
        import_map: draft.import_map || "",
        deployment_policy: draft.deployment_policy || "manual",
        secret_sync_enabled: draft.secret_sync_enabled || "true",
      },
    });
  }

  const setValue = (key: string, value: string) => setDraft((current) => ({ ...current, [key]: value }));
  const setFlag = (key: string, enabled: boolean) => setValue(key, enabled ? "true" : "false");

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Functions</p>
          <h2>Runtime settings</h2>
        </div>
        <SlidersHorizontal size={15} className="text-faint" />
      </div>
      <form className="mt-4 grid gap-3" onSubmit={submit}>
        {!enabled ? (
          <div className="rounded-md border border-border bg-bg p-3">
            <p className="text-sm font-medium">Edge Functions disabled</p>
            <p className="mt-1 text-sm text-muted">Enable the edge_functions feature flag for this org before changing runtime settings.</p>
          </div>
        ) : null}
        <div className="grid grid-cols-3 gap-2 max-lg:grid-cols-1">
          {functionRuntimeFlags.map((flag) => {
            const flagEnabled = (draft[flag.key] ?? "true") === "true";
            return (
              <label className="config-toggle" key={flag.key}>
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{flag.label}</p>
                  <p className="truncate font-mono text-xs text-muted">{flag.detail}</p>
                </div>
                <input checked={flagEnabled} disabled={!enabled} onChange={(event) => setFlag(flag.key, event.target.checked)} type="checkbox" />
              </label>
            );
          })}
        </div>
        <div className="grid grid-cols-[180px_180px_minmax(0,1fr)] gap-2 max-md:grid-cols-1">
          <select className="input" disabled={!enabled} value={draft.deployment_policy ?? "manual"} onChange={(event) => setValue("deployment_policy", event.target.value)}>
            <option value="manual">Manual</option>
            <option value="ci">CI managed</option>
            <option value="locked">Locked</option>
          </select>
          <input
            className="input"
            disabled={!enabled}
            min={100}
            max={300000}
            step={100}
            type="number"
            value={draft.worker_timeout_ms ?? "60000"}
            onChange={(event) => setValue("worker_timeout_ms", event.target.value)}
          />
          <input className="input font-mono" disabled={!enabled} placeholder="import_map.json" value={draft.import_map ?? ""} onChange={(event) => setValue("import_map", event.target.value)} />
        </div>
        <div className="usage-row">
          <p className="text-xs text-muted">{loading ? "Loading function settings..." : config?.updated_at ? `Updated ${formatDateTime(config.updated_at)}` : "Function settings not saved yet."}</p>
          <button className="button secondary" disabled={!enabled || !project || mutation.isPending} type="submit">
            <Save size={14} />
            Save functions
          </button>
        </div>
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </form>
    </section>
  );
}

export function FunctionsPanel({ project, functions, regions, mounts, loading, enabled }: { project?: Project; functions: ProjectFunction[]; regions: ProjectFunctionRegion[]; mounts: ProjectFunctionStorageMount[]; loading: boolean; enabled: boolean; item?: string }) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("hello-api");
  const [entrypoint, setEntrypoint] = useState("index.ts");
  const [verifyJwt, setVerifyJwt] = useState(true);
  const [source, setSource] = useState("Deno.serve(() => new Response(\"ok\"));");
  const [secrets, setSecrets] = useState("");
  const [regionForm, setRegionForm] = useState({ function_name: "hello-api", host_id: "", region: "local", routing_policy: "nearest" });
  const [mountForm, setMountForm] = useState({ function_name: "hello-api", bucket_name: "assets", mount_path: "/mnt/assets", prefix: "", env_alias: "ASSETS_MOUNT", read_only: true });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["project-functions", ref] });
    void queryClient.invalidateQueries({ queryKey: ["function-regions", ref] });
    void queryClient.invalidateQueries({ queryKey: ["function-storage-mounts", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const deployMutation = useMutation({
    mutationFn: ({ ref, input }: { ref: string; input: { name: string; entrypoint: string; verify_jwt: boolean; source: string; secrets: Record<string, string> } }) =>
      deployProjectFunction(ref, input),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, nextName }: { ref: string; nextName: string }) => deleteProjectFunction(ref, nextName),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const regionMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectFunctionRegion(ref, regionForm),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteRegionMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => deleteProjectFunctionRegion(ref, id),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const mountMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectFunctionStorageMount(ref, mountForm),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const unmountMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => deleteProjectFunctionStorageMount(ref, id),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const functionColumns = useMemo<ColumnDef<ProjectFunction>[]>(
    () => [
      {
        header: "Function",
        accessorKey: "name",
        size: 230,
        cell: ({ row }) => (
          <>
            <p className="cell-main truncate font-mono">{row.original.name}</p>
            <p className="cell-sub truncate">v{row.original.version} · {row.original.entrypoint}</p>
          </>
        ),
      },
      {
        header: "Auth",
        accessorKey: "verify_jwt",
        size: 110,
        cell: ({ row }) => <span className={`pill ${row.original.verify_jwt ? "healthy" : "provisioning"}`}>{row.original.verify_jwt ? "jwt" : "public"}</span>,
      },
      {
        header: "Status",
        accessorKey: "status",
        size: 130,
        cell: ({ row }) => <span className={`pill ${row.original.status === "deployed" ? "healthy" : "provisioning"}`}>{row.original.status}</span>,
      },
      {
        header: "Source",
        accessorKey: "source_hash",
        size: 210,
        cell: ({ row }) => (
          <>
            <p className="truncate font-mono text-xs text-muted">{row.original.source_hash.slice(0, 12)}</p>
            <p className="cell-sub">{formatBytes(row.original.source_bytes)}</p>
          </>
        ),
      },
      {
        header: "Secrets",
        id: "secrets",
        size: 160,
        cell: ({ row }) => {
          const secrets = Object.keys(row.original.secrets);
          return secrets.length > 0 ? <p className="truncate font-mono text-xs text-muted">{secrets.join(", ")}</p> : "none";
        },
      },
      {
        header: "Updated",
        accessorKey: "updated_at",
        size: 160,
        cell: ({ row }) => formatDateTime(row.original.updated_at),
      },
      {
        header: "",
        id: "actions",
        size: 56,
        cell: ({ row }) => (
          <button className="icon-button" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, nextName: row.original.name })} title="Delete function" type="button">
            <X size={14} />
          </button>
        ),
      },
    ],
    [deleteMutation, project],
  );
  const regionColumns = useMemo<ColumnDef<ProjectFunctionRegion>[]>(
    () => [
      {
        header: "Function",
        accessorKey: "function_name",
        size: 210,
        cell: ({ row }) => (
          <>
            <p className="cell-main truncate font-mono">{row.original.function_name}</p>
            <p className="cell-sub truncate">{row.original.region}</p>
          </>
        ),
      },
      {
        header: "Routing",
        accessorKey: "routing_policy",
        size: 150,
        cell: ({ row }) => (
          <>
            <p className="text-sm capitalize">{row.original.routing_policy}</p>
            <p className="cell-sub truncate font-mono">{row.original.host_id || "any host"}</p>
          </>
        ),
      },
      {
        header: "Invoke URL",
        accessorKey: "invocation_url",
        size: 360,
        cell: ({ row }) => <p className="truncate font-mono text-xs text-muted">{row.original.invocation_url}</p>,
      },
      {
        header: "Status",
        accessorKey: "status",
        size: 130,
        cell: ({ row }) => <span className={`pill ${row.original.status === "configured" ? "healthy" : "provisioning"}`}>{row.original.status}</span>,
      },
      {
        header: "Updated",
        accessorKey: "updated_at",
        size: 160,
        cell: ({ row }) => formatDateTime(row.original.updated_at),
      },
      {
        header: "",
        id: "actions",
        size: 56,
        cell: ({ row }) => (
          <button className="icon-button" disabled={!project || deleteRegionMutation.isPending} onClick={() => project && deleteRegionMutation.mutate({ ref: project.ref, id: row.original.id })} title="Delete region" type="button">
            <X size={14} />
          </button>
        ),
      },
    ],
    [deleteRegionMutation, project],
  );
  const mountColumns = useMemo<ColumnDef<ProjectFunctionStorageMount>[]>(
    () => [
      {
        header: "Mount",
        accessorKey: "function_name",
        size: 230,
        cell: ({ row }) => (
          <>
            <p className="cell-main truncate font-mono">{row.original.function_name}</p>
            <p className="cell-sub truncate font-mono">{row.original.mount_path}</p>
          </>
        ),
      },
      {
        header: "Bucket",
        accessorKey: "bucket_name",
        size: 220,
        cell: ({ row }) => (
          <>
            <p className="text-sm font-mono">{row.original.bucket_name}</p>
            <p className="cell-sub truncate font-mono">{row.original.prefix || "whole bucket"}</p>
          </>
        ),
      },
      {
        header: "Env",
        accessorKey: "env_alias",
        size: 150,
        cell: ({ row }) => <p className="truncate font-mono text-xs text-muted">{row.original.env_alias || "none"}</p>,
      },
      {
        header: "Mode",
        accessorKey: "read_only",
        size: 110,
        cell: ({ row }) => <span className={`pill ${row.original.read_only ? "provisioning" : "healthy"}`}>{row.original.read_only ? "ro" : "rw"}</span>,
      },
      {
        header: "Status",
        accessorKey: "status",
        size: 130,
        cell: ({ row }) => <span className={`pill ${row.original.status === "configured" ? "healthy" : "provisioning"}`}>{row.original.status}</span>,
      },
      {
        header: "Updated",
        accessorKey: "updated_at",
        size: 160,
        cell: ({ row }) => formatDateTime(row.original.updated_at),
      },
      {
        header: "",
        id: "actions",
        size: 56,
        cell: ({ row }) => (
          <button className="icon-button" disabled={!project || unmountMutation.isPending} onClick={() => project && unmountMutation.mutate({ ref: project.ref, id: row.original.id })} title="Remove mount" type="button">
            <X size={14} />
          </button>
        ),
      },
    ],
    [project, unmountMutation],
  );

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!enabled || !project || name.trim().length === 0 || source.trim().length === 0) {
      return;
    }
    deployMutation.mutate({
      ref: project.ref,
      input: {
        name,
        entrypoint,
        verify_jwt: verifyJwt,
        source,
        secrets: parseKeyValueLines(secrets),
      },
    });
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Functions</p>
          <h2>Deployments</h2>
        </div>
        <RadioTower size={15} className="text-faint" />
      </div>
      {!enabled ? (
        <div className="mt-4 rounded-md border border-border bg-bg p-3">
          <p className="text-sm font-medium">Function deployments disabled</p>
          <p className="mt-1 text-sm text-muted">Enable the edge_functions feature flag for this org before deploying functions, adding regions, or mounting storage.</p>
        </div>
      ) : null}
      <form className="mt-4 grid gap-2" onSubmit={submit}>
        <div className="grid grid-cols-[minmax(0,1fr)_140px_auto] gap-2 max-sm:grid-cols-1">
          <input className="input font-mono" disabled={!enabled} placeholder="hello-api" value={name} onChange={(event) => setName(event.target.value)} />
          <input className="input font-mono" disabled={!enabled} placeholder="index.ts" value={entrypoint} onChange={(event) => setEntrypoint(event.target.value)} />
          <label className="segmented justify-start gap-2 px-3">
            <input checked={verifyJwt} disabled={!enabled} onChange={(event) => setVerifyJwt(event.target.checked)} type="checkbox" />
            JWT
          </label>
        </div>
        <textarea className="input min-h-[96px] font-mono" disabled={!enabled} value={source} onChange={(event) => setSource(event.target.value)} />
        <input className="input font-mono" disabled={!enabled} placeholder="SECRET=value, one per line" value={secrets} onChange={(event) => setSecrets(event.target.value)} />
        <button className="button secondary justify-center" disabled={!enabled || !project || deployMutation.isPending || name.trim().length === 0 || source.trim().length === 0} type="submit">
          <Save size={14} />
          Deploy
        </button>
      </form>
      <div className="mt-4 grid gap-2 rounded-md border border-border bg-bg p-3">
        <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_130px_120px_auto] gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
          <input className="input font-mono" disabled={!enabled} placeholder="function" value={regionForm.function_name} onChange={(event) => setRegionForm({ ...regionForm, function_name: event.target.value })} />
          <input className="input font-mono" disabled={!enabled} placeholder="host id" value={regionForm.host_id} onChange={(event) => setRegionForm({ ...regionForm, host_id: event.target.value })} />
          <input className="input font-mono" disabled={!enabled} placeholder="region" value={regionForm.region} onChange={(event) => setRegionForm({ ...regionForm, region: event.target.value })} />
          <select className="input" disabled={!enabled} value={regionForm.routing_policy} onChange={(event) => setRegionForm({ ...regionForm, routing_policy: event.target.value })}>
            <option value="nearest">Nearest</option>
            <option value="primary">Primary</option>
            <option value="weighted">Weighted</option>
          </select>
          <button className="button secondary justify-center" disabled={!enabled || !project || regionMutation.isPending || regionForm.function_name.trim().length === 0} onClick={() => project && regionMutation.mutate({ ref: project.ref })} type="button">
            <Globe2 size={14} />
            Region
          </button>
        </div>
      </div>
      <div className="mt-4 grid gap-2 rounded-md border border-border bg-bg p-3">
        <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_150px_auto] gap-2 max-lg:grid-cols-2 max-sm:grid-cols-1">
          <input className="input font-mono" disabled={!enabled} placeholder="function" value={mountForm.function_name} onChange={(event) => setMountForm({ ...mountForm, function_name: event.target.value })} />
          <input className="input font-mono" disabled={!enabled} placeholder="bucket" value={mountForm.bucket_name} onChange={(event) => setMountForm({ ...mountForm, bucket_name: event.target.value })} />
          <input className="input font-mono" disabled={!enabled} placeholder="/mnt/assets" value={mountForm.mount_path} onChange={(event) => setMountForm({ ...mountForm, mount_path: event.target.value })} />
          <label className="segmented justify-start gap-2 px-3">
            <input checked={mountForm.read_only} disabled={!enabled} onChange={(event) => setMountForm({ ...mountForm, read_only: event.target.checked })} type="checkbox" />
            Read only
          </label>
        </div>
        <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2 max-sm:grid-cols-1">
          <input className="input font-mono" disabled={!enabled} placeholder="prefix" value={mountForm.prefix} onChange={(event) => setMountForm({ ...mountForm, prefix: event.target.value })} />
          <input className="input font-mono" disabled={!enabled} placeholder="ENV_ALIAS" value={mountForm.env_alias} onChange={(event) => setMountForm({ ...mountForm, env_alias: event.target.value })} />
          <button className="button secondary justify-center" disabled={!enabled || !project || mountMutation.isPending || mountForm.function_name.trim().length === 0 || mountForm.bucket_name.trim().length === 0} onClick={() => project && mountMutation.mutate({ ref: project.ref })} type="button">
            <Boxes size={14} />
            Mount
          </button>
        </div>
      </div>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading functions...</p> : null}
        <div className="grid gap-2">
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Function deployments</p>
              <p className="truncate text-xs text-muted">Versioned Edge Function bundles and JWT policy.</p>
            </div>
            <span className="pill">{functions.length} deployed</span>
          </div>
          <DataTable columns={functionColumns} data={functions} emptyText={loading ? "Loading function deployments..." : "No functions deployed."} minWidth={1080} />
        </div>
        <div className="grid gap-2">
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Regions</p>
              <p className="truncate text-xs text-muted">Host placement and invocation routing for deployed functions.</p>
            </div>
            <span className="pill">{regions.length} regions</span>
          </div>
          <DataTable columns={regionColumns} data={regions} emptyText={loading ? "Loading function regions..." : "No function regions configured."} minWidth={1080} />
        </div>
        <div className="grid gap-2">
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Storage mounts</p>
              <p className="truncate text-xs text-muted">Bucket mounts available to function runtimes.</p>
            </div>
            <span className="pill">{mounts.length} mounts</span>
          </div>
          <DataTable columns={mountColumns} data={mounts} emptyText={loading ? "Loading storage mounts..." : "No storage mounts configured."} minWidth={1080} />
        </div>
        {deployMutation.error ? <p className="text-sm text-danger">{deployMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
        {regionMutation.error ? <p className="text-sm text-danger">{regionMutation.error.message}</p> : null}
        {deleteRegionMutation.error ? <p className="text-sm text-danger">{deleteRegionMutation.error.message}</p> : null}
        {mountMutation.error ? <p className="text-sm text-danger">{mountMutation.error.message}</p> : null}
        {unmountMutation.error ? <p className="text-sm text-danger">{unmountMutation.error.message}</p> : null}
      </div>
    </section>
  );
}
