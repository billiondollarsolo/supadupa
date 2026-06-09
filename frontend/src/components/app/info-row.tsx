import type { LucideIcon } from "lucide-react";

export function InfoRow({ detail, icon: Icon, title, value }: { title: string; detail: string; value: string; icon?: LucideIcon }) {
  return (
    <div className="usage-row">
      <div className="min-w-0">
        <p className="flex items-center gap-2 truncate text-sm font-medium">{Icon ? <Icon size={14} className="text-faint" /> : null}{title}</p>
        <p className="truncate text-xs text-muted">{detail}</p>
      </div>
      <p className="text-right text-xs text-muted">{value}</p>
    </div>
  );
}
