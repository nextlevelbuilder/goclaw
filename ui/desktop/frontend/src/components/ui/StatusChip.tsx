import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { IconAlertTriangle, IconCheck, IconClose, IconDot, IconInfo } from '../common/Icons'

export type StatusTone = 'success' | 'warning' | 'danger' | 'info' | 'idle'

export interface StatusChipProps {
  tone: StatusTone
  /** The word for the state ("Running", "Rate limited"). Defaults to a generic word for the tone. */
  label?: ReactNode
  /** Extra detail shown as a native tooltip. */
  title?: string
  className?: string
}

const toneClass: Record<StatusTone, string> = {
  success: 'bg-success-subtle text-success',
  warning: 'bg-warning-subtle text-warning',
  danger: 'bg-danger-subtle text-danger',
  info: 'bg-info-subtle text-info',
  idle: 'bg-surface-raised text-ink-muted border border-border-subtle',
}

const toneIcon: Record<StatusTone, ReactNode> = {
  success: <IconCheck size={12} />,
  warning: <IconAlertTriangle size={12} />,
  danger: <IconClose size={12} />,
  info: <IconInfo size={12} />,
  idle: <IconDot size={12} />,
}

/** State of an agent, provider, channel or run: always an icon and a word, never colour alone. */
export function StatusChip({ tone, label, title, className }: StatusChipProps) {
  const { t } = useTranslation('common')
  return (
    <span
      title={title}
      data-tone={tone}
      className={cn(
        'inline-flex h-[22px] shrink-0 items-center gap-1 rounded-pill pl-2 pr-2.5 text-caption font-medium whitespace-nowrap',
        toneClass[tone],
        className,
      )}
    >
      {toneIcon[tone]}
      <span>{label ?? t(`tone.${tone}`)}</span>
    </span>
  )
}
