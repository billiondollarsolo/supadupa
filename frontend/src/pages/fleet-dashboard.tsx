import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Activity, ArrowRight, Cpu, Database, Gauge, HardDrive, Network, Server, Shield, type LucideIcon } from "lucide-react";
import { useDashboardContext } from "../lib/dashboard-context";
import { formatBytes, formatTime } from "../lib/format";
import type { FleetMetrics } from "../types";

type FleetHistoryPoint = {
  sampledAt: string;
  cpuCapacityPercent: number;
  memoryCapacityPercent: number;
  networkRxBytes: number;
  networkTxBytes: number;
};

export function FleetDashboardPage() {
  const { projectList, hosts, fleetMetrics, advisorFindings, complianceReport, auditEvents, auditIntegrity, projects, routeToProject } = useDashboardContext();
  const navigate = useNavigate();
  const metrics = fleetMetrics.data;
  const observed = metrics?.observed;
  const host = hosts.data?.[0];
  const healthy = projectList.filter((project) => project.status === "healthy").length;
  const degraded = projectList.filter((project) => project.status === "degraded").length;
  const errored = projectList.filter((project) => project.status === "error").length;
  const [history, setHistory] = useState<FleetHistoryPoint[]>([]);

  const live = metrics ? liveUsageFromMetrics(metrics) : emptyLiveUsage();
  const networkRate = useMemo(() => networkRateFromHistory(history), [history]);

  useEffect(() => {
    if (!metrics) {
      return;
    }
    const point = historyPointFromMetrics(metrics);
    setHistory((current) => {
      if (current[current.length - 1]?.sampledAt === point.sampledAt) {
        return current;
      }
      return [...current.filter((item) => item.sampledAt !== point.sampledAt), point].slice(-24);
    });
  }, [metrics]);

  return (
    <div className="grid gap-6">
      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Dashboard</p>
            <h2>At a glance</h2>
          </div>
          {metrics ? <time className="text-xs text-faint">{formatTime(metrics.sampled_at)}</time> : null}
        </div>
        {fleetMetrics.isLoading ? <p className="mt-4 text-sm text-muted">Loading dashboard...</p> : null}
        <div className="mt-4 metric-grid">
          <Metric label="Projects" value={`${healthy}/${projectList.length} healthy`} detail={`${degraded} degraded / ${errored} errors`} tone={errored > 0 ? "error" : degraded > 0 ? "warning" : "healthy"} />
          <Metric label="Server" value={host ? host.name : `${metrics?.hosts ?? 0} hosts`} detail={metrics ? `${metrics.host_capacity.cpu || "-"} vCPU / ${formatCapacityRAM(metrics.host_capacity.ram_mb)} RAM` : "Capacity pending"} />
          <Metric label="Live CPU" value={`${formatDecimal(live.cpuVCpu)} / ${metrics?.host_capacity.cpu || "-"} vCPU`} detail={`${formatPercent(observed?.cpu_percent ?? 0)} reported`} tone={live.cpuCapacityPercent > 85 ? "warning" : undefined} />
          <Metric label="Live memory" value={`${formatBytes(observed?.memory_bytes ?? 0)} / ${formatCapacityRAM(metrics?.host_capacity.ram_mb ?? 0)}`} detail={`${formatPercent(live.memoryCapacityPercent)} of server RAM`} tone={live.memoryCapacityPercent > 85 ? "warning" : undefined} />
          <Metric label="Reserved CPU" value={`${metrics?.host_used.cpu ?? 0} / ${metrics?.host_capacity.cpu || "-"} vCPU`} detail={`${formatCapacityRAM(metrics?.host_used.ram_mb ?? 0)} RAM reserved`} />
          <Metric label="Telemetry" value={`${observed?.projects_sampled ?? 0}/${projectList.length} fresh`} detail={`${observed?.stale_projects ?? 0} stale samples`} tone={(observed?.stale_projects ?? 0) > 0 ? "warning" : undefined} />
          <Metric label="Security" value={`${advisorFindings.data?.length ?? 0} findings`} detail={`${complianceReport.data?.summary.passed ?? 0}/${complianceReport.data?.summary.total || "-"} controls pass`} tone={(advisorFindings.data?.length ?? 0) > 0 ? "warning" : "healthy"} />
          <Metric label="Audit" value={auditIntegrity.data?.verified ?? metrics?.audit_verified ? "verified" : "pending"} detail={`${auditEvents.data?.length ?? 0} recent events`} tone={auditIntegrity.data?.verified ?? metrics?.audit_verified ? "healthy" : "warning"} />
        </div>
      </section>

      <div className="grid grid-cols-[minmax(0,1.25fr)_minmax(360px,0.75fr)] gap-6 max-xl:grid-cols-1">
        <section className="panel">
          <div className="section-head">
            <div>
              <p className="label">Server</p>
              <h2>Live usage</h2>
            </div>
            <button className="button secondary h-8 min-h-8" onClick={() => void navigate({ to: "/hosts" })} type="button">
              <Server size={14} />
              Hosts
            </button>
          </div>
          <div className="mt-4 grid grid-cols-2 gap-3 max-lg:grid-cols-1">
            <UsageMeter icon={Cpu} label="CPU now" value={`${formatDecimal(live.cpuVCpu)} / ${metrics?.host_capacity.cpu || "-"} vCPU`} detail={`${formatPercent(observed?.cpu_percent ?? 0)} collector CPU`} percent={live.cpuCapacityPercent} />
            <UsageMeter icon={Gauge} label="Memory now" value={`${formatBytes(observed?.memory_bytes ?? 0)} / ${formatCapacityRAM(metrics?.host_capacity.ram_mb ?? 0)}`} detail={`${formatPercent(live.memoryCapacityPercent)} of server RAM`} percent={live.memoryCapacityPercent} />
            <InfoRow icon={Network} title="Network rate" detail={`${formatBytes(networkRate.rxBytesPerSecond)}/s in / ${formatBytes(networkRate.txBytesPerSecond)}/s out`} value={`${formatBytes(networkRate.rxBytesPerSecond + networkRate.txBytesPerSecond)}/s`} />
            <InfoRow icon={HardDrive} title="Disk telemetry" detail={observed && observed.disk_limit_bytes > 0 ? `${formatBytes(observed.disk_used_bytes)} of ${formatBytes(observed.disk_limit_bytes)}` : `${metrics?.host_used.disk_gb ?? 0} GB reserved / ${metrics?.host_capacity.disk_gb || "-"} GB capacity`} value={observed && observed.disk_limit_bytes > 0 ? formatPercent((observed.disk_used_bytes / observed.disk_limit_bytes) * 100) : "not sampled"} />
          </div>
          <UsageTrendChart points={history} />
          <div className="mt-3 grid grid-cols-2 gap-3 max-sm:grid-cols-1">
            <InfoRow title="Fresh samples" detail={`${observed?.projects_sampled ?? 0} active projects sampled`} value={observed?.latest_sampled_at ? formatTime(observed.latest_sampled_at) : "pending"} />
            <InfoRow title="Stale samples" detail={`${observed?.stale_projects ?? 0} active projects older than ${observed?.stale_after_seconds ?? 0}s`} value={observed?.oldest_sampled_at ? formatTime(observed.oldest_sampled_at) : "none"} />
          </div>
        </section>

        <section className="panel">
          <div className="section-head">
            <div>
              <p className="label">Capacity</p>
              <h2>Reservations</h2>
            </div>
          </div>
          <div className="mt-4 grid gap-3">
            <ResourceRow icon={Cpu} label="CPU reserved" used={metrics?.host_used.cpu ?? 0} capacity={metrics?.host_capacity.cpu ?? 0} suffix="vCPU" />
            <ResourceRow icon={Gauge} label="RAM reserved" used={metrics?.host_used.ram_mb ?? 0} capacity={metrics?.host_capacity.ram_mb ?? 0} formatter={(value) => formatBytes(value * 1024 * 1024)} />
            <ResourceRow icon={HardDrive} label="Disk reserved" used={metrics?.host_used.disk_gb ?? 0} capacity={metrics?.host_capacity.disk_gb ?? 0} suffix="GB" />
            <ResourceRow icon={Activity} label="IOPS reserved" used={metrics?.host_used.disk_iops ?? 0} capacity={metrics?.host_capacity.disk_iops ?? 0} />
            <ResourceRow icon={Database} label="Project slots" used={metrics?.host_used.projects ?? projectList.length} capacity={metrics?.host_capacity.projects ?? 0} />
          </div>
        </section>
      </div>

      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Operations</p>
            <h2>Signals</h2>
          </div>
        </div>
        <div className="mt-4 grid grid-cols-3 gap-3 max-xl:grid-cols-1">
          <SignalButton icon={Database} label="Projects" value={`${projectList.length} stacks`} detail={`${healthy} healthy / ${degraded} degraded / ${errored} errors`} onClick={() => void navigate({ to: "/projects" })} />
          <SignalButton icon={Shield} label="Security" value={`${advisorFindings.data?.length ?? 0} findings`} detail={`${complianceReport.data?.summary.passed ?? 0}/${complianceReport.data?.summary.total || "-"} controls pass`} onClick={() => void navigate({ to: "/security" })} />
          <SignalButton icon={Activity} label="Audit" value={auditIntegrity.data?.verified ?? metrics?.audit_verified ? "verified" : "pending"} detail={`${auditEvents.data?.length ?? 0} recent events`} onClick={() => void navigate({ to: "/audit" })} />
        </div>
      </section>

      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Projects</p>
            <h2>Active projects</h2>
          </div>
          <button className="button secondary h-8 min-h-8" onClick={() => void navigate({ to: "/projects" })} type="button">
            <Database size={14} />
            All projects
          </button>
        </div>
        <div className="mt-4 grid gap-2">
          {projects.isLoading ? <p className="text-sm text-muted">Loading projects...</p> : null}
          {!projects.isLoading && projectList.length === 0 ? <p className="text-sm text-muted">No projects deployed yet.</p> : null}
          {projectList.slice(0, 8).map((project) => (
            <button className="usage-row text-left transition hover:border-border-strong hover:bg-surface-2" key={project.ref} onClick={() => routeToProject(project.ref)} type="button">
              <span className="min-w-0">
                <span className="flex min-w-0 items-center gap-2">
                  <span className={`status-dot ${project.status === "healthy" ? "bg-success" : project.status === "error" ? "bg-danger" : "bg-warning"}`} />
                  <span className="truncate text-sm font-medium">{project.name}</span>
                  <span className={`pill ${project.status === "healthy" ? "healthy" : project.status === "error" ? "error" : "provisioning"}`}>{project.status}</span>
                </span>
                <span className="mt-1 block truncate font-mono text-xs text-muted">{project.ref} / {project.spec.domain} / {project.spec.resource_tier}</span>
              </span>
              <span className="flex items-center gap-2 text-xs text-muted">
                {project.runtime_status?.phase ? <span>{project.runtime_status.phase}</span> : null}
                <ArrowRight size={14} />
              </span>
            </button>
          ))}
        </div>
      </section>
    </div>
  );
}

