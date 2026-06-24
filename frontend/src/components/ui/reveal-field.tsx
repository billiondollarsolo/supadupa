import { useState } from "react";
import { Check, Copy, Eye, EyeOff, LoaderCircle } from "lucide-react";
import { cn } from "../../lib/cn";

// A copyable value that is masked by default when `sensitive`. Reveal is an
// explicit, deliberate action, and copy runs an optional `onCopy` hook first so
// callers can record an audit event for high-value credentials.
export function RevealField({
  label,
  value,
  hint,
  sensitive = true,
  monospace = true,
  onReveal,
  onCopy,
  className,
}: {
  label: string;
  value: string;
  hint?: string;
  sensitive?: boolean;
  monospace?: boolean;
  onReveal?: () => Promise<string> | string;
  onCopy?: () => void | Promise<void>;
  className?: string;
}) {
  const [revealed, setRevealed] = useState(!sensitive);
  const [resolvedValue, setResolvedValue] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const shown = revealed || !sensitive;
  const currentValue = resolvedValue ?? value;
  const display = shown ? currentValue : mask(currentValue);

  async function materializeValue() {
    if (!onReveal || resolvedValue !== null) {
      return resolvedValue ?? value;
    }
    setBusy(true);
    setError("");
    try {
      const next = await onReveal();
      setResolvedValue(next);
      return next;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to reveal value");
      throw err;
    } finally {
      setBusy(false);
    }
  }

  async function toggleReveal() {
    if (revealed) {
      setRevealed(false);
      return;
    }
    try {
      await materializeValue();
      setRevealed(true);
    } catch {
      // Error text is rendered below the field.
    }
  }

  async function copy() {
    // The clipboard write is the user's intent and must always happen — never
    // let a best-effort audit call (which can fail, e.g. for non-managed kinds)
    // block it.
    let copyValue = currentValue;
    try {
      copyValue = await materializeValue();
    } catch {
      return;
    }
    try {
      await navigator.clipboard?.writeText(copyValue);
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
              disabled={busy}
              onClick={() => void toggleReveal()}
              title={revealed ? "Hide value" : "Reveal value"}
              type="button"
            >
              {busy ? <LoaderCircle className="animate-spin" size={13} /> : revealed ? <EyeOff size={13} /> : <Eye size={13} />}
            </button>
          ) : null}
          <button className="icon-button h-7 min-h-7 min-w-7" disabled={busy} onClick={() => void copy()} title="Copy value" type="button">
            {busy ? <LoaderCircle className="animate-spin" size={13} /> : copied ? <Check size={13} /> : <Copy size={13} />}
          </button>
        </div>
      </div>
      <code className={cn("block truncate rounded-md border border-border bg-bg px-2 py-1.5 text-xs text-text", monospace ? "font-mono" : "")}>
        {display}
      </code>
      {hint ? <span className="text-xs text-faint">{hint}</span> : null}
      {error ? <span className="text-xs text-danger">{error}</span> : null}
    </div>
  );
}

function mask(value: string) {
  if (!value) return "";
  const visible = value.length > 12 ? 4 : 0;
  return `${"•".repeat(Math.min(24, Math.max(8, value.length - visible)))}${visible ? value.slice(-visible) : ""}`;
}
