import type { ReactElement, ReactNode } from 'react'
import { Tooltip as TooltipPrimitive } from 'radix-ui'
import { cn } from '../../lib/cn'

export interface TooltipProps {
  /** Tooltip text. When empty the trigger renders without a tooltip. */
  content: ReactNode
  /** A single focusable element (button, link). */
  children: ReactElement
  side?: 'top' | 'right' | 'bottom' | 'left'
  align?: 'start' | 'center' | 'end'
  /** Delay before opening on hover, in ms. */
  delay?: number
  className?: string
}

/** Short label shown on hover and keyboard focus. Not for essential information. */
export function Tooltip({ content, children, side = 'top', align = 'center', delay = 400, className }: TooltipProps) {
  if (content === null || content === undefined || content === '') return children
  return (
    <TooltipPrimitive.Provider delayDuration={delay} skipDelayDuration={200}>
      <TooltipPrimitive.Root>
        <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
        <TooltipPrimitive.Portal>
          <TooltipPrimitive.Content
            side={side}
            align={align}
            sideOffset={6}
            collisionPadding={8}
            className={cn(
              'z-[200] max-w-xs rounded-sm bg-ink px-2 py-1 text-caption text-surface-base shadow-popover',
              'animate-pop-in pointer-events-none select-none',
              className,
            )}
          >
            {content}
          </TooltipPrimitive.Content>
        </TooltipPrimitive.Portal>
      </TooltipPrimitive.Root>
    </TooltipPrimitive.Provider>
  )
}
