import { useEffect, useMemo, useState } from "react";
import { Brain, Play, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { ChannelMemoryConfig } from "@/types/channel";
import { useChannelMemoryExtraction } from "../hooks/use-channel-memory-extraction";
import {
  MemoryItemRow,
  NumberField,
  RunSummary,
  TextareaBlock,
  ToggleRow,
} from "./passive-memory-section-parts";

const memoryTypes = ["people", "projects", "decisions", "todos", "preferences", "events"];

const fallbackConfig: ChannelMemoryConfig = {
  enabled: false,
  review_mode: true,
  interval_minutes: 360,
  message_cap: 100,
  retention_hours: 168,
  allowed_types: memoryTypes,
  exclude_users: [],
  exclude_patterns: [],
  min_messages: 5,
  group_only: true,
};

interface PassiveMemorySectionProps {
  instanceId: string;
}

export function PassiveMemorySection({ instanceId }: PassiveMemorySectionProps) {
  const { t } = useTranslation("channels");
  const { status, items, loading, saveSettings, runNow, itemAction } = useChannelMemoryExtraction(instanceId);
  const [config, setConfig] = useState<ChannelMemoryConfig>(fallbackConfig);
  const [excludeUsers, setExcludeUsers] = useState("");
  const [excludePatterns, setExcludePatterns] = useState("");

  useEffect(() => {
    if (!status?.config) return;
    setConfig(status.config);
    setExcludeUsers((status.config.exclude_users ?? []).join("\n"));
    setExcludePatterns((status.config.exclude_patterns ?? []).join("\n"));
  }, [status?.config]);

  const pendingItems = useMemo(() => {
    return items.filter((item) => item.status === "pending_review");
  }, [items]);

  const updateConfig = (patch: Partial<ChannelMemoryConfig>) => {
    setConfig((current) => ({ ...current, ...patch }));
  };

  const save = () => {
    saveSettings.mutate({
      ...config,
      exclude_users: splitLines(excludeUsers),
      exclude_patterns: splitLines(excludePatterns),
      group_only: true,
    });
  };

  return (
    <section className="rounded-lg border bg-card/60 p-4 shadow-xs">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Brain className="h-4 w-4 text-primary" />
            <h2 className="text-sm font-semibold">{t("detail.passiveMemory.title")}</h2>
            <Badge variant={config.enabled ? "success" : "outline"}>
              {config.enabled ? t("enabled") : t("disabled")}
            </Badge>
            {pendingItems.length > 0 && (
              <Badge variant="warning">
                {t("detail.passiveMemory.pendingCount", { count: pendingItems.length })}
              </Badge>
            )}
          </div>
          <p className="mt-1 text-sm text-muted-foreground">
            {t("detail.passiveMemory.description")}
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => runNow.mutate()}
            disabled={runNow.isPending || loading}
          >
            {runNow.isPending ? <RefreshCw className="animate-spin" /> : <Play />}
            {t("detail.passiveMemory.runNow")}
          </Button>
          <Button size="sm" onClick={save} disabled={saveSettings.isPending}>
            {saveSettings.isPending ? t("detail.passiveMemory.saving") : t("detail.passiveMemory.save")}
          </Button>
        </div>
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-[1fr_1.2fr]">
        <div className="space-y-4">
          <ToggleRow
            label={t("detail.passiveMemory.enable")}
            checked={config.enabled}
            onCheckedChange={(checked) => updateConfig({ enabled: checked })}
          />
          <ToggleRow
            label={t("detail.passiveMemory.reviewMode")}
            checked={config.review_mode}
            onCheckedChange={(checked) => updateConfig({ review_mode: checked })}
          />
          <div className="grid grid-cols-2 gap-3">
            <NumberField label={t("detail.passiveMemory.interval")} value={config.interval_minutes} onChange={(v) => updateConfig({ interval_minutes: v })} />
            <NumberField label={t("detail.passiveMemory.messageCap")} value={config.message_cap} onChange={(v) => updateConfig({ message_cap: v })} />
            <NumberField label={t("detail.passiveMemory.retention")} value={config.retention_hours} onChange={(v) => updateConfig({ retention_hours: v })} />
            <NumberField label={t("detail.passiveMemory.minMessages")} value={config.min_messages} onChange={(v) => updateConfig({ min_messages: v })} />
          </div>
          <div>
            <div className="mb-2 text-xs font-medium text-muted-foreground">
              {t("detail.passiveMemory.types")}
            </div>
            <div className="flex flex-wrap gap-2">
              {memoryTypes.map((type) => {
                const active = config.allowed_types.includes(type);
                return (
                  <button
                    key={type}
                    type="button"
                    className={`rounded-md border px-2.5 py-1 text-xs transition-colors ${active ? "border-primary bg-primary/10 text-primary" : "text-muted-foreground hover:bg-accent"}`}
                    onClick={() => updateConfig({ allowed_types: toggleType(config.allowed_types, type) })}
                  >
                    {t(`detail.passiveMemory.type.${type}`)}
                  </button>
                );
              })}
            </div>
          </div>
          <TextareaBlock label={t("detail.passiveMemory.excludeUsers")} value={excludeUsers} onChange={setExcludeUsers} />
          <TextareaBlock label={t("detail.passiveMemory.excludePatterns")} value={excludePatterns} onChange={setExcludePatterns} />
        </div>

        <div className="space-y-3">
          <RunSummary loading={loading} status={status?.last_run} t={t} />
          <div className="space-y-2">
            <div className="text-xs font-medium text-muted-foreground">
              {t("detail.passiveMemory.reviewQueue")}
            </div>
            {items.length === 0 ? (
              <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
                {t("detail.passiveMemory.noItems")}
              </div>
            ) : (
              items.slice(0, 8).map((item) => (
                <MemoryItemRow key={item.id} item={item} pending={itemAction.isPending} onAction={(action) => itemAction.mutate({ id: item.id, action })} />
              ))
            )}
          </div>
        </div>
      </div>
    </section>
  );
}

function toggleType(values: string[], type: string) {
  return values.includes(type) ? values.filter((value) => value !== type) : [...values, type];
}

function splitLines(value: string) {
  return value.split(/\n|,/).map((part) => part.trim()).filter(Boolean);
}
