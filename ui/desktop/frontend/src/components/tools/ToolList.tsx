import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useBuiltinTools } from '../../hooks/use-builtin-tools'
import { IconGear, IconSearch, IconWrench } from '../common/Icons'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { cn } from '../../lib/cn'
import { Badge, Button, EmptyState, ErrorState, Input, PageHeader, SkeletonList, Switch, toast } from '../ui'
import { ToolSettingsDialog } from './ToolSettingsDialog'
import type { BuiltinToolData } from '../../types/builtin-tool'

const CATEGORY_ORDER = [
  'filesystem', 'runtime', 'web', 'memory', 'media', 'browser',
  'sessions', 'messaging', 'scheduling', 'subagents', 'skills', 'delegation', 'teams',
]

const MEDIA_TOOLS = new Set([
  'read_image', 'read_document', 'read_audio', 'read_video',
  'create_image', 'create_video', 'create_audio',
])

const HIDDEN_REQUIRES = new Set(['managed_mode', 'teams'])

function hasEditableSettings(tool: BuiltinToolData): boolean {
  // Media tools + web_fetch always show settings (even when empty — user can add providers)
  if (MEDIA_TOOLS.has(tool.name) || tool.name === 'web_fetch') return true
  return tool.settings != null && Object.keys(tool.settings).length > 0
}

function isDeprecated(tool: BuiltinToolData): boolean {
  return (tool.metadata as Record<string, unknown>)?.deprecated === true
}

