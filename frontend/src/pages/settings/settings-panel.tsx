import type { FormEvent } from "react";
import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowLeft, Play, Plus, Save, ShieldCheck, SlidersHorizontal, TestTube2, Trash2 } from "lucide-react";
import { createBackupStorageTarget, deleteBackupStorageTarget, restorePlatformBackup, testBackupStorageTarget, triggerPlatformBackup, updateBackupStorageTarget, updatePlatformDefaults, updatePlatformSSOConfig, type BackupStorageTargetInput } from "../../api";
import { DataTable } from "../../components/data-table";
import { featureFlagGroups } from "../../lib/feature-flags";
import { formatBytes, formatDateTime, shortChecksum } from "../../lib/format";
import { platformSettingsSections, type PlatformSettingsSection } from "../../lib/project-config";
import type { BackupStorageTarget, PlatformBackup, PlatformDefaults, PlatformSSOConfig, ProvisionerStatus, RuntimeConfig, SCIMGroup, SCIMListResponse, SCIMServiceProviderConfig, SCIMUser, StackReleaseManifest } from "../../types";

function backupTargetReadinessLabel(target: BackupStorageTarget) {
  switch (target.readiness_status) {
    case "off-host-ready":
      return "off-host ready";
    case "validation-failed":
      return "test failed";
    case "validation-pending":
      return "test needed";
    case "local-or-loopback":
      return "local only";
    case "missing-secret":
      return "secret missing";
    default:
      return target.recovery_ready ? "off-host ready" : "not ready";
  }
}

function backupTargetReadinessClass(target: BackupStorageTarget) {
  if (target.recovery_ready) return "healthy";
  if (target.readiness_status === "validation-failed" || target.readiness_status === "local-or-loopback" || target.readiness_status === "missing-secret") return "error";
  return "warning";
}

