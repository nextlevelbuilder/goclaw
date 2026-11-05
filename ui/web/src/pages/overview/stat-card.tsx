import { TrendingUp, TrendingDown } from "lucide-react";

const SPARK_W = 100;
const SPARK_H = 40;

/**
 * Static SVG sparkline. It stretches to its container through the viewBox and never
 * measures the DOM, so it cannot trigger a resize/re-render loop.
 */
function Sparkline({ values, className }: { values: number[]; className?: string }) {
  const min = Math.min(...values);
  const range = Math.max(...values) - min || 1;
  const step = SPARK_W / (values.length - 1);
  const pts = values.map((v, i) => {
    const y = SPARK_H - 2 - ((v - min) / range) * (SPARK_H - 4);
    return `${(i * step).toFixed(2)},${y.toFixed(2)}`;
  });
  const line = `M${pts.join(" L")}`;
  return (
    <svg
      viewBox={`0 0 ${SPARK_W} ${SPARK_H}`}
      preserveAspectRatio="none"
      className={className}
      aria-hidden="true"
    >
      <path d={`${line} L${SPARK_W},${SPARK_H} L0,${SPARK_H} Z`} fill="var(--color-primary)" fillOpacity={0.15} />
      <path
        d={line}
        fill="none"
        stroke="var(--color-primary)"
        strokeWidth={1.5}
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

export function StatCard({
  icon: Icon,
  label,
  value,
  sub,
  sparkline,
  trend,
}: {
  icon: React.ElementType;
  label: string;
  value: string | number;
  sub?: string;
  sparkline?: number[];
  trend?: number | null;
}) {
  const hasTrend = trend != null && trend !== 0;

  return (
    <div className="rounded-lg border bg-card p-5">
      <div className="flex items-center gap-3">
        <div className="rounded-md bg-muted p-2">
          <Icon className="h-4 w-4 text-muted-foreground" />
        </div>
        <div className="flex-1 min-w-0">
          <p className="text-sm text-muted-foreground">{label}</p>
          <div className="flex items-center gap-2">
            <p className="text-2xl font-semibold">{value}</p>
            {hasTrend && (
              <span
                className={`flex items-center gap-0.5 text-xs font-medium ${
                  trend > 0 ? "text-success-text" : "text-destructive-text"
                }`}
              >
                {trend > 0 ? (
                  <TrendingUp className="h-3 w-3" />
                ) : (
                  <TrendingDown className="h-3 w-3" />
                )}
                {trend > 0 ? `+${trend}%` : `${trend}%`}
              </span>
            )}
          </div>
          {sub && <p className="text-xs text-muted-foreground">{sub}</p>}
        </div>
      </div>
      {sparkline && sparkline.length > 1 && (
        <Sparkline values={sparkline} className="mt-3 h-[40px] w-full" />
      )}
    </div>
  );
}
