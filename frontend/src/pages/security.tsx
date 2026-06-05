import { Link, useRouterState } from "@tanstack/react-router";
import { Activity, FileCheck2, KeyRound, ShieldCheck } from "lucide-react";
import { securitySections, type SecuritySection } from "../lib/project-config";
import { useDashboardContext } from "../lib/dashboard-context";
import { AdvisorPanel, CompliancePanel } from "./fleet/panels";
import { AccessReviewPanel, SecurityPanel } from "./security/security-panel";

export function SecurityRoutePage() {
  const { accessReview, advisorFindings, complianceReport, mfaStatus } = useDashboardContext();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selected = pathname.match(/^\/security\/([^/]+)/)?.[1] ?? "overview";
  const section: SecuritySection = securitySections.some((item) => item.id === selected) ? selected as SecuritySection : "overview";

  if (section === "mfa") {
    return <SecurityPanel status={mfaStatus.data ?? null} loading={mfaStatus.isLoading} />;
  }

  if (section === "access") {
    return <AccessReviewPanel review={accessReview.data ?? null} loading={accessReview.isLoading} />;
  }

  if (section === "advisor") {
    return <AdvisorPanel findings={advisorFindings.data ?? []} loading={advisorFindings.isLoading} />;
  }

  if (section === "compliance") {
    return <CompliancePanel report={complianceReport.data ?? null} loading={complianceReport.isLoading} />;
  }

  const findings = advisorFindings.data ?? [];
  const report = complianceReport.data;
  const review = accessReview.data;
  const status = mfaStatus.data;
  const highFindings = findings.filter((finding) => finding.severity === "critical" || finding.severity === "high").length;
  const complianceSummary = report?.summary ?? { passed: 0, action_needed: 0, manual_review: 0, total: 0 };

  return (
    <div className="grid gap-4">
      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Security</p>
            <h2>Posture overview</h2>
          </div>
          <span className={`pill ${highFindings > 0 || complianceSummary.action_needed > 0 ? "error" : complianceSummary.manual_review > 0 ? "paused" : "healthy"}`}>
            {highFindings > 0 || complianceSummary.action_needed > 0 ? "action needed" : complianceSummary.manual_review > 0 ? "review" : "clean"}
          </span>
        </div>
        <div className="mt-4 grid grid-cols-4 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
          <SecuritySummaryCard
            description={status?.enabled ? "Authenticator enforced for this platform account." : status?.pending ? "Authenticator enrollment has not been verified." : "No verified authenticator is enrolled."}
            href="/security/mfa"
            icon={KeyRound}
            label="MFA"
            meta={mfaStatus.isLoading ? "loading" : status?.enabled ? "enabled" : status?.pending ? "pending" : "disabled"}
            tone={status?.enabled ? "healthy" : status?.pending ? "paused" : "error"}
          />
          <SecuritySummaryCard
            description="Org memberships, teams, explicit grants, and effective project access."
            href="/security/access"
            icon={ShieldCheck}
            label="Access Review"
            meta={accessReview.isLoading ? "loading" : `${review?.projects?.length ?? 0} projects`}
            tone="default"
          />
          <SecuritySummaryCard
            description="Security and performance findings collected across the fleet."
            href="/security/advisor"
            icon={Activity}
            label="Advisor"
            meta={advisorFindings.isLoading ? "loading" : findings.length === 0 ? "clean" : `${findings.length} open`}
            tone={highFindings > 0 ? "error" : findings.length > 0 ? "paused" : "healthy"}
          />
          <SecuritySummaryCard
            description="SOC 2 and HIPAA-oriented technical controls for operator review."
            href="/security/compliance"
            icon={FileCheck2}
            label="Compliance"
            meta={complianceReport.isLoading ? "loading" : complianceSummary.total > 0 ? `${complianceSummary.passed}/${complianceSummary.total} pass` : "not sampled"}
            tone={complianceSummary.action_needed > 0 ? "error" : complianceSummary.manual_review > 0 ? "paused" : "healthy"}
          />
        </div>
      </section>
    </div>
  );
}

function SecuritySummaryCard({
  description,
  href,
  icon: Icon,
  label,
  meta,
  tone,
}: {
  description: string;
  href: string;
  icon: typeof KeyRound;
  label: string;
  meta: string;
  tone: "default" | "healthy" | "paused" | "error";
}) {
  return (
    <Link className="project-card min-h-[172px] content-start" to={href}>
      <div className="flex items-start justify-between gap-3">
        <div className="grid h-9 w-9 place-items-center rounded-md border border-border bg-surface-2 text-muted">
          <Icon size={16} />
        </div>
        <span className={`pill ${tone === "default" ? "" : tone}`}>{meta}</span>
      </div>
      <div className="mt-4">
        <p className="text-sm font-medium">{label}</p>
        <p className="mt-2 text-xs leading-5 text-muted">{description}</p>
      </div>
    </Link>
  );
}
