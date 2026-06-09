import { useMemo } from "react";
import type { ColumnDef } from "@tanstack/react-table";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { DataTable } from "../../components/data-table";
import { StatusPill } from "../../components/ui/status-pill";
import type { AdvisorFinding, ComplianceReport } from "../../types";
import { formatTime } from "../../lib/format";
import { type Tone } from "../../lib/status";

export function AdvisorPanel({ findings, loading }: { findings?: AdvisorFinding[] | null; loading: boolean }) {
  const safeFindings = findings ?? [];
  const counts = safeFindings.reduce<Record<string, number>>((acc, finding) => {
    acc[finding.severity] = (acc[finding.severity] ?? 0) + 1;
    return acc;
  }, {});
  const visible = safeFindings.slice(0, 8);
  const columns = useMemo<ColumnDef<AdvisorFinding>[]>(
    () => [
      {
        header: "Finding",
        accessorKey: "title",
        size: 330,
        cell: ({ row }) => (
          <>
            <div className="mb-1 flex min-w-0 flex-wrap items-center gap-2">
              <StatusPill tone={advisorSeverityTone(row.original.severity)} label={row.original.severity} />
              <p className="cell-main truncate">{row.original.title}</p>
            </div>
            <p className="cell-sub truncate">{row.original.message}</p>
          </>
        ),
      },
      {
        header: "Scope",
        accessorKey: "project_ref",
        size: 170,
        cell: ({ row }) => (
          <>
            <p className="font-mono text-xs text-muted">{row.original.project_ref}</p>
            <p className="cell-sub">{row.original.category}</p>
          </>
        ),
      },
      {
        header: "Recommendation",
        accessorKey: "recommendation",
        size: 360,
        cell: ({ row }) => <p className="text-sm text-muted">{row.original.recommendation}</p>,
      },
      {
        header: "Opened",
        accessorKey: "created_at",
        size: 120,
        cell: ({ row }) => <time className="text-xs text-faint">{formatTime(row.original.created_at)}</time>,
      },
    ],
    [],
  );

  return (
    <AppPanel
      actions={
        <StatusPill
          tone={safeFindings.some((finding) => finding.severity === "critical" || finding.severity === "high") ? "danger" : safeFindings.length > 0 ? "warning" : "success"}
          label={loading ? "loading" : safeFindings.length === 0 ? "clean" : `${safeFindings.length} open`}
        />
      }
      eyebrow="Security & Performance Advisor"
      title="Fleet findings"
    >
      <div className="mt-4 grid gap-3">
        <div className="flex flex-wrap gap-2">
          {["critical", "high", "medium", "low", "info"].map((severity) => (
            <StatusPill key={severity} tone={advisorSeverityTone(severity)} label={`${severity} ${counts[severity] ?? 0}`} />
          ))}
        </div>
        {loading ? <p className="text-sm text-muted">Loading advisor findings...</p> : null}
        <DataTable columns={columns} data={visible} emptyText={loading ? "Loading advisor findings..." : "No open findings."} minWidth={980} rowClassName={(finding) => finding.severity === "critical" || finding.severity === "high" ? "table-row-error" : finding.severity === "medium" ? "table-row-warning" : ""} />
        {safeFindings.length > visible.length ? <p className="text-xs text-faint">{safeFindings.length - visible.length} more findings hidden in this view.</p> : null}
      </div>
    </AppPanel>
  );
}

function advisorSeverityTone(severity: string): Tone {
  if (severity === "critical" || severity === "high") return "danger";
  if (severity === "medium") return "warning";
  if (severity === "low") return "info";
  return "neutral";
}

export function CompliancePanel({ report, loading }: { report?: ComplianceReport | null; loading: boolean }) {
  const controls = report?.controls ?? [];
  const frameworks = report?.frameworks ?? [];
  const summary = report?.summary ?? { passed: 0, action_needed: 0, manual_review: 0, total: 0 };
  const visible = controls.slice(0, 9);
  const columns = useMemo<ColumnDef<ComplianceReport["controls"][number]>[]>(
    () => [
      {
        header: "Control",
        accessorKey: "title",
        size: 320,
        cell: ({ row }) => (
          <>
            <div className="mb-1 flex min-w-0 flex-wrap items-center gap-2">
              <StatusPill tone={complianceStatusTone(row.original.status ?? "manual_review")} label={(row.original.status ?? "manual_review").replace(/_/g, " ")} />
              <p className="cell-main truncate">{row.original.id} · {row.original.title}</p>
            </div>
            <p className="cell-sub truncate">{row.original.category} · {(row.original.frameworks ?? []).join(", ")}</p>
          </>
        ),
      },
      {
        header: "Evidence",
        accessorKey: "evidence",
        size: 320,
        cell: ({ row }) => <p className="truncate text-sm text-muted">{(row.original.evidence ?? []).join(" · ") || "No evidence attached"}</p>,
      },
      {
        header: "Recommendation",
        accessorKey: "recommendation",
        size: 320,
        cell: ({ row }) => <p className="text-sm text-muted">{row.original.recommendation}</p>,
      },
    ],
    [],
  );

  return (
    <AppPanel
      actions={
        report ? (
          <StatusPill
            tone={summary.action_needed > 0 ? "danger" : summary.manual_review > 0 ? "neutral" : "success"}
            label={`${summary.passed}/${summary.total} pass`}
          />
        ) : null
      }
      eyebrow="Compliance"
      title="SOC 2 / HIPAA controls"
    >
      <div className="mt-4 grid gap-3">
        {loading ? <p className="text-sm text-muted">Loading compliance report...</p> : null}
        {report ? (
          <>
            <div className="metric-grid">
              <MetricCard label="Passed" value={summary.passed.toString()} detail={`of ${summary.total} controls`} tone={summary.passed > 0 ? "success" : "default"} />
              <MetricCard label="Action needed" value={summary.action_needed.toString()} tone={summary.action_needed > 0 ? "danger" : "default"} />
              <MetricCard label="Manual review" value={summary.manual_review.toString()} tone={summary.manual_review > 0 ? "warning" : "default"} />
              <MetricCard label="Frameworks" value={frameworks.join(" / ") || "-"} />
            </div>
            <DataTable columns={columns} data={visible} emptyText="No compliance controls returned." minWidth={980} rowClassName={(control) => control.status === "action_needed" ? "table-row-error" : control.status === "manual_review" ? "table-row-warning" : ""} />
            {controls.length > visible.length ? <p className="text-xs text-faint">{controls.length - visible.length} more controls hidden in this view.</p> : null}
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
    </AppPanel>
  );
}

function complianceStatusTone(status: string): Tone {
  if (status === "pass") return "success";
  if (status === "manual_review") return "neutral";
  return "danger";
}