function Metric({ detail, label, tone, value }: { label: string; value: string; detail?: string; tone?: "healthy" | "warning" | "error" }) {
  const valueClass = tone === "healthy" ? "text-success" : tone === "warning" ? "text-warning" : tone === "error" ? "text-danger" : "";
  return (
    <div className="metric-cell bg-bg">
      <p className="label">{label}</p>
      <p className={`truncate text-sm font-medium ${valueClass}`}>{value}</p>
      {detail ? <p className="mt-1 truncate text-xs text-faint">{detail}</p> : null}
    </div>
  );
}

function UsageMeter({ detail, icon: Icon, label, percent, value }: { icon: LucideIcon; label: string; value: string; detail: string; percent: number }) {
  const normalized = clampPercent(percent);
  return (
    <div className="usage-row">
      <div className="min-w-0">
        <p className="flex items-center gap-2 truncate text-sm font-medium"><Icon size={14} className="text-faint" />{label}</p>
        <p className="mt-1 truncate text-xs text-muted">{detail}</p>
        <div className="resource-bar mt-2"><span style={{ width: `${normalized || 2}%` }} /></div>
      </div>
      <div className="text-right text-xs text-muted">
        <p className="text-sm font-medium text-text">{value}</p>
        <p>{formatPercent(percent)}</p>
      </div>
    </div>
  );
}

