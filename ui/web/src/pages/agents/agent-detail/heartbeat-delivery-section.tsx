import { useTranslation } from "react-i18next";
import { Send } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import type { DeliveryTarget } from "@/pages/agents/hooks/use-agent-heartbeat";

interface HeartbeatDeliverySectionProps {
  channelNames: string[];
  channel: string;
  setChannel: (v: string) => void;
  chatId: string;
  setChatId: (v: string) => void;
  targets: DeliveryTarget[];
}

/** Channel and chat-ID selectors for heartbeat delivery destination. */
export function HeartbeatDeliverySection({
  channelNames,
  channel, setChannel,
  chatId, setChatId,
  targets,
}: HeartbeatDeliverySectionProps) {
  const { t } = useTranslation("agents");

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Send className="h-3.5 w-3.5 text-blue-500" />
        <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          {t("heartbeat.sectionDelivery")}
        </h4>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-[140px_1fr]">
        <div className="space-y-1">
          <Label className="text-xs">{t("heartbeat.channel")}</Label>
          {channelNames.length > 0 ? (
            <Select
              value={channel || "__none__"}
              onValueChange={(v) => { setChannel(v === "__none__" ? "" : v); setChatId(""); }}
            >
              <SelectTrigger className="w-full text-base md:text-sm">
                <SelectValue placeholder={t("heartbeat.channelPlaceholder")} />
              </SelectTrigger>
              <SelectContent position="popper">
                <SelectItem value="__none__">{t("heartbeat.channelNone")}</SelectItem>
                {channelNames.map((ch) => (
                  <SelectItem key={ch} value={ch}>{ch}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <Input
              placeholder="telegram"
              value={channel}
              onChange={(e) => setChannel(e.target.value)}
              className="text-base md:text-sm"
            />
          )}
        </div>
        <div className="space-y-1">
          <Label className="text-xs">{t("heartbeat.chatId")}</Label>
          {(() => {
            if (!channel) {
              return <Input placeholder={t("heartbeat.selectChannelFirst")} disabled className="text-base md:text-sm" />;
            }
            const filtered = targets.filter((tgt) => tgt.channel === channel);
            if (filtered.length > 0) {
              return (
                <Select value={chatId || "__none__"} onValueChange={(v) => setChatId(v === "__none__" ? "" : v)}>
                  <SelectTrigger className="w-full text-base md:text-sm">
                    <SelectValue placeholder={t("heartbeat.chatIdPlaceholder")} />
                  </SelectTrigger>
                  <SelectContent position="popper">
                    <SelectItem value="__none__">{t("heartbeat.channelNone")}</SelectItem>
                    {filtered.map((tgt) => (
                      <SelectItem key={tgt.chatId} value={tgt.chatId}>
                        {tgt.title ? `${tgt.title} (${tgt.chatId})` : tgt.chatId}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              );
            }
            return (
              <Input
                placeholder="-100123456789"
                value={chatId}
                onChange={(e) => setChatId(e.target.value)}
                className="text-base md:text-sm"
              />
            );
          })()}
        </div>
      </div>
    </div>
  );
}
