import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useBuiltinTools } from '../../../hooks/use-builtin-tools'
import { Switch } from '../../common/Switch'
import { RefreshButton } from '../../common/RefreshButton'
import { ToolSettingsDialog } from './ToolSettingsDialog'
import type { BuiltinToolData } from '../../../types/builtin-tool'

const CATEGORY_ORDER = [
  'filesystem', 'runtime', 'web', 'memory', 'media', 'browser',
  'sessions', 'messaging', 'scheduling', 'subagents', 'skills', 'delegation', 'teams',
]

const MEDIA_TOOLS = new Set([
  'read_image', 'read_document', 'read_audio', 'read_video',
  'create_image', 'create_video', 'create_audio',
])

function hasEditableSettings(tool: BuiltinToolData): boolean {
  // Media tools + web_fetch always show settings (even when empty — user can add providers)
  if (MEDIA_TOOLS.has(tool.name) || tool.name === 'web_fetch') return true
  return tool.settings != null && Object.keys(tool.settings).length > 0
}

function isDeprecated(tool: BuiltinToolData): boolean {
  return (tool.metadata as Record<string, unknown>)?.deprecated === true
}

export function ToolList() {
  const { t } = useTranslation('tools')
  const { tools, loading, fetchTools, toggleTool, updateSettings } = useBuiltinTools()
  const [search, setSearch] = useState('')
  const [settingsTool, setSettingsTool] = useState<BuiltinToolData | null>(null)

  const filtered = useMemo(() => {
    if (!search.trim()) return tools
    const q = search.toLowerCase()
    return tools.filter((t) =>
      t.display_name.toLowerCase().includes(q)
      || t.name.toLowerCase().includes(q)
      || t.description.toLowerCase().includes(q)
    )
  }, [tools, search])

  const grouped = useMemo(() => {
    const map = new Map<string, BuiltinToolData[]>()
    for (const t of filtered) {
      const cat = t.category || 'other'
      if (!map.has(cat)) map.set(cat, [])
      map.get(cat)!.push(t)
    }
    const sorted: [string, BuiltinToolData[]][] = []
    for (const cat of CATEGORY_ORDER) {
      if (map.has(cat)) {
        sorted.push([cat, map.get(cat)!])
        map.delete(cat)
      }
    }
    for (const [cat, items] of map) {
      sorted.push([cat, items])
    }
    return sorted
  }, [filtered])

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">{t('builtin.title')}</h2>
          <p className="text-xs text-text-muted mt-0.5">
            {filtered.length !== 1 ? t('builtin.toolCountPlural', { count: filtered.length }) : t('builtin.toolCount', { count: filtered.length })} · {t('builtin.categoryCount', { count: grouped.length })}
          </p>
        </div>
        <RefreshButton onRefresh={fetchTools} />
      </div>

      {/* Search */}
      {tools.length > 10 && (
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('builtin.searchPlaceholder')}
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
        />
      )}

      {/* Loading */}
      {loading ? (
        <div className="space-y-2">
          {[1, 2, 3, 4, 5, 6, 7, 8].map((i) => (
            <div key={i} className="h-10 rounded-lg bg-surface-tertiary/50 animate-pulse" />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        /* Empty */
        <div className="flex flex-col items-center gap-2 py-12">
          <svg className="h-10 w-10 text-text-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
            <path d="M11 21.73a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73z" />
            <path d="M12 22V12" /><path d="m3.3 7 7.703 4.734a2 2 0 0 0 1.994 0L20.7 7" />
          </svg>
          <p className="text-sm text-text-muted">{search ? t('builtin.noMatchTitle') : t('builtin.emptyTitle')}</p>
        </div>
      ) : (
        /* Category groups */
        <div className="rounded-lg border border-border overflow-hidden">
          {grouped.map(([category, items]) => (
            <div key={category}>
              {/* Category header */}
              <div className="flex items-center gap-2 border-b border-border bg-surface-tertiary/40 px-4 py-2">
                <span className="text-xs font-medium text-text-muted uppercase tracking-wide">{t(`builtin.categories.${category}`, category)}</span>
                <span className="bg-surface-tertiary text-text-secondary rounded-full px-1.5 text-[11px]">{items.length}</span>
              </div>
              {/* Tool rows */}
              <div className="divide-y divide-border">
                {items.map((tool) => (
                  <ToolRow
                    key={tool.name}
                    tool={tool}
                    onToggle={toggleTool}
                    onOpenSettings={() => setSettingsTool(tool)}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Settings dialog */}
      {settingsTool && (
        <ToolSettingsDialog
          open
          onOpenChange={() => setSettingsTool(null)}
          tool={settingsTool}
          onSave={updateSettings}
        />
      )}
    </div>
  )
}

/* --- ToolRow --- */

const HIDDEN_REQUIRES = new Set(['managed_mode', 'teams'])

function ToolRow({ tool, onToggle, onOpenSettings }: {
  tool: BuiltinToolData
  onToggle: (name: string, enabled: boolean) => void
  onOpenSettings: () => void
}) {
  const { t } = useTranslation('tools')
  const deprecated = isDeprecated(tool)
  const editable = hasEditableSettings(tool) && !deprecated
  const visibleRequires = (tool.requires ?? []).filter((r) => !HIDDEN_REQUIRES.has(r))

  return (
    <div className={`px-4 py-3 hover:bg-surface-tertiary/30 transition-colors flex items-start justify-between gap-3 ${deprecated ? 'opacity-60' : ''}`}>
      {/* Left */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-sm font-medium text-text-primary leading-tight">{tool.display_name}</span>
          <span className="font-mono text-[11px] text-text-muted bg-surface-tertiary/50 px-1.5 py-0.5 rounded">{tool.name}</span>
        </div>
        {visibleRequires.length > 0 && !deprecated && (
          <span className="inline-block mt-1 text-[10px] border border-border rounded-full px-1.5 py-0.5 text-text-muted">
            {t('builtin.requiresTooltip', { list: visibleRequires.join(', ') })}
          </span>
        )}
        {tool.description && (
          <p className="text-xs text-text-muted leading-snug mt-0.5 line-clamp-2">{tool.description}</p>
        )}
      </div>

      {/* Right */}
      <div className="flex items-center gap-1.5 shrink-0">
        {editable && (
          <button
            onClick={onOpenSettings}
            className="rounded px-2 py-1 text-[11px] text-text-secondary hover:bg-surface-tertiary transition-colors flex items-center gap-1"
          >
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
              <circle cx="12" cy="12" r="3" />
            </svg>
            {t('builtin.settings')}
          </button>
        )}
        <Switch
          checked={tool.enabled}
          onCheckedChange={(v) => onToggle(tool.name, v)}
          disabled={deprecated}
        />
      </div>
    </div>
  )
}
