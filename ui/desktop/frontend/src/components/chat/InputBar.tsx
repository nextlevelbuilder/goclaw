import { useState, useRef, useCallback, type KeyboardEvent, type DragEvent } from 'react'
import { useTranslation } from 'react-i18next'

/** A file queued for upload or a direct filesystem path (desktop). */
export interface AttachedFile {
  id: string
  /** Browser File object — present for drag/drop and file picker uploads. */
  file?: File
  /** Direct filesystem path — present for pasted paths (desktop only, skip HTTP upload). */
  localPath?: string
  name: string
  /** Image thumbnail data URL (only for browser File images). */
  preview?: string
}

interface InputBarProps {
  onSend: (text: string, files?: AttachedFile[]) => void
  onStop?: () => void
  disabled?: boolean
  isRunning?: boolean
  placeholder?: string
}

/** Human-readable file size. */
function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

/** Detect if text looks like one or more absolute file paths (one per line). */
const FILE_PATH_RE = /^(\/[\w.\-/ ]+(?:\.\w+)?|[A-Z]:\\[\w.\-\\ ]+(?:\.\w+)?)$/

function extractFilePaths(text: string): string[] {
  return text.split('\n').map((l) => l.trim()).filter((l) => FILE_PATH_RE.test(l))
}

const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/svg+xml']

export function InputBar({ onSend, onStop, disabled, isRunning, placeholder }: InputBarProps) {
  const { t } = useTranslation('common')
  const [text, setText] = useState('')
  const [files, setFiles] = useState<AttachedFile[]>([])
  const [dragging, setDragging] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const dragCounter = useRef(0)

  const addFiles = useCallback((incoming: FileList | File[]) => {
    const arr = Array.from(incoming)
    const newFiles: AttachedFile[] = arr.map((f) => ({
      id: crypto.randomUUID().slice(0, 8),
      file: f,
      name: f.name,
    }))

    // Generate image previews
    for (const af of newFiles) {
      if (af.file && IMAGE_TYPES.includes(af.file.type)) {
        const fileObj = af.file
        const reader = new FileReader()
        reader.onload = () => {
          setFiles((prev) => prev.map((f) => f.id === af.id ? { ...f, preview: reader.result as string } : f))
        }
        reader.readAsDataURL(fileObj)
      }
    }

    setFiles((prev) => [...prev, ...newFiles])
  }, [])

  /** Add files by filesystem path (desktop paste). */
  const addLocalPaths = useCallback((paths: string[]) => {
    const newFiles: AttachedFile[] = paths.map((p) => ({
      id: crypto.randomUUID().slice(0, 8),
      localPath: p,
      name: p.split('/').pop() || p.split('\\').pop() || p,
    }))
    setFiles((prev) => [...prev, ...newFiles])
  }, [])

  const removeFile = useCallback((id: string) => {
    setFiles((prev) => prev.filter((f) => f.id !== id))
  }, [])

  const handleSend = useCallback(() => {
    const hasContent = text.trim().length > 0 || files.length > 0
    if (!hasContent || disabled) return
    onSend(text.trim(), files.length > 0 ? files : undefined)
    setText('')
    setFiles([])
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto'
    }
  }, [text, files, disabled, onSend])

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

  // --- Paste: detect file paths ---
  const handlePaste = useCallback((e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    // If clipboard has files (e.g. screenshot paste), handle as file upload
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

    // Check if pasted text looks like file path(s)
    const pasted = e.clipboardData.getData('text/plain')
    const paths = extractFilePaths(pasted)
    if (paths.length > 0) {
      e.preventDefault()
      addLocalPaths(paths)
    }
    // Otherwise, let default paste behavior handle it (normal text)
  }, [addFiles, addLocalPaths])

  // --- Drag & drop ---
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
  const handleDragOver = (e: DragEvent) => {
    e.preventDefault()
  }
  const handleDrop = (e: DragEvent) => {
    e.preventDefault()
    dragCounter.current = 0
    setDragging(false)
    if (e.dataTransfer.files.length > 0) {
      addFiles(e.dataTransfer.files)
    }
  }

  const handleAttachClick = () => {
    fileInputRef.current?.click()
  }

  const handleFileChange = () => {
    const input = fileInputRef.current
    if (input?.files && input.files.length > 0) {
      addFiles(input.files)
      input.value = '' // reset so same file can be re-selected
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
        {/* Hidden file input */}
        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={handleFileChange}
        />

        {/* Drop overlay */}
        {dragging && (
          <div className="mb-2 rounded-xl border-2 border-dashed border-accent/50 bg-accent/5 py-4 text-center text-xs text-accent">
            {t('dropFilesHere', 'Drop files here')}
          </div>
        )}

        {/* Attached files preview */}
        {files.length > 0 && (
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
                  onClick={() => removeFile(af.id)}
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
        )}

        {/* Input container */}
        <div className={[
          'flex items-end gap-0 bg-surface-secondary rounded-2xl border transition-colors',
          dragging ? 'border-accent/50' : 'border-border focus-within:border-accent/40',
        ].join(' ')}>
          {/* Attach button */}
          <button
            onClick={handleAttachClick}
            className="p-3 text-text-muted hover:text-text-secondary transition-colors shrink-0 cursor-pointer"
            title={t('attachFile')}
            disabled={disabled}
          >
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48" />
            </svg>
          </button>

          {/* Textarea */}
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

          {/* Send / Stop button */}
          <div className="p-2 shrink-0">
            {isRunning ? (
              <button
                onClick={onStop}
                className="w-10 h-10 flex items-center justify-center hover:opacity-90 transition-opacity"
                title={t('stopGeneration')}
              >
                {/* Spinning border ring */}
                <svg className="absolute w-8 h-8 animate-spin" viewBox="0 0 32 32" fill="none" style={{ animationDuration: '1.5s' }}>
                  <circle cx="16" cy="16" r="14" stroke="currentColor" strokeWidth="2" className="text-error/20" />
                  <path d="M16 2 A14 14 0 0 1 30 16" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className="text-error" />
                </svg>
                {/* Center stop icon */}
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
