import { useState, useMemo, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Combobox } from "@/components/ui/combobox";
import type { AgentData } from "@/types/agent";
import { slugify, isValidSlug } from "@/lib/slug";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";
import { useProviderVerify } from "@/pages/providers/hooks/use-provider-verify";
import { useAgentPresets } from "./agent-presets";

interface AgentCreateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (data: Partial<AgentData>) => Promise<unknown>;
}

export function AgentCreateDialog({ open, onOpenChange, onCreate }: AgentCreateDialogProps) {
  const { t } = useTranslation("agents");
  const agentPresets = useAgentPresets();
  const { providers } = useProviders();
  const [emoji, setEmoji] = useState("");
  const [agentKey, setAgentKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [displayName, setDisplayName] = useState("");
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [agentType, setAgentType] = useState<"open" | "predefined">("predefined");
  const [description, setDescription] = useState("");
  const [selfEvolve, setSelfEvolve] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const enabledProviders = providers.filter((p) => p.enabled);

  // Look up provider ID from selected provider name for model fetching
  const selectedProvider = useMemo(
    () => enabledProviders.find((p) => p.name === provider),
    [enabledProviders, provider],
  );
  const selectedProviderId = selectedProvider?.id;
  const { models, loading: modelsLoading } = useProviderModels(selectedProviderId);
  const { verify, verifying, result: verifyResult, reset: resetVerify } = useProviderVerify();

  // Reset verification when provider or model changes
  useEffect(() => {
    resetVerify();
  }, [provider, model, resetVerify]);

  const handleVerify = async () => {
    if (!selectedProviderId || !model.trim()) return;
    await verify(selectedProviderId, model.trim());
  };

  const handleVerifyAndCreate = async () => {
    if (!selectedProviderId || !model.trim()) return;
    const res = await verify(selectedProviderId, model.trim());
    if (res?.valid) await handleCreate();
  };

  const handleCreate = async () => {
    if (!agentKey.trim()) return;
    setLoading(true);
    setError("");
    try {
      const otherConfig: Record<string, unknown> = {};
      if (emoji.trim()) otherConfig.emoji = emoji.trim();
      if (description.trim()) otherConfig.description = description.trim();
      if (selfEvolve) otherConfig.self_evolve = true;
      await onCreate({
        agent_key: agentKey.trim(),
        display_name: displayName.trim() || undefined,
        provider: provider.trim(),
        model: model.trim(),
        agent_type: agentType,
        other_config: Object.keys(otherConfig).length > 0 ? otherConfig : undefined,
      });
      onOpenChange(false);
      setEmoji("");
      setAgentKey("");
      setKeyTouched(false);
      setDisplayName("");
      setProvider("");
      setModel("");
      setAgentType("open");
      setDescription("");
      setSelfEvolve(false);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("create.failedToCreate"));
    } finally {
      setLoading(false);
    }
  };

  const handleProviderChange = (value: string) => {
    setProvider(value);
    setModel("");
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl max-h-[90vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("create.title")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-4 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="displayName">{t("create.displayName")}</Label>
              <div className="flex gap-2">
                <Input
                  id="emoji"
                  value={emoji}
                  onChange={(e) => setEmoji(e.target.value)}
                  placeholder="🤖"
                  className="w-14 shrink-0 text-center text-lg"
                  maxLength={2}
                  title={t("create.emojiHint")}
                />
                <Input
                  id="displayName"
                  value={displayName}
                  onChange={(e) => setDisplayName(e.target.value)}
                  onBlur={() => {
                    if (!keyTouched && displayName.trim()) {
                      setAgentKey(slugify(displayName.trim()));
                    }
                  }}
                  placeholder={t("create.displayNamePlaceholder")}
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="agentKey">{t("create.agentKey")}</Label>
              <Input
                id="agentKey"
                value={agentKey}
                onChange={(e) => {
                  setKeyTouched(true);
                  setAgentKey(e.target.value);
                }}
                onBlur={() => setAgentKey(slugify(agentKey))}
                placeholder={t("create.agentKeyPlaceholder")}
              />
              <p className="text-xs text-muted-foreground">{t("create.agentKeyHint")}</p>
            </div>
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label>{t("create.provider")}</Label>
              {enabledProviders.length > 0 ? (
                <Select value={provider} onValueChange={handleProviderChange}>
                  <SelectTrigger>
                    <SelectValue placeholder={t("create.selectProvider")} />
                  </SelectTrigger>
                  <SelectContent>
                    {enabledProviders.map((p) => (
                      <SelectItem key={p.name} value={p.name}>
                        {p.display_name || p.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                  placeholder="openrouter"
                />
              )}
            </div>
            <div className="space-y-2">
              <Label>{t("create.model")}</Label>
              <div className="flex gap-2">
                <div className="flex-1">
                  <Combobox
                    value={model}
                    onChange={setModel}
                    options={models.map((m) => ({ value: m.id, label: m.name }))}
                    placeholder={modelsLoading ? t("create.loadingModels") : t("create.enterOrSelectModel")}
                  />
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-9 px-3"
                  disabled={!selectedProviderId || !model.trim() || verifying}
                  onClick={handleVerify}
                >
                  {verifying ? "..." : t("create.check")}
                </Button>
              </div>
              {verifyResult && (
                <p className={`text-xs ${verifyResult.valid ? "text-success" : "text-destructive"}`}>
                  {verifyResult.valid ? t("create.modelVerified") : verifyResult.error || t("create.verificationFailed")}
                </p>
              )}
              {!verifyResult && provider && !modelsLoading && models.length === 0 && (
                <p className="text-xs text-muted-foreground">{t("create.noModelsHint")}</p>
              )}
            </div>
          </div>
          <div className="space-y-2">
            <Label>{t("create.agentType")}</Label>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setAgentType("predefined")}
                className={`flex-1 rounded-md border px-3 py-2 text-sm font-medium transition-colors ${
                  agentType === "predefined"
                    ? "border-primary bg-primary text-primary-foreground"
                    : "border-input bg-background hover:bg-accent"
                }`}
              >
                {t("create.predefined")}
                <span className="block text-xs font-normal opacity-70">{t("create.predefinedSubLabel")}</span>
              </button>
              <button
                type="button"
                onClick={() => setAgentType("open")}
                className={`flex-1 rounded-md border px-3 py-2 text-sm font-medium transition-colors ${
                  agentType === "open"
                    ? "border-primary bg-primary text-primary-foreground"
                    : "border-input bg-background hover:bg-accent"
                }`}
              >
                {t("create.open")}
                <span className="block text-xs font-normal opacity-70">{t("create.openSubLabel")}</span>
              </button>
            </div>
          </div>

          {agentType === "predefined" && (
            <div className="space-y-3">
              <Label>{t("create.describeAgent")}</Label>
              <div className="flex flex-wrap gap-1.5">
                {agentPresets.map((preset) => (
                  <button
                    key={preset.label}
                    type="button"
                    onClick={() => setDescription(preset.prompt)}
                    className="rounded-full border px-2.5 py-0.5 text-xs transition-colors hover:bg-accent"
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t("create.descriptionPlaceholder")}
                className="min-h-[120px]"
              />
              <p className="text-xs text-muted-foreground">
                {t("create.descriptionHint")}
              </p>
              <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
                <div className="space-y-0.5">
                  <Label htmlFor="create-self-evolve" className="text-sm font-normal">{t("create.selfEvolution")}</Label>
                  <p className="text-xs text-muted-foreground">{t("create.selfEvolutionHint")}</p>
                </div>
                <Switch id="create-self-evolve" checked={selfEvolve} onCheckedChange={setSelfEvolve} />
              </div>
            </div>
          )}
          {error && (
            <p className="text-sm text-destructive">{error}</p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {t("create.cancel")}
          </Button>
          {loading ? (
            <Button disabled>{t("create.creating")}</Button>
          ) : !verifyResult?.valid && selectedProviderId && model.trim() ? (
            <Button onClick={handleVerifyAndCreate} disabled={verifying || !displayName.trim() || !agentKey.trim() || !isValidSlug(agentKey) || (agentType === "predefined" && !description.trim())}>
              {verifying ? t("create.checking") : t("create.checkAndCreate")}
            </Button>
          ) : (
            <Button onClick={handleCreate} disabled={!displayName.trim() || !agentKey.trim() || !isValidSlug(agentKey) || !provider.trim() || !model.trim() || !verifyResult?.valid || (agentType === "predefined" && !description.trim())}>
              {t("create.create")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
