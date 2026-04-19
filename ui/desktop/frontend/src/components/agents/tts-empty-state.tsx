import { useTranslation } from 'react-i18next'

/**
 * Shown in AgentDetailPanel TTS Voice section when no global TTS provider is configured.
 * Desktop variant — no router navigation (single-user app, settings are in-app).
 */
export function TtsEmptyState() {
  const { t } = useTranslation('tts')

  return (
    <div className="flex flex-col items-center gap-1.5 rounded-lg border border-dashed border-border p-3 text-center">
      {/* VolumeX icon — inline SVG to avoid lucide-react dep on desktop */}
      <svg
        className="w-5 h-5 text-text-muted"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5" />
        <line x1="23" y1="9" x2="17" y2="15" />
        <line x1="17" y1="9" x2="23" y2="15" />
      </svg>
      <p className="text-xs font-medium text-text-primary">{t('empty_state.title')}</p>
      <p className="text-[11px] text-text-muted">{t('empty_state.non_owner_hint')}</p>
    </div>
  )
}
