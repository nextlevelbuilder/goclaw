import { useTranslation } from 'react-i18next'
import { IconStar, IconTrash } from '../common/Icons'
import { Badge, DataTable, Menu, StatusChip, Switch, type DataTableColumn } from '../ui'
import type { SkillInfo } from '../../types/skill'

interface SkillTableProps {
  skills: SkillInfo[]
  /** Disables writes while the gateway is unreachable. */
  readOnly?: boolean
  onToggle: (skill: SkillInfo, enabled: boolean) => void
  onEdit: (skill: SkillInfo) => void
  onDelete: (skill: SkillInfo) => void
}

/** Skills table: name, description, status, enable switch and a row menu. System skills cannot be edited or deleted. */
export function SkillTable({ skills, readOnly = false, onToggle, onEdit, onDelete }: SkillTableProps) {
  const { t } = useTranslation('skills')

  const columns: DataTableColumn<SkillInfo>[] = [
    {
      id: 'name',
      header: t('columns.name'),
      className: 'min-w-[200px]',
      cell: (skill) => (
        <div className="flex min-w-0 items-center gap-2">
          <IconStar size={14} className="shrink-0 text-ink-muted" />
          <span className="truncate text-ui font-semibold text-ink">{skill.name}</span>
          {skill.is_system && <Badge tone="info">{t('system')}</Badge>}
          {(skill.version ?? 0) > 0 && <span className="shrink-0 text-caption text-ink-muted">v{skill.version}</span>}
        </div>
      ),
    },
    {
      id: 'description',
      header: t('columns.description'),
      className: 'min-w-[200px]',
      cell: (skill) => (
        <span className="block max-w-sm truncate text-ui text-ink-secondary">{skill.description || t('noDescription')}</span>
      ),
    },
    {
      id: 'status',
      header: t('columns.status'),
      className: 'w-44',
      cell: (skill) => {
        const archived = skill.status === 'archived'
        const missing = skill.missing_deps ?? []
        return (
          <div className="flex flex-col items-start gap-0.5">
            <StatusChip tone={archived ? 'warning' : 'success'} label={archived ? t('deps.statusArchived') : t('deps.statusActive')} />
            {missing.length > 0 && (
              <span className="max-w-40 truncate text-caption text-warning">
                {missing.slice(0, 3).join(', ')}{missing.length > 3 ? ` +${missing.length - 3}` : ''}
              </span>
            )}
          </div>
        )
      },
    },
    {
      id: 'enabled',
      header: t('toggle.enable'),
      className: 'w-28',
      cell: (skill) => (
        <div onClick={(e) => e.stopPropagation()}>
          <Switch
            checked={skill.enabled !== false}
            onCheckedChange={(v) => onToggle(skill, v)}
            disabled={!skill.id || readOnly}
            aria-label={t('screen.enableLabel', { name: skill.name })}
          />
        </div>
      ),
    },
    {
      id: 'actions',
      header: <span className="sr-only">{t('columns.actions')}</span>,
      className: 'w-14 text-right',
      cell: (skill) => (
        skill.is_system || !skill.id ? null : (
          <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>
            <Menu
              label={t('screen.rowActions', { name: skill.name })}
              items={[
                { label: t('screen.editSkill'), onSelect: () => onEdit(skill), disabled: readOnly },
                'separator',
                {
                  label: t('screen.deleteSkill'),
                  icon: <IconTrash size={14} />,
                  destructive: true,
                  disabled: readOnly,
                  onSelect: () => onDelete(skill),
                },
              ]}
            />
          </div>
        )
      ),
    },
  ]

  return (
    <DataTable
      aria-label={t('title')}
      columns={columns}
      rows={skills}
      getRowId={(s) => s.id ?? s.name}
      onRowClick={(s) => { if (!s.is_system && s.id && !readOnly) onEdit(s) }}
    />
  )
}
