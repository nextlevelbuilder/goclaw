import { useTranslation } from 'react-i18next'
import { ConfigSection } from './config-section'
import { Switch } from '../ui'
import { numOrUndef } from '../../lib/format'
import type { SandboxConfig } from '../../types/agent'
import { Input, Select } from '../ui'

interface SandboxSectionProps {
  enabled: boolean
  value: SandboxConfig
  onToggle: (v: boolean) => void
  onChange: (v: SandboxConfig) => void
}

export function SandboxSection({ enabled, value, onToggle, onChange }: SandboxSectionProps) {
  const { t } = useTranslation('agents')
  const update = (patch: Partial<SandboxConfig>) => onChange({ ...value, ...patch })

  const selectCls = 'w-full bg-surface-hover border border-border-subtle rounded-lg px-3 py-2 text-base md:text-ui text-ink focus:outline-none focus:ring-1 focus:ring-accent'
  const inputCls = selectCls

  return (
    <ConfigSection
      title={t('configSections.sandbox.title')}
      description={t('configSections.sandbox.description')}
      enabled={enabled}
      onToggle={onToggle}
    >
      <div className="rounded-lg border border-border-subtle bg-warning-subtle p-2.5">
        <p className="text-caption text-warning">
          Requires Docker installed locally. Sandbox containers run on this machine.
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.sandbox.mode')}</label>
          <Select value={value.mode ?? 'off'} onChange={(e) => update({ mode: e.target.value as SandboxConfig['mode'] })} className={selectCls}>
            <option value="off">off</option>
            <option value="non-main">non-main</option>
            <option value="all">all</option>
          </Select>
        </div>
        <div className="space-y-1">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.sandbox.workspaceAccess')}</label>
          <Select value={value.workspace_access ?? 'none'} onChange={(e) => update({ workspace_access: e.target.value as SandboxConfig['workspace_access'] })} className={selectCls}>
            <option value="none">none</option>
            <option value="ro">ro (read-only)</option>
            <option value="rw">rw (read-write)</option>
          </Select>
        </div>
      </div>

      <div className="space-y-1">
        <label className="text-caption font-medium text-ink-secondary">{t('configSections.sandbox.image')}</label>
        <Input
          type="text"
          value={value.image ?? ''}
          placeholder="goclaw-sandbox:bookworm-slim"
          onChange={(e) => update({ image: e.target.value || undefined })}
          className={`${inputCls} font-mono`}
        />
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.sandbox.scope')}</label>
          <Select value={value.scope ?? 'session'} onChange={(e) => update({ scope: e.target.value as SandboxConfig['scope'] })} className={selectCls}>
            <option value="session">session</option>
            <option value="agent">agent</option>
            <option value="shared">shared</option>
          </Select>
        </div>
        <div className="space-y-1">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.sandbox.timeout')}</label>
          <Input type="number" min={0} value={value.timeout_sec ?? ''} onChange={(e) => update({ timeout_sec: numOrUndef(e.target.value) })} className={inputCls} />
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.sandbox.memoryMb')}</label>
          <Input type="number" min={0} value={value.memory_mb ?? ''} onChange={(e) => update({ memory_mb: numOrUndef(e.target.value) })} className={inputCls} />
        </div>
        <div className="space-y-1">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.sandbox.cpus')}</label>
          <Input type="number" min={0} step={0.5} value={value.cpus ?? ''} onChange={(e) => update({ cpus: numOrUndef(e.target.value) })} className={inputCls} />
        </div>
      </div>

      <div className="flex items-center justify-between rounded-lg border border-border-subtle p-2.5">
        <span className="text-caption text-ink-secondary">{t('configSections.sandbox.networkEnabled')}</span>
        <Switch
          checked={value.network_enabled ?? false}
          onCheckedChange={(v) => update({ network_enabled: v })}
        />
      </div>
    </ConfigSection>
  )
}
