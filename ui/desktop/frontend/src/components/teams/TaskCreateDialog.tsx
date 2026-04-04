import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { IconClose } from '../common/Icons'
import type { TeamMemberData } from '../../types/team'

interface TaskCreateDialogProps {
  teamId: string
  members: TeamMemberData[]
  onClose: () => void
  onCreate: (teamId: string, params: { subject: string; description?: string; priority?: number; assignee?: string }) => Promise<unknown>
}

const PRIORITIES = [
  { value: 0, label: 'P-0 Critical' },
  { value: 1, label: 'P-1 High' },
  { value: 2, label: 'P-2 Medium' },
  { value: 3, label: 'P-3 Low' },
]

export function TaskCreateDialog({ teamId, members, onClose, onCreate }: TaskCreateDialogProps) {
  const { t } = useTranslation('teams')
  const [subject, setSubject] = useState('')
  const [description, setDescription] = useState('')
  const [priority, setPriority] = useState(2)
  const [assignee, setAssignee] = useState('')
  const [saving, setSaving] = useState(false)

  const memberOptions = members.map((m) => ({
    value: m.agent_key || m.agent_id,
    label: `${m.emoji || ''} ${m.display_name || m.agent_key || m.agent_id}`.trim(),
  }))

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!subject.trim()) return
    setSaving(true)
    try {
      await onCreate(teamId, {
        subject: subject.trim(),
        description: description.trim() || undefined,
        priority,
        assignee: assignee || undefined,
      })
      onClose()
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        onClick={(e) => e.stopPropagation()}
        className="bg-surface-primary border border-border rounded-xl shadow-xl w-full max-w-md mx-4"
      >
        <div className="flex items-center justify-between p-4 border-b border-border">
          <h3 className="text-sm font-semibold text-text-primary">{t('createTask', 'Create Task')}</h3>
          <button onClick={onClose} className="text-text-muted hover:text-text-primary cursor-pointer">
            <IconClose size={16} />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="p-4 space-y-3">
          {/* Subject */}
          <div>
            <label className="text-xs text-text-muted mb-1 block">{t('subject', 'Subject')} *</label>
            <input
              type="text"
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
              placeholder={t('subjectPlaceholder', 'Task subject...')}
              className="w-full bg-surface-secondary border border-border rounded-lg px-3 py-2 text-xs text-text-primary placeholder:text-text-muted text-base md:text-sm focus:outline-none focus:ring-1 focus:ring-accent"
              autoFocus
            />
          </div>

          {/* Description */}
          <div>
            <label className="text-xs text-text-muted mb-1 block">{t('description', 'Description')}</label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t('descPlaceholder', 'Task details...')}
              rows={3}
              className="w-full bg-surface-secondary border border-border rounded-lg px-3 py-2 text-xs text-text-primary placeholder:text-text-muted text-base md:text-sm focus:outline-none focus:ring-1 focus:ring-accent resize-none"
            />
          </div>

          {/* Priority */}
          <div>
            <label className="text-xs text-text-muted mb-1 block">{t('priority', 'Priority')}</label>
            <select
              value={priority}
              onChange={(e) => setPriority(Number(e.target.value))}
              className="w-full bg-surface-secondary border border-border rounded-lg px-3 py-2 text-xs text-text-primary text-base md:text-sm focus:outline-none focus:ring-1 focus:ring-accent"
            >
              {PRIORITIES.map((p) => (
                <option key={p.value} value={p.value}>{p.label}</option>
              ))}
            </select>
          </div>

          {/* Assign to */}
          <div>
            <label className="text-xs text-text-muted mb-1 block">{t('assignTo', 'Assign to')}</label>
            <Combobox
              options={memberOptions}
              value={assignee}
              onChange={setAssignee}
              placeholder={t('selectAgent', 'Select agent...')}
            />
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-2">
            <button type="button" onClick={onClose} className="text-xs text-text-muted hover:text-text-primary px-3 py-1.5 rounded cursor-pointer">
              {t('cancel', 'Cancel')}
            </button>
            <button
              type="submit"
              disabled={!subject.trim() || saving}
              className="text-xs font-medium bg-accent text-white px-4 py-1.5 rounded-lg hover:bg-accent/90 transition-colors disabled:opacity-50 cursor-pointer"
            >
              {saving ? t('creating', 'Creating...') : t('create', 'Create')}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