function ResourceRow({ capacity, formatter, icon: Icon, label, suffix = "", used }: { icon: LucideIcon; label: string; used: number; capacity: number; suffix?: string; formatter?: (value: number) => string }) {
  const percent = capacity > 0 ? (used / capacity) * 100 : 0;
  const render = formatter ?? ((value: number) => suffix ? `${value.toLocaleString()} ${suffix}` : value.toLocaleString());
  return (
    <div className="usage-row">
      <div className="min-w-0">
        <p className="flex items-center gap-2 truncate text-sm font-medium"><Icon size={14} className="text-faint" />{label}</p>
        <div className="resource-bar mt-2"><span style={{ width: `${Math.max(clampPercent(percent), used > 0 ? 3 : 0)}%` }} /></div>
      </div>
      <div className="text-right text-xs text-muted">
        <p className="text-sm font-medium text-text">{render(used)}</p>
        <p>{capacity > 0 ? `${formatPercent(percent)} of ${render(capacity)}` : "capacity unset"}</p>
      </div>
    </div>
  );
}

function InfoRow({ detail, icon: Icon, title, value }: { title: string; detail: string; value: string; icon?: LucideIcon }) {
  return (
    <div className="usage-row">
      <div className="min-w-0">
        <p className="flex items-center gap-2 truncate text-sm font-medium">{Icon ? <Icon size={14} className="text-faint" /> : null}{title}</p>
        <p className="truncate text-xs text-muted">{detail}</p>
      </div>
      <p className="text-right text-xs text-muted">{value}</p>
    </div>
  );
}

