import { useTranslation } from 'react-i18next'
import { useUiStore, type ThemePreference } from '../../stores/ui-store'
import { IconMoon, IconSun } from '../common/Icons'
import { SegmentedControl } from '../ui'
import { SettingsCard, SettingsRow } from './settings-row'

export function AppearanceTab() {
  const { t } = useTranslation('settings')
  const theme = useUiStore((s) => s.theme)
  const setTheme = useUiStore((s) => s.setTheme)

  return (
    <SettingsCard title={t('appearance.themeTitle')}>
      <SettingsRow label={t('appearance.theme')} hint={t('appearance.themeHint')}>
        <SegmentedControl<ThemePreference>
          aria-label={t('appearance.theme')}
          value={theme}
          onValueChange={setTheme}
          options={[
            { value: 'system', label: t('appearance.system') },
            { value: 'light', label: t('appearance.light'), icon: <IconSun size={14} /> },
            { value: 'dark', label: t('appearance.dark'), icon: <IconMoon size={14} /> },
          ]}
        />
      </SettingsRow>
    </SettingsCard>
  )
}
