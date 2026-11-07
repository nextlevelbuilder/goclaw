import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { IconDocument, IconDownload } from '../common/Icons'
import { cn } from '../../lib/cn'
import { Button, Dialog, DialogFooter, toast } from '../ui'
import type { UploadResult } from '../../hooks/use-skills'

const MAX_SIZE = 20 * 1024 * 1024

interface SkillAddDialogProps {
  onClose: () => void
  onUpload: (file: File) => Promise<UploadResult>
}

/** Add a skill from a .zip with SKILL.md at its root (20 MB limit). */
export function SkillAddDialog({ onClose, onUpload }: SkillAddDialogProps) {
  const { t } = useTranslation('skills')
  const inputRef = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [error, setError] = useState('')
  const [uploading, setUploading] = useState(false)
  const [dragging, setDragging] = useState(false)

  const pick = (f: File | undefined) => {
    if (!f) return
    if (!f.name.toLowerCase().endsWith('.zip')) { setFile(null); setError(t('upload.onlyZip')); return }
    if (f.size > MAX_SIZE) { setFile(null); setError(t('upload.tooLarge')); return }
    setError('')
    setFile(f)
  }

  const submit = async () => {
    if (!file) return
    setUploading(true)
    setError('')
    try {
      const res = await onUpload(file)
      toast.success(t('toast.uploaded'), res.name)
      if (res.deps_warning) toast.info(res.deps_warning)
      onClose()
    } catch (err) {
      setError((err as Error).message || t('upload.failed'))
      toast.error(t('toast.uploadFailed'))
    } finally {
      setUploading(false)
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(o) => { if (!o) onClose() }}
      title={t('screen.addTitle')}
      description={t('upload.description')}
      size="md"
      dismissible={!uploading}
      footer={(
        <DialogFooter>
          <Button variant="secondary" onClick={onClose} disabled={uploading}>{t('upload.cancel')}</Button>
          <Button variant="primary" onClick={() => void submit()} loading={uploading} disabled={!file}>
            {t('screen.addSubmit')}
          </Button>
        </DialogFooter>
      )}
    >
      <div className="flex flex-col gap-3">
        <input
          ref={inputRef}
          type="file"
          accept=".zip"
          className="sr-only"
          tabIndex={-1}
          aria-label={t('screen.chooseFile')}
          onChange={(e) => { pick(e.target.files?.[0]); e.target.value = '' }}
        />
        <button
          type="button"
          onClick={() => inputRef.current?.click()}
          onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
          onDragLeave={() => setDragging(false)}
          onDrop={(e) => { e.preventDefault(); setDragging(false); pick(e.dataTransfer.files?.[0]) }}
          className={cn(
            'flex min-h-28 w-full cursor-pointer flex-col items-center justify-center gap-2 rounded-md border border-dashed px-4 py-6 text-center transition-colors',
            'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus-ring',
            dragging ? 'border-accent bg-accent-subtle' : 'border-border-control hover:bg-surface-hover',
          )}
        >
          {file ? <IconDocument size={20} className="text-ink-secondary" /> : <IconDownload size={20} className="text-ink-muted" />}
          {file ? (
            <>
              <span className="text-caption text-ink-muted">{t('screen.sourceFile')}</span>
              <span className="max-w-full truncate text-ui font-medium text-ink">{file.name}</span>
              <span className="text-caption text-ink-secondary">{t('screen.chooseAnother')}</span>
            </>
          ) : (
            <span className="text-ui text-ink-secondary">{dragging ? t('upload.dropHere') : t('upload.dropOrClick')}</span>
          )}
        </button>
        {error && <p role="alert" className="text-caption text-danger">{error}</p>}
      </div>
    </Dialog>
  )
}
