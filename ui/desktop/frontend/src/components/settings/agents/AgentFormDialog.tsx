import { useState, useEffect, useMemo, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../../common/Combobox'
import { useProviders } from '../../../hooks/use-providers'
import { getApiClient } from '../../../lib/api'
import { Switch } from '../../common/Switch'
import { slugify } from '../../../lib/slug'
import type { AgentData, AgentInput } from '../../../types/agent'

// Preset keys match agents.json presets (foxSpirit, artisan, astrologer)
// plus desktop-only extras (researcher, writer, coder) in desktop.json
const PRESET_KEYS = [
  { key: 'foxSpirit', emoji: '🦊', ns: 'agents', agentKey: 'little-fox' },
  { key: 'artisan', emoji: '🎨', ns: 'agents', agentKey: 'artisan' },
  { key: 'astrologer', emoji: '🔮', ns: 'agents', agentKey: 'mimi' },
  { key: 'researcher', emoji: '🔬', ns: 'desktop', agentKey: 'scholar' },
  { key: 'writer', emoji: '✍️', ns: 'desktop', agentKey: 'quill' },
  { key: 'coder', emoji: '👨‍💻', ns: 'desktop', agentKey: 'dev' },
]

interface AgentFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  agent?: AgentData | null
  onSubmit: (input: AgentInput) => Promise<AgentData | void>
}

