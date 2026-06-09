import type { CSSProperties, ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { Progress } from "../ui/progress";

export type ResourceMeterTone = "neutral" | "info" | "success" | "warning" | "danger";

const TONE_FILL: Record<ResourceMeterTone, string | undefined> = {
  neutral: undefined,
  info: "var(--color-accent)",
  success: "var(--color-success)",
  warning: "var(--color-warning)",
  danger: "var(--color-danger)",
};

export function ResourceMeter({
  detail,
  footer,
  icon: Icon,
  label,
  percent,
  tone = "neutral",
  value,
}: {
  icon?: LucideIcon;
  label: string;
  value: ReactNode;
  detail: ReactNode;
  percent: number;
  footer?: ReactNode;
  tone?: ResourceMeterTone;
}) {
  const normalized = Math.min(100, Math.max(0, Number.isFinite(percent) ? percent : 0));
  const fill = TONE_FILL[tone];
  const indicatorStyle: CSSProperties | undefined = fill ? { backgroundColor: fill } : undefined;
  return (
    <div className="usage-row">
      <div className="min-w-0">
        <p className="flex items-center gap-2 truncate text-sm font-medium">{Icon ? <Icon size={14} className="text-faint" /> : null}{label}</p>
        <p className="mt-1 truncate text-xs text-muted">{detail}</p>
        <Progress className="mt-2" value={normalized || 2} indicatorStyle={indicatorStyle} />
      </div>
      <div className="text-right text-xs text-muted">
        <p className="text-sm font-medium text-text">{value}</p>
        <p>{footer ?? `${normalized.toFixed(1)}%`}</p>
      </div>
    </div>
  );
}
