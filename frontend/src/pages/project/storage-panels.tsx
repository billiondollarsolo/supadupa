import { FormEvent, useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowLeft, Boxes, Globe2, Plus, RotateCcw, Save, SlidersHorizontal, X } from "lucide-react";
import {
  createProjectCDNInvalidation,
  createProjectCDNObjectEvent,
  createProjectStorageBucket,
  deleteProjectStorageBucket,
  updateProjectConfig,
  updateProjectCDNPolicy,
} from "../../api";
import { DataTable } from "../../components/data-table";
import { formatBytes, formatDateTime, formatTime } from "../../lib/format";
import { parseKeyValueLines, parseLines } from "../../lib/parse";
import type { CDNInvalidation, Project, ProjectCDNPolicy, ProjectConfig, ProjectStorageBucket } from "../../types";

const storageCapabilities = [
  { key: "image_transform_enabled", label: "Image transforms", detail: "imgproxy" },
  { key: "resumable_upload_enabled", label: "Resumable uploads", detail: "TUS" },
  { key: "s3_compat_enabled", label: "S3 compatibility", detail: "S3 API" },
] as const;

export function StorageConfigPanel({ project, config, loading }: { project?: Project; config?: ProjectConfig; loading: boolean }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const configKey = `${project?.ref ?? ""}:${config?.updated_at ?? ""}`;

  useEffect(() => {
    setDraft(config?.config ?? {});
  }, [configKey, config]);

  const mutation = useMutation({
    mutationFn: ({ ref, values }: { ref: string; values: Record<string, string> }) => updateProjectConfig(ref, "storage", values),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-config", variables.ref, "storage"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!project) {
      return;
    }
    mutation.mutate({
      ref: project.ref,
      values: {
        file_size_limit_mb: draft.file_size_limit_mb || "50",
        image_transform_enabled: draft.image_transform_enabled || "true",
        resumable_upload_enabled: draft.resumable_upload_enabled || "true",
        s3_compat_enabled: draft.s3_compat_enabled || "true",
      },
    });
  }

  const setValue = (key: string, value: string) => setDraft((current) => ({ ...current, [key]: value }));
  const setFlag = (key: string, enabled: boolean) => setValue(key, enabled ? "true" : "false");

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Storage</p>
          <h2>Runtime settings</h2>
        </div>
        <SlidersHorizontal size={15} className="text-faint" />
      </div>
      <form className="mt-4 grid gap-3" onSubmit={submit}>
        <div className="grid grid-cols-[minmax(0,1fr)_140px] items-end gap-2 max-sm:grid-cols-1">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">File storage limit</p>
            <p className="truncate text-xs text-muted">Maximum object size per upload.</p>
          </div>
          <input
            className="input font-mono"
            inputMode="numeric"
            min="1"
            value={draft.file_size_limit_mb ?? "50"}
            onChange={(event) => setValue("file_size_limit_mb", event.target.value)}
            type="number"
          />
        </div>
        <div className="grid gap-2">
          {storageCapabilities.map((capability) => {
            const enabled = (draft[capability.key] ?? "true") === "true";
            return (
              <label className="config-toggle" key={capability.key}>
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{capability.label}</p>
                  <p className="truncate font-mono text-xs text-muted">{capability.detail}</p>
                </div>
                <input checked={enabled} onChange={(event) => setFlag(capability.key, event.target.checked)} type="checkbox" />
              </label>
            );
          })}
        </div>
        <div className="usage-row">
          <p className="text-xs text-muted">{loading ? "Loading runtime settings..." : config?.updated_at ? `Updated ${formatDateTime(config.updated_at)}` : "Runtime settings not saved yet."}</p>
          <button className="button secondary" disabled={!project || mutation.isPending} type="submit">
            <Save size={14} />
            Save storage
          </button>
        </div>
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </form>
    </section>
  );
}

