import { useState, useEffect, useCallback } from 'react'
import { getApiClient } from '../lib/api'
import { toast } from '../stores/toast-store'
import type { ChannelInstanceData, ChannelInstanceInput } from '../types/channel'

export function useChannelCrud() {
  const [instances, setInstances] = useState<ChannelInstanceData[]>([])
  const [loading, setLoading] = useState(true)

  const fetchInstances = useCallback(async () => {
    try {
      const res = await getApiClient().get<{ instances: ChannelInstanceData[] | null }>('/v1/channels/instances')
      setInstances(res.instances ?? [])
    } catch (err) {
      console.error('Failed to fetch channel instances:', err)
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
      toast.success('Channel created')
      return res
    } catch (err) {
      toast.error('Failed to create channel', (err as Error).message)
      throw err
    }
  }, [fetchInstances])

  const updateInstance = useCallback(async (id: string, data: Record<string, unknown>) => {
    try {
      await getApiClient().put(`/v1/channels/instances/${id}`, data)
      await fetchInstances()
      toast.success('Channel updated')
    } catch (err) {
      toast.error('Failed to update channel', (err as Error).message)
      throw err
    }
  }, [fetchInstances])

  const deleteInstance = useCallback(async (id: string) => {
    try {
      await getApiClient().delete(`/v1/channels/instances/${id}`)
      await fetchInstances()
      toast.success('Channel deleted')
    } catch (err) {
      toast.error('Failed to delete channel', (err as Error).message)
      throw err
    }
  }, [fetchInstances])

  const telegramExists = instances.some((i) => i.channel_type === 'telegram')
  const discordExists = instances.some((i) => i.channel_type === 'discord')
  const atLimit = instances.length >= 2

  return { instances, loading, atLimit, telegramExists, discordExists, fetchInstances, createInstance, updateInstance, deleteInstance }
}
