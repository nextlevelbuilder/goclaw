import { useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  AgentData, MemoryConfig, SubagentsConfig, ToolPolicyConfig,
} from "@/types/agent";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import { PersonalitySection } from "./overview-sections/personality-section";
import { ModelBudgetSection } from "./overview-sections/model-budget-section";
import { SkillsSection } from "./overview-sections/skills-section";
import { EvolutionSection } from "./overview-sections/evolution-section";
import { CapabilitiesSection } from "./overview-sections/capabilities-section";
import { HeartbeatCard } from "./overview-sections/heartbeat-card";
import type { UseAgentHeartbeatReturn } from "../hooks/use-agent-heartbeat";

interface AgentOverviewTabProps {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  heartbeat: UseAgentHeartbeatReturn;
}

export function AgentOverviewTab({ agent, onUpdate, heartbeat }: AgentOverviewTabProps) {
  const { t } = useTranslation("agents");

  const otherCfg = (agent.other_config ?? {}) as Record<string, unknown>;

  // Personality
  const [emoji, setEmoji] = useState(typeof otherCfg.emoji === "string" ? otherCfg.emoji : "");
  const [displayName, setDisplayName] = useState(agent.display_name ?? "");
  const [frontmatter, setFrontmatter] = useState(agent.frontmatter ?? "");
  const [status, setStatus] = useState(agent.status);
  const [isDefault, setIsDefault] = useState(agent.is_default);

  // Model & Budget
  const [provider, setProvider] = useState(agent.provider);
  const [model, setModel] = useState(agent.model);
  const [contextWindow, setContextWindow] = useState(agent.context_window || 200000);
  const [maxToolIterations, setMaxToolIterations] = useState(agent.max_tool_iterations || 20);
  const [budgetDollars, setBudgetDollars] = useState(
    agent.budget_monthly_cents ? String(agent.budget_monthly_cents / 100) : "",
  );
  // Evolution (predefined only)
  const [selfEvolve, setSelfEvolve] = useState(Boolean(otherCfg.self_evolve));
  const [skillEvolve, setSkillEvolve] = useState(Boolean(otherCfg.skill_evolve));
  const [skillNudgeInterval, setSkillNudgeInterval] = useState(
    typeof otherCfg.skill_nudge_interval === "number" ? otherCfg.skill_nudge_interval : 15,
  );

  // Capabilities
  const [memEnabled, setMemEnabled] = useState(agent.memory_config != null);
  const [mem, setMem] = useState<MemoryConfig>(agent.memory_config ?? {});
  const [subEnabled, setSubEnabled] = useState(agent.subagents_config != null);
  const [sub, setSub] = useState<SubagentsConfig>(agent.subagents_config ?? {});
  const [toolsEnabled, setToolsEnabled] = useState(agent.tools_config != null);
  const [tools, setTools] = useState<ToolPolicyConfig>(agent.tools_config ?? {});

  // Save state
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [llmSaveBlocked, setLlmSaveBlocked] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    setSaveError(null);
    setSaved(false);
    try {
      const updatedOtherConfig = {
        ...otherCfg,
        emoji: emoji.trim() || undefined,
        self_evolve: selfEvolve,
        skill_evolve: skillEvolve,
        skill_nudge_interval: skillEvolve ? skillNudgeInterval : undefined,
      };
      const budgetCents = budgetDollars ? Math.round(parseFloat(budgetDollars) * 100) : null;
      await onUpdate({
        display_name: displayName,
        frontmatter: frontmatter || null,
        provider,
        model,
        context_window: contextWindow,
        max_tool_iterations: maxToolIterations,
        status,
        is_default: isDefault,
        other_config: updatedOtherConfig,
        budget_monthly_cents: budgetCents,
        memory_config: memEnabled ? mem : null,
        subagents_config: subEnabled ? sub : null,
        tools_config: toolsEnabled
          ? { profile: tools.profile, allow: tools.allow, deny: tools.deny, alsoAllow: tools.alsoAllow, byProvider: tools.byProvider }
          : {},
      });
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t("general.failedToSave"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <PersonalitySection
        agentKey={agent.agent_key}
        emoji={emoji}
        onEmojiChange={setEmoji}
        displayName={displayName}
        onDisplayNameChange={setDisplayName}
        frontmatter={frontmatter}
        onFrontmatterChange={setFrontmatter}
        status={status}
        onStatusChange={setStatus}
        isDefault={isDefault}
        onIsDefaultChange={setIsDefault}
      />

      <ModelBudgetSection
        provider={provider}
        onProviderChange={setProvider}
        model={model}
        onModelChange={setModel}
        contextWindow={contextWindow}
        onContextWindowChange={setContextWindow}
        maxToolIterations={maxToolIterations}
        onMaxToolIterationsChange={setMaxToolIterations}
        savedProvider={agent.provider}
        savedModel={agent.model}
        budgetDollars={budgetDollars}
        onBudgetDollarsChange={setBudgetDollars}
        onSaveBlockedChange={setLlmSaveBlocked}
      />

      <HeartbeatCard heartbeat={heartbeat} />

      <SkillsSection agentId={agent.id} />

      {agent.agent_type === "predefined" && (
        <EvolutionSection
          selfEvolve={selfEvolve}
          onSelfEvolveChange={setSelfEvolve}
          skillEvolve={skillEvolve}
          onSkillEvolveChange={setSkillEvolve}
          skillNudgeInterval={skillNudgeInterval}
          onSkillNudgeIntervalChange={setSkillNudgeInterval}
        />
      )}

      <CapabilitiesSection
        memEnabled={memEnabled}
        mem={mem}
        onMemToggle={setMemEnabled}
        onMemChange={setMem}
        subEnabled={subEnabled}
        sub={sub}
        onSubToggle={setSubEnabled}
        onSubChange={setSub}
        toolsEnabled={toolsEnabled}
        tools={tools}
        onToolsToggle={setToolsEnabled}
        onToolsChange={setTools}
      />

      <StickySaveBar
        onSave={handleSave}
        saving={saving}
        saved={saved}
        error={saveError}
        disabled={llmSaveBlocked}
        label={t("general.saveChanges")}
        savingLabel={t("general.saving")}
        savedLabel={t("general.saved")}
      />
    </div>
  );
}
