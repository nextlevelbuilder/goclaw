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

const quickAckModeValues = ["llm_generated", "fixed_template", "off"] as const;
type QuickAckMode = (typeof quickAckModeValues)[number];

export interface ChatBehaviorValues {
  enabled?: boolean;
  quick_ack?: {
    enabled?: boolean;
    mode?: QuickAckMode;
    min_delay_ms?: number;
    templates?: string[];
  };
  final_split?: {
    enabled?: boolean;
    min_chars?: number;
    max_messages?: number;
    delay_ms?: number;
  };
}

interface PreviewResponse {
  ack?: { shouldSend?: boolean; content?: string; fallbackContent?: string; source?: string };
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
  const patchSplit = (updates: NonNullable<ChatBehaviorValues["final_split"]>) =>
    patch({ final_split: { ...(value.final_split ?? {}), ...updates } });

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

        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-3 rounded-md border p-3">
            <div className="flex items-start justify-between gap-4">
              <div>
                <Label>{t("behavior.quickAck")}</Label>
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
                value={value.quick_ack?.mode ?? "llm_generated"}
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
                type="number"
                min={0}
                value={value.quick_ack?.min_delay_ms ?? 1000}
                onChange={(e) => patchAck({ min_delay_ms: Number(e.target.value) })}
                disabled={!value.enabled}
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="chat-behavior-ack-templates">{t("behavior.quickAckTemplates")}</Label>
              <Textarea
                id="chat-behavior-ack-templates"
                rows={3}
                value={templatesText}
                onChange={(e) => patchAck({ templates: e.target.value.split("\n").map((v) => v.trim()).filter(Boolean) })}
                disabled={!value.enabled}
              />
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
    return t("behavior.previewGeneratedAck", { fallback: ack.fallbackContent ?? "" });
  }
  return `${t("behavior.previewAck")}: ${ack.content ?? ""}`;
}

function NumberField({ label, value, disabled, onChange }: { label: string; value: number; disabled: boolean; onChange: (v: number) => void }) {
  return (
    <div className="grid gap-1.5">
      <Label>{label}</Label>
      <Input type="number" min={0} value={value} disabled={disabled} onChange={(e) => onChange(Number(e.target.value))} />
    </div>
  );
}
