import { useState, useEffect, useCallback } from 'react'
import i18n from '../i18n'
import { cronService, type CronJobPatch } from '../services/cron-service'
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
  const [error, setError] = useState<Error | null>(null)

  const fetchJobs = useCallback(async () => {
    try {
      const res = await cronService.list(agentId)
      setJobs(res.jobs ?? [])
      setError(null)
    } catch (err) {
      console.error('Failed to fetch cron jobs:', err)
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setLoading(false)
    }
  }, [agentId])

  useEffect(() => { fetchJobs() }, [fetchJobs])

  const createJob = useCallback(async (params: CreateJobParams) => {
    try {
      const job = await cronService.create(params)
      setJobs((prev) => [...prev, job])
      toast.success(i18n.t('cron:toast.created'), i18n.t('cron:toast.createdDesc', { name: params.name }))
      return job
    } catch (err) {
      toast.error(i18n.t('cron:toast.failedCreate'), (err as Error).message)
      throw err
    }
  }, [])

  const updateJob = useCallback(async (jobId: string, patch: CronJobPatch) => {
    try {
      const job = await cronService.update(jobId, patch)
      setJobs((prev) => prev.map((j) => (j.id === jobId ? { ...j, ...job } : j)))
      toast.success(i18n.t('cron:toast.updated'))
      return job
    } catch (err) {
      toast.error(i18n.t('cron:toast.failedUpdate'), (err as Error).message)
      throw err
    }
  }, [])

  const deleteJob = useCallback(async (jobId: string) => {
    try {
      await cronService.delete(jobId)
      setJobs((prev) => prev.filter((j) => j.id !== jobId))
      toast.success(i18n.t('cron:toast.deleted'))
    } catch (err) {
      toast.error(i18n.t('cron:toast.failedDelete'), (err as Error).message)
      throw err
    }
  }, [])

  const toggleJob = useCallback(async (jobId: string) => {
    try {
      const res = await cronService.toggle(jobId)
      setJobs((prev) => prev.map((j) => j.id === jobId ? { ...j, enabled: res.enabled } : j))
      toast.success(i18n.t(res.enabled ? 'cron:toast.enabled' : 'cron:toast.disabled'))
    } catch (err) {
      toast.error(i18n.t('cron:toast.failedToggle'), (err as Error).message)
      throw err
    }
  }, [])

  const runJob = useCallback(async (jobId: string) => {
    try {
      await cronService.run(jobId)
      toast.success(i18n.t('cron:toast.triggered'))
    } catch (err) {
      toast.error(i18n.t('cron:toast.failedRun'), (err as Error).message)
      throw err
    }
  }, [])

  const fetchRuns = useCallback(async (jobId: string): Promise<CronRunLog[]> => {
    const res = await cronService.runs(jobId)
    return res.runs ?? []
  }, [])

  return { jobs, loading, error, fetchJobs, createJob, updateJob, deleteJob, toggleJob, runJob, fetchRuns }
}
