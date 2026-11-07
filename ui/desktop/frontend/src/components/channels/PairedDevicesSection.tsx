import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { usePairedDevices } from '../../hooks/use-paired-devices'
import { Badge, Button, ConfirmDialog, DataTable, EmptyState, ErrorState, IconButton, SkeletonList, type DataTableColumn } from '../ui'
import { IconRefresh } from '../common/Icons'
import type { PendingPairing, PairedDevice } from '../../types/channel'

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** Localized "5 minutes ago" style label for a past timestamp in milliseconds. */
function formatRelativeTime(ms: number, locale: string): string {
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  const diff = ms - Date.now()
  const abs = Math.abs(diff)
  if (abs < MINUTE) return rtf.format(0, 'minute')
  if (abs < HOUR) return rtf.format(Math.round(diff / MINUTE), 'minute')
  if (abs < DAY) return rtf.format(Math.round(diff / HOUR), 'hour')
  return rtf.format(Math.round(diff / DAY), 'day')
}

function formatDate(ms: number, locale: string): string {
  return new Date(ms).toLocaleDateString(locale, { month: 'short', day: 'numeric', year: 'numeric' })
}

interface PairedDevicesSectionProps {
  /** Disables approve, deny and revoke while the gateway is unreachable. */
  readOnly?: boolean
}

