import { useTranslation } from "react-i18next";
import { Send } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

export interface DeliveryTarget {
  channel: string;
  chatId: string;
  title?: string;
  kind: string;
}

interface CronDeliverySectionProps {
  deliver: boolean;
  setDeliver: (v: boolean) => void;
  channel: string;
  setChannel: (v: string) => void;
  to: string;
  setTo: (v: string) => void;
  wakeHeartbeat: boolean;
  setWakeHeartbeat: (v: boolean) => void;
  channelNames: string[];
  targets: DeliveryTarget[];
  readonly: boolean;
}

export function CronDeliverySection({
  deliver,
  setDeliver,
  channel,
  setChannel,
  to,
  setTo,
  wakeHeartbeat,
  setWakeHeartbeat,
  channelNames,
  targets,
  readonly,
}: CronDeliverySectionProps) {
  const { t } = useTranslation("cron");

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="flex items-center gap-2">
        <Send className="h-4 w-4 text-blue-500" />
        <h3 className="text-sm font-medium">{t("detail.delivery")}</h3>
      </div>
      <p className="text-xs text-muted-foreground">{t("detail.deliveryDesc")}</p>

      <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
        <p className="text-sm font-medium">{t("detail.deliverToChannel")}</p>
        <Switch checked={deliver} onCheckedChange={setDeliver} disabled={readonly} />
      </div>

      {deliver && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-[140px_1fr]">
          <div className="space-y-2 min-w-0">
            <Label>{t("detail.channelLabel")}</Label>
            {channelNames.length > 0 ? (
              <Select value={channel || "__none__"}
                onValueChange={(v) => { setChannel(v === "__none__" ? "" : v); setTo(""); }}>
                <SelectTrigger className="text-base md:text-sm">
                  <SelectValue placeholder={t("detail.channelPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none__">{t("detail.channelPlaceholder")}</SelectItem>
                  {channelNames.map((ch) => (
                    <SelectItem key={ch} value={ch}>{ch}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <Input value={channel} onChange={(e) => setChannel(e.target.value)}
                placeholder={t("detail.channelPlaceholder")} className="text-base md:text-sm" />
            )}
          </div>
          <div className="space-y-2 min-w-0">
            <Label>{t("detail.toLabel")}</Label>
            {(() => {
              if (!channel) {
                return <Input placeholder={t("detail.channelPlaceholder")} disabled className="text-base md:text-sm" />;
              }
              const filtered = targets.filter((tgt) => tgt.channel === channel);
              if (filtered.length > 0) {
                return (
                  <Select value={to || "__none__"} onValueChange={(v) => setTo(v === "__none__" ? "" : v)}>
                    <SelectTrigger className="text-base md:text-sm">
                      <SelectValue placeholder={t("detail.toPlaceholder")} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__none__">{t("detail.toPlaceholder")}</SelectItem>
                      {filtered.map((tgt) => (
                        <SelectItem key={tgt.chatId} value={tgt.chatId}
                          title={tgt.title ? `${tgt.title} (${tgt.chatId})` : tgt.chatId}>
                          <span className="truncate">{tgt.title ? `${tgt.title} (${tgt.chatId})` : tgt.chatId}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                );
              }
              return <Input value={to} onChange={(e) => setTo(e.target.value)}
                placeholder={t("detail.toPlaceholder")} className="text-base md:text-sm" />;
            })()}
          </div>
        </div>
      )}

      <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
        <div>
          <p className="text-sm font-medium">{t("detail.wakeHeartbeat")}</p>
          <p className="text-xs text-muted-foreground">{t("detail.wakeHeartbeatDesc")}</p>
        </div>
        <Switch checked={wakeHeartbeat} onCheckedChange={setWakeHeartbeat} disabled={readonly} />
      </div>
    </section>
  );
}
