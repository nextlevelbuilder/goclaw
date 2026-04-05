import { useState } from 'react'

interface KeyValueEditorProps {
  value: Record<string, string>
  onChange: (v: Record<string, string>) => void
  sensitivePattern?: RegExp
  placeholder?: { key?: string; value?: string }
}

interface Row {
  id: number
  key: string
  value: string
}

let nextRowId = 1

function toRows(obj: Record<string, string>): Row[] {
  const entries = Object.entries(obj)
  if (entries.length === 0) return []
  return entries.map(([key, value]) => ({ id: nextRowId++, key, value }))
}

function fromRows(rows: Row[]): Record<string, string> {
  const result: Record<string, string> = {}
  for (const row of rows) {
    if (row.key.trim()) result[row.key.trim()] = row.value
  }
  return result
}

export function KeyValueEditor({ value, onChange, sensitivePattern, placeholder }: KeyValueEditorProps) {
  const [rows, setRows] = useState<Row[]>(() => toRows(value))

  function updateAndEmit(updated: Row[]) {
    setRows(updated)
    onChange(fromRows(updated))
  }

  function addRow() {
    updateAndEmit([...rows, { id: nextRowId++, key: '', value: '' }])
  }

  function removeRow(id: number) {
    updateAndEmit(rows.filter((r) => r.id !== id))
  }

  function updateRow(id: number, field: 'key' | 'value', val: string) {
    updateAndEmit(rows.map((r) => r.id === id ? { ...r, [field]: val } : r))
  }

  function isSensitive(key: string): boolean {
    if (!sensitivePattern || !key) return false
    return sensitivePattern.test(key)
  }

  return (
    <div className="space-y-2">
      {rows.map((row) => (
        <div key={row.id} className="flex items-center gap-2">
          <input
            value={row.key}
            onChange={(e) => updateRow(row.id, 'key', e.target.value)}
            placeholder={placeholder?.key ?? 'Key'}
            className="flex-1 min-w-0 bg-surface-tertiary border border-border rounded-lg px-2.5 py-1.5 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
          <input
            type={isSensitive(row.key) ? 'password' : 'text'}
            value={row.value}
            onChange={(e) => updateRow(row.id, 'value', e.target.value)}
            placeholder={placeholder?.value ?? 'Value'}
            className="flex-1 min-w-0 bg-surface-tertiary border border-border rounded-lg px-2.5 py-1.5 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
          <button
            type="button"
            onClick={() => removeRow(row.id)}
            className="shrink-0 p-1 text-text-muted hover:text-error transition-colors"
          >
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>
      ))}
      <button
        type="button"
        onClick={addRow}
        className="text-xs text-accent hover:text-accent-hover flex items-center gap-1 transition-colors"
      >
        <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M5 12h14" /><path d="M12 5v14" />
        </svg>
        Add row
      </button>
    </div>
  )
}
