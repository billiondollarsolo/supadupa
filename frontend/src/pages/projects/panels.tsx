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
import { DbExposureBadge, dbExposureMeta } from "../../components/db-exposure-badge";
import { formatBytes, formatDateTime } from "../../lib/format";
import { isValidProjectRef } from "../../lib/validators";
import type { Host, HostCapacity, Org, PlatformDefaults, Project, StackReleaseManifest } from "../../types";

type StackProfile = "essential" | "full" | "orioledb";

type CreateProjectForm = {
  ref: string;
  name: string;
  host_id: string;
  domain: string;
  stack_version: string;
  profile: StackProfile;
  cpu: number;
  ram_mb: number;
  disk_gb: number;
  // Applies real per-container CPU/memory limits across enabled services.
  enforce_limits: boolean;
  // Per-service enable map (keys = ALL_SERVICES). The profile seeds this; the
  // user can then toggle any service individually.
  services: Record<string, boolean>;
};

// Platform sizing bounds (must mirror the control-plane validateResourceSizing).
const SIZING_BOUNDS = { maxCpu: 64, minRamMB: 256, maxRamMB: 262144, maxDiskGB: 16384 } as const;
const RECOMMENDATION_HEADROOM = { cpuPercent: 20, ramPercent: 25, diskPercent: 20, ramStepMB: 256, diskStepGB: 5 } as const;

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
      cell: (info) => {
        const exposure = dbExposureMeta(info.row.original.db_ingress_mode);
        return (
          <span className="flex min-w-0 flex-col gap-1">
            <span className="truncate font-medium">{info.row.original.name}</span>
            {exposure ? <DbExposureBadge className="self-start" mode={info.row.original.db_ingress_mode} /> : null}
          </span>
        );
      },
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
        <ResourceBar label={host ? "CPU reserved / host" : "CPU"} value={reservation.cpu} total={host?.capacity.cpu} reference={RESOURCE_REFERENCE.cpu} suffix="vCPU" />
        <ResourceBar label={host ? "RAM reserved / host" : "RAM"} value={reservation.ram_mb} total={host?.capacity.ram_mb} reference={RESOURCE_REFERENCE.ram_mb} format={(value) => formatBytes(value * 1024 * 1024)} />
        <ResourceBar label={host ? "Disk reserved / host" : "Disk"} value={reservation.disk_gb} total={host?.capacity.disk_gb} reference={RESOURCE_REFERENCE.disk_gb} suffix="GB" />
      </div>
      <p className="mt-2 truncate text-xs text-faint">
        {host ? `Host total reserved: ${host.used.cpu}/${host.capacity.cpu || "-"} vCPU · ${formatBytes(host.used.ram_mb * 1024 * 1024)} RAM` : "No host selected; bars are scaled to a reference allocation for comparison."}
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
  // the chosen size (scaled against a reference allocation for comparison).
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

// Comparison scale for plan-panel bars when no host is selected.
const RESOURCE_REFERENCE: HostCapacity = { cpu: 8, ram_mb: 16384, disk_gb: 160, projects: 1 };

function minimumReservation(profile: StackProfile, services: Record<string, boolean>): HostCapacity {
  let ramMB = 2048;
  let cpuUnits = 100;
  let diskGB = 20;
  if (services.auth) {
    ramMB += 256;
    cpuUnits += 20;
  }
  if (services.rest) {
    ramMB += 256;
    cpuUnits += 20;
  }
  if (services.realtime) {
    ramMB += 512;
    cpuUnits += 30;
  }
  if (services.storage) {
    ramMB += 512;
    cpuUnits += 30;
    diskGB += 20;
  }
  if (services.imgproxy) {
    ramMB += 256;
    cpuUnits += 20;
  }
  if (services.functions) {
    ramMB += 512;
    cpuUnits += 20;
  }
  if (services.pooler) {
    ramMB += 256;
    cpuUnits += 10;
  }
  if (services.studio) {
    ramMB += 512;
    cpuUnits += 10;
  }
  if (services.analytics) {
    ramMB += 1024;
    cpuUnits += 30;
    diskGB += 10;
  }
  if (services.vector) {
    ramMB += 256;
    cpuUnits += 10;
  }
  if (services.graphql) {
    ramMB += 128;
    cpuUnits += 10;
  }
  if (profile === "orioledb") {
    ramMB += 1024;
    cpuUnits += 50;
    diskGB += 20;
  }
  if (ramMB % 512 !== 0) {
    ramMB = (Math.floor(ramMB / 512) + 1) * 512;
  }
  return { cpu: Math.max(1, Math.ceil(cpuUnits / 100)), ram_mb: ramMB, disk_gb: Math.max(20, diskGB), projects: 1 };
}

