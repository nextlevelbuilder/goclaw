// Skill table row with status badge, toggle switch, and delete action.

import { useTranslation } from 'react-i18next'
import { Switch } from '../common/Switch'
import type { SkillInfo } from '../../types/skill'

interface SkillCardProps {
  skill: SkillInfo
  onToggle: (id: string, enabled: boolean) => void
  onDelete: () => void
}

export function SkillCard({ skill, onToggle, onDelete }: SkillCardProps) {
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
