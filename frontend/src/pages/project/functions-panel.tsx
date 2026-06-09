import { FormEvent, useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowLeft, Boxes, ChevronDown, ChevronRight, Globe2, RadioTower, Save, SlidersHorizontal, X } from "lucide-react";
import {
  createProjectFunctionRegion,
  createProjectFunctionStorageMount,
  deleteProjectFunction,
  deleteProjectFunctionRegion,
  deleteProjectFunctionStorageMount,
  deployProjectFunction,
  updateProjectConfig,
} from "../../api";
import { AppPanel } from "../../components/app/app-panel";
import { DataTable } from "../../components/data-table";
import { Button } from "../../components/ui/button";
import { CollapsibleCard } from "../../components/ui/collapsible-card";
import { EmptyState } from "../../components/ui/empty-state";
import { Field, SubSection } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import { Switch } from "../../components/ui/switch";
import { Textarea } from "../../components/ui/textarea";
import { formatBytes, formatDateTime, formatRelativeTime } from "../../lib/format";
import { parseKeyValueLines } from "../../lib/parse";
import { projectPath } from "../../lib/routes";
import { statusTone } from "../../lib/status";
import type { Project, ProjectConfig, ProjectFunction, ProjectFunctionRegion, ProjectFunctionStorageMount, ProjectLog } from "../../types";

const functionRuntimeFlags = [
  { key: "runtime_enabled", label: "Runtime", detail: "Run functions on the Deno-based edge runtime" },
  { key: "verify_jwt_by_default", label: "Verify JWT", detail: "Require a signed user token by default" },
  { key: "secret_sync_enabled", label: "Secret sync", detail: "Push function secrets to the runtime" },
] as const;

// Edge-function deployment status that isn't a clean success or in-progress is a
// real failure the operator should see, not a generic "provisioning".
function functionTone(status: string) {
  if (status === "deployed" || status === "active" || status === "ready") return "success" as const;
  return statusTone(status);
}

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
  const setFlag = (key: string, flagEnabled: boolean) => setValue(key, flagEnabled ? "true" : "false");

  return (
    <CollapsibleCard
      eyebrow="Functions"
      title="Runtime settings"
      description={loading ? "Loading function settings..." : config?.updated_at ? `Updated ${formatRelativeTime(config.updated_at)}` : "Not saved yet"}
      actions={<SlidersHorizontal size={15} className="text-faint" />}
    >
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
                    <p className="truncate text-xs text-muted">{flag.detail}</p>
                  </div>
                  <input checked={flagEnabled} disabled={!enabled} onChange={(event) => setFlag(flag.key, event.target.checked)} type="checkbox" />
                </label>
              );
            })}
          </div>
          <div className="grid grid-cols-[180px_180px_minmax(0,1fr)] gap-2 max-md:grid-cols-1">
            <Field label="Deployment policy" hint="Who is allowed to publish new versions">
              <NativeSelect disabled={!enabled} value={draft.deployment_policy ?? "manual"} onChange={(event) => setValue("deployment_policy", event.target.value)}>
                <option value="manual">Manual (dashboard)</option>
                <option value="ci">CI-managed</option>
                <option value="locked">Locked (no changes)</option>
              </NativeSelect>
            </Field>
            <Field label="Worker timeout" hint="Max run time per invocation (ms)">
              <Input
                disabled={!enabled}
                min={100}
                max={300000}
                step={100}
                type="number"
                value={draft.worker_timeout_ms ?? "60000"}
                onChange={(event) => setValue("worker_timeout_ms", event.target.value)}
              />
            </Field>
            <Field label="Import map" hint="Path to a JSON file mapping import names to module URLs (import_map.json)">
              <Input className="font-mono" disabled={!enabled} placeholder="import_map.json" value={draft.import_map ?? ""} onChange={(event) => setValue("import_map", event.target.value)} />
            </Field>
          </div>
          <div className="usage-row">
            <p className="text-xs text-muted">{loading ? "Loading function settings..." : config?.updated_at ? `Updated ${formatDateTime(config.updated_at)}` : "Function settings not saved yet."}</p>
            <Button variant="secondary" disabled={!enabled || !project || mutation.isPending} type="submit">
              <Save size={14} />
              Save functions
            </Button>
          </div>
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </form>
    </CollapsibleCard>
  );
}

