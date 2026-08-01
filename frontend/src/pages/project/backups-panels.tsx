import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import { Database, HardDriveUpload, Play, RotateCcw } from "lucide-react";
import { DataTable } from "../../components/data-table";
import { Modal } from "../../components/modal";
import { AppPanel } from "../../components/app/app-panel";
import { Button, buttonVariants } from "../../components/ui/button";
import { CollapsibleCard } from "../../components/ui/collapsible-card";
import { EmptyState } from "../../components/ui/empty-state";
import { Field, SubSection } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { MetricCard } from "../../components/app/metric-card";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import { Switch } from "../../components/ui/switch";
import {
  archiveWAL,
  restoreBackup,
  restoreToTime,
  triggerBackup,
  updateBackupPolicy,
  updatePITRPolicy,
} from "../../api";
import { formatBytes, formatDateTime, formatRelativeTime, shortChecksum } from "../../lib/format";
import { statusTone } from "../../lib/status";
import type { Backup, BackupPolicy, BackupStorageTarget, PITRPolicy, Project, ProjectRecoverabilityStatus, WALArchive } from "../../types";
import { useUIStore } from "../../lib/ui-store";

type BackupConfirm = { kind: "trigger" } | { kind: "restore"; backup: Backup };
type GateState = "ready" | "warning" | "missing";
type RecoverabilityGate = { label: string; detail: string; state: GateState };
type GateGroup = { title: string; description: string; gates: RecoverabilityGate[] };

function gateTone(state: GateState) {
  return state === "ready" ? ("success" as const) : state === "warning" ? ("warning" as const) : ("danger" as const);
}

function gateLabel(state: GateState) {
  return state === "ready" ? "ready" : state === "warning" ? "pending" : "missing";
}

function recoverabilityTitle(status: ProjectRecoverabilityStatus) {
  if (status.restore_to_time_available) return "Restore-to-time ready";
  if (status.off_host_backup_verified) return "Off-host backup verified";
  if (status.latest_verified_backup) return "Local backup verified";
  if (status.backup_policy_enabled) return "Backup scheduled";
  return "Recovery not protected";
}

function recoverabilityDetail(status: ProjectRecoverabilityStatus) {
  if (status.restore_to_time_available && status.recovery_window_start && status.recovery_window_end) {
    return `PITR window ${formatDateTime(status.recovery_window_start)} to ${formatDateTime(status.recovery_window_end)}`;
  }
  if (status.off_host_backup_verified) {
    return "Latest verified backup has an off-host artifact.";
  }
  if (status.latest_verified_backup) {
    return status.warnings[0] ?? "Latest verified backup is local-only.";
  }
  return status.recommendations[0] ?? status.warnings[0] ?? "Enable backups before relying on project recovery.";
}

