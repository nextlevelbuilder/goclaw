import type { ReactNode } from 'react'
import { IconXCircle } from '../common/Icons'

/** Heading, description and body of one setup step inside the wizard card. */
export function StepShell({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1">
        <h2 className="text-display text-ink">{title}</h2>
        <p className="text-body text-ink-secondary">{description}</p>
      </div>
      {children}
    </div>
  )
}

/** Inline failure message for a step (announced to screen readers). */
export function StepError({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="flex items-start gap-1.5 rounded-md bg-danger-subtle px-3 py-2 text-caption text-danger">
      <IconXCircle size={14} className="mt-px shrink-0" />
      <span>{children}</span>
    </p>
  )
}
