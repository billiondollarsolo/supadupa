import { useState } from "react";
import { Check, Copy, Eye, EyeOff } from "lucide-react";
import { cn } from "../../lib/cn";

// A copyable value that is masked by default when `sensitive`. Reveal is an
// explicit, deliberate action, and copy runs an optional `onCopy` hook first so
// callers can record an audit event for high-value credentials before the value
// hits the clipboard.
export function RevealField({
  label,
  value,
  hint,
  sensitive = true,
  monospace = true,
  onCopy,
  className,
}: {
  label: string;
  value: string;
  hint?: string;
  sensitive?: boolean;
  monospace?: boolean;
  onCopy?: () => void | Promise<void>;
  className?: string;
}) {
  const [revealed, setRevealed] = useState(!sensitive);
  const [copied, setCopied] = useState(false);

  const shown = revealed || !sensitive;
  const display = shown ? value : mask(value);

  async function copy() {
    // The clipboard write is the user's intent and must always happen — never
    // let a best-effort audit call (which can fail, e.g. for non-managed kinds)
    // block it.
    try {
      await navigator.clipboard?.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard unavailable (insecure context); nothing actionable here.
    }
    try {
      await onCopy?.();
    } catch {
      // audit is best-effort; the copy already succeeded.
    }
  }

  return (
    <div className={cn("grid gap-1", className)}>
      <div className="flex items-center justify-between gap-2">
        <span className="label">{label}</span>
        <div className="flex items-center gap-1">
          {sensitive ? (
            <button
              className="icon-button h-7 min-h-7 min-w-7"
              onClick={() => setRevealed((v) => !v)}
              title={revealed ? "Hide value" : "Reveal value"}
              type="button"
            >
              {revealed ? <EyeOff size={13} /> : <Eye size={13} />}
            </button>
          ) : null}
          <button className="icon-button h-7 min-h-7 min-w-7" onClick={() => void copy()} title="Copy value" type="button">
            {copied ? <Check size={13} /> : <Copy size={13} />}
          </button>
        </div>
      </div>
      <code className={cn("block truncate rounded-md border border-border bg-bg px-2 py-1.5 text-xs text-text", monospace ? "font-mono" : "")}>
        {display}
      </code>
      {hint ? <span className="text-xs text-faint">{hint}</span> : null}
    </div>
  );
}

function mask(value: string) {
  if (!value) return "";
  const visible = value.length > 12 ? 4 : 0;
  return `${"•".repeat(Math.min(24, Math.max(8, value.length - visible)))}${visible ? value.slice(-visible) : ""}`;
}
