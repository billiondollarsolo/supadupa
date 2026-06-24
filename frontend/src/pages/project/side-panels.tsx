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
	  type ProjectResourcesInput,
	  upgradeProject,
	} from "../../api";
import { Modal } from "../../components/modal";
import { AppPanel } from "../../components/app/app-panel";
import { Badge } from "../../components/ui/badge";
import { CollapsibleCard } from "../../components/ui/collapsible-card";
import { Button } from "../../components/ui/button";
import { Field } from "../../components/ui/field";
import { Input } from "../../components/ui/input";
import { NativeSelect } from "../../components/ui/native-select";
import { StatusPill } from "../../components/ui/status-pill";
import { useUIStore } from "../../lib/ui-store";
import type { Tone } from "../../lib/status";
import { formatBytes, formatDateTime, formatTime } from "../../lib/format";
import type { Project, ProjectSecret } from "../../types";

type LifecycleConfirm = "pause" | "resume" | "restart" | "upgrade" | "scale";
type ScaleDraft = ProjectResourcesInput;
type StackProfile = "essential" | "full" | "orioledb";

const SIZING_BOUNDS = { maxCpu: 64, minRamMB: 256, maxRamMB: 262144, maxDiskGB: 16384 } as const;
const RECOMMENDATION_HEADROOM = { cpuPercent: 20, ramPercent: 25, diskPercent: 20, ramStepMB: 256, diskStepGB: 5 } as const;
const PROJECT_SERVICE_KEYS = ["auth", "rest", "graphql", "realtime", "storage", "imgproxy", "functions", "pooler", "studio", "analytics", "vector"] as const;
const ESSENTIAL_OFF = new Set(["graphql", "imgproxy", "analytics", "vector"]);

