import type { ComponentPropsWithRef, ReactNode } from 'react'
import { cn } from '../../lib/cn'
import { Spinner } from './Spinner'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md'

export interface ButtonProps extends ComponentPropsWithRef<'button'> {
  /** primary: accent fill (one per view). secondary: outline. ghost: no outline. danger: destructive. */
  variant?: ButtonVariant
  /** md = control-h (32px, default); sm = 28px for dense rows. */
  size?: ButtonSize
  /** Shows a spinner in place of the icon, disables the button and keeps its width. */
  loading?: boolean
  /** Leading icon (16px). */
  icon?: ReactNode
}

export const buttonVariantClass: Record<ButtonVariant, string> = {
  primary: 'bg-accent text-on-accent border-transparent hover:bg-accent-hover',
  secondary: 'bg-transparent text-ink border-border-control hover:bg-surface-hover',
  ghost: 'bg-transparent text-ink-secondary border-transparent hover:bg-surface-hover hover:text-ink',
  danger: 'bg-danger-subtle text-danger border-transparent hover:border-danger',
}

export const buttonSizeClass: Record<ButtonSize, string> = {
  sm: 'h-7 px-2.5 gap-1.5 text-caption font-medium',
  md: 'h-control px-3.5 gap-1.5 text-ui',
}

export function Button({
  variant = 'secondary',
  size = 'md',
  loading = false,
  icon,
  disabled,
  className,
  children,
  type = 'button',
  ...props
}: ButtonProps) {
  return (
    <button
      type={type}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      data-variant={variant}
      className={cn(
        'inline-flex shrink-0 items-center justify-center whitespace-nowrap rounded-md border transition-colors',
        'disabled:opacity-50 disabled:pointer-events-none',
        buttonVariantClass[variant],
        buttonSizeClass[size],
        className,
      )}
      {...props}
    >
      {loading ? <Spinner size={14} /> : icon}
      {children}
    </button>
  )
}
