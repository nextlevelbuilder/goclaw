/** Team data types — simplified for desktop lite edition */

export interface TeamData {
  id: string
  name: string
  description?: string
  lead_agent_id: string
  lead_agent_key?: string
  lead_display_name?: string
  status: 'active' | 'archived'
  settings?: Record<string, unknown>
  member_count?: number
  members?: TeamMemberData[]
  created_at?: string
}

export interface TeamMemberData {
  team_id: string
  agent_id: string
  agent_key?: string
  display_name?: string
  emoji?: string
  role: 'lead' | 'member' | 'reviewer'
}

export type TaskStatus = 'pending' | 'blocked' | 'in_progress' | 'completed' | 'failed' | 'cancelled'

export interface TeamTaskData {
  id: string
  team_id: string
  task_number?: number
  identifier?: string
  task_type?: string
  subject: string
  description?: string
  result?: string
  status: TaskStatus
  priority: number
  owner_agent_id?: string
  owner_agent_key?: string
  blocked_by?: string[]
  progress_percent?: number
  progress_step?: string
  locked_at?: string
  lock_expires_at?: string
  comment_count?: number
  attachment_count?: number
  created_at?: string
  updated_at?: string
}

export interface TeamTaskAttachment {
  id: string
  task_id: string
  team_id: string
  path: string
  file_size: number
  mime_type?: string
  created_at: string
  download_url?: string
}

/** Notification config stored in team.settings.notifications */
export interface TeamNotifyConfig {
  dispatched?: boolean
  progress?: boolean
  failed?: boolean
  completed?: boolean
  new_task?: boolean
  mode?: 'direct' | 'leader'
}

/** All kanban column statuses in display order */
export const KANBAN_STATUSES: TaskStatus[] = [
  'pending', 'blocked', 'in_progress', 'completed', 'failed', 'cancelled',
]

/** Status dot color classes */
export const STATUS_COLORS: Record<TaskStatus, string> = {
  pending: 'bg-ink-muted',
  blocked: 'bg-warning',
  in_progress: 'bg-info',
  completed: 'bg-success',
  failed: 'bg-danger',
  cancelled: 'bg-ink-muted',
}

/** Tone of the status chip for each task status. */
export const STATUS_TONE: Record<TaskStatus, 'success' | 'warning' | 'danger' | 'info' | 'idle'> = {
  pending: 'idle',
  blocked: 'warning',
  in_progress: 'info',
  completed: 'success',
  failed: 'danger',
  cancelled: 'idle',
}

/** Priority badge classes */
export const PRIORITY_BADGE: Record<number, { label: string; cls: string }> = {
  0: { label: 'P-0', cls: 'bg-danger-subtle text-danger' },
  1: { label: 'P-1', cls: 'bg-warning-subtle text-warning' },
  2: { label: 'P-2', cls: 'bg-info-subtle text-info' },
  3: { label: 'P-3', cls: 'bg-surface-hover text-ink-secondary' },
}

/** Terminal task statuses (no further state transitions) */
export const TERMINAL_STATUSES: Set<TaskStatus> = new Set(['completed', 'failed', 'cancelled'])

/** Check if agent is actively running on a task */
export function isTaskLocked(task: TeamTaskData): boolean {
  if (!task.locked_at) return false
  const expiry = task.lock_expires_at ? new Date(task.lock_expires_at) : null
  return !expiry || expiry > new Date()
}

/** Group tasks by status for kanban columns */
export function groupByStatus(tasks: TeamTaskData[]): Map<TaskStatus, TeamTaskData[]> {
  const map = new Map<TaskStatus, TeamTaskData[]>()
  for (const s of KANBAN_STATUSES) map.set(s, [])
  for (const t of tasks) {
    const arr = map.get(t.status)
    if (arr) arr.push(t)
  }
  return map
}
