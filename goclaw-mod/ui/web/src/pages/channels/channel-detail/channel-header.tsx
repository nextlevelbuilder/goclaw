import { useTranslation } from "react-i18next";
import { ArrowLeft, Radio, Settings, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { channelTypeLabels, getChannelCheckedLabel, getChannelFailureKindLabel, getChannelStatusMeta } from "../channels-status-view";
import type {
  ChannelInstanceData,
  ChannelRuntimeStatus,
} from "@/types/channel";

interface ChannelHeaderProps {
  instance: ChannelInstanceData;
  status: ChannelRuntimeStatus | null;
  agentName: string;
  onBack: () => void;
  onAdvanced: () => void;
  onDelete: () => void;
  primaryAction?: {
    label: string;
    onClick: () => void;
  } | null;
}

export function ChannelHeader({
  instance,
  status,
  agentName,
  onBack,
  onAdvanced,
  onDelete,
  primaryAction,
}: ChannelHeaderProps) {
  const { t } = useTranslation("channels");
  const displayTitle = instance.display_name || instance.name;
  const typeLabel =
    channelTypeLabels[instance.channel_type] || instance.channel_type;
  const statusMeta = getChannelStatusMeta(status, instance.enabled, t);
  const failureKind = getChannelFailureKindLabel(status?.failure_kind, t);
  const checkedLabel = getChannelCheckedLabel(status, t);
  const summaryLine = status?.summary || statusMeta.label;

  return (
    <TooltipProvider>
      <div className="sticky top-0 z-10 border-b bg-card/95 px-3 py-2 backdrop-blur landscape-compact sm:px-4">
        <div className="flex items-start gap-2 sm:gap-3">
          <Button
            variant="ghost"
            size="icon"
            onClick={onBack}
            className="shrink-0 size-9"
          >
            <ArrowLeft className="h-4 w-4" />
          </Button>

          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary sm:h-12 sm:w-12">
            <Radio className="h-5 w-5 sm:h-6 sm:w-6" />
          </div>

          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <h2 className="truncate text-base font-semibold">{displayTitle}</h2>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span
                    className={cn(
                      "inline-block h-2.5 w-2.5 shrink-0 rounded-full",
                      instance.enabled
                        ? "bg-emerald-500"
                        : "bg-muted-foreground/50",
                    )}
                  />
                </TooltipTrigger>
                <TooltipContent side="bottom" className="text-xs">
                  {instance.enabled ? t("enabled") : t("disabled")}
                </TooltipContent>
              </Tooltip>
              <Badge variant={statusMeta.badgeVariant} className="text-2xs">
                {statusMeta.label}
              </Badge>
              {failureKind && <Badge variant="outline" className="text-2xs">{failureKind}</Badge>}
            </div>
            <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
              <span className="font-mono text-xs-plus">{instance.name}</span>
              <span className="text-border">·</span>
              <Badge variant="outline" className="text-2xs">
                {typeLabel}
              </Badge>
              <span className="text-border">·</span>
              <span>{t("detail.agent", { name: agentName })}</span>
              {checkedLabel && (
                <>
                  <span className="text-border">·</span>
                  <span>{checkedLabel}</span>
                </>
              )}
            </div>
            <div className="mt-1 truncate text-xs text-muted-foreground">
              {summaryLine}
            </div>
          </div>

          {primaryAction && (
            <Button
              size="sm"
              onClick={primaryAction.onClick}
              className="hidden shrink-0 sm:inline-flex"
            >
              {primaryAction.label}
            </Button>
          )}

          <Button
            variant="ghost"
            size="sm"
            onClick={onAdvanced}
            className="shrink-0 gap-1.5 size-9 sm:w-auto sm:px-3"
          >
            <Settings className="h-4 w-4" />
            <span className="hidden sm:inline">{t("detail.advanced")}</span>
          </Button>

          {!instance.is_default && (
            <Button
              variant="ghost"
              size="sm"
              onClick={onDelete}
              className="shrink-0 gap-1.5 size-9 sm:w-auto sm:px-3 text-muted-foreground hover:text-destructive"
            >
              <Trash2 className="h-4 w-4" />
              <span className="hidden sm:inline">{t("delete.title")}</span>
            </Button>
          )}
        </div>
      </div>
    </TooltipProvider>
  );
}
