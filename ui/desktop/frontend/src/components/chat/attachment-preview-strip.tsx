import { useTranslation } from 'react-i18next'
import { IconClose, IconDocument } from '../common/Icons'
import { IconButton } from '../ui'
import { formatSize, type AttachedFile } from './use-file-attachments'

interface AttachmentPreviewStripProps {
  files: AttachedFile[]
  onRemove: (id: string) => void
}

/** Files queued in the composer, each removable before sending. */
export function AttachmentPreviewStrip({ files, onRemove }: AttachmentPreviewStripProps) {
  const { t } = useTranslation('chat')

  if (files.length === 0) return null

  return (
    <ul className="mb-2 flex flex-wrap gap-1.5">
      {files.map((af) => (
        <li
          key={af.id}
          className="flex h-7 max-w-52 items-center gap-1.5 rounded-sm border border-border-subtle bg-surface-base pl-2 text-caption"
        >
          {af.preview ? (
            <img src={af.preview} alt="" className="size-4 shrink-0 rounded-sm object-cover" />
          ) : (
            <IconDocument size={14} className="shrink-0 text-ink-muted" />
          )}
          <span className="truncate text-ink-secondary">{af.name}</span>
          {af.file && <span className="shrink-0 text-ink-muted">{formatSize(af.file.size)}</span>}
          <IconButton
            size="sm"
            aria-label={t('composer.removeFile', { name: af.name })}
            icon={<IconClose size={12} />}
            onClick={() => onRemove(af.id)}
          />
        </li>
      ))}
    </ul>
  )
}
