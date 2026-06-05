import { useNavigate, useRouterState } from "@tanstack/react-router";
import { CreditCard, Gauge, Shield, Users, UserPlus } from "lucide-react";
import { useDashboardContext } from "../lib/dashboard-context";
import { formatBytes } from "../lib/format";
import { organizationSections, type OrganizationSection } from "../lib/project-config";
import { BillingPanel, MembersPanel, OrgFeaturesPanel, OrgPanel, QuotaPanel, TeamsPanel, UsagePanel } from "./organizations/panels";

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
    return (
      <div className="grid gap-6">
        <OrgPanel orgs={orgs.data ?? []} selectedOrgId={activeOrgId} onSelectOrg={setSelectedOrgId} onCreated={onOrgCreated} />
        <section className="panel">
          <div className="section-head">
            <div>
              <p className="label">Organizations</p>
              <h2>Admin overview</h2>
            </div>
          </div>
          <div className="mt-4 grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
            <OrgSummaryCard icon={Users} label="Members" value={`${members.data?.length ?? 0} org grants`} detail={`${users.data?.length ?? 0} platform users · global admin separate from project admin`} onClick={() => openSection("members")} />
            <OrgSummaryCard icon={Shield} label="Teams" value={`${teams.data?.length ?? 0} teams`} detail="Project-scoped RBAC grants flow through teams and users." onClick={() => openSection("teams")} />
            <OrgSummaryCard icon={Shield} label="Features" value={`${Object.values(orgFeatures.data?.effective ?? {}).filter(Boolean).length} enabled`} detail={`${Object.keys(orgFeatures.data?.overrides ?? {}).length} org overrides · inherited from platform defaults`} onClick={() => openSection("features")} />
            <OrgSummaryCard icon={Gauge} label="Quotas" value={`${quota.data?.used.projects ?? 0}/${quota.data?.max_projects || "-"} projects`} detail={quota.data ? `${quota.data.used.cpu}/${quota.data.max_cpu || "-"} CPU · ${formatBytes(quota.data.used.ram_mb * 1024 * 1024)} RAM` : "Quota sample pending"} onClick={() => openSection("quotas")} />
            <OrgSummaryCard icon={Gauge} label="Usage" value={`${usage.data?.resources.projects ?? 0} projects`} detail={usage.data ? `${formatBytes(usage.data.db_allocated_bytes)} DB allocation · ${usageMeteringEnabled ? `${usageSnapshots.data?.length ?? 0} snapshots` : "snapshots disabled"}` : "Metering sample pending"} onClick={() => openSection("usage")} />
            <OrgSummaryCard icon={CreditCard} label="Billing" value={billingEnabled ? `${billingInvoices.data?.length ?? 0} invoices` : "disabled"} detail={billingEnabled ? "Draft invoices are generated from durable metering snapshots." : "Enable billing in org feature flags to generate invoices."} onClick={() => openSection("billing")} />
          </div>
        </section>
      </div>
    );
  }

  return (
    <div className="grid gap-6">
      <OrgSwitcher orgs={orgs.data ?? []} selectedOrgId={activeOrgId} onSelectOrg={setSelectedOrgId} />
      {section === "members" ? <MembersPanel orgId={activeOrgId} members={members.data ?? []} users={users.data ?? []} loading={members.isLoading || users.isLoading} /> : null}
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
      {section === "quotas" ? <QuotaPanel orgId={activeOrgId} quota={quota.data} loading={quota.isLoading} /> : null}
      {section === "usage" ? <UsagePanel orgId={activeOrgId} usage={usage.data} snapshots={usageSnapshots.data ?? []} loading={usage.isLoading} snapshotsLoading={usageSnapshots.isLoading} snapshotEnabled={usageMeteringEnabled} /> : null}
      {section === "billing" ? <BillingPanel orgId={activeOrgId} invoices={billingInvoices.data ?? []} loading={billingInvoices.isLoading} enabled={billingEnabled} /> : null}
    </div>
  );
}

function OrgSwitcher({ orgs, selectedOrgId, onSelectOrg }: { orgs: { id: string; name: string }[]; selectedOrgId: string; onSelectOrg: (id: string) => void }) {
  return (
    <section className="rounded-md border border-border bg-surface px-3 py-2">
      <div className="flex min-h-9 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <UserPlus size={14} className="text-faint" />
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Org scope</p>
            <p className="truncate text-xs text-faint">Global org access is separate from project-level administration.</p>
          </div>
        </div>
        <select className="input max-w-[260px]" value={selectedOrgId} onChange={(event) => onSelectOrg(event.target.value)}>
          {orgs.length === 0 ? <option value="">No orgs yet</option> : null}
          {orgs.map((org) => (
            <option key={org.id} value={org.id}>
              {org.name}
            </option>
          ))}
        </select>
      </div>
    </section>
  );
}

function OrgSummaryCard({ detail, icon: Icon, label, onClick, value }: { icon: typeof Users; label: string; value: string; detail: string; onClick: () => void }) {
  return (
    <button className="grid min-h-32 content-between rounded-md border border-border bg-bg p-3 text-left transition hover:border-border-strong hover:bg-surface-2" onClick={onClick} type="button">
      <span className="flex min-w-0 items-center gap-2">
        <Icon size={14} className="text-faint" />
        <span className="label">{label}</span>
      </span>
      <span>
        <span className="block truncate text-sm font-medium">{value}</span>
        <span className="mt-1 block text-xs text-muted">{detail}</span>
      </span>
    </button>
  );
}