/** Built-in tools grouped by category, each with an enable switch and, where it applies, a settings dialog. */
export function ToolList() {
  const { t } = useTranslation('tools')
  const { disconnected } = useGatewayStatus()
  const { tools, loading, error, fetchTools, toggleTool, updateSettings } = useBuiltinTools()
  const [search, setSearch] = useState('')
  const [settingsTool, setSettingsTool] = useState<BuiltinToolData | null>(null)
  const [retrying, setRetrying] = useState(false)

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return tools
    return tools.filter((x) =>
      x.display_name.toLowerCase().includes(q) || x.name.toLowerCase().includes(q) || x.description.toLowerCase().includes(q),
    )
  }, [tools, search])

  const grouped = useMemo(() => {
    const map = new Map<string, BuiltinToolData[]>()
    for (const x of filtered) {
      const cat = x.category || 'other'
      if (!map.has(cat)) map.set(cat, [])
      map.get(cat)!.push(x)
    }
    const sorted: [string, BuiltinToolData[]][] = []
    for (const cat of CATEGORY_ORDER) {
      if (map.has(cat)) {
        sorted.push([cat, map.get(cat)!])
        map.delete(cat)
      }
    }
    for (const entry of map) sorted.push(entry)
    return sorted
  }, [filtered])

  const retry = useCallback(async () => {
    setRetrying(true)
    try { await fetchTools() } finally { setRetrying(false) }
  }, [fetchTools])

  const handleToggle = async (tool: BuiltinToolData, enabled: boolean) => {
    const ok = await toggleTool(tool.name, enabled)
    if (!ok) toast.error(t('builtin.toggleFailed'), tool.display_name)
  }

  const handleSave = async (name: string, settings: Record<string, unknown>) => {
    try {
      await updateSettings(name, settings)
      toast.success(t('builtin.settingsDialog.toast.saved'))
    } catch (err) {
      toast.error(t('builtin.saveFailed'), err instanceof Error ? err.message : undefined)
      throw err
    }
  }

  const summary = t('builtin.summary', {
    tools: t(filtered.length === 1 ? 'builtin.toolCount' : 'builtin.toolCountPlural', { count: filtered.length }),
    categories: t('builtin.categoryCount', { count: grouped.length }),
  })

  let body
  if (error && tools.length === 0) {
    body = (
      <ErrorState
        title={t('builtin.errorTitle')}
        description={t('builtin.errorDescription')}
        onRetry={() => void retry()}
        retrying={retrying}
      />
    )
  } else if (loading) {
    body = <SkeletonList rows={8} label={t('builtin.loading')} />
  } else if (tools.length === 0) {
    body = <EmptyState icon={<IconWrench size={20} />} title={t('builtin.emptyTitle')} />
  } else {
    body = (
      <div className="flex flex-col gap-3">
        <div className="relative w-full max-w-72">
          <IconSearch size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-muted" />
          <Input
            type="search"
            aria-label={t('builtin.searchPlaceholder')}
            placeholder={t('builtin.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-8"
          />
        </div>
        {filtered.length === 0 ? (
          <EmptyState
            icon={<IconSearch size={20} />}
            title={t('builtin.noMatchTitle')}
            action={<Button variant="secondary" onClick={() => setSearch('')}>{t('builtin.clearSearch')}</Button>}
          />
        ) : (
          <div className="overflow-hidden rounded-card border border-border-subtle">
            {grouped.map(([category, items]) => (
              <section key={category} aria-label={t(`builtin.categories.${category}`, category)}>
                <div className="flex items-center gap-2 border-b border-border-subtle bg-surface-secondary px-4 py-2">
                  <h2 className="text-caption font-medium uppercase text-ink-secondary">{t(`builtin.categories.${category}`, category)}</h2>
                  <span className="rounded-pill bg-surface-tertiary px-1.5 text-caption text-ink-secondary">{items.length}</span>
                </div>
                <ul className="divide-y divide-border-subtle">
                  {items.map((tool) => (
                    <ToolRow
                      key={tool.name}
                      tool={tool}
                      readOnly={disconnected}
                      onToggle={(v) => void handleToggle(tool, v)}
                      onOpenSettings={() => setSettingsTool(tool)}
                    />
                  ))}
                </ul>
              </section>
            ))}
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader title={t('builtin.title')} description={!loading && !error ? summary : undefined} />
      <div className={cn('min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4', disconnected && 'opacity-60')}>{body}</div>

      {settingsTool && (
        <ToolSettingsDialog
          open
          onOpenChange={() => setSettingsTool(null)}
          tool={settingsTool}
          onSave={handleSave}
        />
      )}
    </div>
  )
}

function ToolRow({ tool, readOnly, onToggle, onOpenSettings }: {
  tool: BuiltinToolData
  readOnly: boolean
  onToggle: (enabled: boolean) => void
  onOpenSettings: () => void
}) {
  const { t } = useTranslation('tools')
  const deprecated = isDeprecated(tool)
  const editable = hasEditableSettings(tool) && !deprecated
  const needsSetup = editable && Object.keys(tool.settings ?? {}).length === 0
  const visibleRequires = (tool.requires ?? []).filter((r) => !HIDDEN_REQUIRES.has(r))

  return (
    <li className={cn('flex items-start justify-between gap-3 px-4 py-3', deprecated && 'opacity-60')}>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span id={`tool-${tool.name}`} className="text-ui font-medium text-ink">{tool.display_name}</span>
          <span className="rounded-sm bg-surface-tertiary px-1.5 font-mono text-caption text-ink-muted">{tool.name}</span>
          {needsSetup && <Badge tone="warning">{t('builtin.notConfigured')}</Badge>}
          {visibleRequires.length > 0 && !deprecated && (
            <Badge tone="neutral">{t('builtin.requiresTooltip', { list: visibleRequires.join(', ') })}</Badge>
          )}
        </div>
        {tool.description && <p className="mt-0.5 line-clamp-2 text-caption text-ink-muted">{tool.description}</p>}
      </div>

      <div className="flex shrink-0 items-center gap-2">
        {editable && (
          <Button
            size="sm"
            variant="ghost"
            icon={<IconGear size={14} />}
            aria-label={t('builtin.settingsLabel', { name: tool.display_name })}
            onClick={onOpenSettings}
            disabled={readOnly}
          >
            {t('builtin.settings')}
          </Button>
        )}
        <Switch
          checked={tool.enabled}
          onCheckedChange={onToggle}
          disabled={deprecated || readOnly}
          aria-labelledby={`tool-${tool.name}`}
        />
      </div>
    </li>
  )
}
