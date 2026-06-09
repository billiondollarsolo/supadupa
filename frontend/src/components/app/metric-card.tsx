import type { ReactNode } from "react";
import { cn } from "../../lib/cn";

type Tone = "default" | "success" | "warning" | "danger";

export function MetricCard({
  className,
  detail,
  label,
  tone = "default",
  value,
}: {
  label: string;
  value: ReactNode;
  detail?: ReactNode;
  tone?: Tone;
  className?: string;
}) {
  return (
    <div className={cn("min-w-0 rounded-md border border-border bg-bg p-2.5", className)}>
      <p className="label">{label}</p>
      <p className={cn("truncate text-sm font-medium", toneClass(tone))}>{value}</p>
      {detail ? <p className="mt-1 truncate text-xs text-faint">{detail}</p> : null}
    </div>
  );
}

export function toneClass(tone: Tone) {
  switch (tone) {
    case "success":
      return "text-success";
    case "warning":
      return "text-warning";
    case "danger":
      return "text-danger";
    default:
      return "";
  }
}
