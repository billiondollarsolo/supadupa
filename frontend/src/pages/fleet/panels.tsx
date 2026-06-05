import type { AdvisorFinding, ComplianceReport, FleetMetrics, Host, Project } from "../../types";
import { formatBytes, formatTime } from "../../lib/format";

export function KpiRow({ metrics, projects, hosts }: { metrics?: FleetMetrics; projects: Project[]; hosts: Host[] }) {
  const healthy = projects.filter((project) => project.status === "healthy").length;
  const errored = projects.filter((project) => project.status === "error").length;
  return (
    <div className="grid grid-cols-4 gap-3 max-xl:grid-cols-3 max-lg:grid-cols-2 max-sm:grid-cols-1">
      <Kpi label="Projects" value={projects.length.toString()} />
      <Kpi label="Healthy" value={healthy.toString()} tone="success" />
      <Kpi label="Errors" value={errored.toString()} tone={errored > 0 ? "danger" : "default"} />
      <Kpi label="Hosts" value={hosts.length.toString()} />
      <Kpi label="Reserved CPU" value={metrics ? `${metrics.host_used.cpu}/${metrics.host_capacity.cpu || "-"}` : "-"} />
      <Kpi label="Reserved RAM" value={metrics ? `${formatBytes(metrics.host_used.ram_mb * 1024 * 1024)}/${metrics.host_capacity.ram_mb ? formatBytes(metrics.host_capacity.ram_mb * 1024 * 1024) : "-"}` : "-"} />
      <Kpi label="Reserved disk" value={metrics ? `${metrics.host_used.disk_gb}/${metrics.host_capacity.disk_gb || "-"} GB` : "-"} />
      <Kpi label="Reserved IOPS" value={metrics ? `${metrics.host_used.disk_iops}/${metrics.host_capacity.disk_iops || "-"}` : "-"} />
    </div>
  );
}

function Kpi({ label, value, tone = "default" }: { label: string; value: string; tone?: "default" | "success" | "danger" }) {
  return (
    <div className="panel">
      <p className="label">{label}</p>
      <p className={tone === "success" ? "kpi text-success" : tone === "danger" ? "kpi text-danger" : "kpi"}>{value}</p>
    </div>
  );
}

export function FleetMetricsPanel({ metrics, loading }: { metrics?: FleetMetrics; loading: boolean }) {
  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Reports</p>
          <h2>Fleet metrics</h2>
        </div>
        {metrics ? <time className="text-xs text-faint">{formatTime(metrics.sampled_at)}</time> : null}
      </div>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading fleet metrics...</p> : null}
        {metrics ? (
          <>
            <div className="metric-grid">
              <Metric label="Orgs" value={metrics.orgs.toString()} />
              <Metric label="Users" value={metrics.users.toString()} />
              <Metric label="Routes" value={metrics.routes.toString()} />
              <Metric label="Audit" value={metrics.audit_verified ? "verified" : "broken"} />
            </div>
            <div className="usage-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Capacity reserved</p>
                <p className="truncate text-xs text-muted">{metrics.host_used.projects}/{metrics.host_capacity.projects || "-"} projects · {metrics.host_used.cpu}/{metrics.host_capacity.cpu || "-"} CPU</p>
              </div>
              <div className="text-right text-xs text-muted">
                <p>{formatBytes(metrics.host_used.ram_mb * 1024 * 1024)} / {metrics.host_capacity.ram_mb ? formatBytes(metrics.host_capacity.ram_mb * 1024 * 1024) : "-"}</p>
                <p>{metrics.host_used.disk_gb}/{metrics.host_capacity.disk_gb || "-"} GB disk</p>
                <p>{metrics.host_used.disk_iops}/{metrics.host_capacity.disk_iops || "-"} IOPS</p>
              </div>
            </div>
            <div className="usage-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Observed telemetry</p>
                <p className="truncate text-xs text-muted">{metrics.observed?.projects_sampled ?? 0} sampled projects · {metrics.observed?.stale_projects ?? 0} stale</p>
              </div>
              <div className="text-right text-xs text-muted">
                <p>{(metrics.observed?.cpu_percent ?? 0).toFixed(1)}% CPU</p>
                <p>{formatBytes(metrics.observed?.memory_bytes ?? 0)} / {metrics.observed?.memory_limit_bytes ? formatBytes(metrics.observed.memory_limit_bytes) : "-"}</p>
                <p>{formatBytes(metrics.observed?.disk_used_bytes ?? 0)} / {metrics.observed?.disk_limit_bytes ? formatBytes(metrics.observed.disk_limit_bytes) : "-"}</p>
              </div>
            </div>
            <div className="usage-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Operational surface</p>
                <p className="truncate text-xs text-muted">{metrics.read_replicas} replicas · {metrics.function_deployments} functions · {metrics.function_regions} regions · {metrics.function_storage_mounts} mounts · {metrics.replication_pipelines} pipelines · {metrics.embedding_jobs} embeddings</p>
              </div>
              <div className="text-right text-xs text-muted">
                <p>{metrics.database_extensions} extensions · {metrics.database_cron_jobs} cron jobs · {metrics.database_queues} queues · {metrics.database_webhooks} webhooks · {metrics.database_schemas} schemas · {metrics.auth_clients} auth clients · {metrics.auth_hooks} auth hooks · {metrics.database_roles} database roles</p>
                <p>{metrics.storage_buckets} storage buckets · {metrics.vector_buckets} vector buckets · {metrics.analytics_buckets} analytics buckets · {metrics.log_drains} drains · {metrics.cdn_enabled_projects} CDN · {metrics.network_connections} networks</p>
                <p>{metrics.backups} backups · {metrics.wal_archives} WAL</p>
                <p>{formatBytes(metrics.backup_storage_bytes + metrics.wal_archive_bytes)} retained</p>
                <p>{metrics.project_log_events} project logs · {metrics.audit_events} audit events</p>
              </div>
            </div>
          </>
        ) : null}
      </div>
    </section>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border bg-surface-2 p-3">
      <p className="label">{label}</p>
      <p className="mt-1 truncate text-sm font-medium">{value}</p>
    </div>
  );
}

