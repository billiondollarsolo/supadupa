import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowRight, Cpu, Database, Gauge, HardDrive, Network, Plus, Server, ShieldAlert, type LucideIcon } from "lucide-react";
import { AppPanel } from "../components/app/app-panel";
import { InfoRow } from "../components/app/info-row";
import { MetricCard } from "../components/app/metric-card";
import { ResourceMeter } from "../components/app/resource-meter";
import { TelemetryLineChart } from "../components/charts/telemetry-line-chart";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { CollapsibleCard } from "../components/ui/collapsible-card";
import { EmptyState } from "../components/ui/empty-state";
import { StatusPill } from "../components/ui/status-pill";
import { getFleetTraffic } from "../api";
import { useDashboardContext } from "../lib/dashboard-context";
import { formatBytes, formatTime } from "../lib/format";
import { type Tone } from "../lib/status";
import type { FleetMetrics, Project } from "../types";

type FleetHistoryPoint = {
  sampledAt: string;
  cpuCapacityPercent: number;
  memoryCapacityPercent: number;
  networkRxBytes: number;
  networkTxBytes: number;
  nodeCpuPercent: number;
  nodeMemoryPercent: number;
  nodeNetworkSampled: boolean;
  nodeNetworkRxBytes: number;
  nodeNetworkTxBytes: number;
};

function projectResourceSummary(project: Project) {
  const cpu = project.spec.cpu ?? 0;
  const ramMB = project.spec.ram_mb ?? 0;
  if (cpu > 0 && ramMB > 0) {
    return `${cpu} vCPU / ${formatBytes(ramMB * 1024 * 1024)}`;
  }
  return project.spec.enforce_limits ? "limits on" : "no limits";
}

