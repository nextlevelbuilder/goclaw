import { useTranslation } from "react-i18next";
import { Switch } from "../common/Switch";
import type {
  ChannelInstanceData,
  ChannelStatus,
} from "../../types/channel";
import { getChannelStatusDisplay } from "../../utils/channel-status";

interface ChannelCardProps {
  instance: ChannelInstanceData;
  status: ChannelStatus | null;
  agentName: string;
  onToggleEnabled: (enabled: boolean) => void;
  onClick: () => void;
}

function TelegramIcon() {
  return (
    <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M11.944 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0a12 12 0 0 0-.056 0zm4.962 7.224c.1-.002.321.023.465.14a.506.506 0 0 1 .171.325c.016.093.036.306.02.472-.18 1.898-.962 6.502-1.36 8.627-.168.9-.499 1.201-.82 1.23-.696.065-1.225-.46-1.9-.902-1.056-.693-1.653-1.124-2.678-1.8-1.185-.78-.417-1.21.258-1.91.177-.184 3.247-2.977 3.307-3.23.007-.032.014-.15-.056-.212s-.174-.041-.249-.024c-.106.024-1.793 1.14-5.061 3.345-.48.33-.913.49-1.302.48-.428-.008-1.252-.241-1.865-.44-.752-.245-1.349-.374-1.297-.789.027-.216.325-.437.893-.663 3.498-1.524 5.83-2.529 6.998-3.014 3.332-1.386 4.025-1.627 4.476-1.635z" />
    </svg>
  );
}

function DiscordIcon() {
  return (
    <svg className="w-5 h-5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M20.317 4.3698a19.7913 19.7913 0 00-4.8851-1.5152.0741.0741 0 00-.0785.0371c-.211.3753-.4447.8648-.6083 1.2495-1.8447-.2762-3.68-.2762-5.4868 0-.1636-.3933-.4058-.8742-.6177-1.2495a.077.077 0 00-.0785-.037 19.7363 19.7363 0 00-4.8852 1.515.0699.0699 0 00-.0321.0277C.5334 9.0458-.319 13.5799.0992 18.0578a.0824.0824 0 00.0312.0561c2.0528 1.5076 4.0413 2.4228 5.9929 3.0294a.0777.0777 0 00.0842-.0276c.4616-.6304.8731-1.2952 1.226-1.9942a.076.076 0 00-.0416-.1057c-.6528-.2476-1.2743-.5495-1.8722-.8923a.077.077 0 01-.0076-.1277c.1258-.0943.2517-.1923.3718-.2914a.0743.0743 0 01.0776-.0105c3.9278 1.7933 8.18 1.7933 12.0614 0a.0739.0739 0 01.0785.0095c.1202.099.246.1981.3728.2924a.077.077 0 01-.0066.1276 12.2986 12.2986 0 01-1.873.8914.0766.0766 0 00-.0407.1067c.3604.698.7719 1.3628 1.225 1.9932a.076.076 0 00.0842.0286c1.961-.6067 3.9495-1.5219 6.0023-3.0294a.077.077 0 00.0313-.0552c.5004-5.177-.8382-9.6739-3.5485-13.6604a.061.061 0 00-.0312-.0286zM8.02 15.3312c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9555-2.4189 2.157-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.9555 2.4189-2.1569 2.4189zm7.9748 0c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9554-2.4189 2.1569-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.946 2.4189-2.1568 2.4189z" />
    </svg>
  );
}

export function ChannelCard({
  instance,
  status,
  agentName,
  onToggleEnabled,
  onClick,
}: ChannelCardProps) {
  const { t } = useTranslation("channels");
  const isTelegram = instance.channel_type === "telegram";

  const { dotColor, statusText } = getChannelStatusDisplay(instance.enabled, status, t);
  const summaryHint =
    instance.enabled && status?.summary && status.state !== "healthy"
      ? status.summary
      : null;

  return (
    <div
      onClick={onClick}
      className="border border-border rounded-xl p-4 hover:bg-surface-tertiary/30 transition-colors cursor-pointer"
    >
      <div className="flex items-start gap-3">
        {/* Channel icon */}
        <div className="shrink-0 text-text-muted mt-0.5">
          {isTelegram ? <TelegramIcon /> : <DiscordIcon />}
        </div>

        {/* Info */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-text-primary truncate">
              {instance.display_name || instance.name}
            </span>
            <span className="rounded-full px-1.5 py-0.5 text-[10px] bg-surface-tertiary text-text-secondary border border-border shrink-0">
              {t(`channelTypes.${instance.channel_type}`)}
            </span>
          </div>
          <div className="flex items-center gap-2 mt-1">
            <span className={`w-1.5 h-1.5 rounded-full ${dotColor}`} />
            <span className="text-[11px] text-text-muted">{statusText}</span>
            <span className="text-[11px] text-text-muted">·</span>
            <span className="text-[11px] text-text-muted truncate">
              {agentName}
            </span>
          </div>
          {summaryHint && (
            <div className="mt-1 truncate text-[11px] text-text-muted">
              {summaryHint}
            </div>
          )}
        </div>

        {/* Enable toggle */}
        <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
          <Switch
            checked={instance.enabled}
            onCheckedChange={onToggleEnabled}
          />
        </div>
      </div>
    </div>
  );
}
