import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneDark, oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { useResolvedTheme } from '../layout/use-resolved-theme'

interface CodeBlockProps {
  language: string
  code: string
}

export function CodeBlock({ language, code }: CodeBlockProps) {
  const { t } = useTranslation('common')
  const theme = useResolvedTheme()
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(code)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="mb-3 overflow-hidden rounded-md border border-border-subtle">
      <div className="flex items-center justify-between bg-surface-hover px-3 py-1.5 text-caption">
        <span className="font-mono text-ink-muted">{language}</span>
        <button
          type="button"
          onClick={handleCopy}
          className="rounded-sm text-ink-muted transition-colors hover:text-ink"
        >
          {copied ? `${t('copy')}!` : t('copy')}
        </button>
      </div>
      <SyntaxHighlighter
        language={language}
        style={theme === 'dark' ? oneDark : oneLight}
        customStyle={{
          margin: 0,
          padding: '12px 16px',
          fontSize: '13px',
          background: 'var(--color-surface-window)',
          borderRadius: 0,
        }}
      >
        {code}
      </SyntaxHighlighter>
    </div>
  )
}
