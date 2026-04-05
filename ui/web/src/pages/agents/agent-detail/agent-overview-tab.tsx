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
import { ChatGPTOAuthRoutingSummarySection } from "./overview-sections/chatgpt-oauth-routing-summary-section";
import { HeartbeatCard } from "./overview-sections/heartbeat-card";
import { MemorySection } from "./config-sections";
import type { UseAgentHeartbeatReturn } from "../hooks/use-agent-heartbeat";

interface AgentOverviewTabProps {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  heartbeat: UseAgentHeartbeatReturn;
  onManageCodexPool: () => void;
}

export function AgentOverviewTab({ agent, onUpdate, heartbeat, onManageCodexPool }: AgentOverviewTabProps) {
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

  // Memory (always shown — per-agent overrides, empty = use system defaults)
  const [mem, setMem] = useState<MemoryConfig>(agent.memory_config ?? {});

  // Capabilities (subagents + tool policy)
  const [subEnabled, setSubEnabled] = useState(agent.subagents_config != null);
  const [sub, setSub] = useState<SubagentsConfig>(agent.subagents_config ?? {});
  const [toolsEnabled, setToolsEnabled] = useState(agent.tools_config != null);
  const [tools, setTools] = useState<ToolPolicyConfig>(agent.tools_config ?? {});

  // Save state
  const [saving, setSaving] = useState(false);
  const [llmSaveBlocked, setLlmSaveBlocked] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      const updatedOtherConfig: Record<string, unknown> = {
        ...otherCfg,
        emoji: emoji.trim() || undefined,
        self_evolve: selfEvolve,
        skill_evolve: skillEvolve,
        skill_nudge_interval: skillEvolve ? skillNudgeInterval : undefined,
      };
      // When the provider changes, clear stale pool routing config so it
      // doesn't reference members from the previous provider's pool.
      if (provider !== agent.provider) {
        delete updatedOtherConfig.chatgpt_oauth_routing;
      }
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
        memory_config: mem,
        subagents_config: subEnabled ? sub : null,
        tools_config: toolsEnabled
          ? { profile: tools.profile, allow: tools.allow, deny: tools.deny, alsoAllow: tools.alsoAllow, byProvider: tools.byProvider }
          : {},
      });
    } catch {
      // toast shown by hook
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

      <ChatGPTOAuthRoutingSummarySection agent={agent} onManage={onManageCodexPool} />
      {provider !== agent.provider && !!otherCfg.chatgpt_oauth_routing && (
        <p className="text-xs text-amber-600 dark:text-amber-400 -mt-2 px-1">
          {t("chatgptOAuthRouting.providerChangedWarning")}
        </p>
      )}

      {/* Memory — always visible, per-agent overrides */}
      <MemorySection
        value={mem}
        onChange={setMem}
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
        disabled={llmSaveBlocked}
        label={t("general.saveChanges")}
        savingLabel={t("general.saving")}
      />
    </div>
  );
}