// The eight readiness checks grouped so they ladder up to the headline verdict:
// can I recover locally, off-host, and to an arbitrary point in time?
function recoverabilityGroups(status: ProjectRecoverabilityStatus): GateGroup[] {
  const walVerifiedAt = status.latest_wal_archive?.verified_at ?? status.latest_wal_archive?.created_at;
  return [
    {
      title: "Local protection",
      description: "Scheduled backups exist and have been verified on this host.",
      gates: [
        {
          label: "Backup policy",
          detail: status.backup_policy_enabled ? "Scheduled project backups are enabled." : "Scheduled backups are disabled.",
          state: status.backup_policy_enabled ? "ready" : "missing",
        },
        {
          label: "Verified backup",
          detail: status.latest_verified_backup
            ? `${status.latest_verified_backup.kind} backup verified ${formatDateTime(status.latest_verified_backup.verified_at ?? status.latest_verified_backup.created_at)}.`
            : "No completed verified backup is available.",
          state: status.latest_verified_backup ? "ready" : status.latest_backup ? "warning" : "missing",
        },
      ],
    },
    {
      title: "Off-host durability",
      description: "Backups survive loss of this host by living on a remote target.",
      gates: [
        {
          label: "Off-host target",
          detail: status.off_host_backup_configured ? "A default or project S3-compatible target has passed validation." : "No validated default or project S3-compatible target is configured.",
          state: status.off_host_backup_configured ? "ready" : "missing",
        },
        {
          label: "Off-host artifact",
          detail: status.off_host_backup_verified ? "Latest verified backup has a remote artifact." : "Run a backup after configuring the target.",
          state: status.off_host_backup_verified ? "ready" : status.off_host_backup_configured ? "warning" : "missing",
        },
      ],
    },
    {
      title: "Point-in-time recovery",
      description: "WAL archiving and a base backup let you restore to any moment in the window.",
      gates: [
        {
          label: "Physical base backup",
          detail: status.physical_backup_available ? "Verified physical backup is available for PITR." : "No verified physical base backup is available.",
          state: status.physical_backup_available ? "ready" : "missing",
        },
        {
          label: "PITR policy",
          detail: status.pitr_enabled ? "WAL archive policy is enabled." : "PITR is disabled.",
          state: status.pitr_enabled ? "ready" : "missing",
        },
        {
          label: "WAL archive",
          detail: status.wal_archive_off_host_verified
            ? `Remote WAL archive verified ${walVerifiedAt ? formatDateTime(walVerifiedAt) : "recently"}.`
            : status.latest_wal_archive
              ? "WAL archive exists only locally."
              : "No verified WAL archive is available.",
          state: status.wal_archive_off_host_verified ? "ready" : status.latest_wal_archive ? "warning" : "missing",
        },
        {
          label: "Restore command",
          detail: status.restore_to_time_configured ? "Restore-to-time command is configured." : "SUPADUPA_PITR_RESTORE_COMMAND is not configured.",
          state: status.restore_to_time_configured ? "ready" : "missing",
        },
      ],
    },
  ];
}

