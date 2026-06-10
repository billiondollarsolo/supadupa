import type { FormEvent } from "react";
import { useEffect, useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { flexRender, getCoreRowModel, getFilteredRowModel, getSortedRowModel, useReactTable, type ColumnDef, type SortingState } from "@tanstack/react-table";
import { Boxes, Plus, Search } from "lucide-react";
import { createProject } from "../../api";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { Button } from "../../components/ui/button";
import { EmptyState } from "../../components/ui/empty-state";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../components/ui/table";
import { Field } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import { DbExposureBadge } from "../../components/db-exposure-badge";
import { formatBytes, formatDateTime } from "../../lib/format";
import type { Host, HostCapacity, Org, PlatformDefaults, Project, StackReleaseManifest } from "../../types";

type StackProfile = "essential" | "full" | "orioledb";

type CreateProjectForm = {
  ref: string;
  name: string;
  host_id: string;
  domain: string;
  stack_version: string;
  profile: StackProfile;
  resource_tier: "small" | "medium" | "large";
  // Exact-size overrides. 0 means "use the tier preset" for that dimension.
  cpu: number;
  ram_mb: number;
  disk_gb: number;
  // Opt-in: apply real container CPU/memory limits to the database service.
  enforce_limits: boolean;
  // Per-service enable map (keys = ALL_SERVICES). The profile seeds this; the
  // user can then toggle any service individually.
  services: Record<string, boolean>;
};

// Platform sizing bounds (must mirror the control-plane validateResourceSizing).
const SIZING_BOUNDS = { maxCpu: 64, minRamMB: 256, maxRamMB: 262144, maxDiskGB: 16384 } as const;

// The full Supabase service set the control plane can render per project. Each is
// individually gated in the rendered compose file, so any combination is valid.
const PROJECT_SERVICES: Array<{ key: string; label: string; description: string }> = [
  { key: "auth", label: "Auth", description: "GoTrue — signups, login, JWTs, OAuth/SSO." },
  { key: "rest", label: "REST API", description: "PostgREST — auto REST API over your tables." },
  { key: "graphql", label: "GraphQL", description: "pg_graphql — GraphQL endpoint over your schema." },
  { key: "realtime", label: "Realtime", description: "Postgres changes, presence & broadcast over WebSockets." },
  { key: "storage", label: "Storage", description: "S3-compatible object storage with RLS." },
  { key: "imgproxy", label: "Imgproxy", description: "On-the-fly image resizing/transforms for Storage." },
  { key: "functions", label: "Edge Functions", description: "Deno runtime for serverless functions." },
  { key: "pooler", label: "Pooler", description: "Supavisor — connection pooling for Postgres." },
  { key: "studio", label: "Studio", description: "The web dashboard for this project." },
  { key: "analytics", label: "Analytics", description: "Logflare — log analytics backend." },
  { key: "vector", label: "Vector", description: "Log shipping/collection agent." },
];
const ALL_SERVICES = PROJECT_SERVICES.map((s) => s.key);

// "Essential" trims the heavier optional surfaces (GraphQL, image proxy, log
// analytics + shipping) to a lean app-serving core; "full"/"orioledb" enable
// everything. The DB engine is the other axis: orioledb swaps stock Postgres for
// the OrioleDB storage engine (preview).
const ESSENTIAL_OFF = new Set(["graphql", "imgproxy", "analytics", "vector"]);

function servicesForProfile(profile: StackProfile): Record<string, boolean> {
  const out: Record<string, boolean> = {};
  for (const key of ALL_SERVICES) out[key] = profile === "essential" ? !ESSENTIAL_OFF.has(key) : true;
  return out;
}

const STACK_PROFILES: Array<{ id: StackProfile; label: string; engine: string; blurb: string }> = [
  { id: "full", label: "Full", engine: "Postgres", blurb: "Stock Postgres with the complete Supabase service set. The default for real apps." },
  { id: "essential", label: "Essential", engine: "Postgres", blurb: "Stock Postgres, lean core (no GraphQL, image proxy, or log analytics/shipping). Lighter footprint." },
  { id: "orioledb", label: "OrioleDB", engine: "OrioleDB (preview)", blurb: "Swaps stock Postgres for the OrioleDB storage engine (preview). Same services as Full." },
];

// Single sortable, searchable projects table.
export function ProjectsListPanel({
  projects,
  orgNamesById,
  hostsById,
  selectedRef,
  onSelect,
  onCreate,
  loading,
}: {
  projects: Project[];
  orgNamesById: Map<string, string>;
  hostsById: Map<string, Host>;
  selectedRef: string;
  onSelect: (ref: string) => void;
  onCreate: () => void;
  loading: boolean;
}) {
  const [query, setQuery] = useState("");
  const [sorting, setSorting] = useState<SortingState>([]);
  const columns = useMemo<Array<ColumnDef<Project>>>(() => [
    {
      accessorKey: "name",
      header: "Name",
      cell: (info) => (
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate font-medium">{info.row.original.name}</span>
          <DbExposureBadge mode={info.row.original.db_ingress_mode} />
        </span>
      ),
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
      cell: (info) => <StatusPill status={info.row.original.status} />,
    },
    {
      id: "runtime",
      header: "Runtime phase",
      accessorFn: (project) => project.runtime_status?.phase ?? "",
      cell: (info) => {
        const phase = info.row.original.runtime_status?.phase;
        return phase ? <StatusPill status={phase} /> : <span className="text-faint">—</span>;
      },
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
        project.runtime_status?.phase ?? "",
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
  const filteredProjects = rows.map((row) => row.original);

  return (
    <AppPanel
      className="overflow-hidden"
      eyebrow="Projects"
      title={`${projects.length} project${projects.length === 1 ? "" : "s"}`}
      actions={
        <div className="flex items-center gap-2">
          <div className="relative w-44 sm:w-56">
            <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" size={14} />
            <Input
              aria-label="Search projects"
              className="pl-8"
              placeholder="Search projects"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </div>
          <Button className="shrink-0" onClick={onCreate} type="button">
            <Plus size={14} />
            New project
          </Button>
        </div>
      }
    >
      {loading ? <p className="mt-4 text-sm text-muted">Loading projects...</p> : null}
      {!loading && projects.length === 0 ? (
        <EmptyState className="mt-4" icon={Boxes} title="No projects yet" description="Provision an isolated Supabase stack to get started." action={<Button className="justify-center" onClick={onCreate} size="sm" type="button" variant="secondary"><Plus size={14} />Create project</Button>} />
      ) : null}
      {!loading && projects.length > 0 && filteredProjects.length === 0 ? (
        <EmptyState className="mt-4" icon={Search} title="No matches" description="No projects match your search." />
      ) : null}
      {!loading && rows.length > 0 ? (
        <div className="data-table-wrap mt-4">
          <Table className="data-table" style={{ minWidth: 1160 }}>
            <TableHeader>
              {table.getHeaderGroups().map((headerGroup) => (
                <TableRow key={headerGroup.id}>
                  {headerGroup.headers.map((header) => {
                    const sorted = header.column.getIsSorted();
                    return (
                      <TableHead key={header.id}>
                        {header.column.getCanSort() ? (
                          <button className="flex items-center gap-1 text-left uppercase transition hover:text-text" onClick={header.column.getToggleSortingHandler()} type="button">
                            {flexRender(header.column.columnDef.header, header.getContext())}
                            {sorted ? <span className="text-accent">{sorted === "asc" ? "↑" : "↓"}</span> : null}
                          </button>
                        ) : (
                          flexRender(header.column.columnDef.header, header.getContext())
                        )}
                      </TableHead>
                    );
                  })}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {rows.map((row) => {
                const project = row.original;
                return (
                  <TableRow className={`cursor-pointer ${project.ref === selectedRef ? "bg-surface-2" : ""}`} key={row.id} onClick={() => onSelect(project.ref)}>
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
                    ))}
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      ) : null}
    </AppPanel>
  );
}

function ResourceSummary({ host, reservation }: { host?: Host; reservation: HostCapacity }) {
  return (
    <div className="resource-summary">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="label">Reserved of host capacity</p>
          <p className="mt-1 truncate text-xs text-muted">
            {reservation.cpu} vCPU · {formatBytes(reservation.ram_mb * 1024 * 1024)} RAM · {reservation.disk_gb} GB disk
          </p>
        </div>
      </div>
      <div className="grid gap-2">
        <ResourceBar label={host ? "CPU reserved / host" : "CPU"} value={reservation.cpu} total={host?.capacity.cpu} reference={LARGE_PRESET.cpu} suffix="vCPU" />
        <ResourceBar label={host ? "RAM reserved / host" : "RAM"} value={reservation.ram_mb} total={host?.capacity.ram_mb} reference={LARGE_PRESET.ram_mb} format={(value) => formatBytes(value * 1024 * 1024)} />
        <ResourceBar label={host ? "Disk reserved / host" : "Disk"} value={reservation.disk_gb} total={host?.capacity.disk_gb} reference={LARGE_PRESET.disk_gb} suffix="GB" />
      </div>
      <p className="mt-2 truncate text-xs text-faint">
        {host ? `Host total reserved: ${host.used.cpu}/${host.capacity.cpu || "-"} vCPU · ${formatBytes(host.used.ram_mb * 1024 * 1024)} RAM` : "No host selected; bars are scaled to the large preset for comparison."}
      </p>
    </div>
  );
}

function ResourceBar({
  label,
  value,
  total,
  reference,
  suffix,
  format,
}: {
  label: string;
  value: number;
  total?: number;
  // Fallback scale used when no host total is known, so the bar still reflects
  // the chosen size (scaled against the largest preset for comparison).
  reference?: number;
  suffix?: string;
  format?: (value: number) => string;
}) {
  const scale = total && total > 0 ? total : reference;
  const percent = scale && scale > 0 ? Math.min(100, Math.max(0, (value / scale) * 100)) : 0;
  const valueLabel = format ? format(value) : `${value}${suffix ? ` ${suffix}` : ""}`;
  const totalLabel = total && total > 0 ? (format ? format(total) : `${total}${suffix ? ` ${suffix}` : ""}`) : null;
  return (
    <div className="grid gap-1">
      <div className="flex items-center justify-between gap-2 text-xs">
        <span className="text-faint">{label}</span>
        <span className="truncate text-muted">{valueLabel}{totalLabel ? ` / ${totalLabel}` : ""}</span>
      </div>
      <div className="resource-bar" aria-label={`${label} reserved`}>
        <span style={{ width: `${Math.max(percent, 4)}%` }} />
      </div>
    </div>
  );
}

// Largest tier preset — used as the comparison scale for the plan-panel bars
// when no host is selected, so different sizes render at visibly different fills.
const LARGE_PRESET = reservationForTier("large");

function reservationForTier(tier: string): HostCapacity {
  if (tier === "large") return { cpu: 4, ram_mb: 8192, disk_gb: 100, projects: 1 };
  if (tier === "medium") return { cpu: 2, ram_mb: 4096, disk_gb: 50, projects: 1 };
  return { cpu: 1, ram_mb: 2048, disk_gb: 20, projects: 1 };
}

// effectiveReservation mirrors the control plane: start from the tier preset and
// apply any exact per-dimension override (> 0). This is what will actually be
// reserved and, when enforcement is on, limited on the container.
function effectiveReservation(form: CreateProjectForm): HostCapacity {
  const preset = reservationForTier(form.resource_tier);
  return {
    cpu: form.cpu > 0 ? form.cpu : preset.cpu,
    ram_mb: form.ram_mb > 0 ? form.ram_mb : preset.ram_mb,
    disk_gb: form.disk_gb > 0 ? form.disk_gb : preset.disk_gb,
    projects: 1,
  };
}

export function CreateProjectPanel({
  orgId,
  orgs,
  hosts,
  defaults,
  stackReleases,
  onSelectOrg,
  onCreated,
}: {
  orgId: string;
  orgs: Org[];
  hosts: Host[];
  defaults?: PlatformDefaults;
  stackReleases: StackReleaseManifest[];
  onSelectOrg: (orgId: string) => void;
  onCreated: (project: Project) => void;
}) {
  const wizardSteps = ["Identity", "Org & placement", "Stack"];
  const [step, setStep] = useState(0);
  const smallPreset = reservationForTier("small");
  const [form, setForm] = useState<CreateProjectForm>({
    ref: "",
    name: "",
    host_id: "",
    domain: "supadupa.test",
    stack_version: "latest",
    profile: "full",
    resource_tier: "small",
    // Exact size always carries concrete numbers (prefilled from the tier
    // preset); the user can edit any field to override that dimension.
    cpu: smallPreset.cpu,
    ram_mb: smallPreset.ram_mb,
    disk_gb: smallPreset.disk_gb,
    enforce_limits: false,
    services: servicesForProfile("full"),
  });
  useEffect(() => {
    if (!defaults) return;
    const tier = defaults.resource_tier === "medium" || defaults.resource_tier === "large" ? defaults.resource_tier : "small";
    const preset = reservationForTier(tier);
    const profile: StackProfile = defaults.profile === "essential" || defaults.profile === "orioledb" ? defaults.profile : "full";
    setForm((current) => ({
      ...current,
      domain: defaults.domain || current.domain,
      stack_version: defaults.stack_version || current.stack_version,
      profile,
      resource_tier: tier,
      cpu: preset.cpu,
      ram_mb: preset.ram_mb,
      disk_gb: preset.disk_gb,
      services: servicesForProfile(profile),
    }));
  }, [defaults]);
  const mutation = useMutation({
    mutationFn: createProject,
    onSuccess: onCreated,
  });

  // Picking a profile re-seeds the service selection from that profile's
  // template; the user can still toggle individual services afterward.
  function chooseProfile(profile: StackProfile) {
    setForm((current) => ({ ...current, profile, services: servicesForProfile(profile) }));
  }

  function toggleService(key: string) {
    setForm((current) => {
      const services = { ...current.services, [key]: !current.services[key] };
      // Hard dependency: Imgproxy only transforms Storage objects, so it can't
      // run without Storage. Turning Storage off also turns Imgproxy off.
      if (key === "storage" && !services.storage) services.imgproxy = false;
      return { ...current, services };
    });
  }

  // Picking a tier preset snaps the exact-size inputs to that preset, so the
  // numbers always reflect the chosen preset until the user edits them.
  function chooseTier(tier: CreateProjectForm["resource_tier"]) {
    const preset = reservationForTier(tier);
    setForm((current) => ({
      ...current,
      resource_tier: tier,
      cpu: preset.cpu,
      ram_mb: preset.ram_mb,
      disk_gb: preset.disk_gb,
    }));
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!orgId) return;
    mutation.mutate({ orgId, ...form });
  }

  const selectedHost = hosts.find((host) => host.id === form.host_id);
  const selectedOrg = orgs.find((org) => org.id === orgId);
  const latestRelease = stackReleases[0];
  const selectedRelease = stackReleases.find((release) => release.version === form.stack_version);
  const reservation = effectiveReservation(form);
  const enabledServiceCount = ALL_SERVICES.filter((key) => form.services[key]).length;
  // Soft advisories — the stack is still valid, but the combination is unusual
  // and worth surfacing. (Hard deps like Imgproxy→Storage are auto-enforced.)
  const serviceWarnings: string[] = [];
  if (!form.services.studio) serviceWarnings.push("Studio is off — this project won't have a dashboard; manage it via the API, CLI, or SQL.");
  if (form.services.analytics !== form.services.vector) serviceWarnings.push("Analytics (Logflare) and Vector are the logging pipeline — enable both or neither, or project logs won't be collected.");
  if (!form.services.rest && form.services.storage) serviceWarnings.push("Storage works best with the REST API enabled; some Storage operations route through PostgREST.");
  const hostCapacityProblem = Boolean(selectedHost && !hostCanFit(selectedHost, reservation));
  const identityValid = form.name.trim().length > 0 && form.ref.trim().length > 0 && form.domain.trim().length > 0;
  const placementValid = orgId.length > 0 && !hostCapacityProblem;
  const sizingValid =
    form.cpu >= 1 && form.cpu <= SIZING_BOUNDS.maxCpu &&
    form.ram_mb >= SIZING_BOUNDS.minRamMB && form.ram_mb <= SIZING_BOUNDS.maxRamMB &&
    form.disk_gb >= 1 && form.disk_gb <= SIZING_BOUNDS.maxDiskGB;
  // Every step now validates before Next, so users can't skip past required input.
  const currentValid =
    step === 0 ? identityValid :
    step === 1 ? placementValid :
    sizingValid;
  const canSubmit = orgId.length > 0 && identityValid && !hostCapacityProblem && sizingValid;
  const nextStep = () => setStep((current) => Math.min(current + 1, wizardSteps.length - 1));
  const previousStep = () => setStep((current) => Math.max(current - 1, 0));

  return (
    <div className="grid grid-cols-[minmax(0,1fr)_360px] gap-6 max-xl:grid-cols-1">
      <AppPanel
        eyebrow="Create project"
        title="Provision isolated Supabase stack"
        actions={<StatusPill tone="neutral" label={`${form.profile} · ${form.resource_tier}`} />}
      >
        <p className="mt-2 max-w-2xl text-sm text-muted">
          Name the project, place it on a host, and pick the stack profile and size. supadupa provisions an isolated Supabase stack with its own Postgres volume, network, JWT keys, and routing.
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
            <div className="grid gap-3">
              <div className="grid grid-cols-2 gap-3 max-md:grid-cols-1">
                <Field label="Project name" required hint="Human-friendly display name.">
                  <Input placeholder="My production app" aria-label="Project name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
                </Field>
                <Field label="Project ref" required hint="Lowercase slug used in URLs and the CLI.">
                  <Input className="font-mono" placeholder="my-production-app" aria-label="Project ref" value={form.ref} onChange={(event) => setForm({ ...form, ref: normalizeProjectRef(event.target.value) })} />
                </Field>
              </div>
              <Field label="Base domain" required hint="Projects are exposed at <ref>.<domain>.">
                <Input placeholder="supadupa.test" aria-label="Project domain" value={form.domain} onChange={(event) => setForm({ ...form, domain: event.target.value })} />
              </Field>
              <div className="wizard-review">
                <ReviewRow label="Primary API URL" value={`https://${form.ref || "<ref>"}.${form.domain || "<domain>"}`} />
                <ReviewRow label="Project boundary" value="Dedicated Postgres volume, network, JWT keys, and Supabase services" />
              </div>
            </div>
          ) : null}

          {step === 1 ? (
            <div className="grid gap-3">
              <Field label="Organization" required hint="Global admins can see all; org and project grants scope everyone else.">
                <NativeSelect aria-label="Organization" value={orgId} onChange={(event) => onSelectOrg(event.target.value)}>
                  <option value="">Select organization</option>
                  {orgs.map((org) => (
                    <option key={org.id} value={org.id}>
                      {org.name}
                    </option>
                  ))}
                </NativeSelect>
              </Field>
              {orgs.length === 0 ? <p className="text-sm text-muted">Create an organization first.</p> : null}
              <Field label="Host placement" hint="Leave on the local runtime for Compose installs; pick a registered host for capacity accounting.">
                <NativeSelect aria-label="Host placement" value={form.host_id} onChange={(event) => setForm({ ...form, host_id: event.target.value })}>
                  <option value="">Default local runtime</option>
                  {hosts.map((host) => (
                    <option key={host.id} value={host.id}>
                      {host.name} · {host.address}
                    </option>
                  ))}
                </NativeSelect>
              </Field>
              {selectedHost ? <HostFitPanel host={selectedHost} reservation={reservation} /> : <PlacementDefaultPanel />}
              {hostCapacityProblem ? <p className="text-sm text-danger">Selected host does not have enough registered capacity for this tier.</p> : null}
            </div>
          ) : null}

          {step === 2 ? (
            <div className="grid gap-4">
              <Field label="Stack version" hint={`Catalog: ${stackReleases.length ? `${stackReleases.length} supported stable releases` : "loading"} · resolves to ${form.stack_version === "latest" ? latestRelease?.version ?? "latest" : selectedRelease?.version ?? form.stack_version}`}>
                <NativeSelect className="font-mono" aria-label="Stack version" value={form.stack_version} onChange={(event) => setForm({ ...form, stack_version: event.target.value })}>
                  <option value="latest">latest{latestRelease ? ` (${latestRelease.version})` : ""}</option>
                  {stackReleases.map((release) => (
                    <option key={release.version} value={release.version}>{release.version}</option>
                  ))}
                </NativeSelect>
              </Field>
              <div>
                <p className="label">Stack profile</p>
                <p className="mt-1 text-xs text-faint">Sets the database engine and a starting service set. Fine-tune individual services below.</p>
                <div className="mt-2 grid grid-cols-3 gap-2 max-lg:grid-cols-1">
                  {STACK_PROFILES.map((profile) => (
                    <button className={form.profile === profile.id ? "choice active min-h-[104px]" : "choice min-h-[104px]"} key={profile.id} onClick={() => chooseProfile(profile.id)} type="button">
                      <span className="flex items-center justify-between gap-2">
                        <span className="text-sm font-medium">{profile.label}</span>
                        <span className="rounded border border-border px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-faint">{profile.engine}</span>
                      </span>
                      <span className="text-xs leading-5 text-muted">{profile.blurb}</span>
                    </button>
                  ))}
                </div>
              </div>
              <div className="rounded-md border border-border bg-bg p-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="label">Services</p>
                  <span className="text-xs text-faint">{enabledServiceCount} of {ALL_SERVICES.length} enabled</span>
                </div>
                <p className="mt-1 text-xs leading-5 text-muted">Each service runs as its own container in this project's isolated stack. The profile seeds these — toggle any of them. The database is always included.</p>
                <div className="mt-3 grid grid-cols-2 gap-2 max-md:grid-cols-1">
                  {PROJECT_SERVICES.map((service) => {
                    // Imgproxy is gated on Storage (it transforms Storage objects).
                    const requiresStorage = service.key === "imgproxy";
                    const blocked = requiresStorage && !form.services.storage;
                    const enabled = Boolean(form.services[service.key]) && !blocked;
                    return (
                      <label className={`${enabled ? "choice active" : "choice"} items-start text-left ${blocked ? "opacity-50" : ""}`} key={service.key}>
                        <span className="flex w-full items-start gap-2">
                          <input type="checkbox" className="mt-1" checked={enabled} disabled={blocked} onChange={() => toggleService(service.key)} aria-label={service.label} />
                          <span className="grid gap-0.5">
                            <span className="text-sm font-medium">{service.label}</span>
                            <span className="text-xs leading-5 text-muted">{service.description}{blocked ? " Requires Storage." : ""}</span>
                          </span>
                        </span>
                      </label>
                    );
                  })}
                </div>
                {serviceWarnings.length ? (
                  <ul className="mt-3 grid gap-1">
                    {serviceWarnings.map((warning) => (
                      <li className="text-xs leading-5 text-warning" key={warning}>⚠ {warning}</li>
                    ))}
                  </ul>
                ) : null}
              </div>
              <div>
                <p className="label">Resource tier preset</p>
                <p className="mt-1 text-xs text-faint">Pick a preset to set sensible numbers, then fine-tune the exact size below.</p>
                <div className="mt-2 grid grid-cols-3 gap-2 max-lg:grid-cols-1">
                  {(["small", "medium", "large"] as const).map((resourceTier) => {
                    const tierReservation = reservationForTier(resourceTier);
                    return (
                      <button className={form.resource_tier === resourceTier ? "choice active" : "choice"} key={resourceTier} onClick={() => chooseTier(resourceTier)} type="button">
                        <span className="text-sm font-medium capitalize">{resourceTier}</span>
                        <span className="text-xs text-faint">{tierReservation.cpu} vCPU · {formatBytes(tierReservation.ram_mb * 1024 * 1024)} · {tierReservation.disk_gb} GB disk</span>
                      </button>
                    );
                  })}
                </div>
              </div>
              <div className="rounded-md border border-border bg-bg p-3">
                <p className="label">Exact size</p>
                <p className="mt-1 text-xs leading-5 text-muted">Prefilled from the {form.resource_tier} preset — edit any field to set an exact CPU / RAM / disk size for this project.</p>
                <div className="mt-3 grid grid-cols-3 gap-2 max-md:grid-cols-1">
                  <Field label="CPU (cores)" hint={`1 – ${SIZING_BOUNDS.maxCpu}`}>
                    <Input className="font-mono" type="number" min={1} max={SIZING_BOUNDS.maxCpu} aria-label="CPU cores" value={form.cpu} onChange={(event) => setForm({ ...form, cpu: Number(event.target.value) })} />
                  </Field>
                  <Field label="RAM (MB)" hint={`${SIZING_BOUNDS.minRamMB} – ${SIZING_BOUNDS.maxRamMB}`}>
                    <Input className="font-mono" type="number" min={SIZING_BOUNDS.minRamMB} max={SIZING_BOUNDS.maxRamMB} step={256} aria-label="RAM in MB" value={form.ram_mb} onChange={(event) => setForm({ ...form, ram_mb: Number(event.target.value) })} />
                  </Field>
                  <Field label="Disk (GB)" hint={`1 – ${SIZING_BOUNDS.maxDiskGB}`}>
                    <Input className="font-mono" type="number" min={1} max={SIZING_BOUNDS.maxDiskGB} aria-label="Disk in GB" value={form.disk_gb} onChange={(event) => setForm({ ...form, disk_gb: Number(event.target.value) })} />
                  </Field>
                </div>
                {!sizingValid ? <p className="mt-2 text-xs text-danger">Sizing is out of range. CPU 1–{SIZING_BOUNDS.maxCpu} cores, RAM {SIZING_BOUNDS.minRamMB}–{SIZING_BOUNDS.maxRamMB} MB, disk 1–{SIZING_BOUNDS.maxDiskGB} GB.</p> : null}
              </div>
              <div className="rounded-md border border-border bg-bg p-3">
                <label className="flex cursor-pointer items-start gap-3">
                  <input type="checkbox" className="mt-1" checked={form.enforce_limits} onChange={(event) => setForm({ ...form, enforce_limits: event.target.checked })} aria-label="Enforce runtime limits" />
                  <span className="grid gap-1">
                    <span className="text-sm font-medium">Enforce limits on the database container</span>
                    <span className="text-xs leading-5 text-muted">When on, the project's Postgres container gets hard CPU/memory limits ({reservation.cpu} vCPU · {formatBytes(reservation.ram_mb * 1024 * 1024)}). When off, the size is used for placement and quota accounting only.</span>
                  </span>
                </label>
              </div>
            </div>
          ) : null}

          <div className="flex gap-2 max-sm:flex-col">
            <Button className="justify-center" disabled={step === 0 || mutation.isPending} onClick={previousStep} type="button" variant="secondary">
              Back
            </Button>
            {step < wizardSteps.length - 1 ? (
              <Button className="justify-center" disabled={!currentValid || mutation.isPending} onClick={nextStep} type="button">
                Next
              </Button>
            ) : (
              <Button className="justify-center" disabled={!canSubmit || mutation.isPending} type="submit">
                <Plus size={14} />
                Create project
              </Button>
            )}
          </div>
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </form>
      </AppPanel>
      <CreatePlanPanel form={form} host={selectedHost} org={selectedOrg} reservation={reservation} stackReleases={stackReleases} />
    </div>
  );
}

function CreatePlanPanel({
  form,
  host,
  org,
  reservation,
  stackReleases,
}: {
  form: CreateProjectForm;
  host?: Host;
  org?: Org;
  reservation: HostCapacity;
  stackReleases: StackReleaseManifest[];
}) {
  const latestRelease = stackReleases[0];
  const enabledServices = PROJECT_SERVICES.filter((service) => form.services[service.key]).map((service) => service.label);
  return (
    <aside className="grid content-start gap-3">
      <AppPanel
        eyebrow="Provisioning plan"
        title={form.name || "New project"}
        actions={<StatusPill tone="neutral" label={form.resource_tier} />}
      >
        <div className="mt-4 grid gap-3">
          <div className="grid grid-cols-2 gap-2">
            <MetricCard label="vCPU" value={`${reservation.cpu}`} />
            <MetricCard label="RAM" value={formatBytes(reservation.ram_mb * 1024 * 1024)} />
            <MetricCard label="Disk" value={`${reservation.disk_gb} GB`} />
          </div>
          <ResourceSummary host={host} reservation={reservation} />
          <div className="wizard-review">
            <ReviewRow label="Org" value={org?.name ?? "Select organization"} />
            <ReviewRow label="Host" value={host ? host.name : "Default local runtime"} />
            <ReviewRow label="Stack" value={form.stack_version === "latest" ? `latest (${latestRelease?.version ?? "default"})` : form.stack_version} />
            <ReviewRow label="API URL" value={`https://${form.ref || "<ref>"}.${form.domain || "<domain>"}`} />
            <ReviewRow label="Engine" value={form.profile === "orioledb" ? "OrioleDB (preview)" : "Postgres"} />
            <ReviewRow label="Services" value={`${enabledServices.length}/${ALL_SERVICES.length}: ${enabledServices.join(", ") || "none"}`} />
          </div>
        </div>
      </AppPanel>
    </aside>
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
        <StatusPill tone={fits ? "success" : "danger"} label={fits ? "capacity ok" : "capacity short"} />
      </div>
      <div className="grid gap-2">
        <ResourceBar label="CPU after create" value={host.used.cpu + reservation.cpu} total={host.capacity.cpu} suffix="vCPU" />
        <ResourceBar label="RAM after create" value={host.used.ram_mb + reservation.ram_mb} total={host.capacity.ram_mb} format={(value) => formatBytes(value * 1024 * 1024)} />
        <ResourceBar label="Disk after create" value={host.used.disk_gb + reservation.disk_gb} total={host.capacity.disk_gb} suffix="GB" />
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
