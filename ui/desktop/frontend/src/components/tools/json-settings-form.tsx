import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import type { BuiltinToolData } from '../../types/builtin-tool'

interface JsonSettingsFormProps {
  tool: BuiltinToolData
  onSave: (name: string, settings: Record<string, unknown>) => Promise<void>
  onClose: () => void
}

export function JsonSettingsForm({ tool, onSave, onClose }: JsonSettingsFormProps) {
  const { t } = useTranslation(['tools', 'common'])
  const [value, setValue] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    setValue(JSON.stringify(tool.settings, null, 2))
    setError('')
    setSaving(false)
  }, [tool])

  function validate(json: string): Record<string, unknown> | null {
    try {
      const parsed = JSON.parse(json)
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
        setError(t('builtin.jsonDialog.invalidJson'))
        return null
      }
      setError('')
      return parsed as Record<string, unknown>
    } catch {
      setError(t('builtin.jsonDialog.invalidJson'))
      return null
    }
  }

  function handleFormat() {
    const parsed = validate(value)
    if (parsed) setValue(JSON.stringify(parsed, null, 2))
  }

  async function handleSave() {
    const parsed = validate(value)
    if (!parsed) return
    setSaving(true)
    try {
      await onSave(tool.name, parsed)
      onClose()
    } catch (err) {
      setError((err as Error).message || t('builtin.jsonDialog.invalidJson'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <div className="p-5">
        <textarea
          value={value}
          onChange={(e) => { setValue(e.target.value); setError('') }}
          spellCheck={false}
          className="w-full h-64 bg-surface-tertiary border border-border rounded-lg px-3 py-2 font-mono text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent resize-y"
        />
        <div className="flex items-center justify-between mt-1 min-h-[20px]">
          {error ? <span className="text-xs text-error">{error}</span> : <span />}
          <button type="button" onClick={handleFormat} className="text-[11px] text-accent hover:text-accent-hover transition-colors">
            {t('builtin.jsonDialog.formatJson')}
          </button>
        </div>
      </div>

      <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-4">
        <button type="button" onClick={onClose} className="border border-border rounded-lg px-4 py-1.5 text-sm text-text-secondary hover:bg-surface-tertiary transition-colors">
          {t('builtin.jsonDialog.cancel')}
        </button>
        <button type="button" onClick={handleSave} disabled={!!error || saving} className="bg-accent rounded-lg px-4 py-1.5 text-sm text-white hover:bg-accent-hover disabled:opacity-50 transition-colors">
          {saving ? t('builtin.jsonDialog.saving') : t('builtin.jsonDialog.save')}
        </button>
      </div>
    </>
  )
}
