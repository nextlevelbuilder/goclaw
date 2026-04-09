import { useState, useCallback } from 'react'
import type {
  AgentData, ContextPruningConfig, SubagentsConfig, ToolPolicyConfig,
  SandboxConfig, AgentReasoningConfig, ReasoningOverrideMode,
} from '../types/agent'

export function useAgentDetailState(
  agent: AgentData,
  onSave: (id: string, updates: Partial<AgentData>) => Promise<void>,
  onClose: () => void,
) {
  // --- Identity ---
  const [emoji, setEmoji] = useState((agent.other_config?.emoji as string) ?? '🤖')
  const [displayName, setDisplayName] = useState(agent.display_name ?? '')
  const [description, setDescription] = useState((agent.other_config?.description as string) ?? '')
  const [status, setStatus] = useState(agent.status ?? 'active')
  const [isDefault, setIsDefault] = useState(agent.is_default ?? false)

  // --- Model ---
  const [provider, setProvider] = useState(agent.provider)
  const [model, setModel] = useState(agent.model)
  const [contextWindow, setContextWindow] = useState(agent.context_window ?? 200000)
  const [maxToolIterations, setMaxToolIterations] = useState(agent.max_tool_iterations ?? 25)

  // --- Evolution ---
  const [selfEvolve, setSelfEvolve] = useState(!!(agent.other_config?.self_evolve))
  const [skillLearning, setSkillLearning] = useState(!!(agent.other_config?.skill_learning))
  const [skillNudgeInterval, setSkillNudgeInterval] = useState(
    (agent.other_config?.skill_nudge_interval as number) ?? 15,
  )

  // --- Prompt mode ---
  const [promptMode, setPromptMode] = useState((agent.other_config?.prompt_mode as string) || 'full')

  // --- Thinking ---
  const reasoning = (agent.other_config?.reasoning ?? {}) as AgentReasoningConfig
  const [reasoningMode, setReasoningMode] = useState<ReasoningOverrideMode>(reasoning.override_mode ?? 'inherit')
  const [thinkingLevel, setThinkingLevel] = useState(reasoning.effort ?? 'off')

  // --- Context pruning ---
  const [pruningEnabled, setPruningEnabled] = useState(agent.context_pruning != null)
  const [pruningConfig, setPruningConfig] = useState<ContextPruningConfig>(agent.context_pruning ?? {})

  // --- Compaction ---
  const [compactionConfig, setCompactionConfig] = useState(agent.compaction_config ?? {})

  // --- Subagents ---
  const [subEnabled, setSubEnabled] = useState(agent.subagents_config != null)
  const [subConfig, setSubConfig] = useState<SubagentsConfig>(agent.subagents_config ?? {})

  // --- Tool policy ---
  const [toolsEnabled, setToolsEnabled] = useState(agent.tools_config != null)
  const [toolsConfig, setToolsConfig] = useState<ToolPolicyConfig>(agent.tools_config ?? {})

  // --- Sandbox ---
  const [sandboxEnabled, setSandboxEnabled] = useState(agent.sandbox_config != null)
  const [sandboxConfig, setSandboxConfig] = useState<SandboxConfig>(agent.sandbox_config ?? {})

  // --- Pinned skills ---
  const [pinnedSkills, setPinnedSkills] = useState<string[]>(
    (agent.other_config?.pinned_skills as string[]) ?? [],
  )

  // --- Save state ---
  const [saveBlocked, setSaveBlocked] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')

  const handleSave = useCallback(async () => {
    setSaving(true)
    setSaveError('')
    try {
      const otherConfig: Record<string, unknown> = { ...agent.other_config }
      if (emoji) otherConfig.emoji = emoji
      if (description.trim()) otherConfig.description = description.trim()
      else delete otherConfig.description
      otherConfig.self_evolve = selfEvolve

      // Skill learning
      otherConfig.skill_learning = skillLearning
      if (skillLearning && skillNudgeInterval > 0) {
        otherConfig.skill_nudge_interval = skillNudgeInterval
      } else {
        delete otherConfig.skill_nudge_interval
      }

      // Prompt mode
      if (promptMode && promptMode !== 'full') {
        otherConfig.prompt_mode = promptMode
      } else {
        delete otherConfig.prompt_mode
      }

      // Thinking / reasoning
      if (reasoningMode === 'custom') {
        otherConfig.reasoning = { override_mode: 'custom', effort: thinkingLevel }
      } else {
        delete otherConfig.reasoning
      }

      // Pinned skills
      if (pinnedSkills.length > 0) {
        otherConfig.pinned_skills = pinnedSkills
      } else {
        delete otherConfig.pinned_skills
      }

      await onSave(agent.id, {
        display_name: displayName.trim() || undefined,
        provider,
        model,
        context_window: contextWindow,
        max_tool_iterations: maxToolIterations,
        is_default: isDefault,
        status,
        other_config: otherConfig,
        context_pruning: pruningEnabled ? pruningConfig : null,
        compaction_config: compactionConfig,
        subagents_config: subEnabled ? subConfig : null,
        tools_config: toolsEnabled ? toolsConfig : null,
        sandbox_config: sandboxEnabled ? sandboxConfig : null,
      })
      onClose()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }, [
    agent, emoji, displayName, description, selfEvolve, skillLearning, skillNudgeInterval,
    promptMode, reasoningMode, thinkingLevel, pinnedSkills,
    provider, model, contextWindow, maxToolIterations, isDefault, status,
    pruningEnabled, pruningConfig, compactionConfig,
    subEnabled, subConfig, toolsEnabled, toolsConfig, sandboxEnabled, sandboxConfig,
    onSave, onClose,
  ])

  return {
    // Identity
    emoji, setEmoji, displayName, setDisplayName, description, setDescription,
    status, setStatus, isDefault, setIsDefault,
    // Model
    provider, setProvider, model, setModel,
    contextWindow, setContextWindow, maxToolIterations, setMaxToolIterations,
    // Evolution
    selfEvolve, setSelfEvolve, skillLearning, setSkillLearning,
    skillNudgeInterval, setSkillNudgeInterval,
    // Prompt mode
    promptMode, setPromptMode,
    // Thinking
    reasoningMode, setReasoningMode, thinkingLevel, setThinkingLevel,
    // Pruning
    pruningEnabled, setPruningEnabled, pruningConfig, setPruningConfig,
    // Compaction
    compactionConfig, setCompactionConfig,
    // Subagents
    subEnabled, setSubEnabled, subConfig, setSubConfig,
    // Tool policy
    toolsEnabled, setToolsEnabled, toolsConfig, setToolsConfig,
    // Sandbox
    sandboxEnabled, setSandboxEnabled, sandboxConfig, setSandboxConfig,
    // Pinned skills
    pinnedSkills, setPinnedSkills,
    // Save
    saveBlocked, setSaveBlocked, saving, saveError, handleSave,
  }
}
