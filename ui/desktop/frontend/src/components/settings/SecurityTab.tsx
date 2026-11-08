import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { wails } from '../../lib/wails'
import { IconXCircle } from '../common/Icons'
import { Button, ConfirmDialog, StatusChip } from '../ui'
import { SettingsCard, SettingsRow } from './settings-row'

const CONFIRM_WORD = 'RESET'

export function SecurityTab() {
  const { t } = useTranslation('settings')
  const [confirming, setConfirming] = useState(false)
  const [resetting, setResetting] = useState(false)
  const [error, setError] = useState(false)

  const reset = async () => {
    setResetting(true)
    setError(false)
    try {
      await wails.resetDatabase()
    } catch {
      setError(true)
      setResetting(false)
      setConfirming(false)
    }
  }

  return (
    <>
      <SettingsCard title={t('security.dataTitle')}>
        <SettingsRow label={t('security.storage')} hint={t('security.storageHint')}>
          <StatusChip tone="success" label={t('security.secure')} />
        </SettingsRow>
      </SettingsCard>

      <SettingsCard title={t('security.dangerTitle')} tone="danger">
        <SettingsRow label={t('security.reset.title')} hint={t('security.reset.description')}>
          <Button variant="danger" onClick={() => setConfirming(true)}>{t('security.reset.button')}</Button>
        </SettingsRow>
      </SettingsCard>
      {error && (
        <p role="alert" className="flex items-start gap-1 text-caption text-danger">
          <IconXCircle size={14} className="mt-px shrink-0" />
          <span>{t('security.reset.failed')}</span>
        </p>
      )}

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        variant="destructive"
        title={t('security.reset.confirmTitle')}
        description={t('security.reset.confirmDescription')}
        confirmLabel={t('security.reset.confirm')}
        confirmValue={CONFIRM_WORD}
        loading={resetting}
        onConfirm={() => { void reset() }}
      />
    </>
  )
}
