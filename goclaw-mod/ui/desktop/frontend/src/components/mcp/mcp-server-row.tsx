// MCP server table row with status badge and action buttons.

import { useTranslation } from 'react-i18next'
import type { MCPServerData } from '../../types/mcp'

interface McpServerRowProps {
  server: MCPServerData
  reconnectingId: string | null
  onReconnect: (id: string) => void
  onEdit: (server: MCPServerData) => void
  onDelete: (server: MCPServerData) => void
  onViewTools: (server: MCPServerData) => void
  onManageGrants: (server: MCPServerData) => void
}

export function McpServerRow({
  server: s, reconnectingId, onReconnect, onEdit, onDelete, onViewTools, onManageGrants,
}: McpServerRowProps) {
  const { t } = useTranslation(['mcp', 'common'])

  return (
    <tr className="border-b border-border last:border-0 hover:bg-surface-tertiary/30 transition-colors [&>td]:align-middle">
      {/* Name */}
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <svg className="h-4 w-4 text-text-muted shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 22v-5" /><path d="M9 8V2" /><path d="M15 8V2" />
            <path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z" />
          </svg>
          <div className="min-w-0">
            <div className="text-sm font-medium text-text-primary truncate">{s.display_name || s.name}</div>
            <div className="font-mono text-[11px] text-text-muted bg-surface-tertiary/50 px-1.5 py-0.5 rounded inline-block mt-0.5">
              mcp_{s.name}
            </div>
          </div>
        </div>
      </td>
      {/* Transport */}
      <td className="px-4 py-3">
        <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
          s.transport === 'sse'
            ? 'bg-surface-tertiary text-text-secondary'
            : 'border border-border text-text-muted'
        }`}>
          {s.transport.toUpperCase()}
        </span>
      </td>
      {/* Tools */}
      <td className="px-4 py-3 text-center">
        <button
          onClick={() => onViewTools(s)}
          className="p-1 text-text-muted hover:text-text-primary transition-colors"
          title={t('viewTools')}
        >
          <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
          </svg>
        </button>
      </td>
      {/* Agents */}
      <td className="px-4 py-3 text-center">
        <button
          onClick={() => onManageGrants(s)}
          className="inline-flex items-center gap-1 text-sm text-text-primary hover:text-accent transition-colors"
          title={t('manageGrants')}
        >
          <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
            <circle cx="9" cy="7" r="4" />
            <path d="M22 21v-2a4 4 0 0 0-3-3.87" /><path d="M16 3.13a4 4 0 0 1 0 7.75" />
          </svg>
          {s.agent_count ?? 0}
        </button>
      </td>
      {/* Enabled */}
      <td className="px-4 py-3">
        <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
          s.enabled
            ? 'bg-emerald-500/15 text-emerald-700 border border-emerald-500/25 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/20'
            : 'bg-surface-tertiary text-text-secondary'
        }`}>
          {s.enabled ? t('common:enabled') : t('common:disabled')}
        </span>
      </td>
      {/* Actions */}
      <td className="px-4 py-3 text-right">
        <div className="flex items-center justify-end gap-1">
          <button
            disabled={reconnectingId === s.id}
            onClick={() => onReconnect(s.id)}
            className="p-1 text-text-muted hover:text-text-primary transition-colors disabled:opacity-50"
            title={t('reconnect')}
          >
            <svg className={`h-3.5 w-3.5${reconnectingId === s.id ? ' animate-spin' : ''}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8" /><path d="M21 3v5h-5" />
            </svg>
          </button>
          <button onClick={() => onEdit(s)} className="p-1 text-text-muted hover:text-text-primary transition-colors" title="Edit">
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z" />
            </svg>
          </button>
          <button onClick={() => onDelete(s)} className="p-1 text-text-muted hover:text-error transition-colors" title="Delete">
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
            </svg>
          </button>
        </div>
      </td>
    </tr>
  )
}
