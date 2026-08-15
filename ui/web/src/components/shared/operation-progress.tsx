import { CheckCircle2, XCircle, Loader2, Circle } from "lucide-react";

export interface ProgressStep {
  id: string;
  label: string;
  status: "pending" | "running" | "done" | "error";
  detail?: string;
  current?: number;
  total?: number;
  errorMessage?: string;
}

interface OperationProgressProps {
  steps: ProgressStep[];
  elapsed?: number;
  className?: string;
}

function StepIcon({ status }: { status: ProgressStep["status"] }) {
  switch (status) {
    case "done":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-green-500" />;
    case "error":
      return <XCircle className="h-4 w-4 shrink-0 text-destructive" />;
    case "running":
      return <Loader2 className="h-4 w-4 shrink-0 text-blue-500 animate-spin" />;
    default:
      return <Circle className="h-4 w-4 shrink-0 text-muted-foreground/40" />;
  }
}

export function OperationProgress({ steps, elapsed, className }: OperationProgressProps) {
  return (
    <div className={`rounded-lg border bg-card p-4 space-y-2.5 ${className ?? ""}`}>
      {steps.map((step) => (
        <div key={step.id}>
          <div className="flex items-center gap-2 text-sm">
            <StepIcon status={step.status} />
            <span className={step.status === "pending" ? "text-muted-foreground" : ""}>
              {step.label}
            </span>
            <span className="text-xs text-muted-foreground ml-auto">
              {step.detail
                ? step.detail
                : step.status === "done" && step.total != null && step.total > 0
                  ? `${step.total} items`
                  : null}
            </span>
          </div>

          {step.status === "running" && step.total != null && step.total > 0 && (
            <div className="ml-6 mt-1.5 flex items-center gap-2">
              <div className="h-1.5 flex-1 rounded-full bg-muted overflow-hidden">
                <div
                  className="h-full bg-primary rounded-full transition-all duration-300"
                  style={{ width: `${Math.min(100, ((step.current ?? 0) / step.total) * 100)}%` }}
                />
              </div>
              <span className="text-xs text-muted-foreground tabular-nums whitespace-nowrap">
                {step.current ?? 0}/{step.total}
              </span>
            </div>
          )}

          {step.status === "error" && step.errorMessage && (
            <p className="ml-6 mt-1 text-xs text-destructive">{step.errorMessage}</p>
          )}
        </div>
      ))}

      {elapsed != null && (
        <div className="text-xs text-muted-foreground pt-2 border-t">
          {elapsed < 60 ? `${elapsed}s` : `${Math.floor(elapsed / 60)}m ${elapsed % 60}s`}
        </div>
      )}
    </div>
  );
}