export function PairedDevicesSection({ readOnly = false }: PairedDevicesSectionProps) {
  const { t, i18n } = useTranslation('channels')
  const { pendingPairings, pairedDevices, loading, error, refresh, approvePairing, denyPairing, revokePairing } = usePairedDevices()
  const [approveTarget, setApproveTarget] = useState<PendingPairing | null>(null)
  const [denyTarget, setDenyTarget] = useState<PendingPairing | null>(null)
  const [revokeTarget, setRevokeTarget] = useState<PairedDevice | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const locale = i18n.language

  const handleRefresh = async () => {
    setRefreshing(true)
    try { await refresh() } finally { setRefreshing(false) }
  }

  const isEmpty = pendingPairings.length === 0 && pairedDevices.length === 0

  const pendingColumns: DataTableColumn<PendingPairing>[] = [
    {
      id: 'sender',
      header: t('pairing.columns.sender'),
      className: 'min-w-[200px]',
      cell: (p) => (
        <div className="min-w-0">
          <p className="truncate font-mono text-code text-ink">{p.sender_id}</p>
          {p.chat_id && <p className="truncate text-caption text-ink-muted">{t('pairing.chat')}{p.chat_id}</p>}
        </div>
      ),
    },
    { id: 'channel', header: t('pairing.columns.channel'), className: 'w-28', cell: (p) => <Badge>{p.channel}</Badge> },
    { id: 'code', header: t('pairing.code'), className: 'w-28', cell: (p) => <span className="font-mono text-code text-ink">{p.code}</span> },
    { id: 'requested', header: t('pairing.columns.requested'), className: 'w-36', cell: (p) => <span className="text-ui text-ink-muted">{formatRelativeTime(p.created_at, locale)}</span> },
    {
      id: 'actions',
      header: <span className="sr-only">{t('screen.columns.actions')}</span>,
      className: 'w-48 text-right',
      cell: (p) => (
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={() => setDenyTarget(p)} disabled={readOnly}>{t('pairing.deny')}</Button>
          <Button size="sm" onClick={() => setApproveTarget(p)} disabled={readOnly}>{t('pairing.approve')}</Button>
        </div>
      ),
    },
  ]

  const pairedColumns: DataTableColumn<PairedDevice>[] = [
    { id: 'sender', header: t('pairing.columns.sender'), className: 'min-w-[200px]', cell: (d) => <span className="truncate font-mono text-code text-ink">{d.sender_id}</span> },
    { id: 'channel', header: t('pairing.columns.channel'), className: 'w-28', cell: (d) => <Badge>{d.channel}</Badge> },
    { id: 'paired', header: t('pairing.columns.paired'), className: 'w-36', cell: (d) => <span className="text-ui text-ink-muted">{formatDate(d.paired_at, locale)}</span> },
    { id: 'by', header: t('pairing.columns.by'), className: 'w-32', cell: (d) => <span className="truncate text-ui text-ink-muted">{d.paired_by}</span> },
    {
      id: 'actions',
      header: <span className="sr-only">{t('screen.columns.actions')}</span>,
      className: 'w-28 text-right',
      cell: (d) => (
        <div className="flex justify-end">
          <Button size="sm" variant="ghost" onClick={() => setRevokeTarget(d)} disabled={readOnly}>{t('pairing.revoke')}</Button>
        </div>
      ),
    },
  ]

  let body
  if (loading) {
    body = <SkeletonList rows={2} avatar={false} label={t('screen.loading')} />
  } else if (error && isEmpty) {
    body = <ErrorState title={t('pairing.loadError')} description={t('screen.loadErrorHint')} onRetry={() => { void handleRefresh() }} retrying={refreshing} className="py-8" />
  } else if (isEmpty) {
    body = <EmptyState title={t('pairing.empty')} description={t('pairing.emptyDesc')} className="py-8" />
  } else {
    body = (
      <div className="flex flex-col gap-5">
        {pendingPairings.length > 0 && (
          <div className="flex flex-col gap-2">
            <h3 className="text-ui font-medium text-ink">{t('pairing.pending', { count: pendingPairings.length })}</h3>
            <DataTable aria-label={t('pairing.pendingTableLabel')} columns={pendingColumns} rows={pendingPairings} getRowId={(p) => p.code} />
          </div>
        )}
        {pairedDevices.length > 0 && (
          <div className="flex flex-col gap-2">
            <h3 className="text-ui font-medium text-ink">{t('pairing.paired', { count: pairedDevices.length })}</h3>
            <DataTable aria-label={t('pairing.pairedTableLabel')} columns={pairedColumns} rows={pairedDevices} getRowId={(d) => `${d.channel}-${d.sender_id}`} />
          </div>
        )}
      </div>
    )
  }

  return (
    <section aria-labelledby="channels-pairing-title" className="flex flex-col gap-3 border-t border-border-subtle pt-5">
      <div className="flex items-center justify-between gap-3">
        <h2 id="channels-pairing-title" className="text-title text-ink">{t('pairing.title')}</h2>
        <IconButton
          aria-label={t('pairing.refresh')}
          icon={<IconRefresh size={16} />}
          onClick={() => { void handleRefresh() }}
          disabled={refreshing}
        />
      </div>

      {body}

      <ConfirmDialog
        open={approveTarget !== null}
        onOpenChange={(open) => { if (!open) setApproveTarget(null) }}
        title={t('pairing.confirmApprove.title')}
        description={approveTarget ? t('pairing.confirmApprove.description', { channel: approveTarget.channel, senderId: approveTarget.sender_id, code: approveTarget.code }) : ''}
        confirmLabel={t('pairing.confirmApprove.confirmLabel')}
        onConfirm={async () => {
          if (approveTarget && await approvePairing(approveTarget.code)) setApproveTarget(null)
        }}
      />
      <ConfirmDialog
        open={denyTarget !== null}
        onOpenChange={(open) => { if (!open) setDenyTarget(null) }}
        title={t('pairing.confirmDeny.title')}
        description={denyTarget ? t('pairing.confirmDeny.description', { channel: denyTarget.channel, senderId: denyTarget.sender_id, code: denyTarget.code }) : ''}
        confirmLabel={t('pairing.confirmDeny.confirmLabel')}
        variant="destructive"
        onConfirm={async () => {
          if (denyTarget && await denyPairing(denyTarget.code)) setDenyTarget(null)
        }}
      />
      <ConfirmDialog
        open={revokeTarget !== null}
        onOpenChange={(open) => { if (!open) setRevokeTarget(null) }}
        title={t('pairing.confirmRevoke.title')}
        description={revokeTarget ? t('pairing.confirmRevoke.description', { channel: revokeTarget.channel, senderId: revokeTarget.sender_id }) : ''}
        confirmLabel={t('pairing.confirmRevoke.confirmLabel')}
        variant="destructive"
        onConfirm={async () => {
          if (revokeTarget && await revokePairing(revokeTarget.sender_id, revokeTarget.channel)) setRevokeTarget(null)
        }}
      />
    </section>
  )
}
