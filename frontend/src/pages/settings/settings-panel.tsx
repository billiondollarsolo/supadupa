import type { FormEvent } from "react";
import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { ArrowLeft, History, Plus, Save, ShieldCheck, SlidersHorizontal, TestTube2, Trash2 } from "lucide-react";
import { createBackupStorageTarget, deleteBackupStorageTarget, restorePlatformBackup, testBackupStorageTarget, triggerPlatformBackup, updateBackupStorageTarget, updatePlatformDefaults, updatePlatformSSOConfig, type BackupStorageTargetInput } from "../../api";
import { DataTable } from "../../components/data-table";
import { Modal } from "../../components/modal";
import { AppPanel } from "../../components/app/app-panel";
import { MetricCard } from "../../components/app/metric-card";
import { CardButton } from "../../components/ui/card-button";
import { StatusPill } from "../../components/ui/status-pill";
import { EmptyState } from "../../components/ui/empty-state";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { Textarea } from "../../components/ui/textarea";
import { type Tone } from "../../lib/status";
import { featureFlagGroups } from "../../lib/feature-flags";
import { formatBytes, formatDateTime, shortChecksum } from "../../lib/format";
import { parseLines } from "../../lib/parse";
import { platformSettingsSections, type PlatformSettingsSection } from "../../lib/project-config";
import type { BackupStorageTarget, FleetMetrics, PlatformBackup, PlatformDefaults, PlatformSSOConfig, ProvisionerStatus, RuntimeConfig, SCIMGroup, SCIMListResponse, SCIMServiceProviderConfig, SCIMUser, StackReleaseManifest } from "../../types";

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

function backupTargetReadinessTone(target: BackupStorageTarget): Tone {
  if (target.recovery_ready) return "success";
  if (target.readiness_status === "validation-failed" || target.readiness_status === "local-or-loopback" || target.readiness_status === "missing-secret") return "danger";
  return "warning";
}

// One-line explanation per feature flag. Toggling these gates whole UI surfaces
// and API availability, so each gets a plain-language summary of what it turns on.
const featureFlagDescriptions: Record<string, string> = {
  multi_org: "Show the Organizations area (orgs, teams, members, quotas, billing). Off for single-tenant / MVP installs.",
  team_rbac: "Enable team-scoped roles and permissions within orgs.",
  project_access_grants: "Grant individual users scoped access to specific projects.",
  project_self_service: "Let non-admins create and manage their own projects.",
  supabase_cli_compat: "Expose Supabase-CLI-compatible endpoints for local tooling.",
  service_toggles: "Allow enabling/disabling individual project services (Auth, Storage, etc.).",
  custom_domains: "Let projects attach and verify custom domains.",
  network_restrictions: "Expose per-project IP allowlists for database and API access.",
  log_drains: "Forward project logs to external sinks (e.g. Datadog, S3).",
  pitr: "Enable point-in-time recovery and WAL-based restore for projects.",
  production_posture: "Hold platform-wide recovery posture (backup-target guards, recovery-ready targets) to full advisor severity. Off keeps local/MVP installs from showing high-severity recovery findings.",
  preview_branches: "Spin up ephemeral database branches for previews.",
  read_replicas: "Provision and route to read-only Postgres replicas.",
  edge_functions: "Deploy and manage edge functions per project.",
  ai_integrations: "Surface AI/vector tooling and embedding jobs.",
  usage_metering: "Track and display per-project resource usage.",
  billing: "Enable invoicing and billing surfaces.",
  platform_sso_scim: "Enable platform-wide SAML SSO and SCIM provisioning for admins.",
  kubernetes_operator: "Use the Kubernetes operator provisioner instead of Compose.",
};

