import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useAgents } from '../../hooks/use-agents'
import { useUiStore } from '../../stores/ui-store'
import { LANGUAGES, getAllTimezones } from '../../lib/constants'

function LanguagePicker() {
  const locale = useUiStore((s) => s.locale)
  const setLocale = useUiStore((s) => s.setLocale)
  const { i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const current = LANGUAGES.find((l) => l.value === locale) ?? LANGUAGES[0]

  function handleSelect(lang: string) {
    setLocale(lang)
    i18n.changeLanguage(lang)
    setOpen(false)
  }

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen(!open)}
        className="wails-no-drag flex items-center gap-1 px-2 py-1 rounded-lg text-xs text-text-muted hover:text-text-primary hover:bg-surface-tertiary transition-colors"
      >
        <span>{current.flag}</span>
        <span>{current.label}</span>
        <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      {open && (
        <div className="absolute right-0 top-full mt-1 bg-surface-secondary border border-border rounded-lg shadow-lg overflow-hidden z-50">
          {LANGUAGES.map((lang) => (
            <button
              key={lang.value}
              onClick={() => handleSelect(lang.value)}
              className={`w-full flex items-center gap-2 px-3 py-1.5 text-xs transition-colors ${
                locale === lang.value
                  ? 'bg-accent/10 text-accent font-medium'
                  : 'text-text-secondary hover:bg-surface-tertiary'
              }`}
            >
              <span>{lang.flag}</span>
              <span>{lang.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

function TimezonePicker() {
  const timezone = useUiStore((s) => s.timezone)
  const setTimezone = useUiStore((s) => s.setTimezone)
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const allTimezones = getAllTimezones()
  const filtered = search
    ? allTimezones.filter((tz: string) => tz.toLowerCase().includes(search.toLowerCase()))
    : allTimezones

  // Short display: "Asia/Saigon" → "Saigon"
  const shortTz = timezone.split('/').pop() ?? timezone

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => { setOpen(!open); setSearch('') }}
        className="wails-no-drag flex items-center gap-1 px-2 py-1 rounded-lg text-xs text-text-muted hover:text-text-primary hover:bg-surface-tertiary transition-colors"
        title={timezone}
      >
        <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
        </svg>
        <span>{shortTz}</span>
        <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      {open && (
        <div className="absolute right-0 top-full mt-1 w-56 bg-surface-secondary border border-border rounded-lg shadow-lg overflow-hidden z-50">
          <div className="p-1.5">
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search timezone..."
              className="w-full px-2 py-1 text-xs bg-surface-tertiary border border-border rounded text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
              autoFocus
            />
          </div>
          <div className="max-h-48 overflow-y-auto overscroll-contain">
            {filtered.slice(0, 50).map((tz) => (
              <button
                key={tz}
                onClick={() => { setTimezone(tz); setOpen(false) }}
                className={`w-full text-left px-3 py-1 text-xs transition-colors ${
                  timezone === tz
                    ? 'bg-accent/10 text-accent font-medium'
                    : 'text-text-secondary hover:bg-surface-tertiary'
                }`}
              >
                {tz}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

export function ChatTopBar() {
  const { selectedAgent } = useAgents()
  const toggleSidebar = useUiStore((s) => s.toggleSidebar)
  const sidebarOpen = useUiStore((s) => s.sidebarOpen)

  return (
    <div className="h-12 flex items-center px-4 shrink-0 mt-4">
      {/* Sidebar toggle */}
      {!sidebarOpen && (
        <button
          onClick={toggleSidebar}
          className="wails-no-drag w-7 h-7 flex items-center justify-center rounded-lg text-text-muted hover:text-text-primary hover:bg-surface-tertiary transition-colors mr-2"
          title="Show sidebar"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <line x1="3" y1="6" x2="21" y2="6" /><line x1="3" y1="12" x2="21" y2="12" /><line x1="3" y1="18" x2="21" y2="18" />
          </svg>
        </button>
      )}

      {/* Agent info */}
      <div className="flex-1 min-w-0">
        {selectedAgent ? (
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-text-primary">{selectedAgent.name}</span>
            <span className="text-[11px] font-mono px-1.5 py-0.5 rounded bg-surface-tertiary text-text-muted">
              {selectedAgent.model}
            </span>
          </div>
        ) : (
          <span className="text-sm text-text-muted">Select an agent to start chatting</span>
        )}
      </div>

      {/* Top right pickers */}
      <div className="flex items-center gap-1">
        <TimezonePicker />
        <LanguagePicker />
      </div>
    </div>
  )
}
