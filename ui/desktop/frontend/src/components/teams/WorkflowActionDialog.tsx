import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ConfirmDialog } from '../common/ConfirmDialog'
import type { WorkflowAction } from '../../types/team'

const MAX_REASON_LENGTH = 10_000

interface WorkflowActionDialogProps {
  action: WorkflowAction | null
  loading: boolean
  onClose: () => void
  onConfirm: (reason: string) => Promise<void>
}

export function WorkflowActionDialog({ action, loading, onClose, onConfirm }: WorkflowActionDialogProps) {
  const { t } = useTranslation('teams')
  const [reason, setReason] = useState('')
  const [confirming, setConfirming] = useState(false)

  useEffect(() => {
    setReason('')
    setConfirming(false)
  }, [action])

  if (!action) return null

  const trimmedReason = reason.trim()
  const destructive = action === 'cancel_workflow' || action === 'fail_workflow'

  const handleConfirm = async () => {
    await onConfirm(trimmedReason)
    setConfirming(false)
  }

  return (
    <>
      {!confirming && (
        <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/50" onClick={loading ? undefined : onClose}>
          <div onClick={(event) => event.stopPropagation()} className="bg-surface-primary border border-border rounded-xl shadow-xl max-w-lg w-[95vw] mx-4 p-5 space-y-4">
            <div className="space-y-1.5">
              <h3 className="text-sm font-semibold text-text-primary">{t(`workflow.actions.${action}.label`)}</h3>
              <p className="text-xs text-text-muted">{t(`workflow.actions.${action}.description`)}</p>
            </div>

            <div className="space-y-1.5">
              <label htmlFor="workflow-action-reason" className="text-xs font-medium text-text-secondary">
                {t('workflow.reason.label')}
              </label>
              <textarea
                id="workflow-action-reason"
                value={reason}
                maxLength={MAX_REASON_LENGTH}
                disabled={loading}
                onChange={(event) => setReason(event.target.value)}
                placeholder={t('workflow.reason.placeholder')}
                className="min-h-28 w-full resize-y rounded-lg border border-border bg-surface-secondary px-3 py-2 text-sm text-text-primary outline-none placeholder:text-text-muted focus:border-accent disabled:opacity-50"
              />
              <div className="flex justify-between text-[11px] text-text-muted">
                <span>{trimmedReason ? '' : t('workflow.reason.required')}</span>
                <span>{reason.length}/{MAX_REASON_LENGTH}</span>
              </div>
            </div>

            <div className="flex justify-end gap-2">
              <button
                type="button"
                disabled={loading}
                onClick={onClose}
                className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors disabled:opacity-50"
              >
                {t('workflow.actions.cancel')}
              </button>
              <button
                type="button"
                disabled={loading || !trimmedReason}
                onClick={() => setConfirming(true)}
                className={`px-3 py-1.5 text-xs text-white rounded-lg transition-opacity disabled:opacity-50 ${
                  destructive ? 'bg-error hover:opacity-90' : 'bg-accent hover:bg-accent-hover'
                }`}
              >
                {t('workflow.actions.confirm')}
              </button>
            </div>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t(`workflow.actions.${action}.label`)}
        description={t(`workflow.actions.${action}.confirm`)}
        confirmLabel={t('workflow.actions.confirm', 'Confirm')}
        cancelLabel={t('workflow.actions.cancel', 'Cancel')}
        variant={destructive ? 'destructive' : 'default'}
        loading={loading}
        onConfirm={handleConfirm}
      />
    </>
  )
}
