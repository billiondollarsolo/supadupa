import type { ReactNode } from "react";
import { cn } from "../../lib/cn";

// Wraps a form control with a visible label + optional hint/unit so inputs stay
// meaningful after the user has typed (placeholders disappear; labels don't).
export function Field({
  label,
  hint,
  htmlFor,
  required,
  className,
  children,
}: {
  label: ReactNode;
  hint?: ReactNode;
  htmlFor?: string;
  required?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <label className={cn("grid gap-1", className)} htmlFor={htmlFor}>
      <span className="label flex items-center gap-1">
        {label}
        {required ? <span className="text-danger">*</span> : null}
      </span>
      {children}
      {hint ? <span className="text-xs text-faint">{hint}</span> : null}
    </label>
  );
}

// A labeled grouping inside a panel, for breaking "walls of equal-weight rows"
// into scannable clusters with their own heading and optional action.
export function SubSection({
  title,
  description,
  actions,
  className,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section className={cn("grid gap-2", className)}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="label">{title}</p>
          {description ? <p className="mt-0.5 text-xs text-muted">{description}</p> : null}
        </div>
        {actions ? <div className="shrink-0">{actions}</div> : null}
      </div>
      {children}
    </section>
  );
}
