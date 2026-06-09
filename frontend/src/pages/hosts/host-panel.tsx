import type { FormEvent, ReactNode } from "react";
import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowLeft, Plus, Save, Trash2 } from "lucide-react";
import { createHost, deleteHost } from "../../api";
import { AppPanel } from "../../components/app/app-panel";
import { DataTable } from "../../components/data-table";
import { Modal } from "../../components/modal";
import { Button } from "../../components/ui/button";
import { Field } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { StatusPill } from "../../components/ui/status-pill";
import { formatBytes, formatDateTime } from "../../lib/format";
import { type Tone } from "../../lib/status";
import type { Host, HostCapacity, NodeTelemetrySample } from "../../types";

type HostPanelProps = {
  hosts: Host[];
  item?: string;
  loading: boolean;
  nodeObserved?: NodeTelemetrySample[];
  provisioner?: string;
  scope?: "hosts" | "settings";
};

export function HostPanel({ hosts, item, loading, nodeObserved = [], provisioner, scope = "hosts" }: HostPanelProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const isCompose = provisioner === "compose";
  const [pendingDelete, setPendingDelete] = useState<Host | null>(null);
  const observedByHost = useMemo(() => {
    const map = new Map<string, NodeTelemetrySample>();
    for (const sample of nodeObserved) {
      map.set(sample.host_id, sample);
    }
    return map;
  }, [nodeObserved]);
  const [form, setForm] = useState({
    name: "local",
    address: "localhost",
    projects: 20,
    cpu: 8,
    ram_mb: 32768,
    disk_gb: 500,
  });
  const mutation = useMutation({
    mutationFn: createHost,
    onSuccess: () => {
      closeCreate();
      void queryClient.invalidateQueries({ queryKey: ["hosts"] });
      void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: deleteHost,
    onSuccess: () => {
      setPendingDelete(null);
      void queryClient.invalidateQueries({ queryKey: ["hosts"] });
      void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  const creating = item === "new";
  const canCreate = form.name.trim().length > 0 &&
    form.address.trim().length > 0 &&
    form.projects >= 0 &&
    form.cpu >= 0 &&
    form.ram_mb >= 0 &&
    form.disk_gb >= 0;
  const hostColumns = useMemo<ColumnDef<Host>[]>(
    () => [
      {
        header: "Host",
        accessorKey: "name",
        size: 220,
        cell: ({ row }) => (
          <>
            <p className="cell-main truncate">{row.original.name}</p>
            <p className="cell-sub truncate font-mono">{row.original.address}</p>
            <p className="cell-sub truncate font-mono">{row.original.id}</p>
          </>
        ),
      },
      {
        header: "Projects",
        id: "projects",
        size: 120,
        cell: ({ row }) => <CapacityCell used={row.original.used.projects} capacity={row.original.capacity.projects} />,
      },
      {
        header: "CPU (reserved · observed)",
        id: "cpu",
        size: 180,
        cell: ({ row }) => {
          const observed = observedByHost.get(row.original.id);
          return <CapacityCell used={row.original.used.cpu} capacity={row.original.capacity.cpu} observed={observed ? `${observed.cpu_percent.toFixed(0)}% node CPU` : undefined} />;
        },
      },
      {
        header: "RAM (reserved · observed)",
        id: "ram",
        size: 200,
        cell: ({ row }) => {
          const observed = observedByHost.get(row.original.id);
          const observedLabel = observed && observed.memory_total_bytes > 0 ? `${formatBytes(observed.memory_used_bytes)} node used` : undefined;
          return <CapacityCell used={formatBytes(row.original.used.ram_mb * 1024 * 1024)} usedRatio={ratio(row.original.used.ram_mb, row.original.capacity.ram_mb)} capacity={row.original.capacity.ram_mb} capacityLabel={row.original.capacity.ram_mb ? formatBytes(row.original.capacity.ram_mb * 1024 * 1024) : "-"} observed={observedLabel} />;
        },
      },
      {
        header: "Disk (reserved · observed)",
        id: "disk",
        size: 190,
        cell: ({ row }) => {
          const observed = observedByHost.get(row.original.id);
          const observedLabel = observed && observed.disk_total_bytes > 0 ? `${formatBytes(observed.disk_used_bytes)} node used` : undefined;
          return <CapacityCell used={`${row.original.used.disk_gb} GB`} usedRatio={ratio(row.original.used.disk_gb, row.original.capacity.disk_gb)} capacity={row.original.capacity.disk_gb} capacityLabel={row.original.capacity.disk_gb ? `${row.original.capacity.disk_gb} GB` : "-"} observed={observedLabel} />;
        },
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
        size: 52,
        cell: ({ row }) => (
          <Button variant="ghost" size="icon" disabled={deleteMutation.isPending} onClick={() => setPendingDelete(row.original)} title="Delete host" type="button">
            <Trash2 size={14} />
          </Button>
        ),
      },
    ],
    [deleteMutation.isPending, observedByHost],
  );

  function submit(event: FormEvent) {
    event.preventDefault();
    mutation.mutate({
      name: form.name.trim(),
      address: form.address.trim(),
      capacity: {
        projects: form.projects,
        cpu: form.cpu,
        ram_mb: form.ram_mb,
        disk_gb: form.disk_gb,
      },
    });
  }

  function openCreate() {
    if (scope === "settings") {
      void navigate({ to: "/settings/$section/$item", params: { section: "hosts", item: "new" } });
      return;
    }
    void navigate({ to: "/hosts/$item", params: { item: "new" } });
  }

  function closeCreate() {
    if (scope === "settings") {
      void navigate({ to: "/settings/$section", params: { section: "hosts" } });
      return;
    }
    void navigate({ to: "/hosts" });
  }

  return (
    <AppPanel
      actions={
        creating ? (
          <Button variant="ghost" size="icon" onClick={closeCreate} title="Back to hosts" type="button">
            <ArrowLeft size={14} />
          </Button>
        ) : isCompose && hosts.length > 0 ? null : (
          <Button variant="ghost" size="icon" onClick={openCreate} title="Register host" type="button">
            <Plus size={14} />
          </Button>
        )
      }
      description={creating ? (isCompose ? "Compose deploys to one local Docker node. Capacity is the budget projects schedule against — not a second machine." : "Add an operator-managed capacity source for future project placement.") : isCompose ? "Single-node Compose runtime. Reserved is committed to projects; observed is live node load." : "Reserved is committed to project placement; observed is live node telemetry where sampled."}
      eyebrow="Hosts"
      title={creating ? "Register host" : isCompose ? "Local Docker capacity" : "Fleet capacity"}
    >
      {creating ? (
        <form className="mt-4 grid gap-3" onSubmit={submit}>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Host identity</p>
              <p className="truncate text-xs text-muted">{isCompose ? "Local Compose installs use the local Docker node." : "Use a reachable address for future remote provisioners; local installs can use localhost."}</p>
            </div>
            <StatusPill tone="neutral" label={`${hosts.length} registered`} />
          </div>
          <div className="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
            <Field label="Name" hint="A short identifier for this capacity source.">
              <Input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            </Field>
            <Field label="Address" hint="Hostname or IP; localhost for local installs.">
              <Input className="font-mono" value={form.address} onChange={(event) => setForm({ ...form, address: event.target.value })} />
            </Field>
            <Field label="Project capacity" hint="Max concurrent projects scheduled here.">
              <Input className="font-mono" min={0} type="number" value={form.projects} onChange={(event) => setForm({ ...form, projects: Number(event.target.value) })} />
            </Field>
            <Field label="CPU" hint="vCPU budget available to projects.">
              <Input className="font-mono" min={0} type="number" value={form.cpu} onChange={(event) => setForm({ ...form, cpu: Number(event.target.value) })} />
            </Field>
            <Field label="RAM" hint="Megabytes (MB). 32768 = 32 GB.">
              <Input className="font-mono" min={0} type="number" value={form.ram_mb} onChange={(event) => setForm({ ...form, ram_mb: Number(event.target.value) })} />
            </Field>
            <Field label="Disk" hint="Gigabytes (GB) of reservable storage.">
              <Input className="font-mono" min={0} type="number" value={form.disk_gb} onChange={(event) => setForm({ ...form, disk_gb: Number(event.target.value) })} />
            </Field>
          </div>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Registered capacity</p>
              <p className="truncate text-xs text-muted">{capacitySummary(form)}</p>
            </div>
            <div className="flex items-center gap-2">
              <Button variant="secondary" onClick={closeCreate} type="button">
                Cancel
              </Button>
              <Button disabled={!canCreate || mutation.isPending} type="submit">
                <Save size={14} />
                Register
              </Button>
            </div>
          </div>
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </form>
      ) : (
        <div className="mt-4 grid gap-2">
          {loading ? <p className="text-sm text-muted">Loading hosts...</p> : null}
          <DataTable columns={hostColumns} data={hosts} emptyText="No hosts registered yet. Projects can still use the default local runtime." minWidth={1140} rowClassName={(host) => rowToneClass(host)} />
          {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
        </div>
      )}

      <Modal
        description={pendingDelete ? `Remove ${pendingDelete.name} from the fleet.` : undefined}
        onClose={() => !deleteMutation.isPending && setPendingDelete(null)}
        open={Boolean(pendingDelete)}
        title="Delete host"
        footer={(
          <>
            <Button variant="secondary" disabled={deleteMutation.isPending} onClick={() => setPendingDelete(null)} type="button">Cancel</Button>
            <Button variant="danger" disabled={deleteMutation.isPending} onClick={() => pendingDelete && deleteMutation.mutate(pendingDelete.id)} type="button">
              {deleteMutation.isPending ? "Deleting..." : "Delete host"}
            </Button>
          </>
        )}
      >
        {pendingDelete ? (
          <div className="grid gap-2 text-sm text-muted">
            <p>This unregisters <span className="font-medium text-text">{pendingDelete.name}</span> ({pendingDelete.address}) and frees its reserved capacity:</p>
            <p className="font-mono text-xs">{capacitySummary(pendingDelete.capacity)}</p>
            {pendingDelete.used.projects > 0 ? <p className="text-warning">{pendingDelete.used.projects} project slot{pendingDelete.used.projects === 1 ? "" : "s"} are currently reserved here — confirm placements are migrated first.</p> : null}
          </div>
        ) : null}
      </Modal>
    </AppPanel>
  );
}

function ratio(used: number, capacity: number) {
  return capacity > 0 ? (used / capacity) * 100 : 0;
}

function utilizationTone(percent: number, capacity: number): Tone {
  if (capacity <= 0) return "neutral";
  if (percent >= 100) return "danger";
  if (percent >= 85) return "warning";
  return "success";
}

function toneText(tone: Tone) {
  switch (tone) {
    case "danger":
      return "text-danger";
    case "warning":
      return "text-warning";
    default:
      return "text-muted";
  }
}

function CapacityCell({ capacity, capacityLabel, observed, used, usedRatio }: { used: ReactNode; usedRatio?: number; capacity: number; capacityLabel?: ReactNode; observed?: ReactNode }) {
  const percent = usedRatio ?? (typeof used === "number" ? ratio(used, capacity) : 0);
  const tone = utilizationTone(percent, capacity);
  return (
    <div className="min-w-0">
      <p className="cell-main truncate">{used}/{capacityLabel ?? (capacity || "-")}</p>
      <p className={`cell-sub truncate ${toneText(tone)}`}>{capacity > 0 ? `${percent.toFixed(0)}%${percent >= 100 ? " · over" : percent >= 85 ? " · near limit" : ""}` : "no limit"}</p>
      {observed ? <p className="cell-sub truncate text-faint">{observed}</p> : null}
    </div>
  );
}

function rowToneClass(host: Host) {
  const percents = [
    ratio(host.used.cpu, host.capacity.cpu),
    ratio(host.used.ram_mb, host.capacity.ram_mb),
    ratio(host.used.disk_gb, host.capacity.disk_gb),
    ratio(host.used.projects, host.capacity.projects),
  ];
  const peak = Math.max(...percents);
  if (peak >= 100) return "table-row-error";
  if (peak >= 85) return "table-row-warning";
  return "";
}

function capacitySummary(capacity: HostCapacity) {
  return `${capacity.projects} projects · ${capacity.cpu} CPU · ${formatBytes(capacity.ram_mb * 1024 * 1024)} RAM · ${capacity.disk_gb} GB disk`;
}
