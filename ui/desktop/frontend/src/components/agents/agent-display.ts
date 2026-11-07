import type { StatusTone } from '../ui'
import type { AgentData } from '../../types/agent'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export function isUuidKey(key: string): boolean {
  return UUID_RE.test(key)
}

/** Name shown for an agent; null when it has neither a display name nor a readable key. */
export function agentDisplayName(agent: AgentData): string | null {
  return agent.display_name || (isUuidKey(agent.agent_key) ? null : agent.agent_key)
}

export function agentEmoji(agent: AgentData): string {
  if (agent.emoji) return agent.emoji
  return typeof agent.other_config?.emoji === 'string' ? agent.other_config.emoji : ''
}

/** One-line summary of what the agent does (expertise summary, then personality). */
export function agentSummary(agent: AgentData): string {
  return String(agent.frontmatter || agent.agent_description || '').replace(/\s+/g, ' ').trim()
}

export interface AgentStatusView {
  tone: StatusTone
  /** Key under agents:screen.status. */
  key: 'active' | 'inactive' | 'summoning' | 'failed'
}

export function agentStatusView(status: string): AgentStatusView {
  switch (status) {
    case 'active':
    case 'running':
      return { tone: 'success', key: 'active' }
    case 'summoning':
      return { tone: 'info', key: 'summoning' }
    case 'summon_failed':
      return { tone: 'danger', key: 'failed' }
    default:
      return { tone: 'idle', key: 'inactive' }
  }
}

export function isAgentEnabled(agent: AgentData): boolean {
  return agent.status === 'active' || agent.status === 'running'
}
