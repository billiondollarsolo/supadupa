import { FormEvent, ReactNode, useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { ArrowLeft, Boxes, Copy, Database, ExternalLink, GitBranch, Plus, RadioTower, RotateCcw, Save, ShieldCheck, SlidersHorizontal, Trash2, X } from "lucide-react";
import {
  createProjectAnalyticsBucket,
  createProjectBranch,
  createProjectDatabaseCronJob,
  createProjectDatabaseQueue,
  createProjectDatabaseRole,
  createProjectDatabaseSchema,
  createProjectDatabaseWebhook,
  createProjectEmbeddingJob,
  createProjectReplica,
  createProjectReplicationPipeline,
  createProjectVectorBucket,
  deleteProjectAnalyticsBucket,
  deleteProjectBranch,
  deleteProjectDatabaseCronJob,
  deleteProjectDatabaseQueue,
  deleteProjectDatabaseRole,
  deleteProjectDatabaseSchema,
  deleteProjectDatabaseWebhook,
  deleteProjectEmbeddingJob,
  deleteProjectReplica,
  deleteProjectReplicationPipeline,
  deleteProjectVectorBucket,
  failoverProjectReplica,
  promoteProjectReplica,
  updateProjectConfig,
  updateProjectDatabaseExtension,
} from "../../api";
import { AppPanel } from "../../components/app/app-panel";
import { Button } from "../../components/ui/button";
import { EmptyState } from "../../components/ui/empty-state";
import { Field, SubSection } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import { Switch } from "../../components/ui/switch";
import { Textarea } from "../../components/ui/textarea";
import { formatDateTime, formatTime, shortChecksum } from "../../lib/format";
import { parseKeyValueLines, parseLines } from "../../lib/parse";
import { projectPath } from "../../lib/routes";
import type {
  Host,
  Project,
  ProjectAnalyticsBucket,
  ProjectBranch,
  ProjectConfig,
  ProjectDatabaseCronJob,
  ProjectDatabaseExtension,
  ProjectDatabaseQueue,
  ProjectDatabaseRole,
  ProjectDatabaseSchema,
  ProjectDatabaseWebhook,
  ProjectEmbeddingJob,
  ProjectReplica,
  ProjectReplicaRouting,
  ProjectReplicationPipeline,
  ProjectVectorBucket,
} from "../../types";

// API surfaces / Security / Extensions, each with a one-line consequence.
const databaseRuntimeGroups = [
  {
    title: "API surfaces",
    description: "Generated and event-driven access on top of Postgres.",
    flags: [
      { key: "pg_graphql_enabled", label: "GraphQL API", consequence: "Exposes a generated GraphQL endpoint (pg_graphql)." },
      { key: "database_webhooks", label: "Webhooks", consequence: "Delivers row-change events to HTTP endpoints via triggers." },
      { key: "pg_cron_enabled", label: "Cron", consequence: "Runs scheduled SQL jobs in-database (pg_cron)." },
      { key: "pgmq_enabled", label: "Queues", consequence: "Provides transactional message queues (pgmq)." },
    ],
  },
  {
    title: "Security",
    description: "Connection and access hardening.",
    flags: [
      { key: "vault_enabled", label: "Vault", consequence: "Stores encrypted secrets inside Postgres." },
      { key: "supavisor_enabled", label: "Supavisor", consequence: "Routes client connections through the pooler." },
      { key: "ssl_enforced", label: "DB SSL", consequence: "Rejects unencrypted database connections." },
      { key: "extension_toggle_ui", label: "Extension UI", consequence: "Lets admins toggle extensions from the dashboard." },
    ],
  },
  {
    title: "Extensions",
    description: "Optional Postgres capabilities.",
    flags: [
      { key: "fdw_enabled", label: "FDW", consequence: "Enables foreign data wrappers to external sources." },
      { key: "pgvector_enabled", label: "pgvector", consequence: "Adds vector columns and similarity search." },
    ],
  },
] as const;

function DatabaseDetailHeader({ detail, title, onBack }: { title: string; detail: string; onBack: () => void }) {
  return (
    <div className="rounded-md border border-border bg-bg p-3">
      <Button className="mb-3" onClick={onBack} size="sm" type="button" variant="secondary">
        <ArrowLeft size={14} />
        Back
      </Button>
      <p className="label">{title}</p>
      <p className="mt-1 text-sm text-muted">{detail}</p>
    </div>
  );
}

// One standardized list row across every database resource: a disclosure whose
// summary shows the name, neutral type/role chips, and a single health pill, and
// whose body shows the full readable config (SQL, grants, URIs, …) plus delete.
function ResourceRow({
  title,
  meta,
  chips,
  status,
  onDelete,
  deleting,
  children,
}: {
  title: ReactNode;
  meta?: ReactNode;
  chips?: ReactNode;
  status?: string;
  onDelete?: () => void;
  deleting?: boolean;
  children?: ReactNode;
}) {
  return (
    <details className="rounded-md border border-border bg-bg">
      <summary className="flex cursor-pointer list-none items-center gap-3 p-3">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium">{title}</p>
          {meta ? <p className="truncate font-mono text-xs text-muted">{meta}</p> : null}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {chips}
          {status ? <StatusPill status={status} /> : null}
          {onDelete ? (
            <Button disabled={deleting} onClick={(event) => { event.preventDefault(); onDelete(); }} size="icon" title="Delete" type="button" variant="ghost">
              <Trash2 size={14} />
            </Button>
          ) : null}
        </div>
      </summary>
      {children ? <div className="grid gap-2 border-t border-border p-3">{children}</div> : null}
    </details>
  );
}

function NeutralChip({ children }: { children: ReactNode }) {
  return <span className="pill neutral">{children}</span>;
}

function DetailBlock({ label, value, mono }: { label: string; value: ReactNode; mono?: boolean }) {
  return (
    <div className="rounded-md border border-border bg-surface-2 p-2.5">
      <p className="label">{label}</p>
      <p className={`mt-1 whitespace-pre-wrap break-words text-sm text-muted${mono ? " font-mono" : ""}`}>{value}</p>
    </div>
  );
}

export function DatabaseConfigPanel({ project, config, loading }: { project?: Project; config?: ProjectConfig; loading: boolean }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const configKey = `${project?.ref ?? ""}:${config?.updated_at ?? ""}`;

  useEffect(() => {
    setDraft(config?.config ?? {});
  }, [configKey, config]);

  const mutation = useMutation({
    mutationFn: ({ ref, values }: { ref: string; values: Record<string, string> }) => updateProjectConfig(ref, "database", values),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-config", variables.ref, "database"] });
      void queryClient.invalidateQueries({ queryKey: ["database-extensions", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["database-cron-jobs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["database-queues", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["database-webhooks", variables.ref] });
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
        pg_graphql_enabled: draft.pg_graphql_enabled || "true",
        database_webhooks: draft.database_webhooks || "true",
        pg_cron_enabled: draft.pg_cron_enabled || "true",
        pgmq_enabled: draft.pgmq_enabled || "true",
        fdw_enabled: draft.fdw_enabled || "true",
        vault_enabled: draft.vault_enabled || "true",
        pgvector_enabled: draft.pgvector_enabled || "true",
        supavisor_enabled: draft.supavisor_enabled || "true",
        ssl_enforced: draft.ssl_enforced || "true",
        extension_toggle_ui: draft.extension_toggle_ui || "false",
        performance_advisor_mode: draft.performance_advisor_mode || "studio",
        orioledb_profile: draft.orioledb_profile || "off",
      },
    });
  }

  const setValue = (key: string, value: string) => setDraft((current) => ({ ...current, [key]: value }));
  const setFlag = (key: string, enabled: boolean) => setValue(key, enabled ? "true" : "false");

  return (
    <AppPanel eyebrow="Database" title="Runtime" actions={<SlidersHorizontal size={15} className="text-faint" />}>
      <form className="mt-4 grid gap-4" onSubmit={submit}>
        {databaseRuntimeGroups.map((group) => (
          <SubSection key={group.title} title={group.title} description={group.description}>
            <div className="grid grid-cols-2 gap-2 max-lg:grid-cols-1">
              {group.flags.map((flag) => {
                const enabled = (draft[flag.key] ?? (flag.key === "extension_toggle_ui" ? "false" : "true")) === "true";
                return (
                  <label className="config-toggle" key={flag.key}>
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{flag.label}</p>
                      <p className="text-xs text-muted">{flag.consequence}</p>
                    </div>
                    <Switch checked={enabled} onCheckedChange={(next) => setFlag(flag.key, next)} aria-label={flag.label} />
                  </label>
                );
              })}
            </div>
          </SubSection>
        ))}
        <SubSection title="Advisors & storage engine">
          <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
            <Field label="Performance advisor mode" hint="Where advisory findings are surfaced">
              <NativeSelect value={draft.performance_advisor_mode ?? "studio"} onChange={(event) => setValue("performance_advisor_mode", event.target.value)}>
                <option value="studio">Studio advisor</option>
                <option value="fleet">Fleet advisor</option>
                <option value="off">Off</option>
              </NativeSelect>
            </Field>
            <Field label="OrioleDB profile" hint="Alternative storage engine availability">
              <NativeSelect value={draft.orioledb_profile ?? "off"} onChange={(event) => setValue("orioledb_profile", event.target.value)}>
                <option value="off">OrioleDB off</option>
                <option value="optional">OrioleDB optional</option>
                <option value="default">OrioleDB default</option>
              </NativeSelect>
            </Field>
          </div>
        </SubSection>
        <div className="usage-row">
          <p className="text-xs text-muted">{loading ? "Loading runtime settings..." : config?.updated_at ? `Updated ${formatDateTime(config.updated_at)}` : "Runtime settings not saved yet."}</p>
          <Button disabled={!project || mutation.isPending} type="submit" variant="secondary">
            <Save size={14} />
            Save runtime
          </Button>
        </div>
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </form>
    </AppPanel>
  );
}

export function DatabasePoolerPanel({ project, config, loading }: { project?: Project; config?: ProjectConfig; loading: boolean }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const configKey = `${project?.ref ?? ""}:${config?.updated_at ?? ""}`;

  useEffect(() => {
    setDraft(config?.config ?? {});
  }, [configKey, config]);

  const mutation = useMutation({
    mutationFn: ({ ref, values }: { ref: string; values: Record<string, string> }) => updateProjectConfig(ref, "pooler", values),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-config", variables.ref, "pooler"] });
      void queryClient.invalidateQueries({ queryKey: ["connect", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["cli-profile", variables.ref] });
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
        dedicated_pooler_enabled: draft.dedicated_pooler_enabled || "false",
        dedicated_pooler_tier: draft.dedicated_pooler_tier || "small",
        pool_mode: draft.pool_mode || "transaction",
        default_pool_size: draft.default_pool_size || "20",
        max_client_connections: draft.max_client_connections || "200",
      },
    });
  }

  const setValue = (key: string, value: string) => setDraft((current) => ({ ...current, [key]: value }));
  const setFlag = (key: string, enabled: boolean) => setValue(key, enabled ? "true" : "false");

  return (
    <AppPanel eyebrow="Database" title="Pooler settings" actions={<RadioTower size={15} className="text-faint" />}>
      <form className="mt-4 grid gap-3" onSubmit={submit}>
        <div className="grid grid-cols-[minmax(0,1fr)_160px_160px] gap-2 max-lg:grid-cols-1">
          <label className="config-toggle">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Dedicated pooler</p>
              <p className="truncate font-mono text-xs text-muted">co-located Supavisor</p>
            </div>
            <Switch checked={(draft.dedicated_pooler_enabled ?? "false") === "true"} onCheckedChange={(next) => setFlag("dedicated_pooler_enabled", next)} aria-label="Dedicated pooler" />
          </label>
          <NativeSelect value={draft.dedicated_pooler_tier ?? "small"} onChange={(event) => setValue("dedicated_pooler_tier", event.target.value)}>
            <option value="small">Small tier</option>
            <option value="medium">Medium tier</option>
            <option value="large">Large tier</option>
          </NativeSelect>
          <NativeSelect value={draft.pool_mode ?? "transaction"} onChange={(event) => setValue("pool_mode", event.target.value)}>
            <option value="transaction">Transaction</option>
            <option value="session">Session</option>
            <option value="both">Both modes</option>
          </NativeSelect>
        </div>
        <div className="grid grid-cols-4 gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
          <Field label="Default pool size" hint="server connections per pool">
            <Input className="font-mono" inputMode="numeric" min="1" value={draft.default_pool_size ?? "20"} onChange={(event) => setValue("default_pool_size", event.target.value)} type="number" />
          </Field>
          <Field label="Max client connections" hint="accepted client connections">
            <Input className="font-mono" inputMode="numeric" min="1" value={draft.max_client_connections ?? "200"} onChange={(event) => setValue("max_client_connections", event.target.value)} type="number" />
          </Field>
          <div className="rounded-md border border-border bg-surface-2 px-3 py-2">
            <p className="label">Transaction port</p>
            <p className="mt-1 font-mono text-sm">6543</p>
          </div>
          <div className="rounded-md border border-border bg-surface-2 px-3 py-2">
            <p className="label">Session port</p>
            <p className="mt-1 font-mono text-sm">5432</p>
          </div>
        </div>
        <div className="usage-row">
          <p className="text-xs text-muted">{loading ? "Loading pooler settings..." : config?.updated_at ? `Updated ${formatDateTime(config.updated_at)}` : "Pooler settings not saved yet."}</p>
          <Button disabled={!project || mutation.isPending} type="submit" variant="secondary">
            <Save size={14} />
            Save pooler
          </Button>
        </div>
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </form>
    </AppPanel>
  );
}

export function BranchesPanel({ project, branches, loading, onSelect, enabled }: { project?: Project; branches: ProjectBranch[]; loading: boolean; onSelect: (ref: string) => void; enabled: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({ ref: "", name: "", ttl_hours: 24, with_data: false });
  const [deleteTarget, setDeleteTarget] = useState("");
  const [deleteConfirmation, setDeleteConfirmation] = useState("");
  const projectRef = project?.ref ?? "";
  useEffect(() => {
    if (project) {
      setForm({
        ref: `${project.ref}-preview`,
        name: `${project.name} Preview`,
        ttl_hours: 24,
        with_data: false,
      });
    }
  }, [projectRef, project]);
  const mutation = useMutation({
    mutationFn: ({ ref, input }: { ref: string; input: { ref: string; name: string; ttl_hours: number; with_data: boolean } }) => createProjectBranch(ref, input),
    onSuccess: (payload, variables) => {
      onSelect(payload.project.ref);
      void queryClient.invalidateQueries({ queryKey: ["projects"] });
      void queryClient.invalidateQueries({ queryKey: ["project-branches", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["connect", payload.project.ref] });
      void queryClient.invalidateQueries({ queryKey: ["cli-profile", payload.project.ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-route-manifest", payload.project.ref] });
      void queryClient.invalidateQueries({ queryKey: ["org-quota", project?.org_id ?? ""] });
      void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, branchRef }: { ref: string; branchRef: string }) => deleteProjectBranch(ref, branchRef),
    onSuccess: (_, variables) => {
      setDeleteTarget("");
      setDeleteConfirmation("");
      void queryClient.invalidateQueries({ queryKey: ["projects"] });
      void queryClient.invalidateQueries({ queryKey: ["project-branches", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["project", variables.branchRef] });
      void queryClient.invalidateQueries({ queryKey: ["connect", variables.branchRef] });
      void queryClient.invalidateQueries({ queryKey: ["cli-profile", variables.branchRef] });
      void queryClient.invalidateQueries({ queryKey: ["project-route-manifest", variables.branchRef] });
      void queryClient.invalidateQueries({ queryKey: ["org-quota", project?.org_id ?? ""] });
      void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
      void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
      void queryClient.invalidateQueries({ queryKey: ["hosts"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!enabled || !project || form.ref.trim().length === 0 || form.name.trim().length === 0) {
      return;
    }
    mutation.mutate({ ref: project.ref, input: form });
  }

  return (
    <AppPanel eyebrow="Branches" title="Preview stacks" actions={<GitBranch size={15} className="text-faint" />}>
      {!enabled ? (
        <div className="mt-4 rounded-md border border-border bg-bg p-3">
          <p className="text-sm font-medium">Preview branches disabled</p>
          <p className="mt-1 text-sm text-muted">Enable the preview_branches feature flag for this org before creating branch stacks.</p>
        </div>
      ) : null}
      <form className="mt-4 grid gap-2" onSubmit={submit}>
        <div className="grid grid-cols-[minmax(0,1fr)_96px] gap-2 max-sm:grid-cols-1">
          <Input className="font-mono" disabled={!enabled} placeholder="alpha-preview" value={form.ref} onChange={(event) => setForm({ ...form, ref: event.target.value })} />
          <Input disabled={!enabled} min={0} type="number" value={form.ttl_hours} onChange={(event) => setForm({ ...form, ttl_hours: Number(event.target.value) })} />
        </div>
        <div className="flex gap-2 max-sm:flex-col">
          <Input disabled={!enabled} placeholder="Preview name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
          <label className="checkbox-row compact">
            <input checked={form.with_data} disabled={!enabled} onChange={(event) => setForm({ ...form, with_data: event.target.checked })} type="checkbox" />
            Clone data
          </label>
          <Button className="justify-self-start" disabled={!enabled || !project || mutation.isPending || form.ref.trim().length === 0} type="submit" variant="secondary">
            <Plus size={14} />
            Branch
          </Button>
        </div>
      </form>
      <div className="mt-3 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading branches...</p> : null}
        {!loading && branches.length === 0 ? (
          <EmptyState
            icon={GitBranch}
            title="No preview branches"
            description={enabled ? "Spin up an isolated copy of this project from the form above." : "Enable the preview_branches feature flag to create branches."}
          />
        ) : null}
        {branches.map((branch) => (
          <div className="branch-row" key={branch.id}>
            <div className="min-w-0">
              <button className="w-full min-w-0 text-left" onClick={() => onSelect(branch.project_ref)} type="button">
              <p className="truncate text-sm font-medium">{branch.name}</p>
              <p className="truncate font-mono text-xs text-muted">{branch.project_ref}</p>
              <p className="truncate text-xs text-faint">{branch.with_data ? "Includes source data" : "Schema-only branch"}</p>
              {branch.expires_at ? <p className="truncate text-xs text-faint">Expires {formatTime(branch.expires_at)}</p> : null}
              </button>
              {deleteTarget === branch.id ? (
                <div className="mt-2 grid grid-cols-[minmax(0,1fr)_auto_auto] gap-2 max-sm:grid-cols-1">
                  <Input
                    className="font-mono"
                    placeholder={branch.project_ref}
                    value={deleteConfirmation}
                    onChange={(event) => setDeleteConfirmation(event.target.value)}
                  />
                  <Button className="justify-self-start" onClick={() => { setDeleteTarget(""); setDeleteConfirmation(""); }} type="button" variant="secondary">
                    <X size={14} />
                  </Button>
                  <Button
                    className="justify-self-start"
                    disabled={!project || deleteMutation.isPending || deleteConfirmation !== branch.project_ref}
                    onClick={() => project && deleteMutation.mutate({ ref: project.ref, branchRef: branch.project_ref })}
                    type="button"
                    variant="danger"
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              ) : null}
            </div>
            <div className="flex items-center gap-2">
              <StatusPill status={branch.status} />
              <Button onClick={() => onSelect(branch.project_ref)} size="icon" type="button" variant="ghost">
                <ExternalLink size={14} />
              </Button>
              <Button
                disabled={!project || deleteMutation.isPending}
                onClick={() => { setDeleteTarget(branch.id); setDeleteConfirmation(""); }}
                size="icon"
                title="Delete branch"
                type="button"
                variant="ghost"
              >
                <Trash2 size={14} />
              </Button>
            </div>
          </div>
        ))}
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function ReplicasPanel({ project, hosts, replicas, routing, loading, enabled }: { project?: Project; hosts: Host[]; replicas: ProjectReplica[]; routing?: ProjectReplicaRouting; loading: boolean; enabled: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({ name: "east", host_id: "", region: "local", tier: "small", read_weight: 100, failover_priority: 1 });
  const [deleteTarget, setDeleteTarget] = useState("");
  const [deleteConfirmation, setDeleteConfirmation] = useState("");
  const projectRef = project?.ref ?? "";
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["project-replicas", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-replica-routing", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["org-quota", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["hosts"] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  useEffect(() => {
    if (project) {
      setForm((current) => ({
        ...current,
        name: current.name || "east",
        tier: project.spec.resource_tier || "small",
      }));
    }
  }, [projectRef, project]);
  const mutation = useMutation({
    mutationFn: ({ ref, input }: { ref: string; input: { name: string; host_id: string; region: string; tier: string; read_weight: number; failover_priority: number } }) => createProjectReplica(ref, input),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const promoteMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => promoteProjectReplica(ref, id, "manual promotion from admin UI"),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => deleteProjectReplica(ref, id),
    onSuccess: (_, variables) => {
      setDeleteTarget("");
      setDeleteConfirmation("");
      invalidate(variables.ref);
    },
  });
  const failoverMutation = useMutation({
    mutationFn: (ref: string) => failoverProjectReplica(ref, "operator-triggered failover from admin UI"),
    onSuccess: (_, ref) => invalidate(ref),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!enabled || !project || form.name.trim().length === 0) {
      return;
    }
    mutation.mutate({ ref: project.ref, input: form });
  }

  return (
    <AppPanel eyebrow="Replicas" title="Read scaling" actions={<Database size={15} className="text-faint" />}>
      {!enabled ? (
        <div className="mt-4 rounded-md border border-border bg-bg p-3">
          <p className="text-sm font-medium">Read replicas disabled</p>
          <p className="mt-1 text-sm text-muted">Enable the read_replicas feature flag for this org before provisioning replica stacks.</p>
        </div>
      ) : null}
      <form className="mt-4 grid gap-2" onSubmit={submit}>
        <div className="grid grid-cols-[minmax(0,1fr)_110px] gap-2 max-sm:grid-cols-1">
          <Input className="font-mono" disabled={!enabled} placeholder="east" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
          <NativeSelect disabled={!enabled} value={form.tier} onChange={(event) => setForm({ ...form, tier: event.target.value })}>
            <option value="small">Small</option>
            <option value="medium">Medium</option>
            <option value="large">Large</option>
          </NativeSelect>
        </div>
        <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2 max-sm:grid-cols-1">
          <NativeSelect disabled={!enabled} value={form.host_id} onChange={(event) => setForm({ ...form, host_id: event.target.value })}>
            <option value="">Default host</option>
            {hosts.map((host) => (
              <option key={host.id} value={host.id}>
                {host.name} · {host.address}
              </option>
            ))}
          </NativeSelect>
          <Input disabled={!enabled} placeholder="region" value={form.region} onChange={(event) => setForm({ ...form, region: event.target.value })} />
          <Button className="justify-self-start" disabled={!enabled || !project || mutation.isPending || form.name.trim().length === 0} type="submit" variant="secondary">
            <Plus size={14} />
            Replica
          </Button>
        </div>
        <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
          <Field label="Read weight" hint="relative share of read traffic">
            <Input disabled={!enabled} min={0} type="number" value={form.read_weight} onChange={(event) => setForm({ ...form, read_weight: Number(event.target.value) })} />
          </Field>
          <Field label="Failover priority" hint="lower promotes first">
            <Input disabled={!enabled} min={1} type="number" value={form.failover_priority} onChange={(event) => setForm({ ...form, failover_priority: Number(event.target.value) })} />
          </Field>
        </div>
      </form>
      {routing ? (
        <div className="quota-row mt-3">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Routing</p>
            <p className="truncate font-mono text-xs text-muted">{routing.primary_uri}</p>
            <p className="truncate text-xs text-faint">{routing.read_strategy} · {routing.healthy_read_targets.length} read targets · auto-failover {routing.auto_failover ? "on" : "off"}</p>
          </div>
          <div className="flex items-center gap-2">
            {routing.failover_candidate ? <NeutralChip>{routing.failover_candidate.name} next</NeutralChip> : <StatusPill tone="warning" label="no candidate" />}
            <Button disabled={!project || failoverMutation.isPending || !routing.failover_candidate} onClick={() => project && failoverMutation.mutate(project.ref)} type="button" variant="secondary">
              <ShieldCheck size={14} />
              Failover
            </Button>
          </div>
        </div>
      ) : null}
      <div className="mt-3 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading replicas...</p> : null}
        {!loading && replicas.length === 0 ? (
          <EmptyState
            icon={Database}
            title="No read replicas"
            description={enabled ? "Provision a replica from the form above to scale read traffic." : "Enable the read_replicas feature flag to provision replicas."}
          />
        ) : null}
        {replicas.map((replica) => (
          <div className="replica-row" key={replica.id}>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{replica.name}</p>
              <p className="truncate font-mono text-xs text-muted">{replica.public_read_uri || replica.read_uri}</p>
              {replica.internal_read_uri ? <p className="truncate font-mono text-xs text-faint">{replica.internal_read_uri}</p> : null}
              <p className="truncate text-xs text-faint">{replica.region || "local"} · {replica.tier} · weight {replica.read_weight} · priority {replica.failover_priority}{replica.message ? ` · ${replica.message}` : ""}</p>
              {deleteTarget === replica.id ? (
                <div className="mt-2 grid grid-cols-[minmax(0,1fr)_auto_auto] gap-2 max-sm:grid-cols-1">
                  <Input
                    className="font-mono"
                    placeholder={replica.name}
                    value={deleteConfirmation}
                    onChange={(event) => setDeleteConfirmation(event.target.value)}
                  />
                  <Button className="justify-self-start" onClick={() => { setDeleteTarget(""); setDeleteConfirmation(""); }} type="button" variant="secondary">
                    <X size={14} />
                  </Button>
                  <Button
                    className="justify-self-start"
                    disabled={!project || deleteMutation.isPending || deleteConfirmation !== replica.name}
                    onClick={() => project && deleteMutation.mutate({ ref: project.ref, id: replica.id })}
                    type="button"
                    variant="danger"
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              ) : null}
            </div>
            <div className="flex items-center gap-2">
              <NeutralChip>{replica.role || "read"}</NeutralChip>
              <StatusPill status={replica.status} />
              <Button disabled={!project || replica.status !== "healthy" || replica.role === "primary" || promoteMutation.isPending} onClick={() => project && promoteMutation.mutate({ ref: project.ref, id: replica.id })} size="icon" type="button" variant="ghost">
                <ShieldCheck size={14} />
              </Button>
              <Button onClick={() => void navigator.clipboard.writeText(replica.public_read_uri || replica.read_uri)} size="icon" type="button" variant="ghost">
                <Copy size={14} />
              </Button>
              <Button
                disabled={!project || replica.role === "primary" || deleteMutation.isPending}
                onClick={() => { setDeleteTarget(replica.id); setDeleteConfirmation(""); }}
                size="icon"
                title="Delete replica"
                type="button"
                variant="ghost"
              >
                <Trash2 size={14} />
              </Button>
            </div>
          </div>
        ))}
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        {promoteMutation.error ? <p className="text-sm text-danger">{promoteMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
        {failoverMutation.error ? <p className="text-sm text-danger">{failoverMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function ReplicationPanel({ project, pipelines, loading }: { project?: Project; pipelines: ProjectReplicationPipeline[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    name: "",
    type: "etl",
    source_schema: "public",
    source_table: "",
    destination: "s3",
    destination_uri: "",
    credential_handle: "",
    config: "",
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["replication-pipelines", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref, input }: { ref: string; input: {
      name: string;
      type: string;
      source_schema: string;
      source_table: string;
      destination: string;
      destination_uri: string;
      credential_handle: string;
      config: Record<string, string>;
    } }) => createProjectReplicationPipeline(ref, input),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => deleteProjectReplicationPipeline(ref, id),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!project || form.source_table.trim().length === 0 || form.destination.trim().length === 0) {
      return;
    }
    createMutation.mutate({
      ref: project.ref,
      input: {
        name: form.name,
        type: form.type,
        source_schema: form.source_schema,
        source_table: form.source_table,
        destination: form.destination,
        destination_uri: form.destination_uri,
        credential_handle: form.credential_handle,
        config: parseKeyValueLines(form.config),
      },
    });
  }

  return (
    <AppPanel eyebrow="Replication" title="ETL pipelines" actions={<Database size={15} className="text-faint" />}>
      <form className="mt-4 grid gap-2" onSubmit={submit}>
        <div className="grid grid-cols-[minmax(0,1fr)_120px_150px] gap-2 max-sm:grid-cols-1">
          <Field label="Pipeline name" required>
            <Input className="font-mono" placeholder="orders-etl" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
          </Field>
          <Field label="Type">
            <NativeSelect value={form.type} onChange={(event) => setForm({ ...form, type: event.target.value })}>
              <option value="logical">Logical</option>
              <option value="etl">ETL</option>
              <option value="analytics_bucket">Bucket</option>
            </NativeSelect>
          </Field>
          <Field label="Destination">
            <NativeSelect value={form.destination} onChange={(event) => setForm({ ...form, destination: event.target.value })}>
              <option value="postgres">Postgres</option>
              <option value="webhook">Webhook</option>
              <option value="s3">S3</option>
              <option value="iceberg">Iceberg</option>
              <option value="bigquery">BigQuery</option>
              <option value="snowflake">Snowflake</option>
              <option value="redshift">Redshift</option>
            </NativeSelect>
          </Field>
        </div>
        <div className="grid grid-cols-[120px_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
          <Field label="Source schema">
            <Input className="font-mono" placeholder="public" value={form.source_schema} onChange={(event) => setForm({ ...form, source_schema: event.target.value })} />
          </Field>
          <Field label="Source table" required>
            <Input className="font-mono" placeholder="orders" value={form.source_table} onChange={(event) => setForm({ ...form, source_table: event.target.value })} />
          </Field>
        </div>
        <Field label="Destination URI">
          <Input className="font-mono" placeholder="s3://lakehouse/events" value={form.destination_uri} onChange={(event) => setForm({ ...form, destination_uri: event.target.value })} />
        </Field>
        <Field label="Credential handle">
          <Input className="font-mono" placeholder="secret://projects/ref/replication-credential" value={form.credential_handle} onChange={(event) => setForm({ ...form, credential_handle: event.target.value })} />
        </Field>
        <Field label="Config" hint="key=value per line">
          <Textarea className="min-h-[72px] font-mono" placeholder={"bucket=analytics-lake\nprefix=orders/"} value={form.config} onChange={(event) => setForm({ ...form, config: event.target.value })} />
        </Field>
        <Button className="justify-self-start" disabled={!project || createMutation.isPending || form.source_table.trim().length === 0} type="submit" variant="secondary">
          <Plus size={14} />
          Add pipeline
        </Button>
      </form>
      <div className="mt-3 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading replication pipelines...</p> : null}
        {!loading && pipelines.length === 0 ? (
          <EmptyState icon={Database} title="No replication pipelines" description="Stream changes to an external destination using the form above." />
        ) : null}
        {pipelines.map((pipeline) => (
          <ResourceRow
            key={pipeline.id}
            title={pipeline.name}
            meta={`${pipeline.source_schema}.${pipeline.source_table} → ${pipeline.destination}`}
            chips={<NeutralChip>{pipeline.type}</NeutralChip>}
            status={pipeline.status}
            onDelete={() => project && deleteMutation.mutate({ ref: project.ref, id: pipeline.id })}
            deleting={!project || deleteMutation.isPending}
          >
            <DetailBlock label="Destination" value={pipeline.destination_uri || pipeline.destination} mono />
            {Object.keys(pipeline.config).length > 0 ? (
              <DetailBlock label="Config" value={Object.entries(pipeline.config).map(([key, value]) => `${key}=${value}`).join("\n")} mono />
            ) : null}
            {pipeline.message ? <DetailBlock label="Message" value={pipeline.message} /> : null}
          </ResourceRow>
        ))}
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function AnalyticsBucketsPanel({ project, buckets, loading }: { project?: Project; buckets: ProjectAnalyticsBucket[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    name: "",
    storage_uri: "",
    catalog_uri: "",
    warehouse: "",
    credential_handle: "",
    format_version: "2",
    partitioning: "",
    retention_days: "",
    compaction_schedule: "",
    metadata: "",
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["analytics-buckets", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectAnalyticsBucket(ref, {
      name: form.name,
      storage_uri: form.storage_uri,
      catalog_uri: form.catalog_uri,
      warehouse: form.warehouse,
      credential_handle: form.credential_handle,
      format_version: Number(form.format_version) || 0,
      partitioning: form.partitioning,
      retention_days: Number(form.retention_days) || 0,
      compaction_schedule: form.compaction_schedule,
      metadata: parseKeyValueLines(form.metadata),
    }),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, name }: { ref: string; name: string }) => deleteProjectAnalyticsBucket(ref, name),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!project || form.name.trim().length === 0 || form.storage_uri.trim().length === 0) {
      return;
    }
    createMutation.mutate({ ref: project.ref });
  }

  return (
    <AppPanel eyebrow="Analytics" title="Iceberg buckets" actions={<Database size={15} className="text-faint" />}>
      <div className="mt-4 grid gap-4">
        <form className="grid gap-2" onSubmit={submit}>
          <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
            <Field label="Bucket name" required>
              <Input className="font-mono" placeholder="events" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            </Field>
            <Field label="Storage URI" required>
              <Input className="font-mono" placeholder="s3://lakehouse/events" value={form.storage_uri} onChange={(event) => setForm({ ...form, storage_uri: event.target.value })} />
            </Field>
            <Field label="Warehouse">
              <Input className="font-mono" placeholder="analytics" value={form.warehouse} onChange={(event) => setForm({ ...form, warehouse: event.target.value })} />
            </Field>
          </div>
          <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
            <Field label="Catalog URI">
              <Input className="font-mono" placeholder="http://iceberg-rest:8181" value={form.catalog_uri} onChange={(event) => setForm({ ...form, catalog_uri: event.target.value })} />
            </Field>
            <Field label="Credential handle">
              <Input className="font-mono" placeholder="secret://projects/ref/iceberg" value={form.credential_handle} onChange={(event) => setForm({ ...form, credential_handle: event.target.value })} />
            </Field>
          </div>
          <div className="grid grid-cols-[100px_minmax(0,1fr)_110px_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
            <Field label="Iceberg format">
              <NativeSelect value={form.format_version} onChange={(event) => setForm({ ...form, format_version: event.target.value })}>
                <option value="2">V2</option>
                <option value="1">V1</option>
              </NativeSelect>
            </Field>
            <Field label="Partitioning">
              <Input className="font-mono" placeholder="days(created_at)" value={form.partitioning} onChange={(event) => setForm({ ...form, partitioning: event.target.value })} />
            </Field>
            <Field label="Retention" hint="days · 0 = indefinite">
              <Input className="font-mono" inputMode="numeric" placeholder="365" value={form.retention_days} onChange={(event) => setForm({ ...form, retention_days: event.target.value })} />
            </Field>
            <Field label="Compaction">
              <Input className="font-mono" placeholder="manual" value={form.compaction_schedule} onChange={(event) => setForm({ ...form, compaction_schedule: event.target.value })} />
            </Field>
          </div>
          <Field label="Metadata" hint="key=value pairs">
            <Input className="font-mono" placeholder="purpose=warehouse" value={form.metadata} onChange={(event) => setForm({ ...form, metadata: event.target.value })} />
          </Field>
          <Button className="justify-self-start" disabled={!project || createMutation.isPending || form.storage_uri.trim().length === 0} type="submit" variant="secondary">
            <Plus size={14} />
            Add bucket
          </Button>
        </form>
        <div className="grid gap-2">
          {loading ? <p className="text-sm text-muted">Loading analytics buckets...</p> : null}
          {!loading && buckets.length === 0 ? (
            <EmptyState icon={Database} title="No analytics buckets" description="Register an Iceberg analytics bucket using the form above." />
          ) : null}
          {buckets.map((bucket) => (
            <ResourceRow
              key={bucket.id}
              title={bucket.name}
              meta={`${bucket.storage_uri} · Iceberg v${bucket.format_version}`}
              chips={<NeutralChip>{bucket.warehouse || "no warehouse"}</NeutralChip>}
              status={bucket.status}
              onDelete={() => project && deleteMutation.mutate({ ref: project.ref, name: bucket.name })}
              deleting={!project || deleteMutation.isPending}
            >
              <DetailBlock label="Storage URI" value={bucket.storage_uri} mono />
              {bucket.catalog_uri ? <DetailBlock label="Catalog URI" value={bucket.catalog_uri} mono /> : null}
              <DetailBlock label="Retention & compaction" value={`${bucket.retention_days === 0 ? "indefinite" : `${bucket.retention_days} days`} · ${bucket.compaction_schedule || "manual"}${bucket.partitioning ? ` · ${bucket.partitioning}` : ""}`} />
            </ResourceRow>
          ))}
          {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
          {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
        </div>
      </div>
    </AppPanel>
  );
}

export function DatabaseExtensionsPanel({ project, extensions, loading }: { project?: Project; extensions: ProjectDatabaseExtension[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [forms, setForms] = useState<Record<string, { schema: string; version: string; enabled: boolean }>>({});
  useEffect(() => {
    setForms((current) => {
      const next = { ...current };
      for (const extension of extensions) {
        if (!next[extension.name]) {
          next[extension.name] = { schema: extension.schema, version: extension.version ?? "", enabled: extension.enabled };
        }
      }
      return next;
    });
  }, [extensions]);
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["database-extensions", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const updateMutation = useMutation({
    mutationFn: ({ ref, name, form }: { ref: string; name: string; form: { schema: string; version: string; enabled: boolean } }) => updateProjectDatabaseExtension(ref, name, form),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const updateForm = (name: string, patch: Partial<{ schema: string; version: string; enabled: boolean }>) => {
    setForms((current) => ({
      ...current,
      [name]: {
        schema: current[name]?.schema ?? "",
        version: current[name]?.version ?? "",
        enabled: current[name]?.enabled ?? true,
        ...patch,
      },
    }));
  };
  return (
    <AppPanel eyebrow="Database" title="Extensions" actions={<SlidersHorizontal size={15} className="text-faint" />}>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading database extensions...</p> : null}
        {!loading && extensions.length === 0 ? (
          <EmptyState icon={SlidersHorizontal} title="No extensions reported" description="Postgres extensions appear here once the project reports them." />
        ) : null}
        {extensions.map((extension) => {
          const form = forms[extension.name] ?? { schema: extension.schema, version: extension.version ?? "", enabled: extension.enabled };
          return (
            <div className="extension-row" key={extension.name}>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{extension.name}</p>
                <p className="truncate text-xs text-faint">{extension.message || extension.status}</p>
              </div>
              <Input className="font-mono" value={form.schema} onChange={(event) => updateForm(extension.name, { schema: event.target.value })} />
              <Input className="font-mono" placeholder="version" value={form.version} onChange={(event) => updateForm(extension.name, { version: event.target.value })} />
              <label className="checkbox-row compact">
                <input type="checkbox" checked={form.enabled} onChange={(event) => updateForm(extension.name, { enabled: event.target.checked })} />
                {form.enabled ? "Enabled" : "Disabled"}
              </label>
              <Button disabled={!project || updateMutation.isPending} onClick={() => project && updateMutation.mutate({ ref: project.ref, name: extension.name, form })} size="icon" type="button" variant="ghost">
                <Save size={14} />
              </Button>
            </div>
          );
        })}
        {updateMutation.error ? <p className="text-sm text-danger">{updateMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function DatabaseCronPanel({ project, jobs, loading }: { project?: Project; jobs: ProjectDatabaseCronJob[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedItem = pathname.match(/^\/projects\/[^/]+\/database\/cron\/([^/]+)/)?.[1];
  const selectedName = selectedItem ? decodeURIComponent(selectedItem) : "";
  const selectedJob = selectedName && selectedName !== "new" ? jobs.find((job) => job.name === selectedName) : undefined;
  const basePath = project ? projectPath(project.ref, "database", "cron") : "";
  const [form, setForm] = useState({
    name: "",
    schedule: "",
    command: "",
    database: "postgres",
    username: "postgres",
    active: true,
    timeout_seconds: 60,
    max_runtime_seconds: 120,
    metadata: "",
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["database-cron-jobs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectDatabaseCronJob(ref, {
      name: form.name,
      schedule: form.schedule,
      command: form.command,
      database: form.database,
      username: form.username,
      active: form.active,
      timeout_seconds: form.timeout_seconds,
      max_runtime_seconds: form.max_runtime_seconds,
      metadata: parseKeyValueLines(form.metadata),
    }),
    onSuccess: (_, variables) => {
      invalidate(variables.ref);
      void navigate({ to: projectPath(variables.ref, "database", "cron") });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, name }: { ref: string; name: string }) => deleteProjectDatabaseCronJob(ref, name),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!project || form.name.trim().length === 0 || form.command.trim().length === 0) {
      return;
    }
    createMutation.mutate({ ref: project.ref });
  }

  return (
    <AppPanel eyebrow="Database" title="Cron jobs" actions={<RotateCcw size={15} className="text-faint" />}>
      {!selectedItem ? (
        <div className="mt-4 grid gap-3">
          <Button className="w-fit" disabled={!project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button" variant="secondary">
            <Plus size={14} />
            Add cron job
          </Button>
          <div className="grid gap-2">
            {loading ? <p className="text-sm text-muted">Loading cron jobs...</p> : null}
            {!loading && jobs.length === 0 ? (
              <EmptyState
                icon={RotateCcw}
                title="No cron jobs"
                description="Schedule a pg_cron job to run SQL on a recurring schedule."
                action={
                  <Button disabled={!project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button" variant="secondary">
                    <Plus size={14} />
                    Add cron job
                  </Button>
                }
              />
            ) : null}
            {jobs.map((job) => (
              <ResourceRow
                key={job.id}
                title={job.name}
                meta={`${job.schedule} · ${job.database}.${job.username}`}
                chips={<NeutralChip>{job.active ? "active" : "paused"}</NeutralChip>}
                status={job.status}
                onDelete={() => project && deleteMutation.mutate({ ref: project.ref, name: job.name })}
                deleting={!project || deleteMutation.isPending}
              >
                <DetailBlock label="Command" value={job.command} mono />
                <DetailBlock label="Limits" value={`timeout ${job.timeout_seconds}s · max runtime ${job.max_runtime_seconds}s`} />
              </ResourceRow>
            ))}
          </div>
        </div>
      ) : null}
      {selectedItem === "new" ? (
        <form className="mt-4 grid gap-2" onSubmit={onSubmit}>
          <DatabaseDetailHeader detail="Create one pg_cron job declaration for this project." title="New cron job" onBack={() => basePath && void navigate({ to: basePath })} />
          <div className="grid grid-cols-[minmax(0,1fr)_160px_auto] gap-2 max-lg:grid-cols-1">
            <Field label="Job name" required>
              <Input className="font-mono" placeholder="refresh-rollups" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            </Field>
            <Field label="Schedule" hint="cron expression">
              <Input className="font-mono" placeholder="*/15 * * * *" value={form.schedule} onChange={(event) => setForm({ ...form, schedule: event.target.value })} />
            </Field>
            <label className="flex items-center gap-2 text-sm self-end">
              <Switch checked={form.active} onCheckedChange={(next) => setForm({ ...form, active: next })} aria-label="Active" />
              Active
            </label>
          </div>
          <Field label="Command" required hint="SQL run on the schedule">
            <Textarea className="min-h-[84px] font-mono" placeholder="select analytics.refresh_rollups();" value={form.command} onChange={(event) => setForm({ ...form, command: event.target.value })} />
          </Field>
          <div className="grid grid-cols-4 gap-2 max-lg:grid-cols-2 max-sm:grid-cols-1">
            <Field label="Database">
              <Input className="font-mono" placeholder="postgres" value={form.database} onChange={(event) => setForm({ ...form, database: event.target.value })} />
            </Field>
            <Field label="Run as user">
              <Input className="font-mono" placeholder="postgres" value={form.username} onChange={(event) => setForm({ ...form, username: event.target.value })} />
            </Field>
            <Field label="Statement timeout" hint="seconds">
              <Input min={1} type="number" value={form.timeout_seconds} onChange={(event) => setForm({ ...form, timeout_seconds: Number(event.target.value) })} />
            </Field>
            <Field label="Max runtime" hint="seconds">
              <Input min={1} type="number" value={form.max_runtime_seconds} onChange={(event) => setForm({ ...form, max_runtime_seconds: Number(event.target.value) })} />
            </Field>
          </div>
          <Field label="Metadata" hint="key=value pairs">
            <Input className="font-mono" placeholder="owner=analytics" value={form.metadata} onChange={(event) => setForm({ ...form, metadata: event.target.value })} />
          </Field>
          <Button className="justify-self-start" disabled={!project || createMutation.isPending || form.name.trim().length === 0 || form.command.trim().length === 0} type="submit" variant="secondary">
            <Plus size={14} />
            Add cron job
          </Button>
        </form>
      ) : null}
      {selectedItem && selectedItem !== "new" ? (
        <div className="mt-4 grid gap-3">
          <DatabaseDetailHeader detail={selectedJob ? `${selectedJob.schedule} on ${selectedJob.database}.${selectedJob.username}` : "Cron job not found in the current project."} title={selectedName} onBack={() => basePath && void navigate({ to: basePath })} />
          {selectedJob ? (
            <div className="grid gap-2">
              <div className="metric-cell">
                <p className="label">Command</p>
                <p className="mt-1 font-mono text-sm text-muted">{selectedJob.command}</p>
              </div>
              <div className="metric-grid">
                <div className="metric-cell"><p className="label">Status</p><p className="text-sm font-medium">{selectedJob.status}</p></div>
                <div className="metric-cell"><p className="label">Timeout</p><p className="text-sm font-medium">{selectedJob.timeout_seconds}s</p></div>
                <div className="metric-cell"><p className="label">Max runtime</p><p className="text-sm font-medium">{selectedJob.max_runtime_seconds}s</p></div>
                <div className="metric-cell"><p className="label">Updated</p><p className="text-sm font-medium">{formatTime(selectedJob.updated_at)}</p></div>
              </div>
              <Button className="w-fit" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, name: selectedJob.name })} type="button" variant="danger">
                <X size={14} />
                Delete cron job
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="mt-4 grid gap-2">
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function DatabaseQueuesPanel({ project, queues, loading }: { project?: Project; queues: ProjectDatabaseQueue[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedItem = pathname.match(/^\/projects\/[^/]+\/database\/queues\/([^/]+)/)?.[1];
  const selectedName = selectedItem ? decodeURIComponent(selectedItem) : "";
  const selectedQueue = selectedName && selectedName !== "new" ? queues.find((queue) => queue.name === selectedName) : undefined;
  const basePath = project ? projectPath(project.ref, "database", "queues") : "";
  const [form, setForm] = useState({
    name: "",
    schema: "pgmq",
    retention_minutes: 10080,
    visibility_timeout_seconds: 45,
    max_retries: 5,
    dead_letter_queue: "",
    active: true,
    metadata: "",
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["database-queues", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectDatabaseQueue(ref, {
      name: form.name,
      schema: form.schema,
      retention_minutes: form.retention_minutes,
      visibility_timeout_seconds: form.visibility_timeout_seconds,
      max_retries: form.max_retries,
      dead_letter_queue: form.dead_letter_queue,
      active: form.active,
      metadata: parseKeyValueLines(form.metadata),
    }),
    onSuccess: (_, variables) => {
      invalidate(variables.ref);
      void navigate({ to: projectPath(variables.ref, "database", "queues") });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, name }: { ref: string; name: string }) => deleteProjectDatabaseQueue(ref, name),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!project || form.name.trim().length === 0) {
      return;
    }
    createMutation.mutate({ ref: project.ref });
  }

  return (
    <AppPanel eyebrow="Database" title="Queues" actions={<Boxes size={15} className="text-faint" />}>
      {!selectedItem ? (
        <div className="mt-4 grid gap-3">
          <Button className="w-fit" disabled={!project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button" variant="secondary">
            <Plus size={14} />
            Add queue
          </Button>
          <div className="grid gap-2">
            {loading ? <p className="text-sm text-muted">Loading queues...</p> : null}
            {!loading && queues.length === 0 ? (
              <EmptyState
                icon={Boxes}
                title="No queues"
                description="Declare a pgmq queue for transactional background work."
                action={
                  <Button disabled={!project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button" variant="secondary">
                    <Plus size={14} />
                    Add queue
                  </Button>
                }
              />
            ) : null}
            {queues.map((queue) => (
              <ResourceRow
                key={queue.id}
                title={queue.name}
                meta={`${queue.schema} · retain ${queue.retention_minutes}m · vt ${queue.visibility_timeout_seconds}s`}
                chips={<NeutralChip>{queue.active ? "active" : "paused"}</NeutralChip>}
                status={queue.status}
                onDelete={() => project && deleteMutation.mutate({ ref: project.ref, name: queue.name })}
                deleting={!project || deleteMutation.isPending}
              >
                <DetailBlock label="Delivery" value={`retention ${queue.retention_minutes}m · visibility ${queue.visibility_timeout_seconds}s · ${queue.max_retries} retries`} />
                <DetailBlock label="Dead-letter queue" value={queue.dead_letter_queue || "none"} mono />
              </ResourceRow>
            ))}
          </div>
        </div>
      ) : null}
      {selectedItem === "new" ? (
        <form className="mt-4 grid gap-2" onSubmit={onSubmit}>
          <DatabaseDetailHeader detail="Create one pgmq queue declaration for this project." title="New queue" onBack={() => basePath && void navigate({ to: basePath })} />
          <div className="grid grid-cols-[minmax(0,1fr)_140px_auto] gap-2 max-lg:grid-cols-1">
            <Field label="Queue name" required>
              <Input className="font-mono" placeholder="events" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            </Field>
            <Field label="Schema">
              <Input className="font-mono" placeholder="pgmq" value={form.schema} onChange={(event) => setForm({ ...form, schema: event.target.value })} />
            </Field>
            <label className="flex items-center gap-2 text-sm self-end">
              <Switch checked={form.active} onCheckedChange={(next) => setForm({ ...form, active: next })} aria-label="Active" />
              Active
            </label>
          </div>
          <div className="grid grid-cols-4 gap-2 max-lg:grid-cols-2 max-sm:grid-cols-1">
            <Field label="Retention" hint="minutes">
              <Input min={1} type="number" value={form.retention_minutes} onChange={(event) => setForm({ ...form, retention_minutes: Number(event.target.value) })} />
            </Field>
            <Field label="Visibility timeout" hint="seconds">
              <Input min={1} type="number" value={form.visibility_timeout_seconds} onChange={(event) => setForm({ ...form, visibility_timeout_seconds: Number(event.target.value) })} />
            </Field>
            <Field label="Max retries" hint="attempts before dead-letter">
              <Input min={0} type="number" value={form.max_retries} onChange={(event) => setForm({ ...form, max_retries: Number(event.target.value) })} />
            </Field>
            <Field label="Dead-letter queue">
              <Input className="font-mono" placeholder="events-dlq" value={form.dead_letter_queue} onChange={(event) => setForm({ ...form, dead_letter_queue: event.target.value })} />
            </Field>
          </div>
          <Field label="Metadata" hint="key=value pairs">
            <Input className="font-mono" placeholder="owner=backend" value={form.metadata} onChange={(event) => setForm({ ...form, metadata: event.target.value })} />
          </Field>
          <Button className="justify-self-start" disabled={!project || createMutation.isPending || form.name.trim().length === 0} type="submit" variant="secondary">
            <Plus size={14} />
            Add queue
          </Button>
        </form>
      ) : null}
      {selectedItem && selectedItem !== "new" ? (
        <div className="mt-4 grid gap-3">
          <DatabaseDetailHeader detail={selectedQueue ? `${selectedQueue.schema} queue with ${selectedQueue.max_retries} retries.` : "Queue not found in the current project."} title={selectedName} onBack={() => basePath && void navigate({ to: basePath })} />
          {selectedQueue ? (
            <div className="grid gap-2">
              <div className="metric-grid">
                <div className="metric-cell"><p className="label">Status</p><p className="text-sm font-medium">{selectedQueue.status}</p></div>
                <div className="metric-cell"><p className="label">Retention</p><p className="text-sm font-medium">{selectedQueue.retention_minutes}m</p></div>
                <div className="metric-cell"><p className="label">Visibility</p><p className="text-sm font-medium">{selectedQueue.visibility_timeout_seconds}s</p></div>
                <div className="metric-cell"><p className="label">Updated</p><p className="text-sm font-medium">{formatTime(selectedQueue.updated_at)}</p></div>
              </div>
              <div className="metric-cell">
                <p className="label">Dead-letter queue</p>
                <p className="mt-1 font-mono text-sm text-muted">{selectedQueue.dead_letter_queue || "none"}</p>
              </div>
              <Button className="w-fit" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, name: selectedQueue.name })} type="button" variant="danger">
                <X size={14} />
                Delete queue
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="mt-4 grid gap-2">
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function DatabaseWebhooksPanel({ project, webhooks, loading }: { project?: Project; webhooks: ProjectDatabaseWebhook[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedItem = pathname.match(/^\/projects\/[^/]+\/database\/webhooks\/([^/]+)/)?.[1];
  const selectedName = selectedItem ? decodeURIComponent(selectedItem) : "";
  const selectedWebhook = selectedName && selectedName !== "new" ? webhooks.find((webhook) => webhook.name === selectedName) : undefined;
  const basePath = project ? projectPath(project.ref, "database", "webhooks") : "";
  const [form, setForm] = useState({
    name: "",
    schema: "public",
    table: "",
    events: "",
    endpoint: "",
    http_method: "POST",
    headers: "",
    timeout_seconds: 10,
    retry_count: 3,
    active: true,
    metadata: "",
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["database-webhooks", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectDatabaseWebhook(ref, {
      name: form.name,
      schema: form.schema,
      table: form.table,
      events: parseListInput(form.events),
      endpoint: form.endpoint,
      http_method: form.http_method,
      headers: parseKeyValueLines(form.headers),
      timeout_seconds: form.timeout_seconds,
      retry_count: form.retry_count,
      active: form.active,
      metadata: parseKeyValueLines(form.metadata),
    }),
    onSuccess: (_, variables) => {
      invalidate(variables.ref);
      void navigate({ to: projectPath(variables.ref, "database", "webhooks") });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, name }: { ref: string; name: string }) => deleteProjectDatabaseWebhook(ref, name),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!project || form.name.trim().length === 0 || form.table.trim().length === 0 || form.endpoint.trim().length === 0) {
      return;
    }
    createMutation.mutate({ ref: project.ref });
  }

  return (
    <AppPanel eyebrow="Database" title="Webhooks" actions={<RadioTower size={15} className="text-faint" />}>
      {!selectedItem ? (
        <div className="mt-4 grid gap-3">
          <Button className="w-fit" disabled={!project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button" variant="secondary">
            <Plus size={14} />
            Add webhook
          </Button>
          <div className="grid gap-2">
            {loading ? <p className="text-sm text-muted">Loading webhooks...</p> : null}
            {!loading && webhooks.length === 0 ? (
              <EmptyState
                icon={RadioTower}
                title="No database webhooks"
                description="Send row-change events to an HTTP endpoint."
                action={
                  <Button disabled={!project} onClick={() => basePath && void navigate({ to: `${basePath}/new` })} type="button" variant="secondary">
                    <Plus size={14} />
                    Add webhook
                  </Button>
                }
              />
            ) : null}
            {webhooks.map((webhook) => (
              <ResourceRow
                key={webhook.id}
                title={webhook.name}
                meta={`${webhook.schema}.${webhook.table} · ${webhook.http_method}`}
                chips={
                  <>
                    {webhook.events.map((event) => <NeutralChip key={event}>{event}</NeutralChip>)}
                    <NeutralChip>{webhook.active ? "active" : "paused"}</NeutralChip>
                  </>
                }
                status={webhook.status}
                onDelete={() => project && deleteMutation.mutate({ ref: project.ref, name: webhook.name })}
                deleting={!project || deleteMutation.isPending}
              >
                <DetailBlock label="Endpoint" value={`${webhook.http_method} ${webhook.endpoint}`} mono />
                {Object.keys(webhook.headers).length > 0 ? (
                  <DetailBlock label="Headers" value={Object.entries(webhook.headers).map(([key, value]) => `${key}: ${value}`).join("\n")} mono />
                ) : null}
                <DetailBlock label="Delivery" value={`timeout ${webhook.timeout_seconds}s · ${webhook.retry_count} retries`} />
              </ResourceRow>
            ))}
          </div>
        </div>
      ) : null}
      {selectedItem === "new" ? (
        <form className="mt-4 grid gap-2" onSubmit={onSubmit}>
          <DatabaseDetailHeader detail="Create one database change webhook declaration for this project." title="New webhook" onBack={() => basePath && void navigate({ to: basePath })} />
          <div className="grid grid-cols-[minmax(0,1fr)_120px_160px_auto] gap-2 max-lg:grid-cols-2 max-sm:grid-cols-1">
            <Field label="Webhook name" required>
              <Input className="font-mono" placeholder="orders-events" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            </Field>
            <Field label="Schema">
              <Input className="font-mono" placeholder="public" value={form.schema} onChange={(event) => setForm({ ...form, schema: event.target.value })} />
            </Field>
            <Field label="Table" required>
              <Input className="font-mono" placeholder="orders" value={form.table} onChange={(event) => setForm({ ...form, table: event.target.value })} />
            </Field>
            <label className="flex items-center gap-2 text-sm self-end">
              <Switch checked={form.active} onCheckedChange={(next) => setForm({ ...form, active: next })} aria-label="Active" />
              Active
            </label>
          </div>
          <div className="grid grid-cols-[minmax(0,1fr)_120px_120px_120px] gap-2 max-lg:grid-cols-2 max-sm:grid-cols-1">
            <Field label="Endpoint" required>
              <Input className="font-mono" placeholder="https://hooks.example.com/orders" value={form.endpoint} onChange={(event) => setForm({ ...form, endpoint: event.target.value })} />
            </Field>
            <Field label="HTTP method">
              <Input className="font-mono" placeholder="POST" value={form.http_method} onChange={(event) => setForm({ ...form, http_method: event.target.value })} />
            </Field>
            <Field label="Timeout" hint="seconds">
              <Input min={1} type="number" value={form.timeout_seconds} onChange={(event) => setForm({ ...form, timeout_seconds: Number(event.target.value) })} />
            </Field>
            <Field label="Retries" hint="attempts">
              <Input min={0} type="number" value={form.retry_count} onChange={(event) => setForm({ ...form, retry_count: Number(event.target.value) })} />
            </Field>
          </div>
          <Field label="Events" hint="comma separated: insert, update, delete">
            <Input className="font-mono" placeholder="insert,update,delete" value={form.events} onChange={(event) => setForm({ ...form, events: event.target.value })} />
          </Field>
          <Field label="Headers" hint="key=value per line">
            <Input className="font-mono" placeholder="Authorization=secret://projects/ref/webhooks/token" value={form.headers} onChange={(event) => setForm({ ...form, headers: event.target.value })} />
          </Field>
          <Field label="Metadata" hint="key=value pairs">
            <Input className="font-mono" placeholder="owner=backend" value={form.metadata} onChange={(event) => setForm({ ...form, metadata: event.target.value })} />
          </Field>
          <Button className="justify-self-start" disabled={!project || createMutation.isPending || form.name.trim().length === 0 || form.table.trim().length === 0 || form.endpoint.trim().length === 0} type="submit" variant="secondary">
            <Plus size={14} />
            Add webhook
          </Button>
        </form>
      ) : null}
      {selectedItem && selectedItem !== "new" ? (
        <div className="mt-4 grid gap-3">
          <DatabaseDetailHeader detail={selectedWebhook ? `${selectedWebhook.schema}.${selectedWebhook.table} sends ${selectedWebhook.events.join(", ")} events.` : "Webhook not found in the current project."} title={selectedName} onBack={() => basePath && void navigate({ to: basePath })} />
          {selectedWebhook ? (
            <div className="grid gap-2">
              <div className="metric-cell">
                <p className="label">Endpoint</p>
                <p className="mt-1 truncate font-mono text-sm text-muted">{selectedWebhook.endpoint}</p>
              </div>
              <div className="metric-grid">
                <div className="metric-cell"><p className="label">Status</p><p className="text-sm font-medium">{selectedWebhook.status}</p></div>
                <div className="metric-cell"><p className="label">Method</p><p className="text-sm font-medium">{selectedWebhook.http_method}</p></div>
                <div className="metric-cell"><p className="label">Timeout</p><p className="text-sm font-medium">{selectedWebhook.timeout_seconds}s</p></div>
                <div className="metric-cell"><p className="label">Retries</p><p className="text-sm font-medium">{selectedWebhook.retry_count}</p></div>
              </div>
              <Button className="w-fit" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, name: selectedWebhook.name })} type="button" variant="danger">
                <X size={14} />
                Delete webhook
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="mt-4 grid gap-2">
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function DatabaseSchemasPanel({ project, schemas, loading }: { project?: Project; schemas: ProjectDatabaseSchema[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    name: "",
    version: "",
    schema: "public",
    sql: "",
    apply_order: 10,
    active: true,
    metadata: "",
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["database-schemas", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectDatabaseSchema(ref, {
      name: form.name,
      version: form.version,
      schema: form.schema,
      sql: form.sql,
      apply_order: form.apply_order,
      active: form.active,
      metadata: parseKeyValueLines(form.metadata),
    }),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, name, version }: { ref: string; name: string; version: string }) => deleteProjectDatabaseSchema(ref, name, version),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!project || form.name.trim().length === 0 || form.version.trim().length === 0 || form.sql.trim().length === 0) {
      return;
    }
    createMutation.mutate({ ref: project.ref });
  }

  return (
    <AppPanel eyebrow="Database" title="Declarative schemas" actions={<Database size={15} className="text-faint" />}>
      <form className="mt-4 grid gap-2" onSubmit={onSubmit}>
        <div className="grid grid-cols-[minmax(0,1fr)_160px_120px_100px_auto] gap-2 max-lg:grid-cols-2 max-sm:grid-cols-1">
          <Field label="Migration name" required>
            <Input className="font-mono" placeholder="app-schema" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
          </Field>
          <Field label="Version" required>
            <Input className="font-mono" placeholder="20260605_001" value={form.version} onChange={(event) => setForm({ ...form, version: event.target.value })} />
          </Field>
          <Field label="Schema">
            <Input className="font-mono" placeholder="public" value={form.schema} onChange={(event) => setForm({ ...form, schema: event.target.value })} />
          </Field>
          <Field label="Apply order" hint="ascending">
            <Input min={0} type="number" value={form.apply_order} onChange={(event) => setForm({ ...form, apply_order: Number(event.target.value) })} />
          </Field>
          <label className="flex items-center gap-2 text-sm self-end">
            <Switch checked={form.active} onCheckedChange={(next) => setForm({ ...form, active: next })} aria-label="Active" />
            Active
          </label>
        </div>
        <Field label="SQL" required hint="declarative migration body">
          <Textarea className="min-h-[108px] font-mono" placeholder="create table public.accounts(id uuid primary key);" value={form.sql} onChange={(event) => setForm({ ...form, sql: event.target.value })} />
        </Field>
        <Field label="Metadata" hint="key=value pairs">
          <Input className="font-mono" placeholder="owner=backend" value={form.metadata} onChange={(event) => setForm({ ...form, metadata: event.target.value })} />
        </Field>
        <Button className="justify-self-start" disabled={!project || createMutation.isPending || form.name.trim().length === 0 || form.version.trim().length === 0 || form.sql.trim().length === 0} type="submit" variant="secondary">
          <Plus size={14} />
          Add schema migration
        </Button>
      </form>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading schema migrations...</p> : null}
        {!loading && schemas.length === 0 ? (
          <EmptyState icon={Database} title="No declarative schemas" description="Record a versioned migration using the form above." />
        ) : null}
        {schemas.map((schema) => (
          <ResourceRow
            key={schema.id}
            title={`${schema.name}@${schema.version}`}
            meta={`${schema.schema} · order ${schema.apply_order} · ${shortChecksum(schema.checksum)}`}
            chips={<NeutralChip>{schema.active ? "active" : "paused"}</NeutralChip>}
            status={schema.status}
            onDelete={() => project && deleteMutation.mutate({ ref: project.ref, name: schema.name, version: schema.version })}
            deleting={!project || deleteMutation.isPending}
          >
            <DetailBlock label="SQL" value={schema.sql} mono />
          </ResourceRow>
        ))}
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function DatabaseRolesPanel({ project, roles, loading }: { project?: Project; roles: ProjectDatabaseRole[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    name: "",
    login: true,
    inherit: true,
    bypass_rls: false,
    connection_limit: "",
    password_secret_handle: "",
    member_of: "",
    schema_grants: "",
    metadata: "",
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["database-roles", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectDatabaseRole(ref, {
      name: form.name,
      login: form.login,
      inherit: form.inherit,
      bypass_rls: form.bypass_rls,
      connection_limit: Number(form.connection_limit) || 0,
      password_secret_handle: form.password_secret_handle,
      member_of: parseLines(form.member_of),
      schema_grants: parseGrantLines(form.schema_grants),
      metadata: parseKeyValueLines(form.metadata),
    }),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, name }: { ref: string; name: string }) => deleteProjectDatabaseRole(ref, name),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!project || form.name.trim().length === 0) {
      return;
    }
    createMutation.mutate({ ref: project.ref });
  }
  return (
    <AppPanel eyebrow="Database" title="Roles and grants" actions={<Database size={15} className="text-faint" />}>
      <form className="mt-4 grid gap-2" onSubmit={onSubmit}>
        <div className="grid grid-cols-[minmax(0,1fr)_120px_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
          <Field label="Role name" required>
            <Input className="font-mono" placeholder="app_writer" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
          </Field>
          <Field label="Connection limit" hint="-1 = unlimited">
            <Input className="font-mono" inputMode="numeric" placeholder="25" value={form.connection_limit} onChange={(event) => setForm({ ...form, connection_limit: event.target.value })} />
          </Field>
          <Field label="Password secret handle">
            <Input className="font-mono" placeholder="secret://projects/ref/db/app-writer" value={form.password_secret_handle} onChange={(event) => setForm({ ...form, password_secret_handle: event.target.value })} />
          </Field>
        </div>
        <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
          <Field label="Member of" hint="comma separated roles">
            <Input className="font-mono" placeholder="authenticated" value={form.member_of} onChange={(event) => setForm({ ...form, member_of: event.target.value })} />
          </Field>
          <Field label="Schema grants" hint="schema=usage,select per line">
            <Input className="font-mono" placeholder="public=usage,select" value={form.schema_grants} onChange={(event) => setForm({ ...form, schema_grants: event.target.value })} />
          </Field>
        </div>
        <Field label="Metadata" hint="key=value pairs">
          <Input className="font-mono" placeholder="purpose=application-writes" value={form.metadata} onChange={(event) => setForm({ ...form, metadata: event.target.value })} />
        </Field>
        <div className="grid grid-cols-3 gap-2 max-sm:grid-cols-1">
          <label className="checkbox-row">
            <input type="checkbox" checked={form.login} onChange={(event) => setForm({ ...form, login: event.target.checked })} />
            Login
          </label>
          <label className="checkbox-row">
            <input type="checkbox" checked={form.inherit} onChange={(event) => setForm({ ...form, inherit: event.target.checked })} />
            Inherit
          </label>
          <label className="checkbox-row">
            <input type="checkbox" checked={form.bypass_rls} onChange={(event) => setForm({ ...form, bypass_rls: event.target.checked })} />
            Bypass RLS
          </label>
        </div>
        <Button className="justify-self-start" disabled={!project || createMutation.isPending || form.name.trim().length === 0} type="submit" variant="secondary">
          <Plus size={14} />
          Add database role
        </Button>
      </form>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading database roles...</p> : null}
        {!loading && roles.length === 0 ? (
          <EmptyState icon={Database} title="No database roles" description="Create a Postgres role with schema grants using the form above." />
        ) : null}
        {roles.map((role) => {
          const grants = Object.entries(role.schema_grants);
          return (
            <ResourceRow
              key={role.id}
              title={role.name}
              meta={`${role.login ? "login" : "group"} · ${role.inherit ? "inherit" : "noinherit"} · limit ${role.connection_limit}`}
              chips={role.bypass_rls ? <NeutralChip>bypass RLS</NeutralChip> : undefined}
              status={role.status}
              onDelete={() => project && deleteMutation.mutate({ ref: project.ref, name: role.name })}
              deleting={!project || deleteMutation.isPending}
            >
              <DetailBlock
                label="Schema grants"
                value={grants.length > 0 ? grants.map(([schema, list]) => `${schema}: ${list}`).join("\n") : "no schema grants"}
                mono
              />
              {role.member_of.length > 0 ? <DetailBlock label="Member of" value={role.member_of.join(", ")} mono /> : null}
            </ResourceRow>
          );
        })}
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

export function VectorAIPanel({ project, jobs, buckets, loading }: { project?: Project; jobs: ProjectEmbeddingJob[]; buckets: ProjectVectorBucket[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [jobForm, setJobForm] = useState({
    name: "",
    source_schema: "public",
    source_table: "",
    source_column: "",
    primary_key_column: "id",
    destination_table: "",
    destination_column: "",
    provider: "openai",
    model: "",
    dimension: "1536",
    schedule: "manual",
    batch_size: "100",
  });
  const [bucketForm, setBucketForm] = useState({
    name: "",
    dimension: "1536",
    distance: "cosine",
    index_method: "hnsw",
    storage_backend: "postgres",
    storage_uri: "",
    metadata: "",
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["embedding-jobs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["vector-buckets", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createJobMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectEmbeddingJob(ref, {
      name: jobForm.name,
      source_schema: jobForm.source_schema,
      source_table: jobForm.source_table,
      source_column: jobForm.source_column,
      primary_key_column: jobForm.primary_key_column,
      destination_table: jobForm.destination_table,
      destination_column: jobForm.destination_column,
      provider: jobForm.provider,
      model: jobForm.model,
      dimension: Number(jobForm.dimension) || 0,
      schedule: jobForm.schedule,
      batch_size: Number(jobForm.batch_size) || 0,
    }),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteJobMutation = useMutation({
    mutationFn: ({ ref, id }: { ref: string; id: string }) => deleteProjectEmbeddingJob(ref, id),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const createBucketMutation = useMutation({
    mutationFn: ({ ref }: { ref: string }) => createProjectVectorBucket(ref, {
      name: bucketForm.name,
      dimension: Number(bucketForm.dimension) || 0,
      distance: bucketForm.distance,
      index_method: bucketForm.index_method,
      storage_backend: bucketForm.storage_backend,
      storage_uri: bucketForm.storage_uri,
      metadata: parseKeyValueLines(bucketForm.metadata),
    }),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteBucketMutation = useMutation({
    mutationFn: ({ ref, name }: { ref: string; name: string }) => deleteProjectVectorBucket(ref, name),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function submitJob(event: FormEvent) {
    event.preventDefault();
    if (!project || jobForm.source_table.trim().length === 0 || jobForm.source_column.trim().length === 0) {
      return;
    }
    createJobMutation.mutate({ ref: project.ref });
  }

  function submitBucket(event: FormEvent) {
    event.preventDefault();
    if (!project || bucketForm.name.trim().length === 0) {
      return;
    }
    createBucketMutation.mutate({ ref: project.ref });
  }

  return (
    <AppPanel eyebrow="Vector / AI" title="Embeddings and buckets" actions={<Boxes size={15} className="text-faint" />}>
      <div className="mt-4 grid gap-4">
        <form className="grid gap-2" onSubmit={submitJob}>
          <SubSection title="Embedding job" description="Embed a source column into a destination vector column.">
            <div className="grid grid-cols-[minmax(0,1fr)_130px_170px] gap-2 max-sm:grid-cols-1">
              <Field label="Job name">
                <Input className="font-mono" placeholder="docs-embeddings" value={jobForm.name} onChange={(event) => setJobForm({ ...jobForm, name: event.target.value })} />
              </Field>
              <Field label="Provider">
                <NativeSelect value={jobForm.provider} onChange={(event) => setJobForm({ ...jobForm, provider: event.target.value })}>
                  <option value="openai">OpenAI</option>
                  <option value="huggingface">Hugging Face</option>
                  <option value="local">Local</option>
                </NativeSelect>
              </Field>
              <Field label="Model">
                <Input className="font-mono" placeholder="text-embedding-3-small" value={jobForm.model} onChange={(event) => setJobForm({ ...jobForm, model: event.target.value })} />
              </Field>
            </div>
            <div className="grid grid-cols-[110px_minmax(0,1fr)_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
              <Field label="Source schema">
                <Input className="font-mono" placeholder="public" value={jobForm.source_schema} onChange={(event) => setJobForm({ ...jobForm, source_schema: event.target.value })} />
              </Field>
              <Field label="Source table" required>
                <Input className="font-mono" placeholder="documents" value={jobForm.source_table} onChange={(event) => setJobForm({ ...jobForm, source_table: event.target.value })} />
              </Field>
              <Field label="Source column" required>
                <Input className="font-mono" placeholder="body" value={jobForm.source_column} onChange={(event) => setJobForm({ ...jobForm, source_column: event.target.value })} />
              </Field>
            </div>
            <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_100px_100px] gap-2 max-sm:grid-cols-1">
              <Field label="Destination table">
                <Input className="font-mono" placeholder="document_embeddings" value={jobForm.destination_table} onChange={(event) => setJobForm({ ...jobForm, destination_table: event.target.value })} />
              </Field>
              <Field label="Destination column">
                <Input className="font-mono" placeholder="embedding" value={jobForm.destination_column} onChange={(event) => setJobForm({ ...jobForm, destination_column: event.target.value })} />
              </Field>
              <Field label="Dimensions">
                <Input className="font-mono" inputMode="numeric" placeholder="1536" value={jobForm.dimension} onChange={(event) => setJobForm({ ...jobForm, dimension: event.target.value })} />
              </Field>
              <Field label="Batch size">
                <Input className="font-mono" inputMode="numeric" placeholder="100" value={jobForm.batch_size} onChange={(event) => setJobForm({ ...jobForm, batch_size: event.target.value })} />
              </Field>
            </div>
          </SubSection>
          <Button className="justify-self-start" disabled={!project || createJobMutation.isPending || jobForm.source_column.trim().length === 0} type="submit" variant="secondary">
            <Plus size={14} />
            Add embedding job
          </Button>
        </form>
        <form className="grid gap-2" onSubmit={submitBucket}>
          <SubSection title="Vector bucket" description="A vector index/store for similarity search.">
            <div className="grid grid-cols-[minmax(0,1fr)_100px_120px_120px_130px] gap-2 max-sm:grid-cols-1">
              <Field label="Bucket name" required>
                <Input className="font-mono" placeholder="documents" value={bucketForm.name} onChange={(event) => setBucketForm({ ...bucketForm, name: event.target.value })} />
              </Field>
              <Field label="Dimensions">
                <Input className="font-mono" inputMode="numeric" placeholder="1536" value={bucketForm.dimension} onChange={(event) => setBucketForm({ ...bucketForm, dimension: event.target.value })} />
              </Field>
              <Field label="Distance">
                <NativeSelect value={bucketForm.distance} onChange={(event) => setBucketForm({ ...bucketForm, distance: event.target.value })}>
                  <option value="cosine">Cosine</option>
                  <option value="l2">L2</option>
                  <option value="ip">Inner product</option>
                </NativeSelect>
              </Field>
              <Field label="Index method">
                <NativeSelect value={bucketForm.index_method} onChange={(event) => setBucketForm({ ...bucketForm, index_method: event.target.value })}>
                  <option value="hnsw">HNSW</option>
                  <option value="ivfflat">IVFFlat</option>
                  <option value="none">None</option>
                </NativeSelect>
              </Field>
              <Field label="Storage backend">
                <NativeSelect value={bucketForm.storage_backend} onChange={(event) => setBucketForm({ ...bucketForm, storage_backend: event.target.value })}>
                  <option value="postgres">Postgres</option>
                  <option value="s3">S3</option>
                </NativeSelect>
              </Field>
            </div>
            <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-2 max-sm:grid-cols-1">
              <Field label="Storage URI">
                <Input className="font-mono" placeholder="s3://bucket/prefix" value={bucketForm.storage_uri} onChange={(event) => setBucketForm({ ...bucketForm, storage_uri: event.target.value })} />
              </Field>
              <Field label="Metadata" hint="key=value pairs">
                <Input className="font-mono" placeholder="purpose=semantic-search" value={bucketForm.metadata} onChange={(event) => setBucketForm({ ...bucketForm, metadata: event.target.value })} />
              </Field>
            </div>
          </SubSection>
          <Button className="justify-self-start" disabled={!project || createBucketMutation.isPending || bucketForm.name.trim().length === 0} type="submit" variant="secondary">
            <Plus size={14} />
            Add vector bucket
          </Button>
        </form>
      </div>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading Vector and AI resources...</p> : null}
        {!loading && jobs.length === 0 && buckets.length === 0 ? (
          <EmptyState icon={Boxes} title="No embedding jobs or vector buckets" description="Create an embedding job or a vector bucket using the forms above." />
        ) : null}
        {jobs.map((job) => (
          <ResourceRow
            key={job.id}
            title={job.name}
            meta={`${job.source_schema}.${job.source_table}.${job.source_column} → ${job.destination_table}.${job.destination_column}`}
            chips={<><NeutralChip>{job.provider}</NeutralChip><NeutralChip>{job.schedule}</NeutralChip></>}
            status={job.status}
            onDelete={() => project && deleteJobMutation.mutate({ ref: project.ref, id: job.id })}
            deleting={!project || deleteJobMutation.isPending}
          >
            <DetailBlock label="Model" value={`${job.model || "—"} · ${job.dimension}d · batches of ${job.batch_size}`} mono />
          </ResourceRow>
        ))}
        {buckets.map((bucket) => (
          <ResourceRow
            key={bucket.id}
            title={bucket.name}
            meta={`${bucket.dimension}d · ${bucket.distance} · ${bucket.index_method}`}
            chips={<NeutralChip>{bucket.storage_backend}</NeutralChip>}
            status={bucket.status}
            onDelete={() => project && deleteBucketMutation.mutate({ ref: project.ref, name: bucket.name })}
            deleting={!project || deleteBucketMutation.isPending}
          >
            <DetailBlock label="Storage" value={bucket.storage_uri || `${bucket.storage_backend} (in-database)`} mono />
            {bucket.metadata.purpose ? <DetailBlock label="Purpose" value={bucket.metadata.purpose} /> : null}
          </ResourceRow>
        ))}
        {createJobMutation.error ? <p className="text-sm text-danger">{createJobMutation.error.message}</p> : null}
        {deleteJobMutation.error ? <p className="text-sm text-danger">{deleteJobMutation.error.message}</p> : null}
        {createBucketMutation.error ? <p className="text-sm text-danger">{createBucketMutation.error.message}</p> : null}
        {deleteBucketMutation.error ? <p className="text-sm text-danger">{deleteBucketMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

function parseListInput(value: string) {
  return value.split(/\n|,/).map((item) => item.trim()).filter(Boolean);
}

function parseGrantLines(value: string) {
  const out: Record<string, string> = {};
  for (const line of value.split(/\n/)) {
    const [rawKey, ...rest] = line.split("=");
    const key = rawKey.trim();
    const nextValue = rest.join("=").trim();
    if (key && nextValue) {
      out[key] = nextValue;
    }
  }
  return out;
}
