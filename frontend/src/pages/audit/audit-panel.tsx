import { Fragment, useMemo, useState } from "react";
import type { ColumnDef } from "@tanstack/react-table";
import { ChevronDown, ChevronRight, Search } from "lucide-react";
import { DataTable } from "../../components/data-table";
import { AppPanel } from "../../components/app/app-panel";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import type { AuditEvent, AuditIntegrity } from "../../types";
import { formatTime } from "../../lib/format";
import { statusTone } from "../../lib/status";

// Backend actions are machine strings like "project.create" or
// "account_mfa_enrolled". Humanize them into a readable phrase while keeping the
// raw value available as a tooltip.
function humanizeAction(action: string) {
  return action
    .replace(/[._]/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

// Optional result/severity hints that some events carry in their metadata map.
function eventOutcome(event: AuditEvent): string | undefined {
  const meta = event.metadata ?? {};
  return meta.result ?? meta.status ?? meta.outcome ?? meta.severity ?? undefined;
}

function matchesQuery(event: AuditEvent, query: string) {
  if (!query) return true;
  const haystack = [
    event.action,
    humanizeAction(event.action),
    event.target,
    event.actor_id ?? "",
    eventOutcome(event) ?? "",
    ...Object.entries(event.metadata ?? {}).flatMap(([key, value]) => [key, value]),
  ];
  return haystack.some((value) => value.toLowerCase().includes(query));
}

// Export the currently-filtered events as CSV (client-side download).
function exportAuditCSV(events: AuditEvent[]) {
  const escape = (value: unknown) => `"${String(value ?? "").replace(/"/g, '""')}"`;
  const header = ["seq", "time", "action", "target", "actor", "metadata"];
  const rows = events.map((event) =>
    [event.chain_index, event.created_at, event.action, event.target, event.actor_id ?? "system", JSON.stringify(event.metadata ?? {})]
      .map(escape)
      .join(","),
  );
  const csv = [header.join(","), ...rows].join("\n");
  const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `supadupa-audit-${events.length}.csv`;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

type AuditServer = {
  total: number;
  limit: number;
  offset: number;
  action: string;
  since: string;
  until: string;
  setAction: (value: string) => void;
  setSince: (value: string) => void;
  setUntil: (value: string) => void;
  setOffset: (value: number) => void;
};

export function AuditPanel({ events, integrity, loading, maxEvents, server }: { events: AuditEvent[]; integrity?: AuditIntegrity; loading: boolean; maxEvents?: number; server?: AuditServer }) {
  const compact = typeof maxEvents === "number";
  // Server mode: the parent fetches a server-filtered, paginated page. Action /
  // date filtering and paging are driven server-side; text search and sort stay
  // client-side over the current page.
  const serverMode = Boolean(server) && !compact;
  const [query, setQuery] = useState("");
  const [actionFilter, setActionFilter] = useState("all");
  const [fromDate, setFromDate] = useState("");
  const [toDate, setToDate] = useState("");
  const [sortDir, setSortDir] = useState<"desc" | "asc">("desc");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const actionValue = serverMode ? server!.action : actionFilter;
  const fromValue = serverMode ? server!.since : fromDate;
  const toValue = serverMode ? server!.until : toDate;
  const setActionValue = serverMode ? server!.setAction : setActionFilter;
  const setFromValue = serverMode ? server!.setSince : setFromDate;
  const setToValue = serverMode ? server!.setUntil : setToDate;
  const allActionsValue = serverMode ? "" : "all";

  // Distinct action types for the type filter, sorted for stable display. In
  // server mode this lists actions present on the current page plus the active
  // selection (so it stays selectable across pages).
  const actionTypes = useMemo(() => {
    const set = new Set(events.map((event) => event.action));
    if (serverMode && server!.action) set.add(server!.action);
    return Array.from(set).sort();
  }, [events, serverMode, server]);

  const filtered = useMemo(() => {
    if (compact) return events.slice(0, maxEvents);
    const normalized = query.trim().toLowerCase();
    // In server mode, action + date are already applied upstream; only search
    // and sort run over the current page.
    const fromMs = !serverMode && fromDate ? new Date(`${fromDate}T00:00:00`).getTime() : null;
    const toMs = !serverMode && toDate ? new Date(`${toDate}T23:59:59.999`).getTime() : null;
    const result = events.filter((event) => {
      if (!serverMode && actionFilter !== "all" && event.action !== actionFilter) return false;
      if (!matchesQuery(event, normalized)) return false;
      if (fromMs !== null || toMs !== null) {
        const at = new Date(event.created_at).getTime();
        if (fromMs !== null && at < fromMs) return false;
        if (toMs !== null && at > toMs) return false;
      }
      return true;
    });
    result.sort((left, right) => {
      const diff = new Date(left.created_at).getTime() - new Date(right.created_at).getTime();
      return sortDir === "asc" ? diff : -diff;
    });
    return result;
  }, [events, compact, maxEvents, query, serverMode, actionFilter, fromDate, toDate, sortDir]);

  const columns = useMemo<ColumnDef<AuditEvent>[]>(
    () => {
      const cols: ColumnDef<AuditEvent>[] = [];
      if (!compact) {
        cols.push({
          header: "",
          id: "expander",
          size: 36,
          cell: ({ row }) => {
            const hasDetails = Boolean(row.original.actor_id) || Object.keys(row.original.metadata ?? {}).length > 0;
            if (!hasDetails) return null;
            const open = Boolean(expanded[row.original.id]);
            return (
              <Button
                aria-expanded={open}
                aria-label={open ? "Hide details" : "Show details"}
                variant="ghost"
                size="icon"
                onClick={() => setExpanded((prev) => ({ ...prev, [row.original.id]: !prev[row.original.id] }))}
                type="button"
              >
                {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              </Button>
            );
          },
        });
      }
      cols.push(
        {
          header: "Action",
          accessorKey: "action",
          size: 200,
          cell: ({ row }) => {
            const outcome = eventOutcome(row.original);
            return (
              <div className="flex items-center gap-2">
                <p className="cell-main truncate" title={row.original.action}>{humanizeAction(row.original.action)}</p>
                {outcome ? <StatusPill tone={statusTone(outcome)} label={outcome} /> : null}
              </div>
            );
          },
        },
        {
          header: "Target",
          accessorKey: "target",
          size: 300,
          cell: ({ row }) => <p className="truncate font-mono text-xs text-muted">{row.original.target}</p>,
        },
      );
      if (!compact) {
        cols.push({
          header: "Actor / IP",
          accessorKey: "actor_id",
          size: 200,
          cell: ({ row }) => (
            <div className="min-w-0">
              <p className="truncate font-mono text-xs text-muted" title={row.original.actor_id ?? ""}>{row.original.actor_id || "system"}</p>
              {row.original.metadata?.client_ip ? <p className="truncate font-mono text-xs text-faint">{row.original.metadata.client_ip}</p> : null}
            </div>
          ),
        });
      }
      cols.push(
        {
          // "Sequence #" is the position in the tamper-evident hash chain.
          header: "Seq",
          accessorKey: "chain_index",
          size: 80,
          cell: ({ row }) => <p className="font-mono text-xs text-muted" title="Position in the tamper-evident hash chain">#{row.original.chain_index}</p>,
        },
        {
          header: "Time",
          accessorKey: "created_at",
          size: 110,
          cell: ({ row }) => <time className="text-xs text-faint">{formatTime(row.original.created_at)}</time>,
        },
      );
      return cols;
    },
    [compact, expanded],
  );

  // Render with manual table when we need expandable detail rows; DataTable
  // (read-only) can't host them, so the full audit page uses its own markup but
  // mirrors DataTable's classes. Compact embeds keep using DataTable.
  const showExpandable = !compact;

  return (
    <AppPanel
      eyebrow="Audit log"
      title="Control-plane activity"
      description={!compact ? "Tamper-evident hash chain — each entry seals the previous, so \"verified\" means nothing was altered or removed." : undefined}
      actions={
        integrity ? (
          <div className="flex flex-col items-end gap-1">
            <StatusPill
              tone={integrity.verified ? "success" : "danger"}
              label={integrity.verified ? "chain verified" : `chain broken at #${integrity.broken_at ?? "?"}`}
            />
            {!compact ? (
              <p className="text-xs text-faint">
                {integrity.events} sealed {integrity.events === 1 ? "event" : "events"} &middot; checked {formatTime(integrity.checked_at)}
              </p>
            ) : null}
          </div>
        ) : null
      }
    >
      {!compact ? (
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <div className="relative w-full max-w-xs">
            <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" size={14} />
            <Input
              aria-label="Filter audit events"
              className="pl-8"
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search action, target, actor"
              value={query}
            />
          </div>
          <NativeSelect aria-label="Filter by action type" className="w-auto" onChange={(event) => setActionValue(event.target.value)} value={actionValue}>
            <option value={allActionsValue}>All actions</option>
            {actionTypes.map((action) => (
              <option key={action} value={action}>{humanizeAction(action)}</option>
            ))}
          </NativeSelect>
          <Input aria-label="From date" className="w-auto" type="date" value={fromValue} onChange={(event) => setFromValue(event.target.value)} title="From date" />
          <Input aria-label="To date" className="w-auto" type="date" value={toValue} onChange={(event) => setToValue(event.target.value)} title="To date" />
          <Button variant="secondary" size="sm" type="button" onClick={() => setSortDir((dir) => (dir === "desc" ? "asc" : "desc"))}>
            {sortDir === "desc" ? "Newest first" : "Oldest first"}
          </Button>
          <Button variant="secondary" size="sm" type="button" disabled={filtered.length === 0} onClick={() => exportAuditCSV(filtered)}>
            Export CSV
          </Button>
          {query || actionValue !== allActionsValue || fromValue || toValue ? (
            <Button variant="ghost" size="sm" type="button" onClick={() => { setQuery(""); setActionValue(allActionsValue); setFromValue(""); setToValue(""); }}>Clear</Button>
          ) : null}
          {serverMode ? (
            <div className="ml-auto flex items-center gap-2 text-xs text-faint">
              <span>{server!.total === 0 ? "No events" : `${server!.offset + 1}–${server!.offset + filtered.length} of ${server!.total}`}</span>
              <Button variant="secondary" size="sm" type="button" disabled={server!.offset <= 0} onClick={() => server!.setOffset(Math.max(0, server!.offset - server!.limit))}>Prev</Button>
              <Button variant="secondary" size="sm" type="button" disabled={server!.offset + server!.limit >= server!.total} onClick={() => server!.setOffset(server!.offset + server!.limit)}>Next</Button>
            </div>
          ) : (
            <span className="ml-auto text-xs text-faint">Showing {filtered.length} of {events.length} events</span>
          )}
        </div>
      ) : null}

      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading audit events...</p> : null}
        {showExpandable ? (
          filtered.length === 0 ? (
            <p className="text-sm text-muted">{events.length === 0 ? "No events recorded yet." : "No events match the filter."}</p>
          ) : (
            <div className="data-table-wrap">
              <table className="data-table w-full" style={{ minWidth: 880 }}>
                <thead>
                  <tr>
                    {columns.map((col, index) => (
                      <th key={col.id ?? (col as { accessorKey?: string }).accessorKey ?? index} style={{ width: col.size }}>
                        {typeof col.header === "string" ? col.header : null}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((event) => {
                    const open = Boolean(expanded[event.id]);
                    const metaEntries = Object.entries(event.metadata ?? {});
                    const hasDetails = Boolean(event.actor_id) || metaEntries.length > 0;
                    const outcome = eventOutcome(event);
                    return (
                      <Fragment key={event.id}>
                        <tr>
                          <td>
                            {hasDetails ? (
                              <Button
                                aria-expanded={open}
                                aria-label={open ? "Hide details" : "Show details"}
                                variant="ghost"
                                size="icon"
                                onClick={() => setExpanded((prev) => ({ ...prev, [event.id]: !prev[event.id] }))}
                                type="button"
                              >
                                {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                              </Button>
                            ) : null}
                          </td>
                          <td>
                            <div className="flex items-center gap-2">
                              <p className="cell-main truncate" title={event.action}>{humanizeAction(event.action)}</p>
                              {outcome ? <StatusPill tone={statusTone(outcome)} label={outcome} /> : null}
                            </div>
                          </td>
                          <td><p className="truncate font-mono text-xs text-muted">{event.target}</p></td>
                          <td><p className="truncate font-mono text-xs text-muted" title={event.actor_id ?? ""}>{event.actor_id || "system"}</p></td>
                          <td><p className="font-mono text-xs text-muted" title="Position in the tamper-evident hash chain">#{event.chain_index}</p></td>
                          <td><time className="text-xs text-faint">{formatTime(event.created_at)}</time></td>
                        </tr>
                        {open ? (
                          <tr>
                            <td colSpan={6}>
                              <div className="grid gap-1 rounded-md border border-border bg-bg p-3 text-xs">
                                <div className="grid grid-cols-[120px_minmax(0,1fr)] gap-x-3 gap-y-1">
                                  <span className="text-faint">Actor</span>
                                  <span className="font-mono text-muted">{event.actor_id || "system"}</span>
                                  {metaEntries.map(([key, value]) => (
                                    <Fragment key={key}>
                                      <span className="text-faint">{key}</span>
                                      <span className="font-mono break-all text-muted">{value}</span>
                                    </Fragment>
                                  ))}
                                </div>
                              </div>
                            </td>
                          </tr>
                        ) : null}
                      </Fragment>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )
        ) : (
          <>
            <DataTable columns={columns} data={filtered} emptyText="No events recorded yet." minWidth={760} />
            {compact && events.length > (maxEvents ?? 0) ? <p className="text-xs text-faint">Showing {maxEvents} of {events.length} events.</p> : null}
          </>
        )}
      </div>
    </AppPanel>
  );
}
