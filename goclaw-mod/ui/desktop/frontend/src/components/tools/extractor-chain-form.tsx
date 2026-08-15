import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Switch } from '../common/Switch'
import type { BuiltinToolData } from '../../types/builtin-tool'

interface ExtractorEntry {
  name: string
  enabled: boolean
  base_url?: string
  timeout?: number
  max_retries?: number
}

const EXTRACTOR_DISPLAY: Record<string, string> = {
  defuddle: 'Defuddle',
  'html-to-markdown': 'HTML to Markdown',
}

interface ExtractorChainFormProps {
  tool: BuiltinToolData
  onSave: (name: string, settings: Record<string, unknown>) => Promise<void>
  onClose: () => void
}

export function ExtractorChainForm({ tool, onSave, onClose }: ExtractorChainFormProps) {
  const { t } = useTranslation(['tools', 'common'])
  const [extractors, setExtractors] = useState<ExtractorEntry[]>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    const settings = tool.settings as { extractors?: ExtractorEntry[] }
    setExtractors(settings?.extractors ?? [])
    setError('')
    setSaving(false)
  }, [tool])

  function updateExtractor(index: number, updates: Partial<ExtractorEntry>) {
    setExtractors((prev) => prev.map((e, i) => i === index ? { ...e, ...updates } : e))
  }

  async function handleSave() {
    setSaving(true)
    setError('')
    try {
      await onSave(tool.name, { extractors })
      onClose()
    } catch (err) {
      setError((err as Error).message || 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <div className="max-h-[60vh] overflow-y-auto p-5 space-y-3">
        {extractors.map((ext, i) => (
          <div key={ext.name} className="rounded-lg border border-border p-3 space-y-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <span className="text-[11px] font-mono text-text-muted bg-surface-tertiary rounded px-1.5 py-0.5">#{i + 1}</span>
                <span className="text-sm font-medium text-text-primary">{EXTRACTOR_DISPLAY[ext.name] ?? ext.name}</span>
              </div>
              <Switch checked={ext.enabled} onCheckedChange={(v) => updateExtractor(i, { enabled: v })} />
            </div>

            {ext.name === 'defuddle' && (
              <>
                <div className="space-y-1">
                  <label className="text-xs font-medium text-text-secondary">{t('builtin.extractorChain.baseUrl')}</label>
                  <input
                    value={ext.base_url ?? ''}
                    onChange={(e) => updateExtractor(i, { base_url: e.target.value })}
                    placeholder="https://fetch.goclaw.sh/"
                    className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-1.5 font-mono text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs font-medium text-text-secondary">{t('builtin.mediaChain.retries')}</label>
                  <input
                    type="number" min={0} max={10}
                    value={ext.max_retries ?? 2}
                    onChange={(e) => updateExtractor(i, { max_retries: Math.max(0, Number(e.target.value)) })}
                    className="w-20 bg-surface-tertiary border border-border rounded-lg px-3 py-1.5 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
                  />
                </div>
              </>
            )}

            {(ext.name === 'defuddle' || (ext.timeout && ext.timeout > 0)) && (
              <div className="space-y-1">
                <label className="text-xs font-medium text-text-secondary">{t('builtin.extractorChain.timeout')}</label>
                <input
                  type="number" min={0} max={600}
                  value={ext.timeout ?? 0}
                  onChange={(e) => updateExtractor(i, { timeout: Math.max(0, Number(e.target.value)) })}
                  className="w-20 bg-surface-tertiary border border-border rounded-lg px-3 py-1.5 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
                />
                <p className="text-[10px] text-text-muted">0 = {t('common:default')}</p>
              </div>
            )}
          </div>
        ))}
        {extractors.length === 0 && (
          <p className="text-xs text-text-muted text-center py-4">{t('builtin.mediaChain.noProviders')}</p>
        )}
      </div>

      {error && <div className="px-5"><p className="text-xs text-error">{error}</p></div>}
      <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-4">
        <button type="button" onClick={onClose} className="border border-border rounded-lg px-4 py-1.5 text-sm text-text-secondary hover:bg-surface-tertiary transition-colors">
          {t('builtin.settingsDialog.cancel')}
        </button>
        <button type="button" onClick={handleSave} disabled={saving} className="bg-accent rounded-lg px-4 py-1.5 text-sm text-white hover:bg-accent-hover disabled:opacity-50 transition-colors">
          {saving ? t('builtin.settingsDialog.saving') : t('builtin.settingsDialog.save')}
        </button>
      </div>
    </>
  )
}
