import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { getApiClient, isApiClientReady } from '../../../lib/api'
import { Switch } from '../../common/Switch'

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
        <h3 className="text-sm font-semibold text-text-primary">{t('detail.skills')}</h3>
        {!loading && skills.length > 0 && (
          <span className="text-[11px] text-text-muted">
            {t('skills.skillsGranted', { granted, total: skills.length })}
          </span>
        )}
      </div>

      {error && <p className="text-xs text-error">{error}</p>}

      {loading ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-9 rounded-lg bg-surface-tertiary/50 animate-pulse" />
          ))}
        </div>
      ) : skills.length === 0 ? (
        <p className="text-xs text-text-muted py-3 text-center">{t('skills.noSkillsAvailable')}</p>
      ) : (
        <div className="rounded-lg border border-border divide-y divide-border">
          {skills.map((skill) => (
            <div key={skill.id} className="flex items-center justify-between px-3 py-2 hover:bg-surface-tertiary/30 transition-colors">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <svg className="h-3.5 w-3.5 text-text-muted shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                    <path d="M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z" />
                  </svg>
                  <span className="text-xs font-medium text-text-primary truncate">{skill.name}</span>
                  {skill.is_system && (
                    <span className="rounded-full px-1.5 py-0.5 text-[9px] font-medium bg-blue-500/10 text-blue-600 border border-blue-500/20 dark:text-blue-400 dark:bg-blue-500/5 dark:border-blue-500/15 shrink-0">
                      System
                    </span>
                  )}
                </div>
                {skill.description && (
                  <p className="text-[11px] text-text-muted truncate ml-5">{skill.description}</p>
                )}
              </div>
              {skill.is_system ? (
                <span className="text-[10px] text-text-muted shrink-0">{t('skills.alwaysAvailable')}</span>
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
