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
import { AppPanel } from "../../components/app/app-panel";
import { DataTable } from "../../components/data-table";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { CollapsibleCard } from "../../components/ui/collapsible-card";
import { EmptyState } from "../../components/ui/empty-state";
import { Field, SubSection } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import { Textarea } from "../../components/ui/textarea";
import { formatBytes, formatDateTime, formatTime } from "../../lib/format";
import { parseKeyValueLines, parseLines } from "../../lib/parse";
import type { CDNInvalidation, Project, ProjectCDNPolicy, ProjectConfig, ProjectStorageBucket } from "../../types";

const MIB = 1024 * 1024;

const storageCapabilities = [
  { key: "image_transform_enabled", label: "Image transforms", consequence: "Serve on-the-fly resized/optimized images via imgproxy." },
  { key: "resumable_upload_enabled", label: "Resumable uploads", consequence: "Accept large uploads that survive network drops (TUS)." },
  { key: "s3_compat_enabled", label: "S3 compatibility", consequence: "Expose the S3-compatible endpoint for existing S3 clients." },
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
    <CollapsibleCard
      eyebrow="Storage"
      title="Runtime defaults"
      description="Project-wide upload limit and capability toggles."
      actions={<SlidersHorizontal size={15} className="text-faint" />}
    >
      <form className="mt-4 grid gap-3" onSubmit={submit}>
        <Field label="Default upload limit" hint="MB — per-bucket limits can override this">
          <Input
            className="font-mono"
            inputMode="numeric"
            min="1"
            value={draft.file_size_limit_mb ?? "50"}
            onChange={(event) => setValue("file_size_limit_mb", event.target.value)}
            type="number"
          />
        </Field>
        <SubSection title="Capabilities">
          <div className="grid gap-2">
            {storageCapabilities.map((capability) => {
              const enabled = (draft[capability.key] ?? "true") === "true";
              return (
                <label className="config-toggle" key={capability.key}>
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{capability.label}</p>
                    <p className="text-xs text-muted">{capability.consequence}</p>
                  </div>
                  <input checked={enabled} onChange={(event) => setFlag(capability.key, event.target.checked)} type="checkbox" />
                </label>
              );
            })}
          </div>
        </SubSection>
        <div className="usage-row">
          <p className="text-xs text-muted">{loading ? "Loading runtime defaults..." : config?.updated_at ? `Updated ${formatDateTime(config.updated_at)}` : "Runtime defaults not saved yet."}</p>
          <Button variant="secondary" disabled={!project || mutation.isPending} type="submit">
            <Save size={14} />
            Save defaults
          </Button>
        </div>
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </form>
    </CollapsibleCard>
  );
}

