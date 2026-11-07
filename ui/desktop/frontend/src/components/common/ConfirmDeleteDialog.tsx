import { useTranslation } from 'react-i18next'
import { ConfirmDialog, type ConfirmDialogProps } from '../ui/ConfirmDialog'

// Kept so existing imports keep working; new code uses ConfirmDialog from
// components/ui with variant="destructive" and confirmValue.

interface ConfirmDeleteDialogProps extends Omit<ConfirmDialogProps, 'variant' | 'confirmValue'> {
  /** Text the user must type to confirm (e.g. resource name). */
  confirmValue: string
}

export function ConfirmDeleteDialog({ confirmLabel, ...props }: ConfirmDeleteDialogProps) {
  const { t } = useTranslation('common')
  return <ConfirmDialog variant="destructive" confirmLabel={confirmLabel ?? t('delete')} {...props} />
}
