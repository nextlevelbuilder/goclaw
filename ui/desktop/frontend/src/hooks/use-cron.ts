import { useState, useEffect, useCallback } from 'react'
import { getWsClient } from '../lib/ws'
import { toast } from '../stores/toast-store'
import type { CronJob, CronRunLog, CronSchedule, CronPayload } from '../types/cron'

interface CreateJobParams {
  name: string
  agentId?: string
  schedule: CronSchedule
  payload: CronPayload
  deleteAfterRun?: boolean
}

export function useCron(agentId?: string) {
  const [jobs, setJobs] = useState<CronJob[]>([])
  const [loading, setLoading] = useState(true)

  const fetchJobs = useCallback(async () => {
    try {
      const ws = getWsClient()
      const params: Record<string, unknown> = {}
      if (agentId) params.agentId = agentId
      const res = await ws.call('cron.list', params) as { jobs: CronJob[] | null }
      setJobs(res.jobs ?? [])
    } catch (err) {
      console.error('Failed to fetch cron jobs:', err)
    } finally {
      setLoading(false)
    }
  }, [agentId])

  useEffect(() => { fetchJobs() }, [fetchJobs])

  const createJob = useCallback(async (params: CreateJobParams) => {
    try {
      const ws = getWsClient()
      const job = await ws.call('cron.create', params as unknown as Record<string, unknown>) as CronJob
      setJobs((prev) => [...prev, job])
      toast.success('Cron job created', params.name)
      return job
    } catch (err) {
      toast.error('Failed to create cron job', (err as Error).message)
      throw err
    }
  }, [])

  const deleteJob = useCallback(async (jobId: string) => {
    try {
      const ws = getWsClient()
      await ws.call('cron.delete', { jobId })
      setJobs((prev) => prev.filter((j) => j.id !== jobId))
      toast.success('Cron job deleted')
    } catch (err) {
      toast.error('Failed to delete cron job', (err as Error).message)
      throw err
    }
  }, [])

  const toggleJob = useCallback(async (jobId: string) => {
    try {
      const ws = getWsClient()
      const res = await ws.call('cron.toggle', { jobId }) as { enabled: boolean }
      setJobs((prev) => prev.map((j) => j.id === jobId ? { ...j, enabled: res.enabled } : j))
    } catch (err) {
      toast.error('Failed to toggle cron job', (err as Error).message)
      throw err
    }
  }, [])

  const runJob = useCallback(async (jobId: string) => {
    try {
      const ws = getWsClient()
      await ws.call('cron.run', { jobId })
      toast.success('Cron job triggered')
    } catch (err) {
      toast.error('Failed to run cron job', (err as Error).message)
      throw err
    }
  }, [])

  const fetchRuns = useCallback(async (jobId: string): Promise<CronRunLog[]> => {
    const ws = getWsClient()
    const res = await ws.call('cron.runs', { jobId, limit: 20 }) as { runs: CronRunLog[] | null }
    return res.runs ?? []
  }, [])

  return { jobs, loading, fetchJobs, createJob, deleteJob, toggleJob, runJob, fetchRuns }
}
