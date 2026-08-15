// Cron service — wraps WS cron.* calls
import { getWsClient } from '../lib/ws'
import type { CronJob, CronRunLog, CronSchedule, CronPayload } from '../types/cron'

export interface CreateJobParams {
  name: string
  agentId?: string
  schedule: CronSchedule
  payload: CronPayload
  deleteAfterRun?: boolean
}

export const cronService = {
  list(agentId?: string): Promise<{ jobs: CronJob[] | null }> {
    const params: Record<string, unknown> = {}
    if (agentId) params.agentId = agentId
    return getWsClient().call('cron.list', params) as Promise<{ jobs: CronJob[] | null }>
  },

  create(params: CreateJobParams): Promise<CronJob> {
    return getWsClient().call('cron.create', params as unknown as Record<string, unknown>) as Promise<CronJob>
  },

  delete(jobId: string): Promise<unknown> {
    return getWsClient().call('cron.delete', { jobId })
  },

  toggle(jobId: string): Promise<{ enabled: boolean }> {
    return getWsClient().call('cron.toggle', { jobId }) as Promise<{ enabled: boolean }>
  },

  run(jobId: string): Promise<unknown> {
    return getWsClient().call('cron.run', { jobId })
  },

  runs(jobId: string, limit = 20): Promise<{ runs: CronRunLog[] | null }> {
    return getWsClient().call('cron.runs', { jobId, limit }) as Promise<{ runs: CronRunLog[] | null }>
  },
}