export function FunctionsPanel({ project, functions, regions, mounts, logs, loading, enabled, item }: { project?: Project; functions: ProjectFunction[]; regions: ProjectFunctionRegion[]; mounts: ProjectFunctionStorageMount[]; logs: ProjectLog[]; loading: boolean; enabled: boolean; item?: string }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState("hello-api");
  const [entrypoint, setEntrypoint] = useState("index.ts");
  const [verifyJwt, setVerifyJwt] = useState(true);
  const [source, setSource] = useState("Deno.serve(() => new Response(\"ok\"));");
  const [secrets, setSecrets] = useState("");
  const [openForm, setOpenForm] = useState<"deploy" | "region" | "mount" | null>(null);
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
    onSuccess: (_, variables) => {
      setOpenForm(null);
      invalidate(variables.ref);
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, nextName }: { ref: string; nextName: string }) => deleteProjectFunction(ref, nextName),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const regionMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectFunctionRegion(ref, regionForm),
    onSuccess: (_, variables) => {
      setOpenForm(null);
      invalidate(variables.ref);
    },
  });
  const deleteRegionMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => deleteProjectFunctionRegion(ref, id),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const mountMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectFunctionStorageMount(ref, mountForm),
    onSuccess: (_, variables) => {
      setOpenForm(null);
      invalidate(variables.ref);
    },
  });
  const unmountMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => deleteProjectFunctionStorageMount(ref, id),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  const openFunction = (functionName: string) => {
    if (!project) return;
    void navigate({ to: projectPath(project.ref, "functions", "deployments", functionName) });
  };

  const functionColumns = useMemo<ColumnDef<ProjectFunction>[]>(
    () => [
      {
        header: "Function",
        accessorKey: "name",
        size: 230,
        cell: ({ row }) => (
          <>
            <button className="cell-main truncate font-mono text-left text-accent hover:underline" onClick={() => openFunction(row.original.name)} type="button" title="Open function detail">
              {row.original.name}
            </button>
            <p className="cell-sub truncate">v{row.original.version} · {row.original.entrypoint}</p>
          </>
        ),
      },
      {
        header: "Auth",
        accessorKey: "verify_jwt",
        size: 110,
        cell: ({ row }) =>
          row.original.verify_jwt ? (
            <StatusPill tone="success" label="jwt" />
          ) : (
            // Public functions are an intentional choice, not a warning.
            <StatusPill tone="neutral" label="public" />
          ),
      },
      {
        header: "Status",
        accessorKey: "status",
        size: 130,
        cell: ({ row }) => <StatusPill tone={functionTone(row.original.status)} label={row.original.status} />,
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
          const keys = Object.keys(row.original.secrets);
          return keys.length > 0 ? <p className="truncate font-mono text-xs text-muted">{keys.join(", ")}</p> : <span className="text-xs text-faint">none</span>;
        },
      },
      {
        header: "Updated",
        accessorKey: "updated_at",
        size: 160,
        cell: ({ row }) => formatRelativeTime(row.original.updated_at),
      },
      {
        header: "",
        id: "actions",
        size: 56,
        cell: ({ row }) => (
          <Button variant="ghost" size="icon" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, nextName: row.original.name })} title="Delete function" type="button">
            <X size={14} />
          </Button>
        ),
      },
    ],
    [deleteMutation, project],
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

  // Detail route: show one function with its regions and recent logs.
  if (item) {
    const fn = functions.find((candidate) => candidate.name === item);
    return (
      <FunctionDetail
        project={project}
        fn={fn}
        name={item}
        regions={regions.filter((region) => region.function_name === item)}
        mounts={mounts.filter((mount) => mount.function_name === item)}
        logs={logs.filter((log) => log.metadata?.function === item || log.message.includes(item))}
        loading={loading}
        onBack={() => project && navigate({ to: projectPath(project.ref, "functions") })}
      />
    );
  }

  return (
    <AppPanel eyebrow="Functions" title="Deployments" actions={<RadioTower size={15} className="text-faint" />}>
      {!enabled ? (
        <div className="mt-4 rounded-md border border-border bg-bg p-3">
          <p className="text-sm font-medium">Function deployments disabled</p>
          <p className="mt-1 text-sm text-muted">Enable the edge_functions feature flag for this org before deploying functions, adding regions, or mounting storage.</p>
        </div>
      ) : null}

      {/* Data first: deployments, then disclosures for the creation forms. */}
      <div className="mt-4 grid gap-4">
        <SubSection
          title="Function deployments"
          description="Versioned Edge Function bundles and JWT policy. Open a function for its regions and logs."
          actions={
            <Button variant="secondary" className="justify-self-start" disabled={!enabled} onClick={() => setOpenForm((value) => (value === "deploy" ? null : "deploy"))} type="button">
              {openForm === "deploy" ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              Deploy function
            </Button>
          }
        >
          {openForm === "deploy" ? (
            <form className="grid gap-2 rounded-md border border-border bg-bg p-3" onSubmit={submit}>
              <div className="grid grid-cols-[minmax(0,1fr)_140px_auto] gap-2 max-sm:grid-cols-1">
                <Field label="Name" hint="URL-safe function name">
                  <Input className="font-mono" disabled={!enabled} placeholder="hello-api" value={name} onChange={(event) => setName(event.target.value)} />
                </Field>
                <Field label="Entrypoint" hint="Module to run">
                  <Input className="font-mono" disabled={!enabled} placeholder="index.ts" value={entrypoint} onChange={(event) => setEntrypoint(event.target.value)} />
                </Field>
                <Field label="Verify JWT" hint="Require a signed token">
                  <label className="flex items-center gap-2 text-sm">
                    <Switch checked={verifyJwt} disabled={!enabled} onCheckedChange={(next) => setVerifyJwt(next)} aria-label="JWT" />
                    JWT
                  </label>
                </Field>
              </div>
              <Field label="Source" hint="Function body served by the edge runtime">
                <Textarea className="min-h-[96px] font-mono" disabled={!enabled} value={source} onChange={(event) => setSource(event.target.value)} />
              </Field>
              <Field label="Secrets" hint="One KEY=value per line, injected as environment variables">
                <Input className="font-mono" disabled={!enabled} placeholder="SECRET=value, one per line" value={secrets} onChange={(event) => setSecrets(event.target.value)} />
              </Field>
              <Button variant="secondary" className="justify-self-start" disabled={!enabled || !project || deployMutation.isPending || name.trim().length === 0 || source.trim().length === 0} type="submit">
                <Save size={14} />
                Deploy
              </Button>
              {deployMutation.error ? <p className="text-sm text-danger">{deployMutation.error.message}</p> : null}
            </form>
          ) : null}
          <DataTable columns={functionColumns} data={functions} emptyText={loading ? "Loading function deployments..." : ""} minWidth={1080} />
          {!loading && functions.length === 0 ? (
            <EmptyState
              icon={RadioTower}
              title="No functions deployed"
              description="Deploy an Edge Function bundle to expose a serverless HTTP endpoint for this project."
              action={
                <Button variant="secondary" disabled={!enabled} onClick={() => setOpenForm("deploy")} type="button">
                  <Save size={14} />
                  Deploy function
                </Button>
              }
            />
          ) : null}
        </SubSection>

        <SubSection
          title="Regions"
          description="Host placement and invocation routing for deployed functions."
          actions={
            <Button variant="secondary" className="justify-self-start" disabled={!enabled} onClick={() => setOpenForm((value) => (value === "region" ? null : "region"))} type="button">
              {openForm === "region" ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              Add region
            </Button>
          }
        >
          {openForm === "region" ? (
            <div className="grid gap-2 rounded-md border border-border bg-bg p-3">
              <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_130px_160px] gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
                <Field label="Function" hint="Function to place">
                  <Input className="font-mono" disabled={!enabled} placeholder="hello-api" value={regionForm.function_name} onChange={(event) => setRegionForm({ ...regionForm, function_name: event.target.value })} />
                </Field>
                <Field label="Host" hint="Leave blank for any host">
                  <Input className="font-mono" disabled={!enabled} placeholder="host id" value={regionForm.host_id} onChange={(event) => setRegionForm({ ...regionForm, host_id: event.target.value })} />
                </Field>
                <Field label="Region" hint="Placement label">
                  <Input className="font-mono" disabled={!enabled} placeholder="local" value={regionForm.region} onChange={(event) => setRegionForm({ ...regionForm, region: event.target.value })} />
                </Field>
                <Field label="Routing" hint="How invocations pick a host">
                  <NativeSelect disabled={!enabled} value={regionForm.routing_policy} onChange={(event) => setRegionForm({ ...regionForm, routing_policy: event.target.value })}>
                    <option value="nearest">Nearest (lowest latency)</option>
                    <option value="primary">Primary (fixed host)</option>
                    <option value="weighted">Weighted (split traffic)</option>
                  </NativeSelect>
                </Field>
              </div>
              <Button variant="secondary" className="justify-self-start" disabled={!enabled || !project || regionMutation.isPending || regionForm.function_name.trim().length === 0} onClick={() => project && regionMutation.mutate({ ref: project.ref })} type="button">
                <Globe2 size={14} />
                Add region
              </Button>
              {regionMutation.error ? <p className="text-sm text-danger">{regionMutation.error.message}</p> : null}
            </div>
          ) : null}
          <RegionTable regions={regions} project={project} loading={loading} onOpen={openFunction} onDelete={(id) => project && deleteRegionMutation.mutate({ ref: project.ref, id })} deletePending={deleteRegionMutation.isPending} />
          {deleteRegionMutation.error ? <p className="text-sm text-danger">{deleteRegionMutation.error.message}</p> : null}
        </SubSection>

        <SubSection
          title="Storage mounts"
          description="Bucket mounts available to function runtimes."
          actions={
            <Button variant="secondary" className="justify-self-start" disabled={!enabled} onClick={() => setOpenForm((value) => (value === "mount" ? null : "mount"))} type="button">
              {openForm === "mount" ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              Add mount
            </Button>
          }
        >
          {openForm === "mount" ? (
            <div className="grid gap-2 rounded-md border border-border bg-bg p-3">
              <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_150px_160px] gap-2 max-lg:grid-cols-2 max-sm:grid-cols-1">
                <Field label="Function" hint="Function that gets the mount">
                  <Input className="font-mono" disabled={!enabled} placeholder="hello-api" value={mountForm.function_name} onChange={(event) => setMountForm({ ...mountForm, function_name: event.target.value })} />
                </Field>
                <Field label="Bucket" hint="Storage bucket to mount">
                  <Input className="font-mono" disabled={!enabled} placeholder="assets" value={mountForm.bucket_name} onChange={(event) => setMountForm({ ...mountForm, bucket_name: event.target.value })} />
                </Field>
                <Field label="Mount path" hint="Path inside the runtime">
                  <Input className="font-mono" disabled={!enabled} placeholder="/mnt/assets" value={mountForm.mount_path} onChange={(event) => setMountForm({ ...mountForm, mount_path: event.target.value })} />
                </Field>
                <Field label="Access" hint="Read-write mounts can modify bucket data">
                  <label className="flex items-center gap-2 text-sm">
                    <Switch checked={mountForm.read_only} disabled={!enabled} onCheckedChange={(next) => setMountForm({ ...mountForm, read_only: next })} aria-label="Read only" />
                    Read only
                  </label>
                </Field>
              </div>
              <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
                <Field label="Prefix" hint="Optional key prefix; blank mounts the whole bucket">
                  <Input className="font-mono" disabled={!enabled} placeholder="prefix" value={mountForm.prefix} onChange={(event) => setMountForm({ ...mountForm, prefix: event.target.value })} />
                </Field>
                <Field label="Env alias" hint="Env var exposing the mount path">
                  <Input className="font-mono" disabled={!enabled} placeholder="ASSETS_MOUNT" value={mountForm.env_alias} onChange={(event) => setMountForm({ ...mountForm, env_alias: event.target.value })} />
                </Field>
              </div>
              <Button variant="secondary" className="justify-self-start" disabled={!enabled || !project || mountMutation.isPending || mountForm.function_name.trim().length === 0 || mountForm.bucket_name.trim().length === 0} onClick={() => project && mountMutation.mutate({ ref: project.ref })} type="button">
                <Boxes size={14} />
                Add mount
              </Button>
              {mountMutation.error ? <p className="text-sm text-danger">{mountMutation.error.message}</p> : null}
            </div>
          ) : null}
          <MountTable mounts={mounts} project={project} loading={loading} onOpen={openFunction} onDelete={(id) => project && unmountMutation.mutate({ ref: project.ref, id })} deletePending={unmountMutation.isPending} />
          {unmountMutation.error ? <p className="text-sm text-danger">{unmountMutation.error.message}</p> : null}
        </SubSection>

        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

function RegionTable({ regions, project, loading, onOpen, onDelete, deletePending }: { regions: ProjectFunctionRegion[]; project?: Project; loading: boolean; onOpen: (name: string) => void; onDelete: (id: string) => void; deletePending: boolean }) {
  const columns = useMemo<ColumnDef<ProjectFunctionRegion>[]>(
    () => [
      {
        header: "Function",
        accessorKey: "function_name",
        size: 210,
        cell: ({ row }) => (
          <>
            <button className="cell-main truncate font-mono text-left text-accent hover:underline" onClick={() => onOpen(row.original.function_name)} type="button" title="Open function detail">
              {row.original.function_name}
            </button>
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
        size: 150,
        cell: ({ row }) => (
          <span title={row.original.message || undefined}>
            <StatusPill tone={statusTone(row.original.status)} label={row.original.status} />
          </span>
        ),
      },
      {
        header: "Updated",
        accessorKey: "updated_at",
        size: 160,
        cell: ({ row }) => formatRelativeTime(row.original.updated_at),
      },
      {
        header: "",
        id: "actions",
        size: 56,
        cell: ({ row }) => (
          <Button variant="ghost" size="icon" disabled={!project || deletePending} onClick={() => onDelete(row.original.id)} title="Delete region" type="button">
            <X size={14} />
          </Button>
        ),
      },
    ],
    [project, deletePending, onOpen, onDelete],
  );
  return (
    <>
      <DataTable columns={columns} data={regions} emptyText={loading ? "Loading function regions..." : ""} minWidth={1080} />
      {!loading && regions.length === 0 ? (
        <EmptyState icon={Globe2} title="No regions configured" description="Functions run on their host's default region until you add explicit placement and routing." />
      ) : null}
    </>
  );
}

function MountTable({ mounts, project, loading, onOpen, onDelete, deletePending }: { mounts: ProjectFunctionStorageMount[]; project?: Project; loading: boolean; onOpen: (name: string) => void; onDelete: (id: string) => void; deletePending: boolean }) {
  const columns = useMemo<ColumnDef<ProjectFunctionStorageMount>[]>(
    () => [
      {
        header: "Mount",
        accessorKey: "function_name",
        size: 230,
        cell: ({ row }) => (
          <>
            <button className="cell-main truncate font-mono text-left text-accent hover:underline" onClick={() => onOpen(row.original.function_name)} type="button" title="Open function detail">
              {row.original.function_name}
            </button>
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
        header: "Access",
        accessorKey: "read_only",
        size: 130,
        cell: ({ row }) =>
          row.original.read_only ? (
            // Read-only mount is intentional and safe → neutral, not a warning.
            <StatusPill tone="neutral" label="read only" />
          ) : (
            // Read-write is the riskier mode that can mutate bucket data.
            <StatusPill tone="warning" label="read-write" />
          ),
      },
      {
        header: "Status",
        accessorKey: "status",
        size: 150,
        cell: ({ row }) => (
          <span title={row.original.message || undefined}>
            <StatusPill tone={statusTone(row.original.status)} label={row.original.status} />
          </span>
        ),
      },
      {
        header: "Updated",
        accessorKey: "updated_at",
        size: 160,
        cell: ({ row }) => formatRelativeTime(row.original.updated_at),
      },
      {
        header: "",
        id: "actions",
        size: 56,
        cell: ({ row }) => (
          <Button variant="ghost" size="icon" disabled={!project || deletePending} onClick={() => onDelete(row.original.id)} title="Remove mount" type="button">
            <X size={14} />
          </Button>
        ),
      },
    ],
    [project, deletePending, onOpen, onDelete],
  );
  return (
    <>
      <DataTable columns={columns} data={mounts} emptyText={loading ? "Loading storage mounts..." : ""} minWidth={1080} />
      {!loading && mounts.length === 0 ? (
        <EmptyState icon={Boxes} title="No storage mounts" description="Mount a storage bucket into a function runtime to read or write objects from function code." />
      ) : null}
    </>
  );
}

function FunctionDetail({
  project,
  fn,
  name,
  regions,
  mounts,
  logs,
  loading,
  onBack,
}: {
  project?: Project;
  fn?: ProjectFunction;
  name: string;
  regions: ProjectFunctionRegion[];
  mounts: ProjectFunctionStorageMount[];
  logs: ProjectLog[];
  loading: boolean;
  onBack: () => void;
}) {
  const regionColumns = useMemo<ColumnDef<ProjectFunctionRegion>[]>(
    () => [
      { header: "Region", accessorKey: "region", size: 160, cell: ({ row }) => <p className="text-sm">{row.original.region}</p> },
      { header: "Host", accessorKey: "host_id", size: 180, cell: ({ row }) => <p className="font-mono text-xs text-muted">{row.original.host_id || "any host"}</p> },
      { header: "Routing", accessorKey: "routing_policy", size: 120, cell: ({ row }) => <p className="text-sm capitalize">{row.original.routing_policy}</p> },
      { header: "Invoke URL", accessorKey: "invocation_url", size: 320, cell: ({ row }) => <p className="truncate font-mono text-xs text-muted">{row.original.invocation_url}</p> },
      { header: "Status", accessorKey: "status", size: 140, cell: ({ row }) => <span title={row.original.message || undefined}><StatusPill tone={statusTone(row.original.status)} label={row.original.status} /></span> },
    ],
    [],
  );
  return (
    <section className="panel">
      <div className="section-head">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" onClick={onBack} title="Back to deployments" type="button">
            <ArrowLeft size={14} />
          </Button>
          <div>
            <p className="label">Function</p>
            <h2 className="font-mono">{name}</h2>
          </div>
        </div>
        {fn ? <StatusPill tone={functionTone(fn.status)} label={fn.status} /> : null}
      </div>
      <div className="mt-4 grid gap-4">
        {loading && !fn ? <p className="text-sm text-muted">Loading function...</p> : null}
        {!loading && !fn ? (
          <EmptyState title="Function not found" description={`No deployment named "${name}" exists for this project.`} action={<Button variant="secondary" onClick={onBack} type="button">Back to deployments</Button>} />
        ) : null}
        {fn ? (
          <>
            <div className="grid grid-cols-4 gap-2 max-md:grid-cols-2">
              <DetailStat label="Version" value={`v${fn.version}`} />
              <DetailStat label="Entrypoint" value={fn.entrypoint} mono />
              <DetailStat label="Auth" value={fn.verify_jwt ? "JWT required" : "Public"} />
              <DetailStat label="Size" value={formatBytes(fn.source_bytes)} />
            </div>
            <SubSection title="Regions" description="Where this function is placed and how invocations route to it.">
              <DataTable columns={regionColumns} data={regions} emptyText="" minWidth={900} />
              {regions.length === 0 ? <EmptyState icon={Globe2} title="No explicit regions" description="This function runs on its host's default region." /> : null}
            </SubSection>
            {mounts.length > 0 ? (
              <SubSection title="Storage mounts" description="Buckets mounted into this function's runtime.">
                <div className="grid gap-1">
                  {mounts.map((mount) => (
                    <div className="usage-row" key={mount.id}>
                      <p className="truncate font-mono text-xs text-muted">{mount.bucket_name} → {mount.mount_path}</p>
                      <StatusPill tone={mount.read_only ? "neutral" : "warning"} label={mount.read_only ? "read only" : "read-write"} />
                    </div>
                  ))}
                </div>
              </SubSection>
            ) : null}
            <SubSection title="Recent logs" description="Latest project log events referencing this function.">
              {logs.length === 0 ? (
                <EmptyState title="No recent logs" description="Invocation and deploy events for this function will appear here." />
              ) : (
                <div className="grid gap-1">
                  {logs.slice(0, 20).map((log) => (
                    <div className="usage-row" key={log.id}>
                      <span className="flex min-w-0 items-center gap-2">
                        <StatusPill tone={statusTone(log.level)} label={log.level} />
                        <span className="truncate text-sm">{log.message}</span>
                      </span>
                      <time className="shrink-0 text-xs text-faint">{formatRelativeTime(log.created_at)}</time>
                    </div>
                  ))}
                </div>
              )}
            </SubSection>
          </>
        ) : null}
        {project ? null : <p className="text-sm text-muted">Select a project.</p>}
      </div>
    </section>
  );
}

function DetailStat({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="rounded-md border border-border bg-bg p-2.5">
      <p className="label">{label}</p>
      <p className={`truncate text-sm font-medium${mono ? " font-mono" : ""}`}>{value}</p>
    </div>
  );
}
