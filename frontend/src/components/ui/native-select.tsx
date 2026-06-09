import * as React from "react";
import { cn } from "../../lib/cn";

export const NativeSelect = React.forwardRef<HTMLSelectElement, React.SelectHTMLAttributes<HTMLSelectElement>>(({ className, ...props }, ref) => (
  <select
    className={cn("flex min-h-9 w-full min-w-0 rounded-md border border-border bg-bg px-3 text-sm text-text transition-colors focus:border-border-strong focus:outline-none disabled:cursor-not-allowed disabled:opacity-55", className)}
    ref={ref}
    {...props}
  />
));

NativeSelect.displayName = "NativeSelect";