export function SettingsPanel({
  defaults,
  sso,
  backupStorageTargets,
  platformBackups,
  stackReleases,
  provisionerStatus,
  runtimeConfig,
  scimServiceProviderConfig,
  scimUsers,
  scimGroups,
  item,
  section,
  loading,
}: {
  defaults?: PlatformDefaults;
  sso?: PlatformSSOConfig;
  backupStorageTargets: BackupStorageTarget[];
  platformBackups: PlatformBackup[];
  stackReleases: StackReleaseManifest[];
  provisionerStatus?: ProvisionerStatus;
  runtimeConfig?: RuntimeConfig;
  scimServiceProviderConfig?: SCIMServiceProviderConfig;
  scimUsers?: SCIMListResponse<SCIMUser>;
  scimGroups?: SCIMListResponse<SCIMGroup>;
  item?: string;
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
    scim_enabled: false,
    scim_token_configured: false,
    scim_token: "",
  });
  const [backupTargetForm, setBackupTargetForm] = useState<BackupStorageTargetInput>({
    name: "",
    type: "s3",
    endpoint: "",
    region: "auto",
    bucket: "",
    prefix: "supadupa",
    access_key_id: "",
    secret_access_key: "",
    force_path_style: true,
    default: backupStorageTargets.length === 0,
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
      scim_enabled: sso.scim_enabled,
      scim_token_configured: sso.scim_token_configured,
      scim_token: "",
    });
  }, [sso]);

  useEffect(() => {
    if (section !== "backups" || !item) return;
    if (item === "new") {
      setBackupTargetForm({
        name: "",
        type: "s3",
        endpoint: "",
        region: "auto",
        bucket: "",
        prefix: "supadupa",
        access_key_id: "",
        secret_access_key: "",
        force_path_style: true,
        default: backupStorageTargets.length === 0,
      });
      return;
    }
    const target = backupStorageTargets.find((candidate) => candidate.id === item);
    if (!target) return;
    setBackupTargetForm({
      name: target.name,
      type: target.type,
      endpoint: target.endpoint,
      region: target.region,
      bucket: target.bucket,
      prefix: target.prefix ?? "",
      access_key_id: target.access_key_id ?? "",
      secret_access_key: "",
      force_path_style: target.force_path_style,
      default: target.default,
    });
  }, [backupStorageTargets, item, section]);

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
  const createTargetMutation = useMutation({
    mutationFn: createBackupStorageTarget,
    onSuccess: () => {
      resetBackupTargetForm();
      closeBackupTargetForm();
      void queryClient.invalidateQueries({ queryKey: ["backup-storage-targets"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const updateTargetMutation = useMutation({
    mutationFn: ({ id, input }: { id: string; input: BackupStorageTargetInput }) => updateBackupStorageTarget(id, input),
    onSuccess: () => {
      resetBackupTargetForm();
      closeBackupTargetForm();
      void queryClient.invalidateQueries({ queryKey: ["backup-storage-targets"] });
      void queryClient.invalidateQueries({ queryKey: ["backup-policy"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const deleteTargetMutation = useMutation({
    mutationFn: deleteBackupStorageTarget,
    onSuccess: () => {
      resetBackupTargetForm();
      void queryClient.invalidateQueries({ queryKey: ["backup-storage-targets"] });
      void queryClient.invalidateQueries({ queryKey: ["backup-policy"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const testTargetMutation = useMutation({
    mutationFn: testBackupStorageTarget,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["backup-storage-targets"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const platformBackupMutation = useMutation({
    mutationFn: triggerPlatformBackup,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["platform-backups"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const platformRestoreMutation = useMutation({
    mutationFn: restorePlatformBackup,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["platform-backups"] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
      void queryClient.invalidateQueries({ queryKey: ["projects"] });
      void queryClient.invalidateQueries({ queryKey: ["orgs"] });
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
  const backupTargetItem = section === "backups" ? item : undefined;
  const editingBackupTarget = backupTargetItem && backupTargetItem !== "new" ? backupStorageTargets.find((target) => target.id === backupTargetItem) : undefined;
  const showingBackupTargetForm = section === "backups" && Boolean(backupTargetItem);
  const showingSMTPForm = section === "smtp" && item === "configure";
  const showingSSOForm = section === "sso" && item === "configure";
  const canSaveBackupTarget = !loading &&
    backupTargetForm.name.trim().length > 0 &&
    backupTargetForm.bucket.trim().length > 0 &&
    backupTargetForm.access_key_id.trim().length > 0 &&
    (Boolean(editingBackupTarget) || (backupTargetForm.secret_access_key ?? "").trim().length > 0);
  const scimUserResources = scimUsers?.Resources ?? [];
  const scimGroupResources = scimGroups?.Resources ?? [];
  const scimAuthScheme = scimServiceProviderConfig?.authenticationSchemes?.[0];
  const provisioner = provisionerStatus?.provisioner ?? "loading";
  const provisionerMode = provisioner === "kubernetes" ? "Kubernetes operator" : provisioner === "compose" ? "Docker Compose" : provisioner;
  const runtimeBackup = runtimeConfig?.backup;
  const recoveryGuardOn = runtimeConfig?.recovery.require_recovery_ready_targets ?? false;
  const durableUpgradeGuardOn = runtimeConfig?.upgrade.require_durable_backup ?? false;
  const backupCommandsReady = Boolean(runtimeBackup?.logical_configured && runtimeBackup.physical_configured && runtimeBackup.wal_archive_configured && runtimeBackup.logical_restore_configured && runtimeBackup.pitr_restore_configured);
  const backupDryRunOff = Boolean(runtimeBackup && !runtimeBackup.backup_dry_run && !runtimeBackup.restore_dry_run && !runtimeBackup.wal_archive_dry_run);
  const recoveryReadyTargets = backupStorageTargets.filter((target) => target.recovery_ready);
  const defaultRecoveryTarget = backupStorageTargets.find((target) => target.default && target.recovery_ready);
  const hostedRecoveryMode = recoveryGuardOn && durableUpgradeGuardOn && backupCommandsReady && backupDryRunOff && recoveryReadyTargets.length > 0;
  const runtimeGuardRows = [
    {
      label: "Recovery guard",
      value: recoveryGuardOn ? "enforced" : "off",
      state: recoveryGuardOn ? "healthy" : "warning",
      detail: recoveryGuardOn ? "Physical backups and WAL archives require tested off-host targets." : "Set SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true and restart the API.",
    },
    {
      label: "Upgrade guard",
      value: durableUpgradeGuardOn ? "enforced" : "off",
      state: durableUpgradeGuardOn ? "healthy" : "warning",
      detail: durableUpgradeGuardOn ? "Stack upgrades require durable pre-upgrade backup artifacts." : "Set SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true for production upgrades.",
    },
    {
      label: "Backup runtime",
      value: backupCommandsReady && backupDryRunOff ? "ready" : runtimeConfig ? "incomplete" : "loading",
      state: backupCommandsReady && backupDryRunOff ? "healthy" : "warning",
      detail: runtimeConfig ? `${runtimeConfig.provisioner} · ${runtimeBackup?.compose_defaults ? "Compose defaults" : "custom commands"}${backupDryRunOff ? "" : " · dry-run enabled"}` : "Runtime configuration has not loaded yet.",
    },
    {
      label: "Off-host target",
      value: defaultRecoveryTarget?.name ?? (recoveryReadyTargets.length > 0 ? `${recoveryReadyTargets.length} ready` : "not ready"),
      state: recoveryReadyTargets.length > 0 ? "healthy" : "warning",
      detail: defaultRecoveryTarget ? "Default target is tested and recovery-ready." : recoveryReadyTargets.length > 0 ? "A target is ready; make one default for platform backups." : "Add and test an off-host S3/R2 target.",
    },
  ];
  const enabledFeatures = Object.values(form.feature_flags).filter(Boolean).length;
  const activeSection = platformSettingsSections.find((item) => item.id === section) ?? platformSettingsSections[0];
  const latestRelease = stackReleases[0];
  const platformBackupColumns = useMemo<ColumnDef<PlatformBackup>[]>(
    () => [
      {
        header: "Backup",
        accessorKey: "kind",
        size: 150,
        cell: ({ row }) => (
          <>
            <p className="cell-main capitalize">{row.original.kind}</p>
            <p className="cell-sub font-mono">{row.original.id.slice(0, 12)}</p>
          </>
        ),
      },
      {
        header: "Status",
        accessorKey: "status",
        size: 120,
        cell: ({ row }) => <span className={`pill ${row.original.status === "completed" ? "healthy" : "provisioning"}`}>{row.original.status}</span>,
      },
      {
        header: "Started",
        accessorKey: "started_at",
        size: 150,
        cell: ({ row }) => formatDateTime(row.original.started_at ?? row.original.created_at),
      },
      {
        header: "Finished",
        accessorKey: "finished_at",
        size: 150,
        cell: ({ row }) => row.original.finished_at || row.original.verified_at ? formatDateTime(row.original.finished_at ?? row.original.verified_at ?? "") : "pending",
      },
      {
        header: "Artifact",
        accessorKey: "location",
        size: 340,
        cell: ({ row }) => (
          <>
            <p className="truncate font-mono text-xs text-muted">{row.original.remote_location || row.original.location}</p>
            {row.original.checksum_sha256 ? <p className="cell-sub font-mono">sha256:{shortChecksum(row.original.checksum_sha256)}</p> : null}
          </>
        ),
      },
      {
        header: "Size",
        accessorKey: "size_bytes",
        size: 100,
        cell: ({ row }) => formatBytes(row.original.size_bytes),
      },
      {
        header: "",
        id: "actions",
        size: 52,
        cell: ({ row }) => (
          <button className="icon-button" disabled={platformRestoreMutation.isPending || row.original.status !== "completed"} onClick={() => confirmPlatformRestore(row.original)} title="Restore control plane" type="button">
            <Play size={14} />
          </button>
        ),
      },
    ],
    [platformRestoreMutation.isPending],
  );
  const backupTargetColumns = useMemo<ColumnDef<BackupStorageTarget>[]>(
    () => [
      {
        header: "Target",
        accessorKey: "name",
        size: 180,
        cell: ({ row }) => (
          <button className="min-w-0 text-left" onClick={() => editBackupTarget(row.original)} type="button">
            <p className="cell-main truncate">{row.original.name}</p>
            <p className="cell-sub font-mono">key {row.original.access_key_id || "pending"}</p>
          </button>
        ),
      },
      {
        header: "Destination",
        accessorKey: "bucket",
        size: 320,
        cell: ({ row }) => (
          <>
            <p className="truncate font-mono text-xs text-muted">s3://{row.original.bucket}/{row.original.prefix ? `${row.original.prefix}/` : ""}</p>
            <p className="cell-sub truncate">{row.original.region} · {row.original.endpoint || "AWS endpoint"}</p>
          </>
        ),
      },
      {
        header: "Recovery",
        accessorKey: "readiness_status",
        size: 190,
        cell: ({ row }) => (
          <>
            <span className={`pill ${backupTargetReadinessClass(row.original)}`}>{backupTargetReadinessLabel(row.original)}</span>
            <p className="cell-sub truncate">{row.original.default ? "default" : "available"} · {row.original.secret_configured ? "secret set" : "secret missing"}</p>
            {row.original.readiness_message ? <p className="cell-sub truncate">{row.original.readiness_message}</p> : null}
          </>
        ),
      },
      {
        header: "Last test",
        accessorKey: "last_tested_at",
        size: 180,
        cell: ({ row }) => row.original.last_test_status ? (
          <>
            <p className={row.original.last_test_status === "passed" ? "text-success" : "text-danger"}>{row.original.last_test_status}</p>
            <p className="cell-sub">{row.original.last_tested_at ? formatDateTime(row.original.last_tested_at) : "not recorded"}</p>
            {row.original.last_test_error ? <p className="cell-sub truncate text-danger">{row.original.last_test_error}</p> : null}
          </>
        ) : "Not tested",
      },
      {
        header: "Updated",
        accessorKey: "updated_at",
        size: 150,
        cell: ({ row }) => formatDateTime(row.original.updated_at),
      },
      {
        header: "",
        id: "actions",
        size: 96,
        cell: ({ row }) => (
          <div className="flex justify-end gap-2">
            <button className="icon-button" disabled={testTargetMutation.isPending} onClick={() => testTargetMutation.mutate(row.original.id)} title="Test target" type="button">
              <TestTube2 size={14} />
            </button>
            <button className="icon-button" disabled={deleteTargetMutation.isPending} onClick={() => deleteTargetMutation.mutate(row.original.id)} title="Delete target" type="button">
              <Trash2 size={14} />
            </button>
          </div>
        ),
      },
    ],
    [deleteTargetMutation.isPending, testTargetMutation.isPending],
  );

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
    ssoMutation.mutate(ssoForm, {
      onSuccess: () => setSSOForm((current) => ({ ...current, scim_token: "", scim_token_configured: current.scim_token_configured || current.scim_token.trim().length > 0 })),
    });
  }

  function resetBackupTargetForm() {
    setBackupTargetForm({
      name: "",
      type: "s3",
      endpoint: "",
      region: "auto",
      bucket: "",
      prefix: "supadupa",
      access_key_id: "",
      secret_access_key: "",
      force_path_style: true,
      default: backupStorageTargets.length === 0,
    });
  }

  function closeBackupTargetForm() {
    void navigate({ to: "/settings/$section", params: { section: "backups" } });
  }

  function newBackupTarget() {
    void navigate({ to: "/settings/$section/$item", params: { section: "backups", item: "new" } });
  }

  function editBackupTarget(target: BackupStorageTarget) {
    void navigate({ to: "/settings/$section/$item", params: { section: "backups", item: target.id } });
  }

  function openSMTPForm() {
    void navigate({ to: "/settings/$section/$item", params: { section: "smtp", item: "configure" } });
  }

  function closeSMTPForm() {
    void navigate({ to: "/settings/$section", params: { section: "smtp" } });
  }

  function openSSOForm() {
    void navigate({ to: "/settings/$section/$item", params: { section: "sso", item: "configure" } });
  }

  function closeSSOForm() {
    void navigate({ to: "/settings/$section", params: { section: "sso" } });
  }

  function submitBackupTarget(event: FormEvent) {
    event.preventDefault();
    const input = {
      ...backupTargetForm,
      type: backupTargetForm.type || "s3",
      endpoint: backupTargetForm.endpoint.trim(),
      region: backupTargetForm.region.trim() || "auto",
      bucket: backupTargetForm.bucket.trim(),
      prefix: backupTargetForm.prefix.trim(),
      access_key_id: backupTargetForm.access_key_id.trim(),
      secret_access_key: backupTargetForm.secret_access_key?.trim() ?? "",
    };
    if (editingBackupTarget) {
      updateTargetMutation.mutate({ id: editingBackupTarget.id, input });
      return;
    }
    createTargetMutation.mutate(input);
  }

  function confirmPlatformRestore(backup: PlatformBackup) {
    const confirmation = window.prompt(`Restore Supadupa control-plane state from ${backup.id}? Type restore-control-plane to continue.`);
    if (confirmation !== "restore-control-plane") return;
    platformRestoreMutation.mutate(backup.id);
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
          <SummaryCard label="Backups" title={`${backupStorageTargets.length} targets`} detail={`${platformBackups.length} control-plane backups · ${backupStorageTargets.filter((target) => target.default).length} default target`} status={backupStorageTargets.length > 0 ? "healthy" : "paused"} onClick={() => openSection("backups")} />
          <SummaryCard label="Platform SMTP" title={form.smtp.enabled ? "Enabled" : "Disabled"} detail={form.smtp.enabled ? `${form.smtp.host || "host pending"}:${form.smtp.port} · ${form.smtp.tls_mode}` : "Control-plane mail is not configured."} status={form.smtp.enabled ? "healthy" : "paused"} onClick={() => openSection("smtp")} />
          <SummaryCard label="Platform SSO" title={ssoForm.enabled ? "Enabled" : "Disabled"} detail={ssoForm.enabled ? `${ssoForm.idp_entity_id || "IdP pending"} · ${ssoForm.email_domain || "any domain"}` : "Password login only."} status={ssoForm.enabled ? "healthy" : "paused"} onClick={() => openSection("sso")} />
          <SummaryCard label="SCIM" title={ssoForm.scim_enabled ? "Enabled" : "Disabled"} detail={`${scimUsers?.totalResults ?? 0} users · ${scimGroups?.totalResults ?? 0} groups · ${ssoForm.scim_token_configured ? "token set" : "token missing"}`} status={ssoForm.scim_enabled && ssoForm.scim_token_configured ? "healthy" : "paused"} onClick={() => openSection("scim")} />
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
              <select className="input font-mono" value={form.stack_version} onChange={(event) => setForm({ ...form, stack_version: event.target.value })}>
                <option value="latest">latest{latestRelease ? ` (${latestRelease.version})` : ""}</option>
                {stackReleases.map((release) => (
                  <option key={release.version} value={release.version}>{release.version}</option>
                ))}
              </select>
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

      {section === "backups" ? (
        <div className="mt-4 grid gap-3">
          <div className="usage-row">
            <div className="min-w-0">
              <p className="flex items-center gap-2 truncate text-sm font-medium">
                <ShieldCheck size={14} />
                Hosted-grade recovery mode
              </p>
              <p className="truncate text-xs text-muted">{hostedRecoveryMode ? "Durable off-host backups, WAL archives, PITR, and upgrades are guarded." : "This control plane is still in local/dev recovery mode until every guard below is ready."}</p>
            </div>
            <span className={`pill ${hostedRecoveryMode ? "healthy" : "warning"}`}>{hostedRecoveryMode ? "production ready" : "not production"}</span>
          </div>
          <div className="grid grid-cols-4 gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
            {runtimeGuardRows.map((row) => (
              <div className="rounded-md border border-border bg-bg px-3 py-2" key={row.label}>
                <div className="flex items-center justify-between gap-2">
                  <p className="label">{row.label}</p>
                  <span className={`pill ${row.state}`}>{row.value}</span>
                </div>
                <p className="mt-2 text-xs text-muted">{row.detail}</p>
              </div>
            ))}
          </div>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Supadupa control-plane backup</p>
              <p className="truncate text-xs text-muted">{backupStorageTargets.some((target) => target.default) ? "Uses the default S3-compatible target after writing the local encrypted checkpoint artifact." : "Writes a local encrypted checkpoint artifact; add a default target to copy it off-host."}</p>
            </div>
            <button className="button secondary justify-center" disabled={platformBackupMutation.isPending} onClick={() => platformBackupMutation.mutate()} type="button">
              <Save size={14} />
              {platformBackupMutation.isPending ? "Backing up..." : "Back up now"}
            </button>
          </div>
          <div className="grid gap-2">
            <DataTable columns={platformBackupColumns} data={platformBackups.slice(0, 4)} emptyText="No control-plane backups have been recorded." minWidth={980} />
            {platformBackupMutation.error ? <p className="text-sm text-danger">{platformBackupMutation.error.message}</p> : null}
            {platformRestoreMutation.error ? <p className="text-sm text-danger">{platformRestoreMutation.error.message}</p> : null}
            {platformRestoreMutation.data ? (
              <div className="grid gap-1">
                <p className="truncate font-mono text-xs text-muted">
                  Restore {platformRestoreMutation.data.restore_state}: {platformRestoreMutation.data.restore_path}
                </p>
                {platformRestoreMutation.data.runtime_errors?.length ? (
                  <p className="text-xs text-danger">{platformRestoreMutation.data.runtime_errors.join("; ")}</p>
                ) : null}
              </div>
            ) : null}
          </div>
          {showingBackupTargetForm ? (
            editingBackupTarget || backupTargetItem === "new" ? (
              <form className="grid gap-3" onSubmit={submitBackupTarget}>
                <div className="usage-row">
                  <div className="min-w-0">
                    <button className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase text-muted hover:text-text" onClick={closeBackupTargetForm} type="button">
                      <ArrowLeft size={14} />
                      Backup targets
                    </button>
                    <p className="truncate text-sm font-medium">{editingBackupTarget ? `Editing ${editingBackupTarget.name}` : "New S3-compatible target"}</p>
                    <p className="truncate text-xs text-muted">{editingBackupTarget?.secret_configured ? "Secret is configured; leave it blank to keep the current value." : "Secret key is accepted on save and never rendered back."}</p>
                  </div>
                  <span className="pill">{backupStorageTargets.length} targets</span>
                </div>
                <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
                  <BackupTargetInput label="Name" value={backupTargetForm.name} onChange={(value) => setBackupTargetForm({ ...backupTargetForm, name: value })} />
                  <BackupTargetInput label="Region" value={backupTargetForm.region} onChange={(value) => setBackupTargetForm({ ...backupTargetForm, region: value })} mono />
                  <BackupTargetInput label="Endpoint" value={backupTargetForm.endpoint} onChange={(value) => setBackupTargetForm({ ...backupTargetForm, endpoint: value })} mono placeholder="https://s3.example.com" />
                  <BackupTargetInput label="Bucket" value={backupTargetForm.bucket} onChange={(value) => setBackupTargetForm({ ...backupTargetForm, bucket: value })} mono />
                  <BackupTargetInput label="Prefix" value={backupTargetForm.prefix} onChange={(value) => setBackupTargetForm({ ...backupTargetForm, prefix: value })} mono />
                  <BackupTargetInput label="Access key ID" value={backupTargetForm.access_key_id} onChange={(value) => setBackupTargetForm({ ...backupTargetForm, access_key_id: value })} mono />
                  <label className="grid gap-1">
                    <span className="label">Secret access key</span>
                    <input className="input font-mono" placeholder={editingBackupTarget ? "unchanged" : ""} type="password" value={backupTargetForm.secret_access_key ?? ""} onChange={(event) => setBackupTargetForm({ ...backupTargetForm, secret_access_key: event.target.value })} />
                  </label>
                  <div className="grid gap-2 rounded-md border border-border px-3 py-2">
                    <label className="flex items-center justify-between gap-3 text-sm text-muted">
                      <span>Default target</span>
                      <input checked={backupTargetForm.default} onChange={(event) => setBackupTargetForm({ ...backupTargetForm, default: event.target.checked })} type="checkbox" />
                    </label>
                    <label className="flex items-center justify-between gap-3 text-sm text-muted">
                      <span>Path-style URLs</span>
                      <input checked={backupTargetForm.force_path_style} onChange={(event) => setBackupTargetForm({ ...backupTargetForm, force_path_style: event.target.checked })} type="checkbox" />
                    </label>
                  </div>
                </div>
                <div className="usage-row">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">Backup destination</p>
                    <p className="truncate font-mono text-xs text-muted">{backupTargetForm.bucket ? `s3://${backupTargetForm.bucket}/${backupTargetForm.prefix ? `${backupTargetForm.prefix.replace(/^\/+|\/+$/g, "")}/` : ""}projects/{ref}/backups/...` : "Bucket pending"}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    <button className="button secondary justify-center" onClick={closeBackupTargetForm} type="button">Cancel</button>
                    <button className="button secondary justify-center" disabled={!canSaveBackupTarget || createTargetMutation.isPending || updateTargetMutation.isPending} type="submit">
                      <Save size={14} />
                      {editingBackupTarget ? "Update" : "Add"}
                    </button>
                  </div>
                </div>
                {createTargetMutation.error ? <p className="text-sm text-danger">{createTargetMutation.error.message}</p> : null}
                {updateTargetMutation.error ? <p className="text-sm text-danger">{updateTargetMutation.error.message}</p> : null}
              </form>
            ) : (
              <div className="usage-row">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">Backup target not found</p>
                  <p className="truncate text-xs text-muted">The target may have been deleted or has not loaded yet.</p>
                </div>
                <button className="button secondary justify-center" onClick={closeBackupTargetForm} type="button">
                  <ArrowLeft size={14} />
                  Back
                </button>
              </div>
            )
          ) : (
            <div className="grid gap-2">
              <div className="usage-row">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">Backup targets</p>
                  <p className="truncate text-xs text-muted">S3-compatible destinations for project backups, control-plane backups, and upgrade restore points.</p>
                </div>
                <button className="icon-button" onClick={newBackupTarget} title="Add backup target" type="button">
                  <Plus size={14} />
                </button>
              </div>
              <DataTable columns={backupTargetColumns} data={backupStorageTargets} emptyText="No S3-compatible targets configured." minWidth={940} />
              {testTargetMutation.error ? <p className="text-sm text-danger">{testTargetMutation.error.message}</p> : null}
              {deleteTargetMutation.error ? <p className="text-sm text-danger">{deleteTargetMutation.error.message}</p> : null}
            </div>
          )}
        </div>
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
        <div className="mt-4 grid gap-3">
          {!showingSMTPForm ? (
            <div className="usage-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Platform SMTP</p>
                <p className="truncate text-xs text-muted">{form.smtp.enabled ? `${form.smtp.sender_email || "sender pending"} via ${form.smtp.host || "host pending"}:${form.smtp.port} · ${form.smtp.tls_mode}` : "No platform SMTP connector is configured."}</p>
              </div>
              <div className="flex items-center gap-2">
                <span className={`pill ${form.smtp.enabled ? "healthy" : "paused"}`}>{form.smtp.enabled ? "enabled" : "not configured"}</span>
                <button className={form.smtp.enabled ? "button secondary justify-center" : "button justify-center"} onClick={openSMTPForm} type="button">
                  <Plus size={14} />
                  {form.smtp.enabled ? "Edit SMTP" : "Add SMTP"}
                </button>
              </div>
            </div>
          ) : (
            <form className="grid gap-3" onSubmit={submit}>
              <div className="usage-row">
                <div className="min-w-0">
                  <button className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase text-muted hover:text-text" onClick={closeSMTPForm} type="button">
                    <ArrowLeft size={14} />
                    Platform SMTP
                  </button>
                  <p className="truncate text-sm font-medium">{form.smtp.enabled ? "Edit SMTP connector" : "Add SMTP connector"}</p>
                  <p className="truncate text-xs text-muted">Control-plane mail uses a secret handle; raw passwords stay out of the meta DB payload.</p>
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
              <div className="usage-row">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">Secret policy</p>
                  <p className="truncate text-xs text-muted">Password handles must point at secret:// references.</p>
                </div>
                <div className="flex items-center gap-2">
                  <button className="button secondary justify-center" onClick={closeSMTPForm} type="button">Cancel</button>
                  <button className="button secondary justify-center" disabled={!canSave || mutation.isPending} type="submit">
                    <Save size={14} />
                    Save SMTP
                  </button>
                </div>
              </div>
              {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
            </form>
          )}
        </div>
      ) : null}

      {section === "sso" ? (
        <div className="mt-4 grid gap-3">
          {!showingSSOForm ? (
            <div className="usage-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Platform SAML SSO</p>
                <p className="truncate text-xs text-muted">{ssoForm.enabled ? `${ssoForm.idp_entity_id || "IdP pending"} · ${ssoForm.email_domain || "any domain"} · ${ssoForm.auto_provision ? "auto-provision" : "manual users"}` : "Password login only."}</p>
              </div>
              <div className="flex items-center gap-2">
                <span className={`pill ${ssoForm.enabled ? "healthy" : "paused"}`}>{ssoForm.enabled ? "enabled" : "not configured"}</span>
                <button className={ssoForm.enabled ? "button secondary justify-center" : "button justify-center"} onClick={openSSOForm} type="button">
                  <Plus size={14} />
                  {ssoForm.enabled ? "Edit SSO" : "Add SSO"}
                </button>
              </div>
            </div>
          ) : (
            <form className="grid gap-3" onSubmit={submitSSO}>
              <div className="usage-row">
                <div className="min-w-0">
                  <button className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase text-muted hover:text-text" onClick={closeSSOForm} type="button">
                    <ArrowLeft size={14} />
                    Platform SSO
                  </button>
                  <p className="truncate text-sm font-medium">{ssoForm.enabled ? "Edit SAML SSO" : "Add SAML SSO"}</p>
                  <p className="truncate text-xs text-muted">{ssoForm.acs_url || "Set the ACS URL exposed by this control plane."}</p>
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
                <label className="flex items-center gap-2 rounded-md border border-border px-3 py-2 text-sm text-muted">
                  <input checked={ssoForm.scim_enabled} onChange={(event) => setSSOForm({ ...ssoForm, scim_enabled: event.target.checked })} type="checkbox" />
                  Enable SCIM
                </label>
                <SSOInput label="IdP entity ID" value={ssoForm.idp_entity_id} onChange={(value) => setSSOForm({ ...ssoForm, idp_entity_id: value })} mono />
                <SSOInput label="SSO login URL" value={ssoForm.sso_url} onChange={(value) => setSSOForm({ ...ssoForm, sso_url: value })} mono />
                <SSOInput label="ACS URL" value={ssoForm.acs_url} onChange={(value) => setSSOForm({ ...ssoForm, acs_url: value })} mono />
                <SSOInput label="Metadata URL" value={ssoForm.metadata_url} onChange={(value) => setSSOForm({ ...ssoForm, metadata_url: value })} mono />
                <SSOInput label="Email domain" value={ssoForm.email_domain} onChange={(value) => setSSOForm({ ...ssoForm, email_domain: value })} />
                <SSOInput label={ssoForm.scim_token_configured ? "Rotate SCIM token" : "SCIM token"} value={ssoForm.scim_token} onChange={(value) => setSSOForm({ ...ssoForm, scim_token: value })} mono />
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
              <div className="usage-row">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">SAML callback</p>
                  <p className="truncate text-xs text-muted">{ssoForm.acs_url || "Set the ACS URL exposed by this control plane."}</p>
                </div>
                <div className="flex items-center gap-2">
                  <button className="button secondary justify-center" onClick={closeSSOForm} type="button">Cancel</button>
                  <button className="button secondary justify-center" disabled={!canSaveSSO || ssoMutation.isPending} type="submit">
                    <Save size={14} />
                    Save SSO
                  </button>
                </div>
              </div>
              {ssoMutation.error ? <p className="text-sm text-danger">{ssoMutation.error.message}</p> : null}
            </form>
          )}
        </div>
      ) : null}

      {section === "scim" ? (
        <div className="mt-4 grid gap-3">
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Platform SCIM provisioning</p>
              <p className="truncate text-xs text-muted">
                {scimServiceProviderConfig ? `${scimAuthScheme?.name ?? "Bearer token"} · ${ssoForm.scim_token_configured ? "token configured" : "token missing"} · patch ${scimServiceProviderConfig.patch.supported ? "supported" : "off"}` : "Loading service provider config"}
              </p>
            </div>
            <span className={`pill ${ssoForm.scim_enabled && ssoForm.scim_token_configured ? "healthy" : "paused"}`}>{ssoForm.scim_enabled ? "enabled" : "disabled"}</span>
          </div>
          <div className="grid grid-cols-3 gap-2 max-lg:grid-cols-1">
            <Metric label="Users" value={(scimUsers?.totalResults ?? 0).toString()} />
            <Metric label="Groups" value={(scimGroups?.totalResults ?? 0).toString()} />
            <Metric label="Auth" value={ssoForm.scim_token_configured ? scimAuthScheme?.type ?? "oauthbearertoken" : "token missing"} />
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

function BackupTargetInput({ label, mono = false, onChange, placeholder, value }: { label: string; value: string; onChange: (value: string) => void; mono?: boolean; placeholder?: string }) {
  return (
    <label className="grid gap-1">
      <span className="label">{label}</span>
      <input className={`input ${mono ? "font-mono" : ""}`} placeholder={placeholder} value={value} onChange={(event) => onChange(event.target.value)} />
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
  const columns = useMemo<ColumnDef<SCIMUser>[]>(
    () => [
      {
        header: "User",
        accessorKey: "userName",
        size: 220,
        cell: ({ row }) => (
          <>
            <p className="cell-main truncate">{row.original.displayName || row.original.userName}</p>
            <p className="cell-sub truncate">{row.original.userName}</p>
          </>
        ),
      },
      {
        header: "Email",
        id: "email",
        size: 220,
        cell: ({ row }) => row.original.emails?.find((email) => email.primary)?.value ?? row.original.emails?.[0]?.value ?? "not provided",
      },
      {
        header: "Role",
        id: "role",
        size: 140,
        cell: ({ row }) => row.original["urn:supadupa:params:scim:schemas:extension:User"]?.role ?? "role pending",
      },
      {
        header: "Status",
        accessorKey: "active",
        size: 120,
        cell: ({ row }) => <span className={`pill ${row.original.active ? "healthy" : "paused"}`}>{row.original.active ? "active" : "inactive"}</span>,
      },
      {
        header: "Created",
        id: "created",
        size: 150,
        cell: ({ row }) => row.original.meta.created ? formatDateTime(row.original.meta.created) : "not recorded",
      },
    ],
    [],
  );

  return (
    <div className="grid gap-2">
      <p className="label">Provisioned users</p>
      <DataTable columns={columns} data={users.slice(0, 5)} emptyText="No SCIM users returned." minWidth={780} />
    </div>
  );
}

function SCIMGroups({ groups }: { groups: SCIMGroup[] }) {
  const columns = useMemo<ColumnDef<SCIMGroup>[]>(
    () => [
      {
        header: "Group",
        accessorKey: "displayName",
        size: 220,
        cell: ({ row }) => (
          <>
            <p className="cell-main truncate">{row.original.displayName}</p>
            <p className="cell-sub font-mono">{row.original["urn:supadupa:params:scim:schemas:extension:Group"]?.slug ?? row.original.id}</p>
          </>
        ),
      },
      {
        header: "Members",
        id: "members",
        size: 120,
        cell: ({ row }) => `${row.original.members?.length ?? 0}`,
      },
      {
        header: "Org",
        id: "org",
        size: 220,
        cell: ({ row }) => <p className="truncate font-mono text-xs text-muted">{row.original["urn:supadupa:params:scim:schemas:extension:Group"]?.org_id ?? "not linked"}</p>,
      },
      {
        header: "Type",
        id: "type",
        size: 140,
        cell: ({ row }) => <span className="pill">{row.original.meta.resourceType}</span>,
      },
      {
        header: "Created",
        id: "created",
        size: 150,
        cell: ({ row }) => row.original.meta.created ? formatDateTime(row.original.meta.created) : "not recorded",
      },
    ],
    [],
  );

  return (
    <div className="grid gap-2">
      <p className="label">Provisioned groups</p>
      <DataTable columns={columns} data={groups.slice(0, 5)} emptyText="No SCIM groups returned." minWidth={780} />
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
