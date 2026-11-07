import { useTranslation } from 'react-i18next'
import { Field, Input, Switch, Textarea } from '../ui'
import { SectionCard } from './section-card'

/** Longest personality text the editor accepts. */
export const DESCRIPTION_MAX = 2000

interface IdentitySectionProps {
  emoji: string
  displayName: string
  isDefault: boolean
  status: string
  displayNameError?: string
  readOnly?: boolean
  onEmojiChange: (v: string) => void
  onDisplayNameChange: (v: string) => void
  onIsDefaultChange: (v: boolean) => void
  onStatusChange: (v: string) => void
}

/** Name, emoji, and the enabled and default switches. */
export function IdentitySection({
  emoji, displayName, isDefault, status, displayNameError, readOnly = false,
  onEmojiChange, onDisplayNameChange, onIsDefaultChange, onStatusChange,
}: IdentitySectionProps) {
  const { t } = useTranslation('agents')
  const statusLocked = status === 'summoning' || status === 'summon_failed'
  return (
    <SectionCard title={t('screen.detail.identity')}>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_120px]">
        <Field label={t('identity.displayName')} required error={displayNameError}>
          <Input value={displayName} onChange={(e) => onDisplayNameChange(e.target.value)} placeholder={t('create.displayNamePlaceholder')} disabled={readOnly} />
        </Field>
        <Field label={t('identity.emoji')}>
          <Input value={emoji} onChange={(e) => onEmojiChange(e.target.value.slice(0, 2))} maxLength={2} className="text-center" disabled={readOnly} />
        </Field>
      </div>
      <div className="flex flex-col gap-3">
        <SwitchRow
          label={t('screen.detail.enabled')}
          hint={t('screen.detail.enabledHint')}
          checked={status === 'active'}
          disabled={readOnly || statusLocked}
          onChange={(v) => onStatusChange(v ? 'active' : 'idle')}
        />
        <SwitchRow
          label={t('screen.detail.defaultAgent')}
          hint={t('screen.detail.defaultAgentHint')}
          checked={isDefault}
          disabled={readOnly}
          onChange={onIsDefaultChange}
        />
      </div>
    </SectionCard>
  )
}

function SwitchRow({ label, hint, checked, disabled, onChange }: {
  label: string; hint: string; checked: boolean; disabled?: boolean; onChange: (v: boolean) => void
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div className="min-w-0">
        <p className="text-ui text-ink">{label}</p>
        <p className="text-caption font-normal text-ink-muted">{hint}</p>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} aria-label={label} />
    </div>
  )
}

interface PersonalitySectionProps {
  description: string
  agentType: string
  error?: string
  readOnly?: boolean
  onDescriptionChange: (v: string) => void
}

/** Persona text (predefined agents) or expertise summary (open agents). */
export function PersonalitySection({ description, agentType, error, readOnly = false, onDescriptionChange }: PersonalitySectionProps) {
  const { t } = useTranslation('agents')
  const predefined = agentType === 'predefined'
  return (
    <SectionCard
      title={predefined ? t('screen.detail.personality') : t('identity.expertiseSummary')}
      subtitle={predefined ? t('screen.detail.personalityHint') : t('screen.detail.descriptionHint')}
    >
      <Field label={predefined ? t('screen.detail.personality') : t('identity.expertiseSummary')} error={error}>
        <Textarea
          value={description}
          onChange={(e) => onDescriptionChange(e.target.value)}
          placeholder={predefined ? t('create.descriptionPlaceholder') : t('identity.expertiseSummaryPlaceholder')}
          rows={6}
          disabled={readOnly}
        />
      </Field>
      <p className="-mt-2 self-end text-caption font-normal text-ink-muted" aria-live="polite">
        {t('screen.detail.descriptionCount', { count: description.length, max: DESCRIPTION_MAX })}
      </p>
    </SectionCard>
  )
}
