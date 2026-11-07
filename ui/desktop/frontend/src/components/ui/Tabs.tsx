import type { ComponentPropsWithoutRef } from 'react'
import { Tabs as TabsPrimitive } from 'radix-ui'
import { cn } from '../../lib/cn'

/** Tab set root. Controlled with `value` + `onValueChange`, or uncontrolled with `defaultValue`. */
export function Tabs({ className, ...props }: ComponentPropsWithoutRef<typeof TabsPrimitive.Root>) {
  return <TabsPrimitive.Root className={cn('flex flex-col min-h-0', className)} {...props} />
}

export function TabsList({ className, ...props }: ComponentPropsWithoutRef<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      className={cn('flex shrink-0 items-center gap-1 overflow-x-auto border-b border-border-subtle', className)}
      {...props}
    />
  )
}

export function TabsTrigger({ className, ...props }: ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={cn(
        '-mb-px inline-flex h-control shrink-0 items-center gap-1.5 border-b-2 border-transparent px-3 text-ui text-ink-secondary transition-colors',
        'hover:text-ink data-[state=active]:border-accent data-[state=active]:text-ink',
        'disabled:opacity-50 disabled:pointer-events-none',
        className,
      )}
      {...props}
    />
  )
}

export function TabsContent({ className, ...props }: ComponentPropsWithoutRef<typeof TabsPrimitive.Content>) {
  return <TabsPrimitive.Content className={cn('min-h-0 flex-1', className)} {...props} />
}
