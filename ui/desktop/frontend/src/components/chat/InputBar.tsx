import { useState, useRef, useCallback, type DragEvent, type KeyboardEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { IconPlus } from '../common/Icons'
import { IconButton } from '../ui'
import { useFileAttachments, extractFilePaths } from './use-file-attachments'
import { AttachmentPreviewStrip } from './attachment-preview-strip'
import { IconArrowUp, IconStop } from './chat-icons'
import type { AttachedFile } from './use-file-attachments'

export type { AttachedFile }

interface InputBarProps {
  onSend: (text: string, files?: AttachedFile[]) => void
  onStop?: () => void
  disabled?: boolean
  isRunning?: boolean
  placeholder?: string
  /** Pills shown between the attach button and the send button (agent, model). */
  pills?: ReactNode
  autoFocus?: boolean
}

/** The composer: raised surface, textarea, attach, pills, and a square send button that becomes Stop. */
export function InputBar({ onSend, onStop, disabled, isRunning, placeholder, pills, autoFocus }: InputBarProps) {
  const { t } = useTranslation('chat')
  const [text, setText] = useState('')
  const [dragging, setDragging] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const dragCounter = useRef(0)

  const { files, addFiles, addLocalPaths, removeFile, clearFiles } = useFileAttachments()

  const hasContent = text.trim().length > 0 || files.length > 0

  const handleSend = useCallback(() => {
    if (!hasContent || disabled) return
    onSend(text.trim(), files.length > 0 ? files : undefined)
    setText('')
    clearFiles()
    if (textareaRef.current) textareaRef.current.style.height = 'auto'
  }, [hasContent, text, files, disabled, onSend, clearFiles])

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      handleSend()
    }
  }

  const handleInput = () => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 160) + 'px'
  }

  const handlePaste = useCallback((e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const items = e.clipboardData.items
    const pastedFiles: File[] = []
    for (let i = 0; i < items.length; i++) {
      if (items[i].kind === 'file') {
        const f = items[i].getAsFile()
        if (f) pastedFiles.push(f)
      }
    }
    if (pastedFiles.length > 0) {
      e.preventDefault()
      addFiles(pastedFiles)
      return
    }
    const paths = extractFilePaths(e.clipboardData.getData('text/plain'))
    if (paths.length > 0) {
      e.preventDefault()
      addLocalPaths(paths)
    }
  }, [addFiles, addLocalPaths])

  const handleDragEnter = (e: DragEvent) => {
    e.preventDefault()
    dragCounter.current++
    if (e.dataTransfer.types.includes('Files')) setDragging(true)
  }
  const handleDragLeave = (e: DragEvent) => {
    e.preventDefault()
    dragCounter.current--
    if (dragCounter.current === 0) setDragging(false)
  }
  const handleDragOver = (e: DragEvent) => { e.preventDefault() }
  const handleDrop = (e: DragEvent) => {
    e.preventDefault()
    dragCounter.current = 0
    setDragging(false)
    if (e.dataTransfer.files.length > 0) addFiles(e.dataTransfer.files)
  }

  const handleFileChange = () => {
    const input = fileInputRef.current
    if (input?.files && input.files.length > 0) {
      addFiles(input.files)
      input.value = ''
    }
  }

  return (
    <div
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
      className={cn(
        'rounded-lg border bg-surface-raised px-3 pb-2 pt-3 shadow-panel transition-colors',
        'focus-within:border-border-control has-[:focus-visible]:outline has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-focus-ring',
        dragging ? 'border-accent' : 'border-border-subtle',
      )}
    >
      <input ref={fileInputRef} type="file" multiple className="hidden" onChange={handleFileChange} />

      {dragging && (
        <p className="mb-2 rounded-sm border border-dashed border-accent py-3 text-center text-caption text-accent">
          {t('composer.dropFiles')}
        </p>
      )}

      <AttachmentPreviewStrip files={files} onRemove={removeFile} />

      <textarea
        ref={textareaRef}
        value={text}
        autoFocus={autoFocus}
        onChange={(e) => { setText(e.target.value); handleInput() }}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        placeholder={placeholder}
        aria-label={placeholder}
        disabled={disabled}
        rows={1}
        className="block max-h-40 min-h-[22px] w-full resize-none overflow-y-auto bg-transparent px-0.5 pb-2.5 text-base text-ink outline-none placeholder:text-ink-muted disabled:opacity-50 md:text-body"
      />

      <div className="flex items-center gap-2">
        <IconButton
          aria-label={t('composer.attach')}
          icon={<IconPlus size={16} />}
          onClick={() => fileInputRef.current?.click()}
          disabled={disabled}
        />
        {pills}
        {isRunning ? (
          <IconButton
            variant="primary"
            aria-label={t('composer.stop')}
            icon={<IconStop size={16} />}
            onClick={onStop}
            className="ml-auto"
          />
        ) : (
          <IconButton
            variant="primary"
            aria-label={t('composer.send')}
            icon={<IconArrowUp size={16} />}
            onClick={handleSend}
            disabled={!hasContent || disabled}
            className="ml-auto"
          />
        )}
      </div>
    </div>
  )
}
