import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { MCPServerData, MCPToolInfo } from '../../../types/mcp'

interface McpToolsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  server: MCPServerData
  onLoadTools: (serverId: string) => Promise<MCPToolInfo[]>
}

export function McpToolsDialog({ open, onOpenChange, server, onLoadTools }: McpToolsDialogProps) {
  const { t } = useTranslation('mcp')
  const [tools, setTools] = useState<MCPToolInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) return
    setLoading(true)
    setError('')
    onLoadTools(server.id)
      .then(setTools)
      .catch((err) => setError(err?.message ?? 'Failed to load tools'))
      .finally(() => setLoading(false))
  }, [open, server.id, onLoadTools])

  if (!open) return null

  const displayName = server.display_name || server.name

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={() => onOpenChange(false)} />
      <div className="relative w-full max-w-md bg-surface-secondary rounded-xl border border-border overflow-hidden flex flex-col" style={{ maxHeight: '70vh' }}>
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4 shrink-0">
          <div>
            <div className="flex items-center gap-2">
              <svg className="h-4 w-4 text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
              </svg>
              <span className="text-sm font-semibold text-text-primary">{t('tools.title', { name: displayName })}</span>
              {!loading && tools.length > 0 && (
                <span className="bg-surface-tertiary text-text-secondary rounded-full px-2 py-0.5 text-[11px]">
                  {tools.length} tool{tools.length !== 1 ? 's' : ''}
                </span>
              )}
            </div>
            <p className="font-mono text-[11px] text-text-muted mt-0.5">{t('tools.prefix')} mcp_{server.name}</p>
          </div>
          <button onClick={() => onOpenChange(false)} className="p-1 text-text-muted hover:text-text-primary transition-colors">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>

        {/* Content — scrollable at edges */}
        {loading ? (
          <div className="flex items-center justify-center gap-2 py-8 px-5">
            <svg className="h-4 w-4 animate-spin text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
            <span className="text-xs text-text-muted">{t('tools.discovering')}</span>
          </div>
        ) : error ? (
          <p className="text-xs text-error text-center py-8 px-5">{error}</p>
        ) : tools.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-8 px-5">
            <svg className="h-10 w-10 text-text-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
              <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
            </svg>
            <p className="text-sm text-text-muted">{t('tools.noToolsTitle')}</p>
            <p className="text-xs text-text-muted/70">{t('tools.noToolsDescription')}</p>
          </div>
        ) : (
          <div className="overflow-y-auto overscroll-contain flex-1">
            <div className="px-5 py-3 space-y-1">
              {tools.map((tool) => (
                <div key={tool.name} className="px-3 py-2 rounded-lg bg-surface-tertiary/30 hover:bg-surface-tertiary/50 transition-colors">
                  <div className="flex items-center gap-2">
                    <svg className="h-3.5 w-3.5 text-text-muted shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                      <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
                    </svg>
                    <span className="font-mono text-sm text-text-primary truncate">{tool.name}</span>
                  </div>
                  {tool.description && (
                    <p className="text-xs text-text-muted line-clamp-2 ml-5 mt-0.5">{tool.description}</p>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
