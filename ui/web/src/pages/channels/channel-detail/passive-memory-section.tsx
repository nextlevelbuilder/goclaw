import { useEffect, useMemo, useState } from "react";
import { Brain, ChevronsRight, Play, RefreshCw, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Pagination } from "@/components/shared/pagination";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
  exclude_history_keys: [],
  custom_prompt: "",
  group_custom_prompts: {},
  min_messages: 5,
  group_only: true,
};

interface PassiveMemorySectionProps {
  instanceId: string;
  channelType: string;
}

interface PromptGroupOption {
  history_key: string;
  group_title?: string;
  parent_history_key?: string;
  parent_group_title?: string;
}

export function PassiveMemorySection({ instanceId, channelType }: PassiveMemorySectionProps) {
  const { t } = useTranslation("channels");
  const supportsGroupExclude = channelType === "discord";
  const [reviewPage, setReviewPage] = useState(1);
  const [reviewPageSize, setReviewPageSize] = useState(20);
  const {
    status,
    items,
    itemsTotal,
    loading,
    saveSettings,
    runNow,
    runAll,
    runAllEvents,
    groupOptions,
    itemAction,
  } = useChannelMemoryExtraction(instanceId, { page: reviewPage, pageSize: reviewPageSize, loadGroupOptions: supportsGroupExclude });
  const [config, setConfig] = useState<ChannelMemoryConfig>(fallbackConfig);
  const [channelCustomPrompt, setChannelCustomPrompt] = useState("");
  const [excludeUsers, setExcludeUsers] = useState("");
  const [excludePatterns, setExcludePatterns] = useState("");
  const [selectedPromptHistoryKey, setSelectedPromptHistoryKey] = useState("");

  const promptGroupOptions = useMemo(() => {
    const options: PromptGroupOption[] = [];
    const seen = new Set<string>();
    groupOptions.forEach((group) => {
      if (!group.history_key || seen.has(group.history_key)) return;
      seen.add(group.history_key);
      options.push({
        history_key: group.history_key,
        group_title: group.group_title,
        parent_history_key: group.parent_history_key,
        parent_group_title: group.parent_group_title,
      });
    });
    return options;
  }, [groupOptions]);

  useEffect(() => {
    if (!status?.config) return;
    setConfig(status.config);
    setChannelCustomPrompt(status.config.custom_prompt ?? "");
    setExcludeUsers((status.config.exclude_users ?? []).join("\n"));
    setExcludePatterns((status.config.exclude_patterns ?? []).join("\n"));
  }, [status?.config]);

  useEffect(() => {
    if (promptGroupOptions.length === 0) {
      setSelectedPromptHistoryKey("");
      return;
    }
    if (selectedPromptHistoryKey && promptGroupOptions.some((group) => group.history_key === selectedPromptHistoryKey)) {
      return;
    }
    setSelectedPromptHistoryKey(promptGroupOptions[0]?.history_key ?? "");
  }, [promptGroupOptions, selectedPromptHistoryKey]);

  const pendingItems = useMemo(() => {
    return items.filter((item) => item.status === "pending_review");
  }, [items]);
  const pendingReviewCount = status?.pending_count ?? pendingItems.length;
  const unprocessedMessageCount = status?.unprocessed_message_count ?? 0;
  const reviewTotalPages = Math.max(1, Math.ceil(itemsTotal / reviewPageSize));
  const latestRunAllEvent = runAllEvents[runAllEvents.length - 1] ?? null;
  const visibleRunAllEvents = runAllEvents.filter((event) => event.type !== "final").slice(-5).reverse();
  const excludedHistoryKeys = config.exclude_history_keys ?? [];
  const groupCustomPrompts = config.group_custom_prompts ?? {};
  const selectedPromptGroup = promptGroupOptions.find((group) => group.history_key === selectedPromptHistoryKey);
  const excludedGroupOptions = excludedHistoryKeys.map((historyKey) => (
    promptGroupOptions.find((group) => group.history_key === historyKey) ?? {
      history_key: historyKey,
    }
  ));
  const availableExcludeGroupOptions = promptGroupOptions.filter((group) => {
    return !excludedHistoryKeys.includes(group.history_key) &&
      !excludedHistoryKeys.includes(group.parent_history_key ?? "");
  });

  const updateConfig = (patch: Partial<ChannelMemoryConfig>) => {
    setConfig((current) => ({ ...current, ...patch }));
  };

  const addExcludedHistoryKey = (historyKey: string) => {
    const trimmed = historyKey.trim();
    if (!trimmed || excludedHistoryKeys.includes(trimmed)) return;
    updateConfig({ exclude_history_keys: [...excludedHistoryKeys, trimmed] });
  };

  const updateGroupPrompt = (historyKey: string, value: string) => {
    if (!historyKey) return;
    const next = { ...groupCustomPrompts };
    if (value.trim() === "") {
      delete next[historyKey];
    } else {
      next[historyKey] = value;
    }
    updateConfig({ group_custom_prompts: next });
  };

  const save = () => {
    saveSettings.mutate({
      ...config,
      custom_prompt: channelCustomPrompt,
      group_custom_prompts: supportsGroupExclude ? groupCustomPrompts : {},
      exclude_users: splitLines(excludeUsers),
      exclude_patterns: splitLines(excludePatterns),
      exclude_history_keys: supportsGroupExclude ? excludedHistoryKeys : (config.exclude_history_keys ?? []),
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
            {pendingReviewCount > 0 && (
              <Badge variant="warning">
                {t("detail.passiveMemory.pendingCount", { count: pendingReviewCount })}
              </Badge>
            )}
            {unprocessedMessageCount > 0 && (
              <Badge variant="secondary">
                {t("detail.passiveMemory.unprocessedCount", { count: unprocessedMessageCount })}
              </Badge>
            )}
          </div>
          <p className="mt-1 text-sm text-muted-foreground">
            {t("detail.passiveMemory.description")}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => runNow.mutate()}
            disabled={runNow.isPending || runAll.isPending || loading}
          >
            {runNow.isPending ? <RefreshCw className="animate-spin" /> : <Play />}
            {t("detail.passiveMemory.runNow")}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => runAll.mutate()}
            disabled={runAll.isPending || runNow.isPending || loading}
          >
            {runAll.isPending ? <RefreshCw className="animate-spin" /> : <ChevronsRight />}
            {t("detail.passiveMemory.processAll")}
          </Button>
          <Button size="sm" onClick={save} disabled={saveSettings.isPending}>
            {saveSettings.isPending ? t("detail.passiveMemory.saving") : t("detail.passiveMemory.save")}
          </Button>
        </div>
      </div>

      {(runAll.isPending || runAllEvents.length > 0) && (
        <div className="mt-3 rounded-md border bg-muted/30 p-3 text-xs">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="font-medium text-foreground">{t("detail.passiveMemory.runAllProgress")}</span>
            {latestRunAllEvent && (
              <span className="text-muted-foreground">
                {t("detail.passiveMemory.runAllProgressStats", {
                  runs: latestRunAllEvent.run_count,
                  skipped: latestRunAllEvent.skipped_group_count,
                  errors: latestRunAllEvent.error_count,
                  messages: latestRunAllEvent.message_count,
                })}
              </span>
            )}
          </div>
          {visibleRunAllEvents.length > 0 && (
            <div className="mt-2 space-y-1 text-muted-foreground">
              {visibleRunAllEvents.map((event, index) => (
                <div key={`${event.type}-${event.history_key ?? "final"}-${index}`} className="flex items-center justify-between gap-3">
                  <span className="truncate">
                    {formatRunAllEvent(event, t)}
                  </span>
                  {event.run && (
                    <span className="shrink-0">
                      {t("detail.passiveMemory.runStats", {
                        messages: event.run.message_count,
                        items: event.run.item_count,
                        redactions: event.run.redaction_count,
                      })}
                    </span>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

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
          <TextareaBlock label={t("detail.passiveMemory.channelCustomPrompt")} value={channelCustomPrompt} onChange={setChannelCustomPrompt} />
          {supportsGroupExclude && (
            <div className="space-y-2">
              <div className="space-y-2 rounded-md border p-3">
                <div className="text-xs font-medium text-muted-foreground">
                  {t("detail.passiveMemory.groupCustomPrompt")}
                </div>
                <Select
                  value={selectedPromptHistoryKey}
                  onValueChange={setSelectedPromptHistoryKey}
                  disabled={promptGroupOptions.length === 0}
                >
                  <SelectTrigger size="sm">
                    <SelectValue placeholder={t("detail.passiveMemory.groupCustomPromptPlaceholder")} />
                  </SelectTrigger>
                  <SelectContent>
                    {promptGroupOptions.map((group) => (
                      <SelectItem key={group.history_key} value={group.history_key}>
                        {formatPromptGroupLabel(group)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {selectedPromptHistoryKey ? (
                  <TextareaBlock
                    label={selectedPromptGroup ? formatPromptGroupLabel(selectedPromptGroup) : selectedPromptHistoryKey}
                    value={groupCustomPrompts[selectedPromptHistoryKey] ?? ""}
                    onChange={(value) => updateGroupPrompt(selectedPromptHistoryKey, value)}
                  />
                ) : (
                  <div className="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground">
                    {t("detail.passiveMemory.noPromptGroups")}
                  </div>
                )}
              </div>
              <div className="space-y-2 rounded-md border p-3">
                <div className="text-xs font-medium text-muted-foreground">
                  {t("detail.passiveMemory.excludeGroups")}
                </div>
                <Select
                  value=""
                  onValueChange={addExcludedHistoryKey}
                  disabled={availableExcludeGroupOptions.length === 0}
                >
                  <SelectTrigger size="sm">
                    <SelectValue placeholder={t("detail.passiveMemory.excludeGroupsPlaceholder")} />
                  </SelectTrigger>
                  <SelectContent>
                    {availableExcludeGroupOptions.map((group) => (
                      <SelectItem key={group.history_key} value={group.history_key}>
                        {formatPromptGroupLabel(group)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {excludedGroupOptions.length > 0 ? (
                  <div className="flex flex-wrap gap-2">
                    {excludedGroupOptions.map((group) => {
                      const label = formatPromptGroupLabel(group);
                      return (
                        <Badge key={group.history_key} variant="secondary" className="gap-1 pr-1">
                          <span className="max-w-[220px] truncate">{label}</span>
                          <button
                            type="button"
                            className="rounded-full p-0.5 hover:bg-background/80"
                            onClick={() => updateConfig({ exclude_history_keys: excludedHistoryKeys.filter((key) => key !== group.history_key) })}
                            aria-label={t("detail.passiveMemory.removeExcludedGroup", { group: label })}
                          >
                            <X className="h-3 w-3" />
                          </button>
                        </Badge>
                      );
                    })}
                  </div>
                ) : (
                  <div className="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground">
                    {promptGroupOptions.length === 0
                      ? t("detail.passiveMemory.noExcludeGroups")
                      : t("detail.passiveMemory.noExcludedGroups")}
                  </div>
                )}
              </div>
            </div>
          )}
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
              items.map((item) => (
                <MemoryItemRow key={item.id} item={item} pending={itemAction.isPending} onAction={(action) => itemAction.mutate({ id: item.id, action })} />
              ))
            )}
            <Pagination
              page={reviewPage}
              pageSize={reviewPageSize}
              total={itemsTotal}
              totalPages={reviewTotalPages}
              pageSizes={[10, 20, 50, 100]}
              onPageChange={setReviewPage}
              onPageSizeChange={(size) => {
                setReviewPageSize(size);
                setReviewPage(1);
              }}
              className="rounded-md border"
            />
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

function formatGroupLabel(group: { history_key: string; group_title?: string }) {
  return group.group_title || group.history_key;
}

export function formatPromptGroupLabel(group: PromptGroupOption) {
  const label = formatGroupLabel(group);
  if (!group.parent_history_key) {
    return label;
  }
  const parentLabel = group.parent_group_title || group.parent_history_key;
  if (!parentLabel || parentLabel === label) {
    return label;
  }
  return `${label} / ${parentLabel}`;
}

function formatRunAllEvent(event: {
  type: string;
  history_key?: string;
  group_message_count?: number;
  error?: string;
}, t: (key: string, opts?: Record<string, unknown>) => string) {
  const historyKey = event.history_key || "-";
  switch (event.type) {
    case "group_completed":
      return t("detail.passiveMemory.groupCompleted", { historyKey });
    case "group_skipped":
      return t("detail.passiveMemory.groupSkipped", { historyKey, messageCount: event.group_message_count ?? 0 });
    case "group_failed":
      return t("detail.passiveMemory.groupFailed", { historyKey, error: event.error || "" });
    default:
      return event.type;
  }
}
