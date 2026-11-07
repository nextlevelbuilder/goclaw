import { cloneElement, isValidElement, useId, type ReactElement, type ReactNode } from 'react'
import { cn } from '../../lib/cn'
import { IconXCircle } from '../common/Icons'

export interface FieldProps {
  label: ReactNode
  /** Helper text in caption under the control. Hidden while an error shows. */
  helper?: ReactNode
  /** Validation message beside the field (role="alert"). */
  error?: ReactNode
  required?: boolean
  /** Control id. Defaults to the child's id, then a generated id. */
  htmlFor?: string
  /** A single control (Input, Textarea, Select, Checkbox...). It receives id, aria-describedby and aria-invalid. */
  children: ReactElement<Record<string, unknown>>
  className?: string
}

/** Label above a control, helper text and an inline error. */
export function Field({ label, helper, error, required, htmlFor, children, className }: FieldProps) {
  const autoId = useId()
  const childId = isValidElement(children) ? (children.props.id as string | undefined) : undefined
  const id = htmlFor ?? childId ?? `field-${autoId}`
  const helperId = `${id}-helper`
  const errorId = `${id}-error`
  const hasError = error !== undefined && error !== null && error !== false && error !== ''
  const describedBy = [
    children.props['aria-describedby'] as string | undefined,
    hasError ? errorId : helper ? helperId : undefined,
  ].filter(Boolean).join(' ') || undefined

  const control = cloneElement(children, {
    id,
    'aria-describedby': describedBy,
    'aria-invalid': hasError ? true : children.props['aria-invalid'],
    'aria-required': required || children.props['aria-required'],
  })

  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <label htmlFor={id} className="text-ui text-ink">
        {label}
        {required && <span aria-hidden="true" className="ml-0.5 text-danger">*</span>}
      </label>
      {control}
      {hasError ? (
        <p id={errorId} role="alert" className="flex items-start gap-1 text-caption text-danger">
          <IconXCircle size={14} className="mt-px shrink-0" />
          <span>{error}</span>
        </p>
      ) : helper ? (
        <p id={helperId} className="text-caption text-ink-muted">{helper}</p>
      ) : null}
    </div>
  )
}