export function StorageBucketsPanel({ project, buckets, item, loading }: { project?: Project; buckets: ProjectStorageBucket[]; item?: string; loading: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [form, setForm] = useState({
    name: "assets",
    public: true,
    file_size_limit: "52428800",
    allowed_mime_types: "image/png,image/jpeg,image/webp",
    cache_control: "3600",
    avif_autodetection: true,
    metadata: "purpose=public-assets",
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["storage-buckets", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectStorageBucket(ref, {
      name: form.name,
      public: form.public,
      file_size_limit: Number(form.file_size_limit) || 0,
      allowed_mime_types: parseLines(form.allowed_mime_types),
      cache_control: form.cache_control,
      avif_autodetection: form.avif_autodetection,
      metadata: parseKeyValueLines(form.metadata),
    }),
    onSuccess: (_, variables) => {
      invalidate(variables.ref);
      void navigate({ to: "/projects/$ref/storage", params: { ref: variables.ref } });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, name }: { ref: string; name: string }) => deleteProjectStorageBucket(ref, name),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const bucketColumns = useMemo<ColumnDef<ProjectStorageBucket>[]>(
    () => [
      {
        header: "Bucket",
        accessorKey: "name",
        size: 190,
        cell: ({ row }) => (
          <>
            <p className="cell-main font-mono">{row.original.name}</p>
            <p className="cell-sub">{row.original.public ? "public" : "private"}</p>
          </>
        ),
      },
      {
        header: "Limits",
        accessorKey: "file_size_limit",
        size: 210,
        cell: ({ row }) => (
          <>
            <p className="text-sm">{formatBytes(row.original.file_size_limit)}</p>
            <p className="cell-sub">cache {row.original.cache_control}</p>
          </>
        ),
      },
      {
        header: "MIME policy",
        accessorKey: "allowed_mime_types",
        size: 300,
        cell: ({ row }) => (
          <>
            <p className="truncate text-sm">{row.original.allowed_mime_types.join(", ") || "any type"}</p>
            <p className="cell-sub truncate">{row.original.metadata.purpose || "no metadata purpose"}</p>
          </>
        ),
      },
      {
        header: "Status",
        accessorKey: "status",
        size: 130,
        cell: ({ row }) => <span className={`pill ${row.original.status === "configured" ? "healthy" : "provisioning"}`}>{row.original.status}</span>,
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
          <button className="icon-button" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, name: row.original.name })} title="Delete bucket" type="button">
            <X size={14} />
          </button>
        ),
      },
    ],
    [deleteMutation.isPending, project],
  );
  const showingCreate = item === "new";

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!project || form.name.trim().length === 0) {
      return;
    }
    createMutation.mutate({ ref: project.ref });
  }

  function openCreate() {
    if (!project) return;
    void navigate({ to: "/projects/$ref/storage/$section/$item", params: { ref: project.ref, section: "buckets", item: "new" } });
  }

  function closeCreate() {
    if (!project) return;
    void navigate({ to: "/projects/$ref/storage", params: { ref: project.ref } });
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Storage</p>
          <h2>{showingCreate ? "New bucket" : "Buckets"}</h2>
          <p className="mt-1 text-sm text-muted">{showingCreate ? "Create one storage bucket for this project." : "Project object buckets exposed through Supabase Storage."}</p>
        </div>
        {showingCreate ? (
          <button className="icon-button" onClick={closeCreate} title="Back to buckets" type="button">
            <ArrowLeft size={14} />
          </button>
        ) : (
          <button className="icon-button" disabled={!project} onClick={openCreate} title="Add storage bucket" type="button">
            <Plus size={14} />
          </button>
        )}
      </div>
      {showingCreate ? (
        <form className="grid gap-2" onSubmit={submit}>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{form.name || "Bucket name pending"}</p>
              <p className="truncate text-xs text-muted">{form.public ? "Public objects can be served through the project API." : "Private bucket access requires authenticated storage requests."}</p>
            </div>
            <span className="pill">{buckets.length} existing</span>
          </div>
          <div className="grid grid-cols-[minmax(0,1fr)_130px_130px] gap-2 max-sm:grid-cols-1">
            <input className="input font-mono" placeholder="assets" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            <input className="input font-mono" inputMode="numeric" value={form.file_size_limit} onChange={(event) => setForm({ ...form, file_size_limit: event.target.value })} />
            <input className="input font-mono" placeholder="3600" value={form.cache_control} onChange={(event) => setForm({ ...form, cache_control: event.target.value })} />
          </div>
          <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
            <input className="input font-mono" placeholder="image/png,image/jpeg" value={form.allowed_mime_types} onChange={(event) => setForm({ ...form, allowed_mime_types: event.target.value })} />
            <input className="input font-mono" placeholder="purpose=public-assets" value={form.metadata} onChange={(event) => setForm({ ...form, metadata: event.target.value })} />
          </div>
          <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
            <label className="checkbox-row">
              <input type="checkbox" checked={form.public} onChange={(event) => setForm({ ...form, public: event.target.checked })} />
              Public bucket
            </label>
            <label className="checkbox-row">
              <input type="checkbox" checked={form.avif_autodetection} onChange={(event) => setForm({ ...form, avif_autodetection: event.target.checked })} />
              AVIF autodetection
            </label>
          </div>
          <button className="button secondary justify-center" disabled={!project || createMutation.isPending || form.name.trim().length === 0} type="submit">
            <Plus size={14} />
            Add storage bucket
          </button>
        </form>
      ) : (
        <div className="mt-4 grid gap-2">
          <DataTable columns={bucketColumns} data={buckets} emptyText={loading ? "Loading storage buckets..." : "No storage buckets configured."} minWidth={880} />
        </div>
      )}
      <div className="mt-3 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading storage buckets...</p> : null}
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </section>
  );
}