export function BackupPanel({ project, backups, policy, recoverability, storageTargets, loading }: { project?: Project; backups: Backup[]; policy?: BackupPolicy; recoverability?: ProjectRecoverabilityStatus; storageTargets: BackupStorageTarget[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const addToast = useUIStore((state) => state.addToast);
  const dismissedHints = useUIStore((state) => state.dismissedHints);
  const dismissHint = useUIStore((state) => state.dismissHint);
  const [enabled, setEnabled] = useState(true);
  const [schedule, setSchedule] = useState("daily");
  const [kind, setKind] = useState("logical");
  const [storageTargetID, setStorageTargetID] = useState("");
  const [confirmAction, setConfirmAction] = useState<BackupConfirm | null>(null);
  const [restoreConfirmation, setRestoreConfirmation] = useState("");
  const [showAll, setShowAll] = useState(false);
  const policyKey = `${project?.ref ?? ""}:${policy?.enabled ?? ""}:${policy?.schedule ?? ""}:${policy?.kind ?? ""}:${policy?.storage_target_id ?? ""}`;
  useEffect(() => {
    if (policy) {
      setEnabled(policy.enabled);
      setSchedule(policy.schedule);
      setKind(policy.kind);
      setStorageTargetID(policy.storage_target_id ?? "");
    }
  }, [policyKey, policy]);
  const mutation = useMutation({
    mutationFn: triggerBackup,
    onSuccess: (_, ref) => {
      setConfirmAction(null);
      void queryClient.invalidateQueries({ queryKey: ["backups", ref] });
      void queryClient.invalidateQueries({ queryKey: ["backup-policy", ref] });
      void queryClient.invalidateQueries({ queryKey: ["recoverability", ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
      addToast({ title: "Backup triggered", detail: ref });
    },
    onError: (error) => {
      addToast({ title: "Backup failed", detail: error.message, kind: "danger" });
    },
  });
  const policyMutation = useMutation({
    mutationFn: ({ ref, nextEnabled, nextSchedule, nextKind, nextStorageTargetID }: { ref: string; nextEnabled: boolean; nextSchedule: string; nextKind: string; nextStorageTargetID: string }) =>
      updateBackupPolicy(ref, { enabled: nextEnabled, schedule: nextSchedule, kind: nextKind, storage_target_id: nextStorageTargetID }),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["backup-policy", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["recoverability", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const restoreMutation = useMutation({
    mutationFn: ({ ref, backupId, confirmation }: { ref: string; backupId: string; confirmation: string }) => restoreBackup(ref, backupId, confirmation),
    onSuccess: (_, variables) => {
      setConfirmAction(null);
      setRestoreConfirmation("");
      void queryClient.invalidateQueries({ queryKey: ["recoverability", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const confirmPending = mutation.isPending || restoreMutation.isPending;
  const activeBackupTitle = confirmAction?.kind === "trigger" ? `Trigger ${policy?.kind ?? kind} backup?` : confirmAction?.kind === "restore" ? "Restore backup?" : "Confirm backup action";
  const expectedRestoreConfirmation = project ? `restore project ${project.ref}` : "";

  function runConfirmedBackupAction() {
    if (!project || !confirmAction) return;
    if (confirmAction.kind === "trigger") {
      mutation.mutate(project.ref);
      return;
    }
    restoreMutation.mutate({ ref: project.ref, backupId: confirmAction.backup.id, confirmation: restoreConfirmation });
  }

  const backupColumns = useMemo<ColumnDef<Backup>[]>(
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
        cell: ({ row }) => <StatusPill tone={statusTone(row.original.status)} label={row.original.status} />,
      },
      {
        header: "Started",
        accessorKey: "started_at",
        size: 150,
        cell: ({ row }) => formatRelativeTime(row.original.started_at ?? row.original.created_at),
      },
      {
        header: "Finished",
        accessorKey: "finished_at",
        size: 150,
        cell: ({ row }) => row.original.finished_at || row.original.verified_at ? formatRelativeTime(row.original.finished_at ?? row.original.verified_at ?? "") : "pending",
      },
      {
        header: "Artifact",
        accessorKey: "location",
        size: 320,
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
        cell: ({ row }) => {
          // Restore here only handles completed logical backups; physical
          // backups recover via PITR, so explain rather than silently disable.
          const restorable = row.original.status === "completed" && row.original.kind === "logical";
          const reason = row.original.kind !== "logical" ? "Physical backups restore via PITR (use Restore to time)" : row.original.status !== "completed" ? "Backup is not completed yet" : "Restore from this backup";
          return (
            <Button variant="ghost" size="icon" disabled={!project || restoreMutation.isPending || !restorable} onClick={() => setConfirmAction({ kind: "restore", backup: row.original })} title={reason} type="button">
              <Play size={14} />
            </Button>
          );
        },
      },
    ],
    [project, restoreMutation.isPending],
  );

  const lastVerified = recoverability?.latest_verified_backup;
  const verdict = recoverability ? (recoverability.restore_to_time_available || recoverability.off_host_backup_verified ? "success" : recoverability.latest_verified_backup ? "warning" : "danger") : "default";
  const visibleBackups = showAll ? backups : backups.slice(0, 5);
  // No platform backup storage target means scheduled/triggered backups fall
  // back to local artifacts on the host (not offsite/durable). Guide the user to
  // configure one, but let them dismiss it if local-only is acceptable.
  const guidanceKey = `backup-guidance:${project?.ref ?? ""}`;
  const showBackupGuidance = !loading && storageTargets.length === 0 && !dismissedHints[guidanceKey];

  return (
    <>
      <AppPanel
        eyebrow="Backups"
        title="Recoverability"
        actions={
          <Button variant="secondary" disabled={!project || mutation.isPending} onClick={() => setConfirmAction({ kind: "trigger" })} type="button">
            <RotateCcw size={14} />
            Back up now
          </Button>
        }
      >
        <div className="mt-4 grid gap-3">
          {loading ? <p className="text-sm text-muted">Loading backups...</p> : null}

          {showBackupGuidance ? (
            <div className="rounded-md border border-warning/40 bg-warning/5 p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-start gap-3">
                  <span className="mt-0.5 grid h-8 w-8 shrink-0 place-items-center rounded-md border border-warning/40 text-warning">
                    <HardDriveUpload size={16} />
                  </span>
                  <div className="min-w-0">
                    <p className="text-sm font-medium">No backup destination configured</p>
                    <p className="mt-1 text-xs leading-5 text-muted">
                      This platform has no backup storage target, so backups would be written as local artifacts on the host — lost if the host is. Set up durable, offsite backups:
                    </p>
                    <ol className="mt-2 grid list-decimal gap-1 pl-4 text-xs leading-5 text-muted">
                      <li>Add an S3-compatible backup storage target in <span className="font-medium text-text">Settings → Backups</span> (bucket, region, endpoint, credentials).</li>
                      <li>Choose it as this project's <span className="font-medium text-text">Storage target</span> in the backup policy below and enable a schedule.</li>
                      <li>Or click <span className="font-medium text-text">Back up now</span> any time for a one-off snapshot.</li>
                    </ol>
                  </div>
                </div>
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                <Link className={buttonVariants({ variant: "secondary", size: "sm" })} to="/settings">
                  Configure backup targets
                </Link>
                <Button variant="ghost" size="sm" onClick={() => dismissHint(guidanceKey)} type="button">
                  Dismiss — I don't need backups here
                </Button>
              </div>
            </div>
          ) : null}

          {recoverability ? (
            <>
              {/* RPO/RTO headline — answer "how recoverable am I?" before the gates. */}
              <div className="grid grid-cols-4 gap-2 max-lg:grid-cols-2">
                <MetricCard
                  label="Recovery readiness"
                  value={recoverabilityTitle(recoverability)}
                  detail={recoverabilityDetail(recoverability)}
                  tone={verdict === "default" ? undefined : verdict}
                />
                <MetricCard
                  label="Last successful backup"
                  value={lastVerified ? formatRelativeTime(lastVerified.verified_at ?? lastVerified.created_at) : "none"}
                  detail={lastVerified ? `${lastVerified.kind} · ${formatBytes(lastVerified.size_bytes)}` : "No verified backup yet"}
                  tone={lastVerified ? "success" : "danger"}
                />
                <MetricCard
                  label="Recovery point (RPO)"
                  value={recoverability.restore_to_time_available ? "continuous (PITR)" : recoverability.backup_policy_enabled ? `~${policy?.schedule ?? schedule}` : "unprotected"}
                  detail={recoverability.restore_to_time_available ? "Restore to any point in the window" : recoverability.backup_policy_enabled ? "Data since last scheduled backup is at risk" : "Enable a backup policy to bound data loss"}
                  tone={recoverability.restore_to_time_available ? "success" : recoverability.backup_policy_enabled ? "warning" : "danger"}
                />
                <MetricCard
                  label="PITR window"
                  value={recoverability.recovery_window_start && recoverability.recovery_window_end ? `${formatDateTime(recoverability.recovery_window_start)} →` : "unavailable"}
                  detail={recoverability.recovery_window_start && recoverability.recovery_window_end ? formatDateTime(recoverability.recovery_window_end) : recoverability.restore_to_time_unavailable ?? "Restore-to-time is not configured"}
                  tone={recoverability.restore_to_time_available ? "success" : undefined}
                />
              </div>

              {/* Gates grouped by what they protect, laddering up to the verdict. */}
              <div className="grid gap-3 rounded-md border border-border bg-bg p-3">
                {recoverabilityGroups(recoverability).map((group) => (
                  <SubSection key={group.title} title={group.title} description={group.description}>
                    <div className="recoverability-grid">
                      {group.gates.map((gate) => (
                        <div className="readiness-row" key={gate.label}>
                          <div className="min-w-0">
                            <p className="truncate text-sm font-medium">{gate.label}</p>
                            <p className="mt-1 text-xs text-muted">{gate.detail}</p>
                          </div>
                          <StatusPill tone={gateTone(gate.state)} label={gateLabel(gate.state)} />
                        </div>
                      ))}
                    </div>
                  </SubSection>
                ))}
              </div>
            </>
          ) : null}

          {project && policy ? (
            <CollapsibleCard
              eyebrow="Schedule"
              title="Backup policy"
              description={`${policy.enabled ? `Next run ${policy.next_run_at ? formatRelativeTime(policy.next_run_at) : "pending"}` : "Disabled"} · ${policy.storage_target_id ? storageTargets.find((target) => target.id === policy.storage_target_id)?.name ?? "target pending" : storageTargets.some((target) => target.default) ? "platform default target" : "local artifact"}`}
            >
              <div className="grid grid-cols-[auto_120px_120px_minmax(0,1fr)_auto] items-end gap-2 max-md:grid-cols-1">
                <Field label="Enabled" hint="Run scheduled backups">
                  <label className="flex h-9 items-center gap-2 text-sm">
                    <Switch checked={enabled} onCheckedChange={(next) => setEnabled(next)} aria-label="Scheduled backups enabled" />
                    On
                  </label>
                </Field>
                <Field label="Schedule" hint="Backup cadence">
                  <NativeSelect value={schedule} onChange={(event) => setSchedule(event.target.value)}>
                    <option value="daily">Daily</option>
                    <option value="hourly">Hourly</option>
                  </NativeSelect>
                </Field>
                <Field label="Kind" hint="Backup type">
                  <NativeSelect value={kind} onChange={(event) => setKind(event.target.value)}>
                    <option value="logical">Logical</option>
                    <option value="physical">Physical</option>
                  </NativeSelect>
                </Field>
                <Field label="Storage target" hint="Where artifacts are written">
                  <NativeSelect value={storageTargetID} onChange={(event) => setStorageTargetID(event.target.value)}>
                    <option value="">Platform default</option>
                    {storageTargets.map((target) => (
                      <option key={target.id} value={target.id}>{target.default ? `${target.name} (default)` : target.name}</option>
                    ))}
                  </NativeSelect>
                </Field>
                <Button variant="secondary" disabled={policyMutation.isPending} onClick={() => policyMutation.mutate({ ref: project.ref, nextEnabled: enabled, nextSchedule: schedule, nextKind: kind, nextStorageTargetID: storageTargetID })} type="button">
                  Save
                </Button>
              </div>
              {policyMutation.error ? <p className="mt-2 text-sm text-danger">{policyMutation.error.message}</p> : null}
            </CollapsibleCard>
          ) : null}

          <SubSection
            title="Backup artifacts"
            description="Logical backups restore here; physical backups recover via PITR."
            actions={backups.length > 5 ? <Button variant="secondary" size="sm" onClick={() => setShowAll((value) => !value)} type="button">{showAll ? "Show recent" : `View all (${backups.length})`}</Button> : <span className="text-xs text-faint">{backups.length} total</span>}
          >
            {backups.length === 0 ? (
              <EmptyState icon={RotateCcw} title="No backups recorded" description="Trigger a backup or enable a schedule to start protecting this project." action={<Button variant="secondary" disabled={!project || mutation.isPending} onClick={() => setConfirmAction({ kind: "trigger" })} type="button">Back up now</Button>} />
            ) : (
              <DataTable columns={backupColumns} data={visibleBackups} emptyText="" />
            )}
          </SubSection>

          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
          {restoreMutation.error ? <p className="text-sm text-danger">{restoreMutation.error.message}</p> : null}
          {restoreMutation.data ? (
            <p className="truncate font-mono text-xs text-muted">
              Restore {restoreMutation.data.restore_state}: {restoreMutation.data.restore_path}
            </p>
          ) : null}
        </div>
      </AppPanel>
      <Modal
        description={confirmAction?.kind === "trigger" ? `This creates a ${policy?.kind ?? kind} backup artifact for the selected project.` : "This starts a restore workflow from the selected logical backup."}
        onClose={() => {
          if (confirmPending) return;
          setConfirmAction(null);
          setRestoreConfirmation("");
        }}
        open={Boolean(confirmAction)}
        title={activeBackupTitle}
        footer={(
          <>
            <Button variant="secondary" disabled={confirmPending} onClick={() => {
              setConfirmAction(null);
              setRestoreConfirmation("");
            }} type="button">Cancel</Button>
            <Button variant={confirmAction?.kind === "restore" ? "danger" : "default"} disabled={confirmPending || !project || (confirmAction?.kind === "restore" && restoreConfirmation.trim() !== expectedRestoreConfirmation)} onClick={runConfirmedBackupAction} type="button">
              {confirmPending ? "Working..." : confirmAction?.kind === "restore" ? "Restore backup" : "Trigger backup"}
            </Button>
          </>
        )}
      >
        {confirmAction?.kind === "restore" ? (
          <div className="grid gap-3 text-sm text-muted">
            <p>Restoring can overwrite project data depending on the configured restore command.</p>
            <p className="truncate font-mono text-xs text-faint">{confirmAction.backup.location}</p>
            <label className="grid gap-1">
              <span className="label">Type <span className="font-mono text-text">{expectedRestoreConfirmation}</span> to confirm</span>
              <Input autoFocus className="font-mono" placeholder={expectedRestoreConfirmation} value={restoreConfirmation} onChange={(event) => setRestoreConfirmation(event.target.value)} />
            </label>
          </div>
        ) : (
          <p className="text-sm text-muted">Backup jobs can consume disk and database resources while they run.</p>
        )}
      </Modal>
    </>
  );
}

export function PITRPanel({ project, policy, recoverability, archives, loading, enabled: featureEnabled }: { project?: Project; policy?: PITRPolicy; recoverability?: ProjectRecoverabilityStatus; archives: WALArchive[]; loading: boolean; enabled: boolean }) {
  const queryClient = useQueryClient();
  const [pitrEnabled, setPITREnabled] = useState(false);
  const [archiveBucket, setArchiveBucket] = useState("");
  const [retentionDays, setRetentionDays] = useState(7);
  const [confirmArchive, setConfirmArchive] = useState(false);
  const [restoreTarget, setRestoreTarget] = useState("");
  const [restoreConfirmation, setRestoreConfirmation] = useState("");
  const [showAll, setShowAll] = useState(false);
  const policyKey = `${project?.ref ?? ""}:${policy?.enabled ?? ""}:${policy?.archive_bucket ?? ""}:${policy?.retention_days ?? ""}`;
  useEffect(() => {
    if (policy) {
      setPITREnabled(policy.enabled);
      setArchiveBucket(policy.archive_bucket);
      setRetentionDays(policy.retention_days);
    }
  }, [policyKey, policy]);
  const policyMutation = useMutation({
    mutationFn: ({ ref, input }: { ref: string; input: { enabled: boolean; archive_bucket: string; retention_days: number } }) => updatePITRPolicy(ref, input),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["pitr-policy", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["recoverability", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const archiveMutation = useMutation({
    mutationFn: archiveWAL,
    onSuccess: (_, ref) => {
      setConfirmArchive(false);
      void queryClient.invalidateQueries({ queryKey: ["wal-archives", ref] });
      void queryClient.invalidateQueries({ queryKey: ["pitr-policy", ref] });
      void queryClient.invalidateQueries({ queryKey: ["recoverability", ref] });
      void queryClient.invalidateQueries({ queryKey: ["org-usage"] });
      void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const restoreToTimeMutation = useMutation({
    mutationFn: ({ ref, targetUnix, confirmation }: { ref: string; targetUnix: number; confirmation: string }) => restoreToTime(ref, targetUnix, confirmation),
    onSuccess: (_, variables) => {
      setRestoreConfirmation("");
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const restoreTargetUnix = restoreTarget ? Math.floor(new Date(restoreTarget).getTime() / 1000) : 0;
  const expectedPITRConfirmation = project ? `restore pitr project ${project.ref}` : "";
  const restoreToTimeDisabled = !featureEnabled || !project || !recoverability?.restore_to_time_available || !restoreTargetUnix || restoreConfirmation.trim() !== expectedPITRConfirmation || restoreToTimeMutation.isPending;
  const archiveColumns = useMemo<ColumnDef<WALArchive>[]>(
    () => [
      {
        header: "Segment",
        accessorKey: "segment",
        size: 240,
        cell: ({ row }) => (
          <>
            <p className="cell-main font-mono">{row.original.segment}</p>
            {row.original.checksum_sha256 ? <p className="cell-sub font-mono">sha256:{shortChecksum(row.original.checksum_sha256)}</p> : null}
          </>
        ),
      },
      {
        header: "Status",
        accessorKey: "status",
        size: 120,
        cell: ({ row }) => <StatusPill tone={statusTone(row.original.status)} label={row.original.status} />,
      },
      {
        header: "Created",
        accessorKey: "created_at",
        size: 150,
        cell: ({ row }) => formatRelativeTime(row.original.created_at),
      },
      {
        header: "Verified",
        accessorKey: "verified_at",
        size: 150,
        cell: ({ row }) => row.original.verified_at ? formatRelativeTime(row.original.verified_at) : "pending",
      },
      {
        header: "Artifact",
        accessorKey: "location",
        size: 340,
        cell: ({ row }) => <p className="truncate font-mono text-xs text-muted">{row.original.remote_location || row.original.location}</p>,
      },
      {
        header: "Size",
        accessorKey: "size_bytes",
        size: 100,
        cell: ({ row }) => formatBytes(row.original.size_bytes),
      },
    ],
    [],
  );
  const visibleArchives = showAll ? archives : archives.slice(0, 5);

  return (
    <>
      <AppPanel
        eyebrow="PITR"
        title="Restore to time"
        description="Point-in-time recovery via continuous WAL archiving — roll back to any moment in the window."
        actions={
          <Button variant="secondary" disabled={!featureEnabled || !project || archiveMutation.isPending || !policy?.enabled} onClick={() => setConfirmArchive(true)} title="Archive a WAL segment now" type="button">
            <Database size={14} />
            Archive WAL
          </Button>
        }
      >
        <div className="mt-4 grid gap-3">
          {!featureEnabled ? (
            <div className="rounded-md border border-border bg-bg p-3">
              <p className="text-sm font-medium">PITR disabled</p>
              <p className="mt-1 text-sm text-muted">Point-in-time recovery is turned off via platform/org feature flags (pitr). Enable the flag under Settings → Feature flags or org Features before changing WAL archive policy — this is intentional, not a broken panel.</p>
            </div>
          ) : null}
          {loading ? <p className="text-sm text-muted">Loading PITR...</p> : null}

          {/* Restore-to-time is the primary recovery path for physical backups. */}
          {project ? (
            <SubSection title="Restore to point in time" description={recoverability?.restore_to_time_available && recoverability.recovery_window_start && recoverability.recovery_window_end ? `Recoverable window: ${formatDateTime(recoverability.recovery_window_start)} to ${formatDateTime(recoverability.recovery_window_end)}` : recoverability?.restore_to_time_unavailable ?? "Restore-to-time is not configured."}>
              <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)_auto] items-end gap-2 max-lg:grid-cols-1">
                <Field label="Target time" hint="Database is recovered to this moment">
                  <Input disabled={!featureEnabled || !recoverability?.restore_to_time_available} onChange={(event) => setRestoreTarget(event.target.value)} type="datetime-local" value={restoreTarget} />
                </Field>
                <Field label="Confirmation" hint={`Type ${expectedPITRConfirmation}`}>
                  <Input className="font-mono" disabled={!featureEnabled || !recoverability?.restore_to_time_available} onChange={(event) => setRestoreConfirmation(event.target.value)} placeholder={expectedPITRConfirmation} value={restoreConfirmation} />
                </Field>
                <Button variant="danger" disabled={restoreToTimeDisabled} onClick={() => project && restoreToTimeMutation.mutate({ ref: project.ref, targetUnix: restoreTargetUnix, confirmation: restoreConfirmation })} type="button">
                  {restoreToTimeMutation.isPending ? "Restoring..." : "Restore to time"}
                </Button>
              </div>
              {restoreToTimeMutation.error ? <p className="text-sm text-danger">{restoreToTimeMutation.error.message}</p> : null}
              {restoreToTimeMutation.data ? <p className="text-sm text-muted">PITR restore {restoreToTimeMutation.data.restore_state}: {restoreToTimeMutation.data.restore_path}</p> : null}
            </SubSection>
          ) : null}

          {project && policy ? (
            <CollapsibleCard eyebrow="Policy" title="WAL archive policy" description={policy.enabled ? `Archiving · last archive ${policy.last_archive_at ? formatRelativeTime(policy.last_archive_at) : "pending"}` : "Disabled"}>
              <div className="grid grid-cols-[auto_minmax(0,1fr)_120px_auto] items-end gap-2 max-md:grid-cols-1">
                <Field label="Enabled" hint="Continuously archive WAL">
                  <label className="flex items-center gap-2 text-sm">
                    <Switch checked={pitrEnabled} disabled={!featureEnabled} onCheckedChange={(next) => setPITREnabled(next)} aria-label="On" />
                    On
                  </label>
                </Field>
                <Field label="Archive bucket" hint="Blank uses the backup storage target">
                  <Input disabled={!featureEnabled} onChange={(event) => setArchiveBucket(event.target.value)} placeholder="Use backup target automatically" value={archiveBucket} />
                </Field>
                <Field label="Retention" hint="Days of WAL to keep">
                  <Input disabled={!featureEnabled} min={1} max={35} onChange={(event) => setRetentionDays(Number(event.target.value))} type="number" value={retentionDays} />
                </Field>
                <Button variant="secondary" disabled={!featureEnabled || !project || policyMutation.isPending} onClick={() => policyMutation.mutate({ ref: project.ref, input: { enabled: pitrEnabled, archive_bucket: archiveBucket, retention_days: retentionDays } })} type="button">
                  Save
                </Button>
              </div>
              {policyMutation.error ? <p className="mt-2 text-sm text-danger">{policyMutation.error.message}</p> : null}
            </CollapsibleCard>
          ) : null}

          <SubSection
            title="WAL archive segments"
            description="Archived write-ahead log segments backing the recovery window."
            actions={archives.length > 5 ? <Button variant="secondary" size="sm" onClick={() => setShowAll((value) => !value)} type="button">{showAll ? "Show recent" : `View all (${archives.length})`}</Button> : <span className="text-xs text-faint">{archives.length} total</span>}
          >
            {archives.length === 0 ? (
              <EmptyState icon={Database} title="No WAL archives" description="Enable the WAL archive policy to begin continuous archiving for point-in-time recovery." />
            ) : (
              <DataTable columns={archiveColumns} data={visibleArchives} emptyText="" />
            )}
          </SubSection>

          {archiveMutation.error ? <p className="text-sm text-danger">{archiveMutation.error.message}</p> : null}
        </div>
      </AppPanel>
      <Modal
        description="This records a WAL archive segment for point-in-time recovery."
        onClose={() => !archiveMutation.isPending && setConfirmArchive(false)}
        open={confirmArchive}
        title="Archive WAL segment?"
        footer={(
          <>
            <Button variant="secondary" disabled={archiveMutation.isPending} onClick={() => setConfirmArchive(false)} type="button">Cancel</Button>
            <Button disabled={!featureEnabled || !project || archiveMutation.isPending || !policy?.enabled} onClick={() => project && archiveMutation.mutate(project.ref)} type="button">
              {archiveMutation.isPending ? "Archiving..." : "Archive WAL"}
            </Button>
          </>
        )}
      >
        <p className="text-sm text-muted">
          Manual WAL archive jobs can consume storage and should match the configured retention and archive bucket policy.
        </p>
      </Modal>
    </>
  );
}
