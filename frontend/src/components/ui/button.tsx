import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/cn";

const buttonVariants = cva(
  "inline-flex min-h-9 items-center justify-center gap-2 rounded-md border text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:pointer-events-none disabled:opacity-55",
  {
    variants: {
      variant: {
        default: "border-transparent bg-accent text-white hover:bg-accent-hover",
        secondary: "border-border bg-surface-2 text-text hover:border-border-strong",
        danger: "border-transparent bg-danger text-white hover:bg-danger/90",
        ghost: "border-transparent bg-transparent text-muted hover:bg-surface-2 hover:text-text",
      },
      size: {
        default: "px-3",
        sm: "min-h-8 px-2.5 text-xs",
        icon: "min-h-8 min-w-8 px-0",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export type ButtonProps = React.ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean;
  };

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ asChild = false, className, size, variant, ...props }, ref) => {
    const Component = asChild ? Slot : "button";
    return <Component className={cn(buttonVariants({ variant, size }), className)} ref={ref} {...props} />;
  },
);

Button.displayName = "Button";

export { buttonVariants };
