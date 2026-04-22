/**
 * Desktop VoicePicker — capabilities-driven dispatch (Phase 2 mirror).
 * Mirrors web voice-picker.tsx dispatch logic with desktop-native UI.
 *
 *   - "" provider → disabled empty-state
 *   - gemini + static voices → PortalVoicePicker (search + row UI)
 *   - other static voices[] + !voicesDynamic → StaticVoicePicker (Combobox)
 *   - voices_dynamic=true OR minimax → DynamicVoicePicker → PortalVoicePicker
 *   - MiniMax first-fetch failure → FreeTextPicker fallback
 */
import { useId, useState, useRef } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { useVoices, useRefreshVoices } from '../../hooks/use-voices'
import { useTtsCapabilities } from '../../hooks/use-tts-capabilities'
import { VoicePreviewButton } from './voice-preview-button'
import { Combobox } from '../common/Combobox'
import { usePortalDropdownClose } from '../../hooks/use-portal-dropdown-close'
import type { VoiceOption } from '../../api/tts-capabilities'
import type { TtsProviderId } from '@/data/tts-providers'

interface Props {
  value: string | null
  onChange: (voiceId: string) => void
  disabled?: boolean
  provider?: TtsProviderId | ''
}

/**
 * Unified voice shape for PortalVoicePicker (desktop).
 * Matches web PortalVoice — voice_id + name; labels/preview_url optional.
 */
export interface PortalVoice {
  voice_id: string
  name: string
  labels?: Record<string, string>
  preview_url?: string
}

/** Maps a capability VoiceOption to PortalVoice. Labels passed through (e.g. Gemini style). */
function mapCapVoiceToPortal(v: VoiceOption): PortalVoice {
  return { voice_id: v.voice_id, name: v.name, labels: v.labels }
}

function VoiceOption({ voice, selected }: { voice: PortalVoice; selected: boolean }) {
  const labelEntries = ['gender', 'accent', 'age', 'use_case', 'style']
    .filter((k) => voice.labels?.[k])
    .map((k) => voice.labels![k])

  return (
    <span className={['flex items-center gap-1 w-full', selected ? 'text-accent' : ''].join(' ')}>
      <span className="flex-1 truncate" title={voice.name}>{voice.name}</span>
      {labelEntries.slice(0, 1).map((label) => (
        <span key={label} className="text-[10px] px-1 py-0.5 rounded bg-surface-tertiary text-text-muted shrink-0">
          {label}
        </span>
      ))}
      <VoicePreviewButton previewUrl={voice.preview_url} voiceName={voice.name} />
    </span>
  )
}

export function VoicePicker({ value, onChange, disabled, provider }: Props) {
  const { data: caps = [] } = useTtsCapabilities()

  if (provider === '') {
    return <EmptyStatePicker />
  }

  const providerCaps = provider ? caps.find((c) => c.provider === provider) : null
  const voicesDynamic = providerCaps?.custom_features?.['voices_dynamic'] === true
  const staticVoices = providerCaps?.voices ?? []

  // Gemini: static voices rendered through portal picker (search + row UI)
  if (provider === 'gemini' && staticVoices.length > 0) {
    return (
      <PortalVoicePicker
        voices={staticVoices.map(mapCapVoiceToPortal)}
        value={value}
        onChange={onChange}
        disabled={disabled}
      />
    )
  }

  if (providerCaps && !voicesDynamic && staticVoices.length > 0) {
    return (
      <StaticVoicePicker
        value={value}
        onChange={onChange}
        disabled={disabled}
        voices={staticVoices.map((v) => ({ value: v.voice_id, label: v.name }))}
      />
    )
  }

  if (provider === 'minimax' || voicesDynamic) {
    return (
      <DynamicVoicePicker
        value={value}
        onChange={onChange}
        disabled={disabled}
        allowFreeText={provider === 'minimax'}
      />
    )
  }

  return (
    <DynamicVoicePicker
      value={value}
      onChange={onChange}
      disabled={disabled}
      allowFreeText={false}
    />
  )
}

function EmptyStatePicker() {
  const { t } = useTranslation('tts')
  return (
    <div className="flex h-8 w-full items-center rounded border border-border bg-surface-secondary px-2 text-xs text-text-muted opacity-60 cursor-not-allowed">
      {t('voice_picker.requires_provider')}
    </div>
  )
}

function StaticVoicePicker({
  value,
  onChange,
  disabled,
  voices,
}: {
  value: string | null
  onChange: (id: string) => void
  disabled?: boolean
  voices: { value: string; label: string }[]
}) {
  const { t } = useTranslation('tts')
  const options = voices.map((v) => ({ value: v.value, label: v.label }))
  return (
    <Combobox
      value={value ?? ''}
      onChange={onChange}
      options={options}
      placeholder={t('voice_placeholder')}
      allowCustom={false}
      disabled={disabled}
    />
  )
}

function FreeTextVoicePicker({
  value,
  onChange,
  disabled,
}: {
  value: string | null
  onChange: (id: string) => void
  disabled?: boolean
}) {
  const { t } = useTranslation('tts')
  return (
    <input
      type="text"
      className="flex h-8 w-full rounded border border-border bg-surface-secondary px-2 text-xs text-text-primary disabled:opacity-50 disabled:cursor-not-allowed"
      value={value ?? ''}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
      placeholder={t('voice_picker.enter_voice_id', 'Enter voice_id manually')}
    />
  )
}

/**
 * PortalVoicePicker (desktop) — search + scrollable list rendered via portal.
 *
 * Used by:
 *   - DynamicVoicePicker (ElevenLabs, MiniMax) — voices from useVoices()
 *   - VoicePicker dispatcher for Gemini — capability static voices
 *
 * Owns: open/search state, triggerRef/dropdownRef, usePortalDropdownClose, createPortal.
 */
