import type { AuditEvent, AuditIntegrity } from "../../types";
import { formatTime } from "../../lib/format";

export function AuditPanel({ events, integrity, loading, maxEvents }: { events: AuditEvent[]; integrity?: AuditIntegrity; loading: boolean; maxEvents?: number }) {
  const visibleEvents = typeof maxEvents === "number" ? events.slice(0, maxEvents) : events;
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
        {!loading && events.length === 0 ? <p className="text-sm text-muted">No events recorded yet.</p> : null}
        {visibleEvents.map((event) => (
          <div className="audit-row" key={event.id}>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{event.action}</p>
              <p className="truncate font-mono text-xs text-muted">#{event.chain_index} {event.target}</p>
            </div>
            <time className="text-xs text-faint">{formatTime(event.created_at)}</time>
          </div>
        ))}
        {typeof maxEvents === "number" && events.length > maxEvents ? <p className="text-xs text-faint">Showing {maxEvents} of {events.length} events.</p> : null}
      </div>
    </section>
  );
}
