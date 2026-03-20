import { useTranslation } from "react-i18next";
import { ArrowLeft, Radio, Settings, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { channelTypeLabels } from "../channels-status-view";
import type { ChannelInstanceData } from "@/types/channel";

interface ChannelHeaderProps {
  instance: ChannelInstanceData;
  status: { running: boolean } | null;
  agentName: string;
  onBack: () => void;
  onAdvanced: () => void;
  onDelete: () => void;
}

export function ChannelHeader({ instance, status, agentName, onBack, onAdvanced, onDelete }: ChannelHeaderProps) {
  const { t } = useTranslation("channels");
  const displayTitle = instance.display_name || instance.name;
  const typeLabel = channelTypeLabels[instance.channel_type] || instance.channel_type;

  return (
    <TooltipProvider>
      <div className="sticky top-0 z-10 flex items-center gap-2 border-b bg-card px-3 py-2 landscape-compact sm:px-4 sm:gap-3">
        <Button variant="ghost" size="icon" onClick={onBack} className="shrink-0 size-9">
          <ArrowLeft className="h-4 w-4" />
        </Button>

        {/* Channel icon */}
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary sm:h-12 sm:w-12">
          <Radio className="h-5 w-5 sm:h-6 sm:w-6" />
        </div>

        {/* Channel info */}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5 flex-wrap">
            <h2 className="truncate text-base font-semibold">{displayTitle}</h2>
            <Tooltip>
              <TooltipTrigger asChild>
                <span
                  className={cn(
                    "inline-block h-2.5 w-2.5 shrink-0 rounded-full",
                    instance.enabled ? "bg-emerald-500" : "bg-muted-foreground/50",
                  )}
                />
              </TooltipTrigger>
              <TooltipContent side="bottom" className="text-xs">
                {instance.enabled ? t("enabled") : t("disabled")}
              </TooltipContent>
            </Tooltip>
            {status && (
              <Badge variant={status.running ? "success" : "secondary"} className="text-[10px]">
                {status.running ? t("status.running") : t("status.stopped")}
              </Badge>
            )}
          </div>
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground mt-0.5">
            <span className="font-mono text-[11px]">{instance.name}</span>
            <span className="text-border">·</span>
            <Badge variant="outline" className="text-[10px]">{typeLabel}</Badge>
            <span className="text-border">·</span>
            <span>{t("detail.agent", { name: agentName })}</span>
          </div>
        </div>

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
    </TooltipProvider>
  );
}
