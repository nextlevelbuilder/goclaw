// File upload progress list — renders per-file status icons and entry rows.
// Extracted from StorageUploadDialog for focused display logic.

import { useTranslation } from 'react-i18next'

export type FileStatus = 'ready' | 'uploading' | 'success' | 'error'

export interface FileEntry {
  id: string
  file: File
  status: FileStatus
  error?: string
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

interface UploadProgressListProps {
  entries: FileEntry[]
  uploading: boolean
  onRemove: (id: string) => void
}

export function UploadProgressList({ entries, uploading, onRemove }: UploadProgressListProps) {
  const { t } = useTranslation('common')

  if (entries.length === 0) return null

  return (
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
              onClick={(e) => { e.stopPropagation(); onRemove(entry.id) }}
              className="shrink-0 rounded-sm p-1 text-text-muted hover:text-text-primary"
              title={t('remove', 'Remove')}
            >
              <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
              </svg>
            </button>
          )}
        </div>
      ))}
    </div>
  )
}
