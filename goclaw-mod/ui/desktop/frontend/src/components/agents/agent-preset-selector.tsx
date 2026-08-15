import { useTranslation } from 'react-i18next'

const PRESET_KEYS = [
  { key: 'foxSpirit', emoji: '🦊', ns: 'agents', agentKey: 'little-fox' },
  { key: 'artisan', emoji: '🎨', ns: 'agents', agentKey: 'artisan' },
  { key: 'astrologer', emoji: '🔮', ns: 'agents', agentKey: 'mimi' },
  { key: 'researcher', emoji: '🔬', ns: 'desktop', agentKey: 'scholar' },
  { key: 'writer', emoji: '✍️', ns: 'desktop', agentKey: 'quill' },
  { key: 'coder', emoji: '👨‍💻', ns: 'desktop', agentKey: 'dev' },
]

interface AgentPresetSelectorProps {
  currentDescription: string
  onSelect: (preset: { description: string; emoji: string; displayName: string; agentKey: string }) => void
}

export function AgentPresetSelector({ currentDescription, onSelect }: AgentPresetSelectorProps) {
  const { t } = useTranslation(['agents', 'desktop'])

  return (
    <div className="flex flex-wrap gap-1.5">
      {PRESET_KEYS.map((p) => {
        const label = t(`${p.ns}:presets.${p.key}.label`)
        const prompt = t(`${p.ns}:presets.${p.key}.prompt`)
        const nameMatch = prompt.match(/^Name:\s*([^.]+)/)
        const presetDisplayName = nameMatch ? nameMatch[1].trim() : label.replace(/^\S+\s*/, '')
        return (
          <button
            key={p.key}
            type="button"
            onClick={() => onSelect({ description: prompt, emoji: p.emoji, displayName: presetDisplayName, agentKey: p.agentKey })}
            className={`rounded-full border px-2.5 py-1 text-[11px] transition-colors ${
              currentDescription === prompt
                ? 'border-accent bg-accent/10 text-accent font-medium'
                : 'border-border text-text-secondary hover:bg-surface-tertiary hover:text-text-primary'
            }`}
          >
            {label}
          </button>
        )
      })}
    </div>
  )
}
