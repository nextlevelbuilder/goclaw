import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { TooltipProvider } from "@/components/ui/tooltip";
import { InfoTip } from "@/pages/setup/info-tip";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { CHANNEL_TYPES } from "@/constants/channels";
import { credentialsSchema } from "@/pages/channels/channel-schemas";
import { ChannelFields } from "@/pages/channels/channel-fields";
import { useChannelInstances } from "@/pages/channels/hooks/use-channel-instances";
import { slugify } from "@/lib/slug";
import type { AgentData } from "@/types/agent";

interface StepChannelProps {
  agent: AgentData | null;
  onComplete: () => void;
  onSkip: () => void;
  onBack?: () => void;
}

export function StepChannel({ agent, onComplete, onSkip, onBack }: StepChannelProps) {
  const { t } = useTranslation("setup");
  const { createInstance } = useChannelInstances();

  const [channelType, setChannelType] = useState("telegram");
  const [name, setName] = useState("telegram");
  const [displayName, setDisplayName] = useState("");
  const [credsValues, setCredsValues] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const credsFields = credentialsSchema[channelType] ?? [];

  const handleTypeChange = (value: string) => {
    setChannelType(value);
    setName(slugify(value));
    setCredsValues({});
    setError("");
  };

  const handleCredsChange = useCallback((key: string, value: unknown) => {
    setCredsValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const handleCreate = async () => {
    if (!agent) { setError(t("channel.errors.noAgent")); return; }

    const missing = credsFields.filter((f) => f.required && !credsValues[f.key]);
    if (missing.length > 0) {
      setError(t("channel.errors.requiredFields", { fields: missing.map((f) => f.label).join(", ") }));
      return;
    }

    setLoading(true);
    setError("");

    try {
      const cleanCreds = Object.fromEntries(
        Object.entries(credsValues).filter(([, v]) => v !== undefined && v !== "" && v !== null),
      );

      await createInstance({
        name: name.trim(),
        display_name: displayName.trim() || undefined,
        channel_type: channelType,
        agent_id: agent.id,
        credentials: Object.keys(cleanCreds).length > 0 ? cleanCreds : undefined,
        config: {
          dm_policy: "pairing",
          group_policy: "pairing",
          ...(channelType === "telegram" && { reaction_level: "full" }),
        },
        enabled: true,
      });
      onComplete();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("channel.errors.failedCreate"));
    } finally {
      setLoading(false);
    }
  };

  const agentLabel = agent?.display_name || agent?.agent_key || "—";

  return (
    <Card>
      <CardContent className="space-y-4 pt-6">
        <TooltipProvider>
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <h2 className="text-lg font-semibold">{t("channel.title")}</h2>
            <p className="text-sm text-muted-foreground">
              {t("channel.description")}
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={onSkip}>
            {t("channel.skip")}
          </Button>
        </div>

        {/* Agent badge */}
        <div className="flex items-center gap-2">
          <span className="text-sm text-muted-foreground">{t("channel.agent")}</span>
          <Badge variant="secondary">{agentLabel}</Badge>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label className="inline-flex items-center gap-1.5">
              {t("channel.channelType")}
              <InfoTip text={t("channel.channelTypeHint")} />
            </Label>
            <Select value={channelType} onValueChange={handleTypeChange}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {CHANNEL_TYPES.map((ct) => (
                  <SelectItem key={ct.value} value={ct.value}>{ct.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label className="inline-flex items-center gap-1.5">
              {t("channel.name")}
              <InfoTip text={t("channel.nameHint")} />
            </Label>
            <Input value={name} onChange={(e) => setName(slugify(e.target.value))} placeholder={t("channel.namePlaceholder")} />
          </div>
        </div>

        {displayName !== undefined && (
          <div className="space-y-2">
            <Label className="inline-flex items-center gap-1.5">
              {t("channel.displayName")}
              <InfoTip text={t("channel.displayNameHint")} />
            </Label>
            <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder={t("channel.displayNamePlaceholder")} />
          </div>
        )}

        {credsFields.length > 0 && (
          <fieldset className="rounded-md border p-3 space-y-3">
            <legend className="px-1 text-sm font-medium">{t("channel.credentials")}</legend>
            <ChannelFields fields={credsFields} values={credsValues} onChange={handleCredsChange} idPrefix="setup-cred" />
            <p className="text-xs text-muted-foreground">{t("channel.credentialsHint")}</p>
          </fieldset>
        )}

        {error && <p className="text-sm text-destructive">{error}</p>}

        <div className={`flex ${onBack ? "justify-between" : "justify-end"} gap-2`}>
          {onBack && (
            <Button variant="secondary" onClick={onBack}>
              ← {t("common.back")}
            </Button>
          )}
          <div className="flex gap-2">
            <Button variant="outline" onClick={onSkip} disabled={loading}>
              {t("channel.skipFinish")}
            </Button>
            <Button onClick={handleCreate} disabled={loading}>
              {loading ? t("channel.creating") : t("channel.create")}
            </Button>
          </div>
        </div>
        </TooltipProvider>
      </CardContent>
    </Card>
  );
}