export function FleetDashboardPage() {
  const { projectList, hosts, fleetMetrics, advisorFindings, complianceReport, projects, provisionerStatus, routeToProject } = useDashboardContext();
  const navigate = useNavigate();
  const metrics = fleetMetrics.data;
  const observed = metrics?.observed;
  const nodeObserved = metrics?.node_observed?.[0];
  const host = hosts.data?.[0];
  const isCompose = provisionerStatus.data?.provisioner === "compose";
  const healthy = projectList.filter((project) => project.status === "healthy").length;
  const degraded = projectList.filter((project) => project.status === "degraded").length;
  const errored = projectList.filter((project) => project.status === "error").length;
  const [history, setHistory] = useState<FleetHistoryPoint[]>([]);

  const live = metrics ? liveUsageFromMetrics(metrics) : emptyLiveUsage();
  const networkRate = useMemo(() => networkRateFromHistory(history), [history]);
  const nodeNetworkRate = useMemo(() => nodeNetworkRateFromHistory(history), [history]);
  // Distinguish "no rate yet" (only one sample collected) from a real measured 0.
  const networkRateReady = history.length >= 2;
  const nodeNetworkRateReady = history.filter((point) => point.nodeNetworkSampled).length >= 2;
  const findings = advisorFindings.data?.length ?? 0;
  const controlsPassed = complianceReport.data?.summary.passed ?? 0;
  const controlsTotal = complianceReport.data?.summary.total ?? 0;
  const hasProjectTelemetry = projectList.length > 0 || (observed?.projects_sampled ?? 0) > 0 || (observed?.stale_projects ?? 0) > 0;
  const hasDiskTelemetry = (observed?.disk_limit_bytes ?? 0) > 0;
  const nodeMemoryPercent = nodeObserved && nodeObserved.memory_total_bytes > 0 ? (nodeObserved.memory_used_bytes / nodeObserved.memory_total_bytes) * 100 : 0;
  const nodeDiskPercent = nodeObserved && nodeObserved.disk_total_bytes > 0 ? (nodeObserved.disk_used_bytes / nodeObserved.disk_total_bytes) * 100 : 0;
  const reservationStatus = reservationStatusFromMetrics(metrics);
  const databaseIngress = databaseIngressStatus(metrics);
  const nodeLabel = isCompose ? "Local Docker node" : (host ? host.name : `${metrics?.hosts ?? 0} nodes`);
  const nodeDetail = isCompose ? "single-node Compose runtime" : metrics ? `${metrics.host_capacity.cpu || "-"} vCPU / ${formatCapacityRAM(metrics.host_capacity.ram_mb)} RAM` : "Capacity pending";

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
      <AppPanel actions={metrics ? <time className="text-xs text-faint">{formatTime(metrics.sampled_at)}</time> : null} eyebrow="Dashboard" title="At a glance">
        {fleetMetrics.isLoading ? <p className="mt-4 text-sm text-muted">Loading dashboard...</p> : null}
        <div className="mt-4 metric-grid">
          <MetricCard label="Projects" value={`${projectList.length} ${projectList.length === 1 ? "project" : "projects"}`} detail={`${healthy} healthy / ${degraded} degraded / ${errored} errors`} tone={errored > 0 ? "danger" : degraded > 0 ? "warning" : "success"} />
          <MetricCard label={isCompose ? "Runtime" : "Nodes"} value={nodeLabel} detail={nodeDetail} />
          <Link className="rounded-md outline-none focus-visible:ring-2 focus-visible:ring-accent" to="/settings/$section" params={{ section: "db-ingress" }} title="Open database ingress settings">
            <MetricCard label="DB ingress →" value={databaseIngress.value} detail={databaseIngress.detail} tone={databaseIngress.tone} />
          </Link>
          <Link className="rounded-md outline-none focus-visible:ring-2 focus-visible:ring-accent" to="/security" title="Open the Security page">
            <MetricCard label="Security →" value={`${findings} ${findings === 1 ? "finding" : "findings"}`} detail={`${controlsPassed}/${controlsTotal || "-"} controls pass`} tone={findings > 0 ? "warning" : "success"} />
          </Link>
        </div>
      </AppPanel>

      <div className="grid grid-cols-[minmax(0,1.25fr)_minmax(360px,0.75fr)] gap-6 max-xl:grid-cols-1">
        <AppPanel
          actions={
            <Button onClick={() => void navigate({ to: "/hosts" })} size="sm" type="button" variant="secondary">
              <Server size={14} />
              Node
            </Button>
          }
          description="Live OS-level CPU, memory and disk measured on the node now — not what projects reserve."
          eyebrow={isCompose ? "Compose" : "Infrastructure"}
          title={isCompose ? "Local node usage" : "Node usage"}
        >
          {nodeObserved ? (
            <>
              <div className="mt-4 grid grid-cols-2 gap-3 max-lg:grid-cols-1">
                <ResourceMeter icon={Cpu} label="Node CPU" value={formatPercent(nodeObserved.cpu_percent)} detail={`${formatDecimal(nodeObserved.cpu_used_cores)} / ${nodeObserved.cpu_capacity_cores || metrics?.host_capacity.cpu || "-"} vCPU`} footer={formatOptionalTime(nodeObserved.sampled_at, "sample pending")} percent={nodeObserved.cpu_percent} />
                <ResourceMeter icon={Gauge} label="Node memory" value={formatPercent(nodeMemoryPercent)} detail={`${formatBytes(nodeObserved.memory_used_bytes)} / ${formatBytes(nodeObserved.memory_total_bytes)}`} footer="used" percent={nodeMemoryPercent} />
                <ResourceMeter icon={HardDrive} label="Node disk" value={formatPercent(nodeDiskPercent)} detail={`${formatBytes(nodeObserved.disk_used_bytes)} / ${formatBytes(nodeObserved.disk_total_bytes)}`} footer={`${formatBytes(nodeObserved.disk_available_bytes)} free`} percent={nodeDiskPercent} />
                {nodeObserved.network_sampled ? <NetworkRow icon={Network} ready={nodeNetworkRateReady} rate={nodeNetworkRate} title="Node network" /> : null}
                <InfoRow icon={Server} title={isCompose ? "Local node" : "Observed node"} detail={isCompose ? "Single-node Docker Compose runtime" : host?.address ?? "node address unavailable"} value={isCompose ? "Docker Compose" : nodeObserved.source.replace(/-/g, " ")} />
              </div>
              <TelemetryLineChart ariaLabel="Recent node CPU and memory utilization" points={history.map((point) => ({ sampledAt: point.sampledAt, cpu: point.nodeCpuPercent, memory: point.nodeMemoryPercent }))} title="Recent node usage" />
            </>
          ) : (
            <EmptyState
              className="mt-4"
              icon={Server}
              title="Waiting for node sample"
              description={host ? `No telemetry received from ${host.name} yet. The node agent reports usage once it starts sampling; this can take a minute after first boot.` : "No local node is registered yet, so there is nothing to sample. Register the local Docker node to start collecting live usage."}
              action={host ? undefined : <Button onClick={() => void navigate({ to: "/hosts" })} size="sm" type="button"><Server size={14} />Register node</Button>}
            />
          )}
        </AppPanel>

        <AppPanel actions={<Badge variant={mcardTone(reservationStatus.tone) === "default" ? "muted" : mcardTone(reservationStatus.tone)}>{reservationStatus.label}</Badge>} description="Capacity committed to projects — a budget, not live load. Bars warn at 85% and turn red at 100%." eyebrow={isCompose ? "Local capacity" : "Capacity"} title="Reservations">
          <div className="mt-4 grid gap-3">
            <ReservationMeter capacity={metrics?.host_capacity.cpu ?? 0} icon={Cpu} label="CPU reserved" render={(value) => `${value.toLocaleString()} vCPU`} used={metrics?.host_used.cpu ?? 0} />
            <ReservationMeter capacity={metrics?.host_capacity.ram_mb ?? 0} icon={Gauge} label="RAM reserved" render={(value) => formatBytes(value * 1024 * 1024)} used={metrics?.host_used.ram_mb ?? 0} />
            <ReservationMeter capacity={metrics?.host_capacity.disk_gb ?? 0} icon={HardDrive} label="Disk reserved" render={(value) => `${value.toLocaleString()} GB`} used={metrics?.host_used.disk_gb ?? 0} />
            <ReservationMeter capacity={metrics?.host_capacity.projects ?? 0} icon={Database} label="Project slots" render={(value) => value.toLocaleString()} used={metrics?.host_used.projects ?? projectList.length} />
          </div>
        </AppPanel>
      </div>

      <CollapsibleCard
        actions={
          <div className="flex items-center gap-2">
            <Badge variant={databaseIngress.tone}>{databaseIngress.value}</Badge>
            <Link to="/settings/$section" params={{ section: "db-ingress" }}>
              <Button size="sm" type="button" variant={databaseIngress.tone === "danger" ? "default" : "secondary"}>
                <ShieldAlert size={14} />
                {databaseIngress.tone === "danger" ? "Configure allowlist" : "Ingress settings"}
              </Button>
            </Link>
          </div>
        }
        defaultOpen={databaseIngress.tone === "danger"}
        description={databaseIngress.tone === "danger" ? "Raw Postgres is reachable from any network with no CIDR allowlist — restrict access." : "Listener addresses for raw Postgres and the connection pooler."}
        eyebrow="Network access"
        title="Database ingress"
      >
        <div className="mt-4 grid grid-cols-3 gap-3 max-xl:grid-cols-1">
          <InfoRow icon={ShieldAlert} title="Raw database access" detail={databaseIngress.detail} value={databaseIngress.value} />
          <InfoRow icon={Database} title="Postgres listener" detail={metrics?.database_ingress?.postgres_public ? "reachable outside loopback" : "loopback only"} value={metrics?.database_ingress?.postgres_addr ?? "127.0.0.1:5432"} />
          <InfoRow icon={Network} title="Pooler listener" detail={metrics?.database_ingress?.pooler_public ? "reachable outside loopback" : "loopback only"} value={metrics?.database_ingress?.pooler_addr ?? "127.0.0.1:6543"} />
        </div>
      </CollapsibleCard>

      <AppPanel description="Aggregated usage sampled from project containers — distinct from whole-node load and from reservations." eyebrow="Project telemetry" title="Project runtime usage">
        {hasProjectTelemetry ? (
          <>
            <div className="mt-4 grid grid-cols-3 gap-3 max-xl:grid-cols-1">
              <ResourceMeter icon={Cpu} label="Project CPU" value={formatPercent(live.cpuCapacityPercent)} detail={`${formatDecimal(live.cpuVCpu)} / ${metrics?.host_capacity.cpu || "-"} vCPU`} footer={`${observed?.projects_sampled ?? 0} sampled`} percent={live.cpuCapacityPercent} />
              <ResourceMeter icon={Gauge} label="Project memory" value={formatPercent(live.memoryCapacityPercent)} detail={`${formatBytes(observed?.memory_bytes ?? 0)} / ${formatCapacityRAM(metrics?.host_capacity.ram_mb ?? 0)}`} footer="of node RAM" percent={live.memoryCapacityPercent} />
              <NetworkRow icon={Network} ready={networkRateReady} rate={networkRate} title="Project network" />
              {hasDiskTelemetry ? <InfoRow icon={HardDrive} title="Project disk" detail={`${formatBytes(observed?.disk_used_bytes ?? 0)} of ${formatBytes(observed?.disk_limit_bytes ?? 0)}`} value={formatPercent(((observed?.disk_used_bytes ?? 0) / (observed?.disk_limit_bytes ?? 1)) * 100)} /> : null}
              <InfoRow title="Fresh samples" detail={`${observed?.projects_sampled ?? 0} active projects sampled`} value={formatOptionalTime(observed?.latest_sampled_at, "pending")} />
              <InfoRow title="Stale samples" detail={`${observed?.stale_projects ?? 0} active projects older than ${observed?.stale_after_seconds ?? 0}s`} value={formatOptionalTime(observed?.oldest_sampled_at, "none")} />
            </div>
            {history.length > 1 ? <TelemetryLineChart ariaLabel="Recent project CPU and memory utilization" points={history.map((point) => ({ sampledAt: point.sampledAt, cpu: point.cpuCapacityPercent, memory: point.memoryCapacityPercent }))} title="Recent project usage" /> : null}
          </>
        ) : (
          <EmptyState
            className="mt-4"
            icon={Database}
            title="No project runtime samples yet"
            description="No active projects are reporting container telemetry. Deploy a project to start seeing sampled CPU, memory and network usage here."
            action={<Button onClick={() => void navigate({ to: "/projects/new" })} size="sm" type="button"><Plus size={14} />Create project</Button>}
          />
        )}
      </AppPanel>

      <FleetTrafficPanel />

      <AppPanel
        actions={
          <Button onClick={() => void navigate({ to: "/projects" })} size="sm" type="button" variant="secondary">
            <Database size={14} />
            All projects
          </Button>
        }
        eyebrow="Projects"
        title="Active projects"
      >
        <div className="mt-4 grid gap-2">
          {projects.isLoading ? <p className="text-sm text-muted">Loading projects...</p> : null}
          {!projects.isLoading && projectList.length === 0 ? (
            <EmptyState
              icon={Database}
              title="No projects deployed"
              description="No projects have been created on this control plane yet. Create your first project to deploy a Supabase stack."
              action={<Button onClick={() => void navigate({ to: "/projects/new" })} size="sm" type="button"><Plus size={14} />Create project</Button>}
            />
          ) : null}
          {projectList.slice(0, 8).map((project) => (
            <button className="usage-row text-left transition hover:border-border-strong hover:bg-surface-2" key={project.ref} onClick={() => routeToProject(project.ref)} type="button">
              <span className="min-w-0">
                <span className="flex min-w-0 items-center gap-2">
                  <span className={`status-dot ${project.status === "healthy" ? "bg-success" : project.status === "error" ? "bg-danger" : "bg-warning"}`} />
                  <span className="truncate text-sm font-medium">{project.name}</span>
                  <StatusPill status={project.status} />
                </span>
	                <span className="mt-1 block truncate font-mono text-xs text-muted">{project.ref} / {project.spec.domain} / {projectResourceSummary(project)}</span>
              </span>
              <span className="flex items-center gap-2 text-xs text-muted">
                {project.runtime_status?.phase ? <span>{project.runtime_status.phase}</span> : null}
                <ArrowRight size={14} />
              </span>
            </button>
          ))}
          {projectList.length > 8 ? (
            <button className="usage-row text-left text-sm font-medium text-accent transition hover:border-border-strong hover:bg-surface-2" onClick={() => void navigate({ to: "/projects" })} type="button">
              <span>View all {projectList.length} projects</span>
              <span className="flex items-center gap-2 text-xs text-muted">+{projectList.length - 8} more<ArrowRight size={14} /></span>
            </button>
          ) : null}
        </div>
      </AppPanel>
    </div>
  );
}

