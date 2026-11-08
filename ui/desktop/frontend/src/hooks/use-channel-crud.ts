import { useState, useEffect, useCallback } from 'react'
import { getApiClient } from '../lib/api'
import { toast } from '../stores/toast-store'
import i18n from '../i18n'
import type { ChannelInstanceData, ChannelInstanceInput } from '../types/channel'

export function useChannelCrud() {
  const [instances, setInstances] = useState<ChannelInstanceData[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  const fetchInstances = useCallback(async () => {
    setError(false)
    try {
      const res = await getApiClient().get<{ instances: ChannelInstanceData[] | null }>('/v1/channels/instances')
      setInstances(res.instances ?? [])
    } catch (err) {
      console.error('Failed to fetch channel instances:', err)
      setError(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchInstances() }, [fetchInstances])

  const createInstance = useCallback(async (input: ChannelInstanceInput) => {
    try {
      // Map camelCase input to snake_case for backend HTTP API
      const payload = {
        name: input.name,
        display_name: input.displayName,
        channel_type: input.channelType,
        agent_id: input.agentId,
        credentials: input.credentials,
        config: input.config,
        enabled: input.enabled,
      }
      const res = await getApiClient().post<{ id: string }>('/v1/channels/instances', payload)
      await fetchInstances()
      toast.success(i18n.t('channels:toast.created'))
      return res
    } catch (err) {
      toast.error(i18n.t('channels:toast.failedCreate'), (err as Error).message)
      throw err
    }
  }, [fetchInstances])

  const updateInstance = useCallback(async (id: string, data: Record<string, unknown>) => {
    try {
      await getApiClient().put(`/v1/channels/instances/${id}`, data)
      await fetchInstances()
      toast.success(i18n.t('channels:toast.updated'))
    } catch (err) {
      toast.error(i18n.t('channels:toast.failedUpdate'), (err as Error).message)
      throw err
    }
  }, [fetchInstances])

  const deleteInstance = useCallback(async (id: string) => {
    try {
      await getApiClient().delete(`/v1/channels/instances/${id}`)
      await fetchInstances()
      toast.success(i18n.t('channels:toast.deleted'))
    } catch (err) {
      toast.error(i18n.t('channels:toast.failedDelete'), (err as Error).message)
      throw err
    }
  }, [fetchInstances])

  const telegramExists = instances.some((i) => i.channel_type === 'telegram')
  const discordExists = instances.some((i) => i.channel_type === 'discord')
  const atLimit = instances.length >= 2

  return { instances, loading, error, atLimit, telegramExists, discordExists, fetchInstances, createInstance, updateInstance, deleteInstance }
}
