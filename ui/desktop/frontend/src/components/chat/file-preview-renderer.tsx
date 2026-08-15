import { useTranslation } from 'react-i18next'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { MarkdownRenderer } from './MarkdownRenderer'
import { AuthImage, downloadFile } from './AuthImage'

export function getLanguage(filename: string): string {
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

export function isImage(filename: string, mimeType?: string): boolean {
  if (mimeType?.startsWith('image/')) return true
  return /\.(jpe?g|png|gif|webp|svg|bmp|ico)$/i.test(filename)
}

export function isVideo(filename: string, mimeType?: string): boolean {
  if (mimeType?.startsWith('video/')) return true
  return /\.(mp4|webm|ogg|mov)$/i.test(filename)
}

export function isAudio(filename: string, mimeType?: string): boolean {
  if (mimeType?.startsWith('audio/')) return true
  return /\.(mp3|wav|ogg|flac|aac|m4a)$/i.test(filename)
}

export function isMarkdown(filename: string): boolean {
  return /\.(md|markdown)$/i.test(filename)
}

export function isCode(filename: string): boolean {
  return /\.(ts|tsx|js|jsx|py|go|rs|java|c|cpp|h|rb|sh|yaml|yml|toml|json|xml|html|css)$/i.test(filename)
}

export function isText(filename: string, mimeType?: string): boolean {
  if (mimeType?.startsWith('text/')) return true
  return /\.(txt|log|csv|conf|ini|env)$/i.test(filename)
}

interface FilePreviewRendererProps {
  url: string
  filename: string
  displayName: string
  mimeType?: string
  textContent: string | null
  loadError: boolean
  needsTextFetch: boolean
}

export function FilePreviewRenderer({
  url, filename, displayName, mimeType, textContent, loadError, needsTextFetch,
}: FilePreviewRendererProps) {
  const { t } = useTranslation('common')

  if (isImage(filename, mimeType)) {
    return <AuthImage src={url} alt={displayName} className="max-w-full max-h-[70vh] object-contain mx-auto rounded-lg" />
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
