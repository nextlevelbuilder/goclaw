import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { chatService } from '../../services/chat-service'
import { mapHistoryMessages } from '../../hooks/chat-history-mapper'
import type { ChatMessage } from '../../stores/chat-store'
import { ErrorState, Skeleton } from '../ui'
import { MessageBubble } from '../chat/MessageBubble'

interface SessionPreviewProps {
  sessionKey: string | null
  /** Bumped by the parent to force a reload (Try again). */
  nonce: number
  onRetry: () => void
}

type PreviewState = { key: string; status: 'loading' | 'ready' | 'error'; messages: ChatMessage[] }

/** Read-only transcript of the selected conversation. */
export function SessionPreview({ sessionKey, nonce, onRetry }: SessionPreviewProps) {
  const { t } = useTranslation('sessions')
  const [state, setState] = useState<PreviewState | null>(null)

  useEffect(() => {
    if (!sessionKey) return
    let cancelled = false
    setState({ key: sessionKey, status: 'loading', messages: [] })
    chatService
      .history(sessionKey)
      .then((res) => {
        if (!cancelled) setState({ key: sessionKey, status: 'ready', messages: mapHistoryMessages(res.messages ?? []) })
      })
      .catch(() => {
        if (!cancelled) setState({ key: sessionKey, status: 'error', messages: [] })
      })
    return () => { cancelled = true }
  }, [sessionKey, nonce])

  if (!sessionKey) {
    return <p className="m-auto px-6 text-center text-ui text-ink-muted">{t('previewEmpty')}</p>
  }
  if (!state || state.key !== sessionKey || state.status === 'loading') {
    return (
      <div role="status" aria-busy="true" className="mx-auto flex w-full max-w-reading flex-col gap-4 p-6">
        <Skeleton className="ml-auto h-8 w-1/3 rounded-lg" />
        <Skeleton className="h-3 w-4/5" />
        <Skeleton className="h-3 w-3/5" />
      </div>
    )
  }
  if (state.status === 'error') {
    return <ErrorState title={t('previewError')} onRetry={onRetry} />
  }
  if (state.messages.length === 0) {
    return <p className="m-auto px-6 text-center text-ui text-ink-muted">{t('previewNoMessages')}</p>
  }
  return (
    <div className="mx-auto flex w-full max-w-reading flex-col gap-4 p-6">
      {state.messages.map((m) => (
        <MessageBubble key={m.id} message={m} />
      ))}
    </div>
  )
}
