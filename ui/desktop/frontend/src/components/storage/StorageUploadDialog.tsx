// Upload dialog with drag-drop zone, blocked extension validation, and multi-file support.

import { useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { IconDownload } from '../common/Icons'
import { Button, Dialog, DialogFooter } from '../ui'
import { UploadProgressList, type FileEntry, type FileStatus } from './upload-progress-list'

const BLOCKED_EXTENSIONS = new Set([
  '.exe', '.sh', '.bat', '.cmd', '.ps1', '.com', '.msi', '.scr',
])
const MAX_FILE_SIZE = 50 * 1024 * 1024 // 50MB

let idCounter = 0
function uniqueId(): string {
  return `upload-${++idCounter}-${Date.now()}`
}

interface StorageUploadDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onUpload: (file: File) => Promise<void>
  title?: string
  description?: string
}

export function StorageUploadDialog({
  open, onOpenChange, onUpload, title, description,
}: StorageUploadDialogProps) {
  const { t } = useTranslation('common')
  const [entries, setEntries] = useState<FileEntry[]>([])
  const [uploading, setUploading] = useState(false)
  const [done, setDone] = useState(false)
  const [dragging, setDragging] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const addFiles = (fileList: FileList) => {
    const existingNames = new Set(entries.map((e) => e.file.name))
    const fresh = Array.from(fileList).filter((f) => !existingNames.has(f.name))
    if (fresh.length === 0) return

    const newEntries: FileEntry[] = fresh.map((f) => {
      const ext = '.' + f.name.split('.').pop()?.toLowerCase()
      if (BLOCKED_EXTENSIONS.has(ext)) {
        return { id: uniqueId(), file: f, status: 'error' as FileStatus, error: t('upload.blockedType', { ext }) }
      }
      if (f.size > MAX_FILE_SIZE) {
        return { id: uniqueId(), file: f, status: 'error' as FileStatus, error: t('upload.tooLarge') }
      }
      return { id: uniqueId(), file: f, status: 'ready' as FileStatus }
    })
    setEntries((prev) => [...prev, ...newEntries])
  }

  const removeEntry = (id: string) => {
    setEntries((prev) => prev.filter((e) => e.id !== id))
  }

  const handleSubmit = async () => {
    const readyEntries = entries.filter((e) => e.status === 'ready')
    if (readyEntries.length === 0) return
    setUploading(true)

    for (const entry of readyEntries) {
      setEntries((prev) => prev.map((e) => (e.id === entry.id ? { ...e, status: 'uploading' as FileStatus } : e)))
      try {
        await onUpload(entry.file)
        setEntries((prev) => prev.map((e) => (e.id === entry.id ? { ...e, status: 'success' as FileStatus } : e)))
      } catch (err) {
        setEntries((prev) =>
          prev.map((e) =>
            e.id === entry.id
              ? { ...e, status: 'error' as FileStatus, error: err instanceof Error ? err.message : t('upload.failed') }
              : e,
          ),
        )
      }
    }
    setUploading(false)
    setDone(true)
  }

  const handleClose = (v: boolean) => {
    if (uploading) return
    setEntries([])
    setDragging(false)
    setDone(false)
    onOpenChange(v)
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setDragging(false)
    if (e.dataTransfer.files.length > 0) addFiles(e.dataTransfer.files)
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) addFiles(e.target.files)
    if (inputRef.current) inputRef.current.value = ''
  }

  const readyCount = entries.filter((e) => e.status === 'ready').length
  const successCount = entries.filter((e) => e.status === 'success').length

  return (
    <Dialog
      open={open}
      onOpenChange={handleClose}
      size="md"
      title={title ?? t('upload.title')}
      description={description}
      dismissible={!uploading}
      footer={(
        <DialogFooter>
          <Button variant="secondary" onClick={() => handleClose(false)} disabled={uploading}>{t('cancel')}</Button>
          {done ? (
            <Button variant="primary" onClick={() => handleClose(false)}>{t('done', 'Done')}</Button>
          ) : (
            <Button variant="primary" onClick={handleSubmit} disabled={readyCount === 0} loading={uploading}>
              {uploading ? t('upload.uploading') : t('upload.uploadCount', { count: readyCount })}
            </Button>
          )}
        </DialogFooter>
      )}
    >
      <div className="flex flex-col gap-4">
        {!uploading && !done && (
          <div
            role="button"
            tabIndex={0}
            aria-label={t('upload.dropOrClick')}
            className={cn(
              'flex cursor-pointer flex-col items-center gap-2 rounded-lg border-2 border-dashed p-6 text-center transition-colors',
              dragging ? 'border-accent bg-accent-subtle' : 'border-border-control hover:border-accent',
            )}
            onClick={() => inputRef.current?.click()}
            onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); inputRef.current?.click() } }}
            onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
            onDragEnter={(e) => { e.preventDefault(); setDragging(true) }}
            onDragLeave={() => setDragging(false)}
            onDrop={handleDrop}
          >
            <IconDownload size={24} className="rotate-180 text-ink-muted" />
            <p className="text-ui text-ink-secondary">{dragging ? t('upload.dropHere') : t('upload.dropOrClick')}</p>
            <p className="text-caption text-ink-muted">{t('upload.maxSize')}</p>
            <input ref={inputRef} type="file" multiple className="hidden" onChange={handleInputChange} />
          </div>
        )}

        <UploadProgressList entries={entries} uploading={uploading} onRemove={removeEntry} />

        {entries.length > 0 && !done && !uploading && (
          <p className="text-caption text-ink-muted">{t('upload.readyCount', { ready: readyCount, total: entries.length })}</p>
        )}
        {done && (
          <p role="status" className="text-caption text-ink-secondary">{t('upload.successCount', { success: successCount, total: entries.length })}</p>
        )}
      </div>
    </Dialog>
  )
}