export function LifecyclePanel({ orgId, project }: { orgId: string; project?: Project }) {
  const queryClient = useQueryClient();
	  const addToast = useUIStore((state) => state.addToast);
	  const [confirmAction, setConfirmAction] = useState<LifecycleConfirm | null>(null);
	  const [version, setVersion] = useState("");
	  const [scaleDraft, setScaleDraft] = useState<ScaleDraft>(() => projectResourceDraft());
	  const projectRef = project?.ref ?? "";
  const stackReleasesQuery = useQuery({ queryKey: ["stack-releases"], queryFn: listStackReleases });
  const releaseVersions = stackReleasesQuery.data?.map((release) => release.version).filter(Boolean) ?? [];
  const upgradeVersions = project
    ? releaseVersions
      .filter((candidate) => compareStackVersions(candidate, project.spec.stack_version) > 0)
      .sort((a, b) => compareStackVersions(b, a))
    : [];
  const selectedRelease = stackReleasesQuery.data?.find((release) => release.version === version);
	  const scaleKey = `${project?.ref ?? ""}:${project?.spec.cpu ?? ""}:${project?.spec.ram_mb ?? ""}:${project?.spec.disk_gb ?? ""}:${project?.spec.enforce_limits ?? ""}`;
	  useEffect(() => {
	    setScaleDraft(projectResourceDraft(project));
	  }, [scaleKey]);
  useEffect(() => {
    if (!project) return;
    const nextVersion = upgradeVersions[0] ?? "";
    setVersion((current) => {
      if (current && upgradeVersions.includes(current)) {
        return current;
      }
      return nextVersion;
    });
  }, [project?.ref, project?.spec.stack_version, upgradeVersions.join("|")]);
  useEffect(() => setConfirmAction(null), [projectRef]);
  const invalidateProject = (ref: string) => {
    void queryClient.invalidateQueries({ queryKey: ["projects"] });
    void queryClient.invalidateQueries({ queryKey: ["project", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-metrics", ref] });
    void queryClient.invalidateQueries({ queryKey: ["project-telemetry-history", ref] });
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
	    mutationFn: ({ ref, input }: { ref: string; input: ProjectResourcesInput }) => scaleProject(ref, input),
	    onSuccess: (updated) => {
	      setConfirmAction(null);
	      invalidateProject(updated.ref);
	    },
	  });
	  const busy = pauseMutation.isPending || resumeMutation.isPending || restartMutation.isPending || upgradeMutation.isPending || scaleMutation.isPending;
	  const lastUpgrade = upgradeMutation.data?.project.ref === project?.ref ? upgradeMutation.data : undefined;
	  const currentResources = projectResourceDraft(project);
	  const scaleChanged = !resourcesEqual(scaleDraft, currentResources);
	  const scaleValid =
	    scaleDraft.cpu >= 1 && scaleDraft.cpu <= SIZING_BOUNDS.maxCpu &&
	    scaleDraft.ram_mb >= SIZING_BOUNDS.minRamMB && scaleDraft.ram_mb <= SIZING_BOUNDS.maxRamMB &&
	    scaleDraft.disk_gb >= 1 && scaleDraft.disk_gb <= SIZING_BOUNDS.maxDiskGB;
	  const scaleServices = projectServiceStates(project);
	  const scaleMinimum = minimumReservation(project?.spec.profile, scaleServices);
	  const scaleRecommendation = recommendedReservation(project?.spec.profile, scaleServices);
	  const scaleBelowRecommendation = scaleDraft.cpu < scaleRecommendation.cpu || scaleDraft.ram_mb < scaleRecommendation.ram_mb || scaleDraft.disk_gb < scaleRecommendation.disk_gb;
	  const activeConfirm = confirmAction ? lifecycleConfirmCopy(confirmAction, project, version, scaleDraft) : null;
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
	        scaleMutation.mutate({ ref: project.ref, input: scaleDraft });
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
                  <NativeSelect className="font-mono" disabled={stackReleasesQuery.isPending || upgradeVersions.length === 0} value={version} onChange={(event) => setVersion(event.target.value)}>
                    {upgradeVersions.length === 0 ? <option value="">No newer versions available</option> : null}
                    {upgradeVersions.map((releaseVersion) => <option key={releaseVersion} value={releaseVersion}>{releaseVersion}</option>)}
                  </NativeSelect>
                ) : (
                  <Input className="font-mono" value={version} onChange={(event) => setVersion(event.target.value)} />
                )}
                <Button disabled={busy || version.trim().length === 0 || compareStackVersions(version, project.spec.stack_version) <= 0} onClick={() => setConfirmAction("upgrade")} type="button" variant="secondary">
                  <RotateCcw size={14} />
                  Upgrade
                </Button>
              </div>
              {releaseVersions.length > 0 && upgradeVersions.length === 0 ? <p className="text-xs text-muted">This project is already on the newest supported stack release.</p> : null}
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
		              <div className="flex items-start justify-between gap-3">
		                <div>
		                  <p className="text-sm font-medium">Resize resources</p>
		                  <p className="mt-1 text-xs text-muted">Change reserved CPU, RAM, and disk for this project.</p>
		                </div>
		                <StatusPill tone={scaleDraft.enforce_limits ? "neutral" : "warning"} label={scaleDraft.enforce_limits ? "limits on" : "no limits"} />
		              </div>
		              <div className="flex items-start justify-between gap-3 max-md:flex-col">
		                <div>
		                  <p className="label">Recommended size</p>
		                  <p className="mt-1 text-xs leading-5 text-muted">{scaleRecommendation.cpu} vCPU · {formatBytes(scaleRecommendation.ram_mb * 1024 * 1024)} RAM · {scaleRecommendation.disk_gb} GB disk with operating headroom.</p>
		                  <p className="mt-1 text-xs leading-5 text-faint">Minimum: {scaleMinimum.cpu} vCPU · {formatBytes(scaleMinimum.ram_mb * 1024 * 1024)} RAM · {scaleMinimum.disk_gb} GB disk.</p>
		                </div>
		                <div className="flex shrink-0 gap-2 max-sm:flex-col">
		                  <Button size="sm" type="button" variant="secondary" onClick={() => setScaleDraft((current) => applyScaleSizing(current, scaleRecommendation))}>
		                    Use recommended
		                  </Button>
		                  <Button size="sm" type="button" variant="secondary" onClick={() => setScaleDraft((current) => applyScaleSizing(current, scaleMinimum))}>
		                    Use minimum
		                  </Button>
		                </div>
		              </div>
		              <div className="grid grid-cols-3 gap-2 max-md:grid-cols-1">
		                <Field label="CPU" hint={`1 – ${SIZING_BOUNDS.maxCpu} cores`}>
		                  <Input className="font-mono" type="number" min={1} max={SIZING_BOUNDS.maxCpu} value={scaleDraft.cpu} onChange={(event) => setScaleDraft({ ...scaleDraft, cpu: Number(event.target.value) })} />
	                </Field>
	                <Field label="RAM" hint="MB">
	                  <Input className="font-mono" type="number" min={SIZING_BOUNDS.minRamMB} max={SIZING_BOUNDS.maxRamMB} step={256} value={scaleDraft.ram_mb} onChange={(event) => setScaleDraft({ ...scaleDraft, ram_mb: Number(event.target.value) })} />
	                </Field>
	                <Field label="Disk" hint="GB">
	                  <Input className="font-mono" type="number" min={1} max={SIZING_BOUNDS.maxDiskGB} value={scaleDraft.disk_gb} onChange={(event) => setScaleDraft({ ...scaleDraft, disk_gb: Number(event.target.value) })} />
	                </Field>
	              </div>
	              <div className="grid grid-cols-2 gap-2 max-md:grid-cols-1">
	                <button className={scaleDraft.enforce_limits ? "choice active" : "choice"} type="button" onClick={() => setScaleDraft({ ...scaleDraft, enforce_limits: true })}>
	                  <span className="text-sm font-medium">Enforce limits</span>
	                  <span className="text-xs leading-5 text-muted">Distribute {scaleDraft.cpu} vCPU and {formatBytes(scaleDraft.ram_mb * 1024 * 1024)} RAM limits across enabled service containers.</span>
	                </button>
	                <button className={!scaleDraft.enforce_limits ? "choice active" : "choice"} type="button" onClick={() => setScaleDraft({ ...scaleDraft, enforce_limits: false })}>
	                  <span className="text-sm font-medium">No limits</span>
	                  <span className="text-xs leading-5 text-muted">Allow project containers to grow past these CPU/RAM values and contend for host resources.</span>
	                </button>
		              </div>
		              {!scaleDraft.enforce_limits ? <p className="text-xs text-warning">No limits is intentional: this can help bursty growth, but a busy project can consume the host and affect other workloads.</p> : null}
		              {!scaleValid ? <p className="text-xs text-danger">Sizing is out of range. CPU 1–{SIZING_BOUNDS.maxCpu}, RAM {SIZING_BOUNDS.minRamMB}–{SIZING_BOUNDS.maxRamMB} MB, disk 1–{SIZING_BOUNDS.maxDiskGB} GB.</p> : null}
		              {scaleBelowRecommendation ? <p className="text-xs text-warning">Below the recommended size. The minimum is a startup floor; enforced limits can become unstable under real traffic.</p> : null}
		              <Button className="justify-center" disabled={busy || !scaleChanged || !scaleValid} onClick={() => setConfirmAction("scale")} type="button" variant="secondary">
	                <SlidersHorizontal size={14} />
	                Apply resize
	              </Button>
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

function lifecycleConfirmCopy(action: LifecycleConfirm, project: Project | undefined, version: string, resources: ScaleDraft) {
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
	        description: `This changes resources to ${formatResourceDraft(resources)}.`,
	        body: resources.enforce_limits
	          ? "Compose will rewrite the runtime manifest with per-service CPU/RAM limits and may recreate stack containers. Kubernetes deployments render matching per-container requests/limits and DB volume sizing."
	          : "CPU/RAM limits will be removed. The selected values remain reserved/accounted, but project containers can consume more host resources when they are busy.",
	        confirmLabel: "Scale project",
	        tone: "danger" as const,
	      };
	  }
	}

