import { useTranslation } from 'react-i18next'
import { cn } from '../../../lib/cn'
import { useUiStore } from '../../../stores/ui-store'
import { IconAlertTriangle, IconCheck, IconMoon, IconSun, IconXCircle } from '../../common/Icons'
import { IconButton, StatusChip, Tooltip, type StatusTone } from '../../ui'
import { useGatewayStatus, type GatewayStatus } from '../use-gateway-status'
import { useResolvedTheme } from '../use-resolved-theme'

const toneFor: Record<GatewayStatus['status'], StatusTone> = {
  online: 'success',
  offline: 'danger',
  connecting: 'info',
}

const labelKey: Record<GatewayStatus['status'], string> = {
  online: 'gateway.online',
  offline: 'gateway.offline',
  connecting: 'gateway.connecting',
}

function useThemeToggle() {
  const { t } = useTranslation('desktop')
  const setTheme = useUiStore((s) => s.setTheme)
  const dark = useResolvedTheme() === 'dark'
  return {
    label: t('palette.actions.toggleTheme'),
    icon: dark ? <IconSun size={16} /> : <IconMoon size={16} />,
    toggle: () => setTheme(dark ? 'light' : 'dark'),
  }
}

/** Sidebar footer: gateway status (icon + word) and the theme switch. */
export function SidebarFooter({ mode }: { mode: 'full' | 'rail' }) {
  const { t } = useTranslation('desktop')
  const { status } = useGatewayStatus()
  const theme = useThemeToggle()
  const label = t(labelKey[status])

  if (mode === 'rail') {
    const Icon = status === 'online' ? IconCheck : status === 'offline' ? IconXCircle : IconAlertTriangle
    return (
      <div className="flex flex-col items-center gap-1 border-t border-border-subtle pt-2">
        <IconButton aria-label={theme.label} icon={theme.icon} tooltipSide="right" onClick={theme.toggle} />
        <Tooltip content={label} side="right">
          <span
            role="status"
            aria-label={label}
            tabIndex={0}
            className={cn(
              'flex size-8 items-center justify-center rounded-pill',
              status === 'online' ? 'text-success' : status === 'offline' ? 'text-danger' : 'text-info',
            )}
          >
            <Icon size={16} />
          </span>
        </Tooltip>
      </div>
    )
  }

  return (
    <div className="flex items-center gap-2 border-t border-border-subtle pt-2.5">
      <span role="status" className="min-w-0 flex-1">
        <StatusChip tone={toneFor[status]} label={label} />
      </span>
      <IconButton aria-label={theme.label} icon={theme.icon} size="sm" onClick={theme.toggle} />
    </div>
  )
}