function FleetTrafficPanel() {
  const traffic = useQuery({ queryKey: ["fleet-traffic"], queryFn: getFleetTraffic, refetchInterval: 15_000 });
  const d = traffic.data;
  const t = d?.totals;
  const rps = (n?: number) => (n !== undefined ? `${n.toFixed(n < 10 ? 2 : 0)}/s` : "—");
  const pct = (n?: number) => (n !== undefined ? `${(n * 100).toFixed(1)}%` : "—");
  const conns = d?.entrypoint_connections ?? {};
  const dbConns = (conns.postgres ?? 0) + (conns.pooler ?? 0);
  const certDays = d?.cert_expires_at ? Math.round((new Date(d.cert_expires_at).getTime() - Date.now()) / 86_400_000) : undefined;
  return (
    <AppPanel
      eyebrow="Traffic"
      title="Edge traffic"
      description="Live request flow across all projects, measured at the Traefik edge. DB/pooler shown as open TCP connections."
      actions={certDays !== undefined ? <StatusPill tone={certDays < 14 ? "danger" : certDays < 30 ? "warning" : "neutral"} label={`TLS ${certDays}d`} /> : undefined}
    >
      {d && !d.enabled ? (
        <p className="mt-3 text-sm text-muted">Edge traffic metrics are not enabled on this deployment.</p>
      ) : (
        <>
          <div className="mt-4 grid grid-cols-5 gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
            <MetricCard label="Requests/sec" value={rps(t?.requests_per_sec)} detail="all HTTP routes" />
            <MetricCard label="Error rate" value={pct(t?.error_rate)} tone={t && t.error_rate > 0.05 ? "danger" : t && t.error_rate > 0.01 ? "warning" : "default"} detail="4xx + 5xx" />
            <MetricCard label="Latency p95" value={t ? `${t.p95_ms.toFixed(0)} ms` : "—"} detail={`avg ${t ? t.avg_latency_ms.toFixed(0) : "—"} ms`} />
            <MetricCard label="Throughput" value={t ? `${formatBytes(t.bytes_out_per_sec)}/s` : "—"} detail={`out · ${t ? formatBytes(t.bytes_in_per_sec) : "—"}/s in`} />
            <MetricCard label="DB connections" value={String(Math.round(dbConns))} detail="postgres + pooler (open)" />
          </div>
          {d?.projects?.length ? (
            <div className="mt-4 grid gap-1">
              <p className="label">Busiest projects</p>
              {d.projects.slice(0, 8).map((p) => (
                <div className="usage-row" key={p.ref}>
                  <p className="truncate font-mono text-sm">{p.ref}</p>
                  <p className="text-right text-xs text-muted">{rps(p.requests_per_sec)} · {pct(p.error_rate)} err</p>
                </div>
              ))}
            </div>
          ) : (
            <p className="mt-3 text-xs text-faint">No HTTP traffic observed yet in the current window.</p>
          )}
        </>
      )}
    </AppPanel>
  );
}

