import { useState, useMemo, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { getApiClient } from '../../lib/api'

import { SummoningModal } from './SummoningModal'
import type { ProviderData } from '../../types/provider'

// Preset keys — labels & prompts from agents.json (web) or desktop.json
// agentKey: stable English slug used as agent_key (never translated)
const PRESET_KEYS = [
  { key: 'foxSpirit', emoji: '🦊', ns: 'agents', agentKey: 'little-fox' },
  { key: 'artisan', emoji: '🎨', ns: 'agents', agentKey: 'artisan' },
  { key: 'astrologer', emoji: '🔮', ns: 'agents', agentKey: 'mimi' },
  { key: 'researcher', emoji: '🔬', ns: 'desktop', agentKey: 'scholar' },
  { key: 'writer', emoji: '✍️', ns: 'desktop', agentKey: 'quill' },
  { key: 'coder', emoji: '👨‍💻', ns: 'desktop', agentKey: 'dev' },
]

interface AgentStepProps {
  provider: ProviderData
  model: string | null
  onBack: () => void
  onComplete: () => void
}

export function AgentStep({ provider, model, onBack, onComplete }: AgentStepProps) {
  const { t } = useTranslation(['desktop', 'agents', 'common'])
  const [selectedPresetIdx, setSelectedPresetIdx] = useState<number>(0)
  const [description, setDescription] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [createdAgent, setCreatedAgent] = useState<{ id: string; name: string } | null>(null)

  // Get prompt text from locale for the selected preset
  function getPresetPrompt(idx: number): string {
    const preset = PRESET_KEYS[idx]
    if (!preset) return ''
    return t(`${preset.ns}:presets.${preset.key}.prompt`)
  }

  // Init description from first preset
  useEffect(() => {
    if (!description) setDescription(getPresetPrompt(0))
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const displayName = useMemo(() => {
    const preset = PRESET_KEYS[selectedPresetIdx]
    if (preset) {
      const label = t(`${preset.ns}:presets.${preset.key}.label`) as string
      // Label format: "🦊 Fox Spirit" — strip emoji prefix
      return label.replace(/^\S+\s*/, '')
    }
    return 'Fox Spirit'
  }, [selectedPresetIdx, t])

  // Agent key: stable English slug, never translated
  const agentKey = useMemo(() => {
    const preset = PRESET_KEYS[selectedPresetIdx]
    return preset?.agentKey ?? 'little-fox'
  }, [selectedPresetIdx])
  const selectedEmoji = PRESET_KEYS[selectedPresetIdx]?.emoji ?? '🦊'

  // Sync description when preset changes
  useEffect(() => {
    if (selectedPresetIdx >= 0) {
      setDescription(getPresetPrompt(selectedPresetIdx))
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedPresetIdx])

  const handleDescriptionChange = (value: string) => {
    setDescription(value)
    if (selectedPresetIdx >= 0) {
      const presetPrompt = getPresetPrompt(selectedPresetIdx)
      if (value !== presetPrompt) setSelectedPresetIdx(-1)
    }
  }

  const handleSubmit = async () => {
    if (!description.trim()) return
    setLoading(true)
    setError('')
    try {
      const result = await getApiClient().post<{ id: string }>('/v1/agents', {
        agent_key: agentKey,
        display_name: displayName.trim() || undefined,
        provider: provider.name,
        model: model || '',
        agent_type: 'predefined',
        is_default: true,
        // Promoted fields at top level — agent_description triggers summoning on backend
        agent_description: description.trim() || null,
        emoji: selectedEmoji || null,
      })
      setCreatedAgent({ id: result.id, name: displayName.trim() || agentKey })
    } catch (err) {
      setError(err instanceof Error ? err.message : t('common:failedToCreateAgent'))
    } finally {
      setLoading(false)
    }
  }

  const providerLabel = provider.display_name || provider.name

  if (createdAgent) {
    return (
      <SummoningModal
        agentId={createdAgent.id}
        agentName={createdAgent.name}
        onContinue={onComplete}
      />
    )
  }

  return (
    <div className="bg-surface-secondary border border-border rounded-xl p-6 space-y-4">
      <div>
        <h2 className="text-lg font-semibold text-text-primary">{t('onboarding.agentStep')}</h2>
        <p className="text-sm text-text-muted">{t('onboarding.agentStepDesc')}</p>
      </div>

      {/* Provider + model info */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <div className="flex items-center gap-2">
          <span className="text-sm text-text-muted">{t('common:provider')}</span>
          <span className="text-xs font-medium px-2 py-0.5 rounded-md bg-surface-tertiary border border-border text-text-secondary">
            {providerLabel}
          </span>
        </div>
        {model && (
          <div className="flex items-center gap-2">
            <span className="text-sm text-text-muted">{t('common:model')}</span>
            <span className="text-xs font-mono px-2 py-0.5 rounded-md border border-border text-text-secondary">
              {model}
            </span>
          </div>
        )}
      </div>

      {/* Preset personality buttons */}
      <div className="space-y-2">
        <label className="block text-sm font-medium text-text-secondary">{t('agents:detail.personality')}</label>
        <div className="flex flex-wrap gap-1.5">
          {PRESET_KEYS.map((preset, idx) => (
            <button
              key={preset.key}
              type="button"
              onClick={() => setSelectedPresetIdx(idx)}
              className={[
                'cursor-pointer rounded-full border px-3 py-1 text-xs transition-colors',
                selectedPresetIdx === idx
                  ? 'border-accent bg-accent/10 text-accent font-medium'
                  : 'border-border text-text-secondary hover:bg-surface-tertiary',
              ].join(' ')}
            >
              {t(`${preset.ns}:presets.${preset.key}.label`)}
            </button>
          ))}
        </div>
      </div>

      {/* Description textarea */}
      <div className="space-y-1.5">
        <label className="block text-sm font-medium text-text-secondary">{t('common:description')}</label>
        <textarea
          value={description}
          onChange={(e) => handleDescriptionChange(e.target.value)}
          placeholder={t('onboarding.descPlaceholder')}
          rows={4}
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2.5 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent resize-none"
        />
      </div>

      {error && <p className="text-sm text-error">{error}</p>}

      <div className="flex justify-between gap-2">
        <button
          onClick={onBack}
          className="px-4 py-2.5 border border-border rounded-lg text-sm font-medium text-text-secondary hover:bg-surface-tertiary transition-colors"
        >
          &larr; {t('common:back')}
        </button>
        <button
          onClick={handleSubmit}
          disabled={loading || !description.trim()}
          className="px-6 py-2.5 bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-40 disabled:cursor-not-allowed flex items-center gap-2"
        >
          {loading && <div className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin" />}
          {t('desktop:agent.summon')}
        </button>
      </div>
    </div>
  )
}
