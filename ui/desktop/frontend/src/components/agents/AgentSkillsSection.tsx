import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { getApiClient, isApiClientReady } from '../../lib/api'
import { Badge, Switch } from '../ui'

interface SkillWithGrant {
  id: string
  name: string
  slug: string
  description: string
  visibility: string
  version: number
  granted: boolean
  pinned_version?: number
  is_system: boolean
}

interface AgentSkillsSectionProps {
  agentId: string
}

export function AgentSkillsSection({ agentId }: AgentSkillsSectionProps) {
  const { t } = useTranslation('agents')
  const [skills, setSkills] = useState<SkillWithGrant[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchSkills = useCallback(async () => {
    if (!isApiClientReady()) return
    try {
      const res = await getApiClient().get<{ skills: SkillWithGrant[] | null }>(`/v1/agents/${agentId}/skills`)
      setSkills(res.skills ?? [])
    } catch (err) {
      console.error('Failed to fetch agent skills:', err)
    } finally {
      setLoading(false)
    }
  }, [agentId])

  useEffect(() => { fetchSkills() }, [fetchSkills])

  async function handleToggle(skill: SkillWithGrant) {
    const newGranted = !skill.granted
    // Optimistic update
    setSkills((prev) => prev.map((s) => s.id === skill.id ? { ...s, granted: newGranted } : s))
    try {
      if (newGranted) {
        await getApiClient().post(`/v1/skills/${skill.id}/grants/agent`, { agent_id: agentId })
      } else {
        await getApiClient().delete(`/v1/skills/${skill.id}/grants/agent/${agentId}`)
      }
    } catch (err) {
      setError((err as Error).message || 'Failed to update grant')
      setSkills((prev) => prev.map((s) => s.id === skill.id ? { ...s, granted: !newGranted } : s))
    }
  }

  const granted = skills.filter((s) => s.granted || s.is_system).length

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-ui font-semibold text-ink">{t('detail.skills')}</h3>
        {!loading && skills.length > 0 && (
          <span className="text-caption text-ink-muted">
            {t('skills.skillsGranted', { granted, total: skills.length })}
          </span>
        )}
      </div>

      {error && <p className="text-caption text-danger">{error}</p>}

      {loading ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i}  />
          ))}
        </div>
      ) : skills.length === 0 ? (
        <p className="text-caption text-ink-muted py-3 text-center">{t('skills.noSkillsAvailable')}</p>
      ) : (
        <div className="rounded-lg border border-border-subtle divide-y divide-border">
          {skills.map((skill) => (
            <div key={skill.id} className="flex items-center justify-between px-3 py-2 hover:bg-surface-hover transition-colors">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <svg className="h-3.5 w-3.5 text-ink-muted shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                    <path d="M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z" />
                  </svg>
                  <span className="text-caption font-medium text-ink truncate">{skill.name}</span>
                  {skill.is_system && (
                    <Badge tone="info">{t('skills.system')}</Badge>
                  )}
                </div>
                {skill.description && (
                  <p className="text-caption text-ink-muted truncate ml-5">{skill.description}</p>
                )}
              </div>
              {skill.is_system ? (
                <span className="text-caption text-ink-muted shrink-0">{t('skills.alwaysAvailable')}</span>
              ) : (
                <Switch
                  checked={skill.granted}
                  onCheckedChange={() => handleToggle(skill)}
                />
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
