import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from './Button'
import { Dialog, DialogFooter } from './Dialog'
import { Field } from './Field'
import { Input } from './Input'

export interface ConfirmDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Names the object, e.g. "Delete agent research-assistant?" */
  title: string
  description: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  variant?: 'default' | 'destructive'
  onConfirm: () => void
  loading?: boolean
  /**
   * Typed confirmation: the confirm button stays disabled until the user types
   * this value (case-insensitive). An empty string accepts any non-empty input.
   */
  confirmValue?: string
}

const normalize = (v: string) => v.normalize('NFC').trim().toLocaleLowerCase()

/** Confirms an action; for destructive ones it names the object and can require typing it. */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  cancelLabel,
  variant = 'default',
  onConfirm,
  loading = false,
  confirmValue,
}: ConfirmDialogProps) {
  const { t } = useTranslation('common')
  const [typed, setTyped] = useState('')

  useEffect(() => {
    if (!open) setTyped('')
  }, [open])

  const typedOk = confirmValue === undefined
    || (confirmValue ? normalize(typed) === normalize(confirmValue) : typed.trim().length > 0)
  const destructive = variant === 'destructive'

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      size="sm"
      dismissible={!loading}
      footer={(
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)} disabled={loading}>
            {cancelLabel ?? t('cancel')}
          </Button>
          <Button
            variant={destructive ? 'danger' : 'primary'}
            onClick={onConfirm}
            loading={loading}
            disabled={!typedOk}
          >
            {confirmLabel ?? t('confirm')}
          </Button>
        </DialogFooter>
      )}
    >
      {confirmValue !== undefined ? (
        <Field
          label={(
            <>
              {t('typeToConfirmPrefix')} <span className="font-semibold">{confirmValue}</span> {t('typeToConfirmSuffix')}
            </>
          )}
        >
          <Input
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder={confirmValue}
            autoComplete="off"
            autoFocus
            onKeyDown={(e) => { if (e.key === 'Enter' && typedOk && !loading) onConfirm() }}
          />
        </Field>
      ) : undefined}
    </Dialog>
  )
}
