import type { ReactNode } from 'react'
import type { IconProps } from '../common/Icons'

// Chat-only glyphs (1.5 stroke, currentColor) that the shared icon set does not have.
function Svg({ size, className, children }: IconProps & { children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
    >
      {children}
    </svg>
  )
}

export function IconArrowUp({ size = 16, className }: IconProps) {
  return <Svg size={size} className={className}><path d="M8 13V3M4 7l4-4 4 4" /></Svg>
}

export function IconStop({ size = 16, className }: IconProps) {
  return <Svg size={size} className={className}><rect x="4" y="4" width="8" height="8" rx="1.5" fill="currentColor" /></Svg>
}

export function IconChevronRight({ size = 12, className }: IconProps) {
  return <Svg size={size} className={className}><path d="M6 3.5L10.5 8 6 12.5" /></Svg>
}
