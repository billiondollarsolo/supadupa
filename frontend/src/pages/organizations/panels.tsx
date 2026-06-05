import type { FormEvent } from "react";
import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Save, UserPlus, X } from "lucide-react";
import {
  createBillingInvoice,
  createOrg,
  createOrgTeam,
  createOrgUsageSnapshot,
  createPlatformUser,
  deleteOrgMember,
  deleteOrgTeam,
  deleteTeamMember,
  updateOrgFeatureFlags,
  updateOrgQuota,
  upsertOrgMember,
  upsertTeamMember,
} from "../../api";
import { featureFlagGroups } from "../../lib/feature-flags";
import { formatBytes, formatMoney, formatTime } from "../../lib/format";
import type { BillingInvoice, Membership, OrgFeatureFlags, OrgQuota, OrgUsage, Team, TeamMember, UsageSnapshot, User } from "../../types";

export function OrgPanel({
  orgs,
  selectedOrgId,
  onSelectOrg,
  onCreated,
}: {
  orgs: { id: string; name: string }[];
  selectedOrgId: string;
  onSelectOrg: (id: string) => void;
  onCreated: (id: string) => void;
}) {
  const [name, setName] = useState("Platform");
  const mutation = useMutation({
    mutationFn: createOrg,
    onSuccess: (org) => onCreated(org.id),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    mutation.mutate(name);
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Organizations</p>
          <h2>Org scope</h2>
        </div>
      </div>
      <div className="mt-4 flex gap-2 max-sm:flex-col">
        <select className="input" value={selectedOrgId} onChange={(event) => onSelectOrg(event.target.value)}>
          {orgs.length === 0 ? <option value="">No orgs yet</option> : null}
          {orgs.map((org) => (
            <option key={org.id} value={org.id}>
              {org.name}
            </option>
          ))}
        </select>
        <form className="flex flex-1 gap-2" onSubmit={submit}>
          <input className="input" value={name} onChange={(event) => setName(event.target.value)} />
          <button className="button" type="submit">
            <Plus size={14} />
            Org
          </button>
        </form>
      </div>
    </section>
  );
}

export function MembersPanel({ orgId, members, users, loading }: { orgId: string; members: Membership[]; users: User[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("developer");
  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["org-members", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["users"] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const upsertMutation = useMutation({
    mutationFn: async ({ nextEmail, nextPassword, nextRole }: { nextEmail: string; nextPassword: string; nextRole: string }) => {
      if (nextPassword.trim().length > 0) {
        try {
          await createPlatformUser({ email: nextEmail, password: nextPassword, role: "admin" });
        } catch (error) {
          if (!(error instanceof Error) || !error.message.includes("conflict")) {
            throw error;
          }
        }
      }
      return upsertOrgMember(orgId, { email: nextEmail, role: nextRole });
    },
    onSuccess: () => {
      setEmail("");
      setPassword("");
      invalidate();
    },
  });
  const deleteMutation = useMutation({
    mutationFn: (memberEmail: string) => deleteOrgMember(orgId, memberEmail),
    onSuccess: invalidate,
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!orgId || email.trim().length === 0) {
      return;
    }
    upsertMutation.mutate({ nextEmail: email, nextPassword: password, nextRole: role });
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Members</p>
          <h2>Global org access</h2>
        </div>
        <UserPlus size={15} className="text-faint" />
      </div>
      <form className="mt-4 grid grid-cols-[minmax(0,1fr)_minmax(140px,0.6fr)_120px_auto] gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1" onSubmit={submit}>
        <input className="input" placeholder="teammate@example.com" value={email} onChange={(event) => setEmail(event.target.value)} type="email" />
        <input className="input" placeholder="Initial password" value={password} onChange={(event) => setPassword(event.target.value)} type="password" />
        <select className="input" value={role} onChange={(event) => setRole(event.target.value)}>
          <option value="owner">Owner</option>
          <option value="admin">Admin</option>
          <option value="developer">Developer</option>
          <option value="viewer">Viewer</option>
        </select>
        <button className="button secondary justify-center max-xl:col-span-2 max-sm:col-span-1" disabled={!orgId || upsertMutation.isPending || email.trim().length === 0} type="submit">
          <Plus size={14} />
          Add
        </button>
      </form>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading members...</p> : null}
        {!loading && members.length === 0 ? <p className="text-sm text-muted">No members assigned yet.</p> : null}
        {members.map((member) => (
          <div className="member-row" key={`${member.org_id}:${member.email}`}>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{member.email}</p>
              <p className="truncate font-mono text-xs text-muted">{member.user_id}</p>
            </div>
            <div className="flex items-center gap-2">
              <span className="pill healthy">{member.role}</span>
              <span className="pill">all org projects</span>
              <button className="icon-button" disabled={!orgId || member.role === "owner" || deleteMutation.isPending} onClick={() => deleteMutation.mutate(member.email)} type="button">
                <X size={14} />
              </button>
            </div>
          </div>
        ))}
        {upsertMutation.error ? <p className="text-sm text-danger">{upsertMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
      <div className="mt-5 border-t border-border pt-4">
        <div className="mb-3 flex items-center justify-between gap-3">
          <p className="label">Platform users</p>
          <span className="pill">{users.length}</span>
        </div>
        <div className="grid gap-2">
          {loading ? <p className="text-sm text-muted">Loading users...</p> : null}
          {!loading && users.length === 0 ? <p className="text-sm text-muted">No platform users created yet.</p> : null}
          {users.map((user) => (
            <div className="member-row" key={user.id}>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{user.email}</p>
                <p className="truncate font-mono text-xs text-muted">{user.id}</p>
              </div>
              <div className="flex items-center gap-2">
                {user.mfa_enabled ? <span className="pill healthy">MFA</span> : <span className="pill">No MFA</span>}
                <span className={`pill ${user.role === "admin" ? "healthy" : ""}`}>{user.role === "admin" ? "global admin" : user.role}</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

export function TeamsPanel({ orgId, teams, selectedSlug, members, onSelect, loading }: { orgId: string; teams: Team[]; selectedSlug: string; members: TeamMember[]; onSelect: (slug: string) => void; loading: boolean }) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("Developers");
  const [slug, setSlug] = useState("developers");
  const [email, setEmail] = useState("");
  const invalidate = (teamSlug = selectedSlug) => {
    void queryClient.invalidateQueries({ queryKey: ["org-teams", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["team-members", orgId, teamSlug] });
    void queryClient.invalidateQueries({ queryKey: ["org-members", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["users"] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: () => createOrgTeam(orgId, { name, slug }),
    onSuccess: (team) => {
      onSelect(team.slug);
      setName("");
      setSlug("");
      invalidate(team.slug);
    },
  });
  const deleteMutation = useMutation({
    mutationFn: (teamSlug: string) => deleteOrgTeam(orgId, teamSlug),
    onSuccess: (_, teamSlug) => {
      if (selectedSlug === teamSlug) onSelect("");
      invalidate(teamSlug);
    },
  });
  const addMemberMutation = useMutation({
    mutationFn: () => upsertTeamMember(orgId, selectedSlug, email),
    onSuccess: () => {
      setEmail("");
      invalidate();
    },
  });
  const deleteMemberMutation = useMutation({
    mutationFn: (memberEmail: string) => deleteTeamMember(orgId, selectedSlug, memberEmail),
    onSuccess: () => invalidate(),
  });

  function createTeam(event: FormEvent) {
    event.preventDefault();
    if (!orgId || !name.trim()) return;
    createMutation.mutate();
  }

  function addMember(event: FormEvent) {
    event.preventDefault();
    if (!orgId || !selectedSlug || !email.trim()) return;
    addMemberMutation.mutate();
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Teams</p>
          <h2>Project RBAC</h2>
        </div>
        <UserPlus size={15} className="text-faint" />
      </div>
      <form className="mt-4 grid grid-cols-[minmax(0,1fr)_150px_auto] gap-2 max-sm:grid-cols-1" onSubmit={createTeam}>
        <input className="input" placeholder="Team name" value={name} onChange={(event) => setName(event.target.value)} />
        <input className="input font-mono" placeholder="slug" value={slug} onChange={(event) => setSlug(event.target.value)} />
        <button className="button secondary justify-center" disabled={!orgId || createMutation.isPending || !name.trim()} type="submit">
          <Plus size={14} />
          Team
        </button>
      </form>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading teams...</p> : null}
        {!loading && teams.length === 0 ? <p className="text-sm text-muted">No teams configured.</p> : null}
        {teams.map((team) => (
          <div className={team.slug === selectedSlug ? "member-row bg-surface-2" : "member-row"} key={team.id}>
            <button className="min-w-0 text-left" onClick={() => onSelect(team.slug)} type="button">
              <p className="truncate text-sm font-medium">{team.name}</p>
              <p className="truncate font-mono text-xs text-muted">{team.slug}</p>
            </button>
            <button className="icon-button" disabled={deleteMutation.isPending} onClick={() => deleteMutation.mutate(team.slug)} type="button">
              <X size={14} />
            </button>
          </div>
        ))}
      </div>
      {selectedSlug ? (
        <div className="mt-5 border-t border-border pt-4">
          <form className="flex gap-2 max-sm:flex-col" onSubmit={addMember}>
            <input className="input" placeholder="member@example.com" value={email} onChange={(event) => setEmail(event.target.value)} type="email" />
            <button className="button secondary justify-center" disabled={addMemberMutation.isPending || !email.trim()} type="submit">
              <Plus size={14} />
              Add
            </button>
          </form>
          <div className="mt-3 grid gap-2">
            {members.map((member) => (
              <div className="member-row" key={`${member.team_id}:${member.email}`}>
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{member.email}</p>
                  <p className="truncate font-mono text-xs text-muted">{member.user_id}</p>
                </div>
                <button className="icon-button" disabled={deleteMemberMutation.isPending} onClick={() => deleteMemberMutation.mutate(member.email)} type="button">
                  <X size={14} />
                </button>
              </div>
            ))}
          </div>
        </div>
      ) : null}
      {createMutation.error ? <p className="mt-3 text-sm text-danger">{createMutation.error.message}</p> : null}
      {addMemberMutation.error ? <p className="mt-3 text-sm text-danger">{addMemberMutation.error.message}</p> : null}
    </section>
  );
}

export function OrgFeaturesPanel({ orgId, features, loading }: { orgId: string; features?: OrgFeatureFlags; loading: boolean }) {
  const queryClient = useQueryClient();
  const [overrides, setOverrides] = useState<Record<string, boolean>>({});
  const featureKey = `${orgId}:${Object.entries(features?.overrides ?? {}).sort(([left], [right]) => left.localeCompare(right)).map(([key, value]) => `${key}:${value}`).join("|")}`;
  useEffect(() => {
    setOverrides(features?.overrides ?? {});
  }, [featureKey, features]);
  const mutation = useMutation({
    mutationFn: () => updateOrgFeatureFlags(orgId, overrides),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["org-features", orgId] });
      void queryClient.invalidateQueries({ queryKey: ["org-usage-snapshots", orgId] });
      void queryClient.invalidateQueries({ queryKey: ["billing-invoices", orgId] });
      void queryClient.invalidateQueries({ queryKey: ["orgs"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const enabledCount = Object.values(features?.effective ?? {}).filter(Boolean).length;
  const overrideCount = Object.keys(overrides).length;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!orgId) {
      return;
    }
    mutation.mutate();
  }

  function setOverride(key: string, value: boolean) {
    setOverrides({ ...overrides, [key]: value });
  }

  function inherit(key: string) {
    const next = { ...overrides };
    delete next[key];
    setOverrides(next);
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Features</p>
          <h2>Org rollout policy</h2>
          <p className="mt-1 text-sm text-muted">Platform defaults define deployment capability; org overrides control tenant rollout for local, Compose, and enterprise modes.</p>
        </div>
        <span className="pill">{enabledCount} enabled</span>
      </div>
      {!orgId ? (
        <div className="mt-4 rounded-md border border-border bg-bg p-3">
          <p className="text-sm font-medium">No org selected</p>
          <p className="mt-1 text-sm text-muted">Create or select an org before setting tenant rollout overrides.</p>
        </div>
      ) : null}
      {orgId ? (
      <form className="mt-4 grid gap-3" onSubmit={submit}>
        <div className="usage-row">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Override posture</p>
            <p className="truncate text-xs text-muted">{overrideCount === 0 ? "All flags inherit platform defaults." : `${overrideCount} explicit org overrides.`}</p>
          </div>
          <button className="button secondary" disabled={!orgId || loading || mutation.isPending} type="submit">
            <Save size={14} />
            Save features
          </button>
        </div>
        <div className="grid grid-cols-3 gap-3 max-xl:grid-cols-1">
          {featureFlagGroups.map((group) => (
            <div className="grid gap-2 rounded-md border border-border bg-bg p-3" key={group.label}>
              <p className="label">{group.label}</p>
              {group.flags.map(([key, label]) => {
                const hasOverride = Object.prototype.hasOwnProperty.call(overrides, key);
                const defaultEnabled = Boolean(features?.defaults[key]);
                const effectiveEnabled = hasOverride ? Boolean(overrides[key]) : defaultEnabled;
                return (
                  <div className="rounded-md border border-border bg-surface px-3 py-2" key={key}>
                    <div className="flex items-center justify-between gap-3">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium">{label}</p>
                        <p className="truncate text-xs text-muted">{hasOverride ? "Org override" : `Inherits platform ${defaultEnabled ? "enabled" : "disabled"}`}</p>
                      </div>
                      <select className="input max-w-[132px]" value={hasOverride ? String(overrides[key]) : "inherit"} onChange={(event) => {
                        if (event.target.value === "inherit") {
                          inherit(key);
                        } else {
                          setOverride(key, event.target.value === "true");
                        }
                      }}>
                        <option value="inherit">Inherit</option>
                        <option value="true">Enabled</option>
                        <option value="false">Disabled</option>
                      </select>
                    </div>
                    <div className="mt-2 flex items-center gap-2">
                      <span className={`pill ${effectiveEnabled ? "healthy" : "paused"}`}>{effectiveEnabled ? "enabled" : "disabled"}</span>
                      <span className="pill">default {defaultEnabled ? "on" : "off"}</span>
                    </div>
                  </div>
                );
              })}
            </div>
          ))}
        </div>
        {loading ? <p className="text-sm text-muted">Loading org features...</p> : null}
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </form>
      ) : null}
    </section>
  );
}

export function QuotaPanel({ orgId, quota, loading }: { orgId: string; quota?: OrgQuota; loading: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({ max_projects: 0, max_cpu: 0, max_ram_mb: 0, max_disk_gb: 0, max_disk_iops: 0 });
  const quotaKey = `${orgId}:${quota?.updated_at ?? ""}:${quota?.max_projects ?? ""}:${quota?.max_cpu ?? ""}:${quota?.max_ram_mb ?? ""}:${quota?.max_disk_gb ?? ""}:${quota?.max_disk_iops ?? ""}`;
  useEffect(() => {
    if (quota) {
      setForm({
        max_projects: quota.max_projects,
        max_cpu: quota.max_cpu,
        max_ram_mb: quota.max_ram_mb,
        max_disk_gb: quota.max_disk_gb,
        max_disk_iops: quota.max_disk_iops,
      });
    }
  }, [quotaKey, quota]);
  const mutation = useMutation({
    mutationFn: () => updateOrgQuota(orgId, form),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["org-quota", orgId] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!orgId) {
      return;
    }
    mutation.mutate();
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Quotas</p>
          <h2>Org limits</h2>
        </div>
      </div>
      <form className="mt-4 grid grid-cols-5 gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1" onSubmit={submit}>
        <input className="input" min={0} type="number" value={form.max_projects} onChange={(event) => setForm({ ...form, max_projects: Number(event.target.value) })} />
        <input className="input" min={0} type="number" value={form.max_cpu} onChange={(event) => setForm({ ...form, max_cpu: Number(event.target.value) })} />
        <input className="input" min={0} type="number" value={form.max_ram_mb} onChange={(event) => setForm({ ...form, max_ram_mb: Number(event.target.value) })} />
        <input className="input" min={0} type="number" value={form.max_disk_gb} onChange={(event) => setForm({ ...form, max_disk_gb: Number(event.target.value) })} />
        <input className="input" min={0} type="number" value={form.max_disk_iops} onChange={(event) => setForm({ ...form, max_disk_iops: Number(event.target.value) })} />
        <button className="button secondary justify-center max-xl:col-span-2 max-sm:col-span-1" disabled={!orgId || mutation.isPending} type="submit">
          <Save size={14} />
          Save quotas
        </button>
      </form>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading quotas...</p> : null}
        {quota ? (
          <div className="quota-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Current usage</p>
              <p className="truncate text-xs text-muted">Zero limits are unlimited.</p>
            </div>
            <div className="text-right text-xs text-muted">
              <p>{quota.used.projects}/{quota.max_projects || "-"} projects</p>
              <p>{quota.used.cpu}/{quota.max_cpu || "-"} CPU</p>
              <p>{formatBytes(quota.used.ram_mb * 1024 * 1024)}/{quota.max_ram_mb ? formatBytes(quota.max_ram_mb * 1024 * 1024) : "-"} RAM</p>
              <p>{quota.used.disk_gb}/{quota.max_disk_gb || "-"} GB disk</p>
              <p>{quota.used.disk_iops}/{quota.max_disk_iops || "-"} disk IOPS</p>
            </div>
          </div>
        ) : null}
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </div>
    </section>
  );
}

export function UsagePanel({
  orgId,
  usage,
  snapshots,
  loading,
  snapshotsLoading,
  snapshotEnabled,
}: {
  orgId: string;
  usage?: OrgUsage;
  snapshots: UsageSnapshot[];
  loading: boolean;
  snapshotsLoading: boolean;
  snapshotEnabled: boolean;
}) {
  const queryClient = useQueryClient();
  const snapshotMutation = useMutation({
    mutationFn: () => createOrgUsageSnapshot(orgId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["org-usage", orgId] });
      void queryClient.invalidateQueries({ queryKey: ["org-usage-snapshots", orgId] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const statuses = usage ? Object.entries(usage.projects_by_status).sort(([left], [right]) => left.localeCompare(right)) : [];
  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Metering</p>
          <h2>Org usage</h2>
        </div>
        <div className="flex items-center gap-2">
          {usage ? <time className="text-xs text-faint">{formatTime(usage.sampled_at)}</time> : null}
          <button className="icon-button" disabled={!orgId || !snapshotEnabled || snapshotMutation.isPending} onClick={() => snapshotMutation.mutate()} title={snapshotEnabled ? "Capture usage snapshot" : "Usage metering flag is disabled"} type="button">
            <Save size={14} />
          </button>
        </div>
      </div>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading usage...</p> : null}
        {usage ? (
          <>
            <div className="metric-grid">
              <Metric label="Projects" value={usage.resources.projects.toString()} />
              <Metric label="CPU" value={usage.resources.cpu.toString()} />
              <Metric label="RAM" value={formatBytes(usage.resources.ram_mb * 1024 * 1024)} />
              <Metric label="IOPS" value={usage.resources.disk_iops.toString()} />
              <Metric label="DB alloc" value={formatBytes(usage.db_allocated_bytes)} />
            </div>
            <div className="usage-row">
              <div>
                <p className="truncate text-sm font-medium">Storage and operations</p>
                <p className="truncate text-xs text-muted">{usage.read_replicas} replicas · {usage.replication_pipelines} pipelines · {usage.analytics_buckets} analytics buckets · {usage.backup_count} backups</p>
              </div>
              <div className="text-right text-xs text-muted">
                <p>{formatBytes(usage.storage_bytes)} storage</p>
                <p>{usage.project_log_events} logs · {usage.database_cron_jobs} cron jobs · {usage.database_queues} queues · {usage.database_webhooks} webhooks · {usage.database_schemas} schemas · {usage.auth_clients} auth clients · {usage.auth_hooks} auth hooks · {usage.log_drains} drains</p>
                <p>{usage.cdn_enabled_projects} CDN · {usage.cdn_invalidations} invalidations · {usage.secrets} secrets</p>
              </div>
            </div>
            <div className="usage-row">
              <div>
                <p className="truncate text-sm font-medium">Data-plane counters</p>
                <p className="truncate text-xs text-muted">Exporter-backed metrics land here.</p>
              </div>
              <div className="text-right text-xs text-muted">
                <p>{formatBytes(usage.egress_bytes)} egress</p>
                <p>{usage.function_invocations} function calls · {usage.auth_maus} MAUs</p>
              </div>
            </div>
            {statuses.length > 0 ? (
              <div className="flex flex-wrap gap-2">
                {statuses.map(([status, count]) => (
                  <span className={`pill ${status}`} key={status}>{status} {count}</span>
                ))}
              </div>
            ) : null}
            <div className="usage-row">
              <div>
                <p className="truncate text-sm font-medium">Usage snapshots</p>
                <p className="truncate text-xs text-muted">{snapshotEnabled ? "Durable metering ledger for billing and quota reviews." : "Enable usage metering in org feature flags to capture snapshots."}</p>
              </div>
              <div className="min-w-36 text-right text-xs text-muted">
                {!snapshotEnabled ? <p>Disabled</p> : null}
                {snapshotEnabled && snapshotsLoading ? <p>Loading...</p> : null}
                {snapshotEnabled && !snapshotsLoading && snapshots.length === 0 ? <p>No captures</p> : null}
                {snapshotEnabled ? snapshots.slice(0, 3).map((snapshot) => (
                  <p key={snapshot.id}>{formatTime(snapshot.sampled_at)} · {snapshot.metrics.resources.projects} projects</p>
                )) : null}
              </div>
            </div>
          </>
        ) : null}
        {snapshotMutation.error ? <p className="text-sm text-danger">{snapshotMutation.error.message}</p> : null}
      </div>
    </section>
  );
}

export function BillingPanel({ orgId, invoices, loading, enabled }: { orgId: string; invoices: BillingInvoice[]; loading: boolean; enabled: boolean }) {
  const queryClient = useQueryClient();
  const invoiceMutation = useMutation({
    mutationFn: () => createBillingInvoice(orgId, { currency: "USD", status: "draft", due_days: 30 }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["billing-invoices", orgId] });
      void queryClient.invalidateQueries({ queryKey: ["org-usage-snapshots", orgId] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const latest = invoices[0];
  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Billing</p>
          <h2>Invoices</h2>
        </div>
        <button className="icon-button" disabled={!orgId || !enabled || invoiceMutation.isPending} onClick={() => invoiceMutation.mutate()} title={enabled ? "Generate draft invoice" : "Billing flag is disabled"} type="button">
          <Save size={14} />
        </button>
      </div>
      <div className="mt-4 grid gap-2">
        {!enabled ? (
          <div className="billing-row">
            <div>
              <p className="truncate text-sm font-medium">Billing disabled</p>
              <p className="truncate text-xs text-muted">Enable billing in org feature flags before listing or generating invoices.</p>
            </div>
          </div>
        ) : null}
        {enabled && loading ? <p className="text-sm text-muted">Loading invoices...</p> : null}
        {enabled && !loading && invoices.length === 0 ? (
          <div className="billing-row">
            <div>
              <p className="truncate text-sm font-medium">No invoices</p>
              <p className="truncate text-xs text-muted">Generate a draft from the latest metering snapshot.</p>
            </div>
          </div>
        ) : null}
        {enabled && latest ? (
          <div className="metric-grid">
            <Metric label="Latest" value={latest.number} />
            <Metric label="Status" value={latest.status} />
            <Metric label="Total" value={formatMoney(latest.total_cents, latest.currency)} />
            <Metric label="Due" value={formatTime(latest.due_at)} />
          </div>
        ) : null}
        {enabled ? invoices.map((invoice) => (
          <div className="billing-row" key={invoice.id}>
            <div>
              <p className="truncate text-sm font-medium">{invoice.number}</p>
              <p className="truncate text-xs text-muted">{formatTime(invoice.period_start)} to {formatTime(invoice.period_end)} · {invoice.line_items.length} line items</p>
            </div>
            <div className="text-right text-xs text-muted">
              <p>{formatMoney(invoice.total_cents, invoice.currency)}</p>
              <p>{invoice.status} · due {formatTime(invoice.due_at)}</p>
            </div>
          </div>
        )) : null}
        {invoiceMutation.error ? <p className="text-sm text-danger">{invoiceMutation.error.message}</p> : null}
      </div>
    </section>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="metric-cell">
      <p className="label">{label}</p>
      <p className="truncate text-sm font-medium">{value}</p>
    </div>
  );
}