function projectServiceStates(project?: Project): Record<string, boolean> {
  const states = servicesForProfile(project?.spec.profile);
  const services = project?.spec.services;
  if (!services) return states;
  for (const key of PROJECT_SERVICE_KEYS) {
    const service = services[key];
    if (service) states[key] = Boolean(service.enabled);
  }
  return states;
}

function servicesForProfile(profile?: string): Record<string, boolean> {
  const normalized = normalizeStackProfile(profile);
  const out: Record<string, boolean> = {};
  for (const key of PROJECT_SERVICE_KEYS) out[key] = normalized === "essential" ? !ESSENTIAL_OFF.has(key) : true;
  return out;
}

function normalizeStackProfile(profile?: string): StackProfile {
  return profile === "essential" || profile === "orioledb" ? profile : "full";
}

function minimumReservation(profile: string | undefined, services: Record<string, boolean>) {
  let ramMB = 2048;
  let cpuUnits = 100;
  let diskGB = 20;
  if (services.auth) {
    ramMB += 256;
    cpuUnits += 20;
  }
  if (services.rest) {
    ramMB += 256;
    cpuUnits += 20;
  }
  if (services.realtime) {
    ramMB += 512;
    cpuUnits += 30;
  }
  if (services.storage) {
    ramMB += 512;
    cpuUnits += 30;
    diskGB += 20;
  }
  if (services.imgproxy) {
    ramMB += 256;
    cpuUnits += 20;
  }
  if (services.functions) {
    ramMB += 512;
    cpuUnits += 20;
  }
  if (services.pooler) {
    ramMB += 256;
    cpuUnits += 10;
  }
  if (services.studio) {
    ramMB += 512;
    cpuUnits += 10;
  }
  if (services.analytics) {
    ramMB += 1024;
    cpuUnits += 30;
    diskGB += 10;
  }
  if (services.vector) {
    ramMB += 256;
    cpuUnits += 10;
  }
  if (services.graphql) {
    ramMB += 128;
    cpuUnits += 10;
  }
  if (normalizeStackProfile(profile) === "orioledb") {
    ramMB += 1024;
    cpuUnits += 50;
    diskGB += 20;
  }
  if (ramMB % 512 !== 0) {
    ramMB = (Math.floor(ramMB / 512) + 1) * 512;
  }
  return { cpu: Math.max(1, Math.ceil(cpuUnits / 100)), ram_mb: ramMB, disk_gb: Math.max(20, diskGB) };
}

