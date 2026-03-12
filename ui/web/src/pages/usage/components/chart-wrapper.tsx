import { ReactNode } from "react";
import { BarChart3 } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

interface ChartWrapperProps {
  title: string;
  subtitle?: string;
  loading?: boolean;
  empty?: boolean;
  emptyText?: string;
  height?: number;
  children: ReactNode;
  className?: string;
  actions?: ReactNode;
}

export function ChartWrapper({
  title,
  subtitle,
  loading,
  empty,
  emptyText = "No data for selected period",
  height = 300,
  children,
  className,
  actions,
}: ChartWrapperProps) {
  return (
    <div className={cn("rounded-lg border bg-card p-4", className)}>
      <div className="mb-4 flex items-start justify-between">
        <div>
          <h3 className="text-sm font-semibold">{title}</h3>
          {subtitle && <p className="mt-0.5 text-xs text-muted-foreground">{subtitle}</p>}
        </div>
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </div>

      {loading ? (
        <div className="space-y-2" style={{ height }}>
          <Skeleton className="h-full w-full" />
        </div>
      ) : empty ? (
        <div
          className="flex flex-col items-center justify-center text-center"
          style={{ height }}
        >
          <div className="mb-2 rounded-full bg-muted p-2">
            <BarChart3 className="h-5 w-5 text-muted-foreground" />
          </div>
          <p className="text-xs text-muted-foreground">{emptyText}</p>
        </div>
      ) : (
        children
      )}
    </div>
  );
}
