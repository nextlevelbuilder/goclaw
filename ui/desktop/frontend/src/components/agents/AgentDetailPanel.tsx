import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PersonalitySection } from './PersonalitySection'
import { ModelBudgetSection } from './ModelBudgetSection'
import { EvolutionSectionExpanded } from './evolution-section-expanded'
import { PromptModeSection } from './prompt-mode-section'
import { ThinkingSection } from './thinking-section'
import { OrchestrationSection } from './orchestration-section'
import { ContextPruningSection } from './context-pruning-section'
import { CompactionSection } from './compaction-section'
import { SubagentsSection } from './subagents-section'
import { ToolPolicySection } from './tool-policy-section'
import { SandboxSection } from './sandbox-section'
import { PinnedSkillsSection } from './pinned-skills-section'
import { EvolutionTab } from './evolution-tab'
import { AgentSkillsSection } from './AgentSkillsSection'
import { AgentMcpSection } from './AgentMcpSection'
import { AgentFilesTab } from './AgentFilesTab'
import { ConfirmDialog } from '../common/ConfirmDialog'
import { useAgentDetailState } from '../../hooks/use-agent-detail-state'
import type { AgentData } from '../../types/agent'

type DetailTab = 'overview' | 'evolution' | 'files'

interface AgentDetailPanelProps {
  agent: AgentData
  onSave: (id: string, updates: Partial<AgentData>) => Promise<void>
  onResummon: (id: string) => Promise<void>
  onClose: () => void
}

