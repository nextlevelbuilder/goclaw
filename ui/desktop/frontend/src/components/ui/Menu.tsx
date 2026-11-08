import type { ReactElement, ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { DropdownMenu } from 'radix-ui'
import { cn } from '../../lib/cn'
import { IconMore } from '../common/Icons'
import { IconButton } from './IconButton'

export interface MenuItem {
  label: ReactNode
  onSelect: () => void
  icon?: ReactNode
  /** Danger styling for destructive actions ("Delete agent"). */
  destructive?: boolean
  disabled?: boolean
  /** Keyboard hint shown on the right, e.g. "Ctrl N". */
  shortcut?: string
}

export type MenuEntry = MenuItem | 'separator'

export interface MenuProps {
  items: MenuEntry[]
  /** Custom trigger element. Defaults to a "More actions" icon button (row menu). */
  trigger?: ReactElement
  /** Accessible label for the default trigger. */
  label?: string
  align?: 'start' | 'center' | 'end'
  side?: 'top' | 'right' | 'bottom' | 'left'
  className?: string
}

/** Row or toolbar menu. Arrow keys move, Enter selects, Esc closes. */
export function Menu({ items, trigger, label, align = 'end', side = 'bottom', className }: MenuProps) {
  const { t } = useTranslation('common')
  return (
    <DropdownMenu.Root modal={false}>
      <DropdownMenu.Trigger asChild>
        {trigger ?? (
          <IconButton aria-label={label ?? t('moreActions')} icon={<IconMore size={16} />} size="sm" tooltip={false} />
        )}
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align={align}
          side={side}
          sideOffset={4}
          collisionPadding={8}
          className={cn(
            'z-[150] min-w-44 rounded-md border border-border-subtle bg-surface-raised p-1 shadow-popover animate-pop-in pointer-events-auto',
            className,
          )}
        >
          {items.map((item, i) =>
            item === 'separator' ? (
              <DropdownMenu.Separator key={`sep-${i}`} className="my-1 h-px bg-border-subtle" />
            ) : (
              <DropdownMenu.Item
                key={i}
                disabled={item.disabled}
                onSelect={item.onSelect}
                className={cn(
                  'flex h-8 cursor-pointer select-none items-center gap-2 rounded-sm px-2 text-ui outline-none',
                  'data-[highlighted]:bg-surface-hover data-[disabled]:pointer-events-none data-[disabled]:opacity-50',
                  item.destructive ? 'text-danger' : 'text-ink',
                )}
              >
                {item.icon && <span className="flex size-4 items-center justify-center text-current">{item.icon}</span>}
                <span className="flex-1 truncate">{item.label}</span>
                {item.shortcut && <kbd className="font-mono text-caption text-ink-muted">{item.shortcut}</kbd>}
              </DropdownMenu.Item>
            ),
          )}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}
