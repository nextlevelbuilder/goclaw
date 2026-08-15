import { useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { FilePreviewDialog } from './FilePreviewDialog'
import { downloadFile } from './AuthImage'

export interface FileButtonProps {
  url: string
  filename: string
  mimeType?: string
  size?: number
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function fileIcon(name: string): string {
  if (/\.(md|markdown)$/i.test(name)) return '📝'
  if (/\.(ts|tsx|js|jsx|py|go|rs|java|c|cpp|h|rb|sh|yaml|yml|toml)$/i.test(name)) return '💻'
  if (/\.(zip|tar|gz|rar|7z)$/i.test(name)) return '📦'
  if (/\.(csv|xls|xlsx)$/i.test(name)) return '📊'
  if (/\.(pdf)$/i.test(name)) return '📕'
  if (/\.(json|xml|html|css)$/i.test(name)) return '📋'
  return '📄'
}

function truncateName(name: string, max = 30): string {
  if (name.length <= max) return name
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.')) : ''
  return name.slice(0, max - ext.length - 1) + '…' + ext
}

export function FileButton({ url, filename, mimeType, size }: FileButtonProps) {
  const { t } = useTranslation('common')
  const [previewOpen, setPreviewOpen] = useState(false)

  // Strip timestamp from filename for display: "file.1774537056.md" → "file.md"
  const displayName = filename.split('?')[0].replace(/\.\d{9,}(\.\w+)$/, '$1')

  return (
    <>
      <span className="inline-flex items-center gap-2 border border-border rounded-lg px-3 py-2 hover:bg-surface-tertiary/30 transition-colors text-sm max-w-xs">
        <button
          type="button"
          onClick={() => setPreviewOpen(true)}
          className="inline-flex items-center gap-2 min-w-0 cursor-pointer"
        >
          <span className="text-base leading-none">{fileIcon(displayName)}</span>
          <span className="text-text-primary truncate text-left">
            {truncateName(displayName)}
          </span>
          {size !== undefined && (
            <span className="text-text-muted text-xs shrink-0">{formatFileSize(size)}</span>
          )}
        </button>
        <button
          type="button"
          title={t('download')}
          onClick={() => downloadFile(url, displayName)}
          className="text-text-muted hover:text-text-primary transition-colors shrink-0 cursor-pointer"
        >
          <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
            <polyline points="7 10 12 15 17 10" />
            <line x1="12" y1="15" x2="12" y2="3" />
          </svg>
        </button>
      </span>

      {previewOpen && createPortal(
        <FilePreviewDialog
          url={url}
          filename={filename}
          mimeType={mimeType}
          onClose={() => setPreviewOpen(false)}
        />,
        document.body,
      )}
    </>
  )
}
