import { useTranslation } from 'react-i18next'
import { Dialog } from '../ui'
import { MediaProviderChainForm } from './MediaProviderChainForm'
import { ExtractorChainForm } from './extractor-chain-form'
import { JsonSettingsForm } from './json-settings-form'
import { SttProviderForm } from '../builtin-tools/stt-provider-form'
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

/** Settings of one built-in tool; routes to the form that matches the tool. Each form brings its own Cancel / Save row. */
export function ToolSettingsDialog({ open, onOpenChange, tool, onSave }: ToolSettingsDialogProps) {
  const { t } = useTranslation('tools')
  const handleClose = () => onOpenChange(false)

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('builtin.settingsDialog.title', { name: tool.display_name })}
      description={<span className="font-mono">{tool.name}</span>}
      size="lg"
    >
      {tool.name === 'web_fetch' ? (
        <ExtractorChainForm tool={tool} onSave={onSave} onClose={handleClose} />
      ) : MEDIA_TOOLS.has(tool.name) ? (
        <MediaProviderChainForm tool={tool} onSave={onSave} onClose={handleClose} />
      ) : tool.name === 'stt' ? (
        <SttProviderForm
          initialSettings={(tool.settings ?? {}) as Record<string, unknown>}
          onSave={(settings) => onSave(tool.name, settings)}
          onCancel={handleClose}
        />
      ) : (
        <JsonSettingsForm tool={tool} onSave={onSave} onClose={handleClose} />
      )}
    </Dialog>
  )
}
