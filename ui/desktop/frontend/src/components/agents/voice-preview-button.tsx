import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useRefreshVoices } from '../../hooks/use-voices'

interface Props {
  previewUrl?: string
  voiceName: string
}

// Singleton — only one preview audio plays at a time across all instances.
let globalAudio: HTMLAudioElement | null = null
let globalStop: (() => void) | null = null

export function VoicePreviewButton({ previewUrl, voiceName }: Props) {
  const { t } = useTranslation('tts')
  const [playing, setPlaying] = useState(false)
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const { mutate: refreshVoices } = useRefreshVoices()

  // Hide entirely when no preview URL (resolves P6-L)
  if (!previewUrl) return null

  const stop = () => {
    if (audioRef.current) {
      audioRef.current.pause()
      audioRef.current.src = ''
      audioRef.current = null
    }
    setPlaying(false)
  }

  const handlePlay = () => {
    if (!previewUrl) return

    // Pause any other playing preview
    if (globalAudio && globalAudio !== audioRef.current) {
      globalAudio.pause()
      globalAudio.src = ''
      globalStop?.()
    }

    if (playing) {
      stop()
      globalAudio = null
      globalStop = null
      return
    }

    const audio = new Audio(previewUrl)
    audioRef.current = audio
    globalAudio = audio
    globalStop = stop

    audio.play().catch(() => {
      refreshVoices()
      stop()
      globalAudio = null
      globalStop = null
    })

    audio.onended = () => {
      stop()
      if (globalAudio === audio) {
        globalAudio = null
        globalStop = null
      }
    }

    audio.onerror = () => {
      refreshVoices()
      stop()
      if (globalAudio === audio) {
        globalAudio = null
        globalStop = null
      }
    }

    setPlaying(true)
  }

  return (
    <button
      type="button"
      title={playing ? t('voice_stop_preview') : t('voice_preview', { name: voiceName })}
      onClick={(e) => { e.stopPropagation(); handlePlay() }}
      className="shrink-0 p-1 rounded hover:bg-surface-tertiary transition-colors text-text-muted hover:text-text-primary"
      aria-label={playing ? t('voice_stop_preview') : t('voice_preview', { name: voiceName })}
    >
      {playing ? (
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor">
          <rect x="6" y="4" width="4" height="16" /><rect x="14" y="4" width="4" height="16" />
        </svg>
      ) : (
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor">
          <polygon points="5,3 19,12 5,21" />
        </svg>
      )}
    </button>
  )
}
