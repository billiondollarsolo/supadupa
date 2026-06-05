import { useMemo } from "react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { useDashboardContext } from "../../lib/dashboard-context";
import { authSections, type AuthSection } from "../../lib/project-config";
import { AccessScopePanel, AuthClientsPanel, AuthEmailPanel, AuthHooksPanel, AuthConfigPanel, AuthProvidersPanel, ProjectAccessPanel } from "./auth-panels";
import { ProjectPage } from "./layout";

export function ProjectAuthPage() {
  const { activeProject, authClients, authEmailTemplatesConfig, authHooks, authProviderConfig, authSMTPConfig, projectAccess, projectConfig, teams } = useDashboardContext();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = pathname.match(/^\/projects\/[^/]+\/auth\/([^/]+)/)?.[1];
  const activeSection: AuthSection = authSections.some((section) => section.id === selectedSection) ? selectedSection as AuthSection : "overview";
  const stats = useMemo(() => {
    const auth = projectConfig.data?.config ?? {};
    const providers = authProviderConfig.data?.config ?? {};
    const enabledProviders = [
      providers.oauth_google_enabled,
      providers.oauth_github_enabled,
      providers.oauth_azure_enabled,
      providers.oauth_oidc_enabled,
      providers.phone_enabled,
      providers.saml_enabled,
      providers.web3_ethereum_enabled,
      providers.web3_solana_enabled,
    ].filter((value) => value === "true").length;
    const mfaEnabled = (auth.mfa_totp_enabled ?? "true") === "true" || (auth.mfa_phone_enabled ?? "false") === "true";
    return {
      login: (auth.email_enabled ?? "true") === "true" ? "Email enabled" : "Email disabled",
      mfa: mfaEnabled ? "MFA available" : "MFA off",
      providers: enabledProviders,
      smtp: (authSMTPConfig.data?.config.enabled ?? "false") === "true" ? "Custom SMTP" : "Default mailer",
      clients: authClients.data?.length ?? 0,
      hooks: authHooks.data?.length ?? 0,
      grants: projectAccess.data?.length ?? 0,
      teams: teams.data?.length ?? 0,
    };
  }, [authClients.data, authHooks.data, authProviderConfig.data, authSMTPConfig.data, projectAccess.data, projectConfig.data, teams.data]);

  return (
    <ProjectPage>
      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Auth workspace</p>
            <h2>{activeProject?.ref ?? "Project auth"}</h2>
            <p className="mt-1 text-xs text-faint">Use the project sidebar to drill into runtime, providers, email, clients, hooks, and access.</p>
          </div>
          <span className="pill healthy">{stats.login}</span>
        </div>
        <div className="mt-4">
          <AuthSectionSummary
            activeSection={activeSection}
            stats={stats}
            onSelect={(section) => {
              if (!activeProject) return;
              void navigate({ to: section === "overview" ? `/projects/${activeProject.ref}/auth` : `/projects/${activeProject.ref}/auth/${section}` });
            }}
          />
        </div>
      </section>

      {activeSection === "runtime" ? <AuthConfigPanel project={activeProject} config={projectConfig.data} loading={projectConfig.isLoading} /> : null}
      {activeSection === "providers" ? <AuthProvidersPanel project={activeProject} config={authProviderConfig.data} loading={authProviderConfig.isLoading} /> : null}
      {activeSection === "email" ? <AuthEmailPanel project={activeProject} templatesConfig={authEmailTemplatesConfig.data} smtpConfig={authSMTPConfig.data} loading={authEmailTemplatesConfig.isLoading || authSMTPConfig.isLoading} /> : null}
      {activeSection === "clients" ? <AuthClientsPanel project={activeProject} clients={authClients.data ?? []} loading={authClients.isLoading} /> : null}
      {activeSection === "hooks" ? <AuthHooksPanel project={activeProject} hooks={authHooks.data ?? []} loading={authHooks.isLoading} /> : null}
      {activeSection === "access" ? (
        <>
          <AccessScopePanel project={activeProject} teams={teams.data ?? []} grants={projectAccess.data ?? []} />
          <ProjectAccessPanel project={activeProject} teams={teams.data ?? []} grants={projectAccess.data ?? []} loading={projectAccess.isLoading} />
        </>
      ) : null}
    </ProjectPage>
  );
}

function AuthSectionSummary({
  activeSection,
  onSelect,
  stats,
}: {
  activeSection: AuthSection;
  stats: {
    login: string;
    mfa: string;
    providers: number;
    smtp: string;
    clients: number;
    hooks: number;
    grants: number;
    teams: number;
  };
  onSelect: (section: AuthSection) => void;
}) {
  if (activeSection !== "overview") {
    const current = authSections.find((section) => section.id === activeSection);
    return (
      <div className="rounded-md border border-border bg-bg p-3">
        <p className="label">{current?.label ?? "Section"}</p>
        <p className="mt-1 text-sm text-muted">{current?.description}</p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
      <AuthSummaryCard label="Runtime" value={stats.login} detail={stats.mfa} onClick={() => onSelect("runtime")} />
      <AuthSummaryCard label="Providers" value={`${stats.providers} enabled`} detail="OAuth, OIDC, SAML, SMS, Web3" onClick={() => onSelect("providers")} />
      <AuthSummaryCard label="Email" value={stats.smtp} detail="SMTP and templates" onClick={() => onSelect("email")} />
      <AuthSummaryCard label="OAuth clients" value={`${stats.clients} registered`} detail="Project as identity provider" onClick={() => onSelect("clients")} />
      <AuthSummaryCard label="Hooks" value={`${stats.hooks} configured`} detail="Auth customization" onClick={() => onSelect("hooks")} />
      <AuthSummaryCard label="Access" value={`${stats.grants} grants`} detail={`${stats.teams} teams available`} onClick={() => onSelect("access")} />
    </div>
  );
}

function AuthSummaryCard({ detail, label, onClick, value }: { label: string; value: string; detail: string; onClick: () => void }) {
  return (
    <button className="rounded-md border border-border bg-bg p-3 text-left transition hover:border-border-strong hover:bg-surface-2" onClick={onClick} type="button">
      <p className="label">{label}</p>
      <p className="mt-2 truncate text-sm font-medium">{value}</p>
      <p className="mt-1 truncate text-xs text-muted">{detail}</p>
    </button>
  );
}
