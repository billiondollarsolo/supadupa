import { useDashboardContext } from "../lib/dashboard-context";
import { AuditPanel } from "./audit/audit-panel";

export function AuditLogPage() {
  const { auditEvents, auditIntegrity } = useDashboardContext();
  return <AuditPanel events={auditEvents.data ?? []} integrity={auditIntegrity.data} loading={auditEvents.isLoading || auditIntegrity.isLoading} />;
}
