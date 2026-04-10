import { useTranslation } from "react-i18next";
import { AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { ChannelRuntimeStatus } from "@/types/channel";
import type { ChannelStatusMeta, ChannelRemediationMeta } from "../channels-status-utils";

interface ChannelDiagnosticsCardProps {
  status: ChannelRuntimeStatus;
  statusMeta: ChannelStatusMeta;
  remediation: ChannelRemediationMeta | null;
  checkedLabel: string | null;
  diagnosticsHint: string;
  timelineItems: Array<{ label: string; value: string }>;
  onRemediationAction: () => void;
}

export function ChannelDiagnosticsCard({
  status,
  statusMeta,
  remediation,
  checkedLabel,
  diagnosticsHint,
  timelineItems,
  onRemediationAction,
}: ChannelDiagnosticsCardProps) {
  const { t } = useTranslation("channels");

  return (
    <div
      className={cn(
        "rounded-xl border p-4",
        statusMeta.surfaceClass,
      )}
    >
      <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_220px]">
        <div>
          <div className="flex items-center gap-2 text-sm font-medium">
            <AlertTriangle className="h-4 w-4" />
            <span>
              {t("detail.whatHappened", {
                defaultValue: "What happened",
              })}
            </span>
          </div>
          <p className="mt-2 text-base font-semibold">
            {status.summary || statusMeta.label}
          </p>
          <p className="mt-1 text-sm text-muted-foreground">
            {status.remediation?.headline || diagnosticsHint}
          </p>

          <div className="mt-4">
            <p className="text-xs-plus font-medium uppercase tracking-[0.16em] text-muted-foreground">
              {t("detail.recommendedAction", {
                defaultValue: "Recommended action",
              })}
            </p>
            <p className="mt-2 text-sm font-medium">
              {remediation?.label ||
                t("actions.inspect", { defaultValue: "Inspect issue" })}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {diagnosticsHint}
            </p>
            {remediation && remediation.target !== "details" && (
              <Button
                size="sm"
                onClick={onRemediationAction}
                className="mt-3 sm:hidden"
              >
                {remediation.label}
              </Button>
            )}
          </div>

          {status.detail && (
            <details className="mt-4 rounded-lg border border-border/80 bg-background/60 p-3">
              <summary className="cursor-pointer text-sm font-medium">
                {t("detail.technicalDetail", {
                  defaultValue: "Technical detail",
                })}
              </summary>
              <p className="mt-2 break-words text-xs text-muted-foreground">
                {status.detail}
              </p>
            </details>
          )}
        </div>

        <div>
          <p className="text-xs-plus font-medium uppercase tracking-[0.16em] text-muted-foreground">
            {t("detail.timeline.title", { defaultValue: "Timeline" })}
          </p>
          <div className="mt-3 space-y-2">
            {timelineItems.length > 0 ? (
              timelineItems.map((item) => (
                <div
                  key={item.label}
                  className="flex items-start justify-between gap-4 rounded-lg bg-background/60 px-3 py-2"
                >
                  <span className="text-xs text-muted-foreground">
                    {item.label}
                  </span>
                  <span className="text-right text-xs font-medium tabular-nums">
                    {item.value}
                  </span>
                </div>
              ))
            ) : (
              <div className="rounded-lg bg-background/60 px-3 py-2 text-xs text-muted-foreground">
                {checkedLabel ||
                  t("detail.timeline.noData", {
                    defaultValue: "No recent channel checks recorded yet.",
                  })}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
