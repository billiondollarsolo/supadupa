import { Activity, Boxes, Copy, Database, ExternalLink, Globe2, KeyRound, Network, Shield } from "lucide-react";
import { RuntimeLink } from "../../components/runtime-link";
import { useDashboardContext } from "../../lib/dashboard-context";
import { formatBytes, formatTime } from "../../lib/format";
import { useUIStore } from "../../lib/ui-store";
import type { ConnectPayload, Project, ProjectMetrics, TelemetrySample } from "../../types";
import { ProjectPage } from "./layout";

export function ProjectOverviewPage() {
  const { activeProject, connect, projectMetrics, routeToProject } = useDashboardContext();
  return (
    <ProjectPage>
      <ProjectStatusStrip project={activeProject} metrics={projectMetrics.data} loading={projectMetrics.isLoading} />
      <div className="grid grid-cols-[minmax(0,1fr)_340px] gap-4 max-xl:grid-cols-1">
        <div className="grid gap-4">
          <ObservedMetricsPanel metrics={projectMetrics.data} loading={projectMetrics.isLoading} />
          <OperationalSurfacePanel metrics={projectMetrics.data} loading={projectMetrics.isLoading} />
        </div>
        <div className="grid content-start gap-4">
          <ConnectionBasicsPanel payload={connect.data} loading={connect.isLoading} onOpenConnect={() => activeProject && routeToProject(activeProject.ref, "connect")} />
        </div>
      </div>
    </ProjectPage>
  );
}

function ProjectStatusStrip({ loading, metrics, project }: { project?: Project; metrics?: ProjectMetrics; loading: boolean }) {
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
          <span className={`pill ${project?.status === "healthy" ? "healthy" : project?.status === "error" ? "error" : "provisioning"}`}>{metrics?.status ?? project?.status ?? (loading ? "loading" : "-")}</span>
          {project ? <span className="pill">{project.spec.resource_tier}</span> : null}
          {project ? <span className="pill">{project.spec.profile}</span> : null}
          {metrics ? <span className="font-mono text-faint">{metrics.resources.cpu} vCPU · {formatBytes(metrics.resources.ram_mb * 1024 * 1024)} RAM · {formatBytes(metrics.db_allocated_bytes)} disk</span> : null}
        </div>
      </div>
    </section>
  );
}

function ObservedMetricsPanel({ loading, metrics }: { metrics?: ProjectMetrics; loading: boolean }) {
  const observed = metrics?.observed;
  const hasDiskSample = Boolean(observed && (observed.disk_used_bytes > 0 || observed.disk_limit_bytes > 0));
  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Monitoring</p>
          <h2>Resource telemetry</h2>
        </div>
        {observed ? <time className="text-xs text-faint">{formatTime(observed.sampled_at)}</time> : null}
      </div>
      {loading ? <p className="mt-4 text-sm text-muted">Loading project metrics...</p> : null}
      <div className="mt-4 grid grid-cols-3 gap-3 max-lg:grid-cols-1">
        <TelemetryCard label="CPU" observed={observed} value={observed ? `${observed.cpu_percent.toFixed(1)}%` : "No sample"} detail={observed ? observed.source : "Collector pending"} percent={observed ? Math.min(100, observed.cpu_percent) : 0} />
        <TelemetryCard label="Memory" observed={observed} value={observed ? formatBytes(observed.memory_bytes) : "No sample"} detail={observed && observed.memory_limit_bytes > 0 ? `of ${formatBytes(observed.memory_limit_bytes)}` : "Collector pending"} percent={observed && observed.memory_limit_bytes > 0 ? (observed.memory_bytes / observed.memory_limit_bytes) * 100 : 0} />
        <TelemetryCard label="Disk" observed={hasDiskSample ? observed : undefined} value={hasDiskSample && observed ? formatBytes(observed.disk_used_bytes) : "No volume sample"} detail={hasDiskSample && observed && observed.disk_limit_bytes > 0 ? `of ${formatBytes(observed.disk_limit_bytes)}` : metrics ? `${formatBytes(metrics.db_allocated_bytes)} reserved` : "Collector pending"} percent={hasDiskSample && observed && observed.disk_limit_bytes > 0 ? (observed.disk_used_bytes / observed.disk_limit_bytes) * 100 : 0} />
      </div>
      {!observed ? (
        <p className="mt-3 text-xs text-faint">
          Showing reserved capacity until a Compose or Kubernetes telemetry collector records live samples.
        </p>
      ) : null}
    </section>
  );
}

