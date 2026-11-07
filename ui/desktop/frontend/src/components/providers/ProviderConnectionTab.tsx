import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { PROVIDER_TYPES } from '../../constants/providers'
import { providerService, type ProviderVerifyResult } from '../../services/provider-service'
import { Button } from '../ui'
import { IconCheckCircle, IconXCircle } from '../common/Icons'
import { ProviderMetaChips } from './ProviderMetaChips'
import type { ProviderData } from '../../types/provider'

interface ProviderConnectionTabProps {
  provider: ProviderData
  onEdit: () => void
  /** Disables writes while the gateway is unreachable. */
  disabled?: boolean
}

/** Read-only summary of the provider connection; editing goes through ProviderFormDialog. */
export function ProviderConnectionTab({ provider, onEdit, disabled = false }: ProviderConnectionTabProps) {
  const { t } = useTranslation('providers')
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<ProviderVerifyResult | null>(null)
  const typeLabel = PROVIDER_TYPES.find((p) => p.value === provider.provider_type)?.label ?? provider.provider_type
  const notSet = <span className="text-ink-muted">{t('providerDetail.notSet')}</span>

  const testConnection = async () => {
    setTesting(true)
    setResult(null)
    try {
      setResult(await providerService.verify(provider.id))
    } catch (err) {
      setResult({ valid: false, error: (err as Error).message })
    } finally {
      setTesting(false)
    }
  }

  const rows: { label: string; value: ReactNode; mono?: boolean }[] = [
    { label: t('providerDetail.fields.name'), value: provider.name, mono: true },
    { label: t('providerDetail.fields.type'), value: typeLabel },
    { label: t('providerDetail.fields.apiBase'), value: provider.api_base || notSet, mono: !!provider.api_base },
    { label: t('providerDetail.fields.apiKey'), value: provider.api_key ? t('providerDetail.apiKeySet') : t('providerDetail.apiKeyNotSet') },
    {
      label: t('providerDetail.fields.status'),
      value: provider.enabled ? t('providerDetail.status.running') : t('providerDetail.status.disabled'),
    },
    { label: t('providerDetail.fields.defaultModel'), value: provider.default_model_id || notSet, mono: !!provider.default_model_id },
    { label: t('providerDetail.fields.models'), value: provider.models?.length ? String(provider.models.length) : notSet },
  ]

  return (
    <div className="flex max-w-reading flex-col gap-4">
      <dl className="grid grid-cols-1 gap-x-4 gap-y-2 text-ui sm:grid-cols-[180px_1fr]">
        {rows.map((r) => (
          <div key={r.label} className="contents">
            <dt className="text-ink-muted">{r.label}</dt>
            <dd className={r.mono ? 'break-all text-code text-ink' : 'break-all text-ink'}>{r.value}</dd>
          </div>
        ))}
        {(provider.wire_api || provider.auth_kind || provider.model_source) && (
          <>
            <dt className="text-ink-muted">{t('providerDetail.fields.transportAuth')}</dt>
            <dd><ProviderMetaChips provider={provider} /></dd>
          </>
        )}
      </dl>

      <div className="flex flex-wrap items-center gap-2">
        <Button variant="primary" onClick={onEdit} disabled={disabled}>{t('providerDetail.edit')}</Button>
        <Button variant="secondary" onClick={testConnection} loading={testing} disabled={disabled}>
          {t('form.testConnection')}
        </Button>
      </div>

      {result && (
        <p
          role={result.valid ? 'status' : 'alert'}
          className={result.valid ? 'flex items-start gap-1.5 text-caption text-success' : 'flex items-start gap-1.5 text-caption text-danger'}
        >
          {result.valid ? <IconCheckCircle size={14} className="mt-px shrink-0" /> : <IconXCircle size={14} className="mt-px shrink-0" />}
          <span>{result.valid ? t('form.testOk') : `${t('form.testFailed')}${result.error ? `: ${result.error}` : ''}`}</span>
        </p>
      )}
    </div>
  )
}
