import { FormEvent, useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { ArrowLeft, KeyRound, Mail, Plus, Save, Shield, SlidersHorizontal, X } from "lucide-react";
import {
  createProjectAuthClient,
  deleteProjectAccess,
  deleteProjectAuthClient,
  deleteProjectAuthHook,
  setProjectAuthHook,
  updateProjectConfig,
  upsertProjectAccess,
} from "../../api";
import { formatDateTime } from "../../lib/format";
import { parseKeyValueLines, parseLines } from "../../lib/parse";
import type { Project, ProjectAccessGrant, ProjectAuthClient, ProjectAuthHook, ProjectConfig, Team } from "../../types";

const authFeatureFlags = [
  { key: "email_enabled", label: "Email login", detail: "email" },
  { key: "magic_link_enabled", label: "Magic links", detail: "passwordless" },
  { key: "mfa_totp_enabled", label: "TOTP MFA", detail: "authenticator" },
  { key: "mfa_phone_enabled", label: "Phone MFA", detail: "sms" },
] as const;

const oauthProviderRows = [
  { id: "google", label: "Google" },
  { id: "github", label: "GitHub" },
  { id: "azure", label: "Azure" },
] as const;

type AuthProviderId = "google" | "github" | "azure" | "oidc" | "phone" | "saml" | "third_party_jwt" | "web3";

const authProviderCards: Array<{ id: AuthProviderId; label: string; type: string; detail: string }> = [
  { id: "google", label: "Google", type: "OAuth", detail: "Client ID and secret handle." },
  { id: "github", label: "GitHub", type: "OAuth", detail: "Client ID and secret handle." },
  { id: "azure", label: "Azure", type: "OAuth", detail: "Client ID and secret handle." },
  { id: "oidc", label: "Custom OIDC", type: "OIDC", detail: "Issuer, scopes, client ID, and secret." },
  { id: "phone", label: "Phone login", type: "SMS", detail: "Provider-specific SMS credentials." },
  { id: "saml", label: "SAML SSO", type: "SAML", detail: "Project-user IdP metadata and entity ID." },
  { id: "third_party_jwt", label: "External JWTs", type: "JWT", detail: "Trusted issuer and audience." },
  { id: "web3", label: "Web3 wallets", type: "Wallet", detail: "Ethereum and Solana login toggles." },
];

const emailTemplateRows = [
  { id: "confirmation", label: "Confirmation" },
  { id: "recovery", label: "Recovery" },
  { id: "magic_link", label: "Magic link" },
  { id: "invite", label: "Invite" },
  { id: "email_change", label: "Email change" },
] as const;

type EmailAreaId = "smtp" | "sms_otp" | typeof emailTemplateRows[number]["id"];

const projectAccessRoles = [
  { value: "owner", label: "Project owner" },
  { value: "admin", label: "Project admin" },
  { value: "developer", label: "Project developer" },
  { value: "viewer", label: "Project viewer" },
] as const;

function projectAccessRoleLabel(role: string) {
  return projectAccessRoles.find((item) => item.value === role)?.label ?? role;
}

export function AuthConfigPanel({ project, config, loading }: { project?: Project; config?: ProjectConfig; loading: boolean }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const configKey = `${project?.ref ?? ""}:${config?.updated_at ?? ""}`;

  useEffect(() => {
    setDraft(config?.config ?? {});
  }, [configKey, config]);

  const mutation = useMutation({
    mutationFn: ({ ref, values }: { ref: string; values: Record<string, string> }) => updateProjectConfig(ref, "auth", values),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-config", variables.ref, "auth"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!project) {
      return;
    }
    mutation.mutate({
      ref: project.ref,
      values: {
        email_enabled: draft.email_enabled || "true",
        magic_link_enabled: draft.magic_link_enabled || "true",
        mfa_totp_enabled: draft.mfa_totp_enabled || "true",
        mfa_totp_enroll_enabled: draft.mfa_totp_enroll_enabled || draft.mfa_totp_enabled || "true",
        mfa_totp_verify_enabled: draft.mfa_totp_verify_enabled || draft.mfa_totp_enabled || "true",
        mfa_phone_enabled: draft.mfa_phone_enabled || "false",
        mfa_phone_enroll_enabled: draft.mfa_phone_enroll_enabled || draft.mfa_phone_enabled || "false",
        mfa_phone_verify_enabled: draft.mfa_phone_verify_enabled || draft.mfa_phone_enabled || "false",
        mfa_phone_otp_length: draft.mfa_phone_otp_length || "6",
        mfa_phone_max_frequency: draft.mfa_phone_max_frequency || "10s",
        captcha_provider: draft.captcha_provider || "",
        captcha_site_key: draft.captcha_site_key || "",
        captcha_secret_handle: draft.captcha_secret_handle || "",
        jwt_key_mode: draft.jwt_key_mode || "shared-secret",
        site_url: draft.site_url || "",
        additional_redirects: draft.additional_redirects || "",
      },
    });
  }

  const setValue = (key: string, value: string) => setDraft((current) => ({ ...current, [key]: value }));
  const setFlag = (key: string, enabled: boolean) => {
    setDraft((current) => {
      const next = { ...current, [key]: enabled ? "true" : "false" };
      if (key === "mfa_totp_enabled") {
        next.mfa_totp_enroll_enabled = enabled ? "true" : "false";
        next.mfa_totp_verify_enabled = enabled ? "true" : "false";
      }
      if (key === "mfa_phone_enabled") {
        next.mfa_phone_enroll_enabled = enabled ? "true" : "false";
        next.mfa_phone_verify_enabled = enabled ? "true" : "false";
      }
      return next;
    });
  };

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Auth</p>
          <h2>Runtime settings</h2>
        </div>
        <SlidersHorizontal size={15} className="text-faint" />
      </div>
      <form className="mt-4 grid gap-3" onSubmit={submit}>
        <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
          {authFeatureFlags.map((flag) => {
            const enabled = (draft[flag.key] ?? (flag.key === "mfa_phone_enabled" ? "false" : "true")) === "true";
            return (
              <label className="config-toggle" key={flag.key}>
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{flag.label}</p>
                  <p className="truncate font-mono text-xs text-muted">{flag.detail}</p>
                </div>
                <input checked={enabled} onChange={(event) => setFlag(flag.key, event.target.checked)} type="checkbox" />
              </label>
            );
          })}
        </div>
        <div className="grid grid-cols-[130px_160px_minmax(0,1fr)_minmax(0,1fr)] gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
          <input className="input font-mono" inputMode="numeric" min="4" max="10" value={draft.mfa_phone_otp_length ?? "6"} onChange={(event) => setValue("mfa_phone_otp_length", event.target.value)} type="number" />
          <input className="input font-mono" value={draft.mfa_phone_max_frequency ?? "10s"} onChange={(event) => setValue("mfa_phone_max_frequency", event.target.value)} />
          <select className="input" value={draft.captcha_provider ?? ""} onChange={(event) => setValue("captcha_provider", event.target.value)}>
            <option value="">Captcha off</option>
            <option value="hcaptcha">hCaptcha</option>
            <option value="turnstile">Turnstile</option>
          </select>
          <select className="input" value={draft.jwt_key_mode ?? "shared-secret"} onChange={(event) => setValue("jwt_key_mode", event.target.value)}>
            <option value="shared-secret">Shared secret</option>
            <option value="asymmetric">Asymmetric</option>
          </select>
        </div>
        <div className="grid grid-cols-2 gap-2 max-lg:grid-cols-1">
          <input className="input font-mono" placeholder="https://app.example.com" value={draft.site_url ?? ""} onChange={(event) => setValue("site_url", event.target.value)} />
          <input className="input font-mono" placeholder="secret://projects/ref/captcha-secret" value={draft.captcha_secret_handle ?? ""} onChange={(event) => setValue("captcha_secret_handle", event.target.value)} />
          <input className="input font-mono" placeholder="captcha site key" value={draft.captcha_site_key ?? ""} onChange={(event) => setValue("captcha_site_key", event.target.value)} />
          <textarea className="input min-h-[52px] font-mono" placeholder="https://app.example.com/auth/callback" value={draft.additional_redirects ?? ""} onChange={(event) => setValue("additional_redirects", event.target.value)} />
        </div>
        <div className="usage-row">
          <p className="text-xs text-muted">{loading ? "Loading auth settings..." : config?.updated_at ? `Updated ${formatDateTime(config.updated_at)}` : "Auth settings not saved yet."}</p>
          <button className="button secondary" disabled={!project || mutation.isPending} type="submit">
            <Save size={14} />
            Save auth
          </button>
        </div>
        {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
      </form>
    </section>
  );
}

export function AuthEmailPanel({
  project,
  templatesConfig,
  smtpConfig,
  loading,
}: {
  project?: Project;
  templatesConfig?: ProjectConfig;
  smtpConfig?: ProjectConfig;
  loading: boolean;
}) {
  const queryClient = useQueryClient();
  const [templatesDraft, setTemplatesDraft] = useState<Record<string, string>>({});
  const [smtpDraft, setSMTPDraft] = useState<Record<string, string>>({});
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedItem = pathname.match(/^\/projects\/[^/]+\/auth\/email\/([^/]+)/)?.[1];
  const emailAreaIds: EmailAreaId[] = ["smtp", ...emailTemplateRows.map((template) => template.id), "sms_otp"];
  const activeArea: EmailAreaId | "" = emailAreaIds.includes(selectedItem as EmailAreaId) ? selectedItem as EmailAreaId : "";
  const templatesKey = `${project?.ref ?? ""}:${templatesConfig?.updated_at ?? ""}`;
  const smtpKey = `${project?.ref ?? ""}:${smtpConfig?.updated_at ?? ""}`;

  useEffect(() => {
    setTemplatesDraft(templatesConfig?.config ?? {});
  }, [templatesKey, templatesConfig]);

  useEffect(() => {
    setSMTPDraft(smtpConfig?.config ?? {});
  }, [smtpKey, smtpConfig]);

  const templatesMutation = useMutation({
    mutationFn: ({ ref, values }: { ref: string; values: Record<string, string> }) => updateProjectConfig(ref, "email_templates", values),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-config", variables.ref, "email_templates"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const smtpMutation = useMutation({
    mutationFn: ({ ref, values }: { ref: string; values: Record<string, string> }) => updateProjectConfig(ref, "smtp", values),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-config", variables.ref, "smtp"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  function submitTemplates(event: FormEvent) {
    event.preventDefault();
    if (!project) {
      return;
    }
    templatesMutation.mutate({
      ref: project.ref,
      values: {
        confirmation_subject: templatesDraft.confirmation_subject || "",
        confirmation_body: templatesDraft.confirmation_body || "",
        recovery_subject: templatesDraft.recovery_subject || "",
        recovery_body: templatesDraft.recovery_body || "",
        magic_link_subject: templatesDraft.magic_link_subject || "",
        magic_link_body: templatesDraft.magic_link_body || "",
        invite_subject: templatesDraft.invite_subject || "",
        invite_body: templatesDraft.invite_body || "",
        email_change_subject: templatesDraft.email_change_subject || "",
        email_change_body: templatesDraft.email_change_body || "",
        sms_otp_message: templatesDraft.sms_otp_message || "",
      },
    });
  }

  function submitSMTP(event: FormEvent) {
    event.preventDefault();
    if (!project) {
      return;
    }
    smtpMutation.mutate({
      ref: project.ref,
      values: {
        enabled: smtpDraft.enabled || "false",
        host: smtpDraft.host || "",
        port: smtpDraft.port || "587",
        sender_name: smtpDraft.sender_name || "",
        sender_email: smtpDraft.sender_email || "",
        username: smtpDraft.username || "",
        password_handle: smtpDraft.password_handle || "",
        tls_mode: smtpDraft.tls_mode || "starttls",
      },
    });
  }

  const setTemplateValue = (key: string, value: string) => setTemplatesDraft((current) => ({ ...current, [key]: value }));
  const setSMTPValue = (key: string, value: string) => setSMTPDraft((current) => ({ ...current, [key]: value }));
  const openEmailArea = (area: EmailAreaId) => {
    if (!project) return;
    void navigate({ to: `/projects/${project.ref}/auth/email/${area}` });
  };
  const closeEmailArea = () => {
    if (!project) return;
    void navigate({ to: `/projects/${project.ref}/auth/email` });
  };

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Auth</p>
          <h2>Email delivery</h2>
        </div>
        <Mail size={15} className="text-faint" />
      </div>
      {!activeArea ? (
        <div className="mt-4 grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
          <EmailAreaCard detail={smtpDraft.enabled === "true" ? `${smtpDraft.host || "host pending"}:${smtpDraft.port || "587"}` : "Default GoTrue mailer"} label="SMTP" status={smtpDraft.enabled === "true" ? "custom" : "default"} onClick={() => openEmailArea("smtp")} />
          {emailTemplateRows.map((template) => (
            <EmailAreaCard
              detail={templatesDraft[`${template.id}_subject`] || "Subject not set"}
              key={template.id}
              label={template.label}
              status={templatesDraft[`${template.id}_body`] ? "configured" : "default"}
              onClick={() => openEmailArea(template.id)}
            />
          ))}
          <EmailAreaCard detail={templatesDraft.sms_otp_message || "Message not set"} label="SMS OTP" status={templatesDraft.sms_otp_message ? "configured" : "default"} onClick={() => openEmailArea("sms_otp")} />
        </div>
      ) : null}
      {activeArea === "smtp" ? (
        <form className="mt-4 grid gap-3" onSubmit={submitSMTP}>
          <DetailHeader detail="Custom SMTP is a project-level mailer override for GoTrue." title="SMTP delivery" onBack={closeEmailArea} />
          <div className="grid grid-cols-[160px_minmax(0,1fr)_110px_140px] gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
            <label className="config-toggle">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Custom SMTP</p>
                <p className="truncate font-mono text-xs text-muted">gotrue mailer</p>
              </div>
              <input checked={(smtpDraft.enabled ?? "false") === "true"} onChange={(event) => setSMTPValue("enabled", event.target.checked ? "true" : "false")} type="checkbox" />
            </label>
            <input className="input font-mono" placeholder="smtp.example.com" value={smtpDraft.host ?? ""} onChange={(event) => setSMTPValue("host", event.target.value)} />
            <input className="input font-mono" inputMode="numeric" value={smtpDraft.port ?? "587"} onChange={(event) => setSMTPValue("port", event.target.value)} />
            <select className="input" value={smtpDraft.tls_mode ?? "starttls"} onChange={(event) => setSMTPValue("tls_mode", event.target.value)}>
              <option value="starttls">STARTTLS</option>
              <option value="implicit">Implicit TLS</option>
              <option value="none">No TLS</option>
            </select>
          </div>
          <div className="grid grid-cols-2 gap-2 max-lg:grid-cols-1">
            <input className="input" placeholder="Supadupa Auth" value={smtpDraft.sender_name ?? ""} onChange={(event) => setSMTPValue("sender_name", event.target.value)} />
            <input className="input font-mono" placeholder="auth@example.com" value={smtpDraft.sender_email ?? ""} onChange={(event) => setSMTPValue("sender_email", event.target.value)} />
            <input className="input font-mono" placeholder="smtp username" value={smtpDraft.username ?? ""} onChange={(event) => setSMTPValue("username", event.target.value)} />
            <input className="input font-mono" placeholder="secret://projects/ref/smtp-password" value={smtpDraft.password_handle ?? ""} onChange={(event) => setSMTPValue("password_handle", event.target.value)} />
          </div>
          <div className="usage-row">
            <p className="text-xs text-muted">{loading ? "Loading mail settings..." : smtpConfig?.updated_at ? `SMTP updated ${formatDateTime(smtpConfig.updated_at)}` : "SMTP not saved yet."}</p>
            <button className="button secondary" disabled={!project || smtpMutation.isPending} type="submit">
              <Save size={14} />
              Save SMTP
            </button>
          </div>
          {smtpMutation.error ? <p className="text-sm text-danger">{smtpMutation.error.message}</p> : null}
        </form>
      ) : null}
      {activeArea && activeArea !== "smtp" ? (
        <form className="mt-4 grid gap-3" onSubmit={submitTemplates}>
          <EmailTemplateDetail activeArea={activeArea} templatesDraft={templatesDraft} setTemplateValue={setTemplateValue} onBack={closeEmailArea} />
          <div className="usage-row">
            <p className="text-xs text-muted">{loading ? "Loading templates..." : templatesConfig?.updated_at ? `Templates updated ${formatDateTime(templatesConfig.updated_at)}` : "Templates not saved yet."}</p>
            <button className="button secondary" disabled={!project || templatesMutation.isPending} type="submit">
              <Save size={14} />
              Save template
            </button>
          </div>
          {templatesMutation.error ? <p className="text-sm text-danger">{templatesMutation.error.message}</p> : null}
        </form>
      ) : null}
    </section>
  );
}

function DetailHeader({ detail, title, onBack }: { title: string; detail: string; onBack: () => void }) {
  return (
    <div className="rounded-md border border-border bg-bg p-3">
      <button className="segmented mb-3 h-8" onClick={onBack} type="button">
        <ArrowLeft size={14} />
        Back
      </button>
      <p className="label">{title}</p>
      <p className="mt-1 text-sm text-muted">{detail}</p>
    </div>
  );
}

function EmailAreaCard({ detail, label, status, onClick }: { label: string; detail: string; status: string; onClick: () => void }) {
  return (
    <button className="rounded-md border border-border bg-bg p-3 text-left transition hover:border-border-strong hover:bg-surface-2" onClick={onClick} type="button">
      <div className="mb-3 flex items-center justify-between gap-2">
        <p className="truncate text-sm font-medium">{label}</p>
        <span className={`pill ${status === "configured" || status === "custom" ? "healthy" : ""}`}>{status}</span>
      </div>
      <p className="truncate text-xs text-muted">{detail}</p>
    </button>
  );
}

function EmailTemplateDetail({
  activeArea,
  templatesDraft,
  setTemplateValue,
  onBack,
}: {
  activeArea: Exclude<EmailAreaId, "smtp">;
  templatesDraft: Record<string, string>;
  setTemplateValue: (key: string, value: string) => void;
  onBack: () => void;
}) {
  if (activeArea === "sms_otp") {
    return (
      <>
        <DetailHeader detail="The SMS OTP message is used by phone login and phone MFA flows." title="SMS OTP message" onBack={onBack} />
        <textarea className="input min-h-[120px] font-mono" placeholder="Your code is {{ .Code }}" value={templatesDraft.sms_otp_message ?? ""} onChange={(event) => setTemplateValue("sms_otp_message", event.target.value)} />
      </>
    );
  }

  const template = emailTemplateRows.find((item) => item.id === activeArea);
  return (
    <>
      <DetailHeader detail={`${template?.label ?? activeArea} email subject and body/template path.`} title={`${template?.label ?? activeArea} template`} onBack={onBack} />
      <input className="input" placeholder={`${template?.label ?? activeArea} subject`} value={templatesDraft[`${activeArea}_subject`] ?? ""} onChange={(event) => setTemplateValue(`${activeArea}_subject`, event.target.value)} />
      <textarea className="input min-h-[160px] font-mono" placeholder={`${template?.label ?? activeArea} template path or body`} value={templatesDraft[`${activeArea}_body`] ?? ""} onChange={(event) => setTemplateValue(`${activeArea}_body`, event.target.value)} />
    </>
  );
}

export function AuthProvidersPanel({ project, config, loading }: { project?: Project; config?: ProjectConfig; loading: boolean }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const selectedItem = pathname.match(/^\/projects\/[^/]+\/auth\/providers\/([^/]+)/)?.[1];
  const activeProvider: AuthProviderId | "" = authProviderCards.some((provider) => provider.id === selectedItem) ? selectedItem as AuthProviderId : "";
  const configKey = `${project?.ref ?? ""}:${config?.updated_at ?? ""}`;

  useEffect(() => {
    setDraft(config?.config ?? {});
  }, [configKey, config]);

  const mutation = useMutation({
    mutationFn: ({ ref, values }: { ref: string; values: Record<string, string> }) => updateProjectConfig(ref, "auth_providers", values),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-config", variables.ref, "auth_providers"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!project) {
      return;
    }
    mutation.mutate({
      ref: project.ref,
      values: {
        oauth_google_enabled: draft.oauth_google_enabled || "false",
        oauth_google_client_id: draft.oauth_google_client_id || "",
        oauth_google_client_secret_handle: draft.oauth_google_client_secret_handle || "",
        oauth_github_enabled: draft.oauth_github_enabled || "false",
        oauth_github_client_id: draft.oauth_github_client_id || "",
        oauth_github_client_secret_handle: draft.oauth_github_client_secret_handle || "",
        oauth_azure_enabled: draft.oauth_azure_enabled || "false",
        oauth_azure_client_id: draft.oauth_azure_client_id || "",
        oauth_azure_client_secret_handle: draft.oauth_azure_client_secret_handle || "",
        oauth_oidc_enabled: draft.oauth_oidc_enabled || "false",
        oauth_oidc_issuer_url: draft.oauth_oidc_issuer_url || "",
        oauth_oidc_client_id: draft.oauth_oidc_client_id || "",
        oauth_oidc_client_secret_handle: draft.oauth_oidc_client_secret_handle || "",
        oauth_oidc_scopes: draft.oauth_oidc_scopes || "openid email profile",
        phone_enabled: draft.phone_enabled || "false",
        sms_provider: draft.sms_provider || "",
        sms_twilio_account_sid: draft.sms_twilio_account_sid || "",
        sms_twilio_auth_token_handle: draft.sms_twilio_auth_token_handle || "",
        sms_twilio_message_service_sid: draft.sms_twilio_message_service_sid || "",
        sms_messagebird_originator: draft.sms_messagebird_originator || "",
        sms_messagebird_access_key_handle: draft.sms_messagebird_access_key_handle || "",
        sms_vonage_from: draft.sms_vonage_from || "",
        sms_vonage_api_key: draft.sms_vonage_api_key || "",
        sms_vonage_api_secret_handle: draft.sms_vonage_api_secret_handle || "",
        saml_enabled: draft.saml_enabled || "false",
        saml_metadata_url: draft.saml_metadata_url || "",
        saml_entity_id: draft.saml_entity_id || "",
        third_party_jwt_issuer: draft.third_party_jwt_issuer || "",
        third_party_jwt_audience: draft.third_party_jwt_audience || "",
        web3_ethereum_enabled: draft.web3_ethereum_enabled || "false",
        web3_solana_enabled: draft.web3_solana_enabled || "false",
      },
    });
  }

  const setValue = (key: string, value: string) => setDraft((current) => ({ ...current, [key]: value }));
  const setFlag = (key: string, enabled: boolean) => setValue(key, enabled ? "true" : "false");
  const openProvider = (provider: AuthProviderId) => {
    if (!project) return;
    void navigate({ to: `/projects/${project.ref}/auth/providers/${provider}` });
  };
  const closeProvider = () => {
    if (!project) return;
    void navigate({ to: `/projects/${project.ref}/auth/providers` });
  };

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Auth</p>
          <h2>Providers</h2>
        </div>
        <KeyRound size={15} className="text-faint" />
      </div>
      {!activeProvider ? (
        <div className="mt-4 grid grid-cols-4 gap-3 max-2xl:grid-cols-3 max-xl:grid-cols-2 max-sm:grid-cols-1">
          {authProviderCards.map((provider) => (
            <ProviderCard
              detail={provider.detail}
              key={provider.id}
              label={provider.label}
              status={providerEnabledStatus(provider.id, draft)}
              type={provider.type}
              onClick={() => openProvider(provider.id)}
            />
          ))}
        </div>
      ) : (
        <form className="mt-4 grid gap-3" onSubmit={submit}>
          <ProviderDetailFields activeProvider={activeProvider} draft={draft} loading={loading} mutationPending={mutation.isPending} projectReady={Boolean(project)} setFlag={setFlag} setValue={setValue} onBack={closeProvider} />
          <div className="usage-row">
            <p className="text-xs text-muted">{loading ? "Loading providers..." : config?.updated_at ? `Updated ${formatDateTime(config.updated_at)}` : "Providers not saved yet."}</p>
            <button className="button secondary" disabled={!project || mutation.isPending} type="submit">
              <Save size={14} />
              Save provider
            </button>
          </div>
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </form>
      )}
    </section>
  );
}

