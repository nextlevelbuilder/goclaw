interface PromptModeSectionProps {
  mode: string
  onModeChange: (mode: string) => void
}

const PROMPT_MODES = ['full', 'task', 'minimal', 'none'] as const

const MODE_SECTIONS: Record<string, string[]> = {
  full: ['persona', 'tools', 'exec-bias', 'call-style', 'safety', 'skills', 'mcp', 'memory', 'sandbox', 'evolution', 'channel'],
  task: ['style', 'tools', 'exec-bias', 'safety-sm', 'skill-search', 'mcp-search', 'memory-sm'],
  minimal: ['tools', 'safety', 'pinned'],
  none: [],
}

const MODE_TOKENS: Record<string, string> = {
  full: '~3.2K', task: '~2.3K', minimal: '~1.4K', none: '~6',
}

const MODE_ICONS: Record<string, string> = {
  full: '⚡', task: '🔧', minimal: '📦', none: '⛔',
}

const MODE_DESC: Record<string, string> = {
  full: 'Complete system prompt with all sections',
  task: 'Optimized for sub-agent and cron tasks',
  minimal: 'Essential tools and safety only',
  none: 'No system prompt injected',
}

export function PromptModeSection({ mode, onModeChange }: PromptModeSectionProps) {
  const sections = MODE_SECTIONS[mode] ?? []

  return (
    <div className="space-y-3">
      <h3 className="text-sm font-semibold text-text-primary">System Prompt Mode</h3>

      {/* 2x2 mode grid */}
      <div className="grid grid-cols-2 gap-2">
        {PROMPT_MODES.map((m) => (
          <button
            key={m}
            onClick={() => onModeChange(m)}
            className={[
              'flex flex-col items-start gap-1 rounded-lg border p-3 text-left transition-colors',
              mode === m
                ? 'ring-2 ring-accent border-accent bg-accent/5'
                : 'border-border hover:bg-surface-tertiary',
            ].join(' ')}
          >
            <div className="flex items-center gap-1.5">
              <span className="text-base">{MODE_ICONS[m]}</span>
              <span className="text-xs font-medium text-text-primary capitalize">{m}</span>
              <span className="text-[9px] px-1 py-0.5 rounded bg-surface-tertiary text-text-muted ml-auto">
                {MODE_TOKENS[m]}
              </span>
            </div>
            <p className="text-[10px] text-text-muted leading-tight">{MODE_DESC[m]}</p>
          </button>
        ))}
      </div>

      {/* Section badges */}
      {sections.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {sections.map((s) => (
            <span key={s} className="rounded bg-surface-tertiary px-1.5 py-0.5 text-[9px] text-text-muted">
              {s}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
