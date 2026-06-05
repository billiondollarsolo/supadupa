import { useNavigate } from "@tanstack/react-router";
import { Activity, ArrowRight, Database, Server, Shield } from "lucide-react";
import { useDashboardContext } from "../lib/dashboard-context";
import { formatBytes, formatTime } from "../lib/format";
import { ProjectCards } from "./projects/panels";

export function FleetDashboardPage() {
  const { activeRef, projectList, hosts, orgs, fleetMetrics, advisorFindings, complianceReport, auditEvents, auditIntegrity, projects, routeToProject } = useDashboardContext();
  const navigate = useNavigate();
  return (
    <div className="grid gap-6">
      <ProjectCards
        projects={projectList}
        orgNamesById={new Map((orgs.data ?? []).map((org) => [org.id, org.name]))}
        hostsById={new Map((hosts.data ?? []).map((host) => [host.id, host]))}
        selectedRef={activeRef}
        onSelect={(ref) => routeToProject(ref)}
        onAccess={(ref) => routeToProject(ref, "auth")}
        onCreate={() => void navigate({ to: "/projects/new" })}
        loading={projects.isLoading || hosts.isLoading || orgs.isLoading}
        maxProjects={6}
      />
      <FleetOverviewPanel
        auditVerified={auditIntegrity.data?.verified ?? fleetMetrics.data?.audit_verified}
        complianceActionNeeded={complianceReport.data?.summary.action_needed ?? 0}
        compliancePassed={complianceReport.data?.summary.passed ?? 0}
        complianceTotal={complianceReport.data?.summary.total ?? 0}
        findings={advisorFindings.data?.length ?? 0}
        hosts={hosts.data?.length ?? 0}
        loading={fleetMetrics.isLoading || advisorFindings.isLoading || complianceReport.isLoading || auditEvents.isLoading || auditIntegrity.isLoading}
        metrics={fleetMetrics.data}
        projects={projectList}
        recentAuditEvents={auditEvents.data?.length ?? 0}
        onOpenAudit={() => void navigate({ to: "/audit" })}
        onOpenHosts={() => void navigate({ to: "/hosts" })}
        onOpenProjects={() => void navigate({ to: "/projects" })}
        onOpenSecurity={() => void navigate({ to: "/security" })}
      />
    </div>
  );
}

