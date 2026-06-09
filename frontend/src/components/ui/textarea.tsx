import * as React from "react";
import { cn } from "../../lib/cn";

export const Textarea = React.forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement>>(({ className, ...props }, ref) => (
  <textarea
    className={cn("flex min-h-20 w-full min-w-0 rounded-md border border-border bg-bg px-3 py-2 text-sm text-text transition-colors placeholder:text-faint focus:border-border-strong focus:outline-none disabled:cursor-not-allowed disabled:opacity-55", className)}
    ref={ref}
    {...props}
  />
));

Textarea.displayName = "Textarea";
