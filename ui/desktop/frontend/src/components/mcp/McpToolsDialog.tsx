import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge, EmptyState, ErrorState, Input, SkeletonList } from '../ui'
import { IconWrench } from '../common/Icons'
import { Dialog } from '../ui'
import type { MCPServerData, MCPToolInfo } from '../../types/mcp'

interface McpToolsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  server: MCPServerData
  onLoadTools: (serverId: string) => Promise<MCPToolInfo[]>
}

export function McpToolsDialog({ open, onOpenChange, server, onLoadTools }: McpToolsDialogProps) {
  const { t } = useTranslation(['mcp', 'common'])
  const [tools, setTools] = useState<MCPToolInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')

  const load = useCallback(() => {
    setLoading(true)
    setError('')
    onLoadTools(server.id)
      .then(setTools)
      .catch((err) => setError(err?.message || t('tools.failedLoad')))
      .finally(() => setLoading(false))
  }, [onLoadTools, server.id, t])

  useEffect(() => {
    if (open) { setQuery(''); load() }
  }, [open, load])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? tools.filter((tool) => `${tool.name} ${tool.description ?? ''}`.toLowerCase().includes(q)) : tools
  }, [tools, query])

  let body
  if (loading) {
    body = <SkeletonList rows={4} avatar={false} label={t('tools.discovering')} />
  } else if (error) {
    body = <ErrorState title={t('tools.failedLoad')} description={error} onRetry={load} className="py-6" />
  } else if (tools.length === 0) {
    body = <EmptyState title={t('tools.noToolsTitle')} description={t('tools.noToolsDescription')} icon={<IconWrench size={20} />} className="py-6" />
  } else {
    body = (
      <>
        <Input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t('tools.filterPlaceholder')}
          aria-label={t('tools.filterPlaceholder')}
        />
        {filtered.length === 0 ? (
          <p className="py-4 text-center text-ui font-normal text-ink-muted">{t('tools.noMatch')}</p>
        ) : (
          <ul className="flex max-h-[50vh] flex-col gap-1 overflow-y-auto overscroll-contain">
            {filtered.map((tool) => (
              <li key={tool.name} className="rounded-md px-3 py-2 hover:bg-surface-hover">
                <p className="truncate font-mono text-code text-ink">{tool.name}</p>
                {tool.description && <p className="line-clamp-2 text-caption font-normal text-ink-muted">{tool.description}</p>}
              </li>
            ))}
          </ul>
        )}
      </>
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={
        <span className="flex items-center gap-2">
          {t('tools.title', { name: server.display_name || server.name })}
          {!loading && tools.length > 0 && <Badge>{t('screen.tools.count', { count: tools.length })}</Badge>}
        </span>
      }
      description={<span className="font-mono text-code">{t('tools.prefix')} mcp_{server.name}</span>}
    >
      <div className="flex flex-col gap-3">{body}</div>
    </Dialog>
  )
}
