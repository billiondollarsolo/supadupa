import type { FormEvent } from "react";
import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Building2, CreditCard, Pencil, Plus, Save, Trash2, UserPlus, X } from "lucide-react";
import { DataTable } from "../../components/data-table";
import { Modal } from "../../components/modal";
import {
  createBillingInvoice,
  createOrg,
  createOrgTeam,
  createOrgUsageSnapshot,
  createPlatformUser,
  deleteOrgMember,
  deleteOrgTeam,
  deleteTeamMember,
  deleteUser,
  updateOrgFeatureFlags,
  updateOrgQuota,
  updateUser,
  upsertOrgMember,
  upsertTeamMember,
} from "../../api";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { EmptyState } from "../../components/ui/empty-state";
import { Field, SubSection } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import { featureFlagGroups } from "../../lib/feature-flags";
import { formatBytes, formatDateTime, formatMoney, formatTime } from "../../lib/format";
import type { BillingInvoice, Membership, OrgFeatureFlags, OrgQuota, OrgUsage, Team, TeamMember, UsageSnapshot, User } from "../../types";

// Single org-scope switcher reused on the overview and every subsection, so the
// "which org am I administering" control looks and behaves identically
// everywhere. Create-org lives in its own distinct affordance (CreateOrgPanel).
export function OrgSwitcher({
  orgs,
  selectedOrgId,
  onSelectOrg,
}: {
  orgs: { id: string; name: string }[];
  selectedOrgId: string;
  onSelectOrg: (id: string) => void;
}) {
  return (
    <section className="rounded-md border border-border bg-surface px-3 py-2">
      <div className="flex min-h-9 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <Building2 size={14} className="text-faint" />
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Org scope</p>
            <p className="truncate text-xs text-faint">Global org access is separate from project-level administration.</p>
          </div>
        </div>
        <NativeSelect className="max-w-[260px]" aria-label="Active organization" value={selectedOrgId} onChange={(event) => onSelectOrg(event.target.value)}>
          {orgs.length === 0 ? <option value="">No orgs yet</option> : null}
          {orgs.map((org) => (
            <option key={org.id} value={org.id}>
              {org.name}
            </option>
          ))}
        </NativeSelect>
      </div>
    </section>
  );
}

