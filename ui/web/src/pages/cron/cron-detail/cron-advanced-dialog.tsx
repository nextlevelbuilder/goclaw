import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Save, Settings, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";
import { IANA_TIMEZONES } from "@/lib/constants";
import type { CronJob } from "../hooks/use-cron";

interface CronAdvancedDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  job: CronJob;
  onUpdate?: (id: string, params: Record<string, unknown>) => Promise<void>;
}

function deriveState(job: CronJob) {
  return {
    timezone: job.schedule.tz ?? "UTC",
    deliver: job.payload?.deliver ?? false,
    channel: job.payload?.channel ?? "",
    to: job.payload?.to ?? "",
    wakeHeartbeat: Boolean((job as unknown as Record<string, unknown>).wakeHeartbeat),
    deleteAfterRun: job.deleteAfterRun ?? false,
  };
}

export function CronAdvancedDialog({ open, onOpenChange, job, onUpdate }: CronAdvancedDialogProps) {
  const { t } = useTranslation("cron");
  const { t: tc } = useTranslation("common");

  const init = deriveState(job);
  const [timezone, setTimezone] = useState(init.timezone);
  const [deliver, setDeliver] = useState(init.deliver);
  const [channel, setChannel] = useState(init.channel);
  const [to, setTo] = useState(init.to);
  const [wakeHeartbeat, setWakeHeartbeat] = useState(init.wakeHeartbeat);
  const [deleteAfterRun, setDeleteAfterRun] = useState(init.deleteAfterRun);
  const [saving, setSaving] = useState(false);

  // Re-sync when dialog opens
  useEffect(() => {
    if (!open) return;
    const s = deriveState(job);
    setTimezone(s.timezone);
    setDeliver(s.deliver);
    setChannel(s.channel);
    setTo(s.to);
    setWakeHeartbeat(s.wakeHeartbeat);
    setDeleteAfterRun(s.deleteAfterRun);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const handleSave = async () => {
    if (!onUpdate) {
      onOpenChange(false);
      return;
    }
    setSaving(true);
    try {
      await onUpdate(job.id, {
        schedule: {
          ...job.schedule,
          tz: timezone !== "UTC" ? timezone : undefined,
        },
        deliver,
        channel: deliver ? channel.trim() || undefined : undefined,
        to: deliver ? to.trim() || undefined : undefined,
        wakeHeartbeat,
        deleteAfterRun,
      });
      onOpenChange(false);
    } catch { // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-2xl">
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
            <Select value={timezone} onValueChange={setTimezone}>
              <SelectTrigger id="adv-timezone" className="text-base md:text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {IANA_TIMEZONES.map((tz) => (
                  <SelectItem key={tz.value} value={tz.value}>
                    {tz.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
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
              <Switch checked={deliver} onCheckedChange={setDeliver} />
            </div>

            {deliver && (
              <>
                <div className="space-y-2">
                  <Label htmlFor="adv-channel">{t("detail.channelLabel")}</Label>
                  <Input
                    id="adv-channel"
                    value={channel}
                    onChange={(e) => setChannel(e.target.value)}
                    placeholder={t("detail.channelPlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="adv-to">{t("detail.toLabel")}</Label>
                  <Input
                    id="adv-to"
                    value={to}
                    onChange={(e) => setTo(e.target.value)}
                    placeholder={t("detail.toPlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
              </>
            )}

            <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
              <div>
                <p className="text-sm font-medium">{t("detail.wakeHeartbeat")}</p>
                <p className="text-xs text-muted-foreground">{t("detail.wakeHeartbeatDesc")}</p>
              </div>
              <Switch checked={wakeHeartbeat} onCheckedChange={setWakeHeartbeat} />
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
            <Switch checked={deleteAfterRun} onCheckedChange={setDeleteAfterRun} />
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
