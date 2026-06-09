import type { ReactNode } from "react";
import { cn } from "../../lib/cn";
import { pillClass, statusTone, type Tone } from "../../lib/status";

// One pill, one tone vocabulary. Pass a raw backend `status` to derive the tone,
// or an explicit `tone` when the semantic meaning isn't the literal status
// (e.g. an intentionally "public" client should be neutral, not a warning).
export function StatusPill({
  status,
  tone,
  label,
  className,
}: {
  status?: string | null;
  tone?: Tone;
  label?: ReactNode;
  className?: string;
}) {
  const resolved = tone ?? statusTone(status);
  return <span className={cn(pillClass(resolved), className)}>{label ?? status}</span>;
}