function recommendedReservation(profile: string | undefined, services: Record<string, boolean>) {
  const minimum = minimumReservation(profile, services);
  return {
    cpu: clampNumber(addPercentRoundUp(minimum.cpu, RECOMMENDATION_HEADROOM.cpuPercent), 1, SIZING_BOUNDS.maxCpu),
    ram_mb: clampNumber(roundUpNumber(addPercentRoundUp(minimum.ram_mb, RECOMMENDATION_HEADROOM.ramPercent), RECOMMENDATION_HEADROOM.ramStepMB), SIZING_BOUNDS.minRamMB, SIZING_BOUNDS.maxRamMB),
    disk_gb: clampNumber(roundUpNumber(addPercentRoundUp(minimum.disk_gb, RECOMMENDATION_HEADROOM.diskPercent), RECOMMENDATION_HEADROOM.diskStepGB), 1, SIZING_BOUNDS.maxDiskGB),
  };
}

function applyScaleSizing(current: ScaleDraft, sizing: { cpu: number; ram_mb: number; disk_gb: number }): ScaleDraft {
  return { ...current, cpu: sizing.cpu, ram_mb: sizing.ram_mb, disk_gb: sizing.disk_gb };
}

function addPercentRoundUp(value: number, percent: number) {
  return Math.ceil(value * (100 + percent) / 100);
}

function roundUpNumber(value: number, step: number) {
  return Math.ceil(value / step) * step;
}

