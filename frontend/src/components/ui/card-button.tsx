import * as React from "react";
import { cn } from "../../lib/cn";

// A clickable card surface (e.g. summary tiles, project cards, section pickers).
// Standardizes the bordered-button-as-card pattern with a consistent hover and
// focus ring.
export const CardButton = React.forwardRef<HTMLButtonElement, React.ButtonHTMLAttributes<HTMLButtonElement>>(
  ({ className, type = "button", ...props }, ref) => (
    <button
      className={cn(
        "min-w-0 rounded-md border border-border bg-bg p-3 text-left transition-colors hover:border-border-strong hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:pointer-events-none disabled:opacity-55",
        className,
      )}
      ref={ref}
      type={type}
      {...props}
    />
  ),
);

CardButton.displayName = "CardButton";
