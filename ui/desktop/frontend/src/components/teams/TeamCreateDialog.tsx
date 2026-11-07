import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { teamService } from '../../services/team-service'
import { Button, Checkbox, Dialog, DialogFooter, Field, Input, Select, toast } from '../ui'
import type { TeamData } from '../../types/team'

interface Agent {
  id: string
  key: string
  name: string
  emoji?: string
}

interface TeamCreateDialogProps {
  agents: Agent[]
  onClose: () => void
  onCreated: (team: TeamData) => void
}

/** New team: a name, a lead agent and at least one member. */
export function TeamCreateDialog({ agents, onClose, onCreated }: TeamCreateDialogProps) {
  const { t } = useTranslation('teams')
  const [name, setName] = useState('')
  const [lead, setLead] = useState('')
  const [memberKeys, setMemberKeys] = useState<string[]>([])
  const [submitted, setSubmitted] = useState(false)
  const [saving, setSaving] = useState(false)

  const label = (a: Agent) => `${a.emoji || ''} ${a.name}`.trim()
  const memberChoices = agents.filter((a) => a.key !== lead)

  const nameError = submitted && !name.trim() ? t('screen.create.nameRequired') : undefined
  const leadError = submitted && !lead ? t('screen.create.leadRequired') : undefined
  const membersError = submitted && lead && memberKeys.length === 0 ? t('screen.create.membersRequired') : undefined

  const toggleMember = (key: string, on: boolean) =>
    setMemberKeys((prev) => (on ? [...prev, key] : prev.filter((k) => k !== key)))

  const submit = async () => {
    setSubmitted(true)
    if (!name.trim() || !lead || memberKeys.length === 0) return
    setSaving(true)
    try {
      const res = await teamService.create({ name: name.trim(), lead, members: memberKeys.filter((k) => k !== lead) })
      toast.success(t('screen.toast.created'), name.trim())
      onCreated(res.team)
      onClose()
    } catch (err) {
      toast.error(t('screen.toast.createFailed'), err instanceof Error ? err.message : undefined)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(o) => { if (!o) onClose() }}
      title={t('screen.create.title')}
      description={t('screen.create.description')}
      size="md"
      dismissible={!saving}
      footer={(
        <DialogFooter>
          <Button variant="secondary" onClick={onClose} disabled={saving}>{t('screen.create.cancel')}</Button>
          <Button variant="primary" onClick={() => void submit()} loading={saving}>{t('screen.create.submit')}</Button>
        </DialogFooter>
      )}
    >
      <div className="flex flex-col gap-4">
        <Field label={t('screen.create.name')} error={nameError} required>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('screen.create.namePlaceholder')}
            autoFocus
          />
        </Field>

        <Field label={t('screen.create.lead')} error={leadError} required>
          <Select
            value={lead}
            onChange={(e) => { setLead(e.target.value); setMemberKeys((prev) => prev.filter((k) => k !== e.target.value)) }}
            placeholder={t('screen.create.leadPlaceholder')}
            options={agents.map((a) => ({ value: a.key, label: label(a) }))}
          />
        </Field>

        {lead && (
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-1 text-ui font-medium text-ink">{t('screen.create.members')}</legend>
            {memberChoices.length === 0 ? (
              <p className="text-caption text-ink-muted">{t('screen.create.noOtherAgents')}</p>
            ) : (
              <div className="flex max-h-40 flex-col gap-1.5 overflow-y-auto overscroll-contain">
                {memberChoices.map((a) => (
                  <Checkbox
                    key={a.key}
                    checked={memberKeys.includes(a.key)}
                    onCheckedChange={(on) => toggleMember(a.key, on)}
                    label={label(a)}
                  />
                ))}
              </div>
            )}
            {membersError && <p role="alert" className="text-caption text-danger">{membersError}</p>}
          </fieldset>
        )}
      </div>
    </Dialog>
  )
}
