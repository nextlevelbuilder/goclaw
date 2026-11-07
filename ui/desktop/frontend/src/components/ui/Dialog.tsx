import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog as DialogPrimitive } from 'radix-ui'
import { cn } from '../../lib/cn'
import { IconClose } from '../common/Icons'
import { IconButton } from './IconButton'

export type DialogSize = 'sm' | 'md' | 'lg' | 'xl'

export interface DialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Labels the dialog (aria-labelledby). */
  title: ReactNode
  /** Optional sentence under the title (aria-describedby). */
  description?: ReactNode
  size?: DialogSize
  /** Footer row, usually a <DialogFooter>. */
  footer?: ReactNode
  /** Hide the top-right close button (Esc still closes). */
  hideClose?: boolean
  /** Block closing on Esc / outside click (e.g. while saving). */
  dismissible?: boolean
  children?: ReactNode
  className?: string
}

const sizeClass: Record<DialogSize, string> = {
  sm: 'max-w-sm',
  md: 'max-w-lg',
  lg: 'max-w-2xl',
  xl: 'max-w-4xl',
}

/** Modal dialog: focus trap, Esc closes, labelled title, popover shadow over a dimmed window. */
export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  size = 'md',
  footer,
  hideClose = false,
  dismissible = true,
  children,
  className,
}: DialogProps) {
  const { t } = useTranslation('common')
  const block = (e: Event) => { if (!dismissible) e.preventDefault() }

  return (
    <DialogPrimitive.Root open={open} onOpenChange={(next) => { if (next || dismissible) onOpenChange(next) }}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[90] bg-surface-window/70" />
        <DialogPrimitive.Content
          onEscapeKeyDown={block}
          onPointerDownOutside={block}
          onInteractOutside={block}
          {...(description ? {} : { 'aria-describedby': undefined })}
          className={cn(
            'fixed left-1/2 top-1/2 z-[100] flex max-h-[calc(100dvh-4rem)] w-[calc(100vw-2rem)] -translate-x-1/2 -translate-y-1/2 flex-col',
            'rounded-xl border border-border-subtle bg-surface-raised text-ink shadow-popover animate-pop-in',
            sizeClass[size],
            className,
          )}
        >
          <div className="flex items-start gap-3 px-5 pt-5 pb-3">
            <div className="min-w-0 flex-1">
              <DialogPrimitive.Title className="text-title text-ink">{title}</DialogPrimitive.Title>
              {description && (
                <DialogPrimitive.Description className="mt-1 text-ui font-normal text-ink-secondary">
                  {description}
                </DialogPrimitive.Description>
              )}
            </div>
            {!hideClose && (
              <DialogPrimitive.Close asChild>
                <IconButton aria-label={t('close')} icon={<IconClose size={16} />} size="sm" tooltip={false} className="-mr-1.5 -mt-1" />
              </DialogPrimitive.Close>
            )}
          </div>
          {children !== undefined && <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-4">{children}</div>}
          {footer}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}

export interface DialogFooterProps {
  /** Right-aligned actions: Cancel, then the primary action on the far right. */
  children: ReactNode
  /** Left-aligned destructive action (e.g. "Delete team" in an edit dialog). */
  destructive?: ReactNode
  className?: string
}

/** Dialog footer: destructive action on the left, Cancel + primary on the right. */
export function DialogFooter({ children, destructive, className }: DialogFooterProps) {
  return (
    <div className={cn('flex items-center gap-2 border-t border-border-subtle px-5 py-3', className)}>
      {destructive && <div className="mr-auto flex items-center gap-2">{destructive}</div>}
      <div className="ml-auto flex items-center gap-2">{children}</div>
    </div>
  )
}
