import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Activity, Database, Globe2, KeyRound, Network, Play, RotateCcw, Shield } from "lucide-react";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { ResourceMeter } from "../../components/app/resource-meter";
import { TelemetryLineChart } from "../../components/charts/telemetry-line-chart";
import { Badge } from "../../components/ui/badge";
import { DbExposureBadge } from "../../components/db-exposure-badge";
import { Button, buttonVariants } from "../../components/ui/button";
import { CardButton } from "../../components/ui/card-button";
import { CollapsibleCard } from "../../components/ui/collapsible-card";
import { RevealField } from "../../components/ui/reveal-field";
import { RuntimeLink } from "../../components/runtime-link";
import { StatusPill } from "../../components/ui/status-pill";
import { auditProjectSecretCopy, getProjectStats, getProjectTraffic } from "../../api";
import { useDashboardContext } from "../../lib/dashboard-context";
import { formatBytes, formatTime } from "../../lib/format";
import type { ProjectTab } from "../../lib/project-config";
import type { ConnectPayload, Project, ProjectMetrics } from "../../types";
import { ProjectPage } from "./layout";
import { RuntimeStatusPanel } from "./side-panels";

type ProjectTelemetryPoint = {
  projectRef: string;
  sampledAt: string;
  cpuPercent: number;
  memoryPercent: number;
};

export function ProjectOverviewPage() {
  const { activeProject, connect, projectMetrics, routeToProject } = useDashboardContext();
  const [telemetryHistory, setTelemetryHistory] = useState<ProjectTelemetryPoint[]>([]);
  const openTab = (tab: ProjectTab) => activeProject && routeToProject(activeProject.ref, tab);

  useEffect(() => {
    const point = telemetryPointFromMetrics(projectMetrics.data);
    if (!point) {
      return;
    }
    setTelemetryHistory((current) => {
      if (current[0]?.projectRef && current[0].projectRef !== point.projectRef) {
        return [point];
      }
      if (current[current.length - 1]?.sampledAt === point.sampledAt) {
        return current;
      }
      return [...current.filter((item) => item.sampledAt !== point.sampledAt), point].slice(-24);
    });
  }, [projectMetrics.data]);

  return (
    <ProjectPage>
      <ProjectStatusStrip project={activeProject} metrics={projectMetrics.data} loading={projectMetrics.isLoading} studioUrl={connect.data?.studio_url} />
      <NextStepsStrip project={activeProject} metrics={projectMetrics.data} onOpenTab={openTab} />
      <ObservedMetricsPanel history={telemetryHistory} metrics={projectMetrics.data} loading={projectMetrics.isLoading} />
      <OperationalSurfacePanel metrics={projectMetrics.data} loading={projectMetrics.isLoading} onOpenTab={openTab} />
      <DatabasePanel projectRef={activeProject?.ref} status={activeProject?.status} />
      <StoragePanel projectRef={activeProject?.ref} status={activeProject?.status} />
      <TrafficPanel projectRef={activeProject?.ref} />
      <ConnectionBasicsPanel payload={connect.data} loading={connect.isLoading} onOpenConnect={() => openTab("connect")} project={activeProject} />
      <RuntimeStatusPanel project={activeProject} />
    </ProjectPage>
  );
}

function ProjectStatusStrip({ loading, metrics, project, studioUrl }: { project?: Project; metrics?: ProjectMetrics; loading: boolean; studioUrl?: string }) {
  return (
    <section className="rounded-md border border-border bg-surface px-3 py-2">
      <div className="flex min-h-9 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <span className={`status-dot ${project?.status === "healthy" ? "bg-success" : project?.status === "error" ? "bg-danger" : "bg-warning"}`} />
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">{project?.name ?? "Project"}</p>
            <p className="truncate font-mono text-xs text-faint">{project?.ref ?? "loading"}</p>
          </div>
        </div>
        <div className="flex min-w-0 flex-wrap items-center justify-end gap-2 text-xs text-muted">
          <StatusPill label={metrics?.status ?? project?.status ?? (loading ? "loading" : "-")} status={metrics?.status ?? project?.status} />
          <DbExposureBadge mode={project?.db_ingress_mode} />
          {project ? <Badge variant="muted">{project.spec.resource_tier}</Badge> : null}
          {project ? <Badge variant="muted">{project.spec.profile}</Badge> : null}
          {metrics ? <span className="font-mono text-faint">{metrics.resources.cpu} vCPU · {formatBytes(metrics.resources.ram_mb * 1024 * 1024)} RAM · {formatBytes(metrics.db_allocated_bytes)} disk</span> : null}
          {studioUrl && project ? (
            <RuntimeLink className={buttonVariants({ variant: "secondary", size: "sm" })} label="Open Studio" projectRef={project.ref} url={studioUrl} />
          ) : null}
        </div>
      </div>
    </section>
  );
}

