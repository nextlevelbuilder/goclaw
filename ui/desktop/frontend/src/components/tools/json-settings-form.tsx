import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Textarea } from '../ui'
import { ToolFormFooter } from './tool-form-footer'
import type { BuiltinToolData } from '../../types/builtin-tool'

interface JsonSettingsFormProps {
  tool: BuiltinToolData
  onSave: (name: string, settings: Record<string, unknown>) => Promise<void>
  onClose: () => void
}

/** Raw JSON editor for tools without a dedicated form. */
export function JsonSettingsForm({ tool, onSave, onClose }: JsonSettingsFormProps) {
  const { t } = useTranslation('tools')
  const [value, setValue] = useState('')
  const [error, setError] = useState('')
  const [saveError, setSaveError] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    setValue(JSON.stringify(tool.settings, null, 2))
    setError('')
    setSaveError('')
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
    setSaveError('')
    try {
      await onSave(tool.name, parsed)
      onClose()
    } catch (err) {
      setSaveError((err as Error).message || t('builtin.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <Textarea
        aria-label={tool.display_name}
        aria-invalid={error ? true : undefined}
        value={value}
        onChange={(e) => { setValue(e.target.value); setError('') }}
        spellCheck={false}
        rows={12}
        className="font-mono"
      />
      {error && <p role="alert" className="mt-1 text-caption text-danger">{error}</p>}
      <div className="mt-1 flex justify-end">
        <Button size="sm" variant="ghost" onClick={handleFormat}>{t('builtin.jsonDialog.formatJson')}</Button>
      </div>
      <ToolFormFooter
        cancelLabel={t('builtin.jsonDialog.cancel')}
        saveLabel={saving ? t('builtin.jsonDialog.saving') : t('builtin.jsonDialog.save')}
        onCancel={onClose}
        onSave={() => void handleSave()}
        saving={saving}
        saveDisabled={!!error}
        error={saveError}
      />
    </div>
  )
}
