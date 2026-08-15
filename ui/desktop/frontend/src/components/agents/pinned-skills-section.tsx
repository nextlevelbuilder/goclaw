import { useTranslation } from 'react-i18next'
import { useAgentSkills } from '../../hooks/use-agent-skills'

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

  if (loading) return <div className="h-10 animate-pulse rounded-lg bg-surface-tertiary" />

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <span className="text-sm">📌</span>
        <h3 className="text-sm font-semibold text-text-primary">{t('detail.pinnedSkills.title')}</h3>
        <span className="text-[10px] text-text-muted">({pinned.length}/{MAX_PINNED})</span>
      </div>

      {pinned.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {pinned.map((slug) => (
            <span
              key={slug}
              onClick={() => onPinnedChange(pinned.filter((s) => s !== slug))}
              className="inline-flex items-center gap-1 px-2 py-0.5 text-xs rounded-md bg-surface-tertiary text-text-secondary cursor-pointer hover:bg-error/10 hover:text-error transition-colors"
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
        <select
          value=""
          onChange={(e) => {
            if (e.target.value) onPinnedChange([...pinned, e.target.value])
            e.target.value = ''
          }}
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
        >
          <option value="">{t('detail.pinnedSkills.addPlaceholder')}</option>
          {availableSkills.map((s) => (
            <option key={s.slug} value={s.slug}>{s.name}</option>
          ))}
        </select>
      )}

      <p className="text-[10px] text-text-muted">{t('detail.pinnedSkills.hint')}</p>
    </div>
  )
}