// Surfaces the most urgent "what do I do next" actions on the landing page so a
// paused stack or a project with no backups gets a clear, in-content CTA instead
// of forcing the user to discover it in the lifecycle side panel.
function NextStepsStrip({ metrics, onOpenTab, project }: { project?: Project; metrics?: ProjectMetrics; onOpenTab: (tab: ProjectTab) => void }) {
  if (!project) {
    return null;
  }
  const steps: Array<{ key: string; label: string; detail: string; icon: typeof Play; tab: ProjectTab; cta: string }> = [];
  if (project.status === "paused") {
    steps.push({ key: "resume", label: "Project is paused", detail: "Resume the stack to restore API, database, and runtime traffic.", icon: Play, tab: "config", cta: "Manage runtime" });
  }
  if (metrics && metrics.backups === 0) {
    steps.push({ key: "backups", label: "No backups yet", detail: "Capture a first backup so this project is recoverable.", icon: RotateCcw, tab: "backups", cta: "Open backups" });
  }
  if (steps.length === 0) {
    return null;
  }
  return (
    <section className="grid gap-2">
      {steps.map((step) => (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-warning/50 bg-warning/5 px-3 py-2" key={step.key}>
          <div className="flex min-w-0 items-center gap-2">
            <step.icon className="text-warning" size={15} />
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{step.label}</p>
              <p className="truncate text-xs text-muted">{step.detail}</p>
            </div>
          </div>
          <Button onClick={() => onOpenTab(step.tab)} size="sm" type="button" variant="secondary">
            {step.cta}
          </Button>
        </div>
      ))}
    </section>
  );
}

function ObservedMetricsPanel({ history, loading, metrics }: { history: ProjectTelemetryPoint[]; metrics?: ProjectMetrics; loading: boolean }) {
  const observed = metrics?.observed;
  const hasDiskSample = Boolean(observed && (observed.disk_used_bytes > 0 || observed.disk_limit_bytes > 0));
  const memoryPercent = observed && observed.memory_limit_bytes > 0 ? (observed.memory_bytes / observed.memory_limit_bytes) * 100 : 0;
  const diskPercent = hasDiskSample && observed && observed.disk_limit_bytes > 0 ? (observed.disk_used_bytes / observed.disk_limit_bytes) * 100 : 0;
  return (
    <AppPanel actions={observed ? <time className="text-xs text-faint">{formatTime(observed.sampled_at)}</time> : null} eyebrow="Monitoring" title="Resource telemetry">
      {loading ? <p className="mt-4 text-sm text-muted">Loading project metrics...</p> : null}
      <div className="mt-4 grid grid-cols-3 gap-3 max-lg:grid-cols-1">
        <ResourceMeter label="CPU" value={observed ? `${observed.cpu_percent.toFixed(1)}%` : "No sample"} detail={observed ? observed.source : "Collector pending"} footer={observed ? "observed" : "reserved"} percent={observed ? Math.min(100, observed.cpu_percent) : 0} />
        <ResourceMeter label="Memory" value={observed ? `${memoryPercent.toFixed(1)}%` : "No sample"} detail={observed && observed.memory_limit_bytes > 0 ? `${formatBytes(observed.memory_bytes)} of ${formatBytes(observed.memory_limit_bytes)}` : "Collector pending"} footer={observed ? "observed" : "reserved"} percent={memoryPercent} />
        <ResourceMeter label="Disk" value={hasDiskSample ? `${diskPercent.toFixed(1)}%` : "No volume sample"} detail={hasDiskSample && observed && observed.disk_limit_bytes > 0 ? `${formatBytes(observed.disk_used_bytes)} of ${formatBytes(observed.disk_limit_bytes)}` : metrics ? `${formatBytes(metrics.db_allocated_bytes)} reserved` : "Collector pending"} footer={hasDiskSample ? "observed" : "reserved"} percent={diskPercent} />
      </div>
      <TelemetryLineChart ariaLabel="Recent project CPU and memory utilization" points={history.map((point) => ({ sampledAt: point.sampledAt, cpu: point.cpuPercent, memory: point.memoryPercent }))} title="Recent telemetry" />
      {!observed ? (
        <p className="mt-3 text-xs text-faint">
          Showing reserved capacity until a Compose or Kubernetes telemetry collector records live samples.
        </p>
      ) : null}
    </AppPanel>
  );
}