function ConnectionBasicsPanel({ loading, onOpenConnect, payload }: { payload?: ConnectPayload; loading: boolean; onOpenConnect: () => void }) {
  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Connection basics</p>
          <h2>Connect</h2>
        </div>
        <button className="button secondary h-8 min-h-8" onClick={onOpenConnect} type="button">
          <KeyRound size={14} />
          Full connect
        </button>
      </div>
      {loading ? <p className="mt-4 text-sm text-muted">Loading connection basics...</p> : null}
      {payload ? (
        <div className="mt-4 grid gap-2">
          {payload.local_api_url ? <CopyMiniRow label="Local API URL" value={payload.local_api_url} /> : null}
          <CopyMiniRow label="API URL" value={payload.api_url} />
          <CopyMiniRow label="Postgres direct" value={payload.postgres.uri ?? payload.postgres.direct ?? ""} />
          <RuntimeLink className="button secondary mt-1 h-8 min-h-8 justify-center" label={payload.local_studio_url ? "Studio local" : "Studio"} url={payload.local_studio_url || payload.studio_url} />
        </div>
      ) : null}
    </section>
  );
}

function OperationalSurfacePanel({ loading, metrics }: { metrics?: ProjectMetrics; loading: boolean }) {
  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Surface area</p>
          <h2>Enabled project capabilities</h2>
        </div>
        {metrics ? <time className="text-xs text-faint">{formatTime(metrics.sampled_at)}</time> : null}
      </div>
      {loading ? <p className="mt-4 text-sm text-muted">Loading counters...</p> : null}
      {metrics ? (
        <div className="mt-4 grid grid-cols-4 gap-2 max-lg:grid-cols-2 max-sm:grid-cols-1">
          <OverviewMetric icon={Globe2} label="Routes" value={metrics.routes.toString()} />
          <OverviewMetric icon={Database} label="DB objects" value={`${metrics.database_extensions} ext · ${metrics.database_roles} roles`} />
          <OverviewMetric icon={Boxes} label="Storage" value={`${metrics.storage_buckets} buckets`} />
          <OverviewMetric icon={Activity} label="Functions" value={`${metrics.function_deployments} deployed`} />
          <OverviewMetric icon={Network} label="Networks" value={`${metrics.custom_domains} domains · ${metrics.network_connections} private`} />
          <OverviewMetric icon={Shield} label="Secrets" value={metrics.secrets.toString()} />
          <OverviewMetric icon={Activity} label="Logs" value={metrics.project_log_events.toString()} />
          <OverviewMetric icon={Database} label="Retained" value={formatBytes(metrics.storage_bytes)} />
        </div>
      ) : null}
    </section>
  );
}

function OverviewMetric({ icon: Icon, label, value }: { icon: typeof Activity; label: string; value: string }) {
  return (
    <div className="metric-cell bg-bg">
      <div className="mb-1 flex items-center gap-1 text-faint">
        <Icon size={13} />
        <p className="label">{label}</p>
      </div>
      <p className="truncate text-sm font-medium">{value}</p>
    </div>
  );
}

function TelemetryCard({ detail, label, observed, percent, value }: { label: string; value: string; detail: string; percent: number; observed?: TelemetrySample }) {
  const normalized = Math.min(100, Math.max(0, percent || 0));
  return (
    <div className="rounded-md border border-border bg-bg p-3">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="label">{label}</p>
          <p className="mt-1 text-lg font-medium">{value}</p>
        </div>
        <span className={`pill ${observed ? "healthy" : ""}`}>{observed ? "observed" : "reserved"}</span>
      </div>
      <p className="mt-1 truncate text-xs text-muted">{detail}</p>
      <div className="resource-bar mt-3" aria-label={`${label} utilization`}>
        <span style={{ width: `${normalized || 8}%` }} />
      </div>
    </div>
  );
}

function CopyMiniRow({ label, value }: { label: string; value: string }) {
  const addToast = useUIStore((state) => state.addToast);
  const url = httpURL(value);
  async function copy() {
    await navigator.clipboard?.writeText(value);
    addToast({ title: "Copied", detail: label });
  }
  return (
    <div className="copy-row compact text-left">
      <div className="min-w-0">
        <p className="label">{label}</p>
        <p className="truncate font-mono text-xs text-muted">{value || "-"}</p>
      </div>
      <div className="flex justify-end gap-1">
        {url ? (
          <a className="icon-button h-8 min-h-8 min-w-8" href={url} rel="noreferrer" target="_blank" title={`Open ${label}`}>
            <ExternalLink size={14} />
          </a>
        ) : null}
        <button className="icon-button h-8 min-h-8 min-w-8" disabled={!value} onClick={() => void copy()} title={`Copy ${label}`} type="button">
          <Copy size={14} />
        </button>
      </div>
    </div>
  );
}

function httpURL(value: string) {
  const trimmed = value.trim();
  if (!trimmed.startsWith("http://") && !trimmed.startsWith("https://")) {
    return "";
  }
  try {
    return new URL(trimmed).toString();
  } catch {
    return "";
  }
}