function ProviderCard({
  detail,
  label,
  status,
  type,
  onClick,
}: {
  label: string;
  type: string;
  detail: string;
  status: string;
  onClick: () => void;
}) {
  return (
    <button className="rounded-md border border-border bg-bg p-3 text-left transition hover:border-border-strong hover:bg-surface-2" onClick={onClick} type="button">
      <div className="mb-3 flex items-center justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium">{label}</p>
          <p className="truncate font-mono text-xs text-faint">{type}</p>
        </div>
        <span className={`pill ${status === "enabled" || status === "configured" ? "healthy" : ""}`}>{status}</span>
      </div>
      <p className="text-xs text-muted">{detail}</p>
    </button>
  );
}

function providerEnabledStatus(provider: AuthProviderId, draft: Record<string, string>) {
  if (provider === "google" || provider === "github" || provider === "azure") {
    return draft[`oauth_${provider}_enabled`] === "true" ? "enabled" : "off";
  }
  if (provider === "oidc") return draft.oauth_oidc_enabled === "true" ? "enabled" : "off";
  if (provider === "phone") return draft.phone_enabled === "true" ? "enabled" : "off";
  if (provider === "saml") return draft.saml_enabled === "true" ? "enabled" : "off";
  if (provider === "web3") return draft.web3_ethereum_enabled === "true" || draft.web3_solana_enabled === "true" ? "enabled" : "off";
  return draft.third_party_jwt_issuer || draft.third_party_jwt_audience ? "configured" : "off";
}

