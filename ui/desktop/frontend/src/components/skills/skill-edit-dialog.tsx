import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Dialog, DialogFooter, Field, Input, Select, Textarea, toast } from '../ui'
import type { SkillUpdate } from '../../hooks/use-skills'
import type { SkillInfo } from '../../types/skill'

interface SkillEditDialogProps {
  skill: SkillInfo
  onClose: () => void
  onSave: (id: string, updates: SkillUpdate) => Promise<void>
}

/** Edit a skill's name, description, visibility and tags. */
export function SkillEditDialog({ skill, onClose, onSave }: SkillEditDialogProps) {
  const { t } = useTranslation('skills')
  const [name, setName] = useState(skill.name)
  const [description, setDescription] = useState(skill.description ?? '')
  const [visibility, setVisibility] = useState(skill.visibility || 'private')
  const [tags, setTags] = useState((skill.tags ?? []).join(', '))
  const [submitted, setSubmitted] = useState(false)
  const [saving, setSaving] = useState(false)

  const nameError = submitted && !name.trim() ? t('screen.editNameRequired') : undefined

  const submit = async () => {
    setSubmitted(true)
    if (!name.trim() || !skill.id) return
    setSaving(true)
    try {
      await onSave(skill.id, {
        name: name.trim(),
        description: description.trim(),
        visibility,
        tags: tags.split(',').map((x) => x.trim()).filter(Boolean),
      })
      toast.success(t('toast.updated'), name.trim())
      onClose()
    } catch (err) {
      toast.error(t('toast.updateFailed'), err instanceof Error ? err.message : undefined)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(o) => { if (!o) onClose() }}
      title={t('edit.title')}
      size="md"
      dismissible={!saving}
      footer={(
        <DialogFooter>
          <Button variant="secondary" onClick={onClose} disabled={saving}>{t('edit.cancel')}</Button>
          <Button variant="primary" onClick={() => void submit()} loading={saving}>{t('edit.save')}</Button>
        </DialogFooter>
      )}
    >
      <div className="flex flex-col gap-4">
        <Field label={t('edit.name')} error={nameError} required>
          <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label={t('edit.description')}>
          <Textarea rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <Field label={t('edit.visibility')}>
          <Select
            value={visibility}
            onChange={(e) => setVisibility(e.target.value)}
            options={[
              { value: 'private', label: t('edit.privateOption') },
              { value: 'internal', label: t('edit.internalOption') },
              { value: 'public', label: t('edit.publicOption') },
            ]}
          />
        </Field>
        <Field label={t('edit.tags')} helper={t('screen.tagsHelper')}>
          <Input value={tags} onChange={(e) => setTags(e.target.value)} />
        </Field>
      </div>
    </Dialog>
  )
}