export function SettingsPanel({
  defaults,
  sso,
  backupStorageTargets,
  fleetMetrics,
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
  fleetMetrics?: FleetMetrics;
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
    resource_tier: "custom",
    backup_schedule: "daily",
    feature_flags: {} as Record<string, boolean>,
    database_ingress_allowed_cidrs: "",
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
  const [restoreTarget, setRestoreTarget] = useState<PlatformBackup | null>(null);
  const [restoreConfirmText, setRestoreConfirmText] = useState("");
  // Target value (true=enabling, false=disabling) for the external-access master
  // switch confirmation; null when the dialog is closed.
  const [externalAccessConfirm, setExternalAccessConfirm] = useState<boolean | null>(null);
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
      database_ingress_allowed_cidrs: (defaults.database_ingress_allowed_cidrs ?? []).join("\n"),
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
      void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
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
  const databaseIngress = fleetMetrics?.database_ingress;
  const databaseIngressCIDRs = parseLines(form.database_ingress_allowed_cidrs);
  const databaseIngressPublic = databaseIngress?.public ?? false;
  const databaseIngressAllowlisted = databaseIngressCIDRs.length > 0;
  // Host-level bind status. Exposure of an individual database is controlled
  // per project (Config -> Network), so a published host port is informational,
  // not a fleet-wide alarm.
  const databaseIngressTitle = databaseIngressPublic ? "Host ports published" : "Loopback only";
  const databaseIngressTone: Tone = !databaseIngress ? "neutral" : databaseIngressPublic ? "info" : "neutral";
  const databaseIngressModeDetail = databaseIngressPublic
    ? "Database/pooler ports are reachable at the host. Each project is private until opened under its Config → Network."
    : "Database/pooler ports are loopback-bound, so no project is reachable externally regardless of its per-project setting.";
  const databaseIngressChangeMode = provisioner === "compose" ? "Deploy-time in Compose" : provisioner === "kubernetes" ? "Cluster networking" : "Provisioner pending";
  const databaseIngressChangeDetail = provisioner === "compose"
    ? "Per-project exposure (private / allowlisted / public) is set on each project's Config → Network. This host-level listener bind comes from SUPADUPA_POSTGRES_ADDR / SUPADUPA_POOLER_ADDR and is set at deploy time."
    : provisioner === "kubernetes"
      ? "Per-project exposure is set on each project's Config → Network. Host-level direct access is exposed through a Kubernetes service or ingress policy at the cluster level."
      : "Provisioner status has not loaded yet.";
  const runtimeBackup = runtimeConfig?.backup;
  const recoveryGuardOn = runtimeConfig?.recovery.require_recovery_ready_targets ?? false;
  const durableUpgradeGuardOn = runtimeConfig?.upgrade.require_durable_backup ?? false;
  const backupCommandsReady = Boolean(runtimeBackup?.logical_configured && runtimeBackup.physical_configured && runtimeBackup.wal_archive_configured && runtimeBackup.logical_restore_configured && runtimeBackup.pitr_restore_configured);
  const backupDryRunOff = Boolean(runtimeBackup && !runtimeBackup.backup_dry_run && !runtimeBackup.restore_dry_run && !runtimeBackup.wal_archive_dry_run);
  const recoveryReadyTargets = backupStorageTargets.filter((target) => target.recovery_ready);
  const defaultRecoveryTarget = backupStorageTargets.find((target) => target.default && target.recovery_ready);
  const hostedRecoveryMode = recoveryGuardOn && durableUpgradeGuardOn && backupCommandsReady && backupDryRunOff && recoveryReadyTargets.length > 0;
  const runtimeGuardRows: Array<{ label: string; value: string; tone: Tone; detail: string }> = [
    {
      label: "Recovery guard",
      value: recoveryGuardOn ? "enforced" : "off",
      tone: recoveryGuardOn ? "success" : "warning",
      detail: recoveryGuardOn ? "Physical backups and WAL archives require tested off-host targets." : "Set SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true and restart the API.",
    },
    {
      label: "Upgrade guard",
      value: durableUpgradeGuardOn ? "enforced" : "off",
      tone: durableUpgradeGuardOn ? "success" : "warning",
      detail: durableUpgradeGuardOn ? "Stack upgrades require durable pre-upgrade backup artifacts." : "Set SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true for production upgrades.",
    },
    {
      label: "Backup runtime",
      value: backupCommandsReady && backupDryRunOff ? "ready" : runtimeConfig ? "incomplete" : "loading",
      tone: backupCommandsReady && backupDryRunOff ? "success" : runtimeConfig ? "warning" : "info",
      detail: runtimeConfig ? `${runtimeConfig.provisioner} · ${runtimeBackup?.compose_defaults ? "Compose defaults" : "custom commands"}${backupDryRunOff ? "" : " · dry-run enabled"}` : "Runtime configuration has not loaded yet.",
    },
    {
      label: "Off-host target",
      value: defaultRecoveryTarget?.name ?? (recoveryReadyTargets.length > 0 ? `${recoveryReadyTargets.length} ready` : "not ready"),
      tone: recoveryReadyTargets.length > 0 ? "success" : "warning",
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
        cell: ({ row }) => <StatusPill status={row.original.status} />,
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
        size: 132,
        cell: ({ row }) => (
          <div className="flex justify-end">
            <Button variant="secondary" disabled={platformRestoreMutation.isPending || row.original.status !== "completed"} onClick={() => openRestoreConfirm(row.original)} title="Restore control plane from this backup" type="button">
              <History size={14} />
              Restore
            </Button>
          </div>
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
            <StatusPill tone={backupTargetReadinessTone(row.original)} label={backupTargetReadinessLabel(row.original)} />
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
            <Button variant="ghost" size="icon" disabled={testTargetMutation.isPending} onClick={() => testTargetMutation.mutate(row.original.id)} title="Test target" type="button">
              <TestTube2 size={14} />
            </Button>
            <Button variant="ghost" size="icon" disabled={deleteTargetMutation.isPending} onClick={() => deleteTargetMutation.mutate(row.original.id)} title="Delete target" type="button">
              <Trash2 size={14} />
            </Button>
          </div>
        ),
      },
    ],
    [deleteTargetMutation.isPending, testTargetMutation.isPending],
  );

  // The defaults endpoint is a full PUT, so a section save must not silently
  // clobber fields another section owns. Each section builds its payload from
  // the persisted server `defaults` (not the in-memory form, which may carry
  // unsaved edits from elsewhere) and overrides only the fields it edits.
  function persistedDefaultsPayload(): Omit<PlatformDefaults, "updated_at"> {
    return {
      domain: defaults?.domain ?? form.domain,
      stack_version: defaults?.stack_version ?? form.stack_version,
      profile: defaults?.profile ?? form.profile,
      resource_tier: defaults?.resource_tier ?? form.resource_tier,
      backup_schedule: defaults?.backup_schedule ?? form.backup_schedule,
      feature_flags: defaults?.feature_flags ?? {},
      database_ingress_allowed_cidrs: defaults?.database_ingress_allowed_cidrs ?? [],
      smtp: {
        enabled: defaults?.smtp?.enabled ?? false,
        host: defaults?.smtp?.host ?? "",
        port: defaults?.smtp?.port ?? 587,
        sender_name: defaults?.smtp?.sender_name ?? "",
        sender_email: defaults?.smtp?.sender_email ?? "",
        username: defaults?.smtp?.username ?? "",
        password_handle: defaults?.smtp?.password_handle ?? "",
        tls_mode: defaults?.smtp?.tls_mode ?? "starttls",
      },
    };
  }

  function submitDefaults(event: FormEvent) {
    event.preventDefault();
	    mutation.mutate({
	      ...persistedDefaultsPayload(),
	      domain: form.domain,
	      stack_version: form.stack_version,
	      profile: form.profile,
	      backup_schedule: form.backup_schedule,
	    });
  }

  function submitFeatureFlags(event: FormEvent) {
    event.preventDefault();
    mutation.mutate({ ...persistedDefaultsPayload(), feature_flags: form.feature_flags });
  }

  function submitDatabaseIngress(event: FormEvent) {
    event.preventDefault();
    mutation.mutate({ ...persistedDefaultsPayload(), database_ingress_allowed_cidrs: parseLines(form.database_ingress_allowed_cidrs) });
  }

  function submitSMTP(event: FormEvent) {
    event.preventDefault();
    mutation.mutate({
      ...persistedDefaultsPayload(),
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
  const externalAccessEnabled = Boolean(form.feature_flags.database_external_access);
  function applyExternalAccess(next: boolean) {
    const feature_flags = { ...form.feature_flags, database_external_access: next };
    setForm({ ...form, feature_flags });
    mutation.mutate({ ...persistedDefaultsPayload(), feature_flags });
    setExternalAccessConfirm(null);
  }

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

  function openRestoreConfirm(backup: PlatformBackup) {
    setRestoreConfirmText("");
    setRestoreTarget(backup);
  }

  function closeRestoreConfirm() {
    if (platformRestoreMutation.isPending) return;
    setRestoreTarget(null);
    setRestoreConfirmText("");
  }

  function confirmPlatformRestore() {
    if (!restoreTarget || restoreConfirmText.trim() !== "restore-control-plane") return;
    platformRestoreMutation.mutate(restoreTarget.id, {
      onSuccess: () => {
        setRestoreTarget(null);
        setRestoreConfirmText("");
      },
    });
  }

  function openSection(target: PlatformSettingsSection) {
    if (target === "overview") {
      void navigate({ to: "/settings" });
      return;
    }
    void navigate({ to: "/settings/$section", params: { section: target } });
  }

  return (
    <AppPanel eyebrow="Settings" title={activeSection.label} actions={<SlidersHorizontal size={15} className="text-faint" />}>
      <p className="mt-1 text-sm text-muted">{activeSection.description}</p>

      {section === "overview" ? (
        <div className="mt-4 grid grid-cols-3 gap-3 max-xl:grid-cols-2 max-md:grid-cols-1">
            <SummaryCard
              label="Database ingress"
              title={databaseIngressTitle}
              detail={databaseIngressModeDetail}
              tone={databaseIngressTone}
              statusLabel={databaseIngressPublic ? "published" : "loopback"}
              onClick={() => openSection("db-ingress")}
            />
            <SummaryCard
              label="Backups"
              title={`${backupStorageTargets.length} ${backupStorageTargets.length === 1 ? "target" : "targets"}`}
              tone={recoveryReadyTargets.length > 0 ? "success" : backupStorageTargets.length > 0 ? "warning" : "neutral"}
              statusLabel={recoveryReadyTargets.length > 0 ? "recovery ready" : backupStorageTargets.length > 0 ? "not ready" : "not configured"}
              metrics={[
                { label: "Targets", value: String(backupStorageTargets.length) },
                { label: "Default", value: String(backupStorageTargets.filter((target) => target.default).length) },
                { label: "Backups", value: String(platformBackups.length) },
              ]}
              onClick={() => openSection("backups")}
            />
            <SummaryCard
              label="Platform SMTP"
              title={defaults?.smtp?.enabled ? "Enabled" : "Disabled"}
              detail={defaults?.smtp?.enabled ? `${defaults.smtp.host || "host pending"}:${defaults.smtp.port} · ${defaults.smtp.tls_mode}` : "Control-plane mail is not configured."}
              tone={defaults?.smtp?.enabled ? "success" : "neutral"}
              statusLabel={defaults?.smtp?.enabled ? "enabled" : "not configured"}
              onClick={() => openSection("smtp")}
            />
            {Boolean(defaults?.feature_flags?.platform_sso_scim) ? (
              <>
                <SummaryCard
                  label="Platform SSO"
                  title={sso?.enabled ? "Enabled" : "Disabled"}
                  detail={sso?.enabled ? `${sso.idp_entity_id || "IdP pending"} · ${sso.email_domain || "any domain"}` : "Password login only."}
                  tone={sso?.enabled ? "success" : "neutral"}
                  statusLabel={sso?.enabled ? "enabled" : "not configured"}
                  onClick={() => openSection("sso")}
                />
                <SummaryCard
                  label="SCIM"
                  title={sso?.scim_enabled ? "Enabled" : "Disabled"}
                  tone={sso?.scim_enabled ? (sso.scim_token_configured ? "success" : "warning") : "neutral"}
                  statusLabel={sso?.scim_enabled ? (sso.scim_token_configured ? "enabled" : "token missing") : "not configured"}
                  metrics={[
                    { label: "Users", value: String(scimUsers?.totalResults ?? 0) },
                    { label: "Groups", value: String(scimGroups?.totalResults ?? 0) },
                    { label: "Token", value: sso?.scim_token_configured ? "set" : "missing" },
                  ]}
                  onClick={() => openSection("scim")}
                />
              </>
            ) : null}
	            <SummaryCard label="Defaults" title={form.profile} detail={`${form.domain} · ${form.stack_version} · ${form.backup_schedule} backups`} onClick={() => openSection("defaults")} />
            <SummaryCard label="Feature flags" title={`${enabledFeatures} of ${Object.keys(form.feature_flags).length || enabledFeatures} enabled`} detail={provisionerMode} onClick={() => openSection("features")} />
        </div>
      ) : null}

      {section === "defaults" ? (
        <form className="mt-4 grid gap-3" onSubmit={submitDefaults}>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Provisioner substrate</p>
              <p className="truncate text-xs text-muted">{provisionerMode} · selected by SUPADUPA_PROVISIONER</p>
            </div>
            <StatusPill tone={provisioner === "unconfigured" ? "warning" : provisioner === "loading" ? "info" : "success"} label={provisioner === "loading" ? "loading" : provisioner} />
          </div>
          <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
            <label className="grid gap-1">
              <span className="label">Base domain</span>
              <Input value={form.domain} onChange={(event) => setForm({ ...form, domain: event.target.value })} />
            </label>
            <label className="grid gap-1">
              <span className="label">Stack version</span>
              <NativeSelect className="font-mono" value={form.stack_version} onChange={(event) => setForm({ ...form, stack_version: event.target.value })}>
                <option value="latest">latest{latestRelease ? ` (${latestRelease.version})` : ""}</option>
                {stackReleases.map((release) => (
                  <option key={release.version} value={release.version}>{release.version}</option>
                ))}
              </NativeSelect>
            </label>
            <label className="grid gap-1">
              <span className="label">Profile</span>
              <NativeSelect value={form.profile} onChange={(event) => setForm({ ...form, profile: event.target.value })}>
                <option value="full">Full</option>
                <option value="essential">Essential</option>
                <option value="orioledb">OrioleDB</option>
              </NativeSelect>
            </label>
	            <label className="grid gap-1">
	              <span className="label">Backup schedule</span>
              <NativeSelect value={form.backup_schedule} onChange={(event) => setForm({ ...form, backup_schedule: event.target.value })}>
                <option value="daily">Daily</option>
                <option value="hourly">Hourly</option>
              </NativeSelect>
            </label>
          </div>
	          <SaveRow disabled={!canSave || mutation.isPending} detail={`${form.stack_version} · ${form.profile} · ${form.backup_schedule}`} title="New project defaults" />
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </form>
      ) : null}

      {section === "db-ingress" ? (
        <div className="mt-4 grid gap-3">
          <div className="rounded-md border border-border bg-bg p-3">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <p className="text-sm font-medium">External database access</p>
                <p className="mt-0.5 text-xs text-muted">Master switch. When off, every project's database is private fleet-wide regardless of its own setting. When on, each project's own exposure (set per project) applies.</p>
              </div>
              <StatusPill tone={externalAccessEnabled ? "warning" : "neutral"} label={externalAccessEnabled ? "enabled" : "disabled"} />
            </div>
            <div className="mt-3 flex items-center justify-between gap-3">
              <p className="text-xs text-faint">Per-project exposure and IP allowlists are configured on each project's Config → Network.</p>
              <Button
                variant={externalAccessEnabled ? "secondary" : "default"}
                disabled={mutation.isPending}
                onClick={() => setExternalAccessConfirm(!externalAccessEnabled)}
                type="button"
              >
                {externalAccessEnabled ? "Disable external access" : "Enable external access"}
              </Button>
            </div>
          </div>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Listener exposure</p>
              <p className="truncate text-xs text-muted">Postgres {databaseIngress?.postgres_addr ?? (loading ? "loading…" : "unknown")} · Pooler {databaseIngress?.pooler_addr ?? (loading ? "loading…" : "unknown")}</p>
            </div>
            <StatusPill tone={databaseIngressTone} label={databaseIngressTitle} />
          </div>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Exposure mode change</p>
              <p className="truncate text-xs text-muted">{databaseIngressChangeDetail}</p>
            </div>
            <StatusPill tone={provisioner === "compose" ? "warning" : provisioner === "loading" ? "info" : "neutral"} label={databaseIngressChangeMode} />
          </div>
          {databaseIngress?.warnings?.length ? (
            <div className="rounded-md border border-border bg-bg px-3 py-2 text-xs text-muted">
              {databaseIngress.warnings.join(" · ")}
            </div>
          ) : null}
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
        </div>
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
            <StatusPill tone={hostedRecoveryMode ? "success" : "warning"} label={hostedRecoveryMode ? "production ready" : "not production"} />
          </div>
          <div className="grid grid-cols-4 gap-2 max-xl:grid-cols-2 max-sm:grid-cols-1">
            {runtimeGuardRows.map((row) => (
              <div className="rounded-md border border-border bg-bg px-3 py-2" key={row.label}>
                <div className="flex items-center justify-between gap-2">
                  <p className="label">{row.label}</p>
                  <StatusPill tone={row.tone} label={row.value} />
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
            <Button variant="secondary" disabled={platformBackupMutation.isPending} onClick={() => platformBackupMutation.mutate()} type="button">
              <Save size={14} />
              {platformBackupMutation.isPending ? "Backing up..." : "Back up now"}
            </Button>
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
                  <Badge variant="muted">{backupStorageTargets.length} targets</Badge>
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
                    <Input className="font-mono" placeholder={editingBackupTarget ? "unchanged" : ""} type="password" value={backupTargetForm.secret_access_key ?? ""} onChange={(event) => setBackupTargetForm({ ...backupTargetForm, secret_access_key: event.target.value })} />
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
                    <Button variant="secondary" onClick={closeBackupTargetForm} type="button">Cancel</Button>
                    <Button variant="secondary" disabled={!canSaveBackupTarget || createTargetMutation.isPending || updateTargetMutation.isPending} type="submit">
                      <Save size={14} />
                      {editingBackupTarget ? "Update" : "Add"}
                    </Button>
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
                <Button variant="secondary" onClick={closeBackupTargetForm} type="button">
                  <ArrowLeft size={14} />
                  Back
                </Button>
              </div>
            )
          ) : (
            <div className="grid gap-2">
              <div className="usage-row">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">Backup targets</p>
                  <p className="truncate text-xs text-muted">S3-compatible destinations for project backups, control-plane backups, and upgrade restore points.</p>
                </div>
                <Button variant="ghost" size="icon" onClick={newBackupTarget} title="Add backup target" type="button">
                  <Plus size={14} />
                </Button>
              </div>
              <DataTable columns={backupTargetColumns} data={backupStorageTargets} emptyText="No S3-compatible targets configured." minWidth={940} />
              {testTargetMutation.error ? <p className="text-sm text-danger">{testTargetMutation.error.message}</p> : null}
              {deleteTargetMutation.error ? <p className="text-sm text-danger">{deleteTargetMutation.error.message}</p> : null}
            </div>
          )}
        </div>
      ) : null}

      {section === "features" ? (
        <form className="mt-4 grid gap-3" onSubmit={submitFeatureFlags}>
          <div className="usage-row">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">Feature flags</p>
              <p className="truncate text-xs text-muted">Use default org mode for local installs; keep org/team/project RBAC as the access model for enterprise growth.</p>
            </div>
            <StatusPill tone={enabledFeatures > 0 ? "info" : "neutral"} label={`${enabledFeatures} enabled`} />
          </div>
          <div className="grid grid-cols-3 gap-3 max-xl:grid-cols-1">
            {featureFlagGroups.map((group) => (
              <div className="grid gap-3 rounded-md border border-border bg-bg p-3" key={group.label}>
                <p className="label">{group.label}</p>
                {group.flags.map(([key, label]) => (
                  <label className="flex items-start justify-between gap-3 text-sm text-muted" key={key}>
                    <span className="min-w-0">
                      <span className="block font-medium text-text">{label}</span>
                      {featureFlagDescriptions[key] ? <span className="mt-0.5 block text-xs text-faint">{featureFlagDescriptions[key]}</span> : null}
                    </span>
                    <input className="mt-1 shrink-0" checked={Boolean(form.feature_flags[key])} onChange={(event) => setFeatureFlag(key, event.target.checked)} type="checkbox" />
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
                <p className="truncate text-xs text-muted">{defaults?.smtp?.enabled ? `${defaults.smtp.sender_email || "sender pending"} via ${defaults.smtp.host || "host pending"}:${defaults.smtp.port} · ${defaults.smtp.tls_mode}` : "No platform SMTP connector is configured."}</p>
              </div>
              <div className="flex items-center gap-2">
                <StatusPill tone={defaults?.smtp?.enabled ? "success" : "neutral"} label={defaults?.smtp?.enabled ? "enabled" : "not configured"} />
                <Button variant={defaults?.smtp?.enabled ? "secondary" : "default"} onClick={openSMTPForm} type="button">
                  <Plus size={14} />
                  {defaults?.smtp?.enabled ? "Edit SMTP" : "Add SMTP"}
                </Button>
              </div>
            </div>
          ) : (
            <form className="grid gap-3" onSubmit={submitSMTP}>
              <div className="usage-row">
                <div className="min-w-0">
                  <button className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase text-muted hover:text-text" onClick={closeSMTPForm} type="button">
                    <ArrowLeft size={14} />
                    Platform SMTP
                  </button>
                  <p className="truncate text-sm font-medium">{defaults?.smtp?.enabled ? "Edit SMTP connector" : "Add SMTP connector"}</p>
                  <p className="truncate text-xs text-muted">Control-plane mail uses a secret handle; raw passwords stay out of the meta DB payload.</p>
                </div>
                <StatusPill tone={form.smtp.enabled ? "success" : "neutral"} label={form.smtp.enabled ? "enabled (draft)" : "disabled (draft)"} />
              </div>
              <div className="grid grid-cols-2 gap-2 max-sm:grid-cols-1">
                <label className="flex items-center gap-2 rounded-md border border-border px-3 py-2 text-sm text-muted">
                  <input checked={form.smtp.enabled} onChange={(event) => setSMTPForm({ enabled: event.target.checked })} type="checkbox" />
                  Enable platform SMTP
                </label>
                <label className="grid gap-1">
                  <span className="label">TLS mode</span>
                  <NativeSelect value={form.smtp.tls_mode} onChange={(event) => setSMTPForm({ tls_mode: event.target.value })}>
                    <option value="starttls">STARTTLS</option>
                    <option value="implicit">Implicit TLS</option>
                    <option value="none">None</option>
                  </NativeSelect>
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
                  <Button variant="secondary" onClick={closeSMTPForm} type="button">Cancel</Button>
                  <Button variant="secondary" disabled={!canSave || mutation.isPending} type="submit">
                    <Save size={14} />
                    Save SMTP
                  </Button>
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
                <p className="truncate text-xs text-muted">{sso?.enabled ? `${sso.idp_entity_id || "IdP pending"} · ${sso.email_domain || "any domain"} · ${sso.auto_provision ? "auto-provision" : "manual users"}` : "Password login only."}</p>
              </div>
              <div className="flex items-center gap-2">
                <StatusPill tone={sso?.enabled ? "success" : "neutral"} label={sso?.enabled ? "enabled" : "not configured"} />
                <Button variant={sso?.enabled ? "secondary" : "default"} onClick={openSSOForm} type="button">
                  <Plus size={14} />
                  {sso?.enabled ? "Edit SSO" : "Add SSO"}
                </Button>
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
                  <p className="truncate text-sm font-medium">{sso?.enabled ? "Edit SAML SSO" : "Add SAML SSO"}</p>
                  <p className="truncate text-xs text-muted">{ssoForm.acs_url || "Set the ACS URL exposed by this control plane."}</p>
                </div>
                <StatusPill tone={ssoForm.enabled ? "success" : "neutral"} label={ssoForm.enabled ? "enabled (draft)" : "disabled (draft)"} />
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
                  <NativeSelect value={ssoForm.default_role} onChange={(event) => setSSOForm({ ...ssoForm, default_role: event.target.value })}>
                    <option value="developer">Developer</option>
                    <option value="viewer">Viewer</option>
                    <option value="admin">Admin</option>
                  </NativeSelect>
                </label>
              </div>
              <label className="grid gap-1">
                <span className="label">Signing certificate PEM</span>
                <Textarea className="min-h-24 font-mono text-xs leading-5" value={ssoForm.certificate_pem} onChange={(event) => setSSOForm({ ...ssoForm, certificate_pem: event.target.value })} />
              </label>
              <div className="usage-row">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">SAML callback</p>
                  <p className="truncate text-xs text-muted">{ssoForm.acs_url || "Set the ACS URL exposed by this control plane."}</p>
                </div>
                <div className="flex items-center gap-2">
                  <Button variant="secondary" onClick={closeSSOForm} type="button">Cancel</Button>
                  <Button variant="secondary" disabled={!canSaveSSO || ssoMutation.isPending} type="submit">
                    <Save size={14} />
                    Save SSO
                  </Button>
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
                {scimServiceProviderConfig ? `${scimAuthScheme?.name ?? "Bearer token"} · ${sso?.scim_token_configured ? "token configured" : "token missing"} · patch ${scimServiceProviderConfig.patch.supported ? "supported" : "off"}` : "Loading service provider config"}
              </p>
            </div>
            <div className="flex items-center gap-2">
              <StatusPill
                tone={sso?.scim_enabled ? (sso.scim_token_configured ? "success" : "warning") : "neutral"}
                label={sso?.scim_enabled ? (sso.scim_token_configured ? "enabled" : "token missing") : "not configured"}
              />
              <Button variant="secondary" onClick={openSSOForm} type="button">
                <Plus size={14} />
                {sso?.scim_enabled ? "Manage in SSO" : "Configure SCIM"}
              </Button>
            </div>
          </div>
          {!sso?.scim_enabled ? (
            <EmptyState
              icon={ShieldCheck}
              title="SCIM provisioning is not enabled"
              description="SCIM is configured alongside SAML SSO: enable SCIM and set a bearer token there, then your IdP can sync users and groups into Supadupa. This page is read-only and reflects what the IdP has provisioned."
              action={
                <Button onClick={openSSOForm} type="button">
                  <Plus size={14} />
                  Enable SCIM in SSO settings
                </Button>
              }
            />
          ) : (
            <>
              <div className="grid grid-cols-3 gap-2 max-lg:grid-cols-1">
                <MetricCard label="Users" value={(scimUsers?.totalResults ?? 0).toString()} />
                <MetricCard label="Groups" value={(scimGroups?.totalResults ?? 0).toString()} />
                <MetricCard label="Auth" value={sso?.scim_token_configured ? scimAuthScheme?.type ?? "oauthbearertoken" : "token missing"} tone={sso?.scim_token_configured ? "default" : "warning"} />
              </div>
              <div className="grid grid-cols-2 gap-3 max-lg:grid-cols-1">
                <SCIMUsers users={scimUserResources} />
                <SCIMGroups groups={scimGroupResources} />
              </div>
            </>
          )}
        </div>
      ) : null}

      <Modal
        description="This overwrites the live control-plane state (orgs, projects, settings) with the contents of this backup and reconciles the runtime. This cannot be undone."
        footer={
          <>
            <Button variant="secondary" disabled={platformRestoreMutation.isPending} onClick={closeRestoreConfirm} type="button">
              Cancel
            </Button>
            <Button variant="danger" disabled={platformRestoreMutation.isPending || restoreConfirmText.trim() !== "restore-control-plane"} onClick={confirmPlatformRestore} type="button">
              {platformRestoreMutation.isPending ? "Restoring..." : "Restore control plane"}
            </Button>
          </>
        }
        onClose={closeRestoreConfirm}
        open={Boolean(restoreTarget)}
        title="Restore control plane"
      >
        <div className="grid gap-3 text-sm text-muted">
          <div className="rounded-md border border-border bg-bg p-3">
            <p className="label">Backup</p>
            <p className="mt-1 truncate text-sm font-medium text-text capitalize">{restoreTarget?.kind}</p>
            <p className="mt-1 truncate font-mono text-xs text-faint">{restoreTarget?.id}</p>
          </div>
          <label className="grid gap-1">
            <span className="label">Type <span className="font-mono text-text">restore-control-plane</span> to confirm</span>
            <Input autoFocus className="font-mono" placeholder="restore-control-plane" value={restoreConfirmText} onChange={(event) => setRestoreConfirmText(event.target.value)} />
          </label>
          {platformRestoreMutation.error ? <p className="text-sm text-danger">{platformRestoreMutation.error.message}</p> : null}
        </div>
      </Modal>
      <Modal
        description={externalAccessConfirm ? "This publishes project databases through the edge router, fleet-wide." : "This forces every project's database private, fleet-wide."}
        footer={
          <>
            <Button variant="secondary" disabled={mutation.isPending} onClick={() => setExternalAccessConfirm(null)} type="button">Cancel</Button>
            <Button variant={externalAccessConfirm ? "default" : "danger"} disabled={mutation.isPending} onClick={() => applyExternalAccess(externalAccessConfirm === true)} type="button">
              {mutation.isPending ? "Working…" : externalAccessConfirm ? "Enable external access" : "Disable external access"}
            </Button>
          </>
        }
        onClose={() => !mutation.isPending && setExternalAccessConfirm(null)}
        open={externalAccessConfirm !== null}
        title={externalAccessConfirm ? "Enable external database access?" : "Disable external database access?"}
      >
        <div className="grid gap-2 text-sm text-muted">
          {externalAccessConfirm ? (
            <p>Projects set to <span className="font-mono">public</span> or <span className="font-mono">allowlisted</span> will become reachable per their own settings; projects left private stay private. The host must also publish the database port for external clients to actually connect.</p>
          ) : (
            <p>Every project's database becomes unreachable from outside the platform immediately, regardless of its per-project setting. Internal project services are unaffected.</p>
          )}
        </div>
      </Modal>
    </AppPanel>
  );
}

function SummaryCard({ detail, label, metrics, onClick, statusLabel, title, tone }: { label: string; title: string; detail?: string; metrics?: Array<{ label: string; value: string }>; statusLabel?: string; tone?: Tone; onClick: () => void }) {
  return (
    <CardButton className="grid min-h-36 content-between gap-4" onClick={onClick}>
      <span className="flex items-start justify-between gap-3">
        <span className="min-w-0">
          <span className="label">{label}</span>
          <span className="mt-1 block truncate text-base font-semibold">{title}</span>
        </span>
        {tone ? <StatusPill tone={tone} label={statusLabel} /> : null}
      </span>
      {metrics ? (
        <span className="grid grid-cols-3 gap-1.5 max-sm:grid-cols-2">
          {metrics.map((metric) => (
            <MetricCard key={metric.label} label={metric.label} value={metric.value} />
          ))}
        </span>
      ) : detail ? (
        <span className="text-sm text-muted">{detail}</span>
      ) : null}
    </CardButton>
  );
}

function SaveRow({ detail, disabled, title }: { title: string; detail: string; disabled: boolean }) {
  return (
    <div className="usage-row">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{title}</p>
        <p className="truncate text-xs text-muted">{detail}</p>
      </div>
      <Button variant="secondary" disabled={disabled} type="submit">
        <Save size={14} />
        Save
      </Button>
    </div>
  );
}

function SMTPInput({ label, mono = false, numeric = false, onChange, placeholder, value }: { label: string; value: string; onChange: (value: string) => void; mono?: boolean; numeric?: boolean; placeholder?: string }) {
  return (
    <label className="grid gap-1">
      <span className="label">{label}</span>
      <Input className={mono ? "font-mono" : undefined} inputMode={numeric ? "numeric" : undefined} placeholder={placeholder} value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function BackupTargetInput({ label, mono = false, onChange, placeholder, value }: { label: string; value: string; onChange: (value: string) => void; mono?: boolean; placeholder?: string }) {
  return (
    <label className="grid gap-1">
      <span className="label">{label}</span>
      <Input className={mono ? "font-mono" : undefined} placeholder={placeholder} value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

function SSOInput({ label, mono = false, onChange, value }: { label: string; value: string; onChange: (value: string) => void; mono?: boolean }) {
  return (
    <label className="grid gap-1">
      <span className="label">{label}</span>
      <Input className={mono ? "font-mono" : undefined} value={value} onChange={(event) => onChange(event.target.value)} />
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
        cell: ({ row }) => <StatusPill tone={row.original.active ? "success" : "neutral"} label={row.original.active ? "active" : "inactive"} />,
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
        cell: ({ row }) => <Badge variant="muted">{row.original.meta.resourceType}</Badge>,
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
