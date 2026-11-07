import type { ComponentPropsWithRef, ReactNode } from 'react'
import { cn } from '../../lib/cn'
import { buttonVariantClass, type ButtonVariant } from './Button'
import { Spinner } from './Spinner'
import { Tooltip } from './Tooltip'

export interface IconButtonProps extends Omit<ComponentPropsWithRef<'button'>, 'aria-label' | 'children'> {
  /** Required accessible name; also the default tooltip text. */
  'aria-label': string
  icon: ReactNode
  variant?: ButtonVariant
  /** md = 32px square (default), sm = 28px. */
  size?: 'sm' | 'md'
  /** Tooltip text. Defaults to aria-label; pass `false` to suppress it. */
  tooltip?: ReactNode | false
  tooltipSide?: 'top' | 'right' | 'bottom' | 'left'
  loading?: boolean
}

/** Square icon-only button with an accessible label and a tooltip. */
export function IconButton({
  icon,
  variant = 'ghost',
  size = 'md',
  tooltip,
  tooltipSide,
  loading = false,
  disabled,
  className,
  type = 'button',
  ...props
}: IconButtonProps) {
  const button = (
    <button
      type={type}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      className={cn(
        'relative inline-flex shrink-0 items-center justify-center rounded-md border transition-colors',
        'disabled:opacity-50 disabled:pointer-events-none',
        buttonVariantClass[variant],
        size === 'sm' ? 'size-7' : 'size-control',
        className,
      )}
      {...props}
    >
      {loading ? <Spinner size={14} /> : icon}
    </button>
  )
  if (tooltip === false) return button
  return (
    <Tooltip content={tooltip ?? props['aria-label']} side={tooltipSide}>
      {button}
    </Tooltip>
  )
}
