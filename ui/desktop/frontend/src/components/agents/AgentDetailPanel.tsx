import { useState, useRef, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge, Button, ConfirmDialog, Menu, StatusChip, Tabs, TabsContent, TabsList, TabsTrigger } from '../ui'
import { IconChat, IconChevronLeft, IconRefresh, IconTrash } from '../common/Icons'
import { chatWithAgent } from '../layout/shell-actions'
import { IdentitySection, PersonalitySection, DESCRIPTION_MAX } from './PersonalitySection'
import { ModelBudgetSection, CONTEXT_WINDOW_RANGE, MAX_TOOL_ITERATIONS_RANGE } from './ModelBudgetSection'
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
import { VoicePicker } from './voice-picker'
import { TtsEmptyState } from './tts-empty-state'
import { TtsOverrideFineTune } from './tts-override-fine-tune'
import { AgentBadge } from './agent-badge'
import { SectionCard } from './section-card'
import { agentDisplayName, agentStatusView } from './agent-display'
import type { ParamValue } from '../dynamic-param-form'
import { useAgentDetailState } from '../../hooks/use-agent-detail-state'
import { useDesktopTtsConfig } from '../../hooks/use-tts-config'
import { useTtsCapabilities } from '../../hooks/use-tts-capabilities'
import type { AgentData } from '../../types/agent'
import type { TtsProviderId } from '@/data/tts-providers'

type DetailTab = 'overview' | 'files' | 'evolution'

interface AgentDetailPanelProps {
  agent: AgentData
  /** The gateway is unreachable: saved data stays visible, writes are disabled. */
  readOnly?: boolean
  onSave: (id: string, updates: Partial<AgentData>) => Promise<void>
  onResummon: (id: string) => Promise<void>
  onDelete: (agent: AgentData) => void
  onClose: () => void
}

/** Agent settings page: header, tabs (Agent, Files, Evolution) and a pinned save bar. */
export function AgentDetailPanel(props: AgentDetailPanelProps) {
  const [tab, setTab] = useState<DetailTab>('overview')
  // Bumping the key remounts the form, which discards unsaved edits.
  const [formKey, setFormKey] = useState(0)
  return <AgentDetailBody key={formKey} {...props} tab={tab} onTabChange={setTab} onDiscard={() => setFormKey((k) => k + 1)} />
}

interface BodyProps extends AgentDetailPanelProps {
  tab: DetailTab
  onTabChange: (tab: DetailTab) => void
  onDiscard: () => void
}

