import type { ReactNode } from "react";
import { cn } from "../../lib/cn";

export type SegmentedOption<T extends string> = {
  value: T;
  label: ReactNode;
};

// shadcn-style segmented control (a tabs-like toggle group). Replaces the
// hand-rolled `.segmented` button class so section navs and mode pickers share
// one consistent primitive.
export function Segmented<T extends string>({
  options,
  value,
  onChange,
  className,
  size = "default",
}: {
  options: ReadonlyArray<SegmentedOption<T>>;
  value: T;
  onChange: (value: T) => void;
  className?: string;
  size?: "default" | "sm";
}) {
  return (
    <div className={cn("inline-flex flex-wrap items-center gap-1 rounded-md border border-border bg-surface-2 p-1", className)} role="tablist">
      {options.map((option) => {
        const active = option.value === value;
        return (
          <button
            aria-selected={active}
            className={cn(
              "rounded font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent",
              size === "sm" ? "px-2 py-0.5 text-xs" : "px-2.5 py-1 text-xs",
              active ? "bg-bg text-text shadow-sm" : "text-muted hover:text-text",
            )}
            key={option.value}
            onClick={() => onChange(option.value)}
            role="tab"
            type="button"
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
