import { useTranslation } from 'react-i18next'

interface ActivityIndicatorProps {
  phase: string
  tool?: string
  iteration?: number
}

const PHASE_COLOR: Record<string, string> = {
  thinking: 'text-amber-500',
  tool_exec: 'text-blue-500',
  compacting: 'text-warning',
  streaming: 'text-text-secondary',
  retrying: 'text-amber-500',
  leader_processing: 'text-emerald-500',
}

export function ActivityIndicator({ phase, tool, iteration }: ActivityIndicatorProps) {
  const { t } = useTranslation('desktop')

  const color = PHASE_COLOR[phase] ?? 'text-text-muted'

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

  return (
    <div className="flex items-center gap-2 py-2 text-xs text-text-muted">
      <PhaseIcon phase={phase} color={color} />
      <span className={color}>{label}</span>
      {phase !== 'retrying' && iteration && iteration > 1 && (
        <span className="text-text-muted">· {t('activity.step', { n: iteration })}</span>
      )}
    </div>
  )
}

function PhaseIcon({ phase, color }: { phase: string; color: string }) {
  const cls = `w-3.5 h-3.5 ${color}`

  switch (phase) {
    case 'thinking':
      // Brain icon
      return (
        <svg className={`${cls} animate-pulse`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 5a3 3 0 1 0-5.997.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z" />
          <path d="M12 5a3 3 0 1 1 5.997.125 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z" />
          <path d="M15 13a4.5 4.5 0 0 1-3-4 4.5 4.5 0 0 1-3 4" />
        </svg>
      )
    case 'tool_exec':
      // Wrench icon
      return (
        <svg className={`${cls} animate-wobble`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
        </svg>
      )
    case 'retrying':
      // RefreshCw icon
      return (
        <svg className={`${cls} animate-spin`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" /><path d="M21 3v5h-5" />
          <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" /><path d="M3 21v-5h5" />
        </svg>
      )
    default:
      // Bouncing dots fallback
      return (
        <span className="flex gap-0.5">
          {[0, 150, 300].map((delay) => (
            <span
              key={delay}
              className="w-1 h-1 rounded-full bg-accent animate-bounce"
              style={{ animationDelay: `${delay}ms` }}
            />
          ))}
        </span>
      )
  }
}
