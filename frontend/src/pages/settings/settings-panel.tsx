import type { FormEvent } from "react";
import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Save, SlidersHorizontal } from "lucide-react";
import { updatePlatformDefaults, updatePlatformSSOConfig } from "../../api";
import { featureFlagGroups } from "../../lib/feature-flags";
import { platformSettingsSections, type PlatformSettingsSection } from "../../lib/project-config";
import type { PlatformDefaults, PlatformSSOConfig, ProvisionerStatus, SCIMGroup, SCIMListResponse, SCIMServiceProviderConfig, SCIMUser } from "../../types";

export function SettingsPanel({
  defaults,
  sso,
  provisionerStatus,
  scimServiceProviderConfig,
  scimUsers,
  scimGroups,
  section,
  loading,
}: {
  defaults?: PlatformDefaults;
  sso?: PlatformSSOConfig;
  provisionerStatus?: ProvisionerStatus;
  scimServiceProviderConfig?: SCIMServiceProviderConfig;
  scimUsers?: SCIMListResponse<SCIMUser>;
  scimGroups?: SCIMListResponse<SCIMGroup>;
  section: PlatformSettingsSection;
  loading: boolean;
}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [form, setForm] = useState({
    domain: "supadupa.test",
    stack_version: "latest",
    profile: "full",
    resource_tier: "small",
    backup_schedule: "daily",
    feature_flags: {} as Record<string, boolean>,
    smtp: {
      enabled: false,
      host: "",
      port: "587",
      sender_name: "",
      sender_email: "",
      username: "",
      password_handle: "",
      tls_mode: "starttls",
    },
  });
  const [ssoForm, setSSOForm] = useState({
    enabled: false,
    idp_entity_id: "",
    sso_url: "",
    certificate_pem: "",
    acs_url: "",
    metadata_url: "",
    email_domain: "",
    auto_provision: false,
    default_role: "developer",
  });

  useEffect(() => {
    if (!defaults) return;
    setForm({
      domain: defaults.domain,
      stack_version: defaults.stack_version,
      profile: defaults.profile,
      resource_tier: defaults.resource_tier,
      backup_schedule: defaults.backup_schedule,
      feature_flags: defaults.feature_flags ?? {},
      smtp: {
        enabled: defaults.smtp?.enabled ?? false,
        host: defaults.smtp?.host ?? "",
        port: String(defaults.smtp?.port ?? 587),
        sender_name: defaults.smtp?.sender_name ?? "",
        sender_email: defaults.smtp?.sender_email ?? "",
        username: defaults.smtp?.username ?? "",
        password_handle: defaults.smtp?.password_handle ?? "",
        tls_mode: defaults.smtp?.tls_mode ?? "starttls",
      },
    });
  }, [defaults]);

  useEffect(() => {
    if (!sso) return;
    setSSOForm({
      enabled: sso.enabled,
      idp_entity_id: sso.idp_entity_id,
      sso_url: sso.sso_url,
      certificate_pem: sso.certificate_pem,
      acs_url: sso.acs_url,
      metadata_url: sso.metadata_url,
      email_domain: sso.email_domain,
      auto_provision: sso.auto_provision,
      default_role: sso.default_role || "developer",
    });
  }, [sso]);

  const mutation = useMutation({
    mutationFn: updatePlatformDefaults,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["platform-defaults"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const ssoMutation = useMutation({
    mutationFn: updatePlatformSSOConfig,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["platform-sso"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

  const smtpPort = Number(form.smtp.port);
  const smtpPasswordHandle = form.smtp.password_handle.trim();
  const canSave = form.domain.trim().length > 0 &&
    form.stack_version.trim().length > 0 &&
    Number.isInteger(smtpPort) &&
    smtpPort >= 1 &&
    smtpPort <= 65535 &&
    (!form.smtp.enabled || form.smtp.host.trim().length > 0) &&
    (smtpPasswordHandle.length === 0 || smtpPasswordHandle.startsWith("secret://")) &&
    !loading;
  const ssoEnabled = ssoForm.enabled;
  const canSaveSSO = !loading && (!ssoEnabled || (
    ssoForm.idp_entity_id.trim().length > 0 &&
    ssoForm.sso_url.trim().length > 0 &&
    ssoForm.acs_url.trim().length > 0 &&
    ssoForm.certificate_pem.trim().length > 0
  ));
  const scimUserResources = scimUsers?.Resources ?? [];
  const scimGroupResources = scimGroups?.Resources ?? [];
  const scimAuthScheme = scimServiceProviderConfig?.authenticationSchemes?.[0];
  const provisioner = provisionerStatus?.provisioner ?? "loading";
  const provisionerMode = provisioner === "kubernetes" ? "Kubernetes operator" : provisioner === "compose" ? "Docker Compose" : provisioner;
  const enabledFeatures = Object.values(form.feature_flags).filter(Boolean).length;
  const activeSection = platformSettingsSections.find((item) => item.id === section) ?? platformSettingsSections[0];

  function submit(event: FormEvent) {
    event.preventDefault();
    mutation.mutate({
      domain: form.domain,
      stack_version: form.stack_version,
      profile: form.profile,
      resource_tier: form.resource_tier,
      backup_schedule: form.backup_schedule,
      feature_flags: form.feature_flags,
      smtp: {
        enabled: form.smtp.enabled,
        host: form.smtp.host,
        port: smtpPort,
        sender_name: form.smtp.sender_name,
        sender_email: form.smtp.sender_email,
        username: form.smtp.username,
        password_handle: smtpPasswordHandle,
        tls_mode: form.smtp.tls_mode,
      },
    });
  }

  const setSMTPForm = (patch: Partial<typeof form.smtp>) => setForm({ ...form, smtp: { ...form.smtp, ...patch } });
  const setFeatureFlag = (key: string, value: boolean) => setForm({ ...form, feature_flags: { ...form.feature_flags, [key]: value } });

  function submitSSO(event: FormEvent) {
    event.preventDefault();
    ssoMutation.mutate(ssoForm);
  }

  function openSection(target: PlatformSettingsSection) {
    if (target === "overview") {
      void navigate({ to: "/settings" });
      return;
    }
    void navigate({ to: "/settings/$section", params: { section: target } });
  }

  return (
    <section className="panel">
      <div className="section-head">
        <div>
          <p className="label">Settings</p>
          <h2>{activeSection.label}</h2>
          <p className="mt-1 text-sm text-muted">{activeSection.description}</p>
        </div>
        <SlidersHorizontal size={15} className="text-faint" />
      </div>

      {section === "overview" ? (
        <div className="mt-4 grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-md:grid-cols-1">
          <SummaryCard label="Defaults" title={`${form.profile} / ${form.resource_tier}`} detail={`${form.domain} · ${form.stack_version} · ${form.backup_schedule} backups`} onClick={() => openSection("defaults")} />
          <SummaryCard label="Feature flags" title={`${enabledFeatures} enabled`} detail="Local, Compose, and enterprise feature availability." onClick={() => openSection("features")} />
          <SummaryCard label="Platform SMTP" title={form.smtp.enabled ? "Enabled" : "Disabled"} detail={form.smtp.enabled ? `${form.smtp.host || "host pending"}:${form.smtp.port} · ${form.smtp.tls_mode}` : "Control-plane mail is not configured."} status={form.smtp.enabled ? "healthy" : "paused"} onClick={() => openSection("smtp")} />
          <SummaryCard label="Platform SSO" title={ssoForm.enabled ? "Enabled" : "Disabled"} detail={ssoForm.enabled ? `${ssoForm.idp_entity_id || "IdP pending"} · ${ssoForm.email_domain || "any domain"}` : "Password login only."} status={ssoForm.enabled ? "healthy" : "paused"} onClick={() => openSection("sso")} />
          <SummaryCard label="SCIM" title={scimServiceProviderConfig ? "Available" : "Loading"} detail={`${scimUsers?.totalResults ?? 0} users · ${scimGroups?.totalResults ?? 0} groups · ${scimAuthScheme?.type ?? "oauthbearertoken"}`} status={scimServiceProviderConfig ? "healthy" : "paused"} onClick={() => openSection("scim")} />
          <SummaryCard label="Hosts" title={provisionerMode} detail="Operator-owned capacity and provisioner inventory." status={provisioner === "unconfigured" ? "warning" : provisioner === "loading" ? "paused" : "healthy"} onClick={() => openSection("hosts")} />
        </div>
      ) : null}

      {section === "defaults" ? (
        <form className="mt-4 grid gap-3" onSubmit={submit}>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Provisioner substrate</p>
              <p className="truncate text-xs text-muted">{provisionerMode} · selected by SUPADUPA_PROVISIONER</p>
            </div>
            <span className={`pill ${provisioner === "unconfigured" ? "warning" : provisioner === "loading" ? "paused" : "healthy"}`}>{provisioner}</span>
          </div>
          <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
            <label className="grid gap-1">
              <span className="label">Base domain</span>
              <input className="input" value={form.domain} onChange={(event) => setForm({ ...form, domain: event.target.value })} />
            </label>
            <label className="grid gap-1">
              <span className="label">Stack version</span>
              <input className="input font-mono" value={form.stack_version} onChange={(event) => setForm({ ...form, stack_version: event.target.value })} />
            </label>
            <label className="grid gap-1">
              <span className="label">Profile</span>
              <select className="input" value={form.profile} onChange={(event) => setForm({ ...form, profile: event.target.value })}>
                <option value="full">Full</option>
                <option value="essential">Essential</option>
                <option value="orioledb">OrioleDB</option>
              </select>
            </label>
            <label className="grid gap-1">
              <span className="label">Tier</span>
              <select className="input" value={form.resource_tier} onChange={(event) => setForm({ ...form, resource_tier: event.target.value })}>
                <option value="small">Small</option>
                <option value="medium">Medium</option>
                <option value="large">Large</option>
              </select>
            </label>
            <label className="grid gap-1">
              <span className="label">Backup schedule</span>
              <select className="input" value={form.backup_schedule} onChange={(event) => setForm({ ...form, backup_schedule: event.target.value })}>
                <option value="daily">Daily</option>
                <option value="hourly">Hourly</option>
              </select>
            </label>
          </div>
          <SaveRow disabled={!canSave || mutation.isPending} detail={`${form.stack_version} · ${form.profile} · ${form.resource_tier} · ${form.backup_schedule}`} title="New project defaults" />
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </form>
      ) : null}

      {section === "features" ? (
        <form className="mt-4 grid gap-3" onSubmit={submit}>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Feature flags</p>
              <p className="truncate text-xs text-muted">Use default org mode for local installs; keep org/team/project RBAC as the access model for enterprise growth.</p>
            </div>
            <span className="pill">{enabledFeatures} enabled</span>
          </div>
          <div className="grid grid-cols-3 gap-3 max-xl:grid-cols-1">
            {featureFlagGroups.map((group) => (
              <div className="grid gap-2 rounded-md border border-border bg-bg p-3" key={group.label}>
                <p className="label">{group.label}</p>
                {group.flags.map(([key, label]) => (
                  <label className="flex items-center justify-between gap-3 text-sm text-muted" key={key}>
                    <span>{label}</span>
                    <input checked={Boolean(form.feature_flags[key])} onChange={(event) => setFeatureFlag(key, event.target.checked)} type="checkbox" />
                  </label>
                ))}
              </div>
            ))}
          </div>
          <SaveRow disabled={!canSave || mutation.isPending} detail="Flags gate UI surfaces and API availability for local, Compose, and enterprise installs." title="Rollout policy" />
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </form>
      ) : null}

      {section === "smtp" ? (
        <form className="mt-4 grid gap-3" onSubmit={submit}>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Platform SMTP</p>
              <p className="truncate text-xs text-muted">{form.smtp.enabled ? `${form.smtp.host || "host pending"}:${form.smtp.port} · ${form.smtp.tls_mode}` : "Disabled for platform mail"}</p>
            </div>
            <span className={`pill ${form.smtp.enabled ? "healthy" : "paused"}`}>{form.smtp.enabled ? "enabled" : "disabled"}</span>
          </div>
          <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
            <label className="flex items-center gap-2 rounded-md border border-border px-3 py-2 text-sm text-muted">
              <input checked={form.smtp.enabled} onChange={(event) => setSMTPForm({ enabled: event.target.checked })} type="checkbox" />
              Enable platform SMTP
            </label>
            <label className="grid gap-1">
              <span className="label">TLS mode</span>
              <select className="input" value={form.smtp.tls_mode} onChange={(event) => setSMTPForm({ tls_mode: event.target.value })}>
                <option value="starttls">STARTTLS</option>
                <option value="implicit">Implicit TLS</option>
                <option value="none">None</option>
              </select>
            </label>
            <SMTPInput label="SMTP host" value={form.smtp.host} onChange={(value) => setSMTPForm({ host: value })} mono />
            <SMTPInput label="Port" value={form.smtp.port} onChange={(value) => setSMTPForm({ port: value })} mono numeric />
            <SMTPInput label="Sender name" value={form.smtp.sender_name} onChange={(value) => setSMTPForm({ sender_name: value })} />
            <SMTPInput label="Sender email" value={form.smtp.sender_email} onChange={(value) => setSMTPForm({ sender_email: value })} />
            <SMTPInput label="Username" value={form.smtp.username} onChange={(value) => setSMTPForm({ username: value })} mono />
            <SMTPInput label="Password handle" value={form.smtp.password_handle} onChange={(value) => setSMTPForm({ password_handle: value })} mono placeholder="secret://platform/smtp-password" />
          </div>
          <SaveRow disabled={!canSave || mutation.isPending} detail="Password handles must point at secret:// references; raw SMTP passwords stay out of the meta DB payload." title="Secret policy" />
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </form>
      ) : null}

      {section === "sso" ? (
        <form className="mt-4 grid gap-3" onSubmit={submitSSO}>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Platform SAML SSO</p>
              <p className="truncate text-xs text-muted">{ssoForm.enabled ? `${ssoForm.idp_entity_id || "IdP pending"} · ${ssoForm.email_domain || "any domain"}` : "Password login only"}</p>
            </div>
            <span className={`pill ${ssoForm.enabled ? "healthy" : "paused"}`}>{ssoForm.enabled ? "enabled" : "disabled"}</span>
          </div>
          <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
            <label className="flex items-center gap-2 rounded-md border border-border px-3 py-2 text-sm text-muted">
              <input checked={ssoForm.enabled} onChange={(event) => setSSOForm({ ...ssoForm, enabled: event.target.checked })} type="checkbox" />
              Enable SAML
            </label>
            <label className="flex items-center gap-2 rounded-md border border-border px-3 py-2 text-sm text-muted">
              <input checked={ssoForm.auto_provision} onChange={(event) => setSSOForm({ ...ssoForm, auto_provision: event.target.checked })} type="checkbox" />
              Auto-provision users
            </label>
            <SSOInput label="IdP entity ID" value={ssoForm.idp_entity_id} onChange={(value) => setSSOForm({ ...ssoForm, idp_entity_id: value })} mono />
            <SSOInput label="SSO login URL" value={ssoForm.sso_url} onChange={(value) => setSSOForm({ ...ssoForm, sso_url: value })} mono />
            <SSOInput label="ACS URL" value={ssoForm.acs_url} onChange={(value) => setSSOForm({ ...ssoForm, acs_url: value })} mono />
            <SSOInput label="Metadata URL" value={ssoForm.metadata_url} onChange={(value) => setSSOForm({ ...ssoForm, metadata_url: value })} mono />
            <SSOInput label="Email domain" value={ssoForm.email_domain} onChange={(value) => setSSOForm({ ...ssoForm, email_domain: value })} />
            <label className="grid gap-1">
              <span className="label">Default role</span>
              <select className="input" value={ssoForm.default_role} onChange={(event) => setSSOForm({ ...ssoForm, default_role: event.target.value })}>
                <option value="developer">Developer</option>
                <option value="viewer">Viewer</option>
                <option value="admin">Admin</option>
              </select>
            </label>
          </div>
          <label className="grid gap-1">
            <span className="label">Signing certificate PEM</span>
            <textarea className="input min-h-24 font-mono text-xs leading-5" value={ssoForm.certificate_pem} onChange={(event) => setSSOForm({ ...ssoForm, certificate_pem: event.target.value })} />
          </label>
          <SaveRow disabled={!canSaveSSO || ssoMutation.isPending} detail={ssoForm.acs_url || "Set the ACS URL exposed by this control plane"} title="SAML callback" />
          {ssoMutation.error ? <p className="text-sm text-danger">{ssoMutation.error.message}</p> : null}
        </form>
      ) : null}

      {section === "scim" ? (
        <div className="mt-4 grid gap-3">
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Platform SCIM provisioning</p>
              <p className="truncate text-xs text-muted">
                {scimServiceProviderConfig ? `${scimAuthScheme?.name ?? "Bearer token"} · patch ${scimServiceProviderConfig.patch.supported ? "supported" : "off"}` : "Loading service provider config"}
              </p>
            </div>
            <span className={`pill ${scimServiceProviderConfig ? "healthy" : "paused"}`}>{scimServiceProviderConfig ? "available" : "loading"}</span>
          </div>
          <div className="grid grid-cols-3 gap-2 max-lg:grid-cols-1">
            <Metric label="Users" value={(scimUsers?.totalResults ?? 0).toString()} />
            <Metric label="Groups" value={(scimGroups?.totalResults ?? 0).toString()} />
            <Metric label="Auth" value={scimAuthScheme?.type ?? "oauthbearertoken"} />
          </div>
          <div className="grid grid-cols-2 gap-3 max-lg:grid-cols-1">
            <SCIMUsers users={scimUserResources} />
            <SCIMGroups groups={scimGroupResources} />
          </div>
        </div>
      ) : null}
    </section>
  );
}

function SummaryCard({ detail, label, onClick, status, title }: { label: string; title: string; detail: string; status?: "healthy" | "warning" | "paused"; onClick: () => void }) {
  return (
    <button className="grid min-h-36 content-between gap-4 rounded-md border border-border bg-bg p-3 text-left transition hover:border-border-strong hover:bg-surface-2" onClick={onClick} type="button">
      <span className="flex items-start justify-between gap-3">
        <span className="min-w-0">
          <span className="label">{label}</span>
          <span className="mt-1 block truncate text-base font-semibold">{title}</span>
        </span>
        {status ? <span className={`pill ${status}`}>{status}</span> : null}
      </span>
      <span className="text-sm text-muted">{detail}</span>
    </button>
  );
}

function SaveRow({ detail, disabled, title }: { title: string; detail: string; disabled: boolean }) {
  return (
    <div className="usage-row">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{title}</p>
        <p className="truncate text-xs text-muted">{detail}</p>
      </div>
      <button className="button secondary justify-center" disabled={disabled} type="submit">
        <Save size={14} />
        Save
      </button>
    </div>
  );
}

function SMTPInput({ label, mono = false, numeric = false, onChange, placeholder, value }: { label: string; value: string; onChange: (value: string) => void; mono?: boolean; numeric?: boolean; placeholder?: string }) {
  return (
    <label className="grid gap-1">
      <span className="label">{label}</span>
      <input className={`input ${mono ? "font-mono" : ""}`} inputMode={numeric ? "numeric" : undefined} placeholder={placeholder} value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function SSOInput({ label, mono = false, onChange, value }: { label: string; value: string; onChange: (value: string) => void; mono?: boolean }) {
  return (
    <label className="grid gap-1">
      <span className="label">{label}</span>
      <input className={`input ${mono ? "font-mono" : ""}`} value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function SCIMUsers({ users }: { users: SCIMUser[] }) {
  return (
    <div className="grid gap-2">
      <p className="label">Provisioned users</p>
      {users.length === 0 ? <p className="text-sm text-muted">No SCIM users returned.</p> : null}
      {users.slice(0, 5).map((user) => (
        <div className="usage-row" key={user.id}>
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">{user.userName}</p>
            <p className="truncate text-xs text-muted">{user["urn:supadupa:params:scim:schemas:extension:User"]?.role ?? "role pending"} · {user.meta.resourceType}</p>
          </div>
          <span className={`pill ${user.active ? "healthy" : "paused"}`}>{user.active ? "active" : "inactive"}</span>
        </div>
      ))}
    </div>
  );
}

function SCIMGroups({ groups }: { groups: SCIMGroup[] }) {
  return (
    <div className="grid gap-2">
      <p className="label">Provisioned groups</p>
      {groups.length === 0 ? <p className="text-sm text-muted">No SCIM groups returned.</p> : null}
      {groups.slice(0, 5).map((group) => (
        <div className="usage-row" key={group.id}>
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">{group.displayName}</p>
            <p className="truncate text-xs text-muted">{group["urn:supadupa:params:scim:schemas:extension:Group"]?.slug ?? group.id} · {group.members?.length ?? 0} members</p>
          </div>
          <span className="pill">{group.meta.resourceType}</span>
        </div>
      ))}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="metric-cell">
      <p className="label">{label}</p>
      <p className="truncate text-sm font-medium">{value}</p>
    </div>
  );
}
