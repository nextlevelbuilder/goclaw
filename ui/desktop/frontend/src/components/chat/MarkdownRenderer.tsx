import ReactMarkdown from 'react-markdown'
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
  return (
    <div className="text-sm leading-relaxed text-text-primary break-words overflow-hidden">
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
                className="px-1.5 py-0.5 rounded bg-surface-tertiary text-accent font-mono text-[13px]"
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
            <a href={href} className="text-accent hover:underline" target="_blank" rel="noopener noreferrer">
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
                  onClick={() => downloadFile(resolvedSrc, filename)}
                  className="absolute top-2 right-2 opacity-0 group-hover/img:opacity-100 transition-opacity rounded bg-black/60 p-1.5 text-white hover:bg-black/80"
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
        li: ({ children }) => <li className="text-sm">{children}</li>,
        blockquote: ({ children }) => (
          <blockquote className="border-l-2 border-accent pl-3 my-3 text-text-secondary italic">
            {children}
          </blockquote>
        ),
        table: ({ children }) => (
          <div className="overflow-x-auto mb-3">
            <table className="min-w-full text-sm border-collapse">{children}</table>
          </div>
        ),
        th: ({ children }) => (
          <th className="border border-border px-3 py-1.5 bg-surface-secondary text-left font-medium text-text-primary">
            {children}
          </th>
        ),
        td: ({ children }) => (
          <td className="border border-border px-3 py-1.5 text-text-secondary">{children}</td>
        ),
        h1: ({ children }) => <h1 className="text-xl font-semibold mb-3 mt-4">{children}</h1>,
        h2: ({ children }) => <h2 className="text-lg font-semibold mb-2 mt-3">{children}</h2>,
        h3: ({ children }) => <h3 className="text-base font-semibold mb-2 mt-3">{children}</h3>,
        hr: () => <hr className="border-border my-4" />,
      }}
    />
    </div>
  )
}
