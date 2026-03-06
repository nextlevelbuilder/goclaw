import { useState, useEffect, useMemo, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { TooltipProvider } from "@/components/ui/tooltip";
import { InfoTip } from "@/pages/setup/info-tip";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { SummoningModal } from "@/pages/agents/summoning-modal";
import { AGENT_PRESETS } from "@/pages/agents/agent-presets";
import { useWsEvent } from "@/hooks/use-ws-event";
import { slugify, isValidSlug } from "@/lib/slug";
import type { ProviderData } from "@/types/provider";
import type { AgentData } from "@/types/agent";

const DEFAULT_PROMPT = `You are GoClaw, my helpful assistant. I am your boss, NextLevelBuilder.`;

interface StepAgentProps {
  provider: ProviderData | null;
  model: string | null;
  onComplete: (agent: AgentData) => void;
}

export function StepAgent({ provider, model, onComplete }: StepAgentProps) {
  const { createAgent, deleteAgent, resummonAgent } = useAgents();

  const [displayName] = useState("GoClaw");
  const [agentKey, setAgentKey] = useState("goclaw");
  const [keyTouched, setKeyTouched] = useState(false);
  const [description, setDescription] = useState(DEFAULT_PROMPT);
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

  const handleCreate = async () => {
    if (!agentKey.trim() || !isValidSlug(agentKey)) return;
    if (!provider) { setError("No provider available"); return; }

    setLoading(true);
    setError("");

    try {
      const data: Partial<AgentData> = {
        agent_key: agentKey.trim(),
        display_name: displayName.trim() || undefined,
        provider: provider.name,
        model: model || "",
        agent_type: "predefined",
        other_config: description.trim() ? { description: description.trim() } : undefined,
      };

      const result = await createAgent(data) as AgentData;
      setAgentResult(result);
      setSummoningOutcome("pending");
      setCreatedAgent({ id: result.id, name: displayName.trim() || agentKey });
      setSummoningOpen(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create agent");
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
    setError("Summoning failed. Please adjust your settings and try again.");
  };

  return (
    <>
      <Card>
        <CardContent className="space-y-4 pt-6">
          <TooltipProvider>
            <div className="space-y-1">
              <h2 className="text-lg font-semibold">Create Your First Agent</h2>
              <p className="text-sm text-muted-foreground">
                An agent is your AI assistant. Customize its name and personality.
              </p>
            </div>

            {/* Provider + model info */}
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
              <div className="flex items-center gap-2">
                <span className="text-sm text-muted-foreground">Provider:</span>
                <Badge variant="secondary">{providerLabel}</Badge>
              </div>
              {model && (
                <div className="flex items-center gap-2">
                  <span className="text-sm text-muted-foreground">Model:</span>
                  <Badge variant="outline">{model}</Badge>
                </div>
              )}
            </div>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label className="inline-flex items-center gap-1.5">
                  Display Name
                  <InfoTip text="Will be auto-generated during summoning based on the agent personality." />
                </Label>
                <Input
                  value={displayName}
                  readOnly
                  className="bg-muted cursor-default"
                />
              </div>
              <div className="space-y-2">
                <Label className="inline-flex items-center gap-1.5">
                  Agent Key
                  <InfoTip text="Unique identifier for the agent. Lowercase letters, numbers, and hyphens only." />
                </Label>
                <Input
                  value={agentKey}
                  onChange={(e) => { setKeyTouched(true); setAgentKey(e.target.value); }}
                  onBlur={() => setAgentKey(slugify(agentKey))}
                  placeholder="my-agent"
                />
              </div>
            </div>

            {/* Prompt / description */}
            <div className="space-y-3">
              <Label className="inline-flex items-center gap-1.5">
                Agent Personality
                <InfoTip text="Describe your agent's role and behavior. AI will generate context files from this during summoning." />
              </Label>
              <div className="flex flex-wrap gap-1.5">
                {AGENT_PRESETS.map((preset) => (
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
                placeholder="Describe your agent's personality, purpose, and behavior..."
                className="min-h-[120px]"
              />
              <p className="text-xs text-muted-foreground">
                Customize this prompt to shape your agent's personality and expertise.
              </p>
            </div>

            {error && <p className="text-sm text-destructive">{error}</p>}

            <div className="flex justify-end">
              <Button
                onClick={handleCreate}
                disabled={loading || !agentKey.trim() || !isValidSlug(agentKey) || !description.trim()}
              >
                {loading ? "Creating..." : "Create Agent"}
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
