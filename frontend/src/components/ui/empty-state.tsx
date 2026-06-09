import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../../lib/cn";

// A guided empty state: says what's missing and what to do about it, instead of
// a flat "No X configured." sentence. Pass `action` to surface the primary CTA
// at the point the user discovers there's nothing here yet.
export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  className,
}: {
  icon?: LucideIcon;
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "grid place-items-center gap-2 rounded-md border border-dashed border-border bg-bg px-4 py-8 text-center",
        className,
      )}
    >
      {Icon ? <Icon size={20} className="text-faint" /> : null}
      <p className="text-sm font-medium text-text">{title}</p>
      {description ? <p className="max-w-sm text-xs text-muted">{description}</p> : null}
      {action ? <div className="mt-1">{action}</div> : null}
    </div>
  );
}
