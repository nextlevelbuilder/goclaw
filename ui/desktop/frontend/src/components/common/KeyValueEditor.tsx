import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, IconButton, Input } from '../ui'
import { IconClose, IconPlus } from './Icons'

interface KeyValueEditorProps {
  value: Record<string, string>
  onChange: (v: Record<string, string>) => void
  sensitivePattern?: RegExp
  placeholder?: { key?: string; value?: string }
  /** Label of the add button ("Add header"). */
  addLabel: string
  disabled?: boolean
}

interface Row {
  id: number
  key: string
  value: string
}

let nextRowId = 1

function toRows(obj: Record<string, string>): Row[] {
  return Object.entries(obj).map(([key, value]) => ({ id: nextRowId++, key, value }))
}

function fromRows(rows: Row[]): Record<string, string> {
  const result: Record<string, string> = {}
  for (const row of rows) {
    if (row.key.trim()) result[row.key.trim()] = row.value
  }
  return result
}

/** Editable list of key/value pairs; values of sensitive-looking keys are masked. */
export function KeyValueEditor({ value, onChange, sensitivePattern, placeholder, addLabel, disabled = false }: KeyValueEditorProps) {
  const { t } = useTranslation('common')
  const [rows, setRows] = useState<Row[]>(() => toRows(value))

  function updateAndEmit(updated: Row[]) {
    setRows(updated)
    onChange(fromRows(updated))
  }

  const isSensitive = (key: string) => Boolean(sensitivePattern && key && sensitivePattern.test(key))

  return (
    <div className="flex flex-col gap-2">
      {rows.map((row) => (
        <div key={row.id} className="flex items-center gap-2">
          <Input
            value={row.key}
            onChange={(e) => updateAndEmit(rows.map((r) => (r.id === row.id ? { ...r, key: e.target.value } : r)))}
            placeholder={placeholder?.key}
            aria-label={placeholder?.key}
            className="min-w-0 flex-1 font-mono"
            disabled={disabled}
          />
          <Input
            type={isSensitive(row.key) ? 'password' : 'text'}
            value={row.value}
            onChange={(e) => updateAndEmit(rows.map((r) => (r.id === row.id ? { ...r, value: e.target.value } : r)))}
            placeholder={placeholder?.value}
            aria-label={placeholder?.value}
            className="min-w-0 flex-1 font-mono"
            disabled={disabled}
          />
          <IconButton
            aria-label={t('remove')}
            icon={<IconClose size={14} />}
            onClick={() => updateAndEmit(rows.filter((r) => r.id !== row.id))}
            disabled={disabled}
          />
        </div>
      ))}
      <div>
        <Button
          size="sm"
          variant="ghost"
          icon={<IconPlus size={12} />}
          onClick={() => updateAndEmit([...rows, { id: nextRowId++, key: '', value: '' }])}
          disabled={disabled}
        >
          {addLabel}
        </Button>
      </div>
    </div>
  )
}
