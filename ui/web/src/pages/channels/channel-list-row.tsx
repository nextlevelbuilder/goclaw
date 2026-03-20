import { Radio, QrCode, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { ChannelInstanceData } from "@/types/channel";
import { channelTypeLabels } from "./channels-status-view";
import { channelsWithAuth } from "./channel-wizard-registry";

interface ChannelListRowProps {
  instance: ChannelInstanceData;
  status: { running: boolean } | null;
  agentName: string;
  onClick: () => void;
  onAuth?: () => void;
  onDelete?: () => void;
}

export function ChannelListRow({ instance, status, agentName, onClick, onAuth, onDelete }: ChannelListRowProps) {
  const { t } = useTranslation("channels");
  const displayName = instance.display_name || instance.name;
  const isRunning = status?.running ?? false;

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full cursor-pointer items-center gap-3 rounded-lg border bg-card px-4 py-3 text-left transition-all hover:border-primary/30 hover:shadow-sm"
    >
      {/* Icon */}
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
        <Radio className="h-4 w-4" />
      </div>

      {/* Name + slug */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-semibold">{displayName}</span>
          <span className={`inline-block h-2 w-2 shrink-0 rounded-full ${
            !instance.enabled ? "bg-muted-foreground/40"
              : isRunning ? "bg-emerald-500"
              : "bg-amber-500"
          }`} />
        </div>
        {instance.display_name && (
          <div className="truncate text-xs text-muted-foreground">{instance.name}</div>
        )}
      </div>

      {/* Type */}
      <div className="hidden shrink-0 sm:block">
        <Badge variant="secondary" className="text-[11px]">
          {channelTypeLabels[instance.channel_type] || instance.channel_type}
        </Badge>
      </div>

      {/* Status text */}
      <div className="hidden shrink-0 text-xs text-muted-foreground sm:block sm:w-16">
        {!instance.enabled
          ? t("disabled")
          : isRunning ? t("status.running") : t("status.stopped")}
      </div>

      {/* Agent */}
      <div className="hidden shrink-0 text-xs text-muted-foreground md:block md:w-28 md:truncate">
        {agentName}
      </div>

      {/* Actions */}
      <div className="flex shrink-0 items-center gap-1">
        {onAuth && channelsWithAuth.has(instance.channel_type) && (
          <Button
            variant="ghost"
            size="xs"
            className="text-muted-foreground hover:text-primary"
            onClick={(e) => { e.stopPropagation(); onAuth(); }}
          >
            <QrCode className="h-3.5 w-3.5" />
          </Button>
        )}
        {onDelete && !instance.is_default && (
          <Button
            variant="ghost"
            size="xs"
            className="text-muted-foreground hover:text-destructive"
            onClick={(e) => { e.stopPropagation(); onDelete(); }}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        )}
      </div>
    </button>
  );
}
