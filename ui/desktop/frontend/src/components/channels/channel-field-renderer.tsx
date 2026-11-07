import { useTranslation } from 'react-i18next'
import { Field, Input, Select, Switch, Textarea } from '../ui'
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
    <div className="flex flex-col gap-4">
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
  const help = field.help
  const required = !!field.required && !isEdit
  const editHint = isEdit && field.type === 'password' ? t('form.credentialsHint') : undefined
  const helper = [help, editHint].filter(Boolean).join(' ') || undefined

  switch (field.type) {
    case 'text':
    case 'password':
      return (
        <Field label={label} helper={helper} required={required}>
          <Input id={id} type={field.type} value={(value as string) ?? ''} onChange={(e) => onChange(e.target.value)} placeholder={field.placeholder} autoComplete="off" />
        </Field>
      )

    case 'number':
      return (
        <Field label={label} helper={help} required={required}>
          <Input
            id={id}
            type="number"
            value={value !== undefined && value !== null ? String(value) : ''}
            onChange={(e) => onChange(e.target.value ? Number(e.target.value) : undefined)}
            placeholder={field.defaultValue !== undefined ? String(field.defaultValue) : undefined}
          />
        </Field>
      )

    case 'boolean': {
      const note = disabledHint ?? help
      return (
        <div className="flex items-start gap-3">
          <Switch
            id={id}
            checked={(value as boolean) ?? (field.defaultValue as boolean) ?? false}
            onCheckedChange={(v) => onChange(v)}
            disabled={disabled}
            aria-labelledby={`${id}-label`}
            aria-describedby={note ? `${id}-note` : undefined}
            className="mt-0.5"
          />
          <div className="flex min-w-0 flex-col">
            <span id={`${id}-label`} className={disabled ? 'text-ui text-ink-muted' : 'text-ui text-ink'}>{label}</span>
            {note && <span id={`${id}-note`} className="text-caption text-ink-muted">{note}</span>}
          </div>
        </div>
      )
    }

    case 'select':
      return (
        <Field label={label} helper={help} required={required}>
          <Select
            id={id}
            value={(value as string) ?? (field.defaultValue as string) ?? ''}
            onChange={(e) => onChange(e.target.value)}
            options={field.options ?? []}
          />
        </Field>
      )

    case 'tags':
      return (
        <Field label={label} helper={help}>
          <Textarea
            id={id}
            value={Array.isArray(value) ? (value as string[]).join('\n') : ''}
            onChange={(e) => {
              const lines = e.target.value.split(/[\n,]/).map((l) => l.trim()).filter(Boolean)
              onChange(lines.length > 0 ? lines : undefined)
            }}
            placeholder={field.placeholder ?? t('form.tagsPlaceholder')}
            rows={3}
            className="font-mono"
          />
        </Field>
      )

    default:
      return null
  }
}