function ConnectionBasicsPanel({ loading, onOpenConnect, payload, project }: { payload?: ConnectPayload; loading: boolean; onOpenConnect: () => void; project?: Project }) {
  const ref = project?.ref ?? "";
  const anonKey = payload?.api_keys.anon ?? payload?.api_keys.anon_key ?? payload?.api_keys.publishable ?? "";
  return (
    <AppPanel
      actions={
        <Button onClick={onOpenConnect} size="sm" type="button" variant="secondary">
          <KeyRound size={14} />
          Full connect
        </Button>
      }
      eyebrow="Connection basics"
      title="Connect"
    >
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading connection basics...</p> : null}
        {!loading && !payload ? <p className="text-sm text-muted">Connection details unavailable.</p> : null}
        {payload ? (
          <>
            {payload.api_url ? <RevealField label="API URL" sensitive={false} value={payload.api_url} /> : null}
            {anonKey ? <RevealField hint="Public client key" label="anon / publishable key" onCopy={() => auditProjectSecretCopy(ref, "anon_key")} sensitive value={anonKey} /> : null}
            {payload.studio_url ? <RevealField label="Studio" sensitive={false} value={payload.studio_url} /> : null}
          </>
        ) : null}
      </div>
    </AppPanel>
  );
}

function useProjectStats(projectRef?: string, status?: string) {
  return useQuery({
    queryKey: ["project-stats", projectRef],
    queryFn: () => getProjectStats(projectRef as string),
    enabled: Boolean(projectRef) && status !== "paused",
    refetchInterval: 30_000,
  });
}

const num = (n?: number, ready = true) => (ready && n !== undefined ? Math.round(n).toLocaleString() : "—");
const size = (n?: number, ready = true) => (ready && n !== undefined ? formatBytes(n) : "—");
const rate = (n?: number, ready = true) => (ready && n !== undefined ? `${n.toFixed(n < 10 ? 1 : 0)}/s` : "—");

function DatabasePanel({ projectRef, status }: { projectRef?: string; status?: string }) {
  const stats = useProjectStats(projectRef, status);
  const d = stats.data;
  const ready = Boolean(d?.available);
  const c = d?.connections;
  const connPct = c && c.max > 0 ? c.total / c.max : 0;
  return (
    <CollapsibleCard
      eyebrow="Database"
      title="Postgres"
      description="Size, connections, throughput, and the largest tables, queried live from the project database."
      actions={d?.available ? <span className="font-mono text-xs text-muted">{size(d.db_size_bytes)} · {c?.total}/{c?.max} conns</span> : null}
    >
      <div className="mt-4 grid grid-cols-4 gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
        <MetricCard label="Database size" value={size(d?.db_size_bytes, ready)} detail={`${num(d?.table_count, ready)} public tables`} />
        <MetricCard label="Connections" value={c ? `${c.total} / ${c.max}` : "—"} tone={connPct > 0.85 ? "danger" : connPct > 0.6 ? "warning" : "default"} detail={c ? `${c.active} active · ${c.idle} idle${c.idle_in_txn ? ` · ${c.idle_in_txn} idle-txn` : ""}` : "active / max"} />
        <MetricCard label="Cache hit" value={ready && d ? `${(d.cache_hit_ratio * 100).toFixed(1)}%` : "—"} tone={ready && d && d.cache_hit_ratio < 0.9 ? "warning" : "default"} detail="lifetime buffer hits" />
        <MetricCard label="Transactions" value={rate(d?.txns_per_sec, ready)} detail={`${rate(d?.tuples_written_per_sec, ready)} rows written`} />
      </div>
      {d?.top_tables?.length ? (
        <div className="mt-4 grid gap-1">
          <div className="flex items-center justify-between"><p className="label">Largest tables</p>{d.deadlocks > 0 ? <span className="text-xs text-warning">{d.deadlocks} deadlocks</span> : null}</div>
          {d.top_tables.map((t) => (
            <div className="usage-row" key={t.name}>
              <p className="truncate font-mono text-sm">{t.name}</p>
              <p className="text-right text-xs text-muted">{size(t.size_bytes)} · {num(t.rows)} rows</p>
            </div>
          ))}
        </div>
      ) : null}
      {stats.isLoading && !d ? <p className="mt-3 text-xs text-muted">Probing project database…</p> : null}
      {d && !d.available ? <p className="mt-3 text-xs text-faint">Live stats unavailable — the project may be paused or its database unreachable.</p> : null}
    </CollapsibleCard>
  );
}

