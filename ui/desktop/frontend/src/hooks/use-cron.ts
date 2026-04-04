import { useState, useEffect, useCallback } from 'react'
import { cronService } from '../services/cron-service'
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
      const res = await cronService.list(agentId)
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
      const job = await cronService.create(params)
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
      await cronService.delete(jobId)
      setJobs((prev) => prev.filter((j) => j.id !== jobId))
      toast.success('Cron job deleted')
    } catch (err) {
      toast.error('Failed to delete cron job', (err as Error).message)
      throw err
    }
  }, [])

  const toggleJob = useCallback(async (jobId: string) => {
    try {
      const res = await cronService.toggle(jobId)
      setJobs((prev) => prev.map((j) => j.id === jobId ? { ...j, enabled: res.enabled } : j))
    } catch (err) {
      toast.error('Failed to toggle cron job', (err as Error).message)
      throw err
    }
  }, [])

  const runJob = useCallback(async (jobId: string) => {
    try {
      await cronService.run(jobId)
      toast.success('Cron job triggered')
    } catch (err) {
      toast.error('Failed to run cron job', (err as Error).message)
      throw err
    }
  }, [])

  const fetchRuns = useCallback(async (jobId: string): Promise<CronRunLog[]> => {
    const res = await cronService.runs(jobId)
    return res.runs ?? []
  }, [])

  return { jobs, loading, fetchJobs, createJob, deleteJob, toggleJob, runJob, fetchRuns }
}
