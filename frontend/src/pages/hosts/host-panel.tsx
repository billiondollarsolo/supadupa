import type { FormEvent } from "react";
import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowLeft, Plus, Save, Trash2 } from "lucide-react";
import { createHost, deleteHost } from "../../api";
import { DataTable } from "../../components/data-table";
import { formatBytes, formatDateTime } from "../../lib/format";
import type { Host, HostCapacity } from "../../types";

type HostPanelProps = {
  hosts: Host[];
  item?: string;
  loading: boolean;
  scope?: "hosts" | "settings";
};

export function HostPanel({ hosts, item, loading, scope = "hosts" }: HostPanelProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    name: "local",
    address: "localhost",
    projects: 20,
    cpu: 8,
    ram_mb: 32768,
    disk_gb: 500,
    disk_iops: 48000,
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
    form.disk_gb >= 0 &&
    form.disk_iops >= 0;
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
        size: 110,
        cell: ({ row }) => capacityValue(row.original.used.projects, row.original.capacity.projects),
      },
      {
        header: "CPU",
        id: "cpu",
        size: 110,
        cell: ({ row }) => capacityValue(row.original.used.cpu, row.original.capacity.cpu),
      },
      {
        header: "RAM",
        id: "ram",
        size: 150,
        cell: ({ row }) => byteCapacity(row.original.used.ram_mb, row.original.capacity.ram_mb),
      },
      {
        header: "Disk",
        id: "disk",
        size: 140,
        cell: ({ row }) => `${row.original.used.disk_gb}/${row.original.capacity.disk_gb || "-"} GB`,
      },
      {
        header: "IOPS",
        id: "iops",
        size: 130,
        cell: ({ row }) => capacityValue(row.original.used.disk_iops, row.original.capacity.disk_iops),
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
          <button className="icon-button" disabled={deleteMutation.isPending} onClick={() => confirmDelete(row.original)} title="Delete host" type="button">
            <Trash2 size={14} />
          </button>
        ),
      },
    ],
    [deleteMutation.isPending],
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
        disk_iops: form.disk_iops,
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

  function confirmDelete(host: Host) {
    const confirmation = window.prompt(`Delete host ${host.name}? Type ${host.name} to continue.`);
    if (confirmation !== host.name) return;
    deleteMutation.mutate(host.id);
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Hosts</p>
          <h2>{creating ? "Register host" : "Fleet capacity"}</h2>
          <p className="mt-1 text-sm text-muted">{creating ? "Add an operator-managed capacity source for future project placement." : "Runtime capacity available to project deployments."}</p>
        </div>
        {creating ? (
          <button className="icon-button" onClick={closeCreate} title="Back to hosts" type="button">
            <ArrowLeft size={14} />
          </button>
        ) : (
          <button className="icon-button" onClick={openCreate} title="Register host" type="button">
            <Plus size={14} />
          </button>
        )}
      </div>

      {creating ? (
        <form className="mt-4 grid gap-3" onSubmit={submit}>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Host identity</p>
              <p className="truncate text-xs text-muted">Use a reachable address for future remote provisioners; local installs can use localhost.</p>
            </div>
            <span className="pill">{hosts.length} registered</span>
          </div>
          <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
            <HostInput label="Name" value={form.name} onChange={(value) => setForm({ ...form, name: value })} />
            <HostInput label="Address" value={form.address} onChange={(value) => setForm({ ...form, address: value })} mono />
            <HostNumberInput label="Project capacity" value={form.projects} onChange={(value) => setForm({ ...form, projects: value })} />
            <HostNumberInput label="CPU" value={form.cpu} onChange={(value) => setForm({ ...form, cpu: value })} />
            <HostNumberInput label="RAM MB" value={form.ram_mb} onChange={(value) => setForm({ ...form, ram_mb: value })} />
            <HostNumberInput label="Disk GB" value={form.disk_gb} onChange={(value) => setForm({ ...form, disk_gb: value })} />
            <HostNumberInput label="Disk IOPS" value={form.disk_iops} onChange={(value) => setForm({ ...form, disk_iops: value })} />
          </div>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Registered capacity</p>
              <p className="truncate text-xs text-muted">{capacitySummary(form)}</p>
            </div>
            <div className="flex items-center gap-2">
              <button className="button secondary justify-center" onClick={closeCreate} type="button">
                Cancel
              </button>
              <button className="button secondary justify-center" disabled={!canCreate || mutation.isPending} type="submit">
                <Save size={14} />
                Register
              </button>
            </div>
          </div>
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </form>
      ) : (
        <div className="mt-4 grid gap-2">
          {loading ? <p className="text-sm text-muted">Loading hosts...</p> : null}
          <DataTable columns={hostColumns} data={hosts} emptyText="No hosts registered yet. Projects can still use the default local runtime." minWidth={1040} />
          {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
        </div>
      )}
    </section>
  );
}

function capacityValue(used: number, capacity: number) {
  return `${used}/${capacity || "-"}`;
}

function byteCapacity(usedMb: number, capacityMb: number) {
  return `${formatBytes(usedMb * 1024 * 1024)}/${capacityMb ? formatBytes(capacityMb * 1024 * 1024) : "-"}`;
}

function capacitySummary(capacity: HostCapacity) {
  return `${capacity.projects} projects · ${capacity.cpu} CPU · ${formatBytes(capacity.ram_mb * 1024 * 1024)} RAM · ${capacity.disk_gb} GB disk · ${capacity.disk_iops} IOPS`;
}

function HostInput({ label, mono = false, onChange, value }: { label: string; value: string; onChange: (value: string) => void; mono?: boolean }) {
  return (
    <label className="grid gap-1">
      <span className="label">{label}</span>
      <input className={`input ${mono ? "font-mono" : ""}`} value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function HostNumberInput({ label, onChange, value }: { label: string; value: number; onChange: (value: number) => void }) {
  return (
    <label className="grid gap-1">
      <span className="label">{label}</span>
      <input className="input font-mono" min={0} type="number" value={value} onChange={(event) => onChange(Number(event.target.value))} />
    </label>
  );
}
