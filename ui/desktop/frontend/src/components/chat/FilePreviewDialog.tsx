import { useEffect, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { downloadFile } from './AuthImage'
import { getApiClient, isApiClientReady } from '../../lib/api'
import { FilePreviewRenderer, isMarkdown, isCode, isText } from './file-preview-renderer'

// Strip ?ft= token and timestamps from filename for display
function cleanFilename(name: string): string {
  const base = name.split('?')[0]
  return base.replace(/\.\d{9,}(\.\w+)$/, '$1')
}

interface FilePreviewDialogProps {
  url: string
  filename: string
  mimeType?: string
  onClose: () => void
}

export function FilePreviewDialog({ url, filename: rawFilename, mimeType, onClose }: FilePreviewDialogProps) {
  const { t } = useTranslation('common')
  const [textContent, setTextContent] = useState<string | null>(null)
  const [loadError, setLoadError] = useState(false)

  const filename = (rawFilename.includes('/') ? rawFilename.split('/').pop() : rawFilename)?.split('?')[0] ?? rawFilename
  const needsTextFetch = isMarkdown(filename) || isCode(filename) || isText(filename, mimeType)

  useEffect(() => {
    if (!needsTextFetch) return
    let cancelled = false
    setTextContent(null)
    setLoadError(false)
    const doFetch = isApiClientReady()
      ? getApiClient().fetchFile(url)
      : fetch(url)
    doFetch
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.text()
      })
      .then((text) => { if (!cancelled) setTextContent(text) })
      .catch(() => { if (!cancelled) setLoadError(true) })
    return () => { cancelled = true }
  }, [url, needsTextFetch])

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() },
    [onClose],
  )

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])

  const displayName = cleanFilename(filename)

  return (
    <div
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="relative bg-surface-primary border border-border rounded-xl shadow-2xl w-full max-w-5xl mx-4 overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center gap-2 px-4 py-3 border-b border-border bg-surface-secondary">
          <span className="flex-1 text-sm font-medium text-text-primary truncate">{displayName}</span>
          <button
            type="button"
            onClick={() => downloadFile(url, displayName)}
            title={t('download')}
            className="p-1.5 rounded text-text-muted hover:text-text-primary hover:bg-surface-tertiary transition-colors"
          >
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="7 10 12 15 17 10" />
              <line x1="12" y1="15" x2="12" y2="3" />
            </svg>
          </button>
          <button
            type="button"
            title={t('close')}
            onClick={onClose}
            className="p-1.5 rounded text-text-muted hover:text-text-primary hover:bg-surface-tertiary transition-colors"
          >
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        {/* Content */}
        <div className="overflow-hidden">
          <FilePreviewRenderer
            url={url}
            filename={filename}
            displayName={displayName}
            mimeType={mimeType}
            textContent={textContent}
            loadError={loadError}
            needsTextFetch={needsTextFetch}
          />
        </div>
      </div>
    </div>
  )
}