function UsageTrendChart({ points }: { points: FleetHistoryPoint[] }) {
  const cpuLine = trendPolyline(points.map((point) => point.cpuCapacityPercent));
  const memoryLine = trendPolyline(points.map((point) => point.memoryCapacityPercent));
  const latest = points[points.length - 1];
  return (
    <div className="usage-trend mt-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="label">Recent utilization</p>
          <p className="mt-1 text-sm font-medium">{points.length > 1 ? `${points.length} samples` : "Waiting for samples"}</p>
        </div>
        <div className="flex flex-wrap justify-end gap-2 text-xs text-muted">
          <span className="trend-legend cpu">CPU</span>
          <span className="trend-legend memory">RAM</span>
          {latest ? <span>{formatTime(latest.sampledAt)}</span> : null}
        </div>
      </div>
      <svg aria-label="Recent CPU and memory utilization" className="trend-chart mt-3" preserveAspectRatio="none" viewBox="0 0 100 48">
        <line className="trend-grid-line" x1="0" x2="100" y1="12" y2="12" />
        <line className="trend-grid-line" x1="0" x2="100" y1="24" y2="24" />
        <line className="trend-grid-line" x1="0" x2="100" y1="36" y2="36" />
        {points.length > 1 ? <polyline className="trend-line cpu" points={cpuLine} /> : null}
        {points.length > 1 ? <polyline className="trend-line memory" points={memoryLine} /> : null}
      </svg>
    </div>
  );
}

