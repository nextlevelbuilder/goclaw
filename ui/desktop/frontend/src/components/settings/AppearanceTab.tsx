import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../stores/ui-store'

const LANGUAGES = [
  { value: 'en', label: 'English' },
  { value: 'vi', label: 'Tiếng Việt' },
  { value: 'zh', label: '中文' },
] as const

export function AppearanceTab() {
  const theme = useUiStore((s) => s.theme)
  const toggleTheme = useUiStore((s) => s.toggleTheme)
  const locale = useUiStore((s) => s.locale)
  const setLocale = useUiStore((s) => s.setLocale)
  const { t, i18n } = useTranslation('desktop')

  function handleLanguageChange(lang: string) {
    setLocale(lang)
    i18n.changeLanguage(lang)
  }

  return (
    <div className="space-y-6 max-w-lg">
      <div>
        <h3 className="text-sm font-semibold text-text-primary mb-3">{t('settings.theme')}</h3>
        <div className="flex gap-3">
          {(['dark', 'light'] as const).map((th) => (
            <button
              key={th}
              onClick={() => { if (theme !== th) toggleTheme() }}
              className={[
                'flex-1 rounded-lg border p-3 text-center text-xs font-medium transition-colors',
                theme === th
                  ? 'border-accent bg-accent/10 text-accent'
                  : 'border-border text-text-secondary hover:bg-surface-tertiary',
              ].join(' ')}
            >
              {th === 'dark' ? `🌙 ${t('settings.dark')}` : `☀️ ${t('settings.light')}`}
            </button>
          ))}
        </div>
      </div>

      <div>
        <h3 className="text-sm font-semibold text-text-primary mb-3">{t('settings.language')}</h3>
        <div className="flex gap-3">
          {LANGUAGES.map((lang) => (
            <button
              key={lang.value}
              onClick={() => handleLanguageChange(lang.value)}
              className={[
                'flex-1 rounded-lg border p-3 text-center text-xs font-medium transition-colors',
                locale === lang.value
                  ? 'border-accent bg-accent/10 text-accent'
                  : 'border-border text-text-secondary hover:bg-surface-tertiary',
              ].join(' ')}
            >
              {lang.label}
            </button>
          ))}
        </div>
      </div>

      <div>
        <h3 className="text-sm font-semibold text-text-primary mb-1">{t('settings.timezone')}</h3>
        <div className="px-3 py-2 rounded-lg border border-border bg-surface-tertiary/50 text-xs text-text-primary">
          {useUiStore.getState().timezone}
        </div>
        <p className="text-[11px] text-text-muted mt-1">{t('settings.timezoneHint')}</p>
      </div>
    </div>
  )
}
