import { cn } from '../../lib/cn'

export interface SpinnerProps {
  size?: number
  className?: string
}

/** Small inline progress indicator in currentColor. Decorative; pair it with text or aria-busy. */
export function Spinner({ size = 16, className }: SpinnerProps) {
  return (
    <span
      aria-hidden="true"
      data-testid="spinner"
      className={cn('inline-block shrink-0 rounded-pill border-2 border-current border-t-transparent animate-spin', className)}
      style={{ width: size, height: size }}
    />
  )
}
