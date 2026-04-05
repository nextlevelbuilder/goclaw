// Multi-format file viewer: code (syntax highlight), markdown, CSV table, images, unsupported.
// Uses react-syntax-highlighter (Prism + oneDark) matching desktop CodeBlock pattern.

import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { MarkdownRenderer } from '../../chat/MarkdownRenderer'
import {
  extOf, langFor, stripFrontmatter, formatSize,
  isImageFile, isTextFile, CODE_EXTENSIONS,
} from '../../../lib/file-helpers'

// --- Code viewer with syntax highlighting + copy ---

function CodeViewer({ content, language }: { content: string; language: string }) {
  const { t } = useTranslation('common')
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="rounded-lg overflow-hidden border border-border">
      <div className="flex items-center justify-between px-3 py-1.5 bg-surface-tertiary text-[11px]">
        <span className="text-text-muted font-mono uppercase tracking-wide">{language || 'text'}</span>
        <button onClick={handleCopy} className="text-text-muted hover:text-text-primary transition-colors cursor-pointer">
          {copied ? `${t('copy')}!` : t('copy')}
        </button>
      </div>
      <SyntaxHighlighter
        language={language}
        style={oneDark}
        customStyle={{
          margin: 0,
          padding: '12px 16px',
          fontSize: '13px',
          background: 'var(--color-surface-primary)',
          borderRadius: 0,
        }}
      >
        {content}
      </SyntaxHighlighter>
    </div>
  )
}

// --- CSV viewer as table ---

function CsvViewer({ content }: { content: string }) {
  const { t } = useTranslation('common')
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const rows = useMemo(() => {
    return content.split('\n').filter(Boolean).map((line) => {
      const cols: string[] = []
      let cur = ''
      let inQuote = false
      for (let i = 0; i < line.length; i++) {
        const ch = line[i]
        if (ch === '"') { inQuote = !inQuote; continue }
        if (ch === ',' && !inQuote) { cols.push(cur.trim()); cur = ''; continue }
        cur += ch
      }
      cols.push(cur.trim())
      return cols
    })
  }, [content])

  const header = rows[0]
  if (!header || rows.length === 0) return <pre className="text-xs p-4 text-text-primary">{content}</pre>
  const body = rows.slice(1)

  return (
    <div className="rounded-lg border border-border flex flex-col overflow-hidden">
      <div className="flex items-center justify-between px-3 py-1.5 bg-surface-tertiary text-[11px] shrink-0">
        <span className="text-text-muted font-mono uppercase tracking-wide">{body.length} rows</span>
        <button onClick={handleCopy} className="text-text-muted hover:text-text-primary transition-colors cursor-pointer">
          {copied ? `${t('copy')}!` : t('copy')}
        </button>
      </div>
      <div className="overflow-auto flex-1 min-h-0">
        <table className="w-full text-xs border-collapse">
          <thead className="sticky top-0 z-10">
            <tr className="bg-surface-tertiary">
              {header.map((col, i) => (
                <th key={i} className="px-3 py-2 text-left text-[11px] font-semibold tracking-wide border-b border-border whitespace-nowrap text-text-primary">
                  {col}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {body.map((row, i) => (
              <tr key={i} className="border-b border-border/40 last:border-0 even:bg-surface-tertiary/30 hover:bg-surface-tertiary/50">
                {header.map((_, j) => (
                  <td key={j} className="px-3 py-1.5 border-r border-border/30 last:border-r-0 text-text-primary">
                    {row[j] ?? ''}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// --- Image viewer (blob fetch → object URL) ---

function ImageViewer({ path, fetchBlob }: { path: string; fetchBlob: (path: string) => Promise<Blob> }) {
  const [src, setSrc] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  useEffect(() => {
    let objectUrl: string | null = null
    setLoading(true)
    setError(false)

    fetchBlob(path)
      .then((blob) => {
        objectUrl = URL.createObjectURL(blob)
        setSrc(objectUrl)
      })
      .catch(() => setError(true))
      .finally(() => setLoading(false))

    return () => {
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [path, fetchBlob])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <svg className="h-5 w-5 animate-spin text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
          <path d="M21 12a9 9 0 1 1-6.219-8.56" />
        </svg>
      </div>
    )
  }

  if (error || !src) {
    return (
      <div className="flex items-center justify-center py-12 text-xs text-text-muted">
        Failed to load image
      </div>
    )
  }

  return (
    <div className="flex items-center justify-center p-4">
      <img
        src={src}
        alt={path.split('/').pop() ?? ''}
        className="max-w-full max-h-[50vh] object-contain rounded-lg border border-border"
      />
    </div>
  )
}

// --- Unsupported file type ---

function UnsupportedViewer({ path, size, onDownload }: { path: string; size: number; onDownload?: () => void }) {
  const { t } = useTranslation('storage')
  const fileName = path.split('/').pop() ?? path

  return (
    <div className="flex flex-col items-center justify-center py-16 gap-4">
      <svg className="h-12 w-12 text-text-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="12" r="10" /><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3" /><line x1="12" y1="17" x2="12.01" y2="17" />
      </svg>
      <p className="text-xs text-text-muted">{t('unsupportedFile')}</p>
      {onDownload && (
        <button
          onClick={onDownload}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors"
        >
          <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline points="7 10 12 15 17 10" /><line x1="12" y1="15" x2="12" y2="3" />
          </svg>
          {fileName}
          <span className="text-[10px] text-text-muted">({formatSize(size)})</span>
        </button>
      )}
    </div>
  )
}

// --- FileContentBody (renders by type) ---

export function FileContentBody({
  path, content, size, fetchBlob, onDownload,
}: {
  path: string; content: string; size?: number
  fetchBlob?: (path: string) => Promise<Blob>; onDownload?: () => void
}) {
  const ext = extOf(path)

  if (isImageFile(path) && fetchBlob) {
    return <ImageViewer path={path} fetchBlob={fetchBlob} />
  }

  if (isTextFile(path) || ext === 'md' || ext === 'csv' || CODE_EXTENSIONS.has(ext)) {
    const displayContent = ext === 'md' ? stripFrontmatter(content) : content
    if (ext === 'md') return <MarkdownRenderer content={displayContent} />
    if (ext === 'csv') return <CsvViewer content={displayContent} />
    if (CODE_EXTENSIONS.has(ext)) return <CodeViewer content={displayContent} language={langFor(ext)} />
    return (
      <pre className="whitespace-pre-wrap rounded-lg border border-border bg-surface-tertiary/30 p-4 text-xs text-text-primary font-mono">
        {displayContent}
      </pre>
    )
  }

  return <UnsupportedViewer path={path} size={size ?? 0} onDownload={onDownload} />
}

// --- FileContentPanel (loading state + empty state wrapper) ---

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
