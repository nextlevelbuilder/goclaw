import { useState, useRef, useCallback, type KeyboardEvent, type DragEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useFileAttachments, extractFilePaths } from './use-file-attachments'
import { AttachmentPreviewStrip } from './attachment-preview-strip'
import type { AttachedFile } from './use-file-attachments'

export type { AttachedFile }

interface InputBarProps {
  onSend: (text: string, files?: AttachedFile[]) => void
  onStop?: () => void
  disabled?: boolean
  isRunning?: boolean
  placeholder?: string
}

export function InputBar({ onSend, onStop, disabled, isRunning, placeholder }: InputBarProps) {
  const { t } = useTranslation('common')
  const [text, setText] = useState('')
  const [dragging, setDragging] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const dragCounter = useRef(0)

  const { files, addFiles, addLocalPaths, removeFile, clearFiles } = useFileAttachments()

  const handleSend = useCallback(() => {
    const hasContent = text.trim().length > 0 || files.length > 0
    if (!hasContent || disabled) return
    onSend(text.trim(), files.length > 0 ? files : undefined)
    setText('')
    clearFiles()
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto'
    }
  }, [text, files, disabled, onSend, clearFiles])

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
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
    const pasted = e.clipboardData.getData('text/plain')
    const paths = extractFilePaths(pasted)
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

  const hasContent = text.trim().length > 0 || files.length > 0

  return (
    <div
      className="px-4 pb-4 pt-1 shrink-0"
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      <div className="max-w-3xl mx-auto">
        <input ref={fileInputRef} type="file" multiple className="hidden" onChange={handleFileChange} />

        {dragging && (
          <div className="mb-2 rounded-xl border-2 border-dashed border-accent/50 bg-accent/5 py-4 text-center text-xs text-accent">
            {t('dropFilesHere', 'Drop files here')}
          </div>
        )}

        <AttachmentPreviewStrip files={files} onRemove={removeFile} />

        <div className={[
          'flex items-end gap-0 bg-surface-secondary rounded-2xl border transition-colors',
          dragging ? 'border-accent/50' : 'border-border focus-within:border-accent/40',
        ].join(' ')}>
          <button
            onClick={() => fileInputRef.current?.click()}
            className="p-3 text-text-muted hover:text-text-secondary transition-colors shrink-0 cursor-pointer"
            title={t('attachFile')}
            disabled={disabled}
          >
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48" />
            </svg>
          </button>

          <textarea
            ref={textareaRef}
            value={text}
            onChange={(e) => { setText(e.target.value); handleInput() }}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            placeholder={placeholder ?? t('sendMessage')}
            disabled={disabled}
            rows={1}
            className="flex-1 bg-transparent text-text-primary text-base md:text-sm py-3 px-0 focus:outline-none placeholder:text-text-muted resize-none overflow-y-auto"
            style={{ maxHeight: 160 }}
          />

          <div className="p-2 shrink-0">
            {isRunning ? (
              <button
                onClick={onStop}
                className="w-10 h-10 flex items-center justify-center hover:opacity-90 transition-opacity"
                title={t('stopGeneration')}
              >
                <svg className="absolute w-8 h-8 animate-spin" viewBox="0 0 32 32" fill="none" style={{ animationDuration: '1.5s' }}>
                  <circle cx="16" cy="16" r="14" stroke="currentColor" strokeWidth="2" className="text-error/20" />
                  <path d="M16 2 A14 14 0 0 1 30 16" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className="text-error" />
                </svg>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor" className="relative text-error">
                  <rect x="4" y="4" width="16" height="16" rx="3" />
                </svg>
              </button>
            ) : (
              <button
                onClick={handleSend}
                disabled={!hasContent || disabled}
                className="w-8 h-8 flex items-center justify-center rounded-xl bg-accent text-white hover:bg-accent-hover transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
                title={t('sendMessageTitle')}
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <line x1="22" y1="2" x2="11" y2="13" />
                  <polygon points="22 2 15 22 11 13 2 9 22 2" />
                </svg>
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
