import { useTranslation } from 'react-i18next'
import { formatSize, type AttachedFile } from './use-file-attachments'

interface AttachmentPreviewStripProps {
  files: AttachedFile[]
  onRemove: (id: string) => void
}

export function AttachmentPreviewStrip({ files, onRemove }: AttachmentPreviewStripProps) {
  const { t } = useTranslation('common')

  if (files.length === 0) return null

  return (
    <div className="flex flex-wrap gap-1.5 mb-2">
      {files.map((af) => (
        <div
          key={af.id}
          className="group flex items-center gap-1.5 bg-surface-secondary border border-border rounded-lg px-2 py-1 text-xs max-w-[200px]"
        >
          {af.preview ? (
            <img src={af.preview} alt="" className="w-5 h-5 rounded object-cover shrink-0" />
          ) : (
            <svg className="w-3.5 h-3.5 text-text-muted shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
              <polyline points="14 2 14 8 20 8" />
            </svg>
          )}
          <span className="truncate text-text-secondary">{af.name}</span>
          {af.file && <span className="text-text-muted shrink-0">({formatSize(af.file.size)})</span>}
          <button
            onClick={() => onRemove(af.id)}
            className="ml-auto shrink-0 text-text-muted hover:text-error transition-colors opacity-0 group-hover:opacity-100"
            title={t('remove', 'Remove')}
          >
            <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5} strokeLinecap="round">
              <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>
      ))}
    </div>
  )
}