export function AgentFormDialog({ open, onOpenChange, agent, onSubmit }: AgentFormDialogProps) {
  const isEditing = !!agent
  const { t } = useTranslation(['agents', 'desktop', 'common'])
  const { providers } = useProviders()

  const [displayName, setDisplayName] = useState('')
  const [providerName, setProviderName] = useState('')
  const [model, setModel] = useState('')
  const agentType = 'predefined' as const
  const [description, setDescription] = useState('')
  const [emoji, setEmoji] = useState('🦊')
  const [isDefault, setIsDefault] = useState(false)
  const [models, setModels] = useState<string[]>([])
  const [modelsLoading, setModelsLoading] = useState(false)
  const [verifyResult, setVerifyResult] = useState<{ valid: boolean; error?: string } | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [selectedPresetKey, setSelectedPresetKey] = useState('')

  // Reset form when dialog opens
  useEffect(() => {
    if (!open) return
    setDisplayName(agent?.display_name ?? '')
    setProviderName(agent?.provider ?? '')
    setModel(agent?.model ?? '')
    setDescription((agent?.other_config?.description as string) ?? '')
    setEmoji((agent?.other_config?.emoji as string) ?? '🦊')
    setIsDefault(agent?.is_default ?? false)
    setError('')
    setSelectedPresetKey('')
    setAgentKeyOverride('')
    setVerifyResult(isEditing ? { valid: true } : null) // editing = already verified
    setModels([])
  }, [open, agent, isEditing])

  // Selected provider object
  const selectedProvider = useMemo(
    () => providers.find((p) => p.name === providerName),
    [providers, providerName],
  )

  // Load models when provider changes
  const loadModels = useCallback(async (providerId: string) => {
    setModelsLoading(true)
    try {
      const res = await getApiClient().get<{ models: Array<{ id: string }> }>(
        `/v1/providers/${providerId}/models`,
      )
      setModels((res.models ?? []).map((m) => m.id))
    } catch {
      setModels([])
    } finally {
      setModelsLoading(false)
    }
  }, [])

  useEffect(() => {
    if (selectedProvider?.id) loadModels(selectedProvider.id)
  }, [selectedProvider?.id, loadModels])

  // Reset verify when provider or model changes
  useEffect(() => {
    if (!isEditing) setVerifyResult(null)
  }, [providerName, model, isEditing])

  const [agentKeyOverride, setAgentKeyOverride] = useState('')
  const agentKey = useMemo(() => {
    if (isEditing) return agent!.agent_key
    if (agentKeyOverride) return agentKeyOverride
    // Use preset agentKey if selected, otherwise slugify display name
    return selectedPresetKey || slugify(displayName) || 'agent'
  }, [isEditing, agent, agentKeyOverride, selectedPresetKey, displayName])

  const providerOptions = useMemo(
    () => providers.filter((p) => p.enabled).map((p) => ({
      value: p.name,
      label: p.display_name || p.name,
    })),
    [providers],
  )

  const modelOptions = useMemo(
    () => models.map((m) => ({ value: m, label: m })),
    [models],
  )

  const handleVerify = async () => {
    if (!selectedProvider?.id || !model.trim()) return
    setVerifying(true)
    try {
      const res = await getApiClient().post<{ valid: boolean; error?: string }>(
        `/v1/providers/${selectedProvider.id}/verify`,
        { model: model.trim() },
      )
      setVerifyResult({ valid: res.valid, error: res.error })
    } catch (err) {
      setVerifyResult({ valid: false, error: err instanceof Error ? err.message : 'Verification failed' })
    } finally {
      setVerifying(false)
    }
  }

  const handleCreate = async () => {
    setLoading(true)
    setError('')
    try {
      const otherConfig: Record<string, unknown> = {}
      if (description.trim()) otherConfig.description = description.trim()
      if (emoji.trim()) otherConfig.emoji = emoji.trim()
      await onSubmit({
        agent_key: agentKey,
        display_name: displayName.trim() || undefined,
        provider: providerName,
        model: model.trim(),
        agent_type: isEditing ? agent!.agent_type : agentType,
        is_default: isDefault || undefined,
        other_config: Object.keys(otherConfig).length > 0 ? otherConfig : undefined,
      })
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save agent')
    } finally {
      setLoading(false)
    }
  }

  if (!open) return null

  const canCreate = isEditing
    ? !!providerName && !!model.trim()
    : !!displayName.trim() && !!providerName && !!model.trim() && verifyResult?.valid && !!description.trim()

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="bg-surface-secondary border border-border rounded-xl shadow-xl max-w-3xl w-full mx-4 overflow-hidden flex flex-col" style={{ maxHeight: '85vh' }}>
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4 shrink-0">
          <h3 className="text-sm font-semibold text-text-primary">
            {isEditing ? t('agents:detail.tabs.agent') + ' — ' + t('common:edit') : t('agents:create.title')}
          </h3>
          <button onClick={() => onOpenChange(false)} className="p-1 text-text-muted hover:text-text-primary transition-colors">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>

        {/* Body — 2 columns */}
        <div className="flex-1 overflow-y-auto overscroll-contain p-5">
          <div className="grid grid-cols-2 gap-5">
            {/* Left column — identity & model */}
            <div className="space-y-4">
              {/* Display name + emoji */}
              <div className="flex gap-2">
                <div className="space-y-1 flex-1">
                  <label className="text-xs font-medium text-text-secondary">{t('agents:create.displayName').replace(' *', '')}</label>
                  <input
                    value={displayName}
                    onChange={(e) => setDisplayName(e.target.value)}
                    placeholder={t('agents:create.displayNamePlaceholder')}
                    className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
                  />
                </div>
                <div className="space-y-1 w-16">
                  <label className="text-xs font-medium text-text-secondary">{t('agents:identity.emoji')}</label>
                  <input
                    value={emoji}
                    onChange={(e) => setEmoji(e.target.value.slice(0, 2))}
                    maxLength={2}
                    className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary text-center focus:outline-none focus:ring-1 focus:ring-accent"
                  />
                </div>
              </div>

              {/* Agent key */}
              {!isEditing && (
                <div className="space-y-1">
                  <label className="text-xs font-medium text-text-secondary">Agent Key</label>
                  <input
                    value={agentKey}
                    onChange={(e) => setAgentKeyOverride(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '-'))}
                    className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent"
                  />
                </div>
              )}

              {/* Provider */}
              <div className="space-y-1">
                <label className="text-xs font-medium text-text-secondary">{t('common:provider')}</label>
                <Combobox value={providerName} onChange={setProviderName} options={providerOptions} placeholder={t('agents:create.selectProvider')} />
              </div>

              {/* Model */}
              <div className="space-y-1">
                <label className="text-xs font-medium text-text-secondary">{t('common:model')}</label>
                <Combobox
                  value={model}
                  onChange={setModel}
                  options={modelOptions}
                  placeholder={modelsLoading ? t('agents:create.loadingModels') : t('agents:create.enterOrSelectModel')}
                  allowCustom
                />
                {verifyResult && !verifyResult.valid && (
                  <p className="text-xs text-error">{verifyResult.error || t('desktop:agent.verifyFailed')}</p>
                )}
                {verifyResult?.valid && !isEditing && (
                  <p className="text-xs text-success">{t('desktop:agent.verified')}</p>
                )}
              </div>

              {/* Default toggle */}
              <div className="flex items-center gap-2">
                <Switch checked={isDefault} onCheckedChange={setIsDefault} />
                <span className="text-xs text-text-secondary">{t('agents:identity.defaultAgent')}</span>
              </div>
            </div>

            {/* Right column — personality */}
            <div className="space-y-1.5 flex flex-col">
              <label className="text-xs font-medium text-text-secondary">{t('agents:detail.personality')}</label>
              {!isEditing && (
                <div className="flex flex-wrap gap-1.5">
                  {PRESET_KEYS.map((p) => {
                    const label = t(`${p.ns}:presets.${p.key}.label`)
                    const prompt = t(`${p.ns}:presets.${p.key}.prompt`)
                    // Extract display name from prompt (first line: "Name: Xxx.")
                    const nameMatch = prompt.match(/^Name:\s*([^.]+)/)
                    const displayName = nameMatch ? nameMatch[1].trim() : label.replace(/^\S+\s*/, '')
                    return (
                      <button
                        key={p.key}
                        type="button"
                        onClick={() => { setDescription(prompt); setEmoji(p.emoji); setDisplayName(displayName); setSelectedPresetKey(p.agentKey); setAgentKeyOverride('') }}
                        className={`rounded-full border px-2.5 py-1 text-[11px] transition-colors ${
                          description === prompt
                            ? 'border-accent bg-accent/10 text-accent font-medium'
                            : 'border-border text-text-secondary hover:bg-surface-tertiary hover:text-text-primary'
                        }`}
                      >
                        {label}
                      </button>
                    )
                  })}
                </div>
              )}
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t('agents:create.descriptionPlaceholder')}
                className="flex-1 min-h-[200px] w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent resize-y"
              />
            </div>
          </div>
        </div>

        {error && <div className="px-5"><p className="text-xs text-error">{error}</p></div>}

        {/* Footer */}
        <div className="flex items-center justify-between border-t border-border px-5 py-4 shrink-0">
          {/* Left — error */}
          <div>
            {verifyResult && !verifyResult.valid && (
              <span className="text-[11px] text-error">{verifyResult.error}</span>
            )}
          </div>

          {/* Right — cancel + verify + summon/save */}
          <div className="flex items-center gap-2">
            <button onClick={() => onOpenChange(false)} className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors">
              {t('agents:create.cancel')}
            </button>
            {!isEditing && selectedProvider?.id && model.trim() && !verifyResult?.valid && (
              <button
                onClick={handleVerify}
                disabled={verifying}
                className="border border-border rounded-lg px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-tertiary transition-colors disabled:opacity-50 flex items-center gap-1.5"
              >
                {verifying ? (
                  <>
                    <svg className="h-3.5 w-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
                    {t('desktop:agent.verifying')}
                  </>
                ) : t('desktop:agent.verifyModel')}
              </button>
            )}
            <button
              onClick={handleCreate}
              disabled={!canCreate || loading}
              className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 flex items-center gap-1.5"
            >
              {loading ? '...' : isEditing ? t('common:save') : (
                <>
                  {verifyResult?.valid && (
                    <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"><path d="M20 6 9 17l-5-5" /></svg>
                  )}
                  {t('desktop:agent.summon')}
                </>
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
