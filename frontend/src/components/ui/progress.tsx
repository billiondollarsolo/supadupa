import * as React from "react";
import * as ProgressPrimitive from "@radix-ui/react-progress";
import { cn } from "../../lib/cn";

export const Progress = React.forwardRef<
  React.ElementRef<typeof ProgressPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof ProgressPrimitive.Root> & {
    value?: number;
    indicatorClassName?: string;
    indicatorStyle?: React.CSSProperties;
  }
>(({ className, value = 0, indicatorClassName, indicatorStyle, ...props }, ref) => {
  const normalized = Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0));
  return (
    <ProgressPrimitive.Root className={cn("relative h-2 w-full overflow-hidden rounded-full border border-border bg-surface-2", className)} ref={ref} {...props}>
      <ProgressPrimitive.Indicator
        className={cn("h-full min-w-0.5 rounded-full bg-accent transition-all", indicatorClassName)}
        style={{ transform: `translateX(-${100 - normalized}%)`, ...indicatorStyle }}
      />
    </ProgressPrimitive.Root>
  );
});

Progress.displayName = ProgressPrimitive.Root.displayName;
