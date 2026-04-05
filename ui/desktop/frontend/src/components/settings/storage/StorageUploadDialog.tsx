// Upload dialog with drag-drop zone, blocked extension validation, and multi-file support.
// Adapted from ui/web file-upload-dialog.tsx for desktop styling (no Radix).

import { useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'

const BLOCKED_EXTENSIONS = new Set([
  '.exe', '.sh', '.bat', '.cmd', '.ps1', '.com', '.msi', '.scr',
])
const MAX_FILE_SIZE = 50 * 1024 * 1024 // 50MB

type FileStatus = 'ready' | 'uploading' | 'success' | 'error'

interface FileEntry {
  id: string
  file: File
  status: FileStatus
  error?: string
}

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
        return { id: uniqueId(), file: f, status: 'error' as const, error: t('upload.blockedType', { ext }) }
      }
      if (f.size > MAX_FILE_SIZE) {
        return { id: uniqueId(), file: f, status: 'error' as const, error: t('upload.tooLarge') }
      }
      return { id: uniqueId(), file: f, status: 'ready' as const }
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
      setEntries((prev) => prev.map((e) => (e.id === entry.id ? { ...e, status: 'uploading' } : e)))
      try {
        await onUpload(entry.file)
        setEntries((prev) => prev.map((e) => (e.id === entry.id ? { ...e, status: 'success' } : e)))
      } catch (err) {
        setEntries((prev) =>
          prev.map((e) =>
            e.id === entry.id
              ? { ...e, status: 'error', error: err instanceof Error ? err.message : t('upload.failed') }
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
            {/* Upload icon */}
            <svg className="h-8 w-8 text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="17 8 12 3 7 8" />
              <line x1="12" y1="3" x2="12" y2="15" />
            </svg>
            <p className="text-xs text-text-muted">
              {dragging ? t('upload.dropHere') : t('upload.dropOrClick')}
            </p>
            <p className="text-[10px] text-text-muted/60">{t('upload.maxSize')}</p>
            <input
              ref={inputRef}
              type="file"
              multiple
              className="hidden"
              onChange={handleInputChange}
            />
          </div>
        )}

        {/* File list */}
        {entries.length > 0 && (
          <div className="flex flex-col gap-1 overflow-y-auto flex-1 min-h-0">
            {entries.map((entry) => (
              <div key={entry.id} className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-xs">
                <StatusIcon status={entry.status} />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium text-text-primary">{entry.file.name}</span>
                    <span className="shrink-0 text-[10px] text-text-muted">
                      {(entry.file.size / 1024).toFixed(1)} KB
                    </span>
                  </div>
                  {entry.status === 'error' && (
                    <p className="text-[10px] text-error truncate">{entry.error}</p>
                  )}
                </div>
                {!uploading && entry.status !== 'uploading' && entry.status !== 'success' && (
                  <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); removeEntry(entry.id) }}
                    className="shrink-0 rounded-sm p-1 text-text-muted hover:text-text-primary"
                  >
                    <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                      <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
                    </svg>
                  </button>
                )}
              </div>
            ))}
          </div>
        )}

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

function StatusIcon({ status }: { status: FileStatus }) {
  switch (status) {
    case 'uploading':
      return (
        <svg className="h-4 w-4 shrink-0 animate-spin text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
          <path d="M21 12a9 9 0 1 1-6.219-8.56" />
        </svg>
      )
    case 'ready':
      return (
        <svg className="h-4 w-4 shrink-0 text-accent" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" /><polyline points="22 4 12 14.01 9 11.01" />
        </svg>
      )
    case 'success':
      return (
        <svg className="h-4 w-4 shrink-0 text-green-600" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" /><polyline points="22 4 12 14.01 9 11.01" />
        </svg>
      )
    case 'error':
      return (
        <svg className="h-4 w-4 shrink-0 text-error" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <circle cx="12" cy="12" r="10" /><line x1="15" y1="9" x2="9" y2="15" /><line x1="9" y1="9" x2="15" y2="15" />
        </svg>
      )
  }
}
