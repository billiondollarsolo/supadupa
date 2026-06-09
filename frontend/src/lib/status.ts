// Canonical mapping from backend status strings to semantic UI tones.
//
// Before this existed, pages reused the amber `provisioning`/`warning`/`paused`
// pill class to mean "in progress", "needs attention", "disabled", "off",
// "optional", "public", "read-only" and "loading" — so intentional states read
// as warnings and some bad states (disabled) rendered green. Route every status
// pill through here so one vocabulary drives one set of colors everywhere.

export type Tone = "success" | "warning" | "danger" | "info" | "neutral";

// Genuinely healthy / done / on.
const SUCCESS = new Set([
  "healthy", "active", "ready", "completed", "complete", "verified", "passed",
  "pass", "live", "running", "enabled", "connected", "online", "ok", "success",
  "available", "scheduled", "sealed", "configured", "custom", "applied", "synced",
]);

// Needs a human / something is failing.
const DANGER = new Set([
  "error", "failed", "failure", "broken", "missing", "unreachable", "dead",
  "exited", "critical", "denied", "offline", "overcommitted", "expired", "revoked",
]);

// Attention but not (yet) failure.
const WARNING = new Set([
  "warning", "degraded", "near", "near_limit", "review", "needs_review",
  "pending_review", "stale", "expiring", "throttled", "at_limit",
]);

// In progress / transient.
const INFO = new Set([
  "provisioning", "loading", "connecting", "pending", "starting", "restarting",
  "upgrading", "scaling", "reconciling",
]);

// Intentionally off / not configured — the neutral state amber used to swallow.
const NEUTRAL = new Set([
  "paused", "idle", "disabled", "off", "unconfigured", "not_configured", "none",
  "default", "draft", "stopped", "unknown", "optional", "public", "read_only",
]);

export function statusTone(raw: string | null | undefined): Tone {
  if (!raw) return "neutral";
  const value = raw.toLowerCase().trim().replace(/[\s-]+/g, "_");
  if (SUCCESS.has(value)) return "success";
  if (DANGER.has(value)) return "danger";
  if (WARNING.has(value)) return "warning";
  if (INFO.has(value)) return "info";
  if (NEUTRAL.has(value)) return "neutral";
  return "neutral";
}

// Class string for the legacy `.pill` element family (used across all pages).
export function pillClass(tone: Tone): string {
  switch (tone) {
    case "success":
      return "pill healthy";
    case "warning":
      return "pill warning";
    case "danger":
      return "pill error";
    case "info":
      return "pill info";
    case "neutral":
    default:
      return "pill neutral";
  }
}

export function statusPillClass(raw: string | null | undefined): string {
  return pillClass(statusTone(raw));
}

// For the CVA `Badge` component variants.
export function badgeVariant(tone: Tone): "default" | "success" | "warning" | "danger" | "muted" {
  switch (tone) {
    case "success":
      return "success";
    case "warning":
      return "warning";
    case "danger":
      return "danger";
    case "info":
      return "default";
    case "neutral":
    default:
      return "muted";
  }
}
