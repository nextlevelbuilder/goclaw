import type { StatusTone } from '../components/ui'
import type { ChannelStatus } from '../types/channel'

interface ChannelStatusChip {
  tone: StatusTone
  label: string
}

/** Maps a channel's runtime state to a StatusChip tone and a localized word. */
export function getChannelStatusChip(
  enabled: boolean,
  status: ChannelStatus | null,
  t: (key: string, options?: Record<string, unknown>) => string,
): ChannelStatusChip {
  if (!enabled) return { tone: 'idle', label: t('status.disabled') }

  switch (status?.state) {
    case 'healthy':
      return { tone: 'success', label: t('status.running') }
    case 'degraded':
      return { tone: 'warning', label: t('status.degraded') }
    case 'starting':
      return { tone: 'info', label: t('status.starting') }
    case 'registered':
      return { tone: 'idle', label: t('status.registered') }
    case 'failed':
      return { tone: 'danger', label: t('status.failed') }
    case 'stopped':
      return { tone: 'idle', label: t('status.stopped') }
    default:
      return status?.running
        ? { tone: 'success', label: t('status.running') }
        : { tone: 'idle', label: t('status.stopped') }
  }
}