export function AdvisorPanel({ findings, loading }: { findings?: AdvisorFinding[] | null; loading: boolean }) {
  const safeFindings = findings ?? [];
  const counts = safeFindings.reduce<Record<string, number>>((acc, finding) => {
    acc[finding.severity] = (acc[finding.severity] ?? 0) + 1;
    return acc;
  }, {});
  const visible = safeFindings.slice(0, 8);

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Security & Performance Advisor</p>
          <h2>Fleet findings</h2>
        </div>
        <span className={`pill ${safeFindings.some((finding) => finding.severity === "critical" || finding.severity === "high") ? "error" : safeFindings.length > 0 ? "paused" : "healthy"}`}>
          {loading ? "loading" : safeFindings.length === 0 ? "clean" : `${safeFindings.length} open`}
        </span>
      </div>
      <div className="mt-4 grid gap-3">
        <div className="flex flex-wrap gap-2">
          {["critical", "high", "medium", "low", "info"].map((severity) => (
            <span className={`pill ${advisorSeverityClass(severity)}`} key={severity}>
              {severity} {counts[severity] ?? 0}
            </span>
          ))}
        </div>
        {loading ? <p className="text-sm text-muted">Loading advisor findings...</p> : null}
        {!loading && safeFindings.length === 0 ? <p className="text-sm text-muted">No open findings.</p> : null}
        {visible.map((finding) => (
          <div className="usage-row" key={finding.id}>
            <div className="min-w-0">
              <div className="mb-1 flex min-w-0 flex-wrap items-center gap-2">
                <span className={`pill ${advisorSeverityClass(finding.severity)}`}>{finding.severity}</span>
                <p className="truncate text-sm font-medium">{finding.title}</p>
              </div>
              <p className="truncate text-xs text-muted">{finding.project_ref} · {finding.category} · {finding.message}</p>
            </div>
            <p className="max-w-[260px] text-right text-xs text-muted max-sm:max-w-none max-sm:text-left">{finding.recommendation}</p>
          </div>
        ))}
        {safeFindings.length > visible.length ? <p className="text-xs text-faint">{safeFindings.length - visible.length} more findings hidden in this view.</p> : null}
      </div>
    </section>
  );
}

function advisorSeverityClass(severity: string) {
  if (severity === "critical" || severity === "high") return "error";
  if (severity === "medium") return "paused";
  if (severity === "info") return "healthy";
  return "";
}

export function CompliancePanel({ report, loading }: { report?: ComplianceReport | null; loading: boolean }) {
  const controls = report?.controls ?? [];
  const frameworks = report?.frameworks ?? [];
  const summary = report?.summary ?? { passed: 0, action_needed: 0, manual_review: 0, total: 0 };
  const visible = controls.slice(0, 9);

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Compliance</p>
          <h2>SOC 2 / HIPAA controls</h2>
        </div>
        {report ? (
          <span className={`pill ${summary.action_needed > 0 ? "error" : summary.manual_review > 0 ? "paused" : "healthy"}`}>
            {summary.passed}/{summary.total} pass
          </span>
        ) : null}
      </div>
      <div className="mt-4 grid gap-3">
        {loading ? <p className="text-sm text-muted">Loading compliance report...</p> : null}
        {report ? (
          <>
            <div className="metric-grid">
              <Metric label="Passed" value={summary.passed.toString()} />
              <Metric label="Action" value={summary.action_needed.toString()} />
              <Metric label="Manual" value={summary.manual_review.toString()} />
              <Metric label="Frameworks" value={frameworks.join(" / ") || "-"} />
            </div>
            <div className="grid gap-2">
              {visible.map((control) => (
                <div className="usage-row" key={control.id}>
                  <div className="min-w-0">
                    <div className="mb-1 flex min-w-0 flex-wrap items-center gap-2">
                      <span className={`pill ${complianceStatusClass(control.status ?? "manual_review")}`}>{(control.status ?? "manual_review").replace("_", " ")}</span>
                      <p className="truncate text-sm font-medium">{control.id} · {control.title}</p>
                    </div>
                    <p className="truncate text-xs text-muted">{control.category} · {(control.frameworks ?? []).join(", ")}</p>
                    <p className="truncate text-xs text-faint">{(control.evidence ?? []).join(" · ")}</p>
                  </div>
                  <p className="max-w-[260px] text-right text-xs text-muted max-sm:max-w-none max-sm:text-left">{control.recommendation}</p>
                </div>
              ))}
              {controls.length > visible.length ? <p className="text-xs text-faint">{controls.length - visible.length} more controls hidden in this view.</p> : null}
            </div>
            <div className="usage-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Operator posture</p>
                <p className="truncate text-xs text-muted">{report.dpa_posture}</p>
              </div>
              <p className="max-w-[260px] text-right text-xs text-muted max-sm:text-left">{report.certification}</p>
            </div>
          </>
        ) : null}
      </div>
    </section>
  );
}

function complianceStatusClass(status: string) {
  if (status === "pass") return "healthy";
  if (status === "manual_review") return "paused";
  return "error";
}
