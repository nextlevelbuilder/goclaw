import { useEffect, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { MarkdownRenderer } from './MarkdownRenderer'
import { AuthImage, downloadFile } from './AuthImage'
import { getApiClient, isApiClientReady } from '../../lib/api'

// Strip ?ft= token and timestamps from filename for display
function cleanFilename(name: string): string {
  // Remove query params
  const base = name.split('?')[0]
  // Remove timestamp suffix: "file.1774537056.md" → "file.md"
  return base.replace(/\.\d{9,}(\.\w+)$/, '$1')
}

interface FilePreviewDialogProps {
  url: string
  filename: string
  mimeType?: string
  onClose: () => void
}

function getLanguage(filename: string): string {
  const ext = filename.split('.').pop()?.toLowerCase() ?? ''
  const map: Record<string, string> = {
    ts: 'typescript', tsx: 'tsx', js: 'javascript', jsx: 'jsx',
    py: 'python', go: 'go', rs: 'rust', java: 'java',
    c: 'c', cpp: 'cpp', h: 'c', rb: 'ruby', sh: 'bash',
    yaml: 'yaml', yml: 'yaml', toml: 'toml', json: 'json',
    xml: 'xml', html: 'html', css: 'css',
  }
  return map[ext] ?? 'text'
}

function isImage(filename: string, mimeType?: string): boolean {
  if (mimeType?.startsWith('image/')) return true
  return /\.(jpe?g|png|gif|webp|svg|bmp|ico)$/i.test(filename)
}

function isVideo(filename: string, mimeType?: string): boolean {
  if (mimeType?.startsWith('video/')) return true
  return /\.(mp4|webm|ogg|mov)$/i.test(filename)
}

function isAudio(filename: string, mimeType?: string): boolean {
  if (mimeType?.startsWith('audio/')) return true
  return /\.(mp3|wav|ogg|flac|aac|m4a)$/i.test(filename)
}

function isMarkdown(filename: string): boolean {
  return /\.(md|markdown)$/i.test(filename)
}

function isCode(filename: string): boolean {
  return /\.(ts|tsx|js|jsx|py|go|rs|java|c|cpp|h|rb|sh|yaml|yml|toml|json|xml|html|css)$/i.test(filename)
}

function isText(filename: string, mimeType?: string): boolean {
  if (mimeType?.startsWith('text/')) return true
  return /\.(txt|log|csv|conf|ini|env)$/i.test(filename)
}

export function FilePreviewDialog({ url, filename: rawFilename, mimeType, onClose }: FilePreviewDialogProps) {
  const { t } = useTranslation('common')
  const [textContent, setTextContent] = useState<string | null>(null)
  const [loadError, setLoadError] = useState(false)

  // Normalize: if filename is a full URL/path, extract the basename
  const filename = (rawFilename.includes('/') ? rawFilename.split('/').pop() : rawFilename)?.split('?')[0] ?? rawFilename
  const needsTextFetch = isMarkdown(filename) || isCode(filename) || isText(filename, mimeType)

  useEffect(() => {
    if (!needsTextFetch) return
    let cancelled = false
    setTextContent(null)
    setLoadError(false)
    // For URLs without ?ft= token, sign them first to get a valid ft= token.
    // This avoids tenant header issues with direct Bearer auth fetch.
    // Fetch with Bearer auth via ApiClient.fetchFile (handles base URL + auth header).
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

  function renderContent() {
    if (isImage(filename, mimeType)) {
      return (
        <AuthImage src={url} alt={displayName} className="max-w-full max-h-[70vh] object-contain mx-auto rounded-lg" />
      )
    }
    if (isVideo(filename, mimeType)) {
      return <video src={url} controls className="max-w-full rounded-lg" />
    }
    if (isAudio(filename, mimeType)) {
      return <audio src={url} controls className="w-full" />
    }
    if (needsTextFetch) {
      if (loadError) {
        return (
          <div className="p-6 flex flex-col items-center gap-3 text-text-secondary">
            <p className="text-sm">{t('errors.serverError', 'Failed to load file preview')}</p>
            <button
              onClick={() => downloadFile(url, displayName)}
              className="inline-flex items-center gap-2 rounded-lg bg-accent/10 px-4 py-2 text-sm text-accent hover:bg-accent/20 transition-colors cursor-pointer"
            >
              {t('download', 'Download')}
            </button>
          </div>
        )
      }
      if (textContent === null) {
        return <p className="text-text-muted text-sm p-4">{t('loading')}</p>
      }
      if (isMarkdown(filename)) {
        // Resolve relative image/link paths in markdown against the file's directory URL
        const baseDir = url.substring(0, url.lastIndexOf('/') + 1)
        const resolved = textContent.replace(
          /(!?\[.*?\])\((?!https?:\/\/|\/|#)(.*?)\)/g,
          (_, prefix, relPath) => `${prefix}(${baseDir}${relPath})`,
        )
        return (
          <div className="p-4 overflow-y-auto max-h-[70vh]">
            <MarkdownRenderer content={resolved} />
          </div>
        )
      }
      if (isCode(filename)) {
        return (
          <div className="overflow-y-auto max-h-[70vh]">
            <SyntaxHighlighter
              language={getLanguage(filename)}
              style={oneDark}
              customStyle={{ margin: 0, fontSize: '13px', borderRadius: 0 }}
            >
              {textContent}
            </SyntaxHighlighter>
          </div>
        )
      }
      // plain text / log / csv
      return (
        <pre className="p-4 text-xs font-mono text-text-primary overflow-auto max-h-[70vh] whitespace-pre-wrap break-words">
          {textContent}
        </pre>
      )
    }
    return (
      <div className="p-6 flex flex-col items-center gap-3 text-text-secondary">
        <p className="text-sm">{t('selectFileToView')}</p>
        <button
          onClick={() => downloadFile(url, displayName)}
          className="inline-flex items-center gap-2 rounded-lg bg-accent/10 px-4 py-2 text-sm text-accent hover:bg-accent/20 transition-colors"
        >
          {t('download')}
        </button>
      </div>
    )
  }

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
          {renderContent()}
        </div>
      </div>
    </div>
  )
}
