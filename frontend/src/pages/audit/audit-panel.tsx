import { useMemo } from "react";
import type { ColumnDef } from "@tanstack/react-table";
import { DataTable } from "../../components/data-table";
import type { AuditEvent, AuditIntegrity } from "../../types";
import { formatTime } from "../../lib/format";

export function AuditPanel({ events, integrity, loading, maxEvents }: { events: AuditEvent[]; integrity?: AuditIntegrity; loading: boolean; maxEvents?: number }) {
  const visibleEvents = typeof maxEvents === "number" ? events.slice(0, maxEvents) : events;
  const columns = useMemo<ColumnDef<AuditEvent>[]>(
    () => [
      {
        header: "Action",
        accessorKey: "action",
        size: 220,
        cell: ({ row }) => <p className="cell-main truncate">{row.original.action}</p>,
      },
      {
        header: "Target",
        accessorKey: "target",
        size: 320,
        cell: ({ row }) => <p className="truncate font-mono text-xs text-muted">{row.original.target}</p>,
      },
      {
        header: "Index",
        accessorKey: "chain_index",
        size: 100,
        cell: ({ row }) => <p className="font-mono text-xs text-muted">#{row.original.chain_index}</p>,
      },
      {
        header: "Time",
        accessorKey: "created_at",
        size: 120,
        cell: ({ row }) => <time className="text-xs text-faint">{formatTime(row.original.created_at)}</time>,
      },
    ],
    [],
  );

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Audit log</p>
          <h2>Control-plane activity</h2>
        </div>
        {integrity ? (
          <span className={`pill ${integrity.verified ? "healthy" : "error"}`}>
            {integrity.verified ? "verified" : `broken #${integrity.broken_at ?? "?"}`}
          </span>
        ) : null}
      </div>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading audit events...</p> : null}
        {integrity ? (
          <div className="audit-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{integrity.events} sealed events</p>
              <p className="truncate font-mono text-xs text-muted">{integrity.head_hash || "genesis"}</p>
            </div>
            <time className="text-xs text-faint">{formatTime(integrity.checked_at)}</time>
          </div>
        ) : null}
        <DataTable columns={columns} data={visibleEvents} emptyText="No events recorded yet." minWidth={760} />
        {typeof maxEvents === "number" && events.length > maxEvents ? <p className="text-xs text-faint">Showing {maxEvents} of {events.length} events.</p> : null}
      </div>
    </section>
  );
}
