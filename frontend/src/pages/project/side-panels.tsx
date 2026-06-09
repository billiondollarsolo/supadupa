import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Eye, Pause, Play, RotateCcw, SlidersHorizontal } from "lucide-react";
import {
  auditProjectSecretCopy,
  listStackReleases,
  pauseProject,
  restartProject,
  resumeProject,
  revealProjectSecret,
  rotateProjectSecret,
  scaleProject,
  upgradeProject,
} from "../../api";
import { Modal } from "../../components/modal";
import { AppPanel } from "../../components/app/app-panel";
import { Badge } from "../../components/ui/badge";
import { CollapsibleCard } from "../../components/ui/collapsible-card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import { useUIStore } from "../../lib/ui-store";
import type { Tone } from "../../lib/status";
import { formatDateTime, formatTime } from "../../lib/format";
import type { Project, ProjectSecret } from "../../types";

type LifecycleConfirm = "pause" | "resume" | "restart" | "upgrade" | "scale";

export function LifecyclePanel({ orgId, project }: { orgId: string; project?: Project }) {
  const queryClient = useQueryClient();
  const addToast = useUIStore((state) => state.addToast);
  const [confirmAction, setConfirmAction] = useState<LifecycleConfirm | null>(null);
  const [version, setVersion] = useState("");
  const [tier, setTier] = useState("small");
  const projectRef = project?.ref ?? "";
  const stackReleasesQuery = useQuery({ queryKey: ["stack-releases"], queryFn: listStackReleases });
  const releaseVersions = stackReleasesQuery.data?.map((release) => release.version).filter(Boolean) ?? [];
  const selectedRelease = stackReleasesQuery.data?.find((release) => release.version === version);
  const tierKey = `${project?.ref ?? ""}:${project?.spec.resource_tier ?? ""}`;
  useEffect(() => {
    if (project) {
      setTier(project.spec.resource_tier || "small");
    }
  }, [tierKey]);
  useEffect(() => {
    if (!project) return;
    const nextVersion = releaseVersions.find((candidate) => candidate !== project.spec.stack_version) ?? project.spec.stack_version;
    setVersion((current) => {
      if (current && current !== project.spec.stack_version && (releaseVersions.length === 0 || releaseVersions.includes(current))) {
        return current;
      }
      return nextVersion;
    });
  }, [project?.ref, project?.spec.stack_version, releaseVersions.join("|")]);
  useEffect(() => setConfirmAction(null), [projectRef]);
  const invalidateProject = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["projects"] });
    void queryClient.invalidateQueries({ queryKey: ["project", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-logs", ref] });
    void queryClient.invalidateQueries({ queryKey: ["org-quota", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["org-usage", orgId] });
    void queryClient.invalidateQueries({ queryKey: ["hosts"] });
    void queryClient.invalidateQueries({ queryKey: ["fleet-metrics"] });
    void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
  };
  const pauseMutation = useMutation({
    mutationFn: pauseProject,
    onSuccess: (updated) => {
      setConfirmAction(null);
      invalidateProject(updated.ref);
    },
  });
  const resumeMutation = useMutation({
    mutationFn: resumeProject,
    onSuccess: (updated) => {
      setConfirmAction(null);
      invalidateProject(updated.ref);
    },
  });
  const restartMutation = useMutation({
    mutationFn: restartProject,
    onSuccess: (updated) => {
      setConfirmAction(null);
      invalidateProject(updated.ref);
    },
  });
  const upgradeMutation = useMutation({
    mutationFn: ({ ref, nextVersion }: { ref: string; nextVersion: string }) => upgradeProject(ref, nextVersion),
    onSuccess: (result) => {
      setConfirmAction(null);
      invalidateProject(result.project.ref);
      void queryClient.invalidateQueries({ queryKey: ["backups", result.project.ref] });
      void queryClient.invalidateQueries({ queryKey: ["recoverability", result.project.ref] });
      addToast({
        title: "Stack upgraded",
        detail: `${result.previous_version} -> ${result.target_version} with backup ${result.backup.id.slice(0, 12)}`,
      });
    },
  });
  const scaleMutation = useMutation({
    mutationFn: ({ ref, nextTier }: { ref: string; nextTier: string }) => scaleProject(ref, nextTier),
    onSuccess: (updated) => {
      setConfirmAction(null);
      invalidateProject(updated.ref);
    },
  });
  const busy = pauseMutation.isPending || resumeMutation.isPending || restartMutation.isPending || upgradeMutation.isPending || scaleMutation.isPending;
  const lastUpgrade = upgradeMutation.data?.project.ref === project?.ref ? upgradeMutation.data : undefined;
  const activeConfirm = confirmAction ? lifecycleConfirmCopy(confirmAction, project, version, tier) : null;
  const confirmPending =
    confirmAction === "pause" ? pauseMutation.isPending :
      confirmAction === "resume" ? resumeMutation.isPending :
        confirmAction === "restart" ? restartMutation.isPending :
          confirmAction === "upgrade" ? upgradeMutation.isPending :
            confirmAction === "scale" ? scaleMutation.isPending :
              false;

  function runConfirmedAction() {
    if (!project || !confirmAction) return;
    switch (confirmAction) {
      case "pause":
        pauseMutation.mutate(project.ref);
        break;
      case "resume":
        resumeMutation.mutate(project.ref);
        break;
      case "restart":
        restartMutation.mutate(project.ref);
        break;
      case "upgrade":
        upgradeMutation.mutate({ ref: project.ref, nextVersion: version });
        break;
      case "scale":
        scaleMutation.mutate({ ref: project.ref, nextTier: tier });
        break;
    }
  }

  return (
    <>
      <AppPanel eyebrow="Lifecycle" title="Runtime actions">
        {!project ? (
          <p className="mt-4 text-sm text-muted">Select a project to manage lifecycle actions.</p>
        ) : (
          <div className="mt-4 grid gap-3">
            <div className="grid grid-cols-3 gap-2">
              <Button disabled={busy || project.status === "paused"} onClick={() => setConfirmAction("pause")} type="button" variant="secondary">
                <Pause size={14} />
                Pause
              </Button>
              <Button disabled={busy || project.status === "healthy"} onClick={() => setConfirmAction("resume")} type="button" variant="secondary">
                <Play size={14} />
                Resume
              </Button>
              <Button disabled={busy} onClick={() => setConfirmAction("restart")} type="button" variant="secondary">
                <RotateCcw size={14} />
                Restart
              </Button>
            </div>
            <div className="grid gap-2 rounded-md border border-border bg-bg p-3">
              <p className="text-sm text-muted">Upgrade stack version from <span className="font-mono text-text">{project.spec.stack_version}</span>.</p>
              <div className="flex gap-2 max-sm:flex-col">
                {releaseVersions.length > 0 ? (
                  <NativeSelect className="font-mono" disabled={stackReleasesQuery.isPending} value={version} onChange={(event) => setVersion(event.target.value)}>
                    {releaseVersions.map((releaseVersion) => (
                      <option key={releaseVersion} value={releaseVersion}>
                        {releaseVersion}{releaseVersion === project.spec.stack_version ? " (current)" : ""}
                      </option>
                    ))}
                  </NativeSelect>
                ) : (
                  <Input className="font-mono" value={version} onChange={(event) => setVersion(event.target.value)} />
                )}
                <Button disabled={busy || version.trim().length === 0 || version === project.spec.stack_version} onClick={() => setConfirmAction("upgrade")} type="button" variant="secondary">
                  <RotateCcw size={14} />
                  Upgrade
                </Button>
              </div>
              {selectedRelease ? (
                <p className="text-xs text-muted">Postgres {selectedRelease.postgres} · API {selectedRelease.rest} · Auth {selectedRelease.auth}</p>
              ) : null}
              {lastUpgrade ? (
                <div className="grid gap-2 rounded-md border border-border bg-panel p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="min-w-0">
                      <p className="label">Last upgrade</p>
                      <p className="truncate text-sm text-muted">
                        {lastUpgrade.previous_version} to {lastUpgrade.target_version}
                      </p>
                    </div>
                    <StatusPill tone={lastUpgrade.rollback_available ? "success" : "warning"} label={lastUpgrade.rollback_available ? "rollback ready" : "rollback unavailable"} />
                  </div>
                  <div className="grid gap-1 text-xs text-muted">
                    <p className="truncate">Pre-upgrade backup <span className="font-mono text-text">{lastUpgrade.backup.id}</span></p>
                    <p>{lastUpgrade.backup.finished_at ? `Finished ${formatDateTime(lastUpgrade.backup.finished_at)}` : "Backup completion time pending"} · {lastUpgrade.backup.remote_location ? "remote artifact" : "local artifact"}</p>
                  </div>
                </div>
              ) : null}
            </div>
            <div className="grid gap-2 rounded-md border border-border bg-bg p-3">
              <p className="text-sm text-muted">Resize resource tier from <span className="font-mono text-text">{project.spec.resource_tier}</span>.</p>
              <div className="flex gap-2 max-sm:flex-col">
                <NativeSelect value={tier} onChange={(event) => setTier(event.target.value)}>
                  <option value="small">Small</option>
                  <option value="medium">Medium</option>
                  <option value="large">Large</option>
                </NativeSelect>
                <Button disabled={busy || tier === project.spec.resource_tier} onClick={() => setConfirmAction("scale")} type="button" variant="secondary">
                  <SlidersHorizontal size={14} />
                  Scale
                </Button>
              </div>
            </div>
            {stackReleasesQuery.error ? <p className="text-sm text-danger">{stackReleasesQuery.error.message}</p> : null}
            {[pauseMutation, resumeMutation, restartMutation, upgradeMutation, scaleMutation].map((mutation, index) =>
              mutation.error ? (
                <p className="text-sm text-danger" key={index}>
                  {mutation.error.message}
                </p>
              ) : null,
            )}
          </div>
        )}
      </AppPanel>
      <Modal
        description={activeConfirm?.description}
        onClose={() => !confirmPending && setConfirmAction(null)}
        open={Boolean(activeConfirm)}
        title={activeConfirm?.title ?? "Confirm action"}
        footer={(
          <>
            <Button disabled={confirmPending} onClick={() => setConfirmAction(null)} type="button" variant="secondary">Cancel</Button>
            <Button disabled={confirmPending} onClick={runConfirmedAction} type="button" variant={activeConfirm?.tone === "danger" ? "danger" : "default"}>
              {confirmPending ? "Working..." : activeConfirm?.confirmLabel ?? "Confirm"}
            </Button>
          </>
        )}
      >
        {activeConfirm ? <p className="text-sm text-muted">{activeConfirm.body}</p> : null}
      </Modal>
    </>
  );
}

function lifecycleConfirmCopy(action: LifecycleConfirm, project: Project | undefined, version: string, tier: string) {
  const name = project?.name ?? "this project";
  switch (action) {
    case "pause":
      return {
        title: `Pause ${name}?`,
        description: "This stops the project stack until it is resumed.",
        body: "API requests, database connections, functions, storage, and realtime traffic may fail while the project is paused.",
        confirmLabel: "Pause project",
        tone: "danger" as const,
      };
    case "resume":
      return {
        title: `Resume ${name}?`,
        description: "This starts the project stack again.",
        body: "Services may take a short time to become healthy after containers start.",
        confirmLabel: "Resume project",
        tone: "default" as const,
      };
    case "restart":
      return {
        title: `Restart ${name}?`,
        description: "This restarts runtime services for the project.",
        body: "Existing connections may be interrupted while the stack restarts.",
        confirmLabel: "Restart project",
        tone: "danger" as const,
      };
    case "upgrade":
      return {
        title: `Upgrade ${name}?`,
        description: `This changes the stack version to ${version}.`,
        body: "Supadupa creates a pre-upgrade backup, applies the target stable stack, and records rollback metadata before marking the upgrade complete. Production deployments can require that backup to be verified on a durable off-host target before the stack changes.",
        confirmLabel: "Upgrade stack",
        tone: "danger" as const,
      };
    case "scale":
      return {
        title: `Scale ${name}?`,
        description: `This changes the resource tier to ${tier}.`,
        body: "Scaling may change host capacity usage and can recreate runtime resources on some provisioners.",
        confirmLabel: "Scale project",
        tone: "danger" as const,
      };
  }
}

export function RuntimeStatusPanel({ project, defaultOpen = false }: { project?: Project; defaultOpen?: boolean }) {
  const runtime = project?.runtime_status;
  const phase = runtime?.phase ?? project?.status ?? "unknown";
  const message = runtime?.message || project?.message || "Runtime status has not been sampled yet.";
  const drift = message.toLowerCase().includes("drift") || phase === "degraded";
  const services = runtime?.services ?? [];
  const runningServices = services.filter((service) => {
    const state = (service.state || "").toLowerCase();
    return state === "running" || state === "rendered";
  }).length;
  const summary = services.length
    ? `${runningServices}/${services.length} services running`
    : runtime
      ? "Provisioner sample recorded"
      : "Awaiting first sample";

  return (
    <CollapsibleCard
      actions={<StatusPill status={phase} />}
      defaultOpen={defaultOpen}
      description={summary}
      eyebrow="Runtime"
      title="Reconciliation status"
    >
      {!project ? (
        <p className="text-sm text-muted">Select a project to inspect runtime state.</p>
      ) : (
        <div className="grid gap-3">
          <div className="flex items-center justify-between gap-3 rounded-md border border-border bg-bg p-3">
            <div className="min-w-0">
              <p className="label">Control plane</p>
              <p className="truncate text-sm text-muted">{project.message || "Desired state recorded"}</p>
            </div>
            <StatusPill status={project.status} />
          </div>
          <div className={`rounded-md border p-3 ${drift ? "border-warning/60 bg-warning/5" : "border-border bg-bg"}`}>
            <div className="flex items-center justify-between gap-3">
              <div className="min-w-0">
                <p className="label">Provisioner sample</p>
                <p className="truncate text-sm text-muted">{runtime ? message : "Waiting for project detail sample"}</p>
              </div>
              <StatusPill status={phase} />
            </div>
            {drift ? <p className="mt-2 text-xs text-warning">Drift detected. Reconcile should converge actual runtime back to desired state.</p> : null}
          </div>
          {runtime?.services?.length ? (
            <div className="rounded-md border border-border bg-bg p-3">
              <div className="mb-2 flex items-center justify-between gap-3">
                <p className="label">Compose services</p>
                <span className="text-xs text-faint">{runtime.services.filter((service) => service.desired).length} desired</span>
              </div>
              <div className="grid gap-1">
                {runtime.services.map((service) => (
                  <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 rounded-md border border-border bg-surface px-2 py-1.5" key={service.compose_service}>
                    <div className="min-w-0">
                      <p className="truncate text-xs font-medium">{service.name}</p>
                      <p className="truncate font-mono text-[11px] text-faint">{service.compose_service}{service.message ? ` · ${service.message}` : ""}</p>
                    </div>
                    <div className="flex items-center gap-1">
                      {!service.desired ? <Badge variant="muted">disabled</Badge> : null}
                      {service.health ? <StatusPill tone={service.health === "healthy" ? "success" : "warning"} label={service.health} /> : null}
                      <StatusPill tone={runtimeServiceTone(service.state)} label={service.state || "unknown"} />
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ) : null}
        </div>
      )}
    </CollapsibleCard>
  );
}

function runtimeServiceTone(state: string): Tone {
  const normalized = state.toLowerCase();
  if (normalized === "running" || normalized === "rendered") return "success";
  if (normalized === "missing" || normalized === "exited" || normalized === "dead") return "danger";
  return "warning";
}

export function SecretsPanel({ project, secrets, loading }: { project?: Project; secrets: ProjectSecret[]; loading: boolean }) {
  const queryClient = useQueryClient();
  const addToast = useUIStore((state) => state.addToast);
  const [revealed, setRevealed] = useState<Record<string, string>>({});
  const revealMutation = useMutation({
    mutationFn: ({ ref, kind }: { ref: string; kind: string }) => revealProjectSecret(ref, kind),
    onSuccess: (payload, variables) => {
      setRevealed((current) => ({ ...current, [payload.kind]: payload.value }));
      addToast({ title: "Secret revealed", detail: payload.kind, kind: "warning" });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
    },
  });
  const copyMutation = useMutation({
    mutationFn: async ({ ref, kind, value }: { ref: string; kind: string; value: string }) => {
      await auditProjectSecretCopy(ref, kind);
      await navigator.clipboard?.writeText(value);
    },
    onSuccess: (_payload, variables) => {
      addToast({ title: "Copied secret", detail: variables.kind, kind: "warning" });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
    },
  });
  const rotateMutation = useMutation({
    mutationFn: ({ ref, kind }: { ref: string; kind: string }) => rotateProjectSecret(ref, kind),
    onSuccess: (payload, variables) => {
      setRevealed((current) => {
        const next = { ...current };
        delete next[payload.kind];
        return next;
      });
      void queryClient.invalidateQueries({ queryKey: ["project-secrets", variables.ref] });
      void queryClient.invalidateQueries({ queryKey: ["audit-events"] });
      void queryClient.invalidateQueries({ queryKey: ["project-logs", variables.ref] });
      addToast({ title: "Secret rotated", detail: payload.kind });
    },
  });

  return (
    <AppPanel eyebrow="Secrets" title="Keys and credentials">
      <div className="mt-4 grid gap-2">
        {loading ? <p className="text-sm text-muted">Loading secrets...</p> : null}
        {!loading && secrets.length === 0 ? <p className="text-sm text-muted">No generated secrets yet.</p> : null}
        {secrets.map((secret) => {
          const revealedValue = revealed[secret.kind];
          const value = revealedValue ?? secret.masked;
          return (
            <div className="secret-row" key={secret.id}>
              <div className="min-w-0">
                <p className="label">{secret.kind}</p>
                <p className="truncate font-mono text-xs text-muted">{value}</p>
                {secret.rotated_at ? <p className="mt-1 text-xs text-faint">Rotated {formatTime(secret.rotated_at)}</p> : null}
              </div>
              <div className="flex gap-2">
                <Button disabled={!project || revealMutation.isPending} onClick={() => project && revealMutation.mutate({ ref: project.ref, kind: secret.kind })} size="icon" type="button" variant="ghost">
                  <Eye size={14} />
                </Button>
                <Button disabled={!project || copyMutation.isPending || !revealedValue} onClick={() => project && revealedValue && copyMutation.mutate({ ref: project.ref, kind: secret.kind, value: revealedValue })} size="icon" type="button" variant="ghost">
                  <Copy size={14} />
                </Button>
                <Button disabled={!project || rotateMutation.isPending} onClick={() => project && rotateMutation.mutate({ ref: project.ref, kind: secret.kind })} size="icon" type="button" variant="ghost">
                  <RotateCcw size={14} />
                </Button>
              </div>
            </div>
          );
        })}
        {revealMutation.error ? <p className="text-sm text-danger">{revealMutation.error.message}</p> : null}
        {copyMutation.error ? <p className="text-sm text-danger">{copyMutation.error.message}</p> : null}
        {rotateMutation.error ? <p className="text-sm text-danger">{rotateMutation.error.message}</p> : null}
      </div>
    </AppPanel>
  );
}
