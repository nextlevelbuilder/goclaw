import { useState, useEffect, useCallback } from 'react'
import { getApiClient } from '../lib/api'
import { toast } from '../stores/toast-store'
import type { ChannelInstanceData, GroupManagerGroupInfo, GroupManagerData, ChannelContact } from '../types/channel'

export function useChannelDetail(instanceId: string | null) {
  const [instance, setInstance] = useState<ChannelInstanceData | null>(null)
  const [loading, setLoading] = useState(false)

  const fetchInstance = useCallback(async () => {
    if (!instanceId) return
    setLoading(true)
    try {
      const res = await getApiClient().get<ChannelInstanceData>(`/v1/channels/instances/${instanceId}`)
      setInstance(res)
    } catch (err) {
      console.error('Failed to fetch channel detail:', err)
    } finally {
      setLoading(false)
    }
  }, [instanceId])

  useEffect(() => { fetchInstance() }, [fetchInstance])

  const updateInstance = useCallback(async (updates: Record<string, unknown>) => {
    if (!instanceId) return
    try {
      await getApiClient().put(`/v1/channels/instances/${instanceId}`, updates)
      await fetchInstance()
      toast.success('Channel updated')
    } catch (err) {
      toast.error('Failed to update channel', (err as Error).message)
      throw err
    }
  }, [instanceId, fetchInstance])

  const listManagerGroups = useCallback(async (): Promise<GroupManagerGroupInfo[]> => {
    if (!instanceId) return []
    const res = await getApiClient().get<{ groups: GroupManagerGroupInfo[] }>(`/v1/channels/instances/${instanceId}/writers/groups`)
    return res.groups ?? []
  }, [instanceId])

  const listManagers = useCallback(async (groupId: string): Promise<GroupManagerData[]> => {
    if (!instanceId) return []
    const res = await getApiClient().get<{ writers: GroupManagerData[] }>(`/v1/channels/instances/${instanceId}/writers?group_id=${encodeURIComponent(groupId)}`)
    return res.writers ?? []
  }, [instanceId])

  const addManager = useCallback(async (groupId: string, userId: string, displayName?: string, username?: string) => {
    if (!instanceId) return
    await getApiClient().post(`/v1/channels/instances/${instanceId}/writers`, {
      group_id: groupId,
      user_id: userId,
      display_name: displayName ?? '',
      username: username ?? '',
    })
  }, [instanceId])

  const removeManager = useCallback(async (groupId: string, userId: string) => {
    if (!instanceId) return
    await getApiClient().delete(`/v1/channels/instances/${instanceId}/writers/${userId}?group_id=${encodeURIComponent(groupId)}`)
  }, [instanceId])

  const listContacts = useCallback(async (search: string): Promise<ChannelContact[]> => {
    const qs = new URLSearchParams({ limit: '20' })
    if (search) qs.set('search', search)
    const res = await getApiClient().get<{ contacts: ChannelContact[] }>(`/v1/contacts?${qs}`)
    return res.contacts ?? []
  }, [])

  return {
    instance, loading, updateInstance, refresh: fetchInstance,
    listManagerGroups, listManagers, addManager, removeManager, listContacts,
  }
}
