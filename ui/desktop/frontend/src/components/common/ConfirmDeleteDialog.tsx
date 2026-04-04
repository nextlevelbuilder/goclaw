import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

interface ConfirmDeleteDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  /** Text the user must type to confirm (e.g. resource name) */
  confirmValue: string
  confirmLabel?: string
  cancelLabel?: string
  onConfirm: () => void
  loading?: boolean
}

export function ConfirmDeleteDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmValue,
  confirmLabel,
  cancelLabel,
  onConfirm,
  loading,
}: ConfirmDeleteDialogProps) {
  const { t } = useTranslation('common')
  const resolvedConfirmLabel = confirmLabel ?? t('delete')
  const resolvedCancelLabel = cancelLabel ?? t('cancel')
  const [inputValue, setInputValue] = useState('')

  useEffect(() => {
    if (!open) setInputValue('')
  }, [open])

  const normalize = (v: string) => v.normalize('NFC').trim().toLocaleLowerCase()
  const isMatch = confirmValue
    ? normalize(inputValue) === normalize(confirmValue)
    : inputValue.trim().length > 0

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="bg-surface-secondary border border-border rounded-xl shadow-xl p-5 max-w-sm w-full mx-4 space-y-4">
        <div className="space-y-1.5">
          <h3 className="text-sm font-semibold text-text-primary">{title}</h3>
          <p className="text-xs text-text-muted">{description}</p>
        </div>
        <div>
          <p className="mb-2 text-xs text-text-muted">
            {t('typeToConfirmPrefix')} <span className="font-semibold text-text-primary">{confirmValue}</span> {t('typeToConfirmSuffix')}
          </p>
          <input
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            placeholder={confirmValue}
            autoFocus
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
        <div className="flex justify-end gap-2">
          <button
            onClick={() => onOpenChange(false)}
            disabled={loading}
            className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors disabled:opacity-50"
          >
            {resolvedCancelLabel}
          </button>
          <button
            onClick={onConfirm}
            disabled={!isMatch || loading}
            className="px-3 py-1.5 text-xs bg-error text-white rounded-lg transition-opacity disabled:opacity-50 hover:opacity-90"
          >
            {loading ? '...' : resolvedConfirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