function AgentDetailBody({ agent, readOnly = false, tab, onTabChange, onDiscard, onSave, onResummon, onDelete, onClose }: BodyProps) {
  const { t, i18n } = useTranslation(['agents', 'common', 'tts'])
  const [confirmResummon, setConfirmResummon] = useState(false)
  const [ttsVoiceId, setTtsVoiceId] = useState<string | null>((agent.other_config?.tts_voice_id as string) ?? null)
  const ttsVoiceIdRef = useRef(ttsVoiceId)
  ttsVoiceIdRef.current = ttsVoiceId

  // Per-agent TTS fine-tune params (generic keys: speed, emotion, style).
  const [ttsParams, setTtsParams] = useState<Record<string, ParamValue>>(
    (agent.other_config?.tts_params as Record<string, ParamValue>) ?? {},
  )
  const ttsParamsRef = useRef(ttsParams)
  ttsParamsRef.current = ttsParams

  // Merge tts_voice_id + tts_params into other_config at save time.
  const onSaveWithVoice = useCallback(async (id: string, updates: Partial<AgentData>) => {
    const merged = { ...updates }
    const existing = (merged.other_config ?? {}) as Record<string, unknown>
    const voiceId = ttsVoiceIdRef.current
    const params = ttsParamsRef.current
    let cfg = { ...existing }
    if (voiceId) {
      cfg = { ...cfg, tts_voice_id: voiceId }
    } else {
      const { tts_voice_id: _v, ...rest } = cfg
      void _v
      cfg = rest
    }
    if (params && Object.keys(params).length > 0) {
      cfg = { ...cfg, tts_params: params }
    } else {
      const { tts_params: _p, ...rest } = cfg
      void _p
      cfg = rest
    }
    merged.other_config = Object.keys(cfg).length > 0 ? cfg : null
    await onSave(id, merged)
  }, [onSave])

  // ── dirty tracking: compare every editable value with the last saved snapshot ──
  const snapshotRef = useRef<Record<string, string>>({})
  const [baseline, setBaseline] = useState<Record<string, string> | null>(null)
  const markSaved = useCallback(() => setBaseline({ ...snapshotRef.current }), [])

  const s = useAgentDetailState(agent, onSaveWithVoice, markSaved)
  const isPredefined = agent.agent_type === 'predefined'
  const { globalProvider } = useDesktopTtsConfig()
  const { data: allCaps } = useTtsCapabilities()

  const values: Record<string, unknown> = {
    emoji: s.emoji, displayName: s.displayName, description: s.description, status: s.status, isDefault: s.isDefault,
    provider: s.provider, model: s.model, contextWindow: s.contextWindow, maxToolIterations: s.maxToolIterations,
    selfEvolve: s.selfEvolve, skillLearning: s.skillLearning, skillNudgeInterval: s.skillNudgeInterval,
    promptMode: s.promptMode, reasoningMode: s.reasoningMode, thinkingLevel: s.thinkingLevel,
    pruningEnabled: s.pruningEnabled, pruningConfig: s.pruningConfig, compactionConfig: s.compactionConfig,
    subEnabled: s.subEnabled, subConfig: s.subConfig, toolsEnabled: s.toolsEnabled, toolsConfig: s.toolsConfig,
    sandboxEnabled: s.sandboxEnabled, sandboxConfig: s.sandboxConfig, pinnedSkills: s.pinnedSkills,
    ttsVoiceId, ttsParams,
  }
  const snapshot: Record<string, string> = {}
  for (const [k, v] of Object.entries(values)) snapshot[k] = JSON.stringify(v ?? null)
  snapshotRef.current = snapshot
  const initialRef = useRef<Record<string, string> | null>(null)
  if (initialRef.current === null) initialRef.current = snapshot
  const saved = baseline ?? initialRef.current
  const changed = Object.keys(snapshot).filter((k) => snapshot[k] !== saved[k])
  const dirty = changed.length > 0

  // ── validation ──
  const touchedName = JSON.parse(saved.displayName ?? '""') !== '' || s.displayName.trim() !== ''
  const errors = (() => {
    const range = (n: number, r: { min: number; max: number }) =>
      !Number.isInteger(n) || n < r.min || n > r.max ? t('agents:screen.errors.contextWindow', r) : undefined
    return {
      displayName: touchedName && !s.displayName.trim() ? t('agents:screen.errors.displayNameRequired') : undefined,
      provider: !s.provider ? t('agents:screen.errors.providerRequired') : undefined,
      model: !s.model.trim() ? t('agents:screen.errors.modelRequired') : undefined,
      contextWindow: range(s.contextWindow, CONTEXT_WINDOW_RANGE),
      maxToolIterations: !Number.isInteger(s.maxToolIterations) || s.maxToolIterations < MAX_TOOL_ITERATIONS_RANGE.min || s.maxToolIterations > MAX_TOOL_ITERATIONS_RANGE.max
        ? t('agents:screen.errors.maxToolIterations', MAX_TOOL_ITERATIONS_RANGE)
        : undefined,
      description: s.description.length > DESCRIPTION_MAX && changed.includes('description')
        ? t('agents:screen.errors.descriptionTooLong', { max: DESCRIPTION_MAX })
        : undefined,
    }
  })()
  const errorCount = Object.values(errors).filter(Boolean).length

  const name = agentDisplayName(agent) ?? t('agents:screen.list.unnamed')
  const statusView = agentStatusView(agent.status)
  const dateFmt = useMemo(() => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }), [i18n.language])
  const fmtDate = (v?: string) => {
    if (!v) return '—'
    const d = new Date(v)
    return Number.isNaN(d.getTime()) ? '—' : dateFmt.format(d)
  }

  const canSave = dirty && errorCount === 0 && !s.saveBlocked && !s.saving && !readOnly
  const showBar = dirty || errorCount > 0 || Boolean(s.saveError)

  let barMessage: string
  let barTone: 'dirty' | 'error' = 'dirty'
  if (s.saveError) {
    barMessage = `${t('agents:screen.detail.saveFailed')}: ${s.saveError}`
    barTone = 'error'
  } else if (errorCount > 0) {
    barMessage = t('agents:screen.detail.fixFields', { count: errorCount })
    barTone = 'error'
  } else if (s.saveBlocked) {
    barMessage = t('agents:screen.detail.checkModel')
  } else {
    barMessage = t('agents:screen.detail.unsaved', { count: changed.length })
  }

  const handleConfirmResummon = async () => {
    setConfirmResummon(false)
    await onResummon(agent.id)
  }

  return (
    <Tabs value={tab} onValueChange={(v) => onTabChange(v as DetailTab)} className="h-full">
      <header className="flex shrink-0 flex-col gap-3 border-b border-border-subtle px-6 pt-4">
        <div>
          <Button variant="ghost" size="sm" icon={<IconChevronLeft size={14} />} onClick={onClose} aria-label={t('agents:screen.detail.backLabel')} className="-ml-2.5">
            {t('agents:screen.detail.back')}
          </Button>
        </div>
        <div className="flex items-center gap-3">
          <AgentBadge emoji={s.emoji || undefined} size="lg" />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="truncate text-title-lg text-ink">{s.displayName.trim() || name}</h1>
              <StatusChip tone={statusView.tone} label={t(`agents:screen.status.${statusView.key}`)} />
              {agent.is_default && <Badge tone="accent">{t('agents:screen.list.defaultBadge')}</Badge>}
            </div>
            <p className="truncate font-mono text-caption font-normal text-ink-muted">{t('agents:screen.detail.agentKey', { key: agent.agent_key })}</p>
          </div>
          <Button icon={<IconChat size={14} />} onClick={() => chatWithAgent(agent.id)} disabled={agent.status !== 'active'}>
            {t('agents:screen.detail.chat')}
          </Button>
          <Menu
            label={t('agents:screen.detail.more')}
            items={[
              { label: t('agents:screen.detail.resummon'), icon: <IconRefresh size={14} />, onSelect: () => setConfirmResummon(true), disabled: readOnly },
              'separator',
              { label: t('agents:screen.detail.deleteAgent'), icon: <IconTrash size={14} />, destructive: true, onSelect: () => onDelete(agent), disabled: readOnly },
            ]}
          />
        </div>
        <TabsList className="border-b-0">
          <TabsTrigger value="overview">{t('agents:detail.tabs.agent')}</TabsTrigger>
          <TabsTrigger value="files">{t('agents:detail.tabs.files')}</TabsTrigger>
          {isPredefined && <TabsTrigger value="evolution">{t('agents:detail.tabs.evolution')}</TabsTrigger>}
        </TabsList>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        <div>
          <TabsContent value="overview" className="px-6 py-6">
            <fieldset disabled={readOnly} className="m-0 min-w-0 border-0 p-0">
              <div className="grid grid-cols-1 gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
                <div className="flex min-w-0 flex-col gap-5">
                  <IdentitySection
                    emoji={s.emoji} displayName={s.displayName} isDefault={s.isDefault} status={s.status}
                    displayNameError={errors.displayName} readOnly={readOnly}
                    onEmojiChange={s.setEmoji} onDisplayNameChange={s.setDisplayName}
                    onIsDefaultChange={s.setIsDefault} onStatusChange={s.setStatus}
                  />
                  <PersonalitySection
                    description={s.description} agentType={agent.agent_type} error={errors.description}
                    readOnly={readOnly} onDescriptionChange={s.setDescription}
                  />
                  <ModelBudgetSection
                    provider={s.provider} model={s.model}
                    contextWindow={s.contextWindow} maxToolIterations={s.maxToolIterations}
                    savedProvider={agent.provider} savedModel={agent.model}
                    errors={errors} readOnly={readOnly}
                    onProviderChange={s.setProvider} onModelChange={s.setModel}
                    onContextWindowChange={s.setContextWindow} onMaxToolIterationsChange={s.setMaxToolIterations}
                    onSaveBlockedChange={s.setSaveBlocked}
                  />
                  {isPredefined && (
                    <SectionCard>
                      <EvolutionSectionExpanded
                        agentId={agent.id}
                        selfEvolve={s.selfEvolve} onSelfEvolveChange={s.setSelfEvolve}
                        skillLearning={s.skillLearning} onSkillLearningChange={s.setSkillLearning}
                        skillNudgeInterval={s.skillNudgeInterval} onSkillNudgeIntervalChange={s.setSkillNudgeInterval}
                      />
                    </SectionCard>
                  )}
                  <SectionCard><PromptModeSection mode={s.promptMode} onModeChange={s.setPromptMode} /></SectionCard>
                  <SectionCard>
                    <ThinkingSection
                      reasoningMode={s.reasoningMode} thinkingLevel={s.thinkingLevel}
                      onReasoningModeChange={s.setReasoningMode} onThinkingLevelChange={s.setThinkingLevel}
                    />
                  </SectionCard>
                  {isPredefined && <SectionCard><OrchestrationSection agentId={agent.id} /></SectionCard>}
                  <SectionCard>
                    <ContextPruningSection enabled={s.pruningEnabled} value={s.pruningConfig} onToggle={s.setPruningEnabled} onChange={s.setPruningConfig} />
                  </SectionCard>
                  <SectionCard><CompactionSection value={s.compactionConfig} onChange={s.setCompactionConfig} /></SectionCard>
                  <SectionCard>
                    <SubagentsSection enabled={s.subEnabled} value={s.subConfig} onToggle={s.setSubEnabled} onChange={s.setSubConfig} />
                  </SectionCard>
                  <SectionCard>
                    <ToolPolicySection enabled={s.toolsEnabled} value={s.toolsConfig} onToggle={s.setToolsEnabled} onChange={s.setToolsConfig} />
                  </SectionCard>
                  <SectionCard>
                    <SandboxSection enabled={s.sandboxEnabled} value={s.sandboxConfig} onToggle={s.setSandboxEnabled} onChange={s.setSandboxConfig} />
                  </SectionCard>
                  <SectionCard><PinnedSkillsSection agentId={agent.id} pinned={s.pinnedSkills} onPinnedChange={s.setPinnedSkills} /></SectionCard>
                  <SectionCard><AgentSkillsSection agentId={agent.id} /></SectionCard>
                  <SectionCard><AgentMcpSection agentId={agent.id} /></SectionCard>
                  <SectionCard title={t('tts:voice_label')}>
                    {globalProvider ? (
                      <>
                        <VoicePicker value={ttsVoiceId} onChange={setTtsVoiceId} provider={globalProvider as TtsProviderId} />
                        {/* agent_overridable_as in capabilities decides which params show and how generic and native keys map. */}
                        <TtsOverrideFineTune globalProvider={globalProvider} allCaps={allCaps} ttsParams={ttsParams} onChange={setTtsParams} />
                      </>
                    ) : (
                      <TtsEmptyState />
                    )}
                  </SectionCard>
                </div>

                <aside className="flex min-w-0 flex-col gap-5">
                  <SectionCard title={t('agents:screen.detail.activity')}>
                    <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-caption">
                      <Stat label={t('agents:screen.detail.type')} value={isPredefined ? t('agents:screen.detail.typePredefined') : t('agents:screen.detail.typeOpen')} />
                      <Stat label={t('agents:screen.detail.created')} value={fmtDate(agent.created_at)} />
                      <Stat label={t('agents:screen.detail.updated')} value={fmtDate(agent.updated_at)} />
                    </dl>
                  </SectionCard>
                  <SectionCard title={t('agents:screen.detail.danger')} tone="danger">
                    <p className="text-caption font-normal text-ink-secondary">{t('agents:screen.detail.dangerText')}</p>
                    <div>
                      <Button variant="danger" icon={<IconTrash size={14} />} onClick={() => onDelete(agent)} disabled={readOnly}>
                        {t('agents:screen.detail.deleteAgent')}
                      </Button>
                    </div>
                  </SectionCard>
                </aside>
              </div>
            </fieldset>
          </TabsContent>
          <TabsContent value="files" className="px-6 py-6">
            <AgentFilesTab agentId={agent.id} agentKey={agent.agent_key} agentType={agent.agent_type} readOnly={readOnly} />
          </TabsContent>
          {isPredefined && (
            <TabsContent value="evolution" className="px-6 py-6">
              <EvolutionTab agentId={agent.id} agentOtherConfig={agent.other_config} readOnly={readOnly} />
            </TabsContent>
          )}
        </div>
      </div>

      {tab === 'overview' && showBar && (
        <div
          role={barTone === 'error' ? 'alert' : 'status'}
          className={`flex shrink-0 items-center gap-3 border-t px-6 py-3 ${barTone === 'error' ? 'border-danger bg-danger-subtle' : 'border-border-subtle bg-surface-raised'}`}
        >
          <span aria-hidden="true" className={`size-2 shrink-0 rounded-pill ${barTone === 'error' ? 'bg-danger' : 'bg-accent'}`} />
          <p className={`min-w-0 flex-1 truncate text-ui ${barTone === 'error' ? 'text-danger' : 'text-ink'}`}>{barMessage}</p>
          <Button onClick={onDiscard} disabled={s.saving}>{t('agents:screen.detail.discard')}</Button>
          <Button variant="primary" onClick={s.handleSave} disabled={!canSave} loading={s.saving}>
            {s.saving ? t('agents:screen.detail.saving') : t('agents:screen.detail.save')}
          </Button>
        </div>
      )}

      <ConfirmDialog
        open={confirmResummon}
        onOpenChange={setConfirmResummon}
        title={t('agents:files.resummonTitle')}
        description={t('agents:files.resummonDesc')}
        confirmLabel={t('agents:files.resummonConfirm')}
        onConfirm={handleConfirmResummon}
      />
    </Tabs>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-caption font-normal text-ink-muted">{label}</dt>
      <dd className="truncate text-ui text-ink">{value}</dd>
    </div>
  )
}
