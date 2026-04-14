import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Save, Settings, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";
import { Combobox } from "@/components/ui/combobox";
import { getAllIanaTimezones, isValidIanaTimezone } from "@/lib/constants";
import { toast } from "@/stores/use-toast-store";
import { useChannels } from "@/pages/channels/hooks/use-channels";
import { useWs } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import type { CronJob, CronJobPatch } from "../hooks/use-cron";
import { cronAdvancedSchema, type CronAdvancedFormData } from "@/schemas/cron-advanced.schema";

interface DeliveryTarget {
  channel: string;
  chatId: string;
  title?: string;
  kind: string;
}

interface CronAdvancedDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  job: CronJob;
  onUpdate?: (id: string, params: CronJobPatch) => Promise<void>;
}

function deriveDefaults(job: CronJob): CronAdvancedFormData {
  return {
    timezone: job.schedule.tz ?? "UTC",
    deliver: job.deliver ?? false,
    channel: job.deliverChannel ?? "",
    to: job.deliverTo ?? "",
    wakeHeartbeat: job.wakeHeartbeat ?? false,
    deleteAfterRun: job.deleteAfterRun ?? false,
    stateless: job.stateless ?? false,
  };
}

export function CronAdvancedDialog({ open, onOpenChange, job, onUpdate }: CronAdvancedDialogProps) {
  const { t } = useTranslation("cron");
  const { t: tc } = useTranslation("common");
  const ws = useWs();
  const { channels: availableChannels } = useChannels();
  const channelNames = Object.keys(availableChannels);

  // UI-only state
  const [saving, setSaving] = useState(false);
  const [targets, setTargets] = useState<DeliveryTarget[]>([]);

  const form = useForm<CronAdvancedFormData>({
    resolver: zodResolver(cronAdvancedSchema),
    mode: "onChange",
    defaultValues: deriveDefaults(job),
  });

  const { watch, setValue, reset } = form;
  const timezone = watch("timezone");
  const deliver = watch("deliver");
  const channel = watch("channel");
  const to = watch("to");
  const wakeHeartbeat = watch("wakeHeartbeat");
  const deleteAfterRun = watch("deleteAfterRun");
  const stateless = watch("stateless");

  const fetchTargets = useCallback(async () => {
    if (!job.agentId || !ws.isConnected) return;
    try {
      const res = await ws.call<{ targets: DeliveryTarget[] }>(
        Methods.HEARTBEAT_TARGETS, { agentId: job.agentId },
      );
      setTargets(res.targets ?? []);
    } catch { /* ignore — fallback to Input */ }
  }, [ws, job.agentId]);

  // Re-sync when dialog opens
  useEffect(() => {
    if (!open) return;
    reset(deriveDefaults(job));
    fetchTargets();
     
  }, [open, job, reset, fetchTargets]);

  const handleSave = async () => {
    if (!onUpdate) {
      onOpenChange(false);
      return;
    }
    const data = form.getValues();
    if (data.timezone && data.timezone !== "UTC" && !isValidIanaTimezone(data.timezone)) {
      toast.error(t("detail.invalidTimezone", "Invalid timezone"));
      return;
    }
    setSaving(true);
    try {
      await onUpdate(job.id, {
        schedule: {
          ...job.schedule,
          tz: data.timezone !== "UTC" ? data.timezone : "",
        },
        deliver: data.deliver,
        deliverChannel: data.deliver ? data.channel.trim() || undefined : undefined,
        deliverTo: data.deliver ? data.to.trim() || undefined : undefined,
        wakeHeartbeat: data.wakeHeartbeat,
        deleteAfterRun: data.deleteAfterRun,
        stateless: data.stateless,
      });
      onOpenChange(false);
    } catch { // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings className="h-4 w-4" />
            {t("detail.advanced")}
          </DialogTitle>
        </DialogHeader>

        {/* Scrollable body */}
        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4">

          {/* Scheduling */}
          <ConfigGroupHeader
            title={t("detail.scheduling")}
            description={t("detail.schedulingDesc")}
          />
          <div className="space-y-2">
            <Label htmlFor="adv-timezone">{t("detail.timezone")}</Label>
            <Combobox
              value={timezone}
              onChange={(v) => setValue("timezone", v)}
              options={getAllIanaTimezones()}
              placeholder={t("detail.timezone")}
              className="text-base md:text-sm"
            />
            <p className="text-xs text-muted-foreground">{t("detail.timezoneDesc")}</p>
          </div>

          {/* Delivery */}
          <ConfigGroupHeader
            title={t("detail.delivery")}
            description={t("detail.deliveryDesc")}
          />
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
              <p className="text-sm font-medium">{t("detail.deliverToChannel")}</p>
              <Switch checked={deliver} onCheckedChange={(v) => setValue("deliver", v)} />
            </div>

            {deliver && (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-[140px_1fr]">
                <div className="space-y-2 min-w-0">
                  <Label>{t("detail.channelLabel")}</Label>
                  {channelNames.length > 0 ? (
                    <Select
                      value={channel || "__none__"}
                      onValueChange={(v) => {
                        setValue("channel", v === "__none__" ? "" : v);
                        setValue("to", "");
                      }}
                    >
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
                    <Input
                      value={channel}
                      onChange={(e) => setValue("channel", e.target.value)}
                      placeholder={t("detail.channelPlaceholder")}
                      className="text-base md:text-sm"
                    />
                  )}
                </div>
                <div className="space-y-2 min-w-0">
                  <Label>{t("detail.toLabel")}</Label>
                  {(() => {
                    if (!channel) {
                      return (
                        <Input
                          placeholder={t("detail.channelPlaceholder")}
                          disabled
                          className="text-base md:text-sm"
                        />
                      );
                    }
                    const filtered = targets.filter((tgt) => tgt.channel === channel);
                    if (filtered.length > 0) {
                      return (
                        <Select
                          value={to || "__none__"}
                          onValueChange={(v) => setValue("to", v === "__none__" ? "" : v)}
                        >
                          <SelectTrigger className="text-base md:text-sm">
                            <SelectValue placeholder={t("detail.toPlaceholder")} />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="__none__">{t("detail.toPlaceholder")}</SelectItem>
                            {filtered.map((tgt) => (
                              <SelectItem key={tgt.chatId} value={tgt.chatId} title={tgt.title ? `${tgt.title} (${tgt.chatId})` : tgt.chatId}>
                                <span className="truncate">{tgt.title ? `${tgt.title} (${tgt.chatId})` : tgt.chatId}</span>
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      );
                    }
                    return (
                      <Input
                        value={to}
                        onChange={(e) => setValue("to", e.target.value)}
                        placeholder={t("detail.toPlaceholder")}
                        className="text-base md:text-sm"
                      />
                    );
                  })()}
                </div>
              </div>
            )}

            <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
              <div>
                <p className="text-sm font-medium">{t("detail.wakeHeartbeat")}</p>
                <p className="text-xs text-muted-foreground">{t("detail.wakeHeartbeatDesc")}</p>
              </div>
              <Switch checked={wakeHeartbeat} onCheckedChange={(v) => setValue("wakeHeartbeat", v)} />
            </div>
          </div>

          {/* Lifecycle */}
          <ConfigGroupHeader
            title={t("detail.lifecycle")}
            description={t("detail.lifecycleDesc")}
          />
          <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
            <div>
              <p className="text-sm font-medium">{t("detail.deleteAfterRun")}</p>
              <p className="text-xs text-muted-foreground">{t("detail.deleteAfterRunDesc")}</p>
            </div>
            <Switch checked={deleteAfterRun} onCheckedChange={(v) => setValue("deleteAfterRun", v)} />
          </div>

          <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
            <div>
              <p className="text-sm font-medium">{t("stateless")}</p>
              <p className="text-xs text-muted-foreground">{t("statelessHelp")}</p>
            </div>
            <Switch checked={stateless} onCheckedChange={(v) => setValue("stateless", v)} />
          </div>

        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 pt-4 border-t shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {tc("cancel")}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {saving ? tc("saving") : tc("save")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