export function CDNPanel({ project, policy, invalidations, loading }: { project?: Project; policy?: ProjectCDNPolicy; invalidations: CDNInvalidation[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    enabled: false,
    browser_ttl_seconds: "3600",
    edge_ttl_seconds: "3600",
    stale_while_revalidate_seconds: "60",
    included_paths: "/storage/v1/object/public/*",
    excluded_paths: "",
    smart_revalidation: false,
    cache_control: "",
    invalidate_paths: "/storage/v1/object/public/*",
    event_id: "",
    event_bucket: "assets",
    event_object_path: "avatars/user.png",
    event_type: "object_updated",
  });
  const policyKey = `${policy?.project_ref ?? ""}:${policy?.updated_at ?? ""}`;
  useEffect(() => {
    if (!policy) {
      return;
    }
    setForm((current) => ({
      ...current,
      enabled: policy.enabled,
      browser_ttl_seconds: policy.browser_ttl_seconds.toString(),
      edge_ttl_seconds: policy.edge_ttl_seconds.toString(),
      stale_while_revalidate_seconds: policy.stale_while_revalidate_seconds.toString(),
      included_paths: policy.included_paths.join("\n"),
      excluded_paths: policy.excluded_paths.join("\n"),
      smart_revalidation: policy.smart_revalidation,
      cache_control: policy.cache_control,
    }));
  }, [policyKey, policy]);

  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["cdn-policy", ref] });
    void queryClient.invalidateQueries({ queryKey: ["cdn-invalidations", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-route-manifest", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const updateMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => updateProjectCDNPolicy(ref, {
      enabled: form.enabled,
      browser_ttl_seconds: Number(form.browser_ttl_seconds) || 0,
      edge_ttl_seconds: Number(form.edge_ttl_seconds) || 0,
      stale_while_revalidate_seconds: Number(form.stale_while_revalidate_seconds) || 0,
      included_paths: parseLines(form.included_paths),
      excluded_paths: parseLines(form.excluded_paths),
      smart_revalidation: form.smart_revalidation,
      cache_control: form.cache_control,
    }),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const invalidationMutation = useMutation({
    mutationFn: ({ ref, paths }: { ref: string; paths: string[] }) => createProjectCDNInvalidation(ref, paths),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const objectEventMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectCDNObjectEvent(ref, {
      event_id: form.event_id,
      bucket: form.event_bucket,
      object_path: form.event_object_path,
      event_type: form.event_type,
    }),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!project) {
      return;
    }
    updateMutation.mutate({ ref: project.ref });
  }

  function invalidatePaths() {
    if (!project) {
      return;
    }
    const paths = parseLines(form.invalidate_paths);
    if (paths.length === 0) {
      return;
    }
    invalidationMutation.mutate({ ref: project.ref, paths });
  }

  function submitObjectEvent() {
    if (!project || form.event_object_path.trim().length === 0) {
      return;
    }
    objectEventMutation.mutate({ ref: project.ref });
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Storage</p>
          <h2>CDN policy</h2>
        </div>
        <Globe2 size={15} className="text-faint" />
      </div>
      <form className="mt-4 grid gap-2" onSubmit={submit}>
        <label className="config-toggle">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Edge caching</p>
            <p className="truncate font-mono text-xs text-muted">{policy?.cache_control ?? "public storage path cache policy"}</p>
          </div>
          <input checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} type="checkbox" />
        </label>
        <div className="grid grid-cols-3 gap-2 max-sm:grid-cols-1">
          <input className="input font-mono" inputMode="numeric" placeholder="browser ttl" value={form.browser_ttl_seconds} onChange={(event) => setForm({ ...form, browser_ttl_seconds: event.target.value })} />
          <input className="input font-mono" inputMode="numeric" placeholder="edge ttl" value={form.edge_ttl_seconds} onChange={(event) => setForm({ ...form, edge_ttl_seconds: event.target.value })} />
          <input className="input font-mono" inputMode="numeric" placeholder="swr ttl" value={form.stale_while_revalidate_seconds} onChange={(event) => setForm({ ...form, stale_while_revalidate_seconds: event.target.value })} />
        </div>
        <label className="config-toggle">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Smart revalidation</p>
            <p className="truncate text-xs text-muted">Record object-change invalidation intent for storage paths.</p>
          </div>
          <input checked={form.smart_revalidation} onChange={(event) => setForm({ ...form, smart_revalidation: event.target.checked })} type="checkbox" />
        </label>
        <textarea className="input min-h-[64px] font-mono" value={form.included_paths} onChange={(event) => setForm({ ...form, included_paths: event.target.value })} />
        <textarea className="input min-h-[52px] font-mono" placeholder="/storage/v1/object/private/*" value={form.excluded_paths} onChange={(event) => setForm({ ...form, excluded_paths: event.target.value })} />
        <input className="input font-mono" value={form.cache_control} onChange={(event) => setForm({ ...form, cache_control: event.target.value })} />
        <button className="button secondary justify-center" disabled={!project || updateMutation.isPending} type="submit">
          <Save size={14} />
          Save CDN
        </button>
      </form>
      <div className="mt-4 grid gap-2">
        <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2 max-sm:grid-cols-1">
          <textarea className="input min-h-[52px] font-mono" value={form.invalidate_paths} onChange={(event) => setForm({ ...form, invalidate_paths: event.target.value })} />
          <button className="button secondary justify-center" disabled={!project || invalidationMutation.isPending || parseLines(form.invalidate_paths).length === 0} onClick={invalidatePaths} type="button">
            <RotateCcw size={14} />
            Invalidate
          </button>
        </div>
        <div className="grid gap-2 rounded-md border border-border p-3">
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Object-change revalidation</p>
              <p className="truncate text-xs text-muted">Posts a storage event and records the generated Smart CDN invalidation.</p>
            </div>
            <span className={`pill ${form.smart_revalidation ? "healthy" : "paused"}`}>{form.smart_revalidation ? "smart" : "off"}</span>
          </div>
          <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
            <input className="input font-mono" placeholder="event id" value={form.event_id} onChange={(event) => setForm({ ...form, event_id: event.target.value })} />
            <input className="input font-mono" placeholder="bucket" value={form.event_bucket} onChange={(event) => setForm({ ...form, event_bucket: event.target.value })} />
            <input className="input font-mono" placeholder="object path" value={form.event_object_path} onChange={(event) => setForm({ ...form, event_object_path: event.target.value })} />
            <select className="input" value={form.event_type} onChange={(event) => setForm({ ...form, event_type: event.target.value })}>
              <option value="object_changed">Changed</option>
              <option value="object_created">Created</option>
              <option value="object_updated">Updated</option>
              <option value="object_deleted">Deleted</option>
            </select>
          </div>
          <button className="button secondary justify-center" disabled={!project || objectEventMutation.isPending || form.event_object_path.trim().length === 0} onClick={submitObjectEvent} type="button">
            <RotateCcw size={14} />
            Revalidate object
          </button>
        </div>
        {loading ? <p className="text-sm text-muted">Loading CDN state...</p> : null}
        {!loading && invalidations.length === 0 ? <p className="text-sm text-muted">No CDN invalidations recorded.</p> : null}
        {invalidations.slice(0, 5).map((invalidation) => (
          <div className="cdn-row" key={invalidation.id}>
            <div className="min-w-0">
              <p className="truncate font-mono text-sm">{invalidation.paths.join(", ")}</p>
              <p className="truncate text-xs text-muted">{formatTime(invalidation.created_at)} - {invalidation.source || "manual"}{invalidation.event_id ? ` - ${invalidation.event_id}` : ""} - {invalidation.message || invalidation.status}</p>
            </div>
            <span className={`pill ${invalidation.status === "completed" ? "healthy" : "provisioning"}`}>{invalidation.status}</span>
          </div>
        ))}
        {updateMutation.error ? <p className="text-sm text-danger">{updateMutation.error.message}</p> : null}
        {invalidationMutation.error ? <p className="text-sm text-danger">{invalidationMutation.error.message}</p> : null}
        {objectEventMutation.error ? <p className="text-sm text-danger">{objectEventMutation.error.message}</p> : null}
      </div>
    </section>
  );
}
