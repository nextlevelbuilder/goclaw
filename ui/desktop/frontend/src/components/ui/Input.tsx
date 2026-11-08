import type { ComponentPropsWithRef } from 'react'
import { cn } from '../../lib/cn'

/** Shared look for text-like controls (input, textarea, select). */
export const controlClass = cn(
  'w-full rounded-sm border border-border-control bg-surface-base text-ui font-normal text-ink',
  'placeholder:text-ink-muted transition-colors',
  'disabled:opacity-50 disabled:cursor-not-allowed',
  'aria-[invalid=true]:border-danger',
)

export interface InputProps extends ComponentPropsWithRef<'input'> {
  /** Marks the field invalid (sets aria-invalid). Field sets this automatically when it has an error. */
  invalid?: boolean
}

export function Input({ invalid, className, type = 'text', ...props }: InputProps) {
  return (
    <input
      type={type}
      aria-invalid={invalid || props['aria-invalid'] || undefined}
      className={cn(controlClass, 'h-control px-2.5', className)}
      {...props}
    />
  )
}
