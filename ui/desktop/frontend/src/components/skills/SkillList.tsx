import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSkills, MAX_SKILLS_LITE } from '../../hooks/use-skills'
import type { RuntimeStatus } from '../../hooks/use-skills'
import { IconPlus, IconSearch, IconStar } from '../common/Icons'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { cn } from '../../lib/cn'
import {
  Button, ConfirmDialog, EmptyState, ErrorState, Input, PageHeader, SkeletonList, toast,
} from '../ui'
import { SkillTable } from './skill-card'
import { SkillAddDialog } from './skill-add-dialog'
import { SkillEditDialog } from './skill-edit-dialog'
import type { SkillInfo } from '../../types/skill'

/** Skills: searchable table with enable switches, add (zip upload), edit and delete. */
export function SkillList() {
  const { t } = useTranslation('skills')
  const { disconnected } = useGatewayStatus()
  const { skills, loading, error, atLimit, fetchSkills, toggleSkill, uploadSkill, updateSkill, checkRuntimes, deleteSkill } = useSkills()
  const [runtimes, setRuntimes] = useState<RuntimeStatus | null>(null)
  const [query, setQuery] = useState('')
  const [addOpen, setAddOpen] = useState(false)
  const [editing, setEditing] = useState<SkillInfo | null>(null)
  const [deleting, setDeleting] = useState<SkillInfo | null>(null)
  const [busy, setBusy] = useState(false)
  const [retrying, setRetrying] = useState(false)

  useEffect(() => {
    checkRuntimes().then((r) => { if (r) setRuntimes(r) }).catch(() => undefined)
  }, [checkRuntimes])

  const retry = useCallback(async () => {
    setRetrying(true)
    try { await fetchSkills() } finally { setRetrying(false) }
  }, [fetchSkills])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return skills
    return skills.filter((s) => s.name.toLowerCase().includes(q) || (s.description ?? '').toLowerCase().includes(q))
  }, [skills, query])

  const handleToggle = async (skill: SkillInfo, enabled: boolean) => {
    if (!skill.id) return
    const ok = await toggleSkill(skill.id, enabled)
    if (!ok) toast.error(t('toast.updateFailed'), skill.name)
  }

  const confirmDelete = async () => {
    if (!deleting?.id) return
    setBusy(true)
    try {
      await deleteSkill(deleting.id)
      toast.success(t('toast.deleted'), deleting.name)
      setDeleting(null)
    } catch (err) {
      toast.error(t('toast.deleteFailed'), err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  const activeCount = skills.filter((s) => s.enabled !== false && s.status !== 'archived').length
  const missingRuntimes = runtimes && !runtimes.ready ? runtimes.runtimes.filter((r) => !r.available).map((r) => r.name).join(', ') : ''

  let body
  if (error && skills.length === 0) {
    body = (
      <ErrorState
        title={t('screen.errorTitle')}
        description={t('screen.errorDescription')}
        onRetry={() => void retry()}
        retrying={retrying}
      />
    )
  } else if (loading) {
    body = <SkeletonList rows={5} label={t('screen.loading')} />
  } else if (skills.length === 0) {
    body = (
      <EmptyState
        icon={<IconStar size={20} />}
        title={t('emptyTitle')}
        description={t('emptyDescription')}
        action={<Button variant="secondary" onClick={() => setAddOpen(true)} disabled={disconnected}>{t('screen.emptyAction')}</Button>}
      />
    )
  } else {
    body = (
      <div className="flex flex-col gap-3">
        <div className="relative w-full max-w-72">
          <IconSearch size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-muted" />
          <Input
            type="search"
            aria-label={t('searchPlaceholder')}
            placeholder={t('searchPlaceholder')}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="pl-8"
          />
        </div>
        {filtered.length === 0 ? (
          <EmptyState
            title={t('noMatchTitle')}
            description={t('noMatchDescription')}
            action={<Button variant="secondary" onClick={() => setQuery('')}>{t('screen.clearSearch')}</Button>}
          />
        ) : (
          <SkillTable
            skills={filtered}
            readOnly={disconnected}
            onToggle={(s, v) => void handleToggle(s, v)}
            onEdit={setEditing}
            onDelete={setDeleting}
          />
        )}
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title={t('title')}
        description={!loading && !error ? t('screen.summary', { count: skills.length, active: activeCount }) : t('description')}
        actions={(
          <Button variant="primary" icon={<IconPlus size={16} />} onClick={() => setAddOpen(true)} disabled={atLimit || disconnected}>
            {t('screen.addSkill')}
          </Button>
        )}
      />
      <div className={cn('min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4', disconnected && 'opacity-60')}>
        {atLimit && <p className="mb-3 text-caption text-ink-muted">{t('screen.limit', { max: MAX_SKILLS_LITE })}</p>}
        {missingRuntimes && (
          <div role="alert" className="mb-3 rounded-md border border-warning bg-warning-subtle p-3">
            <p className="text-ui font-medium text-warning">{t('deps.runtimeMissing')}</p>
            <p className="mt-0.5 text-caption text-ink-secondary">{t('deps.runtimeMissingDesc')} {missingRuntimes}</p>
          </div>
        )}
        {body}
      </div>

      {addOpen && (
        <SkillAddDialog
          onClose={() => setAddOpen(false)}
          onUpload={uploadSkill}
        />
      )}

      {editing && (
        <SkillEditDialog
          skill={editing}
          onClose={() => setEditing(null)}
          onSave={updateSkill}
        />
      )}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => { if (!o && !busy) setDeleting(null) }}
        title={t('delete.title')}
        description={t('delete.description', { name: deleting?.name ?? '' })}
        confirmLabel={t('delete.confirmLabel')}
        cancelLabel={t('edit.cancel')}
        variant="destructive"
        confirmValue={deleting?.name}
        loading={busy}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  )
}
