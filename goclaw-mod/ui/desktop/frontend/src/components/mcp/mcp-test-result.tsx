import { useTranslation } from 'react-i18next'
import type { MCPTestResult } from '../../types/mcp'

interface McpTestResultProps {
  state: 'idle' | 'testing' | 'success' | 'error'
  result: MCPTestResult | null
}

export function McpTestResult({ state, result }: McpTestResultProps) {
  const { t } = useTranslation('mcp')

  if (state === 'success' && result) {
    return (
      <p className="text-[11px] text-emerald-600 dark:text-emerald-400 flex items-center gap-1">
        <svg className="h-3.5 w-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M20 6 9 17l-5-5" />
        </svg>
        {t('form.toolsFound', { count: result.tool_count })}
      </p>
    )
  }

  if (state === 'error' && result) {
    return (
      <p className="text-[11px] text-error flex items-center gap-1">
        <svg className="h-3.5 w-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M18 6 6 18" /><path d="m6 6 12 12" />
        </svg>
        <span className="break-all">{result.error || t('form.errors.connectionFailed')}</span>
      </p>
    )
  }

  return null
}
