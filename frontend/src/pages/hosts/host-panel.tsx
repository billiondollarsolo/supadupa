import type { FormEvent } from "react";
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, X } from "lucide-react";
import { createHost, deleteHost } from "../../api";
import { formatBytes } from "../../lib/format";
import type { Host } from "../../types";

export function HostPanel({ hosts, loading }: { hosts: Host[]; loading: boolean }) {
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
  const [deleteTarget, setDeleteTarget] = useState("");
  const [deleteConfirmation, setDeleteConfirmation] = useState("");
  const mutation = useMutation({
    mutationFn: createHost,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["hosts"] });
      void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: deleteHost,
    onSuccess: () => {
      setDeleteTarget("");
      setDeleteConfirmation("");
      void queryClient.invalidateQueries({ queryKey: ["hosts"] });
      void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    mutation.mutate({
      name: form.name,
      address: form.address,
      capacity: {
        projects: form.projects,
        cpu: form.cpu,
        ram_mb: form.ram_mb,
        disk_gb: form.disk_gb,
        disk_iops: form.disk_iops,
      },
    });
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Hosts</p>
          <h2>Fleet capacity</h2>
        </div>
      </div>
      <form className="mt-4 grid grid-cols-[repeat(2,minmax(0,1fr))_84px_72px_92px_92px_100px_auto] gap-2 max-[1800px]:grid-cols-4 max-lg:grid-cols-2 max-sm:grid-cols-1" onSubmit={submit}>
        <input className="input" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
        <input className="input" value={form.address} onChange={(event) => setForm({ ...form, address: event.target.value })} />
        <input aria-label="Project capacity" className="input" min={0} type="number" value={form.projects} onChange={(event) => setForm({ ...form, projects: Number(event.target.value) })} />
        <input className="input" min={0} type="number" value={form.cpu} onChange={(event) => setForm({ ...form, cpu: Number(event.target.value) })} />
        <input className="input" min={0} type="number" value={form.ram_mb} onChange={(event) => setForm({ ...form, ram_mb: Number(event.target.value) })} />
        <input className="input" min={0} type="number" value={form.disk_gb} onChange={(event) => setForm({ ...form, disk_gb: Number(event.target.value) })} />
        <input className="input" min={0} type="number" value={form.disk_iops} onChange={(event) => setForm({ ...form, disk_iops: Number(event.target.value) })} />
        <button className="button justify-center" disabled={mutation.isPending} type="submit">
          <Plus size={14} />
          Host
        </button>
      </form>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading hosts...</p> : null}
        {!loading && hosts.length === 0 ? <p className="text-sm text-muted">No hosts registered yet. Projects can still use the default local runtime.</p> : null}
        {hosts.map((host) => (
          <div className="host-row" key={host.id}>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{host.name}</p>
              <p className="truncate font-mono text-xs text-muted">{host.address} · {host.id}</p>
              {deleteTarget === host.id ? (
                <div className="mt-2 grid grid-cols-[minmax(0,1fr)_auto_auto] gap-2 max-sm:grid-cols-1">
                  <input
                    className="input font-mono"
                    placeholder={host.name}
                    value={deleteConfirmation}
                    onChange={(event) => setDeleteConfirmation(event.target.value)}
                  />
                  <button className="button secondary justify-center" onClick={() => { setDeleteTarget(""); setDeleteConfirmation(""); }} type="button">
                    <X size={14} />
                  </button>
                  <button
                    className="button danger justify-center"
                    disabled={deleteMutation.isPending || deleteConfirmation !== host.name}
                    onClick={() => deleteMutation.mutate(host.id)}
                    type="button"
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              ) : null}
            </div>
            <div className="flex items-center justify-end gap-3">
              <div className="text-right text-xs text-muted">
                <p>{host.used.projects}/{host.capacity.projects || "-"} projects</p>
                <p>{host.used.cpu}/{host.capacity.cpu || "-"} CPU</p>
                <p>{formatBytes(host.used.ram_mb * 1024 * 1024)}/{host.capacity.ram_mb ? formatBytes(host.capacity.ram_mb * 1024 * 1024) : "-"} RAM</p>
                <p>{host.used.disk_gb}/{host.capacity.disk_gb || "-"} GB disk</p>
                <p>{host.used.disk_iops}/{host.capacity.disk_iops || "-"} IOPS</p>
              </div>
              <button
                className="icon-button"
                disabled={deleteMutation.isPending}
                onClick={() => { setDeleteTarget(host.id); setDeleteConfirmation(""); }}
                title="Delete host"
                type="button"
              >
                <Trash2 size={14} />
              </button>
            </div>
          </div>
        ))}
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </section>
  );
}
