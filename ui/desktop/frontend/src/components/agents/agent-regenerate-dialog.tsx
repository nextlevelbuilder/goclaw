// Dialog for "Edit with AI" — sends prompt to regenerate agent context files endpoint.

import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Dialog, DialogFooter, Field, Textarea } from '../ui'

interface RegenerateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onRegenerate: (prompt: string) => Promise<void>
}

export function RegenerateDialog({ open, onOpenChange, onRegenerate }: RegenerateDialogProps) {
  const { t } = useTranslation(['agents', 'common'])
  const [prompt, setPrompt] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async () => {
    if (!prompt.trim()) return
    setLoading(true)
    try {
      await onRegenerate(prompt.trim())
      onOpenChange(false)
      setPrompt('')
    } catch {
      // The caller reports the failure; keep the dialog open so the text isn't lost.
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('agents:screen.files.regenerateTitle')}
      description={t('agents:screen.files.regenerateDescription')}
      dismissible={!loading}
      footer={
        <DialogFooter>
          <Button onClick={() => onOpenChange(false)} disabled={loading}>{t('common:cancel')}</Button>
          <Button variant="primary" onClick={handleSubmit} disabled={!prompt.trim()} loading={loading}>
            {t('agents:screen.files.regenerateSubmit')}
          </Button>
        </DialogFooter>
      }
    >
      <Field label={t('agents:screen.files.regenerateLabel')}>
        <Textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder={t('agents:screen.files.regeneratePlaceholder')}
          rows={5}
          autoFocus
        />
      </Field>
    </Dialog>
  )
}