function FleetOverviewPanel({
  auditVerified,
  complianceActionNeeded,
  compliancePassed,
  complianceTotal,
  findings,
  hosts,
  loading,
  metrics,
  onOpenAudit,
  onOpenHosts,
  onOpenProjects,
  onOpenSecurity,
  projects,
  recentAuditEvents,
}: {
  auditVerified?: boolean;
  complianceActionNeeded: number;
  compliancePassed: number;
  complianceTotal: number;
  findings: number;
  hosts: number;
  loading: boolean;
  metrics?: ReturnType<typeof useDashboardContext>["fleetMetrics"]["data"];
  projects: ReturnType<typeof useDashboardContext>["projectList"];
  recentAuditEvents: number;
  onOpenAudit: () => void;
  onOpenHosts: () => void;
  onOpenProjects: () => void;
  onOpenSecurity: () => void;
}) {
  const healthy = projects.filter((project) => project.status === "healthy").length;
  const errored = projects.filter((project) => project.status === "error").length;
  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Fleet</p>
          <h2>Operational overview</h2>
        </div>
        {metrics ? <time className="text-xs text-faint">{formatTime(metrics.sampled_at)}</time> : null}
      </div>
      {loading ? <p className="mt-4 text-sm text-muted">Loading fleet status...</p> : null}
      <div className="mt-4 metric-grid">
        <Metric label="Projects" value={projects.length.toString()} />
        <Metric label="Healthy" value={healthy.toString()} tone="healthy" />
        <Metric label="Errors" value={errored.toString()} tone={errored > 0 ? "error" : undefined} />
        <Metric label="Hosts" value={hosts.toString()} />
        <Metric label="Routes" value={(metrics?.routes ?? 0).toString()} />
        <Metric label="Audit" value={auditVerified ? "verified" : "pending"} tone={auditVerified ? "healthy" : "warning"} />
      </div>
      <div className="mt-3 grid grid-cols-2 gap-3 max-lg:grid-cols-1">
        <div className="usage-row">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Reserved capacity</p>
            <p className="truncate text-xs text-muted">{metrics ? `${metrics.host_used.projects}/${metrics.host_capacity.projects || "-"} projects · ${metrics.host_used.cpu}/${metrics.host_capacity.cpu || "-"} CPU` : "No capacity sample"}</p>
          </div>
          <div className="text-right text-xs text-muted">
            <p>{metrics ? `${formatBytes(metrics.host_used.ram_mb * 1024 * 1024)} / ${metrics.host_capacity.ram_mb ? formatBytes(metrics.host_capacity.ram_mb * 1024 * 1024) : "-"}` : "-"}</p>
            <p>{metrics ? `${metrics.host_used.disk_gb}/${metrics.host_capacity.disk_gb || "-"} GB disk` : "-"}</p>
          </div>
        </div>
        <div className="usage-row">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Observed telemetry</p>
            <p className="truncate text-xs text-muted">{metrics ? `${metrics.observed?.projects_sampled ?? 0} sampled projects · ${metrics.observed?.stale_projects ?? 0} stale` : "No telemetry sample"}</p>
          </div>
          <div className="text-right text-xs text-muted">
            <p>{(metrics?.observed?.cpu_percent ?? 0).toFixed(1)}% CPU</p>
            <p>{formatBytes(metrics?.observed?.memory_bytes ?? 0)} memory</p>
          </div>
        </div>
      </div>
      <div className="mt-3 grid grid-cols-4 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
        <DrilldownCard icon={Database} label="Projects" value={`${projects.length} stacks`} detail="Cards, filters, create flow, and access drilldown." onClick={onOpenProjects} />
        <DrilldownCard icon={Shield} label="Security" value={`${findings} findings`} detail={`${compliancePassed}/${complianceTotal || "-"} controls pass · ${complianceActionNeeded} action needed`} onClick={onOpenSecurity} />
        <DrilldownCard icon={Activity} label="Audit" value={`${recentAuditEvents} recent`} detail={auditVerified ? "Integrity chain verified." : "Integrity check pending."} onClick={onOpenAudit} />
        <DrilldownCard icon={Server} label="Hosts" value={`${hosts} registered`} detail="Capacity, reserved resources, and local runtime posture." onClick={onOpenHosts} />
      </div>
    </section>
  );
}

function Metric({ label, tone, value }: { label: string; value: string; tone?: "healthy" | "warning" | "error" }) {
  return (
    <div className="metric-cell bg-bg">
      <p className="label">{label}</p>
      <p className={tone === "healthy" ? "truncate text-sm font-medium text-success" : tone === "warning" ? "truncate text-sm font-medium text-warning" : tone === "error" ? "truncate text-sm font-medium text-danger" : "truncate text-sm font-medium"}>{value}</p>
    </div>
  );
}

function DrilldownCard({ detail, icon: Icon, label, onClick, value }: { icon: typeof Activity; label: string; value: string; detail: string; onClick: () => void }) {
  return (
    <button className="grid min-h-32 content-between rounded-md border border-border bg-bg p-3 text-left transition hover:border-border-strong hover:bg-surface-2" onClick={onClick} type="button">
      <span className="flex items-center justify-between gap-3">
        <span className="flex min-w-0 items-center gap-2">
          <Icon size={14} className="text-faint" />
          <span className="label">{label}</span>
        </span>
        <ArrowRight size={14} className="text-faint" />
      </span>
      <span>
        <span className="block truncate text-sm font-medium">{value}</span>
        <span className="mt-1 block text-xs text-muted">{detail}</span>
      </span>
    </button>
  );
}