export function StorageBucketsPanel({ project, buckets, item, loading }: { project?: Project; buckets: ProjectStorageBucket[]; item?: string; loading: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [form, setForm] = useState({
    name: "",
    public: false,
    file_size_limit_mb: "50",
    allowed_mime_types: "",
    cache_control: "3600",
    avif_autodetection: false,
    metadata: "",
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
      file_size_limit: (Number(form.file_size_limit_mb) || 0) * MIB,
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
        size: 200,
        cell: ({ row }) => (
          <>
            <p className="cell-main font-mono">{row.original.name}</p>
            <Badge variant="muted">{row.original.public ? "public" : "private"}</Badge>
          </>
        ),
      },
      {
        header: "Upload limit",
        accessorKey: "file_size_limit",
        size: 200,
        cell: ({ row }) => (
          <>
            <p className="text-sm">{formatBytes(row.original.file_size_limit)}</p>
            <p className="cell-sub">cache {row.original.cache_control}s</p>
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
        size: 120,
        cell: ({ row }) => <StatusPill status={row.original.status} />,
      },
      {
        header: "Created",
        accessorKey: "created_at",
        size: 150,
        cell: ({ row }) => formatDateTime(row.original.created_at),
      },
      {
        header: "",
        id: "actions",
        size: 56,
        cell: ({ row }) => (
          <Button variant="ghost" size="icon" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, name: row.original.name })} title="Delete bucket" type="button">
            <X size={14} />
          </Button>
        ),
      },
    ],
    [deleteMutation.isPending, project],
  );
  const showingCreate = item === "new";
  const limitBytes = (Number(form.file_size_limit_mb) || 0) * MIB;

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
    <AppPanel
      eyebrow="Storage"
      title={showingCreate ? "New bucket" : "Buckets"}
      actions={
        showingCreate ? (
          <Button variant="secondary" onClick={closeCreate} type="button">
            <ArrowLeft size={14} />
            Back to buckets
          </Button>
        ) : (
          <Button variant="secondary" disabled={!project} onClick={openCreate} type="button">
            <Plus size={14} />
            Add bucket
          </Button>
        )
      }
    >
      <p className="mt-1 text-sm text-muted">{showingCreate ? "Create one storage bucket for this project." : "Project object buckets exposed through Supabase Storage."}</p>
      {showingCreate ? (
        <form className="mt-4 grid gap-3" onSubmit={submit}>
          <SubSection title="Identity">
            <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
              <Field label="Bucket name" required>
                <Input className="font-mono" placeholder="assets" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
              </Field>
              <label className="checkbox-row self-end">
                <input type="checkbox" checked={form.public} onChange={(event) => setForm({ ...form, public: event.target.checked })} />
                Public bucket
              </label>
            </div>
          </SubSection>
          <SubSection title="Limits">
            <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
              <Field label="Upload limit" hint={`MB · = ${formatBytes(limitBytes)}`}>
                <Input className="font-mono" inputMode="numeric" min="1" type="number" value={form.file_size_limit_mb} onChange={(event) => setForm({ ...form, file_size_limit_mb: event.target.value })} />
              </Field>
              <Field label="Cache control" hint="seconds">
                <Input className="font-mono" inputMode="numeric" placeholder="3600" value={form.cache_control} onChange={(event) => setForm({ ...form, cache_control: event.target.value })} />
              </Field>
            </div>
          </SubSection>
          <SubSection title="Content policy">
            <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
              <Field label="Allowed MIME types" hint="comma or newline separated · empty allows any">
                <Input className="font-mono" placeholder="image/png, image/jpeg" value={form.allowed_mime_types} onChange={(event) => setForm({ ...form, allowed_mime_types: event.target.value })} />
              </Field>
              <Field label="Metadata" hint="key=value pairs">
                <Input className="font-mono" placeholder="purpose=public-assets" value={form.metadata} onChange={(event) => setForm({ ...form, metadata: event.target.value })} />
              </Field>
            </div>
            <label className="checkbox-row">
              <input type="checkbox" checked={form.avif_autodetection} onChange={(event) => setForm({ ...form, avif_autodetection: event.target.checked })} />
              AVIF autodetection
            </label>
          </SubSection>
          <Button variant="secondary" className="justify-self-start" disabled={!project || createMutation.isPending || form.name.trim().length === 0} type="submit">
            <Plus size={14} />
            Add bucket
          </Button>
        </form>
      ) : (
        <div className="mt-4 grid gap-2">
          {buckets.length === 0 && !loading ? (
            <EmptyState
              icon={Boxes}
              title="No storage buckets yet"
              description="Buckets hold the project's uploaded objects. Create one to start storing files."
              action={
                <Button variant="secondary" disabled={!project} onClick={openCreate} type="button">
                  <Plus size={14} />
                  Add bucket
                </Button>
              }
            />
          ) : (
            <DataTable columns={bucketColumns} data={buckets} emptyText={loading ? "Loading storage buckets..." : "No storage buckets configured."} minWidth={880} />
          )}
        </div>
      )}
      <div className="mt-3 grid gap-2">
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
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
    event_bucket: "",
    event_object_path: "",
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

  // Reflect the SAVED policy (not the draft) for the smart-revalidation dependency.
  const savedSmart = Boolean(policy?.smart_revalidation);

  return (
    <CollapsibleCard
      eyebrow="Storage"
      title="CDN policy"
      description="Edge cache TTLs, path rules, and invalidation."
      actions={<Globe2 size={15} className="text-faint" />}
    >
      <form className="mt-4 grid gap-4" onSubmit={submit}>
        <label className="config-toggle">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Edge caching</p>
            <p className="text-xs text-muted">Serve cached storage responses from the edge using the policy below.</p>
          </div>
          <input checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} type="checkbox" />
        </label>

        <SubSection title="Cache policy" description="How long responses stay fresh at each layer.">
          <div className="grid grid-cols-3 gap-2 max-sm:grid-cols-1">
            <Field label="Browser TTL" hint="seconds">
              <Input className="font-mono" inputMode="numeric" value={form.browser_ttl_seconds} onChange={(event) => setForm({ ...form, browser_ttl_seconds: event.target.value })} />
            </Field>
            <Field label="Edge TTL" hint="seconds">
              <Input className="font-mono" inputMode="numeric" value={form.edge_ttl_seconds} onChange={(event) => setForm({ ...form, edge_ttl_seconds: event.target.value })} />
            </Field>
            <Field label="Stale-while-revalidate" hint="seconds">
              <Input className="font-mono" inputMode="numeric" value={form.stale_while_revalidate_seconds} onChange={(event) => setForm({ ...form, stale_while_revalidate_seconds: event.target.value })} />
            </Field>
          </div>
          <Field label="Cache-Control override" hint="optional raw header value — empty derives from TTLs">
            <Input className="font-mono" placeholder="public, max-age=3600" value={form.cache_control} onChange={(event) => setForm({ ...form, cache_control: event.target.value })} />
          </Field>
        </SubSection>

        <SubSection title="Path rules" description="Which storage paths the policy applies to.">
          <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
            <Field label="Included paths" hint="one glob per line">
              <Textarea className="min-h-[64px] font-mono" value={form.included_paths} onChange={(event) => setForm({ ...form, included_paths: event.target.value })} />
            </Field>
            <Field label="Excluded paths" hint="one glob per line">
              <Textarea className="min-h-[64px] font-mono" placeholder="/storage/v1/object/private/*" value={form.excluded_paths} onChange={(event) => setForm({ ...form, excluded_paths: event.target.value })} />
            </Field>
          </div>
        </SubSection>

        <SubSection title="Smart revalidation" description="Object changes auto-invalidate matching cached paths. Requires edge caching to be enabled.">
          <label className="config-toggle">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Auto-invalidate on object change</p>
              <p className="text-xs text-muted">{form.enabled ? "Records invalidation intent when storage objects change." : "Enable edge caching first for this to take effect."}</p>
            </div>
            <input checked={form.smart_revalidation} disabled={!form.enabled} onChange={(event) => setForm({ ...form, smart_revalidation: event.target.checked })} type="checkbox" />
          </label>
        </SubSection>

        <Button variant="secondary" className="justify-self-start" disabled={!project || updateMutation.isPending} type="submit">
          <Save size={14} />
          Save CDN policy
        </Button>
        {updateMutation.error ? <p className="text-sm text-danger">{updateMutation.error.message}</p> : null}
      </form>

      <div className="mt-4 grid gap-4">
        <SubSection title="Invalidation" description="Purge cached paths on demand.">
          <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2 max-sm:grid-cols-1">
            <Field label="Paths to invalidate" hint="one glob per line">
              <Textarea className="min-h-[52px] font-mono" value={form.invalidate_paths} onChange={(event) => setForm({ ...form, invalidate_paths: event.target.value })} />
            </Field>
            <Button variant="secondary" className="self-end justify-self-start" disabled={!project || invalidationMutation.isPending || parseLines(form.invalidate_paths).length === 0} onClick={invalidatePaths} type="button">
              <RotateCcw size={14} />
              Invalidate
            </Button>
          </div>
          {invalidationMutation.error ? <p className="text-sm text-danger">{invalidationMutation.error.message}</p> : null}
        </SubSection>

        <details className="rounded-md border border-border bg-bg p-3">
          <summary className="flex cursor-pointer list-none items-center gap-2 text-faint">
            <RotateCcw size={14} />
            <p className="label">Object-change revalidation (debug)</p>
            <StatusPill className="ml-auto" tone={savedSmart ? "success" : "neutral"} label={savedSmart ? "smart on (saved)" : "smart off (saved)"} />
          </summary>
          <div className="mt-3 grid gap-2">
            <p className="text-xs text-muted">Posts a synthetic storage event and records the generated Smart CDN invalidation. Requires saved smart revalidation to actually purge.</p>
            <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
              <Field label="Event ID" hint="optional idempotency key">
                <Input className="font-mono" placeholder="evt_…" value={form.event_id} onChange={(event) => setForm({ ...form, event_id: event.target.value })} />
              </Field>
              <Field label="Bucket">
                <Input className="font-mono" placeholder="assets" value={form.event_bucket} onChange={(event) => setForm({ ...form, event_bucket: event.target.value })} />
              </Field>
              <Field label="Object path">
                <Input className="font-mono" placeholder="avatars/user.png" value={form.event_object_path} onChange={(event) => setForm({ ...form, event_object_path: event.target.value })} />
              </Field>
              <Field label="Event type">
                <NativeSelect value={form.event_type} onChange={(event) => setForm({ ...form, event_type: event.target.value })}>
                  <option value="object_changed">Changed</option>
                  <option value="object_created">Created</option>
                  <option value="object_updated">Updated</option>
                  <option value="object_deleted">Deleted</option>
                </NativeSelect>
              </Field>
            </div>
            <Button variant="secondary" className="justify-self-start" disabled={!project || objectEventMutation.isPending || form.event_object_path.trim().length === 0} onClick={submitObjectEvent} type="button">
              <RotateCcw size={14} />
              Revalidate object
            </Button>
            {objectEventMutation.error ? <p className="text-sm text-danger">{objectEventMutation.error.message}</p> : null}
          </div>
        </details>

        <SubSection title="Recent invalidations">
          {loading ? <p className="text-sm text-muted">Loading CDN state...</p> : null}
          {!loading && invalidations.length === 0 ? <p className="text-sm text-muted">No CDN invalidations recorded.</p> : null}
          {invalidations.slice(0, 5).map((invalidation) => (
            <div className="cdn-row" key={invalidation.id}>
              <div className="min-w-0">
                <p className="truncate font-mono text-sm">{invalidation.paths.join(", ")}</p>
                <p className="truncate text-xs text-muted">{formatTime(invalidation.created_at)} · {invalidation.source || "manual"}{invalidation.event_id ? ` · ${invalidation.event_id}` : ""}{invalidation.message ? ` · ${invalidation.message}` : ""}</p>
              </div>
              <StatusPill status={invalidation.status} />
            </div>
          ))}
        </SubSection>
      </div>
    </CollapsibleCard>
  );
}
