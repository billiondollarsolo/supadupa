import { useNavigate, useRouterState } from "@tanstack/react-router";
import { CreditCard, Gauge, Shield, Users } from "lucide-react";
import { AppPanel } from "../components/app/app-panel";
import { Badge } from "../components/ui/badge";
import { CardButton } from "../components/ui/card-button";
import { useDashboardContext } from "../lib/dashboard-context";
import { formatBytes } from "../lib/format";
import { organizationSections, type OrganizationSection } from "../lib/project-config";
import { BillingPanel, CreateOrgPanel, MembersPanel, OrgFeaturesPanel, OrgSwitcher, QuotaPanel, TeamsPanel, UsagePanel } from "./organizations/panels";

export function OrganizationsPage() {
  const {
    activeOrgId,
    activeTeamSlug,
    billingInvoices,
    members,
    onOrgCreated,
    orgFeatures,
    orgs,
    quota,
    setSelectedOrgId,
    setSelectedTeamSlug,
    teamMembers,
    teams,
    usage,
    usageSnapshots,
    users,
    orgsEnabled,
    quotasEnabled,
  } = useDashboardContext();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = pathname.match(/^\/organizations\/([^/]+)/)?.[1];
  const section: OrganizationSection = organizationSections.some((item) => item.id === selectedSection) ? selectedSection as OrganizationSection : "overview";
  const effectiveFeatures = orgFeatures.data?.effective ?? {};
  const usageMeteringEnabled = Boolean(effectiveFeatures.usage_metering);
  const billingEnabled = Boolean(effectiveFeatures.billing);

  function openSection(target: OrganizationSection) {
    if (target === "overview") {
      void navigate({ to: "/organizations" });
      return;
    }
    void navigate({ to: "/organizations/$section", params: { section: target } });
  }

  if (section === "overview") {
    const quotaData = quota.data;
    const projectsOverQuota = Boolean(quotaData && quotaData.max_projects > 0 && quotaData.used.projects >= quotaData.max_projects);
    const projectsNearQuota = Boolean(quotaData && quotaData.max_projects > 0 && !projectsOverQuota && quotaData.used.projects / quotaData.max_projects >= 0.8);
    return (
      <div className="grid gap-6">
        {orgsEnabled ? <OrgSwitcher orgs={orgs.data ?? []} selectedOrgId={activeOrgId} onSelectOrg={setSelectedOrgId} /> : null}
        {orgsEnabled ? <CreateOrgPanel onCreated={onOrgCreated} /> : null}
        <AppPanel
          eyebrow={orgsEnabled ? "Organizations" : "Access"}
          title={orgsEnabled ? "Admin overview" : "Users, teams & access"}
          description={orgsEnabled ? "Open any area to manage it. Counts reflect the selected org." : "Manage platform users, RBAC teams, and resource quotas. Enable multi-org in Settings → Features for multiple organizations."}
        >
          <div className="mt-4 grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
            <OrgSummaryCard icon={Users} label="Members" value={`${members.data?.length ?? 0} org grants`} detail={`${users.data?.length ?? 0} platform users`} onClick={() => openSection("members")} />
            <OrgSummaryCard icon={Shield} label="Teams" value={`${teams.data?.length ?? 0} teams`} onClick={() => openSection("teams")} />
            <OrgSummaryCard icon={Shield} label="Features" value={`${Object.values(orgFeatures.data?.effective ?? {}).filter(Boolean).length} enabled`} detail={`${Object.keys(orgFeatures.data?.overrides ?? {}).length} org overrides`} onClick={() => openSection("features")} />
            {quotasEnabled ? <OrgSummaryCard icon={Gauge} label="Quotas" value={`${quotaData?.used.projects ?? 0}/${quotaData?.max_projects || "—"} projects`} tone={projectsOverQuota ? "danger" : projectsNearQuota ? "warning" : "neutral"} detail={quotaData ? `${quotaData.used.cpu}/${quotaData.max_cpu || "—"} vCPU · ${formatBytes(quotaData.used.ram_mb * 1024 * 1024)} RAM` : "Quota sample pending"} onClick={() => openSection("quotas")} /> : null}
            {orgsEnabled ? <OrgSummaryCard icon={Gauge} label="Usage" value={`${usage.data?.resources.projects ?? 0} projects`} detail={usage.data ? `${formatBytes(usage.data.db_allocated_bytes)} DB allocation` : "Metering sample pending"} badge={usageMeteringEnabled ? undefined : "metering off"} onClick={() => openSection("usage")} /> : null}
            {orgsEnabled ? <OrgSummaryCard icon={CreditCard} label="Billing" value={billingEnabled ? `${billingInvoices.data?.length ?? 0} invoices` : "—"} detail={billingEnabled ? undefined : "metering snapshots required"} badge={billingEnabled ? undefined : "disabled"} onClick={() => openSection("billing")} /> : null}
          </div>
        </AppPanel>
      </div>
    );
  }

  return (
    <div className="grid gap-6">
      {orgsEnabled ? <OrgSwitcher orgs={orgs.data ?? []} selectedOrgId={activeOrgId} onSelectOrg={setSelectedOrgId} /> : null}
      {section === "members" ? <MembersPanel orgId={activeOrgId} members={members.data ?? []} users={users.data ?? []} loading={members.isLoading || users.isLoading} orgsEnabled={orgsEnabled} /> : null}
      {section === "teams" ? (
        <TeamsPanel
          orgId={activeOrgId}
          teams={teams.data ?? []}
          selectedSlug={activeTeamSlug}
          members={teamMembers.data ?? []}
          onSelect={setSelectedTeamSlug}
          loading={teams.isLoading || teamMembers.isLoading}
        />
      ) : null}
      {section === "features" ? <OrgFeaturesPanel orgId={activeOrgId} features={orgFeatures.data} loading={orgFeatures.isLoading} /> : null}
      {section === "quotas" && quotasEnabled ? <QuotaPanel orgId={activeOrgId} quota={quota.data} loading={quota.isLoading} /> : null}
      {section === "usage" ? <UsagePanel orgId={activeOrgId} usage={usage.data} snapshots={usageSnapshots.data ?? []} loading={usage.isLoading} snapshotsLoading={usageSnapshots.isLoading} snapshotEnabled={usageMeteringEnabled} /> : null}
      {section === "billing" && orgsEnabled ? <BillingPanel orgId={activeOrgId} invoices={billingInvoices.data ?? []} loading={billingInvoices.isLoading} enabled={billingEnabled} /> : null}
    </div>
  );
}

function OrgSummaryCard({ badge, detail, icon: Icon, label, onClick, tone = "neutral", value }: { icon: typeof Users; label: string; value: string; detail?: string; onClick: () => void; tone?: "neutral" | "warning" | "danger"; badge?: string }) {
  const valueToneClass = tone === "danger" ? "text-danger" : tone === "warning" ? "text-warning" : "";
  return (
    <CardButton className="grid min-h-32 content-between" onClick={onClick}>
      <span className="flex min-w-0 items-center justify-between gap-2">
        <span className="flex min-w-0 items-center gap-2">
          <Icon size={14} className="text-faint" />
          <span className="label">{label}</span>
        </span>
        {badge ? <Badge variant="muted">{badge}</Badge> : null}
      </span>
      <span>
        <span className={`block truncate text-sm font-medium ${valueToneClass}`}>{value}</span>
        {detail ? <span className="mt-1 block truncate text-xs text-muted">{detail}</span> : null}
      </span>
    </CardButton>
  );
}
