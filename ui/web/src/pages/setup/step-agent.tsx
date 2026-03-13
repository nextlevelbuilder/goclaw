import { useState, useEffect, useMemo, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent } from "@/components/ui/card";
import { TooltipProvider } from "@/components/ui/tooltip";
import { InfoTip } from "@/pages/setup/info-tip";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { SummoningModal } from "@/pages/agents/summoning-modal";
import { useAgentPresets } from "@/pages/agents/agent-presets";
import { useWsEvent } from "@/hooks/use-ws-event";
import { slugify, isValidSlug } from "@/lib/slug";
import type { ProviderData } from "@/types/provider";
import type { AgentData } from "@/types/agent";

const DEFAULT_PROMPT = `You are GoClaw, my helpful assistant. I am your boss, NextLevelBuilder.`;

interface StepAgentProps {
  provider: ProviderData | null;
  model: string | null;
  onComplete: (agent: AgentData) => void;
  onBack?: () => void;
  existingAgent?: AgentData | null;
}

export function StepAgent({ provider, model, onComplete, onBack, existingAgent }: StepAgentProps) {
  const { t } = useTranslation("setup");
  const { createAgent, updateAgent, deleteAgent, resummonAgent } = useAgents();
  const agentPresets = useAgentPresets();

  const isEditing = !!existingAgent;

  const [displayName, setDisplayName] = useState(existingAgent?.display_name ?? "GoClaw");
  const [agentKey, setAgentKey] = useState(existingAgent?.agent_key ?? "goclaw");
  const [keyTouched, setKeyTouched] = useState(isEditing);
  const [description, setDescription] = useState(
    existingAgent?.other_config?.description as string ?? DEFAULT_PROMPT,
  );
  const [selfEvolve, setSelfEvolve] = useState(
    !!(existingAgent?.other_config?.self_evolve),
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  // Summoning modal state
  const [summoningOpen, setSummoningOpen] = useState(false);
  const [summoningOutcome, setSummoningOutcome] = useState<"pending" | "success" | "failed">("pending");
  const [createdAgent, setCreatedAgent] = useState<{ id: string; name: string } | null>(null);
  const [agentResult, setAgentResult] = useState<AgentData | null>(null);

  // Model display (from provider created in step 1)
  const providerLabel = useMemo(() => {
    if (!provider) return "—";
    return provider.display_name || provider.name;
  }, [provider]);

  useEffect(() => {
    if (!keyTouched && displayName.trim()) {
      setAgentKey(slugify(displayName.trim()));
    }
  }, [displayName, keyTouched]);

  // Track summoning outcome via WS event
  const handleSummoningEvent = useCallback(
    (payload: unknown) => {
      const data = payload as Record<string, string>;
      if (createdAgent && data.agent_id !== createdAgent.id) return;
      if (data.type === "completed") setSummoningOutcome("success");
      if (data.type === "failed") setSummoningOutcome("failed");
    },
    [createdAgent],
  );
  useWsEvent("agent.summoning", handleSummoningEvent);

  // Manual proceed after user sees success state
  const handleContinue = () => {
    if (agentResult) onComplete(agentResult);
  };

  const handleSubmit = async () => {
    if (!agentKey.trim() || !isValidSlug(agentKey)) return;
    if (!provider) { setError(t("agent.errors.noProvider")); return; }

    setLoading(true);
    setError("");

    try {
      const otherConfig: Record<string, unknown> = {};
      if (description.trim()) otherConfig.description = description.trim();
      if (selfEvolve) otherConfig.self_evolve = true;

      if (isEditing) {
        // Update existing agent — skip summoning
        const patch: Partial<AgentData> = {
          display_name: displayName.trim() || undefined,
          provider: provider.name,
          model: model || "",
          other_config: Object.keys(otherConfig).length > 0 ? otherConfig : undefined,
        };
        await updateAgent(existingAgent!.id, patch);
        onComplete({ ...existingAgent!, ...patch } as AgentData);
      } else {
        const data: Partial<AgentData> = {
          agent_key: agentKey.trim(),
          display_name: displayName.trim() || undefined,
          provider: provider.name,
          model: model || "",
          agent_type: "predefined",
          is_default: true,
          other_config: Object.keys(otherConfig).length > 0 ? otherConfig : undefined,
        };

        const result = await createAgent(data) as AgentData;
        setAgentResult(result);
        setSummoningOutcome("pending");
        setCreatedAgent({ id: result.id, name: displayName.trim() || agentKey });
        setSummoningOpen(true);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("agent.errors.failedCreate"));
    } finally {
      setLoading(false);
    }
  };

  // Called by SummoningModal on both success/failure — we ignore it, use WS outcome instead
  const handleSummoningComplete = () => {};

  // Close modal: only allowed after summoning finishes
  const handleModalClose = async () => {
    if (summoningOutcome === "pending") return; // block while summoning
    if (summoningOutcome === "success") return; // auto-proceed handles this

    // Failed: delete agent and reset form
    if (agentResult) {
      try { await deleteAgent(agentResult.id); } catch { /* best effort */ }
    }
    setAgentResult(null);
    setCreatedAgent(null);
    setSummoningOpen(false);
    setSummoningOutcome("pending");
    setError(t("agent.summoningFailed"));
  };

  return (
    <>
      <Card>
        <CardContent className="space-y-4 pt-6">
          <TooltipProvider>
            <div className="space-y-1">
              <h2 className="text-lg font-semibold">{t("agent.title")}</h2>
              <p className="text-sm text-muted-foreground">
                {t("agent.description")}
              </p>
            </div>

            {/* Provider + model info */}
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
              <div className="flex items-center gap-2">
                <span className="text-sm text-muted-foreground">{t("agent.provider")}</span>
                <Badge variant="secondary">{providerLabel}</Badge>
              </div>
              {model && (
                <div className="flex items-center gap-2">
                  <span className="text-sm text-muted-foreground">{t("agent.model")}</span>
                  <Badge variant="outline">{model}</Badge>
                </div>
              )}
            </div>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label className="inline-flex items-center gap-1.5">
                  {t("agent.displayName")}
                  <InfoTip text={t("agent.displayNameHint")} />
                </Label>
                <Input
                  value={displayName}
                  onChange={(e) => setDisplayName(e.target.value)}
                  placeholder={t("agent.displayNamePlaceholder", "e.g. GoClaw")}
                />
              </div>
              <div className="space-y-2">
                <Label className="inline-flex items-center gap-1.5">
                  {t("agent.agentKey")}
                  <InfoTip text={t("agent.agentKeyHint")} />
                </Label>
                <Input
                  value={agentKey}
                  onChange={(e) => { setKeyTouched(true); setAgentKey(e.target.value); }}
                  onBlur={() => setAgentKey(slugify(agentKey))}
                  placeholder={t("agent.agentKeyPlaceholder")}
                  disabled={isEditing}
                />
              </div>
            </div>

            {/* Prompt / description */}
            <div className="space-y-3">
              <Label className="inline-flex items-center gap-1.5">
                {t("agent.personality")}
                <InfoTip text={t("agent.personalityHint")} />
              </Label>
              <div className="flex flex-wrap gap-1.5">
                {agentPresets.map((preset) => (
                  <button
                    key={preset.label}
                    type="button"
                    onClick={() => setDescription(preset.prompt)}
                    className="cursor-pointer rounded-full border px-2.5 py-0.5 text-xs transition-colors hover:bg-accent"
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t("agent.personalityPlaceholder")}
                className="min-h-[120px]"
              />
              <p className="text-xs text-muted-foreground">
                {t("agent.personalityHintBottom")}
              </p>
              <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
                <div className="space-y-0.5">
                  <Label htmlFor="setup-self-evolve" className="text-sm font-normal">{t("agent.selfEvolve")}</Label>
                  <p className="text-xs text-muted-foreground">{t("agent.selfEvolveDesc")}</p>
                </div>
                <Switch id="setup-self-evolve" checked={selfEvolve} onCheckedChange={setSelfEvolve} />
              </div>
            </div>

            {error && <p className="text-sm text-destructive">{error}</p>}

            <div className={`flex ${onBack ? "justify-between" : "justify-end"} gap-2`}>
              {onBack && (
                <Button variant="secondary" onClick={onBack}>
                  ← {t("common.back")}
                </Button>
              )}
              <Button
                onClick={handleSubmit}
                disabled={loading || !agentKey.trim() || !isValidSlug(agentKey) || !description.trim()}
              >
                {loading
                  ? isEditing ? t("agent.updating", "Updating...") : t("agent.creating")
                  : isEditing ? t("agent.update", "Update") : t("agent.create")}
              </Button>
            </div>
          </TooltipProvider>
        </CardContent>
      </Card>

      {/* Summoning animation modal */}
      {createdAgent && (
        <SummoningModal
          open={summoningOpen}
          onOpenChange={handleModalClose}
          agentId={createdAgent.id}
          agentName={createdAgent.name}
          onCompleted={handleSummoningComplete}
          onResummon={resummonAgent}
          hideClose
          onContinue={summoningOutcome === "success" ? handleContinue : undefined}
        />
      )}
    </>
  );
}
