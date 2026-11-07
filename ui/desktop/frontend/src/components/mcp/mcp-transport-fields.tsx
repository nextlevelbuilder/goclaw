import { useTranslation } from 'react-i18next'
import { Field, Input } from '../ui'
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
  /** Field errors, already translated. */
  errors?: { command?: string; url?: string }
  readOnly?: boolean
}

export function McpTransportFields({ transport, headers, env, register, setValue, errors = {}, readOnly = false }: McpTransportFieldsProps) {
  const { t } = useTranslation('mcp')

  return (
    <>
      {transport === 'stdio' && (
        <>
          <Field label={t('form.command')} required error={errors.command}>
            <Input {...register('command')} placeholder="npx" className="font-mono" disabled={readOnly} />
          </Field>
          <Field label={t('form.args')}>
            <Input {...register('args')} placeholder={t('form.argsPlaceholder')} className="font-mono" disabled={readOnly} />
          </Field>
        </>
      )}

      {transport !== 'stdio' && (
        <>
          <Field label={t('form.url')} required error={errors.url}>
            <Input {...register('url')} placeholder="http://localhost:3001/sse" className="font-mono" disabled={readOnly} />
          </Field>
          <Field label={t('form.headers')}>
            <KeyValueEditor
              value={headers}
              onChange={(v) => setValue('headers', v)}
              sensitivePattern={SENSITIVE_HEADER_KEYS}
              placeholder={{ key: t('form.headerKeyPlaceholder'), value: t('form.headerValuePlaceholder') }}
              addLabel={t('form.addHeader')}
              disabled={readOnly}
            />
          </Field>
        </>
      )}

      <Field label={t('form.env')}>
        <KeyValueEditor
          value={env}
          onChange={(v) => setValue('env', v)}
          sensitivePattern={SENSITIVE_ENV_KEYS}
          placeholder={{ key: t('form.envKeyPlaceholder'), value: t('form.envValuePlaceholder') }}
          addLabel={t('form.addVariable')}
          disabled={readOnly}
        />
      </Field>
    </>
  )
}