export function PortalVoicePicker({
  voices,
  value,
  onChange,
  disabled,
  isLoading,
  onRefresh,
}: {
  voices: PortalVoice[]
  value: string | null
  onChange: (voice_id: string) => void
  disabled?: boolean
  isLoading?: boolean
  onRefresh?: () => void
}) {
  const { t } = useTranslation('tts')
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const triggerRef = useRef<HTMLDivElement>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)
  const listboxId = useId()

  const selected = voices.find((v) => v.voice_id === value)

  const filtered = search.trim()
    ? voices.filter((v) => v.name.toLowerCase().includes(search.toLowerCase()))
    : voices

  const handleToggle = () => {
    if (disabled) return
    setOpen((prev) => {
      if (prev) return false
      setSearch('')
      return true
    })
  }

  const handleSelect = (voice: PortalVoice) => {
    onChange(voice.voice_id)
    setOpen(false)
    setSearch('')
  }

  usePortalDropdownClose({
    open,
    onClose: () => setOpen(false),
    ignore: [triggerRef, dropdownRef],
  })

  const dropdownContent = open && (
    <div
      ref={dropdownRef}
      id={listboxId}
      role="listbox"
      aria-label={t('voice_placeholder')}
      className="pointer-events-auto z-50 min-w-[220px] rounded border border-border bg-surface-primary text-text-primary shadow-lg"
      style={(() => {
        if (!triggerRef.current) return {}
        const rect = triggerRef.current.getBoundingClientRect()
        const spaceBelow = window.innerHeight - rect.bottom
        const dropH = 240
        if (spaceBelow < dropH && rect.top > dropH) {
          return { position: 'fixed' as const, bottom: window.innerHeight - rect.top + 4, left: rect.left, width: rect.width }
        }
        return { position: 'fixed' as const, top: rect.bottom + 4, left: rect.left, width: rect.width }
      })()}
    >
      <div className="flex items-center gap-1 border-b border-border px-2 py-1.5">
        <input
          autoFocus
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('voice_placeholder')}
          className="flex-1 bg-transparent text-xs outline-none placeholder:text-text-muted"
        />
        {onRefresh && (
          <button
            type="button"
            title={t('voice_refresh')}
            disabled={isLoading}
            onClick={(e) => { e.stopPropagation(); onRefresh() }}
            className="shrink-0 p-1 rounded hover:bg-surface-tertiary transition-colors text-text-muted hover:text-text-primary disabled:opacity-50"
            aria-label={t('voice_refresh')}
          >
            <svg
              className={['w-3 h-3', isLoading ? 'animate-spin' : ''].join(' ')}
              viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}
              strokeLinecap="round" strokeLinejoin="round"
            >
              <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" />
              <path d="M21 3v5h-5" />
              <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" />
              <path d="M3 21v-5h5" />
            </svg>
          </button>
        )}
      </div>

      <div className="max-h-52 overflow-y-auto p-1">
        {isLoading ? (
          <p className="py-3 text-center text-[11px] text-text-muted">{t('voice_loading')}</p>
        ) : filtered.length === 0 ? (
          <p className="py-3 text-center text-[11px] text-text-muted">
            {voices.length === 0 ? t('voice_save_config_first') : search ? t('voice_no_voices') : t('voice_loading')}
          </p>
        ) : (
          filtered.map((voice) => (
            <div
              key={voice.voice_id}
              role="option"
              aria-selected={voice.voice_id === value}
              className={[
                'flex items-center gap-1 rounded px-2 py-1 cursor-pointer text-xs',
                voice.voice_id === value ? 'bg-surface-tertiary text-accent' : 'hover:bg-surface-secondary',
              ].join(' ')}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => handleSelect(voice)}
            >
              <VoiceOption voice={voice} selected={voice.voice_id === value} />
            </div>
          ))
        )}
      </div>
    </div>
  )

  return (
    <div ref={triggerRef} className="relative">
      <button
        type="button"
        disabled={disabled}
        onClick={handleToggle}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listboxId : undefined}
        className={[
          'flex h-8 w-full items-center justify-between gap-1 rounded border border-border bg-surface-secondary px-2 text-xs',
          'disabled:opacity-50 disabled:cursor-not-allowed',
          !selected ? 'text-text-muted' : 'text-text-primary',
        ].join(' ')}
      >
        <span className="flex-1 truncate text-left">
          {isLoading ? t('voice_loading') : selected?.name ?? t('voice_placeholder')}
        </span>
        <svg className="w-3 h-3 shrink-0 opacity-50" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>

      {open && createPortal(dropdownContent, document.body)}
    </div>
  )
}

/**
 * DynamicVoicePicker (desktop) — thin wrapper: fetches voices, delegates to PortalVoicePicker.
 * Handles free-text fallback for MiniMax on fetch error.
 */
function DynamicVoicePicker({
  value,
  onChange,
  disabled,
  allowFreeText,
}: {
  value: string | null
  onChange: (voiceId: string) => void
  disabled?: boolean
  allowFreeText: boolean
}) {
  const { data: voices = [], isLoading, error } = useVoices()
  const { mutate: refresh, isPending: refreshing } = useRefreshVoices()

  // Fall back to free-text when first fetch fails and list is empty
  if (allowFreeText && error && voices.length === 0) {
    return <FreeTextVoicePicker value={value} onChange={onChange} disabled={disabled} />
  }

  return (
    <PortalVoicePicker
      voices={voices}
      value={value}
      onChange={onChange}
      disabled={disabled}
      isLoading={isLoading || refreshing}
      onRefresh={() => refresh()}
    />
  )
}

export { VoiceOption }