function recommendedReservation(profile: StackProfile, services: Record<string, boolean>): HostCapacity {
  const minimum = minimumReservation(profile, services);
  return {
    cpu: clampNumber(addPercentRoundUp(minimum.cpu, RECOMMENDATION_HEADROOM.cpuPercent), 1, SIZING_BOUNDS.maxCpu),
    ram_mb: clampNumber(roundUpNumber(addPercentRoundUp(minimum.ram_mb, RECOMMENDATION_HEADROOM.ramPercent), RECOMMENDATION_HEADROOM.ramStepMB), SIZING_BOUNDS.minRamMB, SIZING_BOUNDS.maxRamMB),
    disk_gb: clampNumber(roundUpNumber(addPercentRoundUp(minimum.disk_gb, RECOMMENDATION_HEADROOM.diskPercent), RECOMMENDATION_HEADROOM.diskStepGB), 1, SIZING_BOUNDS.maxDiskGB),
    projects: minimum.projects,
  };
}

function addPercentRoundUp(value: number, percent: number) {
  return Math.ceil(value * (100 + percent) / 100);
}

function roundUpNumber(value: number, step: number) {
  return Math.ceil(value / step) * step;
}

function clampNumber(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function applyReservation(form: CreateProjectForm, reservation: HostCapacity): CreateProjectForm {
  return { ...form, cpu: reservation.cpu, ram_mb: reservation.ram_mb, disk_gb: reservation.disk_gb };
}

function effectiveReservation(form: CreateProjectForm): HostCapacity {
  return {
    cpu: form.cpu,
    ram_mb: form.ram_mb,
    disk_gb: form.disk_gb,
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
  const [form, setForm] = useState<CreateProjectForm>(() => {
    const profile: StackProfile = "full";
    const services = servicesForProfile(profile);
    const recommendation = recommendedReservation(profile, services);
    return {
      ref: "",
      name: "",
      host_id: "",
      domain: "supadupa.test",
      stack_version: "latest",
      profile,
      cpu: recommendation.cpu,
      ram_mb: recommendation.ram_mb,
      disk_gb: recommendation.disk_gb,
      enforce_limits: true,
      services,
    };
  });
  useEffect(() => {
    if (!defaults) return;
    const profile: StackProfile = defaults.profile === "essential" || defaults.profile === "orioledb" ? defaults.profile : "full";
    const services = servicesForProfile(profile);
    const recommendation = recommendedReservation(profile, services);
    setForm((current) => ({
      ...current,
      domain: defaults.domain || current.domain,
      stack_version: defaults.stack_version || current.stack_version,
      profile,
      cpu: recommendation.cpu,
      ram_mb: recommendation.ram_mb,
      disk_gb: recommendation.disk_gb,
      services,
    }));
  }, [defaults]);
  const mutation = useMutation({
    mutationFn: createProject,
    onSuccess: onCreated,
  });

  // Picking a profile re-seeds the service selection from that profile's
  // template; the user can still toggle individual services afterward.
  function chooseProfile(profile: StackProfile) {
    setForm((current) => {
      const services = servicesForProfile(profile);
      return applyReservation({ ...current, profile, services }, recommendedReservation(profile, services));
    });
  }

  function toggleService(key: string) {
    setForm((current) => {
      const services = { ...current.services, [key]: !current.services[key] };
      // Hard dependency: Imgproxy only transforms Storage objects, so it can't
      // run without Storage. Turning Storage off also turns Imgproxy off.
      if (key === "storage" && !services.storage) services.imgproxy = false;
      return applyReservation({ ...current, services }, recommendedReservation(current.profile, services));
    });
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
  const refValid = isValidProjectRef(form.ref);
  const refHintInvalid = form.ref.trim().length > 0 && !refValid;
  const identityValid = form.name.trim().length > 0 && refValid && form.domain.trim().length > 0;
  const placementValid = orgId.length > 0 && !hostCapacityProblem;
  const sizingValid =
    form.cpu >= 1 && form.cpu <= SIZING_BOUNDS.maxCpu &&
    form.ram_mb >= SIZING_BOUNDS.minRamMB && form.ram_mb <= SIZING_BOUNDS.maxRamMB &&
    form.disk_gb >= 1 && form.disk_gb <= SIZING_BOUNDS.maxDiskGB;
  const minimum = minimumReservation(form.profile, form.services);
  const recommendation = recommendedReservation(form.profile, form.services);
  const belowRecommendation = form.cpu < recommendation.cpu || form.ram_mb < recommendation.ram_mb || form.disk_gb < recommendation.disk_gb;
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
	        actions={<StatusPill tone="neutral" label={`${form.profile} · ${reservation.cpu} vCPU`} />}
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
                <Field
                  label="Project ref"
                  required
                  hint={refHintInvalid
                    ? "3–55 lowercase letters, numbers, hyphens; cannot start or end with a hyphen."
                    : "Lowercase slug used in URLs and the CLI."}
                >
                  <Input
                    className="font-mono"
                    placeholder="my-production-app"
                    aria-label="Project ref"
                    aria-invalid={refHintInvalid || undefined}
                    value={form.ref}
                    onChange={(event) => setForm({ ...form, ref: normalizeProjectRef(event.target.value) })}
                  />
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
	              {hostCapacityProblem ? <p className="text-sm text-danger">Selected host does not have enough registered capacity for these resources.</p> : null}
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
	              <div className="rounded-md border border-border bg-bg p-3">
	                <div className="flex items-start justify-between gap-3">
	                  <div>
		                    <p className="label">Recommended size</p>
		                    <p className="mt-1 text-xs leading-5 text-muted">{recommendation.cpu} vCPU · {formatBytes(recommendation.ram_mb * 1024 * 1024)} RAM · {recommendation.disk_gb} GB disk with operating headroom.</p>
		                    <p className="mt-1 text-xs leading-5 text-faint">Minimum: {minimum.cpu} vCPU · {formatBytes(minimum.ram_mb * 1024 * 1024)} RAM · {minimum.disk_gb} GB disk.</p>
		                  </div>
		                  <div className="flex shrink-0 gap-2 max-sm:flex-col">
		                    <Button size="sm" type="button" variant="secondary" onClick={() => setForm((current) => applyReservation(current, recommendation))}>
		                      Use recommended
		                    </Button>
		                    <Button size="sm" type="button" variant="secondary" onClick={() => setForm((current) => applyReservation(current, minimum))}>
		                      Use minimum
		                    </Button>
		                  </div>
	                </div>
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
		                {belowRecommendation ? <p className="mt-2 text-xs text-warning">Below the recommended size. The minimum is a startup floor; enforced limits can become unstable under real traffic.</p> : null}
	              </div>
	              <div className="rounded-md border border-border bg-bg p-3">
	                <p className="label">CPU / RAM limits</p>
	                <div className="mt-2 grid grid-cols-2 gap-2 max-md:grid-cols-1">
	                  <button className={form.enforce_limits ? "choice active" : "choice"} type="button" onClick={() => setForm({ ...form, enforce_limits: true })}>
	                    <span className="text-sm font-medium">Enforce limits</span>
	                    <span className="text-xs leading-5 text-muted">Distribute {reservation.cpu} vCPU and {formatBytes(reservation.ram_mb * 1024 * 1024)} RAM limits across enabled service containers.</span>
	                  </button>
	                  <button className={!form.enforce_limits ? "choice active" : "choice"} type="button" onClick={() => setForm({ ...form, enforce_limits: false })}>
	                    <span className="text-sm font-medium">No limits</span>
	                    <span className="text-xs leading-5 text-muted">Allow project containers to grow past the selected CPU/RAM and contend for all host resources.</span>
	                  </button>
	                </div>
	                {!form.enforce_limits ? <p className="mt-2 text-xs text-warning">No limits is your choice: this can improve burst behavior, but a busy project can consume the host and affect other local workloads.</p> : null}
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
        actions={<StatusPill tone={form.enforce_limits ? "neutral" : "warning"} label={form.enforce_limits ? "limits on" : "no limits"} />}
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
