import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { IconClock, IconRefresh, IconWrench } from '../common/Icons'
import './chat-shimmer.css'

interface ActivityIndicatorProps {
  phase: string
  tool?: string
  iteration?: number
}

/** What the agent is doing right now, as one quiet line under the transcript. */
export function ActivityIndicator({ phase, tool, iteration }: ActivityIndicatorProps) {
  const { t } = useTranslation('chat')

  let label: string
  switch (phase) {
    case 'thinking':
      label = t('activity.thinking')
      break
    case 'tool_exec':
      label = tool ? t('activity.runningTool', { tool }) : t('activity.executingTool')
      break
    case 'compacting':
      label = t('activity.compacting')
      break
    case 'streaming':
      label = t('activity.streaming')
      break
    case 'retrying':
      label = iteration ? t('activity.retryingAttempt', { n: iteration }) : t('activity.retrying')
      break
    case 'leader_processing':
      label = t('activity.processing')
      break
    default:
      label = phase
  }

  const Icon = phase === 'tool_exec' ? IconWrench : phase === 'retrying' ? IconRefresh : IconClock

  return (
    <div role="status" className="flex items-center gap-2 text-code text-ink-muted">
      <Icon size={14} />
      <span className={cn(phase === 'thinking' && 'chat-shimmer')}>{label}</span>
      {phase !== 'retrying' && iteration && iteration > 1 && <span>· {t('activity.step', { n: iteration })}</span>}
    </div>
  )
}
