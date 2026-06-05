import { Activity } from "lucide-react";
import { formatTime } from "../../lib/format";
import type { AuditEvent } from "../../types";

export function ProjectActivityPanel({ events, loading }: { events: AuditEvent[]; loading: boolean }) {
  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Activity</p>
          <h2>Project audit trail</h2>
        </div>
        <Activity size={15} className="text-faint" />
      </div>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading project activity...</p> : null}
        {!loading && events.length === 0 ? <p className="text-sm text-muted">No project activity recorded yet.</p> : null}
        {events.slice(0, 8).map((event) => (
          <div className="audit-row" key={event.id}>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{event.action}</p>
              <p className="truncate font-mono text-xs text-muted">#{event.chain_index} {event.target}</p>
            </div>
            <time className="text-xs text-faint">{formatTime(event.created_at)}</time>
          </div>
        ))}
      </div>
    </section>
  );
}
