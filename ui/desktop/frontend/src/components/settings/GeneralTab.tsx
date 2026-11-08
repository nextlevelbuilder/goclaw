import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../stores/ui-store'
import { Select } from '../ui'
import { SettingsCard, SettingsRow } from './settings-row'

export const LANGUAGES = [
  { value: 'en', label: 'English' },
  { value: 'vi', label: 'Tiếng Việt' },
  { value: 'zh', label: '中文' },
  { value: 'ru', label: 'Русский' },
]

/** IANA zone names the webview knows, always including the saved one. */
function timezoneOptions(current: string): { value: string; label: string }[] {
  let zones: string[] = []
  try {
    zones = (Intl as unknown as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf?.('timeZone') ?? []
  } catch {
    zones = []
  }
  const all = zones.includes(current) ? zones : [current, ...zones]
  return all.map((z) => ({ value: z, label: z.replace(/_/g, ' ') }))
}

export function GeneralTab() {
  const { t, i18n } = useTranslation('settings')
  const locale = useUiStore((s) => s.locale)
  const setLocale = useUiStore((s) => s.setLocale)
  const timezone = useUiStore((s) => s.timezone)
  const setTimezone = useUiStore((s) => s.setTimezone)
  const zones = useMemo(() => timezoneOptions(timezone), [timezone])

  const changeLanguage = (lang: string) => {
    setLocale(lang)
    void i18n.changeLanguage(lang)
  }

  return (
    <SettingsCard title={t('general.regionTitle')}>
      <SettingsRow label={t('general.language')} hint={t('general.languageHint')} htmlFor="settings-language">
        <Select id="settings-language" className="w-48" value={locale} options={LANGUAGES} onChange={(e) => changeLanguage(e.target.value)} />
      </SettingsRow>
      <SettingsRow label={t('general.timezone')} hint={t('general.timezoneHint')} htmlFor="settings-timezone">
        <Select id="settings-timezone" className="w-64" value={timezone} options={zones} onChange={(e) => setTimezone(e.target.value)} />
      </SettingsRow>
    </SettingsCard>
  )
}
