import { useTranslation } from 'react-i18next'
import { IconCheck, IconClose } from '../common/Icons'
import type { MCPTestResult } from '../../types/mcp'

interface McpTestResultProps {
  state: 'idle' | 'testing' | 'success' | 'error'
  result: MCPTestResult | null
}

/** Outcome of "Test connection": tools found, or the reason it failed. */
export function McpTestResult({ state, result }: McpTestResultProps) {
  const { t } = useTranslation('mcp')

  if (state === 'success' && result) {
    return (
      <p role="status" className="flex items-center gap-1.5 text-caption text-success">
        <IconCheck size={14} className="shrink-0" />
        {t('form.toolsFound', { count: result.tool_count ?? 0 })}
      </p>
    )
  }

  if (state === 'error' && result) {
    return (
      <p role="alert" className="flex items-start gap-1.5 text-caption text-danger">
        <IconClose size={14} className="mt-0.5 shrink-0" />
        <span className="break-all">{result.error || t('form.errors.connectionFailed')}</span>
      </p>
    )
  }

  return null
}
