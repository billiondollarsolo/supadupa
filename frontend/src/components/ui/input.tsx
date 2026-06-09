import * as React from "react";
import { cn } from "../../lib/cn";

export const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(({ className, ...props }, ref) => (
  <input
    className={cn("flex min-h-9 w-full min-w-0 rounded-md border border-border bg-bg px-3 text-sm text-text transition-colors placeholder:text-faint focus:border-border-strong focus:outline-none disabled:cursor-not-allowed disabled:opacity-55", className)}
    ref={ref}
    {...props}
  />
));

Input.displayName = "Input";
