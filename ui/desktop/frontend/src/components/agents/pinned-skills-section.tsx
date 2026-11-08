import { useTranslation } from 'react-i18next'
import { useAgentSkills } from '../../hooks/use-agent-skills'
import { Select, Skeleton } from '../ui'

const MAX_PINNED = 10

interface PinnedSkillsSectionProps {
  agentId: string
  pinned: string[]
  onPinnedChange: (pinned: string[]) => void
}

export function PinnedSkillsSection({ agentId, pinned, onPinnedChange }: PinnedSkillsSectionProps) {
  const { t } = useTranslation('agents')
  const { skills, loading } = useAgentSkills(agentId)
  const grantedSkills = skills.filter((s) => s.granted)
  const availableSkills = grantedSkills.filter((s) => !pinned.includes(s.slug))

  const skillName = (slug: string): string => {
    const found = skills.find((s) => s.slug === slug)
    return found?.name ?? slug
  }

  if (loading) return <Skeleton className="h-16 w-full" />

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <span className="text-ui">📌</span>
        <h3 className="text-ui font-semibold text-ink">{t('detail.pinnedSkills.title')}</h3>
        <span className="text-caption text-ink-muted">({pinned.length}/{MAX_PINNED})</span>
      </div>

      {pinned.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {pinned.map((slug) => (
            <span
              key={slug}
              onClick={() => onPinnedChange(pinned.filter((s) => s !== slug))}
              className="inline-flex items-center gap-1 px-2 py-0.5 text-caption rounded-md bg-surface-hover text-ink-secondary cursor-pointer hover:bg-danger/10 hover:text-danger transition-colors"
            >
              {skillName(slug)}
              <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                <path d="M18 6L6 18M6 6l12 12" />
              </svg>
            </span>
          ))}
        </div>
      )}

      {pinned.length < MAX_PINNED && availableSkills.length > 0 && (
        <Select
          value=""
          onChange={(e) => {
            if (e.target.value) onPinnedChange([...pinned, e.target.value])
            e.target.value = ''
          }}
          className="w-full"
        >
          <option value="">{t('detail.pinnedSkills.addPlaceholder')}</option>
          {availableSkills.map((s) => (
            <option key={s.slug} value={s.slug}>{s.name}</option>
          ))}
        </Select>
      )}

      <p className="text-caption text-ink-muted">{t('detail.pinnedSkills.hint')}</p>
    </div>
  )
}
