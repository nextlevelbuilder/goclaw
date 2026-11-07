import ReactMarkdown from 'react-markdown'
import { useTranslation } from 'react-i18next'
import remarkGfm from 'remark-gfm'
import rehypeSanitize from 'rehype-sanitize'
import { CodeBlock } from './CodeBlock'
import { FileButton } from './FileButton'
import { AuthImage, downloadFile } from './AuthImage'
import { getApiClient, isApiClientReady } from '../../lib/api'

interface MarkdownRendererProps {
  content: string
  /** Base URL for resolving relative paths (e.g. images in .md files) */
  baseUrl?: string
}

const LOCAL_FILE_EXT_RE = /\.(png|jpg|jpeg|gif|webp|svg|bmp|mp3|wav|ogg|flac|aac|m4a|mp4|webm|mkv|avi|mov|pdf|doc|docx|xls|xlsx|csv|txt|md|json|zip)$/i

function isFileLink(href: string | undefined): boolean {
  if (!href) return false
  if (href.includes('/v1/files/')) return true
  if ((href.startsWith('./') || href.startsWith('../')) && LOCAL_FILE_EXT_RE.test(href)) return true
  return false
}

function resolveFileUrl(path: string, baseUrl?: string): string {
  if (path.startsWith('http://') || path.startsWith('https://')) return path
  // Already a /v1/files/ URL — use as-is
  if (path.includes('/v1/files/')) {
    if (isApiClientReady()) return getApiClient().getBaseUrl() + path
    return path
  }
  // Strip MEDIA: prefix (tool results embed paths as MEDIA:/path/to/file.png)
  const clean = path.replace(/^MEDIA:/, '')
  // Normalize backslashes (Windows paths: C:\Users\... → C:/Users/...)
  const normalized = clean.replace(/\\/g, '/')
  // Relative path with baseUrl — resolve against the base directory
  if (baseUrl && !normalized.startsWith('/') && !/^[a-zA-Z]:\//.test(normalized)) {
    const baseDir = baseUrl.substring(0, baseUrl.lastIndexOf('/') + 1)
    return baseDir + normalized
  }
  // Absolute path: Unix /... or Windows C:/...
  const filePath = normalized.startsWith('/')
    ? `/v1/files${normalized}`
    : /^[a-zA-Z]:\//.test(normalized)
      ? `/v1/files/${normalized}`
      : `/v1/files/${normalized.split('/').pop() ?? normalized}`
  if (isApiClientReady()) return getApiClient().getBaseUrl() + filePath
  return filePath
}

export function MarkdownRenderer({ content, baseUrl }: MarkdownRendererProps) {
  const { t } = useTranslation('common')
  return (
    <div className="text-body text-ink-secondary break-words overflow-hidden">
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      rehypePlugins={[rehypeSanitize]}
      children={content}
      components={{
        code({ className, children, ...props }) {
          const match = /language-(\w+)/.exec(className || '')
          if (!match) {
            return (
              <code
                className="rounded-sm bg-surface-hover px-1.5 py-0.5 font-mono text-code text-ink"
                {...props}
              >
                {children}
              </code>
            )
          }
          return (
            <CodeBlock language={match[1]} code={String(children).replace(/\n$/, '')} />
          )
        },
        p: ({ children }) => <p className="mb-3 last:mb-0">{children}</p>,
        a: ({ children, href }) => {
          if (isFileLink(href)) {
            const filename = decodeURIComponent(href!.split('/').pop() ?? 'file')
            return <FileButton url={resolveFileUrl(href!, baseUrl)} filename={filename} />
          }
          return (
            <a href={href} className="text-accent hover:underline focus-visible:underline" target="_blank" rel="noopener noreferrer">
              {children}
            </a>
          )
        },
        img: ({ src, alt }) => {
          if (isFileLink(src) || (baseUrl && src && !src.startsWith('http'))) {
            const resolvedSrc = resolveFileUrl(src!, baseUrl)
            const filename = src!.split('/').pop()?.split('?')[0] ?? 'image'
            return (
              <span className="group/img relative inline-block">
                <AuthImage src={resolvedSrc} alt={alt ?? ''} className="max-w-full rounded-lg" />
                <button
                  aria-label={t('download')}
                  onClick={() => downloadFile(resolvedSrc, filename)}
                  className="absolute top-2 right-2 opacity-0 group-hover/img:opacity-100 focus-visible:opacity-100 transition-opacity rounded-sm bg-surface-raised p-2.5 text-ink shadow-popover hover:bg-surface-hover"
                >
                  <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                    <polyline points="7 10 12 15 17 10" />
                    <line x1="12" y1="15" x2="12" y2="3" />
                  </svg>
                </button>
              </span>
            )
          }
          return <img src={src} alt={alt ?? ''} className="max-w-full rounded-lg" loading="lazy" />
        },
        ul: ({ children }) => <ul className="list-disc ml-5 mb-3 space-y-1">{children}</ul>,
        ol: ({ children }) => <ol className="list-decimal ml-5 mb-3 space-y-1">{children}</ol>,
        li: ({ children }) => <li>{children}</li>,
        blockquote: ({ children }) => (
          <blockquote className="border-l-2 border-accent pl-3 my-3 text-ink-secondary">
            {children}
          </blockquote>
        ),
        table: ({ children }) => (
          <div className="overflow-x-auto mb-3">
            <table className="min-w-full text-ui border-collapse">{children}</table>
          </div>
        ),
        th: ({ children }) => (
          <th className="border border-border-subtle px-3 py-1.5 bg-surface-raised text-left font-medium text-ink">
            {children}
          </th>
        ),
        td: ({ children }) => (
          <td className="border border-border-subtle px-3 py-1.5 text-ink-secondary">{children}</td>
        ),
        h1: ({ children }) => <h1 className="text-title-lg text-ink mb-3 mt-4">{children}</h1>,
        h2: ({ children }) => <h2 className="text-title text-ink mb-2 mt-3">{children}</h2>,
        h3: ({ children }) => <h3 className="text-ui font-semibold text-ink mb-2 mt-3">{children}</h3>,
        hr: () => <hr className="border-border-subtle my-4" />,
      }}
    />
    </div>
  )
}
