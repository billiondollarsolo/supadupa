import { useMemo } from "react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { Segmented } from "../../components/ui/segmented";
import { StatusPill } from "../../components/ui/status-pill";
import { useDashboardContext } from "../../lib/dashboard-context";
import { authSections, type AuthSection } from "../../lib/project-config";
import { projectPath, projectSectionFromPathname } from "../../lib/routes";
import { AccessScopePanel, AuthClientsPanel, AuthEmailPanel, AuthHooksPanel, AuthConfigPanel, AuthProvidersPanel, ProjectAccessPanel } from "./auth-panels";
import { ProjectPage } from "./layout";

export function ProjectAuthPage() {
  const { activeProject, authClients, authEmailTemplatesConfig, authHooks, authProviderConfig, authSMTPConfig, projectAccess, projectConfig, teams } = useDashboardContext();
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedSection = projectSectionFromPathname(pathname, "auth");
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
    const loginEnabled = (auth.email_enabled ?? "true") === "true";
    return {
      loginEnabled,
      login: loginEnabled ? "Email enabled" : "Email disabled",
      providers: enabledProviders,
      clients: authClients.data?.length ?? 0,
      hooks: authHooks.data?.length ?? 0,
      grants: projectAccess.data?.length ?? 0,
      teams: teams.data?.length ?? 0,
    };
  }, [authClients.data, authHooks.data, authProviderConfig.data, projectAccess.data, projectConfig.data, teams.data]);

  return (
    <ProjectPage>
      <AppPanel
        eyebrow="Auth workspace"
        title={activeProject?.name ?? "Project auth"}
        actions={<StatusPill label={stats.login} tone={stats.loginEnabled ? "success" : "neutral"} />}
      >
        {activeProject ? <p className="-mt-2 truncate font-mono text-xs text-faint">{activeProject.ref}</p> : null}
        <SectionNav
          activeSection={activeSection}
          onSelect={(section) => {
            if (!activeProject) return;
            void navigate({ to: section === "overview" ? projectPath(activeProject.ref, "auth") : projectPath(activeProject.ref, "auth", section) });
          }}
        />
        {activeSection === "overview" ? (
          <div className="mt-4 grid grid-cols-4 gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
            <MetricCard label="Providers enabled" value={stats.providers} detail="OAuth, OIDC, SAML, SMS, Web3" tone={stats.providers > 0 ? "success" : "default"} />
            <MetricCard label="OAuth clients" value={stats.clients} detail="project as identity provider" />
            <MetricCard label="Auth hooks" value={stats.hooks} detail="enabled customizations" />
            <MetricCard label="Project grants" value={stats.grants} detail={`${stats.teams} teams available`} tone={stats.grants > 0 ? "success" : "default"} />
          </div>
        ) : null}
      </AppPanel>

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

// In-page section navigation so users switch Auth subsections from the content
// area instead of being told to "use the project sidebar".
function SectionNav({ activeSection, onSelect }: { activeSection: AuthSection; onSelect: (section: AuthSection) => void }) {
  return (
    <Segmented
      className="mt-3"
      options={authSections.map((section) => ({ value: section.id, label: section.label }))}
      value={activeSection}
      onChange={onSelect}
    />
  );
}

