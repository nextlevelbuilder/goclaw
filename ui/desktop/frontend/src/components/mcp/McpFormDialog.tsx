import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Button, Dialog, DialogFooter, Field, Input, SegmentedControl, Switch } from '../ui'
import { McpTestResult as McpTestResultDisplay } from './mcp-test-result'
import { McpTransportFields } from './mcp-transport-fields'
import { mcpFormSchema, type MCPFormData } from '../../schemas/mcp.schema'
import { slugify } from '../../lib/slug'
import type { MCPServerData, MCPServerInput, MCPTestResult } from '../../types/mcp'

const TRANSPORTS = ['stdio', 'sse', 'streamable-http'] as const
const TRANSPORT_LABEL_KEY = { stdio: 'stdio', sse: 'sse', 'streamable-http': 'http' } as const
const SLUG_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

const EMPTY_FORM: MCPFormData = {
  name: '', displayName: '', transport: 'stdio', command: '', args: '', url: '',
  headers: {}, env: {}, toolPrefix: '', timeoutSec: 30, enabled: true,
}

interface McpFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  server?: MCPServerData | null
  onSubmit: (data: MCPServerInput) => Promise<unknown>
  onTest: (data: { transport: string; command?: string; args?: string[]; url?: string; headers?: Record<string, string>; env?: Record<string, string> }) => Promise<MCPTestResult>
  /** Disables writes while the gateway is unreachable. */
  readOnly?: boolean
  /** Shown on the left of the footer when editing. */
  onDelete?: () => void
}

export function McpFormDialog({ open, onOpenChange, server, onSubmit, onTest, readOnly = false, onDelete }: McpFormDialogProps) {
  const { t } = useTranslation(['mcp', 'common'])
  const isEdit = Boolean(server)

  const { register, handleSubmit, watch, setValue, reset, formState: { isSubmitting } } = useForm<MCPFormData>({
    resolver: zodResolver(mcpFormSchema),
    mode: 'onChange',
    defaultValues: EMPTY_FORM,
  })

  const [attempted, setAttempted] = useState(false)
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
      reset(EMPTY_FORM)
    }
    setAttempted(false)
    setTestState('idle')
    setTestResult(null)
  }, [open, server, reset])

  const transport = watch('transport')
  const headers = watch('headers')
  const env = watch('env')
  const name = watch('name')
  const command = watch('command')
  const url = watch('url')

  const nameError = !attempted ? undefined
    : !name.trim() ? t('form.errors.nameRequired')
    : !SLUG_RE.test(name) ? t('form.errors.nameSlug') : undefined
  const commandError = attempted && transport === 'stdio' && !command.trim() ? t('form.errors.commandRequired') : undefined
  const urlError = attempted && transport !== 'stdio' && !url.trim() ? t('form.errors.urlRequired') : undefined

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
    setAttempted(true)
    if (!data.name.trim() || !SLUG_RE.test(data.name)) return
    if (data.transport === 'stdio' ? !data.command.trim() : !data.url.trim()) return
    try {
      await onSubmit(buildInput(data))
      onOpenChange(false)
    } catch {
      // The hook already reported the failure; keep the dialog open.
    }
  }

  async function handleTest() {
    setTestState('testing')
    setTestResult(null)
    try {
      const data: Parameters<typeof onTest>[0] = { transport }
      if (transport === 'stdio') {
        data.command = command
        const a = watch('args').trim()
        if (a) data.args = a.split(/\s+/)
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

  const canTest = transport === 'stdio' ? command.trim() : url.trim()

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={isEdit ? t('form.editTitle') : t('form.createTitle')}
      dismissible={!isSubmitting}
      footer={
        <DialogFooter
          destructive={isEdit && onDelete ? (
            <Button variant="danger" onClick={onDelete} disabled={readOnly || isSubmitting}>{t('screen.menu.delete')}</Button>
          ) : undefined}
        >
          <Button onClick={() => onOpenChange(false)} disabled={isSubmitting}>{t('form.cancel')}</Button>
          <Button
            variant="primary"
            onClick={() => { setAttempted(true); void handleSubmit(onValid)() }}
            loading={isSubmitting}
            disabled={readOnly}
          >
            {isSubmitting ? t('form.saving') : isEdit ? t('form.update') : t('form.create')}
          </Button>
        </DialogFooter>
      }
    >
      <fieldset disabled={readOnly} className="flex min-w-0 flex-col gap-4 border-0 p-0">
        <Field label={t('form.name')} helper={t('form.nameHint')} required error={nameError}>
          <Input
            value={name}
            onChange={(e) => setValue('name', slugify(e.target.value), { shouldValidate: true })}
            disabled={isEdit}
            placeholder="my-mcp-server"
            className="font-mono"
          />
        </Field>

        <Field label={t('form.displayName')}>
          <Input {...register('displayName')} placeholder={t('form.displayNamePlaceholder')} />
        </Field>

        <div className="flex flex-col gap-1.5">
          <span className="text-ui font-medium text-ink">{t('form.transport')}</span>
          <SegmentedControl
            aria-label={t('form.transport')}
            value={transport}
            onValueChange={(v) => setValue('transport', v)}
            options={TRANSPORTS.map((tr) => ({ value: tr, label: t(`screen.transport.${TRANSPORT_LABEL_KEY[tr]}`) }))}
          />
        </div>

        <McpTransportFields
          transport={transport}
          headers={headers}
          env={env}
          register={register}
          setValue={setValue}
          errors={{ command: commandError, url: urlError }}
          readOnly={readOnly}
        />

        <Field label={t('form.toolPrefix')} helper={t('form.toolPrefixHint')}>
          <div className="flex items-stretch">
            <span className="inline-flex items-center rounded-l-sm border border-r-0 border-border-control bg-surface-hover px-2.5 font-mono text-code text-ink-muted select-none">mcp_</span>
            <Input
              value={watch('toolPrefix')}
              onChange={(e) => setValue('toolPrefix', e.target.value.replace(/[^a-z0-9_]/gi, '_').toLowerCase())}
              placeholder={name.replace(/-/g, '_') || t('form.toolPrefixAuto')}
              className="min-w-0 flex-1 rounded-l-none font-mono"
            />
          </div>
        </Field>

        <Field label={t('form.timeout')}>
          <Input type="number" min={1} {...register('timeoutSec', { valueAsNumber: true })} className="w-28 font-mono" />
        </Field>

        <div className="flex items-center justify-between gap-4 rounded-lg border border-border-subtle p-3">
          <span className="text-ui text-ink">{t('form.enabled')}</span>
          <Switch checked={watch('enabled')} onCheckedChange={(v) => setValue('enabled', v)} aria-label={t('form.enabled')} />
        </div>

        <div className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-surface-base p-3">
          <div className="flex items-center gap-3">
            <Button size="sm" onClick={handleTest} loading={testState === 'testing'} disabled={!canTest || isSubmitting}>
              {testState === 'testing' ? t('form.testing') : t('form.testConnection')}
            </Button>
          </div>
          <McpTestResultDisplay state={testState} result={testResult} />
        </div>
      </fieldset>
    </Dialog>
  )
}
