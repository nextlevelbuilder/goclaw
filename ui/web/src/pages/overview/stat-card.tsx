import { AreaChart, Area, ResponsiveContainer } from "recharts";
import { TrendingUp, TrendingDown } from "lucide-react";

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
  const sparkData = sparkline?.map((v) => ({ v }));
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
                  trend > 0 ? "text-green-600" : "text-red-600"
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
      {sparkData && sparkData.length > 1 && (
        <div className="mt-3 h-[40px]">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={sparkData} margin={{ top: 0, right: 0, left: 0, bottom: 0 }}>
              <defs>
                <linearGradient id="sparkGrad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="var(--color-primary)" stopOpacity={0.3} />
                  <stop offset="95%" stopColor="var(--color-primary)" stopOpacity={0} />
                </linearGradient>
              </defs>
              <Area
                type="monotone"
                dataKey="v"
                stroke="var(--color-primary)"
                strokeWidth={1.5}
                fill="url(#sparkGrad)"
                dot={false}
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  );
}