function StoragePanel({ projectRef, status }: { projectRef?: string; status?: string }) {
  const stats = useProjectStats(projectRef, status);
  const d = stats.data;
  const ready = Boolean(d?.available);
  return (
    <CollapsibleCard
      eyebrow="Storage"
      title="Object storage"
      description="S3-compatible storage usage, broken down per bucket."
      actions={ready && d ? <span className="font-mono text-xs text-muted">{size(d.storage_bytes)} · {num(d.objects)} files</span> : null}
    >
      <div className="mt-4 grid grid-cols-3 gap-2 max-sm:grid-cols-1">
        <MetricCard label="Storage used" value={size(d?.storage_bytes, ready)} detail="across all buckets" />
        <MetricCard label="Files" value={num(d?.objects, ready)} detail="objects stored" />
        <MetricCard label="Buckets" value={num(d?.buckets, ready)} detail="storage buckets" />
      </div>
      {d?.bucket_breakdown?.length ? (
        <div className="mt-4 grid gap-1">
          <p className="label">Per bucket</p>
          {d.bucket_breakdown.map((b) => (
            <div className="usage-row" key={b.name}>
              <p className="flex min-w-0 items-center gap-2 truncate text-sm font-medium">{b.name}<Badge variant="muted">{b.public ? "public" : "private"}</Badge></p>
              <p className="text-right text-xs text-muted">{size(b.bytes)} · {num(b.objects)} files</p>
            </div>
          ))}
        </div>
      ) : ready ? (
        <p className="mt-3 text-xs text-faint">No buckets created yet.</p>
      ) : null}
    </CollapsibleCard>
  );
}

function TrafficPanel({ projectRef }: { projectRef?: string }) {
  const traffic = useQuery({
    queryKey: ["project-traffic", projectRef],
    queryFn: () => getProjectTraffic(projectRef as string),
    enabled: Boolean(projectRef),
    refetchInterval: 15_000,
  });
  const d = traffic.data;
  const t = d?.totals;
  const rps = (n?: number) => (n !== undefined ? `${n.toFixed(n < 10 ? 2 : 0)}/s` : "—");
  const pct = (n?: number) => (n !== undefined ? `${(n * 100).toFixed(1)}%` : "—");
  const ms = (n?: number) => (n !== undefined ? `${n.toFixed(0)} ms` : "—");
  const bps = (n?: number) => (n !== undefined ? `${formatBytes(n)}/s` : "—");
  const certDays = d?.cert_expires_at ? Math.round((new Date(d.cert_expires_at).getTime() - Date.now()) / 86_400_000) : undefined;
  return (
    <CollapsibleCard
      eyebrow="Traffic"
      title="Edge traffic (Traefik)"
      description="Request rate, latency percentiles, throughput, and status mix across this project's public routes."
      actions={
        <div className="flex items-center gap-2">
          {t ? <span className="font-mono text-xs text-muted">{rps(t.requests_per_sec)} · p95 {ms(t.p95_ms)}</span> : null}
          {certDays !== undefined ? <StatusPill tone={certDays < 14 ? "danger" : certDays < 30 ? "warning" : "neutral"} label={`TLS ${certDays}d`} /> : null}
        </div>
      }
    >
      {d && !d.enabled ? (
        <p className="mt-3 text-sm text-muted">Edge traffic metrics are not enabled on this deployment.</p>
      ) : (
        <>
          <div className="mt-4 grid grid-cols-4 gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
            <MetricCard label="Requests/sec" value={rps(t?.requests_per_sec)} detail={`${t ? Math.round(t.requests_total).toLocaleString() : "—"} total`} />
            <MetricCard label="Error rate" value={pct(t?.error_rate)} tone={t && t.error_rate > 0.05 ? "danger" : t && t.error_rate > 0.01 ? "warning" : "default"} detail="4xx + 5xx" />
            <MetricCard label="Latency p95 / p99" value={t ? `${ms(t.p95_ms)}` : "—"} detail={`avg ${ms(t?.avg_latency_ms)}`} />
            <MetricCard label="Throughput" value={bps(t ? t.bytes_out_per_sec : undefined)} detail={`out · ${bps(t?.bytes_in_per_sec)} in`} />
          </div>
          {t ? (
            <div className="mt-3 flex flex-wrap gap-2 text-xs">
              <span className="rounded-md border border-border px-2 py-1">2xx <span className="font-mono text-success">{Math.round(t.status_2xx).toLocaleString()}</span></span>
              <span className="rounded-md border border-border px-2 py-1">3xx <span className="font-mono text-muted">{Math.round(t.status_3xx).toLocaleString()}</span></span>
              <span className="rounded-md border border-border px-2 py-1">4xx <span className="font-mono text-warning">{Math.round(t.status_4xx).toLocaleString()}</span></span>
              <span className="rounded-md border border-border px-2 py-1">5xx <span className="font-mono text-danger">{Math.round(t.status_5xx).toLocaleString()}</span></span>
            </div>
          ) : null}
          {d?.routes?.length ? (
            <div className="mt-4 grid gap-1">
              <p className="label">Per route (host)</p>
              {d.routes.map((rt) => (
                <div className="usage-row" key={rt.route}>
                  <p className="truncate text-sm font-medium">{rt.route}</p>
                  <p className="text-right text-xs text-muted">{rps(rt.requests_per_sec)} · {pct(rt.error_rate)} err · p95 {ms(rt.p95_ms)} · {bps(rt.bytes_out_per_sec)}</p>
                </div>
              ))}
            </div>
          ) : (
            <p className="mt-3 text-xs text-faint">No traffic observed yet in the current window.</p>
          )}
        </>
      )}
    </CollapsibleCard>
  );
}