function ReservationMeter({ capacity, icon, label, render, used }: { icon: LucideIcon; label: string; used: number; capacity: number; render: (value: number) => string }) {
  const percent = ratioPercent(used, capacity);
  const tone = utilizationTone(percent, capacity);
  return (
    <ResourceMeter
      detail={capacity > 0 ? `${formatPercent(percent)} of ${render(capacity)} reserved` : "capacity unset"}
      footer={
        capacity > 0 ? (
          <span className={`font-medium ${meterToneText(tone)}`}>
            {formatPercent(percent)}
            {percent >= 100 ? " · over limit" : percent >= 85 ? " · near 85% limit" : ""}
          </span>
        ) : (
          <span className="text-faint">no limit set</span>
        )
      }
      icon={icon}
      label={label}
      percent={Math.max(clampPercent(percent), used > 0 ? 3 : 0)}
      tone={tone}
      value={render(used)}
    />
  );
}

// MetricCard only knows default/success/warning/danger; map info+neutral to default.
function mcardTone(tone: Tone): "default" | "success" | "warning" | "danger" {
  return tone === "success" || tone === "warning" || tone === "danger" ? tone : "default";
}

function meterToneText(tone: Tone) {
  switch (tone) {
    case "danger":
      return "text-danger";
    case "warning":
      return "text-warning";
    case "success":
      return "text-success";
    default:
      return "text-muted";
  }
}

