import { Globe, ShieldAlert } from "lucide-react";
import { StatusPill } from "./ui/status-pill";

// Maps a project's db_ingress_mode to a badge. Returns null for private/unset so
// the badge only ever appears when a project's database is reachable from
// outside the platform — the operator should always see when that's the case.
export function dbExposureMeta(mode?: string) {
  switch ((mode ?? "private").toLowerCase()) {
    case "public":
      return { tone: "danger" as const, label: "DB public", Icon: Globe };
    case "allowlisted":
      return { tone: "warning" as const, label: "DB allowlisted", Icon: ShieldAlert };
    default:
      return null;
  }
}

export function DbExposureBadge({ mode, className }: { mode?: string; className?: string }) {
  const meta = dbExposureMeta(mode);
  if (!meta) return null;
  const { Icon, tone, label } = meta;
  return <StatusPill className={className} tone={tone} label={<span className="inline-flex items-center gap-1"><Icon size={11} />{label}</span>} />;
}
