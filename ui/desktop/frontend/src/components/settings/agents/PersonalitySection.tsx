import { useTranslation } from 'react-i18next'
import { Switch } from '../../common/Switch'
import { Combobox } from '../../common/Combobox'

interface PersonalitySectionProps {
  emoji: string
  displayName: string
  description: string
  agentKey: string
  agentType: string
  isDefault: boolean
  status: string
  onEmojiChange: (v: string) => void
  onDisplayNameChange: (v: string) => void
  onDescriptionChange: (v: string) => void
  onIsDefaultChange: (v: boolean) => void
  onStatusChange: (v: string) => void
}

export function PersonalitySection({
  emoji, displayName, description, agentKey, agentType, isDefault, status,
  onEmojiChange, onDisplayNameChange, onDescriptionChange, onIsDefaultChange, onStatusChange,
}: PersonalitySectionProps) {
  const { t } = useTranslation('agents')
  const STATUS_OPTIONS = [
    { value: 'active', label: t('identity.active') },
    { value: 'idle', label: t('identity.inactive') },
  ]
  return (
    <div className="space-y-4">
      <h3 className="text-sm font-semibold text-text-primary">{t('detail.personality')}</h3>

      {/* Emoji + Display name */}
      <div className="grid grid-cols-[auto_1fr] gap-4 items-start">
        <div className="space-y-1 text-center">
          <input
            value={emoji}
            onChange={(e) => onEmojiChange(e.target.value.slice(0, 2))}
            maxLength={2}
            className="w-12 h-12 rounded-xl bg-accent/10 border border-border text-2xl text-center focus:outline-none focus:ring-1 focus:ring-accent"
          />
          <p className="text-[10px] text-text-muted">{t('identity.emoji')}</p>
        </div>
        <div className="space-y-3">
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('identity.displayName')}</label>
            <input
              value={displayName}
              onChange={(e) => onDisplayNameChange(e.target.value)}
              placeholder="Agent name"
              className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary font-medium placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
          {/* Agent key (read-only) */}
          <div className="flex items-center gap-2">
            <span className="text-[11px] text-text-muted font-mono bg-surface-tertiary/50 px-2 py-1 rounded">{agentKey}</span>
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-surface-tertiary text-text-muted">{agentType}</span>
          </div>
        </div>
      </div>

      {/* Description / Expertise */}
      <div className="space-y-1">
        <label className="text-xs font-medium text-text-secondary">
          {agentType === 'predefined' ? t('detail.personality') : t('identity.expertiseSummary')}
        </label>
        <textarea
          value={description}
          onChange={(e) => onDescriptionChange(e.target.value)}
          placeholder={agentType === 'predefined' ? t('create.descriptionPlaceholder') : t('identity.expertiseSummaryPlaceholder')}
          rows={4}
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent resize-none"
        />
      </div>

      {/* Status + Default */}
      <div className="grid grid-cols-2 gap-4">
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('identity.status')}</label>
          <Combobox value={status} onChange={onStatusChange} options={STATUS_OPTIONS} placeholder={t('identity.status')} />
        </div>
        <div className="flex items-end pb-1">
          <div className="flex items-center gap-2">
            <Switch checked={isDefault} onCheckedChange={onIsDefaultChange} />
            <span className="text-xs text-text-secondary">{t('identity.defaultAgent')}</span>
          </div>
        </div>
      </div>
    </div>
  )
}
