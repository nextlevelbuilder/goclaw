import { useTranslation } from 'react-i18next'
import { Switch } from '../ui'

interface ConfigSectionProps {
  title: string
  description: string
  enabled: boolean
  onToggle: (v: boolean) => void
  children: React.ReactNode
}

export function ConfigSection({ title, description, enabled, onToggle, children }: ConfigSectionProps) {
  const { t } = useTranslation('agents')

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <div className="space-y-0.5">
          <h4 className="text-caption font-semibold text-ink">{title}</h4>
          <p className="text-caption text-ink-muted">{description}</p>
        </div>
        <Switch checked={enabled} onCheckedChange={onToggle} />
      </div>
      {enabled ? (
        <div className="space-y-3 pl-1">{children}</div>
      ) : (
        <p className="text-caption text-ink-muted italic">{t('config.usingGlobalDefaults')}</p>
      )}
    </div>
  )
}
