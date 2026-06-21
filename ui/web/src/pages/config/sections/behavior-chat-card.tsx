import { useEffect, useMemo, useState } from "react";
import { MessageSquareText, Timer } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Methods } from "@/api/protocol";
import { useWs } from "@/hooks/use-ws";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { InfoLabel } from "@/components/shared/info-label";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

const quickAckModeValues = ["sidecar_generated", "llm_generated", "fixed_template", "off"] as const;
type QuickAckMode = (typeof quickAckModeValues)[number];
const intermediateModeValues = ["sidecar_generated", "off"] as const;
type IntermediateMode = (typeof intermediateModeValues)[number];

export interface ChatBehaviorValues {
  enabled?: boolean;
  quick_ack?: {
    enabled?: boolean;
    mode?: QuickAckMode;
    min_delay_ms?: number;
    provider?: string;
    model?: string;
    timeout_ms?: number;
    max_tokens?: number;
    max_chars?: number;
    templates?: string[];
  };
  intermediate_replies?: {
    enabled?: boolean;
    mode?: IntermediateMode;
    provider?: string;
    model?: string;
    timeout_ms?: number;
    max_tokens?: number;
    max_chars?: number;
  };
  final_split?: {
    enabled?: boolean;
    min_chars?: number;
    max_messages?: number;
    delay_ms?: number;
  };
}

interface PreviewResponse {
  ack?: { shouldSend?: boolean; content?: string; source?: string };
  split?: { parts?: string[] };
}

interface Props {
  value: ChatBehaviorValues;
  onChange: (v: ChatBehaviorValues) => void;
}

const sample = [
  "I found the relevant details and will keep this concise.",
  "First, the runtime sends a short acknowledgement only for non-streaming channel replies.",
  "Then the final answer can be split into safe paragraph-sized messages when the text is long enough.",
].join("\n\n");

