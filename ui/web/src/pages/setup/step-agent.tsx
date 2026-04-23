import { useState, useEffect, useMemo, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
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
import { slugify } from "@/lib/slug";
import type { ProviderData } from "@/types/provider";
import type { AgentData } from "@/types/agent";

interface StepAgentProps {
  provider: ProviderData | null;
  model: string | null;
  onComplete: (agent: AgentData) => void;
  onBack?: () => void;
  existingAgent?: AgentData | null;
}

export function StepAgent({ provider, model, onComplete, onBack, existingAgent }: StepAgentProps) {
  const { t } = useTranslation("setup");
  const { createAgent, updateAgent, deleteAgent, resummonAgent, cancelSummonAgent } = useAgents();
  const agentPresets = useAgentPresets();

  const isEditing = !!existingAgent;

  // Default to first preset (Fox Spirit)
  const defaultPreset = agentPresets[0];
  const [description, setDescription] = useState(
    existingAgent?.agent_description ?? defaultPreset?.prompt ?? "",
  );
  const [selectedPresetIdx, setSelectedPresetIdx] = useState<number | null>(
    existingAgent ? null : 0,
  );
  const [selfEvolve, setSelfEvolve] = useState(
    Boolean(existingAgent?.self_evolve),
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  // Summoning modal state
  const [summoningOpen, setSummoningOpen] = useState(false);
  const [summoningOutcome, setSummoningOutcome] = useState<"pending" | "success" | "failed">("pending");
  const [createdAgent, setCreatedAgent] = useState<{ id: string; name: string } | null>(null);
  const [agentResult, setAgentResult] = useState<AgentData | null>(null);

  // Derive agent key and display name from selected preset or description
  const displayName = useMemo(() => {
    if (existingAgent) return existingAgent.display_name ?? "";
    if (selectedPresetIdx !== null && agentPresets[selectedPresetIdx]) {
      return agentPresets[selectedPresetIdx].label;
    }
    return "Fox Spirit";
  }, [existingAgent, selectedPresetIdx, agentPresets]);

  const agentKey = useMemo(() => {
    const slug = slugify(displayName);
    return slug || "fox-spirit";
  }, [displayName]);

  const selectedEmoji = useMemo(() => {
    if (selectedPresetIdx !== null && agentPresets[selectedPresetIdx]) {
      return agentPresets[selectedPresetIdx].emoji;
    }
    return "🦊";
  }, [selectedPresetIdx, agentPresets]);

  const providerLabel = useMemo(() => {
    if (!provider) return "—";
    return provider.display_name || provider.name;
  }, [provider]);

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

  // Sync description when preset changes on initial load
  useEffect(() => {
    if (selectedPresetIdx !== null && agentPresets[selectedPresetIdx]) {
      setDescription(agentPresets[selectedPresetIdx].prompt);
    }
  }, [selectedPresetIdx, agentPresets]);

  const handleContinue = () => {
    if (agentResult) onComplete(agentResult);
  };

  const handleSelectPreset = (idx: number) => {
    const preset = agentPresets[idx];
    if (!preset) return;
    setSelectedPresetIdx(idx);
    setDescription(preset.prompt);
  };

  const handleDescriptionChange = (value: string) => {
    setDescription(value);
    // If user edits text, deselect preset (unless it still matches)
    if (selectedPresetIdx !== null) {
      const presetPrompt = agentPresets[selectedPresetIdx]?.prompt;
      if (value !== presetPrompt) setSelectedPresetIdx(null);
    }
  };

  const handleSubmit = async () => {
    if (!provider) { setError(t("agent.errors.noProvider")); return; }

    setLoading(true);
    setError("");

    try {
      if (isEditing) {
        const patch: Partial<AgentData> = {
          display_name: displayName.trim() || undefined,
          provider: provider.name,
          model: model || "",
          // Promoted fields at top level
          agent_description: description.trim() || null,
          self_evolve: selfEvolve,
          emoji: selectedEmoji || null,
        };
        await updateAgent(existingAgent!.id, patch);
        onComplete({ ...existingAgent!, ...patch } as AgentData);
      } else {
        const data: Partial<AgentData> = {
          agent_key: agentKey,
          display_name: displayName.trim() || undefined,
          provider: provider.name,
          model: model || "",
          agent_type: "predefined",
          is_default: true,
          // Promoted fields at top level
          agent_description: description.trim() || null,
          self_evolve: selfEvolve,
          emoji: selectedEmoji || null,
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

  const handleSummoningComplete = () => {};

  const handleModalClose = async () => {
    if (summoningOutcome === "pending") return;
    if (summoningOutcome === "success") return;

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
      <Card className="py-0 gap-0">
        <CardContent className="space-y-4 px-6 py-5">
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

            {/* Prompt / description with preset selection */}
            <div className="space-y-3">
              <Label className="inline-flex items-center gap-1.5">
                {t("agent.personality")}
                <InfoTip text={t("agent.personalityHint")} />
              </Label>
              <div className="flex flex-wrap gap-1.5">
                {agentPresets.map((preset, idx) => (
                  <button
                    key={preset.label}
                    type="button"
                    onClick={() => handleSelectPreset(idx)}
                    className={`cursor-pointer rounded-full border px-2.5 py-0.5 text-xs transition-colors ${
                      selectedPresetIdx === idx
                        ? "border-primary bg-primary/10 text-primary font-medium"
                        : "hover:bg-accent"
                    }`}
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
              <Textarea
                value={description}
                onChange={(e) => handleDescriptionChange(e.target.value)}
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
                disabled={loading || !description.trim()}
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
          onCancel={cancelSummonAgent}
          hideClose
          onContinue={summoningOutcome === "success" ? handleContinue : undefined}
        />
      )}
    </>
  );
}
