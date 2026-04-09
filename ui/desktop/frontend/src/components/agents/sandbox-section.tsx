import { useTranslation } from 'react-i18next'
import { ConfigSection } from './config-section'
import { Switch } from '../common/Switch'
import { numOrUndef } from '../../lib/format'
import type { SandboxConfig } from '../../types/agent'

interface SandboxSectionProps {
  enabled: boolean
  value: SandboxConfig
  onToggle: (v: boolean) => void
  onChange: (v: SandboxConfig) => void
}

export function SandboxSection({ enabled, value, onToggle, onChange }: SandboxSectionProps) {
  const { t } = useTranslation('agents')
  const update = (patch: Partial<SandboxConfig>) => onChange({ ...value, ...patch })

  const selectCls = 'w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent'
  const inputCls = selectCls

  return (
    <ConfigSection
      title={t('configSections.sandbox.title')}
      description={t('configSections.sandbox.description')}
      enabled={enabled}
      onToggle={onToggle}
    >
      <div className="rounded-lg border border-amber-500/20 bg-amber-500/5 p-2.5">
        <p className="text-[11px] text-amber-600 dark:text-amber-400">
          Requires Docker installed locally. Sandbox containers run on this machine.
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <label className="text-[11px] font-medium text-text-secondary">{t('configSections.sandbox.mode')}</label>
          <select value={value.mode ?? 'off'} onChange={(e) => update({ mode: e.target.value as SandboxConfig['mode'] })} className={selectCls}>
            <option value="off">off</option>
            <option value="non-main">non-main</option>
            <option value="all">all</option>
          </select>
        </div>
        <div className="space-y-1">
          <label className="text-[11px] font-medium text-text-secondary">{t('configSections.sandbox.workspaceAccess')}</label>
          <select value={value.workspace_access ?? 'none'} onChange={(e) => update({ workspace_access: e.target.value as SandboxConfig['workspace_access'] })} className={selectCls}>
            <option value="none">none</option>
            <option value="ro">ro (read-only)</option>
            <option value="rw">rw (read-write)</option>
          </select>
        </div>
      </div>

      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t('configSections.sandbox.image')}</label>
        <input
          type="text"
          value={value.image ?? ''}
          placeholder="goclaw-sandbox:bookworm-slim"
          onChange={(e) => update({ image: e.target.value || undefined })}
          className={`${inputCls} font-mono`}
        />
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <label className="text-[11px] font-medium text-text-secondary">{t('configSections.sandbox.scope')}</label>
          <select value={value.scope ?? 'session'} onChange={(e) => update({ scope: e.target.value as SandboxConfig['scope'] })} className={selectCls}>
            <option value="session">session</option>
            <option value="agent">agent</option>
            <option value="shared">shared</option>
          </select>
        </div>
        <div className="space-y-1">
          <label className="text-[11px] font-medium text-text-secondary">{t('configSections.sandbox.timeout')}</label>
          <input type="number" min={0} value={value.timeout_sec ?? ''} onChange={(e) => update({ timeout_sec: numOrUndef(e.target.value) })} className={inputCls} />
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <label className="text-[11px] font-medium text-text-secondary">{t('configSections.sandbox.memoryMb')}</label>
          <input type="number" min={0} value={value.memory_mb ?? ''} onChange={(e) => update({ memory_mb: numOrUndef(e.target.value) })} className={inputCls} />
        </div>
        <div className="space-y-1">
          <label className="text-[11px] font-medium text-text-secondary">{t('configSections.sandbox.cpus')}</label>
          <input type="number" min={0} step={0.5} value={value.cpus ?? ''} onChange={(e) => update({ cpus: numOrUndef(e.target.value) })} className={inputCls} />
        </div>
      </div>

      <div className="flex items-center justify-between rounded-lg border border-border p-2.5">
        <span className="text-xs text-text-secondary">{t('configSections.sandbox.networkEnabled')}</span>
        <Switch
          checked={value.network_enabled ?? false}
          onCheckedChange={(v) => update({ network_enabled: v })}
        />
      </div>
    </ConfigSection>
  )
}
