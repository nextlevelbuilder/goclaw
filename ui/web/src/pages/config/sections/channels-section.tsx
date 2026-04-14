import { useState, useEffect } from "react";
import { Save, ChevronDown, ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { MultiUserPicker } from "@/components/shared/multi-user-picker";

 
type ChannelsData = Record<string, any>;

const CHANNEL_META: Record<string, { label: string; secretEnv: string; secretField: string }> = {
  telegram: { label: "Telegram", secretEnv: "GOCLAW_TELEGRAM_TOKEN", secretField: "token" },
  discord: { label: "Discord", secretEnv: "GOCLAW_DISCORD_TOKEN", secretField: "token" },
  slack: { label: "Slack", secretEnv: "GOCLAW_SLACK_BOT_TOKEN", secretField: "bot_token" },
  whatsapp: { label: "WhatsApp", secretEnv: "", secretField: "" },
  zalo: { label: "Zalo", secretEnv: "GOCLAW_ZALO_TOKEN", secretField: "token" },
  feishu: { label: "Feishu / Lark", secretEnv: "GOCLAW_FEISHU_APP_SECRET", secretField: "app_secret" },
};

const DM_POLICIES = ["pairing", "allowlist", "open", "disabled"];
const GROUP_POLICIES = ["open", "pairing", "allowlist", "disabled"];

function isSecret(val: unknown): boolean {
  return typeof val === "string" && val.includes("***");
}

interface Props {
  data: ChannelsData | undefined;
  onSave: (value: ChannelsData) => Promise<void>;
  saving: boolean;
}

export function ChannelsSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<ChannelsData>(data ?? {});
  const [dirty, setDirty] = useState(false);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  useEffect(() => {
    setDraft(data ?? {});
    setDirty(false);
  }, [data]);

  const updateChannel = (ch: string, patch: Record<string, any>) => {
    setDraft((prev) => ({
      ...prev,
      [ch]: { ...(prev[ch] ?? {}), ...patch },
    }));
    setDirty(true);
  };

  const toggle = (key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const handleSave = () => {
    // Strip masked secret fields
    const toSave: ChannelsData = {};
    for (const [ch, val] of Object.entries(draft)) {
      const copy = { ...val };
      const meta = CHANNEL_META[ch];
      if (meta?.secretField && isSecret(copy[meta.secretField])) {
        delete copy[meta.secretField];
      }
      // Also strip other known secret fields
      for (const key of Object.keys(copy)) {
        if (isSecret(copy[key])) delete copy[key];
      }
      toSave[ch] = copy;
    }
    onSave(toSave);
  };

  if (!data) return null;

  const activeChannels = Object.keys(CHANNEL_META).filter((ch) => data[ch] != null);

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("channels.title")}</CardTitle>
        <CardDescription>{t("channels.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        {activeChannels.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("channels.noChannels")}</p>
        ) : (
          activeChannels.map((ch) => {
            const meta = CHANNEL_META[ch]!;
            const chData = draft[ch] ?? {};
            const isOpen = expanded.has(ch);

            return (
              <div key={ch} className="rounded-md border">
                <button
                  type="button"
                  className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-sm hover:bg-muted/50"
                  onClick={() => toggle(ch)}
                >
                  {isOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                  <span className="font-medium">{meta.label}</span>
                  <Badge variant={chData.enabled ? "default" : "secondary"} className="ml-auto text-xs">
                    {chData.enabled ? t("common:enabled", "Enabled") : t("common:disabled", "Disabled")}
                  </Badge>
                </button>
                {isOpen && (
                  <div className="space-y-3 border-t px-3 py-3">
                    <div className="flex items-center justify-between">
                      <Label>{t("channels.enabled")}</Label>
                      <Switch
                        checked={chData.enabled ?? false}
                        onCheckedChange={(v) => updateChannel(ch, { enabled: v })}
                      />
                    </div>

                    {/* Secret token */}
                    {meta.secretField && chData[meta.secretField] !== undefined && (
                      <div className="grid gap-1.5">
                        <Label>{t("channels.token")}</Label>
                        <Input
                          type="password"
                          value={chData[meta.secretField] ?? ""}
                          disabled={isSecret(chData[meta.secretField])}
                          readOnly={isSecret(chData[meta.secretField])}
                          onChange={(e) => updateChannel(ch, { [meta.secretField]: e.target.value })}
                        />
                        {isSecret(chData[meta.secretField]) && meta.secretEnv && (
                          <p className="text-xs text-muted-foreground">{t("channels.managedVia", { envKey: meta.secretEnv })}</p>
                        )}
                      </div>
                    )}

                    {/* Policies */}
                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                      {chData.dm_policy !== undefined && (
                        <div className="grid gap-1.5">
                          <Label>{t("channels.dmPolicy")}</Label>
                          <Select value={chData.dm_policy ?? "pairing"} onValueChange={(v) => updateChannel(ch, { dm_policy: v })}>
                            <SelectTrigger>
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              {DM_POLICIES.map((p) => (
                                <SelectItem key={p} value={p}>{p}</SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </div>
                      )}
                      {chData.group_policy !== undefined && (
                        <div className="grid gap-1.5">
                          <Label>{t("channels.groupPolicy")}</Label>
                          <Select value={chData.group_policy ?? "open"} onValueChange={(v) => updateChannel(ch, { group_policy: v })}>
                            <SelectTrigger>
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              {GROUP_POLICIES.map((p) => (
                                <SelectItem key={p} value={p}>{p}</SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </div>
                      )}
                    </div>

                    {/* Allow from */}
                    {chData.allow_from !== undefined && (
                      <div className="grid gap-1.5">
                        <Label>{t("channels.allowFrom")}</Label>
                        <MultiUserPicker
                          value={chData.allow_from ?? []}
                          onChange={(v) =>
                            updateChannel(ch, {
                              allow_from: v.length > 0 ? v : undefined,
                            })
                          }
                          placeholder={t("channels.allowFromPlaceholder")}
                        />
                      </div>
                    )}

                    {/* Telegram-specific */}
                    {ch === "telegram" && (
                      <div className="space-y-3">
                        <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">{t("channels.streaming")}</p>
                        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                          <div className="rounded-md border px-3 py-2 flex items-center justify-between">
                            <div>
                              <p className="text-sm font-medium">{t("channels.dmStreaming")}</p>
                              <p className="text-xs text-muted-foreground">{t("channels.dmStreamingHint")}</p>
                            </div>
                            <Switch
                              checked={!!chData.dm_stream}
                              onCheckedChange={(v) => updateChannel(ch, { dm_stream: v })}
                            />
                          </div>
                          <div className="rounded-md border px-3 py-2 flex items-center justify-between">
                            <div>
                              <p className="text-sm font-medium">{t("channels.groupStreaming")}</p>
                              <p className="text-xs text-muted-foreground">{t("channels.groupStreamingHint")}</p>
                            </div>
                            <Switch
                              checked={!!chData.group_stream}
                              onCheckedChange={(v) => updateChannel(ch, { group_stream: v })}
                            />
                          </div>
                          {!!chData.dm_stream && (
                            <div className="rounded-md border px-3 py-2 flex items-center justify-between">
                              <div>
                                <p className="text-sm font-medium">{t("channels.draftTransport")}</p>
                                <p className="text-xs text-muted-foreground">{t("channels.draftTransportHint")}</p>
                              </div>
                              <Switch
                                checked={chData.draft_transport !== false}
                                onCheckedChange={(v) => updateChannel(ch, { draft_transport: v })}
                              />
                            </div>
                          )}
                          {(!!chData.dm_stream || !!chData.group_stream) && (
                            <div className="rounded-md border px-3 py-2 flex items-center justify-between">
                              <div>
                                <p className="text-sm font-medium">{t("channels.reasoningStream")}</p>
                                <p className="text-xs text-muted-foreground">{t("channels.reasoningStreamHint")}</p>
                              </div>
                              <Switch
                                checked={chData.reasoning_stream !== false}
                                onCheckedChange={(v) => updateChannel(ch, { reasoning_stream: v })}
                              />
                            </div>
                          )}
                        </div>
                        {chData.reaction_level !== undefined && (
                          <div className="grid gap-1.5">
                            <Label>{t("channels.reactionLevel")}</Label>
                            <Select value={chData.reaction_level ?? "off"} onValueChange={(v) => updateChannel(ch, { reaction_level: v })}>
                              <SelectTrigger><SelectValue /></SelectTrigger>
                              <SelectContent>
                                <SelectItem value="off">{t("channels.reactionOff")}</SelectItem>
                                <SelectItem value="minimal">{t("channels.reactionMinimal")}</SelectItem>
                                <SelectItem value="full">{t("channels.reactionFull")}</SelectItem>
                              </SelectContent>
                            </Select>
                          </div>
                        )}
                      </div>
                    )}

                    {/* Feishu-specific */}
                    {ch === "feishu" && chData.connection_mode !== undefined && (
                      <div className="grid gap-1.5">
                        <Label>{t("channels.connectionMode")}</Label>
                        <Select value={chData.connection_mode ?? "websocket"} onValueChange={(v) => updateChannel(ch, { connection_mode: v })}>
                          <SelectTrigger><SelectValue /></SelectTrigger>
                          <SelectContent>
                            <SelectItem value="websocket">{t("channels.connectionWebsocket")}</SelectItem>
                            <SelectItem value="webhook">{t("channels.connectionWebhook")}</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })
        )}

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={handleSave} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
