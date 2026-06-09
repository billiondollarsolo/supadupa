import { FormEvent, useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Globe2, RotateCcw, Save } from "lucide-react";
import { createProjectCDNInvalidation, createProjectCDNObjectEvent, updateProjectCDNPolicy } from "../../api";
import { Button } from "../../components/ui/button";
import { CollapsibleCard } from "../../components/ui/collapsible-card";
import { Field, SubSection } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import { Textarea } from "../../components/ui/textarea";
import { formatTime } from "../../lib/format";
import { parseLines } from "../../lib/parse";
import type { CDNInvalidation, Project, ProjectCDNPolicy } from "../../types";

// Supadupa edge CDN policy + invalidation. This is a control-plane / edge
// concern (not part of Studio), so it lives in the project Settings → Network
// area rather than a data-plane tab.
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
      included_paths: (policy.included_paths ?? []).join("\n"),
      excluded_paths: (policy.excluded_paths ?? []).join("\n"),
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
      eyebrow="Edge"
      title="CDN policy"
      description="Edge cache TTLs, path rules, and invalidation for this project's public storage routes."
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
                <p className="truncate font-mono text-sm">{(invalidation.paths ?? []).join(", ")}</p>
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