// Renders a network throughput value, or a neutral "collecting" pill while we
// still only have a single sample (so idle 0 B/s isn't confused with no data).
function NetworkRow({ icon, ready, rate, title }: { icon: LucideIcon; ready: boolean; rate: { rxBytesPerSecond: number; txBytesPerSecond: number }; title: string }) {
  if (!ready) {
    return <InfoRow icon={icon} title={title} detail="Collecting samples — needs a second reading" value="collecting…" />;
  }
  return <InfoRow icon={icon} title={title} detail={`${formatBytes(rate.rxBytesPerSecond)}/s in / ${formatBytes(rate.txBytesPerSecond)}/s out`} value={`${formatBytes(rate.rxBytesPerSecond + rate.txBytesPerSecond)}/s`} />;
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
  const node = metrics.node_observed?.[0];
  const nodeMemoryPercent = node && node.memory_total_bytes > 0 ? (node.memory_used_bytes / node.memory_total_bytes) * 100 : 0;
  return {
    sampledAt: metrics.sampled_at,
    cpuCapacityPercent: live.cpuCapacityPercent,
    memoryCapacityPercent: live.memoryCapacityPercent,
    networkRxBytes: metrics.observed?.network_rx_bytes ?? 0,
    networkTxBytes: metrics.observed?.network_tx_bytes ?? 0,
    nodeCpuPercent: node?.cpu_percent ?? 0,
    nodeMemoryPercent,
    nodeNetworkSampled: node?.network_sampled ?? false,
    nodeNetworkRxBytes: node?.network_rx_bytes ?? 0,
    nodeNetworkTxBytes: node?.network_tx_bytes ?? 0,
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

function nodeNetworkRateFromHistory(points: FleetHistoryPoint[]) {
  const sampled = points.filter((point) => point.nodeNetworkSampled);
  const current = sampled[sampled.length - 1];
  const previous = sampled[sampled.length - 2];
  if (!current || !previous) {
    return { rxBytesPerSecond: 0, txBytesPerSecond: 0 };
  }
  const seconds = Math.max(1, (new Date(current.sampledAt).getTime() - new Date(previous.sampledAt).getTime()) / 1000);
  return {
    rxBytesPerSecond: Math.max(0, (current.nodeNetworkRxBytes - previous.nodeNetworkRxBytes) / seconds),
    txBytesPerSecond: Math.max(0, (current.nodeNetworkTxBytes - previous.nodeNetworkTxBytes) / seconds),
  };
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

function databaseIngressStatus(metrics: FleetMetrics | undefined): { value: string; detail: string; tone: "default" | "success" | "warning" | "danger" } {
  const ingress = metrics?.database_ingress;
  if (!ingress || !ingress.public) {
    return {
      value: "Loopback only",
      detail: "DB/pooler ports aren't published; no project is reachable externally",
      tone: "default",
    };
  }
  // Host ports are published; per-project exposure (private by default) is the
  // real gate — so this is informational, not an alarm.
  return {
    value: "Host ports published",
    detail: "Each project stays private until opened (Config → Network)",
    tone: "default",
  };
}

// Per-resource utilization tone: >=100% overcommitted (danger), >=85% near
// limit (warning), otherwise within capacity (success). "neutral" only when no
// capacity is configured yet, so the bar isn't a misleading flat accent blue.
function utilizationTone(percent: number, capacity: number): Tone {
  if (capacity <= 0) return "neutral";
  if (percent >= 100) return "danger";
  if (percent >= 85) return "warning";
  return "success";
}

function reservationStatusFromMetrics(metrics: FleetMetrics | undefined): { label: string; tone: Tone; worstResource: string; worstPercent: number } {
  if (!metrics) {
    return { label: "pending", tone: "neutral", worstResource: "Reservations", worstPercent: 0 };
  }
  const resources: Array<{ name: string; percent: number }> = [
    { name: "CPU", percent: ratioPercent(metrics.host_used.cpu, metrics.host_capacity.cpu) },
    { name: "RAM", percent: ratioPercent(metrics.host_used.ram_mb, metrics.host_capacity.ram_mb) },
    { name: "Disk", percent: ratioPercent(metrics.host_used.disk_gb, metrics.host_capacity.disk_gb) },
    { name: "Project slots", percent: ratioPercent(metrics.host_used.projects, metrics.host_capacity.projects) },
  ];
  const worst = resources.reduce((acc, item) => (item.percent > acc.percent ? item : acc), resources[0]);
  const peak = worst.percent;
  const label = peak >= 100 ? "overcommitted" : peak >= 85 ? "near limit" : "within capacity";
  const tone: Tone = peak >= 100 ? "danger" : peak >= 85 ? "warning" : "success";
  return { label, tone, worstResource: worst.name, worstPercent: peak };
}

function ratioPercent(used: number, capacity: number) {
  return capacity > 0 ? (used / capacity) * 100 : 0;
}

function formatOptionalTime(value: string | undefined, fallback: string) {
  if (!value) {
    return fallback;
  }
  const timestamp = new Date(value).getTime();
  if (!Number.isFinite(timestamp) || timestamp <= 0) {
    return fallback;
  }
  return formatTime(value);
}
