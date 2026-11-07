import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { getApiClient } from '../../lib/api'
import { cn } from '../../lib/cn'
import { slugify, isValidSlug } from '../../lib/slug'
import { agentService } from '../../services/agent-service'
import { Button, Field, Input, Textarea } from '../ui'
import { StepError, StepShell } from './onboarding-shell'
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
  const { t } = useTranslation(['onboarding', 'agents', 'desktop'])
  const [selectedPresetIdx, setSelectedPresetIdx] = useState<number>(0)
  const [description, setDescription] = useState('')
  // Identity is editable state, only *prefilled* from the selected preset. It must
  // survive description edits — deselecting a preset never rewrites the name/key.
  const [displayName, setDisplayName] = useState('')
  const [agentKey, setAgentKey] = useState('')
  const [displayNameEdited, setDisplayNameEdited] = useState(false)
  const [agentKeyEdited, setAgentKeyEdited] = useState(false)
  const [emoji, setEmoji] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [createdAgent, setCreatedAgent] = useState<{ id: string; name: string } | null>(null)

  // Get prompt text from locale for the selected preset
  function getPresetPrompt(idx: number): string {
    const preset = PRESET_KEYS[idx]
    if (!preset) return ''
    return t(`${preset.ns}:presets.${preset.key}.prompt`)
  }

  // Label format: "🦊 Fox Spirit" — the display name drops the emoji prefix
  function getPresetName(idx: number): string {
    const preset = PRESET_KEYS[idx]
    if (!preset) return ''
    const label = t(`${preset.ns}:presets.${preset.key}.label`) as string
    return label.replace(/^\S+\s+/, '').trim() || label.trim()
  }

  // Init key + emoji from the first preset; name and prompt are filled by the
  // locale-sync effects below.
  useEffect(() => {
    setAgentKey(PRESET_KEYS[0].agentKey)
    setEmoji(PRESET_KEYS[0].emoji)
  }, [])

  const presetPrompt = selectedPresetIdx >= 0 ? getPresetPrompt(selectedPresetIdx) : undefined
  const presetName = selectedPresetIdx >= 0 ? getPresetName(selectedPresetIdx) : undefined

  // Language-aware re-sync: while a preset is active and the field is untouched,
  // follow the translated preset text. Deps are plain strings, so this only fires
  // when the translation actually changes (locale switch), not on every render.
  useEffect(() => {
    if (presetPrompt === undefined) return
    setDescription(presetPrompt)
  }, [presetPrompt])

  useEffect(() => {
    if (presetName === undefined || displayNameEdited) return
    setDisplayName(presetName)
  }, [presetName, displayNameEdited])

  const trimmedName = displayName.trim()
  const keyValid = isValidSlug(agentKey)
  const canSubmit = !!trimmedName && keyValid && !!description.trim()

  const handleSelectPreset = (idx: number) => {
    const preset = PRESET_KEYS[idx]
    if (!preset) return
    setSelectedPresetIdx(idx)
    setDescription(getPresetPrompt(idx))
    setDisplayName(getPresetName(idx))
    setDisplayNameEdited(false)
    setEmoji(preset.emoji)
    setAgentKey(preset.agentKey)
    setAgentKeyEdited(false)
  }

  const handleDescriptionChange = (value: string) => {
    setDescription(value)
    // Editing the prompt only clears the preset highlight — identity stays as typed.
    if (selectedPresetIdx >= 0 && value !== presetPrompt) setSelectedPresetIdx(-1)
  }

  const handleDisplayNameChange = (value: string) => {
    setDisplayNameEdited(true)
    setDisplayName(value)
    if (!agentKeyEdited) setAgentKey(slugify(value))
  }

  const handleAgentKeyChange = (value: string) => {
    setAgentKeyEdited(true)
    setAgentKey(value)
  }

  const handleSubmit = async () => {
    if (!canSubmit) return
    setLoading(true)
    setError('')
    try {
      const result = await getApiClient().post<{ id: string }>('/v1/agents', {
        agent_key: agentKey,
        display_name: trimmedName,
        provider: provider.name,
        model: model || '',
        agent_type: 'predefined',
        is_default: true,
        // Promoted fields at top level — agent_description triggers summoning on backend
        agent_description: description.trim() || null,
        emoji: emoji.trim() || null,
      })
      setCreatedAgent({ id: result.id, name: trimmedName || agentKey })
    } catch (err) {
      setError(err instanceof Error && err.message ? err.message : t('agent.createFailed'))
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
        onCancel={(id) => agentService.cancelSummon(id)}
      />
    )
  }

  return (
    <StepShell title={t('agent.title')} description={t('agent.description')}>
      <p className="flex flex-wrap items-center gap-x-4 gap-y-1 text-ui text-ink-secondary">
        <span className="flex items-center gap-2">
          {t('agent.provider')}
          <span className="rounded-pill border border-border-subtle bg-surface-raised px-2 py-0.5 text-caption font-medium text-ink">{providerLabel}</span>
        </span>
        {model && (
          <span className="flex items-center gap-2">
            {t('agent.model')}
            <span className="rounded-pill border border-border-subtle bg-surface-raised px-2 py-0.5 text-code text-ink">{model}</span>
          </span>
        )}
      </p>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="flex gap-2">
          <Field label={t('agent.emoji')} className="w-16 shrink-0">
            <Input value={emoji} maxLength={2} placeholder="🤖" className="text-center" onChange={(e) => setEmoji(e.target.value)} />
          </Field>
          <Field label={t('agent.name')} className="min-w-0 flex-1" required>
            <Input
              value={displayName}
              placeholder={t('agents:create.displayNamePlaceholder')}
              onChange={(e) => handleDisplayNameChange(e.target.value)}
            />
          </Field>
        </div>
        <Field
          label={t('agent.key')}
          helper={t('agents:create.agentKeyHint')}
          error={agentKey && !keyValid ? t('agents:create.agentKeyHint') : undefined}
          required
        >
          <Input
            value={agentKey}
            placeholder={t('agents:create.agentKeyPlaceholder')}
            className="font-mono"
            onChange={(e) => handleAgentKeyChange(e.target.value)}
            onBlur={(e) => setAgentKey(slugify(e.target.value))}
          />
        </Field>
      </div>

      <div className="flex flex-col gap-2">
        <span className="text-ui text-ink">{t('agent.personality')}</span>
        <div className="flex flex-wrap gap-1.5">
          {PRESET_KEYS.map((preset, idx) => (
            <button
              key={preset.key}
              type="button"
              aria-pressed={selectedPresetIdx === idx}
              onClick={() => handleSelectPreset(idx)}
              className={cn(
                'rounded-pill border px-3 py-1 text-caption transition-colors',
                selectedPresetIdx === idx
                  ? 'border-transparent bg-accent-subtle font-medium text-accent-subtle-ink'
                  : 'border-border-control text-ink-secondary hover:bg-surface-hover',
              )}
            >
              {t(`${preset.ns}:presets.${preset.key}.label`)}
            </button>
          ))}
        </div>
      </div>

      <Field label={t('agent.descriptionLabel')}>
        <Textarea
          value={description}
          rows={4}
          placeholder={t('agent.descPlaceholder')}
          onChange={(e) => handleDescriptionChange(e.target.value)}
        />
      </Field>

      {error && <StepError>{error}</StepError>}

      <div className="flex items-center justify-between gap-2">
        <Button variant="ghost" onClick={onBack}>{t('back')}</Button>
        <Button variant="primary" onClick={() => { void handleSubmit() }} loading={loading} disabled={!canSubmit}>
          {t('agent.create')}
        </Button>
      </div>
    </StepShell>
  )
}
