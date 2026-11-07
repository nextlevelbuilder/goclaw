import type { ComponentPropsWithRef } from 'react'
import { cn } from '../../lib/cn'
import { controlClass } from './Input'

export interface TextareaProps extends ComponentPropsWithRef<'textarea'> {
  invalid?: boolean
}

export function Textarea({ invalid, className, rows = 4, ...props }: TextareaProps) {
  return (
    <textarea
      rows={rows}
      aria-invalid={invalid || props['aria-invalid'] || undefined}
      className={cn(controlClass, 'min-h-18 px-2.5 py-1.5 resize-y', className)}
      {...props}
    />
  )
}
