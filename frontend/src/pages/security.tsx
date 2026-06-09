import { useNavigate, useRouterState } from "@tanstack/react-router";
import { securitySections, type SecuritySection } from "../lib/project-config";
import { useDashboardContext } from "../lib/dashboard-context";
import { MetricCard } from "../components/app/metric-card";
import { AppPanel } from "../components/app/app-panel";
import { CardButton } from "../components/ui/card-button";
import { StatusPill } from "../components/ui/status-pill";
import { AdvisorPanel, CompliancePanel } from "./fleet/panels";
import { AccessReviewPanel, SecurityPanel, projectNeedsReview } from "./security/security-panel";

export function SecurityRoutePage() {
  const { accessReview, advisorFindings, complianceReport, mfaStatus } = useDashboardContext();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const openSection = (id: SecuritySection) => void navigate({ to: "/security/$section", params: { section: id } });
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

  const projects = review?.projects ?? [];
  const reviewCount = projects.filter(projectNeedsReview).length;
  const mfaEnabled = Boolean(status?.enabled);
  const mfaPending = Boolean(status?.pending);

  // Roll up the worst signal across all cards so the header pill never reads
  // "clean" while MFA is off or projects need review.
  const hasDanger = highFindings > 0 || complianceSummary.action_needed > 0 || !mfaEnabled;
  const hasWarning = complianceSummary.manual_review > 0 || reviewCount > 0 || findings.length > 0 || mfaPending;
  const rollupTone = hasDanger ? "danger" : hasWarning ? "warning" : "success";
  const rollupLabel = hasDanger ? "action needed" : hasWarning ? "review" : "clean";

  return (
    <div className="grid gap-4">
      <AppPanel
        eyebrow="Security"
        title="Posture overview"
        actions={<StatusPill tone={rollupTone} label={rollupLabel} />}
      >
        <div className="mt-4 grid grid-cols-4 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
          <CardButton className="h-full p-0" onClick={() => openSection("mfa")} title="Open account MFA">
            <MetricCard
              className="h-full border-0 bg-transparent"
              label="Account MFA →"
              value={mfaStatus.isLoading ? "Loading" : mfaEnabled ? "Enabled" : mfaPending ? "Pending" : "Disabled"}
              detail={mfaEnabled ? "Authenticator enforced for your account" : mfaPending ? "Enrollment not yet verified" : "No verified authenticator — at risk"}
              tone={mfaEnabled ? "success" : mfaPending ? "warning" : "danger"}
            />
          </CardButton>
          <CardButton className="h-full p-0" onClick={() => openSection("access")} title="Open access review">
            <MetricCard
              className="h-full border-0 bg-transparent"
              label="Access review →"
              value={accessReview.isLoading ? "Loading" : reviewCount > 0 ? `${reviewCount} need review` : `${projects.length} projects`}
              detail={reviewCount > 0 ? "Access inherited broadly without explicit grants" : "No over-privileged projects detected"}
              tone={reviewCount > 0 ? "warning" : "success"}
            />
          </CardButton>
          <CardButton className="h-full p-0" onClick={() => openSection("advisor")} title="Open advisor findings">
            <MetricCard
              className="h-full border-0 bg-transparent"
              label="Advisor →"
              value={advisorFindings.isLoading ? "Loading" : findings.length === 0 ? "Clean" : `${findings.length} open`}
              detail={highFindings > 0 ? `${highFindings} high/critical` : findings.length > 0 ? "All low severity" : "No open findings"}
              tone={highFindings > 0 ? "danger" : findings.length > 0 ? "warning" : "success"}
            />
          </CardButton>
          <CardButton className="h-full p-0" onClick={() => openSection("compliance")} title="Open compliance report">
            <MetricCard
              className="h-full border-0 bg-transparent"
              label="Compliance →"
              value={complianceReport.isLoading ? "Loading" : complianceSummary.total > 0 ? `${complianceSummary.passed}/${complianceSummary.total} pass` : "Not sampled"}
              detail={complianceSummary.action_needed > 0 ? `${complianceSummary.action_needed} need action` : complianceSummary.manual_review > 0 ? `${complianceSummary.manual_review} manual review` : "Technical control sampling"}
              tone={complianceSummary.action_needed > 0 ? "danger" : complianceSummary.manual_review > 0 ? "warning" : complianceSummary.total > 0 ? "success" : "default"}
            />
          </CardButton>
        </div>
      </AppPanel>
    </div>
  );
}
