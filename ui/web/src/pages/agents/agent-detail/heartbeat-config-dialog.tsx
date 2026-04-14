import { useState, useEffect, useCallback, useMemo } from "react";
import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Play, Loader2, Heart, Clock, FileText, Cpu } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useChannels } from "@/pages/channels/hooks/use-channels";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useUiStore } from "@/stores/use-ui-store";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";
import { isValidIanaTimezone } from "@/lib/constants";
import { toast } from "@/stores/use-toast-store";
import type { HeartbeatConfig, DeliveryTarget } from "@/pages/agents/hooks/use-agent-heartbeat";
import { HeartbeatScheduleSection } from "./heartbeat-schedule-section";
import { HeartbeatAdvancedPanel } from "./heartbeat-advanced-panel";
import { HeartbeatDeliverySection } from "./heartbeat-delivery-section";
import { heartbeatConfigSchema, type HeartbeatConfigFormData } from "@/schemas/heartbeat.schema";

interface HeartbeatConfigDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  config: HeartbeatConfig | null;
  saving: boolean;
  update: (params: Partial<HeartbeatConfig> & { providerName?: string }) => Promise<void>;
  test: () => Promise<void>;
  getChecklist: () => Promise<string>;
  setChecklist: (content: string) => Promise<void>;
  fetchTargets: () => Promise<DeliveryTarget[]>;
  refresh: () => Promise<void>;
  agentProvider?: string;
  agentModel?: string;
}

function deriveFormDefaults(
  config: HeartbeatConfig | null,
  defaultTz: string,
  providerNameById: Record<string, string>,
): HeartbeatConfigFormData {
  if (config) {
    return {
      enabled: config.enabled,
      intervalMin: Math.round(config.intervalSec / 60),
      ackMaxChars: config.ackMaxChars,
      maxRetries: config.maxRetries,
      isolatedSession: config.isolatedSession,
      lightContext: config.lightContext,
      activeHoursStart: config.activeHoursStart ?? "",
      activeHoursEnd: config.activeHoursEnd ?? "",
      timezone: config.timezone || defaultTz,
      channel: config.channel ?? "",
      chatId: config.chatId ?? "",
      hbProvider: config.providerId ? (providerNameById[config.providerId] ?? "") : "",
      hbModel: config.model ?? "",
      checklist: "",
    };
  }
  return {
    enabled: false,
    intervalMin: 30,
    ackMaxChars: 300,
    maxRetries: 2,
    isolatedSession: false,
    lightContext: false,
    activeHoursStart: "",
    activeHoursEnd: "",
    timezone: defaultTz,
    channel: "",
    chatId: "",
    hbProvider: "",
    hbModel: "",
    checklist: "",
  };
}