function SignalButton({ detail, icon: Icon, label, onClick, value }: { icon: LucideIcon; label: string; value: string; detail: string; onClick: () => void }) {
  return (
    <button className="usage-row text-left transition hover:border-border-strong hover:bg-surface-2" onClick={onClick} type="button">
      <span className="flex min-w-0 items-center gap-2">
        <Icon size={14} className="text-faint" />
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">{label}</span>
          <span className="block truncate text-xs text-muted">{detail}</span>
        </span>
      </span>
      <span className="flex items-center gap-2 text-xs text-muted">
        {value}
        <ArrowRight size={14} />
      </span>
    </button>
  );
}

function emptyLiveUsage() {
  return {
    cpuVCpu: 0,
    cpuCapacityPercent: 0,
    memoryCapacityPercent: 0,
  };
}

function liveUsageFromMetrics(metrics: FleetMetrics) {
  const cpuVCpu = (metrics.observed?.cpu_percent ?? 0) / 100;
  const cpuCapacityPercent = metrics.host_capacity.cpu > 0 ? (cpuVCpu / metrics.host_capacity.cpu) * 100 : metrics.observed?.cpu_percent ?? 0;
  const memoryCapacityBytes = metrics.host_capacity.ram_mb * 1024 * 1024;
  const fallbackMemoryLimit = metrics.observed?.memory_limit_bytes ?? 0;
  const memoryLimit = memoryCapacityBytes > 0 ? memoryCapacityBytes : fallbackMemoryLimit;
  return {
    cpuVCpu,
    cpuCapacityPercent,
    memoryCapacityPercent: memoryLimit > 0 ? ((metrics.observed?.memory_bytes ?? 0) / memoryLimit) * 100 : 0,
  };
}

function historyPointFromMetrics(metrics: FleetMetrics): FleetHistoryPoint {
  const live = liveUsageFromMetrics(metrics);
  return {
    sampledAt: metrics.sampled_at,
    cpuCapacityPercent: live.cpuCapacityPercent,
    memoryCapacityPercent: live.memoryCapacityPercent,
    networkRxBytes: metrics.observed?.network_rx_bytes ?? 0,
    networkTxBytes: metrics.observed?.network_tx_bytes ?? 0,
  };
}

function networkRateFromHistory(points: FleetHistoryPoint[]) {
  const current = points[points.length - 1];
  const previous = points[points.length - 2];
  if (!current || !previous) {
    return { rxBytesPerSecond: 0, txBytesPerSecond: 0 };
  }
  const seconds = Math.max(1, (new Date(current.sampledAt).getTime() - new Date(previous.sampledAt).getTime()) / 1000);
  return {
    rxBytesPerSecond: Math.max(0, (current.networkRxBytes - previous.networkRxBytes) / seconds),
    txBytesPerSecond: Math.max(0, (current.networkTxBytes - previous.networkTxBytes) / seconds),
  };
}

function trendPolyline(values: number[]) {
  if (values.length <= 1) {
    return "";
  }
  const maxIndex = Math.max(1, values.length - 1);
  return values.map((value, index) => {
    const x = (index / maxIndex) * 100;
    const y = 48 - (clampPercent(value) / 100) * 44 - 2;
    return `${x.toFixed(2)},${y.toFixed(2)}`;
  }).join(" ");
}

function clampPercent(value: number) {
  return Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0));
}

function formatPercent(value: number) {
  return `${(Number.isFinite(value) ? value : 0).toFixed(1)}%`;
}

function formatDecimal(value: number) {
  return (Number.isFinite(value) ? value : 0).toFixed(1);
}

function formatCapacityRAM(ramMB: number) {
  return ramMB > 0 ? formatBytes(ramMB * 1024 * 1024) : "-";
}
