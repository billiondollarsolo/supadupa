import type { FormEvent } from "react";
import { useEffect, useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { flexRender, getCoreRowModel, getFilteredRowModel, getSortedRowModel, useReactTable, type ColumnDef, type SortingState } from "@tanstack/react-table";
import { Boxes, BrainCircuit, Database, Gauge, Network, Plus, Search, Server, Shield, Sparkles, type LucideIcon } from "lucide-react";
import { createProject } from "../../api";
import { formatBytes, formatDateTime } from "../../lib/format";
import type { Host, HostCapacity, Org, PlatformDefaults, Project } from "../../types";

type CreateProjectForm = {
  ref: string;
  name: string;
  host_id: string;
  domain: string;
  profile: "essential" | "full" | "orioledb";
  resource_tier: "small" | "medium" | "large";
};

type ProjectIntent = "prototype" | "production" | "ai" | "enterprise";

const intentOptions: Array<{
  id: ProjectIntent;
  label: string;
  description: string;
  profile: CreateProjectForm["profile"];
  tier: CreateProjectForm["resource_tier"];
  icon: LucideIcon;
  highlights: string[];
}> = [
  {
    id: "prototype",
    label: "Prototype or local dev",
    description: "Small isolated stack for experiments, demos, and local Docker Compose deployments.",
    profile: "essential",
    tier: "small",
    icon: Sparkles,
    highlights: ["Small footprint", "Fast create", "Easy reset"],
  },
  {
    id: "production",
    label: "Team production app",
    description: "Full Supabase-compatible surface for an app a team will actually run.",
    profile: "full",
    tier: "medium",
    icon: Gauge,
    highlights: ["Full stack", "Team default", "Room to grow"],
  },
  {
    id: "ai",
    label: "AI or data workflow",
    description: "Full stack with storage, functions, queues, embeddings, and pgvector-oriented defaults.",
    profile: "full",
    tier: "medium",
    icon: BrainCircuit,
    highlights: ["pgvector ready", "Functions ready", "Queue friendly"],
  },
  {
    id: "enterprise",
    label: "Enterprise workload",
    description: "Higher reserved capacity for heavier tenants, stricter isolation, and later K8s migration.",
    profile: "full",
    tier: "large",
    icon: Network,
    highlights: ["Dedicated capacity", "P3 ready", "Operational headroom"],
  },
];

export function ProjectTable({
  projects,
  orgNamesById,
  hostsById,
  selectedRef,
  onSelect,
  loading,
}: {
  projects: Project[];
  orgNamesById: Map<string, string>;
  hostsById: Map<string, Host>;
  selectedRef: string;
  onSelect: (ref: string) => void;
  loading: boolean;
}) {
  const [query, setQuery] = useState("");
  const [sorting, setSorting] = useState<SortingState>([]);
  const columns = useMemo<Array<ColumnDef<Project>>>(() => [
    {
      accessorKey: "name",
      header: "Name",
      cell: (info) => <span className="font-medium">{info.row.original.name}</span>,
    },
    {
      accessorKey: "ref",
      header: "Ref",
      cell: (info) => <span className="font-mono text-xs text-muted">{info.row.original.ref}</span>,
    },
    {
      id: "org",
      header: "Org",
      accessorFn: (project) => orgNamesById.get(project.org_id) ?? project.org_id,
      cell: (info) => <span className="text-muted">{String(info.getValue())}</span>,
    },
    {
      accessorKey: "status",
      header: "Status",
      cell: (info) => <span className={`pill ${info.row.original.status}`}>{info.row.original.status}</span>,
    },
    {
      id: "host",
      header: "Host / region",
      accessorFn: (project) => {
        const host = project.spec.host_id ? hostsById.get(project.spec.host_id) : undefined;
        return `${host?.name ?? "Default local runtime"} ${host?.address ?? "local"}`;
      },
      cell: (info) => {
        const project = info.row.original;
        const host = project.spec.host_id ? hostsById.get(project.spec.host_id) : undefined;
        return (
          <>
            <p className="truncate text-sm text-muted">{host?.name ?? "Default local runtime"}</p>
            <p className="truncate font-mono text-xs text-faint">{host?.address ?? "local"}</p>
          </>
        );
      },
    },
    {
      id: "version",
      header: "Version",
      accessorFn: (project) => project.spec.stack_version,
      cell: (info) => <span className="text-muted">{String(info.getValue())}</span>,
    },
    {
      accessorKey: "created_at",
      header: "Created",
      cell: (info) => <span className="text-muted">{formatDateTime(String(info.getValue()))}</span>,
    },
  ], [hostsById, orgNamesById]);
  const table = useReactTable({
    data: projects,
    columns,
    state: {
      globalFilter: query,
      sorting,
    },
    onGlobalFilterChange: setQuery,
    onSortingChange: setSorting,
    globalFilterFn: (row, _columnId, filterValue) => {
      const normalizedQuery = String(filterValue).trim().toLowerCase();
      if (!normalizedQuery) return true;
      const project = row.original;
      const host = project.spec.host_id ? hostsById.get(project.spec.host_id) : undefined;
      return [
        project.name,
        project.ref,
        orgNamesById.get(project.org_id) ?? project.org_id,
        project.status,
        project.spec.stack_version,
        project.spec.host_id ?? "default local runtime",
        host?.name ?? "",
        host?.address ?? "",
      ].some((value) => value.toLowerCase().includes(normalizedQuery));
    },
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });
  const rows = table.getRowModel().rows;

  return (
    <section className="panel overflow-hidden">
      <div className="section-head">
        <div>
          <p className="label">Projects</p>
          <h2>Isolated stacks</h2>
        </div>
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" size={14} />
          <input
            aria-label="Filter projects"
            className="input pl-8"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
      </div>
      <div className="mt-4 overflow-auto">
        <table className="w-full min-w-[920px] text-left text-sm">
          <thead className="text-faint">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr className="border-b border-border" key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  const sorted = header.column.getIsSorted();
                  return (
                    <th className="py-2 pr-4 font-medium" key={header.id}>
                      <button className="flex items-center gap-1 text-left text-faint transition hover:text-text" disabled={!header.column.getCanSort()} onClick={header.column.getToggleSortingHandler()} type="button">
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        {sorted ? <span className="text-[10px]">{sorted === "asc" ? "ASC" : "DESC"}</span> : null}
                      </button>
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td className="py-5 text-muted" colSpan={7}>
                  Loading projects...
                </td>
              </tr>
            ) : null}
            {!loading && rows.length === 0 ? (
              <tr>
                <td className="py-5 text-muted" colSpan={7}>
                  No projects match the filter.
                </td>
              </tr>
            ) : null}
            {rows.map((row) => {
              const project = row.original;
              return (
                <tr
                  className={project.ref === selectedRef ? "table-row bg-surface-2" : "table-row"}
                  key={row.id}
                  onClick={() => onSelect(project.ref)}
                >
                  {row.getVisibleCells().map((cell) => (
                    <td className="py-3 pr-4" key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

export function ProjectCards({
  projects,
  orgNamesById,
  hostsById,
  selectedRef,
  onSelect,
  onAccess,
  onCreate,
  loading,
  maxProjects,
}: {
  projects: Project[];
  orgNamesById: Map<string, string>;
  hostsById: Map<string, Host>;
  selectedRef: string;
  onSelect: (ref: string) => void;
  onAccess: (ref: string) => void;
  onCreate: () => void;
  loading: boolean;
  maxProjects?: number;
}) {
  const visibleProjects = typeof maxProjects === "number" ? projects.slice(0, maxProjects) : projects;
  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Projects</p>
          <h2>Project cards</h2>
        </div>
        <div className="flex items-center gap-2">
          <span className="pill">{projects.length} visible</span>
          <button className="button secondary h-8 min-h-8 justify-center" onClick={onCreate} type="button">
            <Plus size={14} />
            Create project
          </button>
        </div>
      </div>
      <div className="mt-4 grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-lg:grid-cols-1">
        {loading ? <p className="text-sm text-muted">Loading projects...</p> : null}
        {!loading && visibleProjects.length === 0 ? <p className="text-sm text-muted">No projects available.</p> : null}
        {visibleProjects.map((project) => {
          const host = project.spec.host_id ? hostsById.get(project.spec.host_id) : undefined;
          const reservation = reservationForTier(project.spec.resource_tier);
          return (
            <article className={project.ref === selectedRef ? "project-card active" : "project-card"} key={project.ref}>
              <button className="grid min-h-[360px] w-full min-w-0 content-start gap-3 text-left" onClick={() => onSelect(project.ref)} type="button">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="truncate text-base font-medium">{project.name}</p>
                    <p className="truncate font-mono text-xs text-muted">{project.ref}</p>
                  </div>
                  <span className={`pill ${project.status}`}>{project.status}</span>
                </div>
                <div className="rounded-md border border-border bg-surface p-2">
                  <p className="label">API URL</p>
                  <p className="mt-1 truncate font-mono text-xs text-muted">https://{project.ref}.{project.spec.domain}</p>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <CardMetric icon={Database} label="Org" value={orgNamesById.get(project.org_id) ?? project.org_id} />
                  <CardMetric icon={Boxes} label="Tier" value={project.spec.resource_tier} />
                  <CardMetric icon={Database} label="Profile" value={project.spec.profile} />
                  <CardMetric icon={Shield} label="Access" value="org/team + project grants" />
                </div>
                <ResourceSummary host={host} reservation={reservation} />
                <div className="grid grid-cols-2 gap-2">
                  <CardMetric icon={Boxes} label="Host" value={host?.name ?? "Default local runtime"} />
                  <CardMetric icon={Database} label="Version" value={project.spec.stack_version} />
                  <CardMetric icon={Boxes} label="Created" value={formatDateTime(project.created_at)} />
                  <CardMetric icon={Shield} label="Runtime" value={project.runtime_status?.phase ?? project.status} />
                </div>
                <div className="rounded-md border border-border bg-surface p-2">
                  <p className="truncate text-xs text-muted">{project.message ?? "Dedicated isolated stack"}</p>
                  <p className="truncate font-mono text-xs text-faint">{host?.address ?? "local"} · {project.spec.domain}</p>
                </div>
              </button>
              <div className="mt-3 grid grid-cols-2 gap-2">
                <button className="button secondary h-8 min-h-8 justify-center" onClick={() => onSelect(project.ref)} type="button">
                  Open
                </button>
                <button className="button secondary h-8 min-h-8 justify-center" onClick={() => onAccess(project.ref)} type="button">
                  <Shield size={14} />
                  Access
                </button>
              </div>
            </article>
          );
        })}
      </div>
    </section>
  );
}

function ResourceSummary({ host, reservation }: { host?: Host; reservation: HostCapacity }) {
  return (
    <div className="rounded-md border border-border bg-surface p-3">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="label">Reserved resources</p>
          <p className="mt-1 truncate text-xs text-muted">
            {reservation.cpu} vCPU · {formatBytes(reservation.ram_mb * 1024 * 1024)} RAM · {reservation.disk_gb} GB disk
          </p>
        </div>
        <span className="pill">{reservation.disk_iops.toLocaleString()} IOPS</span>
      </div>
      <div className="grid gap-2">
        <ResourceBar label="CPU" value={reservation.cpu} total={host?.capacity.cpu} suffix="vCPU" />
        <ResourceBar label="RAM" value={reservation.ram_mb} total={host?.capacity.ram_mb} format={(value) => formatBytes(value * 1024 * 1024)} />
        <ResourceBar label="Disk" value={reservation.disk_gb} total={host?.capacity.disk_gb} suffix="GB" />
        <ResourceBar label="IOPS" value={reservation.disk_iops} total={host?.capacity.disk_iops} format={(value) => value.toLocaleString()} />
      </div>
      <p className="mt-2 truncate text-xs text-faint">
        {host ? `Host reserved: ${host.used.cpu}/${host.capacity.cpu || "-"} vCPU · ${formatBytes(host.used.ram_mb * 1024 * 1024)} RAM` : "No host capacity registered yet"}
      </p>
    </div>
  );
}

function ResourceBar({
  label,
  value,
  total,
  suffix,
  format,
}: {
  label: string;
  value: number;
  total?: number;
  suffix?: string;
  format?: (value: number) => string;
}) {
  const percent = total && total > 0 ? Math.min(100, Math.max(0, (value / total) * 100)) : 0;
  const valueLabel = format ? format(value) : `${value}${suffix ? ` ${suffix}` : ""}`;
  const totalLabel = total && total > 0 ? (format ? format(total) : `${total}${suffix ? ` ${suffix}` : ""}`) : "-";
  return (
    <div className="grid gap-1">
      <div className="flex items-center justify-between gap-2 text-xs">
        <span className="text-faint">{label}</span>
        <span className="truncate text-muted">{valueLabel} / {totalLabel}</span>
      </div>
      <div className="resource-bar" aria-label={`${label} reserved`}>
        <span style={{ width: `${percent || 8}%` }} />
      </div>
    </div>
  );
}

function CardMetric({ icon: Icon, label, value }: { icon: typeof Database; label: string; value: string }) {
  return (
    <div className="metric-cell min-w-0">
      <div className="mb-1 flex items-center gap-1 text-faint">
        <Icon size={12} />
        <p className="label">{label}</p>
      </div>
      <p className="truncate text-xs text-muted">{value}</p>
    </div>
  );
}

function reservationForTier(tier: string): HostCapacity {
  if (tier === "large") return { cpu: 4, ram_mb: 8192, disk_gb: 100, disk_iops: 12000, projects: 1 };
  if (tier === "medium") return { cpu: 2, ram_mb: 4096, disk_gb: 50, disk_iops: 6000, projects: 1 };
  return { cpu: 1, ram_mb: 2048, disk_gb: 20, disk_iops: 3000, projects: 1 };
}

export function CreateProjectPanel({
  orgId,
  orgs,
  hosts,
  defaults,
  onSelectOrg,
  onCreated,
}: {
  orgId: string;
  orgs: Org[];
  hosts: Host[];
  defaults?: PlatformDefaults;
  onSelectOrg: (orgId: string) => void;
  onCreated: (project: Project) => void;
}) {
  const wizardSteps = ["Goal", "Identity", "Org", "Placement", "Stack", "Review"];
  const [step, setStep] = useState(0);
  const [intent, setIntent] = useState<ProjectIntent>("production");
  const [form, setForm] = useState<CreateProjectForm>({
    ref: "alpha",
    name: "Alpha",
    host_id: "",
    domain: "supadupa.test",
    profile: "full",
    resource_tier: "small",
  });
  useEffect(() => {
    if (!defaults) return;
    setForm((current) => ({
      ...current,
      domain: defaults.domain || current.domain,
      profile: defaults.profile === "essential" || defaults.profile === "orioledb" ? defaults.profile : "full",
      resource_tier: defaults.resource_tier === "medium" || defaults.resource_tier === "large" ? defaults.resource_tier : "small",
    }));
  }, [defaults]);
  const mutation = useMutation({
    mutationFn: createProject,
    onSuccess: onCreated,
  });

  function selectIntent(nextIntent: ProjectIntent) {
    const option = intentOptions.find((item) => item.id === nextIntent);
    setIntent(nextIntent);
    if (!option) return;
    setForm((current) => ({ ...current, profile: option.profile, resource_tier: option.tier }));
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!orgId) return;
    mutation.mutate({ orgId, ...form });
  }

  const selectedHost = hosts.find((host) => host.id === form.host_id);
  const selectedOrg = orgs.find((org) => org.id === orgId);
  const selectedIntent = intentOptions.find((option) => option.id === intent) ?? intentOptions[1];
  const reservation = reservationForTier(form.resource_tier);
  const hostCapacityProblem = Boolean(selectedHost && !hostCanFit(selectedHost, reservation));
  const currentValid =
    step === 1 ? form.name.trim().length > 0 && form.ref.trim().length > 0 && form.domain.trim().length > 0 :
    step === 2 ? orgId.length > 0 :
    true;
  const canSubmit = orgId.length > 0 && form.name.trim().length > 0 && form.ref.trim().length > 0 && form.domain.trim().length > 0 && !hostCapacityProblem;
  const nextStep = () => setStep((current) => Math.min(current + 1, wizardSteps.length - 1));
  const previousStep = () => setStep((current) => Math.max(current - 1, 0));

  return (
    <div className="grid grid-cols-[minmax(0,1fr)_360px] gap-6 max-xl:grid-cols-1">
      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Create project</p>
            <h2>Provision isolated Supabase stack</h2>
          </div>
          <span className="pill">{selectedIntent.label}</span>
        </div>
        <p className="mt-2 max-w-2xl text-sm text-muted">
          Choose the workload first. supadupa turns that into a stack profile, resource reservation, host placement, routing domain, and a dedicated project boundary.
        </p>
        <form className="mt-4 grid gap-4" onSubmit={submit}>
          <div className="wizard-steps">
            {wizardSteps.map((label, index) => (
              <button className={index === step ? "wizard-step active" : index < step ? "wizard-step complete" : "wizard-step"} key={label} onClick={() => setStep(index)} type="button">
                <span>{index + 1}</span>
                {label}
              </button>
            ))}
          </div>

          {step === 0 ? (
            <div className="grid grid-cols-2 gap-3 max-lg:grid-cols-1">
              {intentOptions.map((option) => (
                <IntentCard active={intent === option.id} key={option.id} option={option} onSelect={() => selectIntent(option.id)} />
              ))}
            </div>
          ) : null}

          {step === 1 ? (
            <div className="grid gap-3">
              <div className="grid grid-cols-2 gap-3 max-md:grid-cols-1">
                <label className="grid gap-1">
                  <span className="label">Project name</span>
                  <input className="input" aria-label="Project name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
                </label>
                <label className="grid gap-1">
                  <span className="label">Project ref</span>
                  <input className="input font-mono" aria-label="Project ref" value={form.ref} onChange={(event) => setForm({ ...form, ref: normalizeProjectRef(event.target.value) })} />
                </label>
              </div>
              <label className="grid gap-1">
                <span className="label">Base domain</span>
                <input className="input" aria-label="Project domain" value={form.domain} onChange={(event) => setForm({ ...form, domain: event.target.value })} />
              </label>
              <div className="wizard-review">
                <ReviewRow label="Primary API URL" value={`https://${form.ref || "<ref>"}.${form.domain || "<domain>"}`} />
                <ReviewRow label="Project boundary" value="Dedicated Postgres volume, network, JWT keys, and Supabase services" />
              </div>
            </div>
          ) : null}

          {step === 2 ? (
            <div className="grid gap-3">
              <label className="grid gap-1">
                <span className="label">Organization</span>
                <select className="input" value={orgId} onChange={(event) => onSelectOrg(event.target.value)}>
                  <option value="">Select organization</option>
                  {orgs.map((org) => (
                    <option key={org.id} value={org.id}>
                      {org.name}
                    </option>
                  ))}
                </select>
              </label>
              {orgs.length === 0 ? <p className="text-sm text-muted">Create an organization first.</p> : null}
              <div className="wizard-review">
                <ReviewRow label="Organization" value={selectedOrg ? selectedOrg.name : orgId || "Missing"} />
                {selectedOrg ? <ReviewRow label="Org ID" value={selectedOrg.id} /> : null}
                <ReviewRow label="Access model" value="Global admins can see all; org and project grants scope everyone else" />
              </div>
            </div>
          ) : null}

          {step === 3 ? (
            <div className="grid gap-3">
              <label className="grid gap-1">
                <span className="label">Host placement</span>
                <select className="input" value={form.host_id} onChange={(event) => setForm({ ...form, host_id: event.target.value })}>
                  <option value="">Default local runtime</option>
                  {hosts.map((host) => (
                    <option key={host.id} value={host.id}>
                      {host.name} · {host.address}
                    </option>
                  ))}
                </select>
              </label>
              {selectedHost ? <HostFitPanel host={selectedHost} reservation={reservation} /> : <PlacementDefaultPanel />}
              {hostCapacityProblem ? <p className="text-sm text-danger">Selected host does not have enough registered capacity for this tier.</p> : null}
            </div>
          ) : null}

          {step === 4 ? (
            <div className="grid gap-4">
              <div>
                <p className="label">Stack profile</p>
                <div className="mt-2 grid grid-cols-3 gap-2 max-lg:grid-cols-1">
                  {(["full", "essential", "orioledb"] as const).map((profile) => (
                    <button className={form.profile === profile ? "choice active" : "choice"} key={profile} onClick={() => setForm({ ...form, profile })} type="button">
                      <span className="text-sm font-medium capitalize">{profile}</span>
                      <span className="text-xs text-faint">{profile === "full" ? "All upstream stack surfaces" : profile === "essential" ? "Reduced local footprint" : "Full stack with OrioleDB metadata"}</span>
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <p className="label">Resource tier</p>
                <div className="mt-2 grid grid-cols-3 gap-2 max-lg:grid-cols-1">
                  {(["small", "medium", "large"] as const).map((resourceTier) => {
                    const tierReservation = reservationForTier(resourceTier);
                    return (
                      <button className={form.resource_tier === resourceTier ? "choice active" : "choice"} key={resourceTier} onClick={() => setForm({ ...form, resource_tier: resourceTier })} type="button">
                        <span className="text-sm font-medium capitalize">{resourceTier}</span>
                        <span className="text-xs text-faint">{tierReservation.cpu} vCPU · {formatBytes(tierReservation.ram_mb * 1024 * 1024)} · {tierReservation.disk_gb} GB disk</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            </div>
          ) : null}

          {step === 5 ? (
            <div className="wizard-review">
              <ReviewRow label="Goal" value={selectedIntent.label} />
              <ReviewRow label="Name" value={form.name} />
              <ReviewRow label="Ref" value={form.ref} />
              <ReviewRow label="Org" value={selectedOrg ? selectedOrg.name : orgId || "Missing"} />
              <ReviewRow label="Host" value={selectedHost ? `${selectedHost.name} · ${selectedHost.address}` : "Default local runtime"} />
              <ReviewRow label="Profile" value={form.profile} />
              <ReviewRow label="Tier" value={`${form.resource_tier} · ${reservation.cpu} vCPU · ${formatBytes(reservation.ram_mb * 1024 * 1024)} RAM · ${reservation.disk_gb} GB disk`} />
              <ReviewRow label="API domain" value={`${form.ref}.${form.domain}`} />
            </div>
          ) : null}

          <div className="flex gap-2 max-sm:flex-col">
            <button className="button secondary justify-center" disabled={step === 0 || mutation.isPending} onClick={previousStep} type="button">
              Back
            </button>
            {step < wizardSteps.length - 1 ? (
              <button className="button justify-center" disabled={!currentValid || mutation.isPending} onClick={nextStep} type="button">
                Next
              </button>
            ) : (
              <button className="button justify-center" disabled={!canSubmit || mutation.isPending} type="submit">
                <Plus size={14} />
                Create project
              </button>
            )}
          </div>
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </form>
      </section>
      <CreatePlanPanel form={form} host={selectedHost} intent={selectedIntent} org={selectedOrg} reservation={reservation} />
    </div>
  );
}

function IntentCard({
  active,
  onSelect,
  option,
}: {
  active: boolean;
  onSelect: () => void;
  option: (typeof intentOptions)[number];
}) {
  const Icon = option.icon;
  return (
    <button className={active ? "choice active min-h-[160px]" : "choice min-h-[160px]"} onClick={onSelect} type="button">
      <div className="flex items-start justify-between gap-3">
        <span className="grid h-9 w-9 place-items-center rounded-md border border-border bg-surface-2 text-text">
          <Icon size={16} />
        </span>
        <span className="pill">{option.profile} · {option.tier}</span>
      </div>
      <span className="text-sm font-medium">{option.label}</span>
      <span className="text-xs leading-5 text-muted">{option.description}</span>
      <span className="mt-auto flex flex-wrap gap-1">
        {option.highlights.map((highlight) => (
          <span className="pill" key={highlight}>{highlight}</span>
        ))}
      </span>
    </button>
  );
}

function CreatePlanPanel({
  form,
  host,
  intent,
  org,
  reservation,
}: {
  form: CreateProjectForm;
  host?: Host;
  intent: (typeof intentOptions)[number];
  org?: Org;
  reservation: HostCapacity;
}) {
  return (
    <aside className="grid content-start gap-3">
      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Provisioning plan</p>
            <h2>{form.name || "New project"}</h2>
          </div>
          <span className="pill">{form.resource_tier}</span>
        </div>
        <div className="mt-4 grid gap-3">
          <div className="rounded-md border border-border bg-bg p-3">
            <p className="label">Workload</p>
            <p className="mt-1 text-sm font-medium">{intent.label}</p>
            <p className="mt-1 text-xs leading-5 text-muted">{intent.description}</p>
          </div>
          <ResourceSummary host={host} reservation={reservation} />
          <div className="wizard-review">
            <ReviewRow label="Org" value={org?.name ?? "Select organization"} />
            <ReviewRow label="Host" value={host ? host.name : "Default local runtime"} />
            <ReviewRow label="API URL" value={`https://${form.ref || "<ref>"}.${form.domain || "<domain>"}`} />
            <ReviewRow label="Profile" value={form.profile} />
          </div>
        </div>
      </section>
      <section className="panel">
        <p className="label">This creates</p>
        <div className="mt-3 grid gap-2 text-sm text-muted">
          <CreateFact icon={Database} text="Dedicated Postgres volume and Supabase service stack" />
          <CreateFact icon={Shield} text="Project-scoped JWT secrets, API keys, and RBAC grants" />
          <CreateFact icon={Server} text="Kong route, Studio deep-link, pooler URLs, and CLI profile" />
          <CreateFact icon={Boxes} text="Storage, Realtime, Functions, GraphQL, REST, logs, and backups surfaces" />
        </div>
      </section>
    </aside>
  );
}

function CreateFact({ icon: Icon, text }: { icon: LucideIcon; text: string }) {
  return (
    <div className="flex gap-2 rounded-md border border-border bg-bg p-2">
      <Icon className="mt-0.5 shrink-0 text-faint" size={14} />
      <p className="leading-5">{text}</p>
    </div>
  );
}

function HostFitPanel({ host, reservation }: { host: Host; reservation: HostCapacity }) {
  const fits = hostCanFit(host, reservation);
  return (
    <div className="rounded-md border border-border bg-bg p-3">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="label">Host fit</p>
          <p className="mt-1 truncate text-sm font-medium">{host.name}</p>
        </div>
        <span className={`pill ${fits ? "healthy" : "error"}`}>{fits ? "capacity ok" : "capacity short"}</span>
      </div>
      <div className="grid gap-2">
        <ResourceBar label="CPU after create" value={host.used.cpu + reservation.cpu} total={host.capacity.cpu} suffix="vCPU" />
        <ResourceBar label="RAM after create" value={host.used.ram_mb + reservation.ram_mb} total={host.capacity.ram_mb} format={(value) => formatBytes(value * 1024 * 1024)} />
        <ResourceBar label="Disk after create" value={host.used.disk_gb + reservation.disk_gb} total={host.capacity.disk_gb} suffix="GB" />
        <ResourceBar label="IOPS after create" value={host.used.disk_iops + reservation.disk_iops} total={host.capacity.disk_iops} format={(value) => value.toLocaleString()} />
      </div>
    </div>
  );
}

function PlacementDefaultPanel() {
  return (
    <div className="rounded-md border border-border bg-bg p-3">
      <p className="label">Default placement</p>
      <p className="mt-1 text-sm font-medium">Use the local Compose runtime</p>
      <p className="mt-1 text-xs leading-5 text-muted">Good for MVP Docker Compose installs. Register hosts when you want capacity accounting, named placement, and the later Kubernetes operator path.</p>
    </div>
  );
}

function normalizeProjectRef(value: string) {
  return value.toLowerCase().replace(/[^a-z0-9-]/g, "-").replace(/-+/g, "-").replace(/^-/, "");
}

function hostCanFit(host: Host, reservation: HostCapacity) {
  return (
    (host.capacity.cpu <= 0 || host.used.cpu + reservation.cpu <= host.capacity.cpu) &&
    (host.capacity.ram_mb <= 0 || host.used.ram_mb + reservation.ram_mb <= host.capacity.ram_mb) &&
    (host.capacity.disk_gb <= 0 || host.used.disk_gb + reservation.disk_gb <= host.capacity.disk_gb) &&
    (host.capacity.disk_iops <= 0 || host.used.disk_iops + reservation.disk_iops <= host.capacity.disk_iops) &&
    (host.capacity.projects <= 0 || host.used.projects + reservation.projects <= host.capacity.projects)
  );
}

function ReviewRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="review-row">
      <span className="label">{label}</span>
      <span className="truncate font-mono text-xs text-muted">{value}</span>
    </div>
  );
}