// Distinct create affordance, kept separate from the switcher so picking an org
// and creating one aren't conflated. Name starts empty with a placeholder.
export function CreateOrgPanel({ onCreated }: { onCreated: (id: string) => void }) {
  const [name, setName] = useState("");
  const mutation = useMutation({
    mutationFn: createOrg,
    onSuccess: (org) => {
      setName("");
      onCreated(org.id);
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (name.trim().length === 0) return;
    mutation.mutate(name.trim());
  }

  return (
    <AppPanel
      eyebrow="Organizations"
      title="Create organization"
      actions={<Building2 size={15} className="text-faint" />}
    >
      <form className="mt-4 flex gap-2 max-sm:flex-col" onSubmit={submit}>
        <Input className="flex-1" placeholder="Organization name" aria-label="Organization name" value={name} onChange={(event) => setName(event.target.value)} />
        <Button className="justify-self-start" disabled={name.trim().length === 0 || mutation.isPending} type="submit">
          <Plus size={14} />
          Create organization
        </Button>
      </form>
      {mutation.error ? <p className="mt-3 text-sm text-danger">{mutation.error.message}</p> : null}
    </AppPanel>
  );
}

export function MembersPanel({ orgId, members, users, loading, orgsEnabled }: { orgId: string; members: Membership[]; users: User[]; loading: boolean; orgsEnabled: boolean }) {
  const queryClient = useQueryClient();
  const [addOpen, setAddOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("developer");
  const usersByEmail = useMemo(() => new Map(users.map((user) => [user.email.toLowerCase(), user])), [users]);
  // Map each login to its role in the currently-selected org, so the platform-users
  // list can show that the same person also holds an org grant (single-org mode).
  const orgRoleByEmail = useMemo(() => new Map(members.map((member) => [member.email.toLowerCase(), member.role])), [members]);
  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["org-members", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["users"] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const upsertMutation = useMutation({
    mutationFn: async ({ nextEmail, nextPassword, nextRole }: { nextEmail: string; nextPassword: string; nextRole: string }) => {
      if (nextPassword.trim().length > 0) {
        // Create the backing platform login with the role the admin actually
        // selected — not a hardcoded admin. Conflicts surface as errors rather
        // than being silently swallowed.
        await createPlatformUser({ email: nextEmail, password: nextPassword, role: nextRole });
      }
      return upsertOrgMember(orgId, { email: nextEmail, role: nextRole });
    },
    onSuccess: () => {
      setEmail("");
      setPassword("");
      setRole("developer");
      setAddOpen(false);
      invalidate();
    },
  });
  const deleteMutation = useMutation({
    mutationFn: (memberEmail: string) => deleteOrgMember(orgId, memberEmail),
    onSuccess: invalidate,
  });

  // Platform-user edit (role + optional password reset) and delete.
  const [editUser, setEditUser] = useState<User | null>(null);
  const [editRole, setEditRole] = useState("developer");
  const [editPassword, setEditPassword] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null);
  const editMutation = useMutation({
    mutationFn: ({ id, nextEmail, nextRole, nextPassword }: { id: string; nextEmail: string; nextRole: string; nextPassword: string }) =>
      updateUser(id, { email: nextEmail, role: nextRole, password: nextPassword || undefined }),
    onSuccess: () => {
      setEditUser(null);
      setEditPassword("");
      invalidate();
    },
  });
  const deleteUserMutation = useMutation({
    mutationFn: (id: string) => deleteUser(id),
    onSuccess: () => {
      setDeleteTarget(null);
      invalidate();
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!orgId || email.trim().length === 0) {
      return;
    }
    upsertMutation.mutate({ nextEmail: email, nextPassword: password, nextRole: role });
  }

  const memberColumns = useMemo<ColumnDef<Membership>[]>(
    () => [
      {
        header: "Member",
        accessorKey: "email",
        cell: ({ row }) => (
          <>
            <p className="cell-main">{row.original.email}</p>
            <p className="cell-sub">Joined {formatDateTime(row.original.created_at)}</p>
          </>
        ),
      },
      { header: "Role", accessorKey: "role", size: 130, cell: ({ row }) => <StatusPill tone="info" label={row.original.role} /> },
      {
        header: "Login",
        id: "login",
        size: 150,
        cell: ({ row }) => {
          const backingUser = usersByEmail.get(row.original.email.toLowerCase());
          if (!backingUser) {
            return <StatusPill tone="warning" label="No login" />;
          }
          return (
            <>
              <StatusPill tone={backingUser.mfa_enabled ? "success" : "neutral"} label={backingUser.mfa_enabled ? "MFA on" : "No MFA"} />
              <p className="cell-sub mt-1">{backingUser.last_login_at ? `Seen ${formatDateTime(backingUser.last_login_at)}` : "Never signed in"}</p>
            </>
          );
        },
      },
      {
        header: "",
        id: "actions",
        size: 64,
        cell: ({ row }) => (
          <Button variant="ghost" size="icon" disabled={!orgId || row.original.role === "owner" || deleteMutation.isPending} onClick={() => deleteMutation.mutate(row.original.email)} type="button" aria-label={`Remove ${row.original.email}`}>
            <X size={14} />
          </Button>
        ),
      },
    ],
    [orgId, usersByEmail, deleteMutation],
  );

  const userColumns = useMemo<ColumnDef<User>[]>(
    () => [
      {
        header: "User",
        accessorKey: "email",
        cell: ({ row }) => {
          const orgRole = orgRoleByEmail.get(row.original.email.toLowerCase());
          return (
            <>
              <p className="cell-main flex items-center gap-2">
                <span className="truncate">{row.original.email}</span>
                {!orgsEnabled && orgRole ? <Badge variant="muted">org {orgRole}</Badge> : null}
              </p>
              <p className="cell-sub">Joined {formatDateTime(row.original.created_at)}</p>
            </>
          );
        },
      },
      {
        header: "Role",
        accessorKey: "role",
        size: 150,
        cell: ({ row }) => <StatusPill tone={row.original.role === "admin" ? "success" : "info"} label={row.original.role === "admin" ? "global admin" : row.original.role} />,
      },
      { header: "MFA", id: "mfa", size: 110, cell: ({ row }) => <StatusPill tone={row.original.mfa_enabled ? "success" : "neutral"} label={row.original.mfa_enabled ? "MFA on" : "No MFA"} /> },
      {
        header: "Last login",
        id: "last_login",
        accessorKey: "last_login_at",
        size: 150,
        cell: ({ row }) => (
          <span className={row.original.last_login_at ? "text-sm text-text" : "text-sm text-faint"}>
            {row.original.last_login_at ? formatDateTime(row.original.last_login_at) : "Never"}
          </span>
        ),
      },
      {
        header: "",
        id: "actions",
        size: 96,
        cell: ({ row }) => (
          <div className="flex items-center justify-end gap-1">
            <Button variant="ghost" size="icon" onClick={() => { setEditUser(row.original); setEditRole(row.original.role); setEditPassword(""); }} type="button" aria-label={`Edit ${row.original.email}`}>
              <Pencil size={14} />
            </Button>
            <Button variant="ghost" size="icon" onClick={() => setDeleteTarget(row.original)} type="button" aria-label={`Delete ${row.original.email}`}>
              <Trash2 size={14} />
            </Button>
          </div>
        ),
      },
    ],
    [orgRoleByEmail, orgsEnabled],
  );

  return (
    <AppPanel
      eyebrow="Members"
      title="Users & access"
      description="Platform users are login accounts for the whole platform. Organization access is who holds a role in this org — the same person can appear in both."
      actions={
        <Button size="sm" disabled={!orgId} onClick={() => setAddOpen(true)} type="button">
          <Plus size={14} />
          Add user
        </Button>
      }
    >
      <div className="mt-4 grid gap-5">
        <section className="grid gap-2">
          <div className="flex items-center justify-between gap-3">
            <div className="min-w-0">
              <p className="label">Organization access</p>
              <p className="text-xs text-faint">Who has a role in this org</p>
            </div>
            <Badge variant="muted">{members.length}</Badge>
          </div>
          <DataTable columns={memberColumns} data={members} emptyText={loading ? "Loading members..." : "No members assigned yet. Use “Add user” to grant access."} minWidth={560} sortable />
        </section>
        <section className="grid gap-2">
          <div className="flex items-center justify-between gap-3">
            <div className="min-w-0">
              <p className="label">Platform users</p>
              <p className="text-xs text-faint">Login accounts for the whole platform</p>
            </div>
            <Badge variant="muted">{users.length}</Badge>
          </div>
          <DataTable columns={userColumns} data={users} emptyText={loading ? "Loading users..." : "No platform users created yet."} minWidth={560} sortable />
        </section>
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>

      <Modal
        open={addOpen}
        onClose={() => !upsertMutation.isPending && setAddOpen(false)}
        title="Add user"
        description="Grant a user access to this organization. Set a password to create a new platform login, or leave it blank to grant an existing user."
        footer={
          <>
            <Button variant="secondary" disabled={upsertMutation.isPending} onClick={() => setAddOpen(false)} type="button">Cancel</Button>
            <Button form="add-user-form" disabled={!orgId || upsertMutation.isPending || email.trim().length === 0} type="submit">
              <Plus size={14} />
              Add user
            </Button>
          </>
        }
      >
        <form id="add-user-form" className="grid gap-3" onSubmit={submit}>
          <Field label="Email">
            <Input placeholder="teammate@example.com" aria-label="Member email" value={email} onChange={(event) => setEmail(event.target.value)} type="email" />
          </Field>
          <Field label="Password" hint="Sets a password for a new platform login; leave blank to grant access to an existing user.">
            <Input placeholder="Optional" aria-label="Initial password" value={password} onChange={(event) => setPassword(event.target.value)} type="password" />
          </Field>
          <Field label="Role">
            <NativeSelect aria-label="Member role" value={role} onChange={(event) => setRole(event.target.value)}>
              <option value="owner">Owner</option>
              <option value="admin">Admin</option>
              <option value="developer">Developer</option>
              <option value="viewer">Viewer</option>
            </NativeSelect>
          </Field>
          {upsertMutation.error ? <p className="text-sm text-danger">{upsertMutation.error.message}</p> : null}
        </form>
      </Modal>

      <Modal
        open={Boolean(editUser)}
        onClose={() => !editMutation.isPending && setEditUser(null)}
        title={editUser ? `Edit ${editUser.email}` : "Edit user"}
        description="Change the platform role or set a new password. Leave the password blank to keep the current one."
        footer={
          <>
            <Button variant="secondary" disabled={editMutation.isPending} onClick={() => setEditUser(null)} type="button">Cancel</Button>
            <Button form="edit-user-form" disabled={editMutation.isPending} type="submit">
              <Save size={14} />
              Save changes
            </Button>
          </>
        }
      >
        <form
          id="edit-user-form"
          className="grid gap-3"
          onSubmit={(event) => {
            event.preventDefault();
            if (!editUser) return;
            editMutation.mutate({ id: editUser.id, nextEmail: editUser.email, nextRole: editRole, nextPassword: editPassword });
          }}
        >
          <Field label="Role">
            <NativeSelect aria-label="User role" value={editRole} onChange={(event) => setEditRole(event.target.value)}>
              <option value="admin">Admin</option>
              <option value="developer">Developer</option>
              <option value="viewer">Viewer</option>
            </NativeSelect>
          </Field>
          <Field label="New password" hint="Leave blank to keep the current password.">
            <Input placeholder="••••••••" aria-label="New password" value={editPassword} onChange={(event) => setEditPassword(event.target.value)} type="password" />
          </Field>
          {editMutation.error ? <p className="text-sm text-danger">{editMutation.error.message}</p> : null}
        </form>
      </Modal>

      <Modal
        open={Boolean(deleteTarget)}
        onClose={() => !deleteUserMutation.isPending && setDeleteTarget(null)}
        title="Delete user"
        description={deleteTarget ? `Permanently remove ${deleteTarget.email} and revoke their access. This cannot be undone.` : ""}
        footer={
          <>
            <Button variant="secondary" disabled={deleteUserMutation.isPending} onClick={() => setDeleteTarget(null)} type="button">Cancel</Button>
            <Button disabled={deleteUserMutation.isPending} onClick={() => deleteTarget && deleteUserMutation.mutate(deleteTarget.id)} type="button">
              <Trash2 size={14} />
              Delete user
            </Button>
          </>
        }
      >
        {deleteUserMutation.error ? <p className="text-sm text-danger">{deleteUserMutation.error.message}</p> : null}
      </Modal>
    </AppPanel>
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

  const memberColumns = useMemo<ColumnDef<TeamMember>[]>(
    () => [
      {
        header: "Member",
        accessorKey: "email",
        cell: ({ row }) => (
          <>
            <p className="cell-main">{row.original.email}</p>
            <p className="cell-sub font-mono">{row.original.user_id}</p>
          </>
        ),
      },
      {
        header: "",
        id: "actions",
        size: 56,
        cell: ({ row }) => (
          <Button variant="ghost" size="icon" disabled={deleteMemberMutation.isPending} onClick={() => deleteMemberMutation.mutate(row.original.email)} type="button" aria-label={`Remove ${row.original.email}`}>
            <X size={14} />
          </Button>
        ),
      },
    ],
    [deleteMemberMutation],
  );

  return (
    <AppPanel
      eyebrow="Teams"
      title="Project RBAC"
      actions={<UserPlus size={15} className="text-faint" />}
    >
      <form className="mt-4 grid grid-cols-[minmax(0,1fr)_150px_auto] gap-2 max-sm:grid-cols-1" onSubmit={createTeam}>
        <Input placeholder="Team name" value={name} onChange={(event) => setName(event.target.value)} />
        <Input className="font-mono" placeholder="slug" value={slug} onChange={(event) => setSlug(event.target.value)} />
        <Button className="justify-self-start" disabled={!orgId || createMutation.isPending || !name.trim()} type="submit" variant="secondary">
          <Plus size={14} />
          Team
        </Button>
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
            <Button variant="ghost" size="icon" disabled={deleteMutation.isPending} onClick={() => deleteMutation.mutate(team.slug)} type="button">
              <X size={14} />
            </Button>
          </div>
        ))}
      </div>
      {selectedSlug ? (
        <div className="mt-5 border-t border-border pt-4">
          <form className="flex gap-2 max-sm:flex-col" onSubmit={addMember}>
            <Input placeholder="member@example.com" value={email} onChange={(event) => setEmail(event.target.value)} type="email" />
            <Button className="justify-self-start" disabled={addMemberMutation.isPending || !email.trim()} type="submit" variant="secondary">
              <Plus size={14} />
              Add
            </Button>
          </form>
          <DataTable className="mt-3" columns={memberColumns} data={members} emptyText={loading ? "Loading members..." : "No team members yet."} minWidth={420} sortable />
        </div>
      ) : null}
      {createMutation.error ? <p className="mt-3 text-sm text-danger">{createMutation.error.message}</p> : null}
      {addMemberMutation.error ? <p className="mt-3 text-sm text-danger">{addMemberMutation.error.message}</p> : null}
    </AppPanel>
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
    <AppPanel
      eyebrow="Features"
      title="Org rollout policy"
      actions={<Badge variant="muted">{enabledCount} enabled</Badge>}
    >
      <p className="mt-1 text-sm text-muted">Org overrides control tenant rollout; unset flags inherit platform defaults.</p>
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
          <Button variant="secondary" disabled={!orgId || loading || mutation.isPending} type="submit">
            <Save size={14} />
            Save features
          </Button>
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
                      <NativeSelect className="max-w-[132px]" value={hasOverride ? String(overrides[key]) : "inherit"} onChange={(event) => {
                        if (event.target.value === "inherit") {
                          inherit(key);
                        } else {
                          setOverride(key, event.target.value === "true");
                        }
                      }}>
                        <option value="inherit">Inherit</option>
                        <option value="true">Enabled</option>
                        <option value="false">Disabled</option>
                      </NativeSelect>
                    </div>
                    <div className="mt-2 flex items-center gap-2">
                      <StatusPill tone={effectiveEnabled ? "success" : "neutral"} label={effectiveEnabled ? "enabled" : "disabled"} />
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
    </AppPanel>
  );
}

export function QuotaPanel({ orgId, quota, loading }: { orgId: string; quota?: OrgQuota; loading: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({ max_projects: 0, max_cpu: 0, max_ram_mb: 0, max_disk_gb: 0 });
  const quotaKey = `${orgId}:${quota?.updated_at ?? ""}:${quota?.max_projects ?? ""}:${quota?.max_cpu ?? ""}:${quota?.max_ram_mb ?? ""}:${quota?.max_disk_gb ?? ""}`;
  useEffect(() => {
    if (quota) {
      setForm({
        max_projects: quota.max_projects,
        max_cpu: quota.max_cpu,
        max_ram_mb: quota.max_ram_mb,
        max_disk_gb: quota.max_disk_gb,
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
    <AppPanel eyebrow="Quotas" title="Org limits">
      <form className="mt-4 grid grid-cols-5 items-end gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1" onSubmit={submit}>
        <Field label="Projects" hint="0 = unlimited">
          <Input min={0} type="number" aria-label="Max projects" value={form.max_projects} onChange={(event) => setForm({ ...form, max_projects: Number(event.target.value) })} />
        </Field>
        <Field label="vCPU" hint="0 = unlimited">
          <Input min={0} type="number" aria-label="Max vCPU" value={form.max_cpu} onChange={(event) => setForm({ ...form, max_cpu: Number(event.target.value) })} />
        </Field>
        <Field label="RAM MB" hint="0 = unlimited">
          <Input min={0} type="number" aria-label="Max RAM in MB" value={form.max_ram_mb} onChange={(event) => setForm({ ...form, max_ram_mb: Number(event.target.value) })} />
        </Field>
        <Field label="Disk GB" hint="0 = unlimited">
          <Input min={0} type="number" aria-label="Max disk in GB" value={form.max_disk_gb} onChange={(event) => setForm({ ...form, max_disk_gb: Number(event.target.value) })} />
        </Field>
        <Button className="justify-self-start max-xl:col-span-2 max-sm:col-span-1" disabled={!orgId || mutation.isPending} type="submit" variant="secondary">
          <Save size={14} />
          Save quotas
        </Button>
      </form>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading quotas...</p> : null}
        {quota ? (
          <>
            <div className="flex items-center justify-between gap-3">
              <p className="label">Current usage against limits</p>
              <span className="text-xs text-faint">— = unlimited</span>
            </div>
            <div className="metric-grid">
              <QuotaMetric label="Projects" used={quota.used.projects} max={quota.max_projects} />
              <QuotaMetric label="vCPU" used={quota.used.cpu} max={quota.max_cpu} />
              <QuotaMetric label="RAM" used={quota.used.ram_mb} max={quota.max_ram_mb} render={(value) => formatBytes(value * 1024 * 1024)} />
              <QuotaMetric label="Disk" used={quota.used.disk_gb} max={quota.max_disk_gb} render={(value) => `${value} GB`} />
              </div>
          </>
        ) : null}
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}

// Renders one used/max quota dimension as a MetricCard whose tone goes warning
// near the limit (>=80%) and danger at/over it. A 0 max means unlimited ("—").
function QuotaMetric({ label, used, max, render }: { label: string; used: number; max: number; render?: (value: number) => string }) {
  const format = render ?? ((value: number) => value.toString());
  const unlimited = !max || max <= 0;
  const tone: "default" | "warning" | "danger" = unlimited ? "default" : used >= max ? "danger" : used / max >= 0.8 ? "warning" : "default";
  return <MetricCard label={label} tone={tone} value={`${format(used)} / ${unlimited ? "—" : format(max)}`} detail={unlimited ? "unlimited" : `${Math.round((used / max) * 100)}% used`} />;
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
    <AppPanel
      eyebrow="Metering"
      title="Org usage"
      actions={
        <div className="flex items-center gap-2">
          {usage ? <time className="text-xs text-faint">{formatTime(usage.sampled_at)}</time> : null}
          {snapshotEnabled ? (
            <Button className="justify-self-start" disabled={!orgId || snapshotMutation.isPending} onClick={() => snapshotMutation.mutate()} size="sm" type="button" variant="secondary">
              <Save size={14} />
              Capture snapshot
            </Button>
          ) : (
            <StatusPill tone="neutral" label="Metering disabled" />
          )}
        </div>
      }
    >
      <div className="mt-4 grid gap-4">
        {loading ? <p className="text-sm text-muted">Loading usage...</p> : null}
        {usage ? (
          <>
            <SubSection title="Reserved resources">
              <div className="metric-grid">
                <MetricCard label="Projects" value={usage.resources.projects.toString()} />
                <MetricCard label="vCPU" value={usage.resources.cpu.toString()} />
                <MetricCard label="RAM" value={formatBytes(usage.resources.ram_mb * 1024 * 1024)} />
                <MetricCard label="Disk" value={`${usage.resources.disk_gb} GB`} />
                <MetricCard label="DB allocated" value={formatBytes(usage.db_allocated_bytes)} />
              </div>
            </SubSection>
            <SubSection title="Storage and operations">
              <div className="metric-grid">
                <MetricCard label="Storage" value={formatBytes(usage.storage_bytes)} />
                <MetricCard label="Read replicas" value={usage.read_replicas.toString()} />
                <MetricCard label="Replication pipelines" value={usage.replication_pipelines.toString()} />
                <MetricCard label="Analytics buckets" value={usage.analytics_buckets.toString()} />
                <MetricCard label="Backups" value={usage.backup_count.toString()} />
                <MetricCard label="Log events" value={usage.project_log_events.toLocaleString()} />
                <MetricCard label="Cron jobs" value={usage.database_cron_jobs.toString()} />
                <MetricCard label="Queues" value={usage.database_queues.toString()} />
                <MetricCard label="Webhooks" value={usage.database_webhooks.toString()} />
                <MetricCard label="Schemas" value={usage.database_schemas.toString()} />
                <MetricCard label="Auth clients" value={usage.auth_clients.toString()} />
                <MetricCard label="Auth hooks" value={usage.auth_hooks.toString()} />
                <MetricCard label="Log drains" value={usage.log_drains.toString()} />
                <MetricCard label="CDN projects" value={usage.cdn_enabled_projects.toString()} />
                <MetricCard label="CDN invalidations" value={usage.cdn_invalidations.toString()} />
                <MetricCard label="Secrets" value={usage.secrets.toString()} />
              </div>
            </SubSection>
            <SubSection title="Traffic and activity" description="Throughput and consumption counted toward billing.">
              <div className="metric-grid">
                <MetricCard label="Egress" value={formatBytes(usage.egress_bytes)} />
                <MetricCard label="Function invocations" value={usage.function_invocations.toLocaleString()} />
                <MetricCard label="Monthly active users" value={usage.auth_maus.toLocaleString()} />
              </div>
            </SubSection>
            {statuses.length > 0 ? (
              <SubSection title="Projects by status">
                <div className="flex flex-wrap gap-2">
                  {statuses.map(([status, count]) => (
                    <StatusPill key={status} status={status} label={`${status} ${count}`} />
                  ))}
                </div>
              </SubSection>
            ) : null}
            <SubSection title="Usage snapshots" description={snapshotEnabled ? "Durable metering ledger for billing and quota reviews." : "Enable usage metering in org feature flags to capture snapshots."}>
              {!snapshotEnabled ? <StatusPill tone="neutral" label="Disabled" /> : null}
              {snapshotEnabled && snapshotsLoading ? <p className="text-sm text-muted">Loading...</p> : null}
              {snapshotEnabled && !snapshotsLoading && snapshots.length === 0 ? <p className="text-sm text-muted">No captures yet.</p> : null}
              {snapshotEnabled && snapshots.length > 0 ? (
                <div className="grid gap-2">
                  {snapshots.slice(0, 3).map((snapshot) => (
                    <div className="usage-row" key={snapshot.id}>
                      <p className="text-sm font-medium">{formatTime(snapshot.sampled_at)}</p>
                      <p className="text-xs text-muted">{snapshot.metrics.resources.projects} projects</p>
                    </div>
                  ))}
                </div>
              ) : null}
            </SubSection>
          </>
        ) : null}
        {snapshotMutation.error ? <p className="text-sm text-danger">{snapshotMutation.error.message}</p> : null}
      </div>
    </AppPanel>
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
  const invoiceColumns = useMemo<ColumnDef<BillingInvoice>[]>(
    () => [
      {
        header: "Invoice",
        accessorKey: "number",
        cell: ({ row }) => (
          <>
            <p className="cell-main">{row.original.number}</p>
            <p className="cell-sub">{formatTime(row.original.period_start)} – {formatTime(row.original.period_end)} · {row.original.line_items.length} line items</p>
          </>
        ),
      },
      { header: "Amount", accessorKey: "total_cents", size: 160, cell: ({ row }) => <span className="text-sm">{formatMoney(row.original.total_cents, row.original.currency)}</span> },
      { header: "Due", id: "due", size: 150, cell: ({ row }) => <span className="text-xs text-muted">{formatTime(row.original.due_at)}</span> },
      { header: "Status", accessorKey: "status", size: 120, cell: ({ row }) => <StatusPill status={row.original.status} /> },
    ],
    [],
  );
  return (
    <AppPanel
      eyebrow="Billing"
      title="Invoices"
      actions={
        <div className="flex items-center gap-2">
          {!enabled ? <StatusPill tone="neutral" label="Disabled" /> : null}
          {enabled ? (
            <Button className="justify-self-start" disabled={!orgId || invoiceMutation.isPending} onClick={() => invoiceMutation.mutate()} size="sm" type="button" variant="secondary">
              <Save size={14} />
              Generate invoice
            </Button>
          ) : null}
        </div>
      }
    >
      <div className="mt-4 grid gap-2">
        {!enabled ? (
          <EmptyState icon={CreditCard} title="Billing disabled" description="Enable billing in org feature flags before listing or generating invoices." />
        ) : null}
        {enabled && loading ? <p className="text-sm text-muted">Loading invoices...</p> : null}
        {enabled && !loading && invoices.length === 0 ? (
          <EmptyState icon={CreditCard} title="No invoices yet" description="Generate a draft from the latest metering snapshot." />
        ) : null}
        {enabled && latest ? (
          <div className="metric-grid">
            <MetricCard label="Latest" value={latest.number} />
            <MetricCard label="Status" value={<StatusPill status={latest.status} />} />
            <MetricCard label="Total" value={formatMoney(latest.total_cents, latest.currency)} />
            <MetricCard label="Due" value={formatTime(latest.due_at)} />
          </div>
        ) : null}
        {enabled ? <DataTable columns={invoiceColumns} data={invoices} emptyText={loading ? "Loading invoices..." : "No invoices generated yet."} minWidth={560} sortable /> : null}
        {invoiceMutation.error ? <p className="text-sm text-danger">{invoiceMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}
