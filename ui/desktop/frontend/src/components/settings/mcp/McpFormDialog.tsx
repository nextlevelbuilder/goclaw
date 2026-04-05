import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Switch } from '../../common/Switch'
import { KeyValueEditor } from '../../common/KeyValueEditor'
import type { MCPServerData, MCPServerInput, MCPTestResult } from '../../../types/mcp'

const SENSITIVE_HEADER_KEYS = /(auth|api-key|api_key|bearer|token|secret|password|credential)/i
const SENSITIVE_ENV_KEYS = /(key|secret|token|password|credential)/i

const TRANSPORTS = ['stdio', 'sse', 'streamable-http'] as const

interface McpFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  server?: MCPServerData | null
  onSubmit: (data: MCPServerInput) => Promise<unknown>
  onTest: (data: { transport: string; command?: string; args?: string[]; url?: string; headers?: Record<string, string>; env?: Record<string, string> }) => Promise<MCPTestResult>
}

function slugify(v: string): string {
  return v.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').replace(/^-/, '')
}

export function McpFormDialog({ open, onOpenChange, server, onSubmit, onTest }: McpFormDialogProps) {
  const { t } = useTranslation(['mcp', 'common'])
  const isEdit = Boolean(server)

  const [name, setName] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [transport, setTransport] = useState<string>('stdio')
  const [command, setCommand] = useState('')
  const [args, setArgs] = useState('')
  const [url, setUrl] = useState('')
  const [headers, setHeaders] = useState<Record<string, string>>({})
  const [env, setEnv] = useState<Record<string, string>>({})
  const [toolPrefix, setToolPrefix] = useState('')
  const [timeoutSec, setTimeoutSec] = useState(30)
  const [enabled, setEnabled] = useState(true)
  const [saving, setSaving] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const [testState, setTestState] = useState<'idle' | 'testing' | 'success' | 'error'>('idle')
  const [testResult, setTestResult] = useState<MCPTestResult | null>(null)

  useEffect(() => {
    if (!open) return
    if (server) {
      setName(server.name)
      setDisplayName(server.display_name || '')
      setTransport(server.transport)
      setCommand(server.command || '')
      setArgs(Array.isArray(server.args) ? server.args.join(' ') : '')
      setUrl(server.url || '')
      setHeaders(server.headers ?? {})
      setEnv(server.env ?? {})
      setToolPrefix(server.tool_prefix?.replace(/^mcp_/, '') ?? '')
      setTimeoutSec(server.timeout_sec || 30)
      setEnabled(server.enabled)
    } else {
      setName('')
      setDisplayName('')
      setTransport('stdio')
      setCommand('')
      setArgs('')
      setUrl('')
      setHeaders({})
      setEnv({})
      setToolPrefix('')
      setTimeoutSec(30)
      setEnabled(true)
    }
    setTestState('idle')
    setTestResult(null)
    setSaving(false)
    setSubmitError('')
  }, [open, server])

  if (!open) return null

  function buildInput(): MCPServerInput {
    const input: MCPServerInput = {
      name,
      display_name: displayName || undefined,
      transport,
      timeout_sec: timeoutSec,
      enabled,
    }
    if (transport === 'stdio') {
      input.command = command
      input.args = args.trim() ? args.trim().split(/\s+/) : undefined
    } else {
      input.url = url
      if (Object.keys(headers).length > 0) input.headers = headers
    }
    if (Object.keys(env).length > 0) input.env = env
    if (toolPrefix.trim()) input.tool_prefix = toolPrefix.trim()
    return input
  }

  async function handleSubmit() {
    setSaving(true)
    setSubmitError('')
    try {
      await onSubmit(buildInput())
      onOpenChange(false)
    } catch (err) {
      setSubmitError((err as Error).message || 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  async function handleTest() {
    setTestState('testing')
    setTestResult(null)
    try {
      const data: Parameters<typeof onTest>[0] = { transport }
      if (transport === 'stdio') {
        data.command = command
        if (args.trim()) data.args = args.trim().split(/\s+/)
      } else {
        data.url = url
        if (Object.keys(headers).length > 0) data.headers = headers
      }
      if (Object.keys(env).length > 0) data.env = env
      const result = await onTest(data)
      setTestResult(result)
      setTestState(result.success ? 'success' : 'error')
    } catch (err) {
      setTestResult({ success: false, error: (err as Error).message })
      setTestState('error')
    }
  }

  const canSubmit = name.trim() && (transport === 'stdio' ? command.trim() : url.trim()) && !saving

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={() => onOpenChange(false)} />
      <div className="relative w-full max-w-lg bg-surface-secondary rounded-xl border border-border overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <span className="text-sm font-semibold text-text-primary">{isEdit ? t('form.editTitle') : t('form.createTitle')}</span>
          <button onClick={() => onOpenChange(false)} className="p-1 text-text-muted hover:text-text-primary transition-colors">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>

        {/* Form */}
        <div className="max-h-[70vh] overflow-y-auto p-5 space-y-4">
          {/* Name */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.name')}</label>
            <input
              value={name}
              onChange={(e) => setName(slugify(e.target.value))}
              disabled={isEdit}
              placeholder="my-mcp-server"
              className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent disabled:opacity-50"
            />
            <p className="text-[11px] text-text-muted">{t('form.nameHint')}</p>
          </div>

          {/* Display Name */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.displayName')}</label>
            <input
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder={t('form.displayNamePlaceholder')}
              className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>

          {/* Transport */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.transport')}</label>
            <div className="grid grid-cols-3 gap-2">
              {TRANSPORTS.map((t) => (
                <button
                  key={t}
                  type="button"
                  onClick={() => setTransport(t)}
                  className={`border rounded-lg px-3 py-2 text-xs text-center transition-colors ${
                    transport === t
                      ? 'border-accent bg-accent/10 text-accent font-medium'
                      : 'border-border text-text-secondary hover:bg-surface-tertiary/30'
                  }`}
                >
                  {t.toUpperCase()}
                </button>
              ))}
            </div>
          </div>

          {/* Stdio fields */}
          {transport === 'stdio' && (
            <>
              <div className="space-y-1">
                <label className="text-xs font-medium text-text-secondary">{t('form.command')}</label>
                <input
                  value={command}
                  onChange={(e) => setCommand(e.target.value)}
                  placeholder="npx"
                  className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 font-mono text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs font-medium text-text-secondary">{t('form.args')}</label>
                <input
                  value={args}
                  onChange={(e) => setArgs(e.target.value)}
                  placeholder={t('form.argsPlaceholder')}
                  className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 font-mono text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
                />
              </div>
            </>
          )}

          {/* SSE/HTTP fields */}
          {transport !== 'stdio' && (
            <>
              <div className="space-y-1">
                <label className="text-xs font-medium text-text-secondary">{t('form.url')}</label>
                <input
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder="http://localhost:3001/sse"
                  className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 font-mono text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs font-medium text-text-secondary">{t('form.headers')}</label>
                <KeyValueEditor
                  value={headers}
                  onChange={setHeaders}
                  sensitivePattern={SENSITIVE_HEADER_KEYS}
                  placeholder={{ key: t('form.headerKeyPlaceholder'), value: t('form.headerValuePlaceholder') }}
                />
              </div>
            </>
          )}

          {/* Env Variables */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.env')}</label>
            <KeyValueEditor
              value={env}
              onChange={setEnv}
              sensitivePattern={SENSITIVE_ENV_KEYS}
              placeholder={{ key: t('form.envKeyPlaceholder'), value: t('form.envValuePlaceholder') }}
            />
          </div>

          {/* Tool Prefix */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.toolPrefix')}</label>
            <div className="flex items-stretch">
              <span className="inline-flex items-center bg-surface-tertiary/70 border border-r-0 border-border rounded-l-lg px-2.5 text-base md:text-sm text-text-muted select-none">mcp_</span>
              <input
                value={toolPrefix}
                onChange={(e) => setToolPrefix(e.target.value.replace(/[^a-z0-9_]/gi, '_').toLowerCase())}
                placeholder={name.replace(/-/g, '_') || 'auto'}
                className="flex-1 min-w-0 bg-surface-tertiary border border-border rounded-r-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
              />
            </div>
            <p className="text-[11px] text-text-muted">{t('form.toolPrefixHint')}</p>
          </div>

          {/* Timeout */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.timeout')}</label>
            <input
              type="number"
              min={1}
              value={timeoutSec}
              onChange={(e) => setTimeoutSec(Math.max(1, Number(e.target.value)))}
              className="w-24 bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>

          {/* Enabled */}
          <div className="flex items-center justify-between rounded-lg border border-border p-3">
            <span className="text-xs font-medium text-text-primary">{t('form.enabled')}</span>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </div>
        </div>

        {/* Error */}
        {submitError && (
          <div className="px-5"><p className="text-xs text-error">{submitError}</p></div>
        )}

        {/* Footer */}
        <div className="border-t border-border px-5 py-4 space-y-2">
          {/* Test result (above buttons so it doesn't break layout) */}
          {testState === 'success' && testResult && (
            <p className="text-[11px] text-emerald-600 dark:text-emerald-400 flex items-center gap-1">
              <svg className="h-3.5 w-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                <path d="M20 6 9 17l-5-5" />
              </svg>
              {t('form.toolsFound', { count: testResult.tool_count })}
            </p>
          )}
          {testState === 'error' && testResult && (
            <p className="text-[11px] text-error flex items-center gap-1">
              <svg className="h-3.5 w-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                <path d="M18 6 6 18" /><path d="m6 6 12 12" />
              </svg>
              <span className="break-all">{testResult.error || t('form.errors.connectionFailed')}</span>
            </p>
          )}

          <div className="flex items-center justify-between">
          <button
            type="button"
            onClick={handleTest}
            disabled={saving || testState === 'testing'}
            className="border border-border rounded-lg px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-tertiary transition-colors disabled:opacity-50 shrink-0"
          >
            {testState === 'testing' ? (
              <span className="flex items-center gap-1.5">
                <svg className="h-3.5 w-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
                {t('form.testing')}
              </span>
            ) : t('form.testConnection')}
          </button>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => onOpenChange(false)}
              className="border border-border rounded-lg px-4 py-1.5 text-sm text-text-secondary hover:bg-surface-tertiary transition-colors"
            >
              {t('form.cancel')}
            </button>
            <button
              type="button"
              onClick={handleSubmit}
              disabled={!canSubmit}
              className="bg-accent rounded-lg px-4 py-1.5 text-sm text-white hover:bg-accent-hover disabled:opacity-50 transition-colors"
            >
              {saving ? t('form.saving') : isEdit ? t('form.update') : t('form.create')}
            </button>
          </div>
          </div>
        </div>
      </div>
    </div>
  )
}
