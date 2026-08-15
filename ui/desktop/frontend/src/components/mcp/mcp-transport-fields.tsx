import { useTranslation } from 'react-i18next'
import { KeyValueEditor } from '../common/KeyValueEditor'
import type { UseFormRegister, UseFormSetValue } from 'react-hook-form'
import type { MCPFormData } from '../../schemas/mcp.schema'

const SENSITIVE_HEADER_KEYS = /(auth|api-key|api_key|bearer|token|secret|password|credential)/i
const SENSITIVE_ENV_KEYS = /(key|secret|token|password|credential)/i

interface McpTransportFieldsProps {
  transport: string
  headers: Record<string, string>
  env: Record<string, string>
  register: UseFormRegister<MCPFormData>
  setValue: UseFormSetValue<MCPFormData>
}

export function McpTransportFields({ transport, headers, env, register, setValue }: McpTransportFieldsProps) {
  const { t } = useTranslation('mcp')

  return (
    <>
      {/* Stdio fields */}
      {transport === 'stdio' && (
        <>
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.command')}</label>
            <input {...register('command')} placeholder="npx" className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 font-mono text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent" />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.args')}</label>
            <input {...register('args')} placeholder={t('form.argsPlaceholder')} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 font-mono text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent" />
          </div>
        </>
      )}

      {/* SSE/HTTP fields */}
      {transport !== 'stdio' && (
        <>
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.url')}</label>
            <input {...register('url')} placeholder="http://localhost:3001/sse" className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 font-mono text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent" />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.headers')}</label>
            <KeyValueEditor value={headers} onChange={(v) => setValue('headers', v)} sensitivePattern={SENSITIVE_HEADER_KEYS} placeholder={{ key: t('form.headerKeyPlaceholder'), value: t('form.headerValuePlaceholder') }} />
          </div>
        </>
      )}

      {/* Env Variables */}
      <div className="space-y-1">
        <label className="text-xs font-medium text-text-secondary">{t('form.env')}</label>
        <KeyValueEditor value={env} onChange={(v) => setValue('env', v)} sensitivePattern={SENSITIVE_ENV_KEYS} placeholder={{ key: t('form.envKeyPlaceholder'), value: t('form.envValuePlaceholder') }} />
      </div>
    </>
  )
}