export function HeartbeatConfigDialog({
  open, onOpenChange, config, saving, update, test, getChecklist, setChecklist, fetchTargets, refresh,
  agentProvider, agentModel,
}: HeartbeatConfigDialogProps) {
  const { t } = useTranslation("agents");
  const { channels: availableChannels } = useChannels();
  const { providers, refresh: refreshProviders } = useProviders();
  const channelNames = Object.keys(availableChannels);
  const userTz = useUiStore((s) => s.timezone);
  const browserTz = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const defaultTz = userTz && userTz !== "auto" ? userTz : browserTz;

  // Non-form state: checklist tracking, targets, test spinner
  const [originalChecklist, setOriginalChecklist] = useState("");
  const [checklistLoading, setChecklistLoading] = useState(false);
  const [targets, setTargets] = useState<DeliveryTarget[]>([]);
  const [testRunning, setTestRunning] = useState(false);
  const showTestSpin = useMinLoading(testRunning, 600);

  const providerNameById = useMemo(() => {
    const map: Record<string, string> = {};
    for (const p of providers) map[p.id] = p.name;
    return map;
  }, [providers]);

  const form = useForm<HeartbeatConfigFormData>({
    resolver: zodResolver(heartbeatConfigSchema),
    mode: "onChange",
    defaultValues: {
      enabled: false,
      intervalMin: 30,
      ackMaxChars: 300,
      maxRetries: 2,
      isolatedSession: false,
      lightContext: false,
      activeHoursStart: "",
      activeHoursEnd: "",
      timezone: "",
      channel: "",
      chatId: "",
      hbProvider: "",
      hbModel: "",
      checklist: "",
    },
  });

  const { control, register, watch, setValue, formState: { errors } } = form;
  const enabled = watch("enabled");
  const hbProvider = watch("hbProvider") ?? "";
  const hbModel = watch("hbModel") ?? "";
  const channel = watch("channel") ?? "";
  const chatId = watch("chatId") ?? "";
  const activeHoursStart = watch("activeHoursStart") ?? "";
  const activeHoursEnd = watch("activeHoursEnd") ?? "";
  const timezone = watch("timezone") ?? "";
  const ackMaxChars = watch("ackMaxChars");
  const maxRetries = watch("maxRetries");
  const isolatedSession = watch("isolatedSession");
  const lightContext = watch("lightContext");

  const loadChecklist = useCallback(async () => {
    setChecklistLoading(true);
    try {
      const content = await getChecklist();
      setValue("checklist", content);
      setOriginalChecklist(content);
    } catch { /* ignore */ } finally {
      setChecklistLoading(false);
    }
  }, [getChecklist, setValue]);

  useEffect(() => {
    if (!open) return;
    refreshProviders();
    form.reset(deriveFormDefaults(config, defaultTz, providerNameById));
    loadChecklist();
    fetchTargets().then(setTargets)
      .catch((err) => console.error("[HeartbeatConfig] fetch targets failed:", err));
     
  }, [open]);

  const handleTest = async () => {
    setTestRunning(true);
    try { await test(); } finally { setTestRunning(false); }
  };

  const handleSave = form.handleSubmit(async (values) => {
    if (values.timezone && !isValidIanaTimezone(values.timezone)) {
      toast.error(t("heartbeat.invalidTimezone", "Invalid timezone"));
      return;
    }
    try {
      const clampedMin = Math.max(5, values.intervalMin);
      await update({
        enabled: values.enabled,
        intervalSec: clampedMin * 60,
        ackMaxChars: values.ackMaxChars,
        maxRetries: values.maxRetries,
        isolatedSession: values.isolatedSession,
        lightContext: values.lightContext,
        activeHoursStart: values.activeHoursStart || undefined,
        activeHoursEnd: values.activeHoursEnd || undefined,
        timezone: values.timezone || undefined,
        channel: values.channel || undefined,
        chatId: values.chatId || undefined,
        model: values.hbModel || undefined,
        providerName: values.hbProvider || undefined,
      });
      if (values.checklist !== originalChecklist) {
        await setChecklist(values.checklist ?? "");
        setOriginalChecklist(values.checklist ?? "");
      }
      await refresh();
      onOpenChange(false);
    } catch {
      // toast shown by hook — keep dialog open
    }
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Heart className="h-4 w-4 text-rose-500" />
            {t("heartbeat.configTitle")}
            <Badge variant={enabled ? "success" : "secondary"} className="text-2xs">
              {enabled ? t("heartbeat.on") : t("heartbeat.off")}
            </Badge>
          </DialogTitle>
        </DialogHeader>

        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4 overscroll-contain">

          {/* Enable + Interval */}
          <div className="rounded-lg border p-3 space-y-2">
            <div className="flex items-center justify-between gap-4">
              <div className="flex items-center gap-3 min-w-0">
                <Controller
                  control={control}
                  name="enabled"
                  render={({ field }) => (
                    <Switch checked={field.value} onCheckedChange={field.onChange} />
                  )}
                />
                <div className="min-w-0">
                  <span className="text-sm font-medium">{t("heartbeat.enabled")}</span>
                  <p className="text-xs text-muted-foreground">{t("heartbeat.enabledHint")}</p>
                </div>
              </div>
              <div className="flex items-center gap-1.5 shrink-0">
                <Clock className="h-3.5 w-3.5 text-muted-foreground" />
                <Input
                  type="number"
                  min={5}
                  {...register("intervalMin", {
                    valueAsNumber: true,
                    onChange: (e) => setValue("intervalMin", Math.max(5, Number(e.target.value) || 5)),
                  })}
                  className="w-[4.5rem] text-center text-base md:text-sm"
                />
                <span className="text-xs text-muted-foreground">min</span>
              </div>
            </div>
            {errors.intervalMin && (
              <p className="text-xs text-destructive">{errors.intervalMin.message}</p>
            )}
          </div>

          {/* Provider / Model override */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Cpu className="h-3.5 w-3.5 text-orange-500" />
              <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {t("heartbeat.sectionModel")}
              </h4>
            </div>
            <p className="text-xs text-muted-foreground">{t("heartbeat.modelHint")}</p>
            <ProviderModelSelect
              provider={hbProvider}
              onProviderChange={(v) => setValue("hbProvider", v)}
              model={hbModel}
              onModelChange={(v) => setValue("hbModel", v)}
              allowEmpty
              showVerify={!!(hbProvider && hbModel)}
              providerPlaceholder={agentProvider ? `(${agentProvider})` : "(agent default)"}
              modelPlaceholder={agentModel ? `(${agentModel})` : "(agent default)"}
            />
          </div>

          <HeartbeatDeliverySection
            channelNames={channelNames}
            channel={channel} setChannel={(v) => setValue("channel", v)}
            chatId={chatId} setChatId={(v) => setValue("chatId", v)}
            targets={targets}
          />

          <HeartbeatScheduleSection
            activeHoursStart={activeHoursStart} setActiveHoursStart={(v) => setValue("activeHoursStart", v)}
            activeHoursEnd={activeHoursEnd} setActiveHoursEnd={(v) => setValue("activeHoursEnd", v)}
            timezone={timezone} setTimezone={(v) => setValue("timezone", v)}
            defaultTz={defaultTz}
          />

          <HeartbeatAdvancedPanel
            ackMaxChars={ackMaxChars} setAckMaxChars={(v) => setValue("ackMaxChars", v)}
            maxRetries={maxRetries} setMaxRetries={(v) => setValue("maxRetries", v)}
            isolatedSession={isolatedSession} setIsolatedSession={(v) => setValue("isolatedSession", v)}
            lightContext={lightContext} setLightContext={(v) => setValue("lightContext", v)}
          />

          {/* Checklist */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <FileText className="h-3.5 w-3.5 text-emerald-500" />
              <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {t("heartbeat.checklist")}
              </h4>
            </div>
            <p className="text-xs text-muted-foreground">{t("heartbeat.checklistHint")}</p>
            {checklistLoading ? (
              <div className="flex items-center gap-2 text-xs text-muted-foreground py-4 justify-center">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                {t("heartbeat.checklistLoading")}
              </div>
            ) : (
              <Textarea
                {...register("checklist")}
                placeholder={t("heartbeat.checklistPlaceholder")}
                rows={8}
                className="text-base md:text-sm font-mono resize-y min-h-[120px] sm:min-h-[200px]"
              />
            )}
          </div>

          <div className="h-1" />
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between gap-2 border-t pt-3">
          <Button
            variant="outline" size="sm"
            onClick={handleTest}
            disabled={showTestSpin || saving}
            className="gap-1.5"
          >
            {showTestSpin ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
            {t("heartbeat.testRun")}
          </Button>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
              {t("heartbeat.cancel")}
            </Button>
            <Button size="sm" onClick={handleSave} disabled={saving}>
              {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {saving ? t("heartbeat.saving") : t("heartbeat.save")}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
