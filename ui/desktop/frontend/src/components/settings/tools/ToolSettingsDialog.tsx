import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Switch } from '../../common/Switch'
import { MediaProviderChainForm } from './MediaProviderChainForm'
import type { BuiltinToolData } from '../../../types/builtin-tool'

const MEDIA_TOOLS = new Set([
  'read_image', 'read_document', 'read_audio', 'read_video',
  'create_image', 'create_video', 'create_audio',
])

interface ToolSettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  tool: BuiltinToolData
  onSave: (name: string, settings: Record<string, unknown>) => Promise<void>
}

export function ToolSettingsDialog({ open, onOpenChange, tool, onSave }: ToolSettingsDialogProps) {
  const { t } = useTranslation(['tools', 'common'])
  if (!open) return null

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={() => onOpenChange(false)} />
      <div className="relative w-full max-w-2xl mx-4 bg-surface-secondary rounded-xl border border-border overflow-hidden flex flex-col" style={{ maxHeight: '85vh' }}>
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <div>
            <h3 className="text-sm font-semibold text-text-primary">{t('builtin.settingsDialog.title', { name: tool.display_name })}</h3>
            <p className="font-mono text-[11px] text-text-muted mt-0.5">{tool.name}</p>
          </div>
          <button onClick={() => onOpenChange(false)} className="p-1 text-text-muted hover:text-text-primary transition-colors">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>

        {/* Content — route to specialized form or generic JSON */}
        {tool.name === 'web_fetch' ? (
          <ExtractorChainForm tool={tool} onSave={onSave} onClose={() => onOpenChange(false)} />
        ) : MEDIA_TOOLS.has(tool.name) ? (
          <MediaProviderChainForm tool={tool} onSave={onSave} onClose={() => onOpenChange(false)} />
        ) : (
          <JsonSettingsForm tool={tool} onSave={onSave} onClose={() => onOpenChange(false)} />
        )}
      </div>
    </div>
  )
}

/* ─── Extractor Chain Form (web_fetch) ─── */

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

function ExtractorChainForm({ tool, onSave, onClose }: { tool: BuiltinToolData; onSave: ToolSettingsDialogProps['onSave']; onClose: () => void }) {
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
            {/* Header row */}
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <span className="text-[11px] font-mono text-text-muted bg-surface-tertiary rounded px-1.5 py-0.5">#{i + 1}</span>
                <span className="text-sm font-medium text-text-primary">{EXTRACTOR_DISPLAY[ext.name] ?? ext.name}</span>
              </div>
              <Switch checked={ext.enabled} onCheckedChange={(v) => updateExtractor(i, { enabled: v })} />
            </div>

            {/* Defuddle-specific fields */}
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
                    type="number"
                    min={0}
                    max={10}
                    value={ext.max_retries ?? 2}
                    onChange={(e) => updateExtractor(i, { max_retries: Math.max(0, Number(e.target.value)) })}
                    className="w-20 bg-surface-tertiary border border-border rounded-lg px-3 py-1.5 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
                  />
                </div>
              </>
            )}

            {/* Timeout (show for defuddle or if already set) */}
            {(ext.name === 'defuddle' || (ext.timeout && ext.timeout > 0)) && (
              <div className="space-y-1">
                <label className="text-xs font-medium text-text-secondary">{t('builtin.extractorChain.timeout')}</label>
                <input
                  type="number"
                  min={0}
                  max={600}
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

      {/* Footer */}
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

/* ─── Generic JSON Settings Form (fallback) ─── */

function JsonSettingsForm({ tool, onSave, onClose }: { tool: BuiltinToolData; onSave: ToolSettingsDialogProps['onSave']; onClose: () => void }) {
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