function ProviderDetailFields({
  activeProvider,
  draft,
  setFlag,
  setValue,
  onBack,
}: {
  activeProvider: AuthProviderId;
  draft: Record<string, string>;
  loading: boolean;
  mutationPending: boolean;
  projectReady: boolean;
  setFlag: (key: string, enabled: boolean) => void;
  setValue: (key: string, value: string) => void;
  onBack: () => void;
}) {
  const provider = authProviderCards.find((item) => item.id === activeProvider);
  if (activeProvider === "google" || activeProvider === "github" || activeProvider === "azure") {
    const prefix = `oauth_${activeProvider}`;
    return (
      <>
        <DetailHeader detail={provider?.detail ?? "OAuth provider credentials."} title={`${provider?.label ?? activeProvider} OAuth`} onBack={onBack} />
        <label className="config-toggle">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Enable provider</p>
            <p className="truncate font-mono text-xs text-muted">{prefix}_enabled</p>
          </div>
          <input checked={(draft[`${prefix}_enabled`] ?? "false") === "true"} onChange={(event) => setFlag(`${prefix}_enabled`, event.target.checked)} type="checkbox" />
        </label>
        <input className="input font-mono" placeholder={`${activeProvider} client id`} value={draft[`${prefix}_client_id`] ?? ""} onChange={(event) => setValue(`${prefix}_client_id`, event.target.value)} />
        <input className="input font-mono" placeholder={`secret://projects/ref/${activeProvider}`} value={draft[`${prefix}_client_secret_handle`] ?? ""} onChange={(event) => setValue(`${prefix}_client_secret_handle`, event.target.value)} />
      </>
    );
  }

  if (activeProvider === "oidc") {
    return (
      <>
        <DetailHeader detail="Custom OIDC trusts a project-specific issuer and client configuration." title="Custom OIDC" onBack={onBack} />
        <label className="config-toggle">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Enable OIDC</p>
            <p className="truncate font-mono text-xs text-muted">oauth_oidc_enabled</p>
          </div>
          <input checked={(draft.oauth_oidc_enabled ?? "false") === "true"} onChange={(event) => setFlag("oauth_oidc_enabled", event.target.checked)} type="checkbox" />
        </label>
        <div className="grid grid-cols-2 gap-2 max-lg:grid-cols-1">
          <input className="input font-mono" placeholder="https://issuer.example.com" value={draft.oauth_oidc_issuer_url ?? ""} onChange={(event) => setValue("oauth_oidc_issuer_url", event.target.value)} />
          <input className="input font-mono" placeholder="openid email profile" value={draft.oauth_oidc_scopes ?? "openid email profile"} onChange={(event) => setValue("oauth_oidc_scopes", event.target.value)} />
          <input className="input font-mono" placeholder="oidc client id" value={draft.oauth_oidc_client_id ?? ""} onChange={(event) => setValue("oauth_oidc_client_id", event.target.value)} />
          <input className="input font-mono" placeholder="secret://projects/ref/oidc" value={draft.oauth_oidc_client_secret_handle ?? ""} onChange={(event) => setValue("oauth_oidc_client_secret_handle", event.target.value)} />
        </div>
      </>
    );
  }

  if (activeProvider === "phone") {
    const sharedSecret = draft.sms_twilio_auth_token_handle || draft.sms_messagebird_access_key_handle || draft.sms_vonage_api_secret_handle || "";
    return (
      <>
        <DetailHeader detail="Phone login and phone MFA use this project-specific SMS provider." title="Phone login" onBack={onBack} />
        <label className="config-toggle">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Enable phone login</p>
            <p className="truncate font-mono text-xs text-muted">phone_enabled</p>
          </div>
          <input checked={(draft.phone_enabled ?? "false") === "true"} onChange={(event) => setFlag("phone_enabled", event.target.checked)} type="checkbox" />
        </label>
        <div className="grid grid-cols-2 gap-2 max-lg:grid-cols-1">
          <select className="input" value={draft.sms_provider ?? ""} onChange={(event) => setValue("sms_provider", event.target.value)}>
            <option value="">SMS off</option>
            <option value="twilio">Twilio</option>
            <option value="messagebird">MessageBird</option>
            <option value="vonage">Vonage</option>
          </select>
          <input className="input font-mono" placeholder="secret://projects/ref/sms" value={sharedSecret} onChange={(event) => {
            setValue("sms_twilio_auth_token_handle", event.target.value);
            setValue("sms_messagebird_access_key_handle", event.target.value);
            setValue("sms_vonage_api_secret_handle", event.target.value);
          }} />
          <input className="input font-mono" placeholder="twilio account sid / messagebird originator" value={draft.sms_twilio_account_sid || draft.sms_messagebird_originator || ""} onChange={(event) => {
            setValue("sms_twilio_account_sid", event.target.value);
            setValue("sms_messagebird_originator", event.target.value);
          }} />
          <input className="input font-mono" placeholder="message service / vonage from" value={draft.sms_twilio_message_service_sid || draft.sms_vonage_from || ""} onChange={(event) => {
            setValue("sms_twilio_message_service_sid", event.target.value);
            setValue("sms_vonage_from", event.target.value);
          }} />
          <input className="input font-mono" placeholder="vonage api key" value={draft.sms_vonage_api_key ?? ""} onChange={(event) => setValue("sms_vonage_api_key", event.target.value)} />
        </div>
      </>
    );
  }

  if (activeProvider === "saml") {
    return (
      <>
        <DetailHeader detail="SAML here is for project end-users. Platform SSO remains a separate global admin setting." title="SAML SSO" onBack={onBack} />
        <label className="config-toggle">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">Enable SAML</p>
            <p className="truncate font-mono text-xs text-muted">saml_enabled</p>
          </div>
          <input checked={(draft.saml_enabled ?? "false") === "true"} onChange={(event) => setFlag("saml_enabled", event.target.checked)} type="checkbox" />
        </label>
        <input className="input font-mono" placeholder="https://idp.example.com/metadata" value={draft.saml_metadata_url ?? ""} onChange={(event) => setValue("saml_metadata_url", event.target.value)} />
        <input className="input font-mono" placeholder="saml entity id" value={draft.saml_entity_id ?? ""} onChange={(event) => setValue("saml_entity_id", event.target.value)} />
      </>
    );
  }

  if (activeProvider === "web3") {
    return (
      <>
        <DetailHeader detail="Wallet providers are toggled independently for this project." title="Web3 wallets" onBack={onBack} />
        <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
          <label className="config-toggle">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Ethereum</p>
              <p className="truncate font-mono text-xs text-muted">web3_ethereum_enabled</p>
            </div>
            <input checked={(draft.web3_ethereum_enabled ?? "false") === "true"} onChange={(event) => setFlag("web3_ethereum_enabled", event.target.checked)} type="checkbox" />
          </label>
          <label className="config-toggle">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Solana</p>
              <p className="truncate font-mono text-xs text-muted">web3_solana_enabled</p>
            </div>
            <input checked={(draft.web3_solana_enabled ?? "false") === "true"} onChange={(event) => setFlag("web3_solana_enabled", event.target.checked)} type="checkbox" />
          </label>
        </div>
      </>
    );
  }

  return (
    <>
      <DetailHeader detail="Trust an external JWT issuer for project end-user authentication." title="External JWTs" onBack={onBack} />
      <input className="input font-mono" placeholder="external jwt issuer" value={draft.third_party_jwt_issuer ?? ""} onChange={(event) => setValue("third_party_jwt_issuer", event.target.value)} />
      <input className="input font-mono" placeholder="external jwt audience" value={draft.third_party_jwt_audience ?? ""} onChange={(event) => setValue("third_party_jwt_audience", event.target.value)} />
    </>
  );
}

