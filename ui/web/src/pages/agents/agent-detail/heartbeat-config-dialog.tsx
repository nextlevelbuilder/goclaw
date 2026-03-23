import { useState, useEffect, useCallback, useMemo } from "react";
import { Play, Loader2, ChevronDown, Heart, Clock, Send, FileText, Cpu } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useChannels } from "@/pages/channels/hooks/use-channels";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useUiStore } from "@/stores/use-ui-store";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";
import { IANA_TIMEZONES } from "@/lib/constants";
import type { HeartbeatConfig, DeliveryTarget } from "@/pages/agents/hooks/use-agent-heartbeat";

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
  /** Agent's current provider name (used as default for heartbeat). */
  agentProvider?: string;
  /** Agent's current model (used as default for heartbeat). */
  agentModel?: string;
}

export function HeartbeatConfigDialog({
  open, onOpenChange, config, saving, update, test, getChecklist, setChecklist, fetchTargets, refresh,
  agentProvider, agentModel,
}: HeartbeatConfigDialogProps) {
  const { t } = useTranslation("agents");
  const { channels: availableChannels } = useChannels();
  const { providers } = useProviders();
  const channelNames = Object.keys(availableChannels);
  const userTz = useUiStore((s) => s.timezone);
  const browserTz = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const defaultTz = userTz && userTz !== "auto" ? userTz : browserTz;

  // Form state
  const [enabled, setEnabled] = useState(false);
  const [intervalMin, setIntervalMin] = useState(30);
  const [ackMaxChars, setAckMaxChars] = useState(300);
  const [maxRetries, setMaxRetries] = useState(2);
  const [isolatedSession, setIsolatedSession] = useState(false);
  const [lightContext, setLightContext] = useState(false);
  const [activeHoursStart, setActiveHoursStart] = useState("");
  const [activeHoursEnd, setActiveHoursEnd] = useState("");
  const [timezone, setTimezone] = useState("");
  const [channel, setChannel] = useState("");
  const [chatId, setChatId] = useState("");
  const [hbProvider, setHbProvider] = useState("");
  const [hbModel, setHbModel] = useState("");
  const [checklist, setChecklistState] = useState("");
  const [originalChecklist, setOriginalChecklist] = useState("");
  const [checklistLoading, setChecklistLoading] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [targets, setTargets] = useState<DeliveryTarget[]>([]);

  const [testRunning, setTestRunning] = useState(false);
  const showTestSpin = useMinLoading(testRunning, 600);

  const loadChecklist = useCallback(async () => {
    setChecklistLoading(true);
    try {
      const content = await getChecklist();
      setChecklistState(content);
      setOriginalChecklist(content);
    } catch { /* ignore */ } finally {
      setChecklistLoading(false);
    }
  }, [getChecklist]);

  // Map providerId UUID → provider name for the select component.
  const providerNameById = useMemo(() => {
    const map: Record<string, string> = {};
    for (const p of providers) map[p.id] = p.name;
    return map;
  }, [providers]);

  // Sync form state only when dialog opens (false→true).
  useEffect(() => {
    if (!open) return;
    if (config) {
      setEnabled(config.enabled);
      setIntervalMin(Math.round(config.intervalSec / 60));
      setAckMaxChars(config.ackMaxChars);
      setMaxRetries(config.maxRetries);
      setIsolatedSession(config.isolatedSession);
      setLightContext(config.lightContext);
      setActiveHoursStart(config.activeHoursStart ?? "");
      setActiveHoursEnd(config.activeHoursEnd ?? "");
      setTimezone(config.timezone || defaultTz);
      setChannel(config.channel ?? "");
      setChatId(config.chatId ?? "");
      // Map stored providerId UUID → name for the select
      setHbProvider(config.providerId ? (providerNameById[config.providerId] ?? "") : "");
      setHbModel(config.model ?? "");
    } else {
      // First-time setup defaults
      setTimezone(defaultTz);
      setHbProvider("");
      setHbModel("");
    }
    setShowAdvanced(false);
    loadChecklist();
    fetchTargets().then(setTargets).catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const handleTest = async () => {
    setTestRunning(true);
    try { await test(); } finally { setTestRunning(false); }
  };

  const handleSave = async () => {
    try {
      const clampedMin = Math.max(5, intervalMin);
      await update({
        enabled,
        intervalSec: clampedMin * 60,
        ackMaxChars: ackMaxChars,
        maxRetries: maxRetries,
        isolatedSession: isolatedSession,
        lightContext: lightContext,
        activeHoursStart: activeHoursStart || undefined,
        activeHoursEnd: activeHoursEnd || undefined,
        timezone: timezone || undefined,
        channel: channel || undefined,
        chatId: chatId || undefined,
        model: hbModel || undefined,
        providerName: hbProvider || undefined,
      });
      if (checklist !== originalChecklist) {
        await setChecklist(checklist);
        setOriginalChecklist(checklist);
      }
      await refresh();
      onOpenChange(false);
    } catch {
      // toast shown by hook — keep dialog open
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Heart className="h-4 w-4 text-rose-500" />
            {t("heartbeat.configTitle")}
            <Badge variant={enabled ? "success" : "secondary"} className="text-[10px]">
              {enabled ? t("heartbeat.on") : t("heartbeat.off")}
            </Badge>
          </DialogTitle>
        </DialogHeader>

        {/* Scrollable body — standard pattern: -mx-4 px-4 */}
        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4 overscroll-contain">

          {/* ── Enable + Interval (top priority) ── */}
          <div className="flex items-center justify-between gap-4 rounded-lg border p-3">
            <div className="flex items-center gap-3 min-w-0">
              <Switch checked={enabled} onCheckedChange={setEnabled} />
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
                value={intervalMin}
                onChange={(e) => setIntervalMin(Math.max(5, Number(e.target.value) || 5))}
                className="w-[4.5rem] text-center text-base md:text-sm"
              />
              <span className="text-xs text-muted-foreground">min</span>
            </div>
          </div>

          {/* ── Provider / Model override ── */}
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
              onProviderChange={setHbProvider}
              model={hbModel}
              onModelChange={setHbModel}
              allowEmpty
              showVerify={!!(hbProvider && hbModel)}
              providerPlaceholder={agentProvider ? `(${agentProvider})` : "(agent default)"}
              modelPlaceholder={agentModel ? `(${agentModel})` : "(agent default)"}
            />
          </div>

          {/* ── Delivery — WHERE it sends ── */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Send className="h-3.5 w-3.5 text-blue-500" />
              <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {t("heartbeat.sectionDelivery")}
              </h4>
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label className="text-xs">{t("heartbeat.channel")}</Label>
                {channelNames.length > 0 ? (
                  <Select
                    value={channel || "__none__"}
                    onValueChange={(v) => { setChannel(v === "__none__" ? "" : v); setChatId(""); }}
                  >
                    <SelectTrigger className="text-base md:text-sm">
                      <SelectValue placeholder={t("heartbeat.channelPlaceholder")} />
                    </SelectTrigger>
                    <SelectContent>
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
                    return (
                      <Input
                        placeholder={t("heartbeat.selectChannelFirst")}
                        disabled
                        className="text-base md:text-sm"
                      />
                    );
                  }
                  const filtered = targets.filter((t) => t.channel === channel);
                  if (filtered.length > 0) {
                    return (
                      <Select
                        value={chatId || "__none__"}
                        onValueChange={(v) => setChatId(v === "__none__" ? "" : v)}
                      >
                        <SelectTrigger className="text-base md:text-sm">
                          <SelectValue placeholder={t("heartbeat.chatIdPlaceholder")} />
                        </SelectTrigger>
                        <SelectContent>
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

          {/* ── Schedule — WHEN it runs ── */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Clock className="h-3.5 w-3.5 text-amber-500" />
              <h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {t("heartbeat.sectionSchedule")}
              </h4>
            </div>
            <div className="flex flex-wrap items-end gap-3">
              <div className="space-y-1 w-24">
                <Label htmlFor="hb-start" className="text-xs">{t("heartbeat.activeHoursStart")}</Label>
                <Input
                  id="hb-start"
                  placeholder="08:00"
                  value={activeHoursStart}
                  onChange={(e) => setActiveHoursStart(e.target.value)}
                  className="text-base md:text-sm"
                />
              </div>
              <div className="space-y-1 w-24">
                <Label htmlFor="hb-end" className="text-xs">{t("heartbeat.activeHoursEnd")}</Label>
                <Input
                  id="hb-end"
                  placeholder="22:00"
                  value={activeHoursEnd}
                  onChange={(e) => setActiveHoursEnd(e.target.value)}
                  className="text-base md:text-sm"
                />
              </div>
              <div className="space-y-1 flex-1 min-w-[160px]">
                <Label className="text-xs">{t("heartbeat.timezone")}</Label>
                <Select value={timezone || "__auto__"} onValueChange={(v) => setTimezone(v === "__auto__" ? "" : v)}>
                  <SelectTrigger className="text-base md:text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__auto__">{defaultTz}</SelectItem>
                    {IANA_TIMEZONES.map((tz) => (
                      <SelectItem key={tz.value} value={tz.value}>{tz.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <p className="text-xs text-muted-foreground">{t("heartbeat.scheduleHint")}</p>
          </div>

          {/* ── Advanced (collapsible) ── */}
          <div className="rounded-lg border">
            <button
              type="button"
              onClick={() => setShowAdvanced(!showAdvanced)}
              className="flex w-full items-center justify-between px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
            >
              <span>{t("heartbeat.advancedSettings")}</span>
              <ChevronDown className={`h-3.5 w-3.5 transition-transform ${showAdvanced ? "rotate-180" : ""}`} />
            </button>
            {showAdvanced && (
              <div className="border-t px-3 py-3 space-y-3">
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div className="space-y-1">
                    <Label htmlFor="hb-ack" className="text-xs">{t("heartbeat.ackMaxChars")}</Label>
                    <Input
                      id="hb-ack"
                      type="number"
                      min={0}
                      value={ackMaxChars}
                      onChange={(e) => setAckMaxChars(Number(e.target.value))}
                      className="text-base md:text-sm"
                    />
                    <p className="text-[11px] text-muted-foreground">{t("heartbeat.ackMaxCharsHint")}</p>
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="hb-retries" className="text-xs">{t("heartbeat.maxRetries")}</Label>
                    <Input
                      id="hb-retries"
                      type="number"
                      min={0}
                      max={10}
                      value={maxRetries}
                      onChange={(e) => setMaxRetries(Number(e.target.value))}
                      className="text-base md:text-sm"
                    />
                    <p className="text-[11px] text-muted-foreground">{t("heartbeat.maxRetriesHint")}</p>
                  </div>
                </div>
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <span className="text-xs font-medium">{t("heartbeat.isolatedSession")}</span>
                    <p className="text-[11px] text-muted-foreground">{t("heartbeat.isolatedSessionHint")}</p>
                  </div>
                  <Switch checked={isolatedSession} onCheckedChange={setIsolatedSession} />
                </div>
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <span className="text-xs font-medium">{t("heartbeat.lightContext")}</span>
                    <p className="text-[11px] text-muted-foreground">{t("heartbeat.lightContextHint")}</p>
                  </div>
                  <Switch checked={lightContext} onCheckedChange={setLightContext} />
                </div>
              </div>
            )}
          </div>

          {/* ── Checklist — WHAT the agent does ── */}
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
                value={checklist}
                onChange={(e) => setChecklistState(e.target.value)}
                placeholder={t("heartbeat.checklistPlaceholder")}
                rows={8}
                className="text-base md:text-sm font-mono resize-y min-h-[120px] sm:min-h-[200px]"
              />
            )}
          </div>

          {/* Bottom padding for scroll */}
          <div className="h-1" />
        </div>

        {/* Footer — fixed at bottom */}
        <div className="flex items-center justify-between gap-2 border-t pt-3">
          <Button
            variant="outline"
            size="sm"
            onClick={handleTest}
            disabled={showTestSpin || saving}
            className="gap-1.5"
          >
            {showTestSpin
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : <Play className="h-3.5 w-3.5" />}
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