export function BehaviorChatCard({ value, onChange }: Props) {
  const { t } = useTranslation("config");
  const ws = useWs();
  const [preview, setPreview] = useState<PreviewResponse | null>(null);

  const templatesText = useMemo(() => (value.quick_ack?.templates ?? ["Got it. Working on it..."]).join("\n"), [value.quick_ack?.templates]);

  useEffect(() => {
    const timer = window.setTimeout(async () => {
      try {
        const next = await ws.call<PreviewResponse>(Methods.CHAT_BEHAVIOR_PREVIEW, {
          content: sample,
          isStreaming: false,
          hasToolCalls: true,
          config: value,
        });
        setPreview(next);
      } catch {
        setPreview(null);
      }
    }, 250);
    return () => window.clearTimeout(timer);
  }, [value, ws]);

  const patch = (updates: ChatBehaviorValues) => onChange({ ...value, ...updates });
  const patchAck = (updates: NonNullable<ChatBehaviorValues["quick_ack"]>) =>
    patch({ quick_ack: { ...(value.quick_ack ?? {}), ...updates } });
  const patchIntermediate = (updates: NonNullable<ChatBehaviorValues["intermediate_replies"]>) =>
    patch({ intermediate_replies: { ...(value.intermediate_replies ?? {}), ...updates } });
  const patchSplit = (updates: NonNullable<ChatBehaviorValues["final_split"]>) =>
    patch({ final_split: { ...(value.final_split ?? {}), ...updates } });
  const quickAckMode = value.quick_ack?.mode ?? "llm_generated";

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          <MessageSquareText className="h-4 w-4 text-emerald-500" />
          {t("behavior.chatTitle")}
        </CardTitle>
        <CardDescription>{t("behavior.chatDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <Label>{t("behavior.chatEnabled")}</Label>
            <p className="text-xs text-muted-foreground">{t("behavior.chatEnabledHint")}</p>
          </div>
          <Switch checked={value.enabled ?? false} onCheckedChange={(enabled) => patch({ enabled })} />
        </div>

        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <div className="space-y-3 rounded-md border p-3">
            <div className="flex items-start justify-between gap-4">
              <div>
                <InfoLabel tip={t("behavior.quickAckPurpose")}>{t("behavior.quickAck")}</InfoLabel>
                <p className="text-xs text-muted-foreground">{t("behavior.quickAckHint")}</p>
              </div>
              <Switch
                checked={value.quick_ack?.enabled ?? true}
                onCheckedChange={(enabled) => patchAck({ enabled })}
                disabled={!value.enabled}
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="chat-behavior-ack-mode">{t("behavior.quickAckMode")}</Label>
              <Select
                value={quickAckMode}
                onValueChange={(mode) => patchAck({ mode: mode as QuickAckMode })}
                disabled={!value.enabled}
              >
                <SelectTrigger id="chat-behavior-ack-mode" className="text-base md:text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {quickAckModeValues.map((mode) => (
                    <SelectItem key={mode} value={mode}>{t(`behavior.quickAckMode.${mode}`)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="chat-behavior-ack-delay">{t("behavior.quickAckDelay")}</Label>
              <Input
                id="chat-behavior-ack-delay"
                className="text-base md:text-sm"
                type="number"
                min={0}
                value={value.quick_ack?.min_delay_ms ?? 1000}
                onChange={(e) => patchAck({ min_delay_ms: Number(e.target.value) })}
                disabled={!value.enabled}
              />
            </div>
            <ProviderModelSelect
              provider={value.quick_ack?.provider ?? ""}
              onProviderChange={(provider) => patchAck({ provider, model: "" })}
              model={value.quick_ack?.model ?? ""}
              onModelChange={(model) => patchAck({ model })}
              disabled={!value.enabled}
              allowEmpty
              providerLabel={t("behavior.provider")}
              modelLabel={t("behavior.model")}
              providerPlaceholder={t("behavior.providerPlaceholder")}
              modelPlaceholder={t("behavior.modelPlaceholder")}
            />
            <div className="grid grid-cols-3 gap-3">
              <NumberField label={t("behavior.timeoutMs")} value={value.quick_ack?.timeout_ms ?? 2500} disabled={!value.enabled} onChange={(timeout_ms) => patchAck({ timeout_ms })} />
              <NumberField label={t("behavior.maxTokens")} value={value.quick_ack?.max_tokens ?? 40} disabled={!value.enabled} onChange={(max_tokens) => patchAck({ max_tokens })} />
              <NumberField label={t("behavior.maxChars")} value={value.quick_ack?.max_chars ?? 120} disabled={!value.enabled} onChange={(max_chars) => patchAck({ max_chars })} />
            </div>
            {quickAckMode === "fixed_template" ? (
              <div className="grid gap-1.5">
                <Label htmlFor="chat-behavior-ack-templates">{t("behavior.quickAckTemplates")}</Label>
                <Textarea
                  id="chat-behavior-ack-templates"
                  className="text-base md:text-sm"
                  rows={3}
                  value={templatesText}
                  onChange={(e) => patchAck({ templates: e.target.value.split("\n").map((v) => v.trim()).filter(Boolean) })}
                  disabled={!value.enabled}
                />
              </div>
            ) : null}
          </div>

          <div className="space-y-3 rounded-md border p-3">
            <div className="flex items-start justify-between gap-4">
              <div>
                <InfoLabel tip={t("behavior.intermediatePurpose")}>{t("behavior.intermediate")}</InfoLabel>
                <p className="text-xs text-muted-foreground">{t("behavior.intermediateHint")}</p>
              </div>
              <Switch
                checked={value.intermediate_replies?.enabled ?? false}
                onCheckedChange={(enabled) => patchIntermediate({ enabled })}
                disabled={!value.enabled}
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="chat-behavior-intermediate-mode">{t("behavior.intermediateMode")}</Label>
              <Select
                value={value.intermediate_replies?.mode ?? "sidecar_generated"}
                onValueChange={(mode) => patchIntermediate({ mode: mode as IntermediateMode })}
                disabled={!value.enabled}
              >
                <SelectTrigger id="chat-behavior-intermediate-mode" className="text-base md:text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {intermediateModeValues.map((mode) => (
                    <SelectItem key={mode} value={mode}>{t(`behavior.intermediateMode.${mode}`)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <ProviderModelSelect
              provider={value.intermediate_replies?.provider ?? ""}
              onProviderChange={(provider) => patchIntermediate({ provider, model: "" })}
              model={value.intermediate_replies?.model ?? ""}
              onModelChange={(model) => patchIntermediate({ model })}
              disabled={!value.enabled}
              allowEmpty
              providerLabel={t("behavior.provider")}
              modelLabel={t("behavior.model")}
              providerPlaceholder={t("behavior.providerPlaceholder")}
              modelPlaceholder={t("behavior.modelPlaceholder")}
            />
            <div className="grid grid-cols-3 gap-3">
              <NumberField label={t("behavior.timeoutMs")} value={value.intermediate_replies?.timeout_ms ?? 2500} disabled={!value.enabled} onChange={(timeout_ms) => patchIntermediate({ timeout_ms })} />
              <NumberField label={t("behavior.maxTokens")} value={value.intermediate_replies?.max_tokens ?? 60} disabled={!value.enabled} onChange={(max_tokens) => patchIntermediate({ max_tokens })} />
              <NumberField label={t("behavior.maxChars")} value={value.intermediate_replies?.max_chars ?? 180} disabled={!value.enabled} onChange={(max_chars) => patchIntermediate({ max_chars })} />
            </div>
          </div>

          <div className="space-y-3 rounded-md border p-3">
            <div className="flex items-start justify-between gap-4">
              <div>
                <Label>{t("behavior.finalSplit")}</Label>
                <p className="text-xs text-muted-foreground">{t("behavior.finalSplitHint")}</p>
              </div>
              <Switch
                checked={value.final_split?.enabled ?? true}
                onCheckedChange={(enabled) => patchSplit({ enabled })}
                disabled={!value.enabled}
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <NumberField label={t("behavior.finalSplitMinChars")} value={value.final_split?.min_chars ?? 1200} disabled={!value.enabled} onChange={(min_chars) => patchSplit({ min_chars })} />
              <NumberField label={t("behavior.finalSplitMaxMessages")} value={value.final_split?.max_messages ?? 3} disabled={!value.enabled} onChange={(max_messages) => patchSplit({ max_messages })} />
              <NumberField label={t("behavior.finalSplitDelay")} value={value.final_split?.delay_ms ?? 500} disabled={!value.enabled} onChange={(delay_ms) => patchSplit({ delay_ms })} />
            </div>
          </div>
        </div>

        <div className="rounded-md border bg-muted/30 p-3 text-xs">
          <div className="mb-2 flex items-center gap-2 font-medium">
            <Timer className="h-3.5 w-3.5" />
            {t("behavior.preview")}
          </div>
          <div className="space-y-2 text-muted-foreground">
            <p>{formatAckPreview(preview, t)}</p>
            <p>{t("behavior.previewParts", { count: preview?.split?.parts?.length ?? 1 })}</p>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function formatAckPreview(preview: PreviewResponse | null, t: (key: string, options?: any) => string) {
  const ack = preview?.ack;
  if (!ack?.shouldSend) return t("behavior.previewNoAck");
  if (ack.source === "generated") {
    return t("behavior.previewGeneratedAck");
  }
  return `${t("behavior.previewAck")}: ${ack.content ?? ""}`;
}

function NumberField({ label, value, disabled, onChange }: { label: string; value: number; disabled: boolean; onChange: (v: number) => void }) {
  return (
    <div className="grid gap-1.5">
      <Label>{label}</Label>
      <Input className="text-base md:text-sm" type="number" min={0} value={value} disabled={disabled} onChange={(e) => onChange(Number(e.target.value))} />
    </div>
  );
}
