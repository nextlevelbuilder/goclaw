import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { usePairedDevices } from '../../hooks/use-paired-devices'
import { ConfirmDialog } from '../common/ConfirmDialog'
import { RefreshButton } from '../common/RefreshButton'
import type { PendingPairing, PairedDevice } from '../../types/channel'

function formatRelativeTime(ms: number): string {
  const diff = Date.now() - ms
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return `${Math.floor(diff / 86400000)}d ago`
}

function formatDate(ms: number): string {
  return new Date(ms).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

export function PairedDevicesSection() {
  const { t } = useTranslation('channels')
  const { pendingPairings, pairedDevices, loading, refresh, approvePairing, denyPairing, revokePairing } = usePairedDevices()
  const [approveTarget, setApproveTarget] = useState<PendingPairing | null>(null)
  const [denyTarget, setDenyTarget] = useState<PendingPairing | null>(null)
  const [revokeTarget, setRevokeTarget] = useState<PairedDevice | null>(null)

  const isEmpty = pendingPairings.length === 0 && pairedDevices.length === 0

  return (
    <div className="space-y-3">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h3 className="text-xs font-semibold text-text-secondary">{t('pairing.title')}</h3>
        <RefreshButton onRefresh={refresh} />
      </div>

      {isEmpty && !loading ? (
        <div className="py-6 text-center">
          <p className="text-xs text-text-muted">{t('pairing.empty')}</p>
          <p className="text-[11px] text-text-muted/70 mt-1">{t('pairing.emptyDesc')}</p>
        </div>
      ) : (
        <div className="space-y-4">
          {/* Pending */}
          {pendingPairings.length > 0 && (
            <div>
              <p className="text-[11px] font-medium text-text-secondary mb-2">{t('pairing.pending', { count: pendingPairings.length })}</p>
              <div className="space-y-2">
                {pendingPairings.map((p) => (
                  <div key={p.code} className="flex items-center justify-between rounded-lg border border-border p-3">
                    <div>
                      <div className="flex items-center gap-2">
                        <span className="rounded-full px-1.5 py-0.5 text-[10px] bg-surface-tertiary text-text-secondary border border-border">{p.channel}</span>
                        <span className="font-mono text-xs font-medium text-text-primary">{p.code}</span>
                      </div>
                      <div className="mt-1 text-[11px] text-text-muted">
                        {t('pairing.sender')}{p.sender_id}
                        {p.chat_id && ` | ${t('pairing.chat')}${p.chat_id}`}
                        {' | '}{formatRelativeTime(p.created_at)}
                      </div>
                    </div>
                    <div className="flex gap-1.5 shrink-0">
                      <button onClick={() => setDenyTarget(p)} className="px-2 py-1 text-[11px] border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors cursor-pointer">
                        {t('pairing.deny')}
                      </button>
                      <button onClick={() => setApproveTarget(p)} className="px-2 py-1 text-[11px] bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors cursor-pointer">
                        {t('pairing.approve')}
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Paired */}
          {pairedDevices.length > 0 && (
            <div>
              <p className="text-[11px] font-medium text-text-secondary mb-2">{t('pairing.paired', { count: pairedDevices.length })}</p>
              <div className="overflow-x-auto rounded-lg border border-border">
                <table className="w-full text-xs min-w-[500px]">
                  <thead>
                    <tr className="border-b border-border bg-surface-tertiary/40">
                      <th className="px-3 py-2 text-left text-[11px] font-medium text-text-muted">Channel</th>
                      <th className="px-3 py-2 text-left text-[11px] font-medium text-text-muted">Sender ID</th>
                      <th className="px-3 py-2 text-left text-[11px] font-medium text-text-muted">Paired</th>
                      <th className="px-3 py-2 text-left text-[11px] font-medium text-text-muted">By</th>
                      <th className="px-3 py-2 w-16" />
                    </tr>
                  </thead>
                  <tbody>
                    {pairedDevices.map((d) => (
                      <tr key={`${d.channel}-${d.sender_id}`} className="border-b border-border last:border-0 hover:bg-surface-tertiary/20">
                        <td className="px-3 py-2">
                          <span className="rounded-full px-1.5 py-0.5 text-[10px] bg-surface-tertiary text-text-secondary border border-border">{d.channel}</span>
                        </td>
                        <td className="px-3 py-2 font-mono text-text-primary">{d.sender_id}</td>
                        <td className="px-3 py-2 text-text-muted">{formatDate(d.paired_at)}</td>
                        <td className="px-3 py-2 text-text-muted">{d.paired_by}</td>
                        <td className="px-3 py-2 text-right">
                          <button onClick={() => setRevokeTarget(d)} className="text-[11px] text-text-muted hover:text-error transition-colors cursor-pointer">
                            {t('pairing.revoke')}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Confirm dialogs */}
      {approveTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setApproveTarget(null)}
          title={t('pairing.confirmApprove.title')}
          description={t('pairing.confirmApprove.description', { channel: approveTarget.channel, senderId: approveTarget.sender_id, code: approveTarget.code })}
          confirmLabel={t('pairing.confirmApprove.confirmLabel')}
          onConfirm={async () => { await approvePairing(approveTarget.code); setApproveTarget(null) }}
        />
      )}
      {denyTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setDenyTarget(null)}
          title={t('pairing.confirmDeny.title')}
          description={t('pairing.confirmDeny.description', { channel: denyTarget.channel, senderId: denyTarget.sender_id, code: denyTarget.code })}
          confirmLabel={t('pairing.confirmDeny.confirmLabel')}
          variant="destructive"
          onConfirm={async () => { await denyPairing(denyTarget.code); setDenyTarget(null) }}
        />
      )}
      {revokeTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setRevokeTarget(null)}
          title={t('pairing.confirmRevoke.title')}
          description={t('pairing.confirmRevoke.description', { channel: revokeTarget.channel, senderId: revokeTarget.sender_id })}
          confirmLabel={t('pairing.confirmRevoke.confirmLabel')}
          variant="destructive"
          onConfirm={async () => { await revokePairing(revokeTarget.sender_id, revokeTarget.channel); setRevokeTarget(null) }}
        />
      )}
    </div>
  )
}
