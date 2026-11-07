import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge, Field, Input, Switch } from '../ui'
import { ToolFormFooter } from './tool-form-footer'
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

/** Ordered content extractors for web_fetch: enable each and tune its endpoint, retries and timeout. */
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
      setError((err as Error).message || t('tools:builtin.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <div className="flex flex-col gap-3">
        {extractors.map((ext, i) => {
          const title = EXTRACTOR_DISPLAY[ext.name] ?? ext.name
          return (
            <section key={ext.name} aria-label={title} className="flex flex-col gap-3 rounded-md border border-border-subtle p-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Badge tone="neutral">#{i + 1}</Badge>
                  <span id={`extractor-${ext.name}`} className="text-ui font-medium text-ink">{title}</span>
                </div>
                <Switch
                  checked={ext.enabled}
                  onCheckedChange={(v) => updateExtractor(i, { enabled: v })}
                  aria-labelledby={`extractor-${ext.name}`}
                />
              </div>

              {ext.name === 'defuddle' && (
                <>
                  <Field label={t('tools:builtin.extractorChain.baseUrl')}>
                    <Input
                      value={ext.base_url ?? ''}
                      onChange={(e) => updateExtractor(i, { base_url: e.target.value })}
                      placeholder="https://fetch.goclaw.sh/"
                      className="font-mono"
                    />
                  </Field>
                  <Field label={t('tools:builtin.mediaChain.retries')}>
                    <Input
                      type="number" min={0} max={10}
                      value={ext.max_retries ?? 2}
                      onChange={(e) => updateExtractor(i, { max_retries: Math.max(0, Number(e.target.value)) })}
                      className="w-24"
                    />
                  </Field>
                </>
              )}

              {(ext.name === 'defuddle' || (ext.timeout && ext.timeout > 0)) && (
                <Field label={t('tools:builtin.extractorChain.timeout')} helper={`0 = ${t('common:default')}`}>
                  <Input
                    type="number" min={0} max={600}
                    value={ext.timeout ?? 0}
                    onChange={(e) => updateExtractor(i, { timeout: Math.max(0, Number(e.target.value)) })}
                    className="w-24"
                  />
                </Field>
              )}
            </section>
          )
        })}
        {extractors.length === 0 && (
          <p className="py-4 text-center text-caption text-ink-muted">{t('tools:builtin.mediaChain.noProviders')}</p>
        )}
      </div>

      <ToolFormFooter
        cancelLabel={t('tools:builtin.settingsDialog.cancel')}
        saveLabel={saving ? t('tools:builtin.settingsDialog.saving') : t('tools:builtin.settingsDialog.save')}
        onCancel={onClose}
        onSave={() => void handleSave()}
        saving={saving}
        error={error}
      />
    </div>
  )
}
