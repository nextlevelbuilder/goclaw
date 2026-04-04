import { useTranslation } from 'react-i18next'
import { MediaProviderChainForm } from './MediaProviderChainForm'
import { ExtractorChainForm } from './extractor-chain-form'
import { JsonSettingsForm } from './json-settings-form'
import type { BuiltinToolData } from '../../types/builtin-tool'

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

  const handleClose = () => onOpenChange(false)

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={handleClose} />
      <div className="relative w-full max-w-2xl mx-4 bg-surface-secondary rounded-xl border border-border overflow-hidden flex flex-col" style={{ maxHeight: '85vh' }}>
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <div>
            <h3 className="text-sm font-semibold text-text-primary">{t('builtin.settingsDialog.title', { name: tool.display_name })}</h3>
            <p className="font-mono text-[11px] text-text-muted mt-0.5">{tool.name}</p>
          </div>
          <button onClick={handleClose} className="p-1 text-text-muted hover:text-text-primary transition-colors">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>

        {/* Route to specialized form */}
        {tool.name === 'web_fetch' ? (
          <ExtractorChainForm tool={tool} onSave={onSave} onClose={handleClose} />
        ) : MEDIA_TOOLS.has(tool.name) ? (
          <MediaProviderChainForm tool={tool} onSave={onSave} onClose={handleClose} />
        ) : (
          <JsonSettingsForm tool={tool} onSave={onSave} onClose={handleClose} />
        )}
      </div>
    </div>
  )
}
