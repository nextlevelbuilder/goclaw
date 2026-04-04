import { useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { PersonalitySection } from './PersonalitySection'
import { ModelBudgetSection } from './ModelBudgetSection'
import { EvolutionSection } from './EvolutionSection'
import { AgentSkillsSection } from './AgentSkillsSection'
import { AgentMcpSection } from './AgentMcpSection'
import { AgentFilesTab } from './AgentFilesTab'
import { ConfirmDialog } from '../common/ConfirmDialog'
import type { AgentData } from '../../types/agent'

type DetailTab = 'overview' | 'files'

interface AgentDetailPanelProps {
  agent: AgentData
  onSave: (id: string, updates: Partial<AgentData>) => Promise<void>
  onResummon: (id: string) => Promise<void>
  onClose: () => void
}

export function AgentDetailPanel({ agent, onSave, onResummon, onClose }: AgentDetailPanelProps) {
  const { t } = useTranslation(['agents', 'common'])
  const [tab, setTab] = useState<DetailTab>('overview')

  // --- Overview local state ---
  const [emoji, setEmoji] = useState((agent.other_config?.emoji as string) ?? '🤖')
  const [displayName, setDisplayName] = useState(agent.display_name ?? '')
  const [description, setDescription] = useState((agent.other_config?.description as string) ?? '')
  const [status, setStatus] = useState(agent.status ?? 'active')
  const [isDefault, setIsDefault] = useState(agent.is_default ?? false)
  const [provider, setProvider] = useState(agent.provider)
  const [model, setModel] = useState(agent.model)
  const [contextWindow, setContextWindow] = useState(agent.context_window ?? 200000)
  const [maxToolIterations, setMaxToolIterations] = useState(agent.max_tool_iterations ?? 25)
  const [selfEvolve, setSelfEvolve] = useState(!!(agent.other_config?.self_evolve))
  const [saveBlocked, setSaveBlocked] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [confirmResummon, setConfirmResummon] = useState(false)

  const handleSave = useCallback(async () => {
    setSaving(true)
    setSaveError('')
    try {
      const otherConfig: Record<string, unknown> = { ...agent.other_config }
      if (emoji) otherConfig.emoji = emoji
      if (description.trim()) otherConfig.description = description.trim()
      else delete otherConfig.description
      otherConfig.self_evolve = selfEvolve

      await onSave(agent.id, {
        display_name: displayName.trim() || undefined,
        provider,
        model,
        context_window: contextWindow,
        max_tool_iterations: maxToolIterations,
        is_default: isDefault,
        status,
        other_config: otherConfig,
      })
      onClose()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }, [agent, emoji, displayName, description, selfEvolve, provider, model, contextWindow, maxToolIterations, isDefault, status, onSave, onClose])

  const handleConfirmResummon = async () => {
    setConfirmResummon(false)
    await onResummon(agent.id)
  }

  const isPredefined = agent.agent_type === 'predefined'

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
          {emoji || '🤖'}
        </div>
        <div className="flex-1 min-w-0">
          <h2 className="text-sm font-semibold text-text-primary truncate">
            {displayName || agent.agent_key}
          </h2>
          <div className="flex items-center gap-2">
            <span className={`w-1.5 h-1.5 rounded-full ${status === 'active' ? 'bg-success' : status === 'summon_failed' ? 'bg-error' : 'bg-text-muted/50'}`} />
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
        {(['overview', 'files'] as const).map((tabKey) => (
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
            {tabKey === 'overview' ? t('agents:detail.tabs.agent') : t('agents:detail.tabs.files')}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div className="flex-1 overflow-y-auto overscroll-contain">
        {tab === 'overview' ? (
          <div className="max-w-2xl mx-auto px-4 py-6 space-y-6">
            <PersonalitySection
              emoji={emoji} displayName={displayName} description={description}
              agentKey={agent.agent_key} agentType={agent.agent_type}
              isDefault={isDefault} status={status}
              onEmojiChange={setEmoji} onDisplayNameChange={setDisplayName}
              onDescriptionChange={setDescription} onIsDefaultChange={setIsDefault}
              onStatusChange={setStatus}
            />
            <hr className="border-border" />
            <ModelBudgetSection
              provider={provider} model={model}
              contextWindow={contextWindow} maxToolIterations={maxToolIterations}
              savedProvider={agent.provider} savedModel={agent.model}
              onProviderChange={setProvider} onModelChange={setModel}
              onContextWindowChange={setContextWindow} onMaxToolIterationsChange={setMaxToolIterations}
              onSaveBlockedChange={setSaveBlocked}
            />
            {isPredefined && (
              <>
                <hr className="border-border" />
                <EvolutionSection selfEvolve={selfEvolve} onSelfEvolveChange={setSelfEvolve} />
              </>
            )}
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
            {saveError && <p className="text-xs text-error flex-1">{saveError}</p>}
            <div className="flex items-center gap-3 ml-auto">
              <button onClick={onClose} className="px-4 py-2 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors">
                {t('common:cancel')}
              </button>
              <button
                onClick={handleSave}
                disabled={saving || saveBlocked}
                className="px-5 py-2 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 flex items-center gap-2"
              >
                {saving && <span className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin" />}
                {saving ? t('common:saving') : saveBlocked ? t('agents:create.check') : t('common:saveChanges')}
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
