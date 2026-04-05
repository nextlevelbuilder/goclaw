import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChannelFields } from './channel-field-renderer'
import { credentialsSchema } from './channel-schemas'
import type { ChannelInstanceData } from '../../../types/channel'

interface ChannelCredentialsTabProps {
  instance: ChannelInstanceData
  onUpdate: (updates: Record<string, unknown>) => Promise<void>
}

export function ChannelCredentialsTab({ instance, onUpdate }: ChannelCredentialsTabProps) {
  const { t } = useTranslation('channels')
  const fields = credentialsSchema[instance.channel_type] ?? []
  const [values, setValues] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)

  const handleChange = (key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: String(value ?? '') }))
  }

  const handleSave = async () => {
    // Only send non-empty values (empty = keep current)
    const filtered = Object.fromEntries(
      Object.entries(values).filter(([, v]) => v.trim() !== ''),
    )
    if (Object.keys(filtered).length === 0) return

    setSaving(true)
    try {
      await onUpdate({ credentials: filtered })
      setValues({}) // Clear form after save
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false)
    }
  }

  if (fields.length === 0) {
    return <p className="text-sm text-text-muted py-4">No credentials required for this channel type.</p>
  }

  return (
    <div className="space-y-4">
      <p className="text-xs text-text-muted">{t('detail.credentials.hint')}</p>

      <ChannelFields
        fields={fields}
        values={values}
        onChange={handleChange}
        idPrefix="cc-cred"
        isEdit
      />

      <button
        onClick={handleSave}
        disabled={saving}
        className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 cursor-pointer"
      >
        {saving ? t('detail.credentials.saving') : t('detail.credentials.updateCredentials')}
      </button>
    </div>
  )
}
