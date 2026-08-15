export interface CronJob {
  id: string
  name: string
  agentId: string
  enabled: boolean
  schedule: CronSchedule
  payload: CronPayload
  state: CronState
  deliver?: boolean
  deliverChannel?: string
  deliverTo?: string
  wakeHeartbeat?: boolean
  stateless?: boolean
  deleteAfterRun: boolean
  createdAtMs: number
  updatedAtMs: number
}

export interface CronSchedule {
  kind: 'every' | 'cron' | 'at'
  everyMs?: number
  expr?: string
  tz?: string
  atMs?: number
}

export interface CronPayload {
  kind: string
  message: string
  command?: string
}

export interface CronState {
  nextRunAtMs?: number
  lastRunAtMs?: number
  lastStatus?: string
  lastError?: string
}

export interface CronRunLog {
  ts: number
  jobId: string
  status: string
  error?: string
  summary?: string
  durationMs: number
  inputTokens: number
  outputTokens: number
}
