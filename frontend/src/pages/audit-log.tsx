import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { listAuditEvents } from "../api";
import { useDashboardContext } from "../lib/dashboard-context";
import { AuditPanel } from "./audit/audit-panel";

const PAGE_SIZE = 100;

export function AuditLogPage() {
  const { auditIntegrity } = useDashboardContext();
  const [action, setAction] = useState("");
  const [since, setSince] = useState("");
  const [until, setUntil] = useState("");
  const [offset, setOffset] = useState(0);

  const audit = useQuery({
    queryKey: ["audit-page", action, since, until, offset],
    queryFn: () => listAuditEvents({ limit: PAGE_SIZE, offset, action: action || undefined, since: since || undefined, until: until || undefined }),
    refetchInterval: 15_000,
    placeholderData: (previous) => previous,
  });
  const page = audit.data;

  // Resetting to the first page whenever a filter changes keeps offset valid.
  const reset = (apply: () => void) => {
    setOffset(0);
    apply();
  };

  return (
    <AuditPanel
      events={page?.events ?? []}
      integrity={auditIntegrity.data}
      loading={audit.isLoading}
      server={{
        total: page?.total ?? 0,
        limit: page?.limit ?? PAGE_SIZE,
        offset,
        action,
        since,
        until,
        setAction: (value) => reset(() => setAction(value)),
        setSince: (value) => reset(() => setSince(value)),
        setUntil: (value) => reset(() => setUntil(value)),
        setOffset,
      }}
    />
  );
}
