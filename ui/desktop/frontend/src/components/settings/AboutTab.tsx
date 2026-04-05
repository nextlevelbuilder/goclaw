import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime'
import { wails } from '../../lib/wails'

export function AboutTab() {
  const { t } = useTranslation('desktop')
  const [version, setVersion] = useState('...')
  const [dataDir, setDataDir] = useState('')
  const [showReset, setShowReset] = useState(false)

  useEffect(() => {
    wails.getVersion().then(setVersion).catch(() => setVersion('unknown'))
    wails.getDataDir().then(setDataDir).catch(() => {})
  }, [])

  return (
    <div className="space-y-6 max-w-lg">
      <div className="flex items-center gap-4">
        <img src="/goclaw-icon.svg" alt="GoClaw" className="h-12 w-12" />
        <div>
          <h3 className="text-base font-semibold text-text-primary">{t('about.title')}</h3>
          <p className="text-xs text-text-muted">{t('about.subtitle')}</p>
        </div>
      </div>

      <div className="rounded-lg border border-border p-4 space-y-3">
        <div className="flex justify-between text-xs">
          <span className="text-text-muted">{t('about.version')}</span>
          <span className="text-text-primary font-mono">{version}</span>
        </div>
        <div className="flex justify-between text-xs">
          <span className="text-text-muted">{t('about.edition')}</span>
          <span className="text-accent font-medium">Lite (SQLite)</span>
        </div>
        <div className="flex justify-between text-xs">
          <span className="text-text-muted">{t('about.runtime')}</span>
          <span className="text-text-primary font-mono">{t('about.runtimeValue')}</span>
        </div>
        {dataDir && (
          <div className="flex justify-between text-xs items-center">
            <span className="text-text-muted">{t('about.database')}</span>
            <button
              onClick={() => wails.openFile(dataDir)}
              className="text-accent hover:underline cursor-pointer font-mono text-right truncate max-w-[260px]"
              title={dataDir}
            >
              {dataDir}
            </button>
          </div>
        )}
      </div>

      <div>
        <h3 className="text-sm font-semibold text-text-primary mb-2">{t('about.editionLimits')}</h3>
        <div className="rounded-lg border border-border divide-y divide-border text-xs">
          {[
            { label: t('about.agents'), limit: t('about.maxAgents') },
            { label: t('about.teams'), limit: t('about.maxTeams') },
            { label: t('about.teamMembers'), limit: t('about.maxTeamMembers') },
            { label: t('about.database'), limit: t('about.databaseValue') },
            { label: t('about.users'), limit: t('about.usersValue') },
          ].map((item) => (
            <div key={item.label} className="flex justify-between px-3 py-2">
              <span className="text-text-muted">{item.label}</span>
              <span className="text-text-secondary">{item.limit}</span>
            </div>
          ))}
        </div>
      </div>

      {/* Reset Database */}
      <div className="rounded-lg border border-error/30 bg-error/5 p-4 space-y-2">
        <h3 className="text-sm font-semibold text-error">{t('about.reset.title')}</h3>
        <p className="text-xs text-text-muted">{t('about.reset.description')}</p>
        <button
          onClick={() => setShowReset(true)}
          className="bg-error/10 text-error border border-error/30 rounded-lg px-4 py-1.5 text-sm font-medium hover:bg-error/20 transition-colors cursor-pointer"
        >
          {t('about.reset.button')}
        </button>
      </div>

      <div className="text-xs text-text-muted">
        <button
          onClick={() => BrowserOpenURL('https://github.com/nextlevelbuilder/goclaw')}
          className="text-accent hover:underline cursor-pointer"
        >
          GitHub
        </button>
        <span className="mx-2">·</span>
        <span>{t('about.builtWith')}</span>
      </div>

      {showReset && <ResetConfirmModal onClose={() => setShowReset(false)} />}
    </div>
  )
}

/* ─── Reset Confirm Modal ─── */

function ResetConfirmModal({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation('desktop')
  const [confirmText, setConfirmText] = useState('')
  const [resetting, setResetting] = useState(false)

  const confirmWord = 'RESET'
  const isConfirmed = confirmText === confirmWord

  async function handleReset() {
    if (!isConfirmed) return
    setResetting(true)
    try {
      await wails.resetDatabase()
    } catch {
      setResetting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={onClose} />
      <div className="relative w-full max-w-sm mx-4 bg-surface-secondary rounded-xl border border-border overflow-hidden">
        <div className="p-5 space-y-4">
          <div className="flex items-center gap-3">
            {/* Warning icon */}
            <div className="shrink-0 h-10 w-10 rounded-full bg-error/10 flex items-center justify-center">
              <svg className="h-5 w-5 text-error" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" />
                <path d="M12 9v4" /><path d="M12 17h.01" />
              </svg>
            </div>
            <div>
              <h3 className="text-sm font-semibold text-text-primary">{t('about.reset.confirmTitle')}</h3>
              <p className="text-xs text-text-muted mt-0.5">{t('about.reset.confirmDescription')}</p>
            </div>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs text-text-secondary">
              {t('about.reset.confirmPrompt', { word: confirmWord })}
            </label>
            <input
              value={confirmText}
              onChange={(e) => setConfirmText(e.target.value)}
              placeholder={confirmWord}
              autoFocus
              className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-error font-mono"
            />
          </div>
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3">
          <button
            type="button"
            onClick={onClose}
            disabled={resetting}
            className="border border-border rounded-lg px-4 py-1.5 text-sm text-text-secondary hover:bg-surface-tertiary transition-colors"
          >
            {t('about.reset.cancel')}
          </button>
          <button
            type="button"
            onClick={handleReset}
            disabled={!isConfirmed || resetting}
            className="bg-error rounded-lg px-4 py-1.5 text-sm text-white hover:bg-error/90 disabled:opacity-50 transition-colors"
          >
            {resetting ? t('about.reset.resetting') : t('about.reset.confirm')}
          </button>
        </div>
      </div>
    </div>
  )
}
