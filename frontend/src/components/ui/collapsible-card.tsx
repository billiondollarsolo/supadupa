import type { ReactNode } from "react";
import { ChevronDown } from "lucide-react";
import { cn } from "../../lib/cn";

// A Card-styled collapsible section (native <details>). Use for disclosures that
// previously used `<details className="panel">`, so they match AppPanel.
export function CollapsibleCard({
  eyebrow,
  title,
  description,
  actions,
  defaultOpen = false,
  className,
  children,
}: {
  eyebrow?: string;
  title?: string;
  description?: ReactNode;
  actions?: ReactNode;
  defaultOpen?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <details className={cn("group rounded-lg border border-border bg-surface shadow-sm", className)} open={defaultOpen}>
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 p-4 [&::-webkit-details-marker]:hidden">
        <div className="min-w-0">
          {eyebrow ? <p className="label">{eyebrow}</p> : null}
          {title ? <h2 className="mt-0.5 text-base font-medium">{title}</h2> : null}
          {description ? <p className="mt-1 text-xs text-muted">{description}</p> : null}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {actions}
          <ChevronDown className="text-faint transition-transform group-open:rotate-180" size={16} />
        </div>
      </summary>
      <div className="px-4 pb-4">{children}</div>
    </details>
  );
}
