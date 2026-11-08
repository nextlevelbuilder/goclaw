import { useRef, type KeyboardEvent, type ReactNode } from 'react'
import { cn } from '../../lib/cn'

export interface SegmentedOption<T extends string> {
  value: T
  label: ReactNode
  icon?: ReactNode
  disabled?: boolean
}

export interface SegmentedControlProps<T extends string> {
  value: T
  onValueChange: (value: T) => void
  options: SegmentedOption<T>[]
  'aria-label': string
  size?: 'sm' | 'md'
  className?: string
}

/** Single choice between a few options (role="radiogroup", arrow keys move). */
export function SegmentedControl<T extends string>({
  value,
  onValueChange,
  options,
  size = 'md',
  className,
  ...aria
}: SegmentedControlProps<T>) {
  const refs = useRef<Array<HTMLButtonElement | null>>([])
  const enabled = options.filter((o) => !o.disabled)

  const move = (e: KeyboardEvent<HTMLButtonElement>, from: T) => {
    const keys: Record<string, number> = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }
    let next: SegmentedOption<T> | undefined
    if (e.key in keys) {
      const i = enabled.findIndex((o) => o.value === from)
      next = enabled[(i + keys[e.key] + enabled.length) % enabled.length]
    } else if (e.key === 'Home') {
      next = enabled[0]
    } else if (e.key === 'End') {
      next = enabled[enabled.length - 1]
    }
    if (!next) return
    e.preventDefault()
    onValueChange(next.value)
    refs.current[options.indexOf(next)]?.focus()
  }

  return (
    <div
      role="radiogroup"
      className={cn('inline-flex items-center gap-0.5 rounded-md border border-border-subtle bg-surface-window p-0.5', className)}
      {...aria}
    >
      {options.map((o, i) => {
        const selected = o.value === value
        return (
          <button
            key={o.value}
            ref={(el) => { refs.current[i] = el }}
            type="button"
            role="radio"
            aria-checked={selected}
            tabIndex={selected ? 0 : -1}
            disabled={o.disabled}
            onClick={() => onValueChange(o.value)}
            onKeyDown={(e) => move(e, o.value)}
            className={cn(
              'inline-flex items-center justify-center gap-1.5 rounded-sm px-3 transition-colors',
              size === 'sm' ? 'h-6 text-caption font-medium' : 'h-7 text-ui',
              selected ? 'bg-surface-raised text-ink shadow-panel' : 'text-ink-secondary hover:text-ink',
              'disabled:opacity-50',
            )}
          >
            {o.icon}
            {o.label}
          </button>
        )
      })}
    </div>
  )
}
