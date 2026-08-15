// Upload dialog with drag-drop zone, blocked extension validation, and multi-file support.
// Adapted from ui/web file-upload-dialog.tsx for desktop styling (no Radix).

import { useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'
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

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="bg-surface-secondary border border-border rounded-xl shadow-xl p-5 max-w-md w-full mx-4 space-y-4 max-h-[80vh] flex flex-col">
        {/* Header */}
        <div className="space-y-1.5 shrink-0">
          <h3 className="text-sm font-semibold text-text-primary">{title ?? t('upload.title')}</h3>
          {description && <p className="text-xs text-text-muted">{description}</p>}
        </div>

        {/* Drop zone */}
        {!uploading && !done && (
          <div
            role="button"
            tabIndex={0}
            className={`flex cursor-pointer flex-col items-center gap-2 rounded-lg border-2 border-dashed p-6 text-center transition-colors ${
              dragging ? 'border-accent bg-accent/5' : 'border-border hover:border-accent/50'
            }`}
            onClick={() => inputRef.current?.click()}
            onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); inputRef.current?.click() } }}
            onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
            onDragEnter={(e) => { e.preventDefault(); setDragging(true) }}
            onDragLeave={() => setDragging(false)}
            onDrop={handleDrop}
          >
            <svg className="h-8 w-8 text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="17 8 12 3 7 8" />
              <line x1="12" y1="3" x2="12" y2="15" />
            </svg>
            <p className="text-xs text-text-muted">
              {dragging ? t('upload.dropHere') : t('upload.dropOrClick')}
            </p>
            <p className="text-[10px] text-text-muted/60">{t('upload.maxSize')}</p>
            <input ref={inputRef} type="file" multiple className="hidden" onChange={handleInputChange} />
          </div>
        )}

        {/* File progress list */}
        <UploadProgressList entries={entries} uploading={uploading} onRemove={removeEntry} />

        {/* Summary */}
        {entries.length > 0 && !done && !uploading && (
          <p className="text-[10px] text-text-muted shrink-0">
            {t('upload.readyCount', { ready: readyCount, total: entries.length })}
          </p>
        )}
        {done && (
          <p className="text-xs font-medium text-text-muted shrink-0">
            {t('upload.successCount', { success: successCount, total: entries.length })}
          </p>
        )}

        {/* Footer */}
        <div className="flex justify-end gap-2 shrink-0">
          <button
            onClick={() => handleClose(false)}
            disabled={uploading}
            className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors disabled:opacity-50"
          >
            {t('cancel')}
          </button>
          {done ? (
            <button
              onClick={() => handleClose(false)}
              className="px-3 py-1.5 text-xs bg-accent text-white rounded-lg hover:bg-accent-hover transition-colors"
            >
              {t('done', 'Done')}
            </button>
          ) : (
            <button
              onClick={handleSubmit}
              disabled={readyCount === 0 || uploading}
              className="px-3 py-1.5 text-xs bg-accent text-white rounded-lg hover:bg-accent-hover transition-colors disabled:opacity-50"
            >
              {uploading
                ? t('upload.uploading')
                : t('upload.uploadCount', { count: readyCount })}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
