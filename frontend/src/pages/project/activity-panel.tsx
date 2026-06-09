import { useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Activity } from "lucide-react";
import { AppPanel } from "../../components/app/app-panel";
import { Button, buttonVariants } from "../../components/ui/button";
import { EmptyState } from "../../components/ui/empty-state";
import { StatusPill } from "../../components/ui/status-pill";
import { formatRelativeTime } from "../../lib/format";
import { statusTone } from "../../lib/status";
import type { AuditEvent } from "../../types";

const DEFAULT_LIMIT = 25;

// Mirror the audit panel's humanization so project activity reads the same way
// as the global Audit log (we can't import from audit/, so we replicate it).
function humanizeAction(action: string) {
  return action
    .replace(/[._]/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

function eventActor(event: AuditEvent): string {
  const meta = event.metadata ?? {};
  return event.actor_id || meta.actor || meta.actor_email || meta.user || "system";
}

function eventOutcome(event: AuditEvent): string | undefined {
  const meta = event.metadata ?? {};
  return meta.result ?? meta.status ?? meta.outcome ?? meta.severity ?? undefined;
}

function dayLabel(value: string, now = Date.now()): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown";
  const startOfDay = (ts: number) => {
    const d = new Date(ts);
    d.setHours(0, 0, 0, 0);
    return d.getTime();
  };
  const diffDays = Math.round((startOfDay(now) - startOfDay(date.getTime())) / 86400000);
  if (diffDays <= 0) return "Today";
  if (diffDays === 1) return "Yesterday";
  if (diffDays < 7) return new Intl.DateTimeFormat(undefined, { weekday: "long" }).format(date);
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: diffDays > 330 ? "numeric" : undefined }).format(date);
}

export function ProjectActivityPanel({ events, loading }: { events: AuditEvent[]; loading: boolean }) {
  const [showAll, setShowAll] = useState(false);
  const limit = showAll ? events.length : DEFAULT_LIMIT;

  // Group the visible events by calendar day so a multi-day feed stays readable.
  const groups = useMemo(() => {
    const visible = events.slice(0, limit);
    const buckets: Array<{ label: string; events: AuditEvent[] }> = [];
    let current: { label: string; events: AuditEvent[] } | null = null;
    for (const event of visible) {
      const label = dayLabel(event.created_at);
      if (!current || current.label !== label) {
        current = { label, events: [] };
        buckets.push(current);
      }
      current.events.push(event);
    }
    return buckets;
  }, [events, limit]);

  return (
    <AppPanel
      eyebrow="Activity"
      title="Project audit trail"
      description="Project-scoped slice of the control-plane audit log."
      actions={<Link className={buttonVariants({ variant: "secondary", size: "sm" })} to="/audit">Open Audit log</Link>}
    >
      <div className="mt-4 grid gap-3">
        {loading ? <p className="text-sm text-muted">Loading project activity...</p> : null}
        {!loading && events.length === 0 ? (
          <EmptyState icon={Activity} title="No project activity yet" description="Configuration changes, deploys, and recovery actions for this project will appear here." />
        ) : null}
        {groups.map((group) => (
          <div className="grid gap-1" key={group.label}>
            <p className="label sticky top-0 bg-surface py-1">{group.label}</p>
            {group.events.map((event) => {
              const outcome = eventOutcome(event);
              return (
                <div className="audit-row" key={event.id}>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <p className="truncate text-sm font-medium" title={event.action}>{humanizeAction(event.action)}</p>
                      {outcome ? <StatusPill tone={statusTone(outcome)} label={outcome} /> : null}
                    </div>
                    <p className="truncate text-xs text-muted">
                      <span className="text-faint">{eventActor(event)}</span>
                      {event.target ? <span className="font-mono"> · {event.target}</span> : null}
                    </p>
                  </div>
                  <time className="shrink-0 text-xs text-faint" title={new Date(event.created_at).toLocaleString()}>{formatRelativeTime(event.created_at)}</time>
                </div>
              );
            })}
          </div>
        ))}
        {events.length > 0 ? (
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs text-faint">Showing {Math.min(limit, events.length)} of {events.length} events</span>
            {events.length > DEFAULT_LIMIT ? (
              <Button variant="secondary" size="sm" onClick={() => setShowAll((value) => !value)} type="button">
                {showAll ? "Show recent" : `Show all (${events.length})`}
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>
    </AppPanel>
  );
}
