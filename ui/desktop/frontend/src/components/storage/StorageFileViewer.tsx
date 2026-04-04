// Multi-format file viewer: loading state + empty state wrapper.
// Rendering logic delegated to file-viewer-content.tsx.

import { useTranslation } from 'react-i18next'
import { FileContentBody } from './file-viewer-content'

export function FileContentPanel({
  fileContent, contentLoading, fetchBlob, onDownload,
}: {
  fileContent: { content: string; path: string; size: number } | null
  contentLoading: boolean
  fetchBlob?: (path: string) => Promise<Blob>
  onDownload?: (path: string) => void
}) {
  const { t } = useTranslation('common')

  if (contentLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <svg className="h-5 w-5 animate-spin text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
          <path d="M21 12a9 9 0 1 1-6.219-8.56" />
        </svg>
      </div>
    )
  }

  if (fileContent) {
    return (
      <FileContentBody
        path={fileContent.path}
        content={fileContent.content}
        size={fileContent.size}
        fetchBlob={fetchBlob}
        onDownload={onDownload ? () => onDownload(fileContent.path) : undefined}
      />
    )
  }

  return (
    <div className="flex items-center justify-center py-8 text-xs text-text-muted">
      {t('selectFileToView')}
    </div>
  )
}
