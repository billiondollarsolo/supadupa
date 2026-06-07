import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { AlertTriangle, CheckCircle2, Database, Play, RotateCcw } from "lucide-react";
import { DataTable } from "../../components/data-table";
import { Modal } from "../../components/modal";
import {
  archiveWAL,
  restoreBackup,
  restoreToTime,
  triggerBackup,
  updateBackupPolicy,
  updatePITRPolicy,
} from "../../api";
import { formatBytes, formatDateTime, shortChecksum } from "../../lib/format";
import type { Backup, BackupPolicy, BackupStorageTarget, PITRPolicy, Project, ProjectRecoverabilityStatus, WALArchive } from "../../types";
import { useUIStore } from "../../lib/ui-store";

type BackupConfirm = { kind: "trigger" } | { kind: "restore"; backup: Backup };
type RecoverabilityGate = { label: string; detail: string; state: "ready" | "warning" | "missing" };

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

function recoverabilityGates(status: ProjectRecoverabilityStatus): RecoverabilityGate[] {
  const walVerifiedAt = status.latest_wal_archive?.verified_at ?? status.latest_wal_archive?.created_at;
  return [
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
  ];
}

export function BackupPanel({ project, backups, policy, recoverability, storageTargets, loading }: { project?: Project; backups: Backup[]; policy?: BackupPolicy; recoverability?: ProjectRecoverabilityStatus; storageTargets: BackupStorageTarget[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const addToast = useUIStore((state) => state.addToast);
  const [enabled, setEnabled] = useState(true);
  const [schedule, setSchedule] = useState("daily");
  const [kind, setKind] = useState("logical");
  const [storageTargetID, setStorageTargetID] = useState("");
  const [confirmAction, setConfirmAction] = useState<BackupConfirm | null>(null);
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
    mutationFn: ({ ref, backupId }: { ref: string; backupId: string }) => restoreBackup(ref, backupId),
    onSuccess: (_, variables) => {
      setConfirmAction(null);
      void queryClient.invalidateQueries({ queryKey: ["recoverability", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const confirmPending = mutation.isPending || restoreMutation.isPending;
  const activeBackupTitle = confirmAction?.kind === "trigger" ? `Trigger ${policy?.kind ?? kind} backup?` : confirmAction?.kind === "restore" ? "Restore backup?" : "Confirm backup action";

  function runConfirmedBackupAction() {
    if (!project || !confirmAction) return;
    if (confirmAction.kind === "trigger") {
      mutation.mutate(project.ref);
      return;
    }
    restoreMutation.mutate({ ref: project.ref, backupId: confirmAction.backup.id });
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
        cell: ({ row }) => (
          <button className="icon-button" disabled={!project || restoreMutation.isPending || row.original.status !== "completed" || row.original.kind !== "logical"} onClick={() => setConfirmAction({ kind: "restore", backup: row.original })} title="Restore backup" type="button">
            <Play size={14} />
          </button>
        ),
      },
    ],
    [project, restoreMutation.isPending],
  );

  return (
    <>
      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Backups</p>
            <h2>Backup artifacts</h2>
          </div>
          <button className="icon-button" disabled={!project || mutation.isPending} onClick={() => setConfirmAction({ kind: "trigger" })} type="button">
            <RotateCcw size={15} />
          </button>
        </div>
        <div className="mt-4 grid gap-2">
          {loading ? <p className="text-sm text-muted">Loading backups...</p> : null}
          {recoverability ? (
            <div className="grid gap-2">
              <div className="backup-row">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    {recoverability.restore_to_time_available || recoverability.off_host_backup_verified ? <CheckCircle2 className="text-success" size={16} /> : <AlertTriangle className="text-warning" size={16} />}
                    <p className="truncate text-sm font-medium">{recoverabilityTitle(recoverability)}</p>
                  </div>
                  <p className="mt-1 truncate text-xs text-muted">{recoverabilityDetail(recoverability)}</p>
                </div>
                <div className="min-w-[180px] text-right">
                  <span className={`pill ${recoverability.restore_to_time_available || recoverability.off_host_backup_verified ? "healthy" : recoverability.latest_verified_backup ? "provisioning" : "warning"}`}>{recoverability.status}</span>
                  <p className="mt-1 truncate text-xs text-faint">{recoverability.latest_verified_backup ? `Verified ${formatDateTime(recoverability.latest_verified_backup.verified_at ?? recoverability.latest_verified_backup.created_at)}` : "No verified backup"}</p>
                </div>
              </div>
              <div className="recoverability-grid">
                {recoverabilityGates(recoverability).map((gate) => (
                  <div className="readiness-row" key={gate.label}>
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{gate.label}</p>
                      <p className="mt-1 text-xs text-muted">{gate.detail}</p>
                    </div>
                    <span className={`pill ${gate.state === "ready" ? "healthy" : gate.state === "warning" ? "warning" : "paused"}`}>
                      {gate.state === "ready" ? "ready" : gate.state === "warning" ? "pending" : "missing"}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ) : null}
          {project && policy ? (
            <div className="backup-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Scheduled {policy.kind} backups</p>
                <p className="truncate text-xs text-muted">
                  {policy.enabled ? `Next run ${policy.next_run_at ? formatDateTime(policy.next_run_at) : "pending"}` : "Disabled"} · {policy.storage_target_id ? storageTargets.find((target) => target.id === policy.storage_target_id)?.name ?? "target pending" : storageTargets.some((target) => target.default) ? "platform default target" : "local artifact"}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <label className="flex items-center gap-2 text-sm text-muted">
                  <input checked={enabled} onChange={(event) => setEnabled(event.target.checked)} type="checkbox" />
                  Enabled
                </label>
                <select className="input w-[104px]" value={schedule} onChange={(event) => setSchedule(event.target.value)}>
                  <option value="daily">Daily</option>
                  <option value="hourly">Hourly</option>
                </select>
                <select className="input w-[112px]" value={kind} onChange={(event) => setKind(event.target.value)}>
                  <option value="logical">Logical</option>
                  <option value="physical">Physical</option>
                </select>
                <select className="input min-w-[160px]" value={storageTargetID} onChange={(event) => setStorageTargetID(event.target.value)}>
                  <option value="">Platform default</option>
                  {storageTargets.map((target) => (
                    <option key={target.id} value={target.id}>{target.default ? `${target.name} (default)` : target.name}</option>
                  ))}
                </select>
                <button className="button secondary justify-center" disabled={policyMutation.isPending} onClick={() => policyMutation.mutate({ ref: project.ref, nextEnabled: enabled, nextSchedule: schedule, nextKind: kind, nextStorageTargetID: storageTargetID })} type="button">
                  Save
                </button>
              </div>
            </div>
          ) : null}
          <DataTable columns={backupColumns} data={backups.slice(0, 5)} emptyText="No backups have been recorded." />
          {mutation.error ? <p className="text-sm text-danger">{mutation.error.message}</p> : null}
          {policyMutation.error ? <p className="text-sm text-danger">{policyMutation.error.message}</p> : null}
          {restoreMutation.error ? <p className="text-sm text-danger">{restoreMutation.error.message}</p> : null}
          {restoreMutation.data ? (
            <p className="truncate font-mono text-xs text-muted">
              Restore {restoreMutation.data.restore_state}: {restoreMutation.data.restore_path}
            </p>
          ) : null}
        </div>
      </section>
      <Modal
        description={confirmAction?.kind === "trigger" ? `This creates a ${policy?.kind ?? kind} backup artifact for the selected project.` : "This starts a restore workflow from the selected logical backup."}
        onClose={() => !confirmPending && setConfirmAction(null)}
        open={Boolean(confirmAction)}
        title={activeBackupTitle}
        footer={(
          <>
            <button className="button secondary" disabled={confirmPending} onClick={() => setConfirmAction(null)} type="button">Cancel</button>
            <button className={confirmAction?.kind === "restore" ? "button danger" : "button"} disabled={confirmPending || !project} onClick={runConfirmedBackupAction} type="button">
              {confirmPending ? "Working..." : confirmAction?.kind === "restore" ? "Restore backup" : "Trigger backup"}
            </button>
          </>
        )}
      >
        {confirmAction?.kind === "restore" ? (
          <div className="grid gap-2 text-sm text-muted">
            <p>Restoring can overwrite project data depending on the configured restore command.</p>
            <p className="truncate font-mono text-xs text-faint">{confirmAction.backup.location}</p>
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
    mutationFn: ({ ref, targetUnix }: { ref: string; targetUnix: number }) => restoreToTime(ref, targetUnix),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const restoreTargetUnix = restoreTarget ? Math.floor(new Date(restoreTarget).getTime() / 1000) : 0;
  const restoreToTimeDisabled = !featureEnabled || !project || !recoverability?.restore_to_time_available || !restoreTargetUnix || restoreToTimeMutation.isPending;
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
        cell: ({ row }) => <span className={`pill ${row.original.status === "archived" ? "healthy" : "provisioning"}`}>{row.original.status}</span>,
      },
      {
        header: "Created",
        accessorKey: "created_at",
        size: 150,
        cell: ({ row }) => formatDateTime(row.original.created_at),
      },
      {
        header: "Verified",
        accessorKey: "verified_at",
        size: 150,
        cell: ({ row }) => row.original.verified_at ? formatDateTime(row.original.verified_at) : "pending",
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

  return (
    <>
      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">PITR</p>
            <h2>WAL archive</h2>
          </div>
          <button className="icon-button" disabled={!featureEnabled || !project || archiveMutation.isPending || !policy?.enabled} onClick={() => setConfirmArchive(true)} type="button">
            <Database size={15} />
          </button>
        </div>
        <div className="mt-4 grid gap-2">
          {!featureEnabled ? (
            <div className="rounded-md border border-border bg-bg p-3">
              <p className="text-sm font-medium">PITR disabled</p>
              <p className="mt-1 text-sm text-muted">Enable the pitr feature flag for this org before changing WAL archive policy.</p>
            </div>
          ) : null}
          {loading ? <p className="text-sm text-muted">Loading PITR...</p> : null}
          {project && policy ? (
            <div className="backup-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Point-in-time recovery</p>
                <p className="truncate text-xs text-muted">
                  {policy.enabled ? `Scheduled · Last archive ${policy.last_archive_at ? formatDateTime(policy.last_archive_at) : "pending"}` : "Disabled"}
                </p>
              </div>
              <div className="grid min-w-[220px] gap-2">
                <label className="flex items-center gap-2 text-sm text-muted">
                  <input checked={pitrEnabled} disabled={!featureEnabled} onChange={(event) => setPITREnabled(event.target.checked)} type="checkbox" />
                  Enabled
                </label>
                <input className="input" disabled={!featureEnabled} onChange={(event) => setArchiveBucket(event.target.value)} placeholder="Use backup target automatically" value={archiveBucket} />
                <div className="flex items-center gap-2">
                  <input className="input w-[88px]" disabled={!featureEnabled} min={1} max={35} onChange={(event) => setRetentionDays(Number(event.target.value))} type="number" value={retentionDays} />
                  <button className="button secondary justify-center" disabled={!featureEnabled || !project || policyMutation.isPending} onClick={() => policyMutation.mutate({ ref: project.ref, input: { enabled: pitrEnabled, archive_bucket: archiveBucket, retention_days: retentionDays } })} type="button">
                    Save
                  </button>
                </div>
              </div>
            </div>
          ) : null}
          {project ? (
            <div className="backup-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Restore to time</p>
                <p className="truncate text-xs text-muted">
                  {recoverability?.restore_to_time_available && recoverability.recovery_window_start && recoverability.recovery_window_end
                    ? `${formatDateTime(recoverability.recovery_window_start)} to ${formatDateTime(recoverability.recovery_window_end)}`
                    : recoverability?.restore_to_time_unavailable ?? "Restore-to-time is not configured."}
                </p>
              </div>
              <div className="grid min-w-[220px] gap-2">
                <input className="input" disabled={!featureEnabled || !recoverability?.restore_to_time_available} onChange={(event) => setRestoreTarget(event.target.value)} type="datetime-local" value={restoreTarget} />
                <button className="button secondary justify-center" disabled={restoreToTimeDisabled} onClick={() => project && restoreToTimeMutation.mutate({ ref: project.ref, targetUnix: restoreTargetUnix })} type="button">
                  {restoreToTimeMutation.isPending ? "Restoring..." : "Restore"}
                </button>
              </div>
            </div>
          ) : null}
          <DataTable columns={archiveColumns} data={archives.slice(0, 5)} emptyText="No WAL archives have been recorded." />
          {policyMutation.error ? <p className="text-sm text-danger">{policyMutation.error.message}</p> : null}
          {archiveMutation.error ? <p className="text-sm text-danger">{archiveMutation.error.message}</p> : null}
          {restoreToTimeMutation.error ? <p className="text-sm text-danger">{restoreToTimeMutation.error.message}</p> : null}
          {restoreToTimeMutation.data ? <p className="text-sm text-muted">PITR restore {restoreToTimeMutation.data.restore_state}: {restoreToTimeMutation.data.restore_path}</p> : null}
        </div>
      </section>
      <Modal
        description="This records a WAL archive segment for point-in-time recovery."
        onClose={() => !archiveMutation.isPending && setConfirmArchive(false)}
        open={confirmArchive}
        title="Archive WAL segment?"
        footer={(
          <>
            <button className="button secondary" disabled={archiveMutation.isPending} onClick={() => setConfirmArchive(false)} type="button">Cancel</button>
            <button className="button" disabled={!featureEnabled || !project || archiveMutation.isPending || !policy?.enabled} onClick={() => project && archiveMutation.mutate(project.ref)} type="button">
              {archiveMutation.isPending ? "Archiving..." : "Archive WAL"}
            </button>
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