export function AgentDetailPanel({ agent, onSave, onResummon, onClose }: AgentDetailPanelProps) {
  const { t } = useTranslation(['agents', 'common'])
  const [tab, setTab] = useState<DetailTab>('overview')
  const [confirmResummon, setConfirmResummon] = useState(false)
  const s = useAgentDetailState(agent, onSave, onClose)
  const isPredefined = agent.agent_type === 'predefined'

  const handleConfirmResummon = async () => {
    setConfirmResummon(false)
    await onResummon(agent.id)
  }

  return (
    <div className="fixed inset-0 z-[60] flex flex-col bg-surface-primary">
      {/* Header */}
      <div className="flex items-center gap-3 px-4 py-3 border-b border-border bg-surface-secondary shrink-0">
        <button onClick={onClose} className="p-1 rounded hover:bg-surface-tertiary transition-colors" title="Back">
          <svg className="w-5 h-5 text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <polyline points="15 18 9 12 15 6" />
          </svg>
        </button>
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10 text-xl shrink-0">
          {s.emoji || '🤖'}
        </div>
        <div className="flex-1 min-w-0">
          <h2 className="text-sm font-semibold text-text-primary truncate">
            {s.displayName || agent.agent_key}
          </h2>
          <div className="flex items-center gap-2">
            <span className={`w-1.5 h-1.5 rounded-full ${s.status === 'active' ? 'bg-success' : s.status === 'summon_failed' ? 'bg-error' : 'bg-text-muted/50'}`} />
            <span className="text-[11px] text-text-muted font-mono">{agent.agent_key}</span>
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-surface-tertiary text-text-muted">{agent.agent_type}</span>
          </div>
        </div>
        <button
          onClick={() => setConfirmResummon(true)}
          className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors flex items-center gap-1.5"
        >
          <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" /><path d="M21 3v5h-5" />
            <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" /><path d="M3 21v-5h5" />
          </svg>
          {t('agents:files.resummon')}
        </button>
      </div>

      {/* Tab bar */}
      <div className="flex gap-1 px-4 pt-2 border-b border-border bg-surface-secondary shrink-0">
        {(['overview', ...(isPredefined ? ['evolution'] : []), 'files'] as DetailTab[]).map((tabKey) => (
          <button
            key={tabKey}
            onClick={() => setTab(tabKey)}
            className={[
              'px-3 py-1.5 text-xs rounded-t-md transition-colors -mb-px border-b-2',
              tab === tabKey
                ? 'border-accent text-accent font-medium'
                : 'border-transparent text-text-muted hover:text-text-primary',
            ].join(' ')}
          >
            {tabKey === 'overview' ? t('agents:detail.tabs.agent') : tabKey === 'evolution' ? t('agents:detail.evolution') : t('agents:detail.tabs.files')}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div className="flex-1 overflow-y-auto overscroll-contain">
        {tab === 'evolution' ? (
          <div className="max-w-2xl mx-auto px-4 py-6">
            <EvolutionTab agentId={agent.id} agentOtherConfig={agent.other_config} />
          </div>
        ) : tab === 'overview' ? (
          <div className="max-w-2xl mx-auto px-4 py-6 space-y-6">
            <PersonalitySection
              emoji={s.emoji} displayName={s.displayName} description={s.description}
              agentKey={agent.agent_key} agentType={agent.agent_type}
              isDefault={s.isDefault} status={s.status}
              onEmojiChange={s.setEmoji} onDisplayNameChange={s.setDisplayName}
              onDescriptionChange={s.setDescription} onIsDefaultChange={s.setIsDefault}
              onStatusChange={s.setStatus}
            />
            <hr className="border-border" />
            <ModelBudgetSection
              provider={s.provider} model={s.model}
              contextWindow={s.contextWindow} maxToolIterations={s.maxToolIterations}
              savedProvider={agent.provider} savedModel={agent.model}
              onProviderChange={s.setProvider} onModelChange={s.setModel}
              onContextWindowChange={s.setContextWindow} onMaxToolIterationsChange={s.setMaxToolIterations}
              onSaveBlockedChange={s.setSaveBlocked}
            />
            {isPredefined && (
              <>
                <hr className="border-border" />
                <EvolutionSectionExpanded
                  agentId={agent.id}
                  selfEvolve={s.selfEvolve} onSelfEvolveChange={s.setSelfEvolve}
                  skillLearning={s.skillLearning} onSkillLearningChange={s.setSkillLearning}
                  skillNudgeInterval={s.skillNudgeInterval} onSkillNudgeIntervalChange={s.setSkillNudgeInterval}
                />
              </>
            )}
            <hr className="border-border" />
            <PromptModeSection mode={s.promptMode} onModeChange={s.setPromptMode} />
            <hr className="border-border" />
            <ThinkingSection
              reasoningMode={s.reasoningMode} thinkingLevel={s.thinkingLevel}
              onReasoningModeChange={s.setReasoningMode} onThinkingLevelChange={s.setThinkingLevel}
            />
            {isPredefined && (
              <>
                <hr className="border-border" />
                <OrchestrationSection agentId={agent.id} />
              </>
            )}
            <hr className="border-border" />
            <ContextPruningSection
              enabled={s.pruningEnabled} value={s.pruningConfig}
              onToggle={s.setPruningEnabled} onChange={s.setPruningConfig}
            />
            <hr className="border-border" />
            <CompactionSection value={s.compactionConfig} onChange={s.setCompactionConfig} />
            <hr className="border-border" />
            <SubagentsSection enabled={s.subEnabled} value={s.subConfig} onToggle={s.setSubEnabled} onChange={s.setSubConfig} />
            <hr className="border-border" />
            <ToolPolicySection enabled={s.toolsEnabled} value={s.toolsConfig} onToggle={s.setToolsEnabled} onChange={s.setToolsConfig} />
            <hr className="border-border" />
            <SandboxSection enabled={s.sandboxEnabled} value={s.sandboxConfig} onToggle={s.setSandboxEnabled} onChange={s.setSandboxConfig} />
            <hr className="border-border" />
            <PinnedSkillsSection agentId={agent.id} pinned={s.pinnedSkills} onPinnedChange={s.setPinnedSkills} />
            <hr className="border-border" />
            <AgentSkillsSection agentId={agent.id} />
            <hr className="border-border" />
            <AgentMcpSection agentId={agent.id} />
          </div>
        ) : (
          <div className="max-w-4xl mx-auto px-4 py-6">
            <AgentFilesTab agentId={agent.id} agentKey={agent.agent_key} agentType={agent.agent_type} />
          </div>
        )}
      </div>

      {/* Sticky save bar — only on overview tab */}
      {tab === 'overview' && (
        <div className="shrink-0 border-t border-border bg-surface-secondary/80 backdrop-blur-sm px-4 py-3">
          <div className="max-w-2xl mx-auto flex items-center justify-between">
            {s.saveError && <p className="text-xs text-error flex-1">{s.saveError}</p>}
            <div className="flex items-center gap-3 ml-auto">
              <button onClick={onClose} className="px-4 py-2 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors">
                {t('common:cancel')}
              </button>
              <button
                onClick={s.handleSave}
                disabled={s.saving || s.saveBlocked}
                className="px-5 py-2 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 flex items-center gap-2"
              >
                {s.saving && <span className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin" />}
                {s.saving ? t('common:saving') : s.saveBlocked ? t('agents:create.check') : t('common:saveChanges')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Resummon confirm */}
      <ConfirmDialog
        open={confirmResummon}
        onOpenChange={setConfirmResummon}
        title={t('agents:files.resummonTitle')}
        description={t('agents:files.resummonDesc')}
        confirmLabel={t('agents:files.resummonConfirm')}
        variant="default"
        onConfirm={handleConfirmResummon}
      />
    </div>
  )
}
