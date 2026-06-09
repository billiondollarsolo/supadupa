import type { ReactNode } from "react";
import { cn } from "../../lib/cn";
import { Card, CardContent, CardHeader } from "../ui/card";

export function AppPanel({
  actions,
  children,
  className,
  description,
  eyebrow,
  eyebrowClassName,
  title,
}: {
  title?: string;
  eyebrow?: string;
  /** Optional className for the eyebrow, e.g. to tint a danger panel. */
  eyebrowClassName?: string;
  /** Secondary line rendered under the title, inside the header. */
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  const hasHeader = Boolean(title || eyebrow || actions || description);
  return (
    <Card className={cn("min-w-0", className)}>
      <CardContent>
        {hasHeader ? (
          <CardHeader>
            <div className="min-w-0">
              {eyebrow ? <p className={cn("label", eyebrowClassName)}>{eyebrow}</p> : null}
              {title ? <h2 className="mt-0.5 text-base font-medium">{title}</h2> : null}
              {description ? <p className="mt-1 text-xs text-muted">{description}</p> : null}
            </div>
            {actions ? <div className="shrink-0">{actions}</div> : null}
          </CardHeader>
        ) : null}
        {children}
      </CardContent>
    </Card>
  );
}
