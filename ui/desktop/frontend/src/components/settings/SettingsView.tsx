import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { useUiStore } from '../../stores/ui-store'
import { PageHeader } from '../ui'
import { AboutTab } from './AboutTab'
import { AppearanceTab } from './AppearanceTab'
import { GatewayTab } from './GatewayTab'
import { GeneralTab } from './GeneralTab'
import { SecurityTab } from './SecurityTab'

const SECTIONS = ['general', 'appearance', 'gateway', 'security', 'about'] as const
type Section = (typeof SECTIONS)[number]

const PANELS: Record<Section, () => React.JSX.Element> = {
  general: GeneralTab,
  appearance: AppearanceTab,
  gateway: GatewayTab,
  security: SecurityTab,
  about: AboutTab,
}

/** Settings: a section list on the left and the chosen section on the right. */
export function SettingsView() {
  const { t } = useTranslation('settings')
  const storedTab = useUiStore((s) => s.settingsTab)
  const setSettingsTab = useUiStore((s) => s.setSettingsTab)
  const [section, setSection] = useState<Section>(storedTab === 'about' ? 'about' : 'appearance')

  // openSettings('about') from elsewhere switches the section while this view is mounted.
  useEffect(() => {
    if (storedTab === 'about' || storedTab === 'appearance') setSection(storedTab)
  }, [storedTab])

  const choose = (next: Section) => {
    setSection(next)
    if (next === 'about' || next === 'appearance') setSettingsTab(next)
  }

  const Panel = PANELS[section]

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader title={t('title')} description={t('description')} />
      <div className="flex min-h-0 flex-1 flex-col md:flex-row">
        <nav
          aria-label={t('nav.label')}
          className="flex shrink-0 gap-0.5 overflow-x-auto border-b border-border-subtle p-3 md:w-[200px] md:flex-col md:border-b-0 md:border-r"
        >
          {SECTIONS.map((id) => (
            <button
              key={id}
              type="button"
              aria-current={section === id ? 'page' : undefined}
              onClick={() => choose(id)}
              className={cn(
                'flex h-8 shrink-0 items-center rounded-sm px-2.5 text-left text-ui font-medium transition-colors',
                section === id
                  ? 'bg-accent-subtle text-accent-subtle-ink'
                  : 'text-ink-secondary hover:bg-surface-hover hover:text-ink',
              )}
            >
              {t(`nav.${id}`)}
            </button>
          ))}
        </nav>
        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-5">
          <div className="flex max-w-[640px] flex-col gap-6">
            <Panel />
          </div>
        </div>
      </div>
    </div>
  )
}