function clampNumber(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function projectResourceDraft(project?: Project): ScaleDraft {
  const fallback = defaultResourceDraft();
  return {
    cpu: positiveNumber(project?.spec.cpu) || fallback.cpu,
    ram_mb: positiveNumber(project?.spec.ram_mb) || fallback.ram_mb,
    disk_gb: positiveNumber(project?.spec.disk_gb) || fallback.disk_gb,
    enforce_limits: Boolean(project?.spec.enforce_limits),
  };
}

function defaultResourceDraft() {
  return recommendedReservation("full", servicesForProfile("full"));
}

function positiveNumber(value?: number) {
  return typeof value === "number" && value > 0 ? value : 0;
}

function resourcesEqual(left: ScaleDraft, right: ScaleDraft) {
  return left.cpu === right.cpu &&
    left.ram_mb === right.ram_mb &&
    left.disk_gb === right.disk_gb &&
    left.enforce_limits === right.enforce_limits;
}

function formatResourceDraft(resources: ScaleDraft) {
  return `${resources.cpu} vCPU, ${formatBytes(resources.ram_mb * 1024 * 1024)} RAM, ${resources.disk_gb} GB disk, ${resources.enforce_limits ? "limits on" : "no limits"}`;
}

function compareStackVersions(left: string, right: string): number {
  const leftParts = stackVersionParts(left);
  const rightParts = stackVersionParts(right);
  const count = Math.max(leftParts.length, rightParts.length);
  for (let index = 0; index < count; index += 1) {
    const diff = (leftParts[index] ?? 0) - (rightParts[index] ?? 0);
    if (diff !== 0) {
      return diff;
    }
  }
  return left.localeCompare(right);
}

function stackVersionParts(version: string): number[] {
  return version
    .trim()
    .split(/[._-]/)
    .map((part) => Number(part.match(/^\d+/)?.[0] ?? 0));
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
  const [pendingRotateKind, setPendingRotateKind] = useState<string | null>(null);
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
      setPendingRotateKind(null);
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
      <Modal
        description="Rotating this value updates the project runtime secret and can invalidate existing clients, database URLs, or service credentials that still use the old value."
        footer={
          <>
            <Button disabled={rotateMutation.isPending} onClick={() => setPendingRotateKind(null)} type="button" variant="secondary">
              Cancel
            </Button>
            <Button
              disabled={!project || !pendingRotateKind || rotateMutation.isPending}
              onClick={() => project && pendingRotateKind && rotateMutation.mutate({ ref: project.ref, kind: pendingRotateKind })}
              type="button"
              variant="danger"
            >
              {rotateMutation.isPending ? "Rotating..." : "Rotate secret"}
            </Button>
          </>
        }
        onClose={() => !rotateMutation.isPending && setPendingRotateKind(null)}
        open={Boolean(pendingRotateKind)}
        title={`Rotate ${pendingRotateKind ?? "secret"}`}
      >
        <p className="text-sm text-muted">
          Update any apps, env files, scripts, or clients that use this value after rotation completes.
        </p>
      </Modal>
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
                <Button aria-label={`Reveal ${secret.kind}`} disabled={!project || revealMutation.isPending} onClick={() => project && revealMutation.mutate({ ref: project.ref, kind: secret.kind })} size="icon" title={`Reveal ${secret.kind}`} type="button" variant="ghost">
                  <Eye size={14} />
                </Button>
                <Button aria-label={`Copy ${secret.kind}`} disabled={!project || copyMutation.isPending || !revealedValue} onClick={() => project && revealedValue && copyMutation.mutate({ ref: project.ref, kind: secret.kind, value: revealedValue })} size="icon" title={`Copy ${secret.kind}`} type="button" variant="ghost">
                  <Copy size={14} />
                </Button>
                <Button aria-label={`Rotate ${secret.kind}`} disabled={!project || rotateMutation.isPending} onClick={() => setPendingRotateKind(secret.kind)} size="icon" title={`Rotate ${secret.kind}`} type="button" variant="ghost">
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
