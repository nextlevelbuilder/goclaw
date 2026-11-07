import { Button, DialogFooter } from '../ui'

interface ToolFormFooterProps {
  cancelLabel: string
  saveLabel: string
  onCancel: () => void
  onSave: () => void
  saving?: boolean
  saveDisabled?: boolean
  /** Failure message from the last save attempt. */
  error?: string
}

/** Error line plus Cancel / Save row shared by the tool settings forms. It sits flush against the dialog edges. */
export function ToolFormFooter({ cancelLabel, saveLabel, onCancel, onSave, saving = false, saveDisabled = false, error }: ToolFormFooterProps) {
  return (
    <div className="-mx-5 -mb-4 mt-4">
      {error && <p role="alert" className="px-5 pb-2 text-caption text-danger">{error}</p>}
      <DialogFooter>
        <Button variant="secondary" onClick={onCancel} disabled={saving}>{cancelLabel}</Button>
        <Button variant="primary" onClick={onSave} loading={saving} disabled={saveDisabled}>{saveLabel}</Button>
      </DialogFooter>
    </div>
  )
}
