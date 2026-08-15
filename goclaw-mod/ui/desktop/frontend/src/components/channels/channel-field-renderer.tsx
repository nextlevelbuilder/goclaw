import { useTranslation } from 'react-i18next'
import { Switch } from '../common/Switch'
import { Combobox } from '../common/Combobox'
import type { FieldDef } from './channel-schemas'

interface ChannelFieldsProps {
  fields: FieldDef[]
  values: Record<string, unknown>
  onChange: (key: string, value: unknown) => void
  idPrefix: string
  isEdit?: boolean
  contextValues?: Record<string, unknown>
}

export function ChannelFields({ fields, values, onChange, idPrefix, isEdit, contextValues }: ChannelFieldsProps) {
  const allValues = contextValues ? { ...contextValues, ...values } : values
  return (
    <div className="space-y-3">
      {fields.map((field) => {
        if (field.showWhen) {
          const depValue = allValues[field.showWhen.key] ?? fields.find((f) => f.key === field.showWhen!.key)?.defaultValue
          if (String(depValue) !== field.showWhen.value) return null
        }
        let disabled = false
        let disabledHint: string | undefined
        if (field.disabledWhen) {
          const depValue = allValues[field.disabledWhen.key] ?? fields.find((f) => f.key === field.disabledWhen!.key)?.defaultValue
          if (String(depValue) === field.disabledWhen.value) {
            disabled = true
            disabledHint = field.disabledWhen.hint
          }
        }
        return (
          <FieldRenderer
            key={field.key}
            field={field}
            value={values[field.key]}
            onChange={(v) => onChange(field.key, v)}
            id={`${idPrefix}-${field.key}`}
            isEdit={isEdit}
            disabled={disabled}
            disabledHint={disabledHint}
          />
        )
      })}
    </div>
  )
}

interface FieldRendererProps {
  field: FieldDef
  value: unknown
  onChange: (v: unknown) => void
  id: string
  isEdit?: boolean
  disabled?: boolean
  disabledHint?: string
}

function FieldRenderer({ field, value, onChange, id, isEdit, disabled, disabledHint }: FieldRendererProps) {
  const { t } = useTranslation('channels')
  const label = field.label
  const help = field.help ?? ''
  const labelSuffix = field.required && !isEdit ? ' *' : ''
  const editHint = isEdit && field.type === 'password' ? ` ${t('form.credentialsHint')}` : ''

  const inputClass = 'w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent'

  switch (field.type) {
    case 'text':
    case 'password':
      return (
        <div className="space-y-1">
          <label htmlFor={id} className="text-xs font-medium text-text-secondary">{label}{labelSuffix}{editHint}</label>
          <input id={id} type={field.type} value={(value as string) ?? ''} onChange={(e) => onChange(e.target.value)} placeholder={field.placeholder} className={inputClass} />
          {help && <p className="text-[11px] text-text-muted">{help}</p>}
        </div>
      )

    case 'number':
      return (
        <div className="space-y-1">
          <label htmlFor={id} className="text-xs font-medium text-text-secondary">{label}{labelSuffix}</label>
          <input id={id} type="number" value={value !== undefined && value !== null ? String(value) : ''} onChange={(e) => onChange(e.target.value ? Number(e.target.value) : undefined)} placeholder={field.defaultValue !== undefined ? String(field.defaultValue) : undefined} className={inputClass} />
          {help && <p className="text-[11px] text-text-muted">{help}</p>}
        </div>
      )

    case 'boolean':
      return (
        <div className={`flex items-center gap-2${disabled ? ' opacity-50' : ''}`}>
          <Switch checked={(value as boolean) ?? (field.defaultValue as boolean) ?? false} onCheckedChange={(v) => onChange(v)} disabled={disabled} />
          <label className="text-xs text-text-secondary">{label}</label>
          {disabledHint && <span className="text-[11px] text-text-muted ml-1">— {disabledHint}</span>}
          {!disabledHint && help && <span className="text-[11px] text-text-muted ml-1">— {help}</span>}
        </div>
      )

    case 'select':
      return (
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{label}{labelSuffix}</label>
          <Combobox
            value={(value as string) ?? (field.defaultValue as string) ?? ''}
            onChange={(v) => onChange(v)}
            options={field.options?.map((opt) => ({ value: opt.value, label: opt.label })) ?? []}
            allowCustom={false}
          />
          {help && <p className="text-[11px] text-text-muted">{help}</p>}
        </div>
      )

    case 'tags':
      return (
        <div className="space-y-1">
          <label htmlFor={id} className="text-xs font-medium text-text-secondary">{label}</label>
          <textarea
            id={id}
            value={Array.isArray(value) ? (value as string[]).join('\n') : ''}
            onChange={(e) => {
              const lines = e.target.value.split(/[\n,]/).map((l) => l.trim()).filter(Boolean)
              onChange(lines.length > 0 ? lines : undefined)
            }}
            placeholder={field.placeholder ?? 'One per line or comma-separated'}
            rows={3}
            className={`${inputClass} font-mono resize-y`}
          />
          {help && <p className="text-[11px] text-text-muted">{help}</p>}
        </div>
      )

    default:
      return null
  }
}