function OperationalSurfacePanel({ loading, metrics, onOpenTab }: { metrics?: ProjectMetrics; loading: boolean; onOpenTab: (tab: ProjectTab) => void }) {
  return (
    <AppPanel actions={metrics ? <time className="text-xs text-faint">{formatTime(metrics.sampled_at)}</time> : null} eyebrow="Surface area" title="Project capabilities">
      {loading ? <p className="mt-4 text-sm text-muted">Loading counters...</p> : null}
      {metrics ? (
        <div className="mt-4 grid grid-cols-4 gap-2 max-lg:grid-cols-2 max-sm:grid-cols-1">
          <SurfaceTile icon={Database} label="Replicas" value={`${metrics.read_replicas}`} detail="read scaling" onClick={() => onOpenTab("database")} />
          <SurfaceTile icon={Globe2} label="Routes" value={`${metrics.routes}`} detail={`${metrics.custom_domains} domains`} onClick={() => onOpenTab("config")} />
          <SurfaceTile icon={Network} label="Networks" value={`${metrics.network_connections}`} detail="private" onClick={() => onOpenTab("config")} />
          <SurfaceTile icon={Shield} label="Secrets" value={`${metrics.secrets}`} detail="handles" onClick={() => onOpenTab("connect")} />
          <SurfaceTile icon={Activity} label="Logs" value={`${metrics.project_log_events}`} detail="events" onClick={() => onOpenTab("logs")} />
          <SurfaceTile icon={RotateCcw} label="Backups" value={`${metrics.backups}`} detail="recovery points" onClick={() => onOpenTab("backups")} />
        </div>
      ) : null}
    </AppPanel>
  );
}

function SurfaceTile({ detail, icon: Icon, label, onClick, value }: { icon: typeof Activity; label: string; value: string; detail: string; onClick: () => void }) {
  return (
    <CardButton className="p-2.5" onClick={onClick}>
      <p className="label flex items-center gap-1">
        <Icon size={13} className="text-faint" />
        {label}
      </p>
      <p className="mt-1 truncate text-sm font-medium">{value}</p>
      <p className="mt-0.5 truncate text-xs text-faint">{detail}</p>
    </CardButton>
  );
}

function telemetryPointFromMetrics(metrics?: ProjectMetrics): ProjectTelemetryPoint | null {
  const observed = metrics?.observed;
  if (!observed) {
    return null;
  }
  return {
    projectRef: metrics.project_ref,
    sampledAt: observed.sampled_at,
    cpuPercent: observed.cpu_percent,
    memoryPercent: observed.memory_limit_bytes > 0 ? (observed.memory_bytes / observed.memory_limit_bytes) * 100 : 0,
  };
}
