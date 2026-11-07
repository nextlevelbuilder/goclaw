import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Popover } from 'radix-ui'
import { isCatalogueStale, modelOptionsFor } from '../../api/provider-capabilities'
import { useProviderCapabilities } from '../../hooks/use-provider-capabilities'
import { useChatModelStore } from '../../stores/chat-model-store'
import { cn } from '../../lib/cn'
import { IconAlertTriangle, IconCheck, IconChevronDown, IconRefresh } from '../common/Icons'
import { Input, Spinner } from '../ui'

interface ModelPickerProps {
  /** The agent's own model, shown while no override is set. */
  agentModel?: string
  disabled?: boolean
}

/**
 * Pill that shows the model for this chat and opens a searchable list. The value
 * is a `<provider>/<model>` identity; choosing "Agent default" clears the
 * override so `chat.send` omits `model`.
 */
export function ModelPicker({ agentModel, disabled }: ModelPickerProps) {
  const { t } = useTranslation('chat')
  const { capabilities, isLoading, error, refetch } = useProviderCapabilities()
  const modelOverride = useChatModelStore((s) => s.modelOverride)
  const setModelOverride = useChatModelStore((s) => s.setModelOverride)
  const clearModelOverride = useChatModelStore((s) => s.clearModelOverride)
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')

  const options = useMemo(() => capabilities.flatMap((c) => modelOptionsFor(c)), [capabilities])
  const stale = useMemo(() => capabilities.some((c) => isCatalogueStale(c)), [capabilities])

  const query = search.trim().toLowerCase()
  const filtered = query
    ? options.filter((o) => o.label.toLowerCase().includes(query) || o.value.toLowerCase().includes(query))
    : options
  const canUseCustom = query !== '' && !options.some((o) => o.value.toLowerCase() === query)

  const current = options.find((o) => o.value === modelOverride)?.label ?? modelOverride
  const pillLabel = modelOverride ? current : (agentModel ?? t('model.agentDefault'))

  const choose = (value: string) => {
    if (value) setModelOverride(value)
    else clearModelOverride()
    setOpen(false)
    setSearch('')
  }

  return (
    <Popover.Root open={open} onOpenChange={(next) => { setOpen(next); if (!next) setSearch('') }}>
      <Popover.Trigger asChild>
        <button
          type="button"
          disabled={disabled}
          title={t('modelOverride.label')}
          aria-label={`${t('modelOverride.label')}: ${pillLabel}`}
          className="inline-flex h-7 max-w-56 items-center gap-1.5 rounded-pill border border-border-control px-3 text-caption font-medium text-ink-secondary transition-colors hover:bg-surface-hover disabled:pointer-events-none disabled:opacity-50"
        >
          <span className="truncate">{pillLabel}</span>
          {(stale || error) && <IconAlertTriangle size={12} className="shrink-0 text-warning" />}
          <IconChevronDown size={12} className="shrink-0 text-ink-muted" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          side="top"
          align="start"
          sideOffset={6}
          collisionPadding={8}
          className="pointer-events-auto z-[150] w-80 rounded-md border border-border-subtle bg-surface-raised p-2 shadow-popover animate-pop-in"
        >
          <Input
            autoFocus
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('model.search')}
            aria-label={t('model.search')}
            className="text-base md:text-ui"
          />
          {(stale || error) && (
            <button
              type="button"
              onClick={() => { void refetch() }}
              className="mt-2 flex w-full items-center gap-2 rounded-sm bg-warning-subtle px-2 py-1.5 text-left text-caption text-warning"
            >
              <IconRefresh size={12} className="shrink-0" />
              <span>{error ? t('modelOverride.loadFailed') : t('modelOverride.stale')}</span>
            </button>
          )}
          <ul role="listbox" aria-label={t('modelOverride.label')} className="mt-2 max-h-64 overflow-y-auto overscroll-contain">
            {!query && (
              <ModelRow label={t('model.agentDefault')} hint={agentModel} selected={!modelOverride} onSelect={() => choose('')} />
            )}
            {canUseCustom && (
              <ModelRow label={t('model.useCustom', { value: search.trim() })} selected={false} onSelect={() => choose(search.trim())} />
            )}
            {filtered.map((o) => (
              <ModelRow key={o.value} label={o.label} selected={o.value === modelOverride} onSelect={() => choose(o.value)} />
            ))}
            {isLoading && (
              <li className="flex items-center justify-center py-3"><Spinner /></li>
            )}
            {!isLoading && filtered.length === 0 && !canUseCustom && (
              <li className="px-2 py-3 text-center text-caption text-ink-muted">{t('model.noMatch')}</li>
            )}
          </ul>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  )
}

function ModelRow({ label, hint, selected, onSelect }: { label: string; hint?: string; selected: boolean; onSelect: () => void }) {
  return (
    <li role="option" aria-selected={selected}>
      <button
        type="button"
        onClick={onSelect}
        className={cn(
          'flex h-8 w-full items-center gap-2 rounded-sm px-2 text-left text-ui text-ink transition-colors hover:bg-surface-hover',
          selected && 'bg-accent-subtle text-accent-subtle-ink',
        )}
      >
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {hint && <span className="shrink-0 truncate font-mono text-caption text-ink-muted">{hint}</span>}
        {selected && <IconCheck size={14} className="shrink-0" />}
      </button>
    </li>
  )
}
