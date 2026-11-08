import { useEffect, useRef, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { cn } from '../../lib/cn'

export interface CheckboxProps extends Omit<ComponentPropsWithoutRef<'input'>, 'type' | 'onChange'> {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  /** Shows the mixed state (e.g. "some rows selected"). */
  indeterminate?: boolean
  /** Optional inline label to the right of the box. */
  label?: ReactNode
}

/** Native checkbox in the accent colour, with optional inline label. */
export function Checkbox({ checked, onCheckedChange, indeterminate = false, label, className, disabled, ...props }: CheckboxProps) {
  const ref = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate
  }, [indeterminate])

  const box = (
    <input
      ref={ref}
      type="checkbox"
      checked={checked}
      disabled={disabled}
      aria-checked={indeterminate ? 'mixed' : checked}
      onChange={(e) => onCheckedChange(e.target.checked)}
      className={cn(
        'size-4 shrink-0 rounded-sm border-border-control accent-accent',
        'disabled:opacity-50 disabled:cursor-not-allowed',
        label ? undefined : className,
      )}
      {...props}
    />
  )

  if (!label) return box
  return (
    <label className={cn('inline-flex items-center gap-2 text-ui text-ink', disabled && 'opacity-50', className)}>
      {box}
      <span>{label}</span>
    </label>
  )
}
