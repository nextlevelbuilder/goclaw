import type { ComponentPropsWithRef } from 'react'
import { cn } from '../../lib/cn'
import { IconChevronDown } from '../common/Icons'
import { controlClass } from './Input'

export interface SelectOption {
  value: string
  label: string
  disabled?: boolean
}

export interface SelectProps extends ComponentPropsWithRef<'select'> {
  invalid?: boolean
  /** Options to render. Children are rendered instead when omitted. */
  options?: SelectOption[]
  /** Disabled first option shown while no value is chosen. */
  placeholder?: string
}

/** Native select styled like the other controls. */
export function Select({ invalid, options, placeholder, className, children, ...props }: SelectProps) {
  return (
    <div className={cn('relative w-full', className)}>
      <select
        aria-invalid={invalid || props['aria-invalid'] || undefined}
        className={cn(controlClass, 'h-control appearance-none pl-2.5 pr-8')}
        {...props}
      >
        {placeholder !== undefined && (
          <option value="" disabled>
            {placeholder}
          </option>
        )}
        {options
          ? options.map((o) => (
              <option key={o.value} value={o.value} disabled={o.disabled}>
                {o.label}
              </option>
            ))
          : children}
      </select>
      <IconChevronDown
        size={14}
        className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-ink-muted"
      />
    </div>
  )
}