export function ProjectAccessPanel({ project, teams, grants, loading }: { project?: Project; teams: Team[]; grants: ProjectAccessGrant[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [subjectType, setSubjectType] = useState("team");
  const [subjectId, setSubjectId] = useState("");
  const [role, setRole] = useState("viewer");
  useEffect(() => {
    if (subjectType === "team" && !subjectId && teams[0]) {
      setSubjectId(teams[0].slug);
    }
  }, [subjectType, subjectId, teams]);
  const invalidate = (ref = project?.ref ?? "") => {
    void queryClient.invalidateQueries({ queryKey: ["project-access", ref] });
    void queryClient.invalidateQueries({ queryKey: ["projects"] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const grantMutation = useMutation({
    mutationFn: () => {
      if (!project) throw new Error("select a project first");
      return upsertProjectAccess(project.ref, { subject_type: subjectType, subject_id: subjectId, role });
    },
    onSuccess: () => {
      setSubjectId(subjectType === "team" ? teams[0]?.slug ?? "" : "");
      invalidate();
    },
  });
  const revokeMutation = useMutation({
    mutationFn: (grant: ProjectAccessGrant) => {
      if (!project) throw new Error("select a project first");
      return deleteProjectAccess(project.ref, grant.subject_type, grant.subject_id);
    },
    onSuccess: () => invalidate(),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!project || !subjectId.trim()) return;
    grantMutation.mutate();
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Access</p>
          <h2>Project-scoped grants</h2>
          <p className="mt-1 text-xs text-faint">These roles apply only to this project. Platform admins remain separate and can manage every project.</p>
        </div>
        <Shield size={15} className="text-faint" />
      </div>
      <form className="mt-4 grid grid-cols-[110px_minmax(0,1fr)_120px_auto] gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1" onSubmit={submit}>
        <select className="input" value={subjectType} onChange={(event) => {
          setSubjectType(event.target.value);
          setSubjectId(event.target.value === "team" ? teams[0]?.slug ?? "" : "");
        }}>
          <option value="team">Team</option>
          <option value="user">User</option>
        </select>
        {subjectType === "team" ? (
          <select className="input" value={subjectId} onChange={(event) => setSubjectId(event.target.value)}>
            {teams.length === 0 ? <option value="">No teams</option> : null}
            {teams.map((team) => (
              <option key={team.id} value={team.slug}>{team.name}</option>
            ))}
          </select>
        ) : (
          <input className="input" placeholder="user@example.com" value={subjectId} onChange={(event) => setSubjectId(event.target.value)} type="email" />
        )}
        <select className="input" value={role} onChange={(event) => setRole(event.target.value)}>
          {projectAccessRoles.map((item) => (
            <option key={item.value} value={item.value}>{item.label}</option>
          ))}
        </select>
        <button className="button secondary justify-center max-xl:col-span-2 max-sm:col-span-1" disabled={!project || !subjectId.trim() || grantMutation.isPending} type="submit">
          <Plus size={14} />
          Grant
        </button>
      </form>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading grants...</p> : null}
        {!loading && grants.length === 0 ? <p className="text-sm text-muted">No project-specific grants.</p> : null}
        {grants.map((grant) => (
          <div className="member-row" key={grant.id}>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{grant.subject_name}</p>
              <p className="truncate font-mono text-xs text-muted">{grant.subject_type} - {grant.subject_id}</p>
            </div>
            <div className="flex items-center gap-2">
              <span className="pill healthy">{projectAccessRoleLabel(grant.role)}</span>
              <span className="pill">project only</span>
              <button className="icon-button" disabled={revokeMutation.isPending} onClick={() => revokeMutation.mutate(grant)} type="button">
                <X size={14} />
              </button>
            </div>
          </div>
        ))}
      </div>
      {grantMutation.error ? <p className="mt-3 text-sm text-danger">{grantMutation.error.message}</p> : null}
      {revokeMutation.error ? <p className="mt-3 text-sm text-danger">{revokeMutation.error.message}</p> : null}
    </section>
  );
}

export function AccessScopePanel({ project, teams, grants }: { project?: Project; teams: Team[]; grants: ProjectAccessGrant[] }) {
  const teamGrants = grants.filter((grant) => grant.subject_type === "team").length;
  const userGrants = grants.filter((grant) => grant.subject_type === "user").length;
  const ownerGrants = grants.filter((grant) => grant.role === "owner").length;
  const adminGrants = grants.filter((grant) => grant.role === "admin").length;

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Access model</p>
          <h2>Global and project roles are separate</h2>
          <p className="mt-1 text-xs text-faint">Global admins manage the control plane. Project roles below only scope access to {project?.ref ?? "this project"}.</p>
        </div>
        <Shield size={15} className="text-faint" />
      </div>
      <div className="mt-4 grid grid-cols-4 gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
        <AccessScopeCard title="Global admin" detail="Can see and operate every org and project." metric="control plane" tone="healthy" />
        <AccessScopeCard title="Org members" detail="Org owner/admin/developer/viewer roles govern inherited org access." metric="org scope" />
        <AccessScopeCard title="Teams" detail="Teams group org members before project grants are applied." metric={`${teams.length} teams`} />
        <AccessScopeCard title="Project grants" detail="Explicit owner/admin/developer/viewer grants apply only here." metric={`${grants.length} grants`} tone={grants.length > 0 ? "healthy" : ""} />
      </div>
      <div className="mt-3 grid grid-cols-4 gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
        <div className="metric-cell">
          <p className="label">Team grants</p>
          <p className="truncate text-sm font-medium">{teamGrants}</p>
        </div>
        <div className="metric-cell">
          <p className="label">User grants</p>
          <p className="truncate text-sm font-medium">{userGrants}</p>
        </div>
        <div className="metric-cell">
          <p className="label">Project owners</p>
          <p className="truncate text-sm font-medium">{ownerGrants}</p>
        </div>
        <div className="metric-cell">
          <p className="label">Project admins</p>
          <p className="truncate text-sm font-medium">{adminGrants}</p>
        </div>
      </div>
    </section>
  );
}

function AccessScopeCard({ title, detail, metric, tone = "" }: { title: string; detail: string; metric: string; tone?: string }) {
  return (
    <div className="rounded-md border border-border bg-bg p-3">
      <div className="mb-2 flex items-center justify-between gap-2">
        <p className="truncate text-sm font-medium">{title}</p>
        <span className={`pill ${tone}`}>{metric}</span>
      </div>
      <p className="text-xs text-muted">{detail}</p>
    </div>
  );
}

export function AuthClientsPanel({ project, clients, loading }: { project?: Project; clients: ProjectAuthClient[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    name: "Dashboard App",
    client_id: "dashboard_app",
    client_secret_handle: "",
    redirect_uris: "https://app.example.com/auth/callback",
    grant_types: "authorization_code\nrefresh_token",
    scopes: "openid\nemail\nprofile",
    confidential: true,
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["auth-clients", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const createMutation = useMutation({
    mutationFn: ({ ref, input }: { ref: string; input: {
      name: string;
      client_id: string;
      client_secret_handle: string;
      redirect_uris: string[];
      grant_types: string[];
      scopes: string[];
      confidential: boolean;
    } }) => createProjectAuthClient(ref, input),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, clientId }: { ref: string; clientId: string }) => deleteProjectAuthClient(ref, clientId),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!project || form.name.trim().length === 0) {
      return;
    }
    createMutation.mutate({
      ref: project.ref,
      input: {
        name: form.name,
        client_id: form.client_id,
        client_secret_handle: form.client_secret_handle,
        redirect_uris: parseLines(form.redirect_uris),
        grant_types: parseLines(form.grant_types),
        scopes: parseLines(form.scopes),
        confidential: form.confidential,
      },
    });
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Auth</p>
          <h2>OAuth clients</h2>
        </div>
        <KeyRound size={15} className="text-faint" />
      </div>
      <form className="mt-4 grid gap-2" onSubmit={submit}>
        <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2 max-lg:grid-cols-1">
          <input className="input" placeholder="Dashboard App" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
          <input className="input font-mono" placeholder="dashboard_app" value={form.client_id} onChange={(event) => setForm({ ...form, client_id: event.target.value })} />
          <label className="segmented justify-start gap-2 px-3">
            <input checked={form.confidential} onChange={(event) => setForm({ ...form, confidential: event.target.checked })} type="checkbox" />
            Confidential
          </label>
        </div>
        <input className="input font-mono" placeholder="secret://projects/ref/auth/client" value={form.client_secret_handle} onChange={(event) => setForm({ ...form, client_secret_handle: event.target.value })} />
        <div className="grid grid-cols-3 gap-2 max-lg:grid-cols-1">
          <textarea className="input min-h-[64px] font-mono" value={form.redirect_uris} onChange={(event) => setForm({ ...form, redirect_uris: event.target.value })} />
          <textarea className="input min-h-[64px] font-mono" value={form.grant_types} onChange={(event) => setForm({ ...form, grant_types: event.target.value })} />
          <textarea className="input min-h-[64px] font-mono" value={form.scopes} onChange={(event) => setForm({ ...form, scopes: event.target.value })} />
        </div>
        <button className="button secondary justify-center" disabled={!project || createMutation.isPending || form.name.trim().length === 0} type="submit">
          <Save size={14} />
          Register client
        </button>
      </form>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading auth clients...</p> : null}
        {!loading && clients.length === 0 ? <p className="text-sm text-muted">No OAuth clients registered.</p> : null}
        {clients.map((client) => (
          <div className="vector-row" key={client.id}>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{client.name}</p>
              <p className="truncate font-mono text-xs text-muted">{client.client_id} - {client.redirect_uris.join(", ")}</p>
              <p className="truncate font-mono text-xs text-faint">{client.grant_types.join(", ")} - {client.scopes.join(", ")}</p>
            </div>
            <div className="flex items-center gap-2">
              <span className={`pill ${client.status === "registered" ? "healthy" : "provisioning"}`}>{client.status}</span>
              <span className={`pill ${client.confidential ? "healthy" : "provisioning"}`}>{client.confidential ? "confidential" : "public"}</span>
              <button className="icon-button" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, clientId: client.client_id })} type="button">
                <X size={14} />
              </button>
            </div>
          </div>
        ))}
        {createMutation.error ? <p className="text-sm text-danger">{createMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </section>
  );
}

export function AuthHooksPanel({ project, hooks, loading }: { project?: Project; hooks: ProjectAuthHook[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    hook_type: "custom_access_token",
    enabled: true,
    target_uri: "https://hooks.example.com/auth/token",
    edge_function: "",
    secret_handle: "",
    headers: "authorization=secret://projects/ref/auth/hook-header",
    timeout_ms: 5000,
    retry_attempts: 1,
  });
  const invalidate = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["auth-hooks", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", project?.org_id ?? ""] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const setMutation = useMutation({
    mutationFn: ({ ref, input }: { ref: string; input: {
      hook_type: string;
      enabled: boolean;
      target_uri: string;
      edge_function: string;
      secret_handle: string;
      headers: Record<string, string>;
      timeout_ms: number;
      retry_attempts: number;
    } }) => setProjectAuthHook(ref, input),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ref, hookType }: { ref: string; hookType: string }) => deleteProjectAuthHook(ref, hookType),
    onSuccess: (_, variables) => invalidate(variables.ref),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!project || form.hook_type.trim().length === 0) {
      return;
    }
    setMutation.mutate({
      ref: project.ref,
      input: {
        hook_type: form.hook_type,
        enabled: form.enabled,
        target_uri: form.target_uri,
        edge_function: form.edge_function,
        secret_handle: form.secret_handle,
        headers: parseKeyValueLines(form.headers),
        timeout_ms: form.timeout_ms,
        retry_attempts: form.retry_attempts,
      },
    });
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Auth</p>
          <h2>Auth Hooks</h2>
        </div>
        <SlidersHorizontal size={15} className="text-faint" />
      </div>
      <form className="mt-4 grid gap-2" onSubmit={submit}>
        <div className="grid grid-cols-[minmax(0,1fr)_auto_100px_100px] gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
          <select className="input" value={form.hook_type} onChange={(event) => setForm({ ...form, hook_type: event.target.value })}>
            <option value="custom_access_token">custom_access_token</option>
            <option value="before_user_created">before_user_created</option>
            <option value="send_sms">send_sms</option>
            <option value="send_email">send_email</option>
            <option value="mfa_verification_attempt">mfa_verification_attempt</option>
            <option value="password_verification_attempt">password_verification_attempt</option>
          </select>
          <label className="segmented justify-start gap-2 px-3">
            <input checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} type="checkbox" />
            Enabled
          </label>
          <input className="input" min={100} type="number" value={form.timeout_ms} onChange={(event) => setForm({ ...form, timeout_ms: Number(event.target.value) })} />
          <input className="input" min={0} max={5} type="number" value={form.retry_attempts} onChange={(event) => setForm({ ...form, retry_attempts: Number(event.target.value) })} />
        </div>
        <div className="grid grid-cols-2 gap-2 max-lg:grid-cols-1">
          <input className="input font-mono" placeholder="https://hooks.example.com/auth/token" value={form.target_uri} onChange={(event) => setForm({ ...form, target_uri: event.target.value })} />
          <input className="input font-mono" placeholder="edge-function-name" value={form.edge_function} onChange={(event) => setForm({ ...form, edge_function: event.target.value })} />
        </div>
        <input className="input font-mono" placeholder="secret://projects/ref/auth/hook" value={form.secret_handle} onChange={(event) => setForm({ ...form, secret_handle: event.target.value })} />
        <textarea className="input min-h-[64px] font-mono" value={form.headers} onChange={(event) => setForm({ ...form, headers: event.target.value })} />
        <button className="button secondary justify-center" disabled={!project || setMutation.isPending || form.hook_type.trim().length === 0} type="submit">
          <Save size={14} />
          Save hook
        </button>
      </form>
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading auth hooks...</p> : null}
        {!loading && hooks.length === 0 ? <p className="text-sm text-muted">No auth hooks configured.</p> : null}
        {hooks.map((hook) => (
          <div className="vector-row" key={hook.id}>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{hook.hook_type}</p>
              <p className="truncate font-mono text-xs text-muted">{hook.edge_function ? `edge:${hook.edge_function}` : hook.target_uri || "no target"}</p>
              <p className="truncate font-mono text-xs text-faint">{hook.timeout_ms}ms - {hook.retry_attempts} retries - {Object.keys(hook.headers).join(", ") || "no headers"}</p>
            </div>
            <div className="flex items-center gap-2">
              <span className={`pill ${hook.enabled ? "healthy" : "provisioning"}`}>{hook.enabled ? "enabled" : "disabled"}</span>
              <span className={`pill ${hook.status === "configured" ? "healthy" : "provisioning"}`}>{hook.status}</span>
              <button className="icon-button" disabled={!project || deleteMutation.isPending} onClick={() => project && deleteMutation.mutate({ ref: project.ref, hookType: hook.hook_type })} type="button">
                <X size={14} />
              </button>
            </div>
          </div>
        ))}
        {setMutation.error ? <p className="text-sm text-danger">{setMutation.error.message}</p> : null}
        {deleteMutation.error ? <p className="text-sm text-danger">{deleteMutation.error.message}</p> : null}
      </div>
    </section>
  );
}
