import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Database, Play, RotateCcw } from "lucide-react";
import { Modal } from "../../components/modal";
import {
  archiveWAL,
  restoreBackup,
  triggerBackup,
  updateBackupPolicy,
  updatePITRPolicy,
} from "../../api";
import { formatBytes, formatDateTime, shortChecksum } from "../../lib/format";
import type { Backup, BackupPolicy, PITRPolicy, Project, WALArchive } from "../../types";
import { useUIStore } from "../../lib/ui-store";

type BackupConfirm = { kind: "trigger" } | { kind: "restore"; backup: Backup };

export function BackupPanel({ project, backups, policy, loading }: { project?: Project; backups: Backup[]; policy?: BackupPolicy; loading: boolean }) {
  const queryClient = useQueryClient();
  const addToast = useUIStore((state) => state.addToast);
  const [enabled, setEnabled] = useState(true);
  const [schedule, setSchedule] = useState("daily");
  const [confirmAction, setConfirmAction] = useState<BackupConfirm | null>(null);
  const policyKey = `${project?.ref ?? ""}:${policy?.enabled ?? ""}:${policy?.schedule ?? ""}`;
  useEffect(() => {
    if (policy) {
      setEnabled(policy.enabled);
      setSchedule(policy.schedule);
    }
  }, [policyKey, policy]);
  const mutation = useMutation({
    mutationFn: triggerBackup,
    onSuccess: (_, ref) => {
      setConfirmAction(null);
      void queryClient.invalidateQueries({ queryKey: ["backups", ref] });
      void queryClient.invalidateQueries({ queryKey: ["backup-policy", ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
      addToast({ title: "Backup triggered", detail: ref });
    },
    onError: (error) => {
      addToast({ title: "Backup failed", detail: error.message, kind: "danger" });
    },
  });
  const policyMutation = useMutation({
    mutationFn: ({ ref, nextEnabled, nextSchedule }: { ref: string; nextEnabled: boolean; nextSchedule: string }) =>
      updateBackupPolicy(ref, { enabled: nextEnabled, schedule: nextSchedule, kind: "logical" }),
    onSuccess: (_, variables) => {
      void queryClient.invalidateQueries({ queryKey: ["backup-policy", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const restoreMutation = useMutation({
    mutationFn: ({ ref, backupId }: { ref: string; backupId: string }) => restoreBackup(ref, backupId),
    onSuccess: (_, variables) => {
      setConfirmAction(null);
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });
  const confirmPending = mutation.isPending || restoreMutation.isPending;
  const activeBackupTitle = confirmAction?.kind === "trigger" ? "Trigger logical backup?" : confirmAction?.kind === "restore" ? "Restore backup?" : "Confirm backup action";

  function runConfirmedBackupAction() {
    if (!project || !confirmAction) return;
    if (confirmAction.kind === "trigger") {
      mutation.mutate(project.ref);
      return;
    }
    restoreMutation.mutate({ ref: project.ref, backupId: confirmAction.backup.id });
  }

  return (
    <>
      <section className="panel">
        <div className="section-head">
          <div>
            <p className="label">Backups</p>
            <h2>Logical snapshots</h2>
          </div>
          <button className="icon-button" disabled={!project || mutation.isPending} onClick={() => setConfirmAction({ kind: "trigger" })} type="button">
            <RotateCcw size={15} />
          </button>
        </div>
        <div className="mt-4 grid gap-2">
          {loading ? <p className="text-sm text-muted">Loading backups...</p> : null}
          {project && policy ? (
            <div className="backup-row">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">Scheduled {policy.kind} backups</p>
                <p className="truncate text-xs text-muted">
                  {policy.enabled ? `Next run ${policy.next_run_at ? formatDateTime(policy.next_run_at) : "pending"}` : "Disabled"}
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
                <button className="button secondary justify-center" disabled={policyMutation.isPending} onClick={() => policyMutation.mutate({ ref: project.ref, nextEnabled: enabled, nextSchedule: schedule })} type="button">
                  Save
                </button>
              </div>
            </div>
          ) : null}
          {!loading && backups.length === 0 ? <p className="text-sm text-muted">No backups have been recorded.</p> : null}
          {backups.slice(0, 5).map((backup) => (
            <div className="backup-row" key={backup.id}>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{backup.kind} backup</p>
                <p className="truncate font-mono text-xs text-muted">{backup.location}</p>
                {backup.checksum_sha256 ? <p className="truncate font-mono text-xs text-faint">sha256:{shortChecksum(backup.checksum_sha256)}</p> : null}
              </div>
              <div className="text-right">
                <span className={`pill ${backup.status === "completed" ? "healthy" : "provisioning"}`}>{backup.status}</span>
                <p className="mt-1 text-xs text-faint">{formatBytes(backup.size_bytes)}</p>
              </div>
              <button className="icon-button" disabled={!project || restoreMutation.isPending || backup.status !== "completed"} onClick={() => setConfirmAction({ kind: "restore", backup })} type="button">
                <Play size={14} />
              </button>
            </div>
          ))}
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
        description={confirmAction?.kind === "trigger" ? "This creates a logical backup artifact for the selected project." : "This starts a restore workflow from the selected logical backup."}
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

export function PITRPanel({ project, policy, archives, loading, enabled: featureEnabled }: { project?: Project; policy?: PITRPolicy; archives: WALArchive[]; loading: boolean; enabled: boolean }) {
  const queryClient = useQueryClient();
  const [pitrEnabled, setPITREnabled] = useState(false);
  const [archiveBucket, setArchiveBucket] = useState("");
  const [retentionDays, setRetentionDays] = useState(7);
  const [confirmArchive, setConfirmArchive] = useState(false);
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
      void queryClient.invalidateQueries({ queryKey: ["org-usage"] });
      void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
    },
  });

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
                  {policy.enabled ? `Last archive ${policy.last_archive_at ? formatDateTime(policy.last_archive_at) : "pending"}` : "Disabled"}
                </p>
              </div>
              <div className="grid min-w-[220px] gap-2">
                <label className="flex items-center gap-2 text-sm text-muted">
                  <input checked={pitrEnabled} disabled={!featureEnabled} onChange={(event) => setPITREnabled(event.target.checked)} type="checkbox" />
                  Enabled
                </label>
                <input className="input" disabled={!featureEnabled} onChange={(event) => setArchiveBucket(event.target.value)} placeholder="s3://bucket/project" value={archiveBucket} />
                <div className="flex items-center gap-2">
                  <input className="input w-[88px]" disabled={!featureEnabled} min={1} max={35} onChange={(event) => setRetentionDays(Number(event.target.value))} type="number" value={retentionDays} />
                  <button className="button secondary justify-center" disabled={!featureEnabled || !project || policyMutation.isPending} onClick={() => policyMutation.mutate({ ref: project.ref, input: { enabled: pitrEnabled, archive_bucket: archiveBucket, retention_days: retentionDays } })} type="button">
                    Save
                  </button>
                </div>
              </div>
            </div>
          ) : null}
          {!loading && archives.length === 0 ? <p className="text-sm text-muted">No WAL archives have been recorded.</p> : null}
          {archives.slice(0, 5).map((archive) => (
            <div className="backup-row" key={archive.id}>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{archive.segment}</p>
                <p className="truncate font-mono text-xs text-muted">{archive.location}</p>
                {archive.checksum_sha256 ? <p className="truncate font-mono text-xs text-faint">sha256:{shortChecksum(archive.checksum_sha256)}</p> : null}
              </div>
              <div className="text-right">
                <span className={`pill ${archive.status === "archived" ? "healthy" : "provisioning"}`}>{archive.status}</span>
                <p className="mt-1 text-xs text-faint">{formatBytes(archive.size_bytes)}</p>
              </div>
            </div>
          ))}
          {policyMutation.error ? <p className="text-sm text-danger">{policyMutation.error.message}</p> : null}
          {archiveMutation.error ? <p className="text-sm text-danger">{archiveMutation.error.message}</p> : null}
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
