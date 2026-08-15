import { QrCode, Radio, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type {
  ChannelInstanceData,
  ChannelRuntimeStatus,
} from "@/types/channel";
import {
  channelTypeLabels,
  getChannelCheckedLabel,
  getChannelFailureKindLabel,
  getChannelRemediationMeta,
  getChannelStatusMeta,
} from "./channels-status-view";
import { channelsWithAuth } from "./channel-wizard-registry";

interface ChannelListRowProps {
  instance: ChannelInstanceData;
  status: ChannelRuntimeStatus | null;
  agentName: string;
  onClick: () => void;
  onAuth?: () => void;
  onDelete?: () => void;
}

export function ChannelListRow({
  instance,
  status,
  agentName,
  onClick,
  onAuth,
  onDelete,
}: ChannelListRowProps) {
  const { t } = useTranslation("channels");
  const displayName = instance.display_name || instance.name;
  const supportsReauth = channelsWithAuth.has(instance.channel_type);
  const statusMeta = getChannelStatusMeta(status, instance.enabled, t);
  const failureKind = getChannelFailureKindLabel(status?.failure_kind, t);
  const checkedLabel = getChannelCheckedLabel(status, t);
  const remediation = getChannelRemediationMeta(status, supportsReauth, t);
  const summaryLine = status?.summary || statusMeta.label;
  const streakLabel =
    status?.consecutive_failures && status.consecutive_failures > 1
      ? t("list.failureStreak", {
          defaultValue: "{{count}} failures in a row",
          count: status.consecutive_failures,
        })
      : checkedLabel;
  const nextStepLabel =
    remediation?.label || t("actions.inspect", { defaultValue: "Inspect issue" });
  const nextStepHint =
    remediation?.headline ||
    t("list.openChannelDetail", {
      defaultValue: "Open channel detail for the latest diagnosis",
    });

  return (
    <div
      className={cn(
        "rounded-xl border bg-card shadow-sm transition-colors hover:border-primary/30",
        statusMeta.attention && statusMeta.surfaceClass,
      )}
    >
      <div className="flex items-stretch gap-2 p-3 sm:p-4">
        <button
          type="button"
          onClick={onClick}
          className="flex-1 text-left"
        >
          <div className="grid gap-3 lg:grid-cols-[minmax(0,1.05fr)_minmax(0,1fr)_minmax(180px,0.7fr)]">
            <div className="flex min-w-0 gap-3">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">
                <Radio className="h-4 w-4" />
              </div>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate text-sm font-semibold">
                    {displayName}
                  </span>
                  <span
                    className={cn(
                      "inline-block h-2 w-2 shrink-0 rounded-full",
                      statusMeta.dotClass,
                    )}
                  />
                  <Badge variant="outline" className="text-xs-plus">
                    {channelTypeLabels[instance.channel_type] || instance.channel_type}
                  </Badge>
                </div>
                <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <span className="font-mono">{instance.name}</span>
                  <span className="text-border">·</span>
                  <span className="truncate">{agentName}</span>
                </div>
              </div>
            </div>

            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={statusMeta.badgeVariant}>{statusMeta.label}</Badge>
                {failureKind && <Badge variant="outline">{failureKind}</Badge>}
              </div>
              <p className="mt-2 truncate text-sm font-medium">{summaryLine}</p>
              {streakLabel && (
                <p className="mt-1 truncate text-xs text-muted-foreground">
                  {streakLabel}
                </p>
              )}
            </div>

            <div className="min-w-0">
              <p className="text-xs-plus font-medium uppercase tracking-[0.16em] text-muted-foreground">
                {t("list.nextStep", { defaultValue: "Next step" })}
              </p>
              <p className="mt-2 truncate text-sm font-medium">{nextStepLabel}</p>
              <p className="mt-1 truncate text-xs text-muted-foreground">
                {nextStepHint}
              </p>
            </div>
          </div>
        </button>

        <div className="flex shrink-0 items-start gap-1">
          {onAuth && supportsReauth && (
            <Button
              variant="ghost"
              size="xs"
              className="text-muted-foreground hover:text-primary"
              onClick={(e) => {
                e.stopPropagation();
                onAuth();
              }}
            >
              <QrCode className="h-3.5 w-3.5" />
            </Button>
          )}
          {onDelete && !instance.is_default && (
            <Button
              variant="ghost"
              size="xs"
              className="text-muted-foreground hover:text-destructive"
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
