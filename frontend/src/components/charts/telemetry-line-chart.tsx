import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { formatDateTime, formatTime } from "../../lib/format";

export type TelemetryLineChartPoint = {
  sampledAt: string;
  cpu: number;
  memory: number;
};

export function TelemetryLineChart({
  ariaLabel,
  domain = [0, 100],
  points,
  title,
  unit = "%",
}: {
  points: TelemetryLineChartPoint[];
  title: string;
  ariaLabel: string;
  /** Y-axis domain. Defaults to percentage scale [0, 100]. */
  domain?: [number, number];
  /** Unit suffix shown on Y-axis ticks. Defaults to "%". */
  unit?: string;
}) {
  const latest = points[points.length - 1];
  const first = points[0];
  const [domainMin, domainMax] = domain;
  const spanMs = first && latest ? Math.abs(new Date(latest.sampledAt).getTime() - new Date(first.sampledAt).getTime()) : 0;
  const formatTimeLabel = spanMs >= 24 * 60 * 60 * 1000 ? formatDateTime : formatTime;
  const formatYTick = (value: number) => `${value}${unit}`;
  const formatXTick = (value: string) => {
    // Only label the first and last samples to stay compact.
    if (value === first?.sampledAt || value === latest?.sampledAt) {
      return formatTimeLabel(value);
    }
    return "";
  };
  return (
    <div className="usage-trend mt-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="label">{title}</p>
          <p className="mt-1 text-sm font-medium">{points.length > 1 ? `${points.length} samples` : "Waiting for samples"}</p>
        </div>
        <div className="flex flex-wrap justify-end gap-2 text-xs text-muted">
          <span className="trend-legend cpu">CPU</span>
          <span className="trend-legend memory">RAM</span>
          {latest ? <span>{formatTimeLabel(latest.sampledAt)}</span> : null}
        </div>
      </div>
      <div aria-label={ariaLabel} className="telemetry-chart mt-3" role="img">
        <ResponsiveContainer height={128} width="100%">
          <LineChart data={points} margin={{ bottom: 4, left: 0, right: 8, top: 8 }}>
            <CartesianGrid stroke="var(--color-border)" strokeDasharray="0" vertical={false} />
            <XAxis
              axisLine={{ stroke: "var(--color-border)" }}
              dataKey="sampledAt"
              fontSize={10}
              interval="preserveStartEnd"
              minTickGap={24}
              stroke="var(--color-faint)"
              tick={{ fill: "var(--color-faint)" }}
              tickFormatter={formatXTick}
              tickLine={false}
            />
            <YAxis
              axisLine={false}
              domain={[domainMin, domainMax]}
              fontSize={10}
              stroke="var(--color-faint)"
              tick={{ fill: "var(--color-faint)" }}
              tickCount={3}
              tickFormatter={formatYTick}
              tickLine={false}
              width={36}
            />
            <Tooltip content={<TelemetryTooltip unit={unit} />} cursor={{ stroke: "var(--color-border-strong)" }} />
            <Line dataKey="cpu" dot={{ r: 2 }} isAnimationActive={false} name="CPU" stroke="var(--color-accent)" strokeWidth={1.5} type="monotone" />
            <Line dataKey="memory" dot={{ r: 2 }} isAnimationActive={false} name="RAM" stroke="var(--color-success)" strokeWidth={1.5} type="monotone" />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}

type TelemetryTooltipPayload = {
  color?: string;
  dataKey?: string | number;
  name?: string | number;
  value?: number | string;
};

function TelemetryTooltip({ active, label, payload, unit = "%" }: { active?: boolean; label?: string | number; payload?: TelemetryTooltipPayload[]; unit?: string }) {
  if (!active || !payload?.length || typeof label !== "string") {
    return null;
  }
  return (
    <div className="chart-tooltip">
      <p className="text-xs text-faint">{formatDateTime(label)}</p>
      {payload.map((item) => (
        <p className="text-xs" key={item.dataKey}>
          <span style={{ color: item.color }}>{item.name}</span>: {formatMeasure(Number(item.value ?? 0), unit)}
        </p>
      ))}
    </div>
  );
}

function formatMeasure(value: number, unit: string) {
  return `${(Number.isFinite(value) ? value : 0).toFixed(1)}${unit}`;
}
