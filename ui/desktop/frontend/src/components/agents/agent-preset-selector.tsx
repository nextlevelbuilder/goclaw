import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'

export interface AgentPreset {
  key: string
  emoji: string
  ns: 'agents' | 'desktop'
  agentKey: string
}

export const AGENT_PRESETS: AgentPreset[] = [
  { key: 'foxSpirit', emoji: '🦊', ns: 'agents', agentKey: 'little-fox' },
  { key: 'artisan', emoji: '🎨', ns: 'agents', agentKey: 'artisan' },
  { key: 'astrologer', emoji: '🔮', ns: 'agents', agentKey: 'mimi' },
  { key: 'researcher', emoji: '🔬', ns: 'desktop', agentKey: 'scholar' },
  { key: 'writer', emoji: '✍️', ns: 'desktop', agentKey: 'quill' },
  { key: 'coder', emoji: '👨‍💻', ns: 'desktop', agentKey: 'dev' },
]

export interface ResolvedPreset {
  label: string
  description: string
  emoji: string
  displayName: string
  agentKey: string
}

/** Translated label, persona prompt and derived display name of a preset. */
export function resolvePreset(t: (key: string) => string, p: AgentPreset): ResolvedPreset {
  const label = t(`${p.ns}:presets.${p.key}.label`)
  const prompt = t(`${p.ns}:presets.${p.key}.prompt`)
  const nameMatch = prompt.match(/^Name:\s*([^.]+)/)
  const displayName = nameMatch ? nameMatch[1].trim() : label.replace(/^\S+\s*/, '')
  return { label, description: prompt, emoji: p.emoji, displayName, agentKey: p.agentKey }
}

interface AgentPresetSelectorProps {
  currentDescription: string
  onSelect: (preset: { description: string; emoji: string; displayName: string; agentKey: string }) => void
}

export function AgentPresetSelector({ currentDescription, onSelect }: AgentPresetSelectorProps) {
  const { t } = useTranslation(['agents', 'desktop'])

  return (
    <div className="flex flex-wrap gap-1.5">
      {AGENT_PRESETS.map((p) => {
        const r = resolvePreset(t, p)
        const selected = currentDescription === r.description
        return (
          <button
            key={p.key}
            type="button"
            aria-pressed={selected}
            onClick={() => onSelect(r)}
            className={cn(
              'h-7 rounded-pill border px-2.5 text-caption transition-colors',
              selected
                ? 'border-accent bg-accent-subtle font-medium text-accent-subtle-ink'
                : 'border-border-control text-ink-secondary hover:bg-surface-hover hover:text-ink',
            )}
          >
            {r.label}
          </button>
        )
      })}
    </div>
  )
}
