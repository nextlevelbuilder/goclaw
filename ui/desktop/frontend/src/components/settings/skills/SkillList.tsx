import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useSkills, MAX_SKILLS_LITE } from '../../../hooks/use-skills'
import type { RuntimeStatus } from '../../../hooks/use-skills'
import { Switch } from '../../common/Switch'
import { RefreshButton } from '../../common/RefreshButton'
import { ConfirmDeleteDialog } from '../../common/ConfirmDeleteDialog'
import type { SkillInfo } from '../../../types/skill'

export function SkillList() {
  const { t } = useTranslation(['skills', 'common'])
  const { skills, loading, atLimit, fetchSkills, toggleSkill, uploadSkill, checkRuntimes, deleteSkill } = useSkills()
  const [runtimes, setRuntimes] = useState<RuntimeStatus | null>(null)
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<SkillInfo | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    checkRuntimes().then((r) => { if (r) setRuntimes(r) }).catch(() => {})
  }, [checkRuntimes])

  async function handleUpload(file: File) {
    setUploading(true)
    setUploadError('')
    try {
      const result = await uploadSkill(file)
      if (result.deps_warning) setUploadError(result.deps_warning)
    } catch (err) {
      setUploadError((err as Error).message || t('upload.failed'))
    } finally {
      setUploading(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">{t('title')}</h2>
          <p className="text-xs text-text-muted mt-0.5">{t('description')} (max {MAX_SKILLS_LITE})</p>
        </div>
        <div className="flex items-center gap-2">
          <input
            ref={fileRef}
            type="file"
            accept=".zip"
            className="hidden"
            onChange={(e) => { if (e.target.files?.[0]) handleUpload(e.target.files[0]) }}
          />
          <button
            onClick={() => fileRef.current?.click()}
            disabled={atLimit || uploading}
            className="bg-accent text-white rounded-lg px-3 py-1.5 text-xs hover:bg-accent-hover disabled:opacity-50 transition-colors flex items-center gap-1.5"
          >
            {uploading ? (
              <>
                <svg className="h-3.5 w-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
                {t('upload.uploading')}
              </>
            ) : (
              <>
                <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                  <polyline points="17 8 12 3 7 8" /><line x1="12" y1="3" x2="12" y2="15" />
                </svg>
                {t('upload.button')}
              </>
            )}
          </button>
          <RefreshButton onRefresh={fetchSkills} />
        </div>
      </div>

      {atLimit && (
        <p className="text-[11px] text-amber-600 dark:text-amber-400">
          {t('deps.runtimeMissing')}
        </p>
      )}

      {uploadError && (
        <p className="text-xs text-error">{uploadError}</p>
      )}

      {/* Runtime status */}
      {runtimes && !runtimes.ready && (
        <div className="rounded-lg border border-amber-500/20 bg-amber-500/5 p-3 flex items-start gap-2">
          <svg className="h-4 w-4 text-amber-600 dark:text-amber-400 shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" />
            <line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
          <div>
            <p className="text-xs font-medium text-amber-600 dark:text-amber-400">{t('deps.runtimeMissing')}</p>
            <p className="text-[11px] text-text-muted mt-0.5">
              {t('deps.runtimeMissingDesc')}{' '}
              {runtimes.runtimes.filter((r) => !r.available).map((r) => r.name).join(', ')}
            </p>
          </div>
        </div>
      )}

      {/* Loading */}
      {loading ? (
        <div className="space-y-2">
          {[1, 2, 3, 4, 5].map((i) => (
            <div key={i} className="h-12 rounded-lg bg-surface-tertiary/50 animate-pulse" />
          ))}
        </div>
      ) : skills.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-12">
          <svg className="h-10 w-10 text-text-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
            <path d="M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z" />
          </svg>
          <p className="text-sm text-text-muted">{t('emptyTitle')}</p>
          <p className="text-xs text-text-muted/70">{t('emptyDescription')}</p>
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-surface-tertiary/40">
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.name')}</th>
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.description')}</th>
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.status')}</th>
                <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">{t('columns.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {skills.map((skill) => (
                <SkillRow
                  key={skill.id ?? skill.name}
                  skill={skill}
                  onToggle={toggleSkill}
                  onDelete={() => setDeleteTarget(skill)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Delete confirmation */}
      {deleteTarget && (
        <ConfirmDeleteDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title={t('delete.title')}
          description={t('delete.description', { name: deleteTarget.name })}
          confirmValue={deleteTarget.name}
          onConfirm={async () => {
            if (deleteTarget.id) await deleteSkill(deleteTarget.id)
            setDeleteTarget(null)
          }}
        />
      )}
    </div>
  )
}

/* --- SkillRow --- */

function SkillRow({ skill, onToggle, onDelete }: {
  skill: SkillInfo
  onToggle: (id: string, enabled: boolean) => void
  onDelete: () => void
}) {
  const { t } = useTranslation('skills')
  const isArchived = skill.status === 'archived'
  const isDisabled = !skill.enabled || isArchived
  const hasMissing = (skill.missing_deps?.length ?? 0) > 0

  return (
    <tr className={`border-b border-border last:border-0 hover:bg-surface-tertiary/30 transition-colors ${isDisabled ? 'opacity-60' : ''}`}>
      {/* Name */}
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <svg className="h-4 w-4 text-text-muted shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z" />
          </svg>
          <span className="font-medium text-text-primary">{skill.name}</span>
          {skill.is_system && (
            <span className="rounded-full px-1.5 py-0.5 text-[10px] font-medium bg-blue-500/10 text-blue-600 border border-blue-500/20 dark:text-blue-400 dark:bg-blue-500/5 dark:border-blue-500/15">
              {t('system')}
            </span>
          )}
          {(skill.version ?? 0) > 0 && (
            <span className="text-xs text-text-muted">v{skill.version}</span>
          )}
        </div>
      </td>

      {/* Description */}
      <td className="px-4 py-3 max-w-xs truncate text-text-muted">
        {skill.description || '—'}
      </td>

      {/* Status */}
      <td className="px-4 py-3">
        <div>
          <span className={`inline-block rounded-full px-2 py-0.5 text-[10px] font-medium border ${
            isArchived
              ? 'bg-amber-500/10 text-amber-600 border-amber-500/20 dark:text-amber-400 dark:bg-amber-500/5 dark:border-amber-500/15'
              : 'bg-emerald-500/15 text-emerald-700 border-emerald-500/25 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/20'
          }`}>
            {isArchived ? t('deps.statusArchived') : t('deps.statusActive')}
          </span>
          {hasMissing && (
            <p className="text-[10px] text-amber-600 dark:text-amber-400 mt-0.5">
              {skill.missing_deps!.slice(0, 3).join(', ')}
              {skill.missing_deps!.length > 3 && ` +${skill.missing_deps!.length - 3}`}
            </p>
          )}
        </div>
      </td>

      {/* Actions */}
      <td className="px-4 py-3">
        <div className="flex items-center justify-end gap-1.5">
          {/* Delete (non-system only) */}
          {!skill.is_system && skill.id && (
            <button
              onClick={onDelete}
              className="p-1 text-text-muted hover:text-error transition-colors"
              title={t('delete.title')}
            >
              <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                <polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
              </svg>
            </button>
          )}
          <Switch
            checked={skill.enabled !== false}
            onCheckedChange={(v) => skill.id && onToggle(skill.id, v)}
            disabled={!skill.id}
          />
        </div>
      </td>
    </tr>
  )
}
