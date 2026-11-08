import { cn } from '../../lib/cn'

export interface SwitchProps {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
  id?: string
  'aria-label'?: string
  'aria-labelledby'?: string
  'aria-describedby'?: string
  className?: string
}

/** On/off toggle (role="switch" with aria-checked). */
export function Switch({ checked, onCheckedChange, disabled, id, className, ...aria }: SwitchProps) {
  return (
    <button
      id={id}
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onCheckedChange(!checked)}
      className={cn(
        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-pill transition-colors',
        checked ? 'bg-accent' : 'bg-border-control',
        'disabled:opacity-50 disabled:cursor-not-allowed',
        className,
      )}
      {...aria}
    >
      <span
        aria-hidden="true"
        className={cn(
          'pointer-events-none block size-4 rounded-pill shadow-panel transition-transform',
          checked ? 'translate-x-[18px] bg-on-accent' : 'translate-x-[2px] bg-surface-raised',
        )}
      />
    </button>
  )
}
