import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Switch } from '../common/Switch'
import { McpTestResult as McpTestResultDisplay } from './mcp-test-result'
import { McpTransportFields } from './mcp-transport-fields'
import { mcpFormSchema, type MCPFormData } from '../../schemas/mcp.schema'
import { slugify } from '../../lib/slug'
import type { MCPServerData, MCPServerInput, MCPTestResult } from '../../types/mcp'

const TRANSPORTS = ['stdio', 'sse', 'streamable-http'] as const

interface McpFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  server?: MCPServerData | null
  onSubmit: (data: MCPServerInput) => Promise<unknown>
  onTest: (data: { transport: string; command?: string; args?: string[]; url?: string; headers?: Record<string, string>; env?: Record<string, string> }) => Promise<MCPTestResult>
}

export function McpFormDialog({ open, onOpenChange, server, onSubmit, onTest }: McpFormDialogProps) {
  const { t } = useTranslation(['mcp', 'common'])
  const isEdit = Boolean(server)

  const { register, handleSubmit, watch, setValue, reset, formState: { errors, isSubmitting } } = useForm<MCPFormData>({
    resolver: zodResolver(mcpFormSchema),
    mode: 'onChange',
  })

  // Test connection state (UI-only, not form data)
  const [testState, setTestState] = useState<'idle' | 'testing' | 'success' | 'error'>('idle')
  const [testResult, setTestResult] = useState<MCPTestResult | null>(null)

  useEffect(() => {
    if (!open) return
    if (server) {
      reset({
        name: server.name,
        displayName: server.display_name || '',
        transport: server.transport,
        command: server.command || '',
        args: Array.isArray(server.args) ? server.args.join(' ') : '',
        url: server.url || '',
        headers: server.headers ?? {},
        env: server.env ?? {},
        toolPrefix: server.tool_prefix?.replace(/^mcp_/, '') ?? '',
        timeoutSec: server.timeout_sec || 30,
        enabled: server.enabled,
      })
    } else {
      reset({ name: '', displayName: '', transport: 'stdio', command: '', args: '', url: '', headers: {}, env: {}, toolPrefix: '', timeoutSec: 30, enabled: true })
    }
    setTestState('idle')
    setTestResult(null)
  }, [open, server, reset])

  if (!open) return null

  const transport = watch('transport')
  const headers = watch('headers')
  const env = watch('env')
  const name = watch('name')

  function buildInput(data: MCPFormData): MCPServerInput {
    const input: MCPServerInput = { name: data.name, display_name: data.displayName || undefined, transport: data.transport, timeout_sec: data.timeoutSec, enabled: data.enabled }
    if (data.transport === 'stdio') {
      input.command = data.command
      input.args = data.args.trim() ? data.args.trim().split(/\s+/) : undefined
    } else {
      input.url = data.url
      if (Object.keys(data.headers).length > 0) input.headers = data.headers
    }
    if (Object.keys(data.env).length > 0) input.env = data.env
    if (data.toolPrefix.trim()) input.tool_prefix = data.toolPrefix.trim()
    return input
  }

  const onValid = async (data: MCPFormData) => {
    await onSubmit(buildInput(data))
    onOpenChange(false)
  }

  async function handleTest() {
    setTestState('testing')
    setTestResult(null)
    try {
      const data: Parameters<typeof onTest>[0] = { transport }
      if (transport === 'stdio') {
        data.command = watch('command')
        const a = watch('args').trim()
        if (a) data.args = a.split(/\s+/)
      } else {
        data.url = watch('url')
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

  const canSubmit = watch('name').trim() && (transport === 'stdio' ? watch('command').trim() : watch('url').trim())

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
              onChange={(e) => setValue('name', slugify(e.target.value), { shouldValidate: true })}
              disabled={isEdit}
              placeholder="my-mcp-server"
              className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent disabled:opacity-50"
            />
            <p className="text-[11px] text-text-muted">{t('form.nameHint')}</p>
            {errors.name && <p className="text-xs text-error">{errors.name.message}</p>}
          </div>

          {/* Display Name */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.displayName')}</label>
            <input {...register('displayName')} placeholder={t('form.displayNamePlaceholder')} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent" />
          </div>

          {/* Transport */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.transport')}</label>
            <div className="grid grid-cols-3 gap-2">
              {TRANSPORTS.map((tr) => (
                <button key={tr} type="button" onClick={() => setValue('transport', tr)}
                  className={`border rounded-lg px-3 py-2 text-xs text-center transition-colors ${transport === tr ? 'border-accent bg-accent/10 text-accent font-medium' : 'border-border text-text-secondary hover:bg-surface-tertiary/30'}`}
                >{tr.toUpperCase()}</button>
              ))}
            </div>
          </div>

          <McpTransportFields
            transport={transport}
            headers={headers}
            env={env}
            register={register}
            setValue={setValue}
          />

          {/* Tool Prefix */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.toolPrefix')}</label>
            <div className="flex items-stretch">
              <span className="inline-flex items-center bg-surface-tertiary/70 border border-r-0 border-border rounded-l-lg px-2.5 text-base md:text-sm text-text-muted select-none">mcp_</span>
              <input
                value={watch('toolPrefix')}
                onChange={(e) => setValue('toolPrefix', e.target.value.replace(/[^a-z0-9_]/gi, '_').toLowerCase())}
                placeholder={name.replace(/-/g, '_') || 'auto'}
                className="flex-1 min-w-0 bg-surface-tertiary border border-border rounded-r-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
              />
            </div>
            <p className="text-[11px] text-text-muted">{t('form.toolPrefixHint')}</p>
          </div>

          {/* Timeout */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.timeout')}</label>
            <input type="number" min={1} {...register('timeoutSec', { valueAsNumber: true })} className="w-24 bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent" />
          </div>

          {/* Enabled */}
          <div className="flex items-center justify-between rounded-lg border border-border p-3">
            <span className="text-xs font-medium text-text-primary">{t('form.enabled')}</span>
            <Switch checked={watch('enabled')} onCheckedChange={(v) => setValue('enabled', v)} />
          </div>
        </div>

        {/* Footer */}
        <div className="border-t border-border px-5 py-4 space-y-2">
          <McpTestResultDisplay state={testState} result={testResult} />
          <div className="flex items-center justify-between">
            <button type="button" onClick={handleTest} disabled={isSubmitting || testState === 'testing'} className="border border-border rounded-lg px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-tertiary transition-colors disabled:opacity-50 shrink-0">
              {testState === 'testing' ? (
                <span className="flex items-center gap-1.5">
                  <svg className="h-3.5 w-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
                  {t('form.testing')}
                </span>
              ) : t('form.testConnection')}
            </button>
            <div className="flex items-center gap-2">
              <button type="button" onClick={() => onOpenChange(false)} className="border border-border rounded-lg px-4 py-1.5 text-sm text-text-secondary hover:bg-surface-tertiary transition-colors">{t('form.cancel')}</button>
              <button type="button" onClick={handleSubmit(onValid)} disabled={!canSubmit || isSubmitting} className="bg-accent rounded-lg px-4 py-1.5 text-sm text-white hover:bg-accent-hover disabled:opacity-50 transition-colors">
                {isSubmitting ? t('form.saving') : isEdit ? t('form.update') : t('form.create')}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
