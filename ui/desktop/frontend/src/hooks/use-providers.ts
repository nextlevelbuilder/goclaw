import { useState, useEffect, useCallback } from 'react'
import { providerService } from '../services/provider-service'
import { toast } from '../stores/toast-store'
import type { ProviderData, ProviderInput } from '../types/provider'

export function useProviders() {
  const [providers, setProviders] = useState<ProviderData[]>([])
  const [loading, setLoading] = useState(true)

  const fetchProviders = useCallback(async () => {
    try {
      const res = await providerService.list()
      setProviders(res.providers ?? [])
    } catch (err) {
      console.error('Failed to fetch providers:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchProviders() }, [fetchProviders])

  const createProvider = useCallback(async (input: ProviderInput) => {
    try {
      const res = await providerService.create(input)
      setProviders((prev) => [...prev, res])
      toast.success('Provider created')
      return res
    } catch (err) {
      toast.error('Failed to create provider', (err as Error).message)
      throw err
    }
  }, [])

  const updateProvider = useCallback(async (id: string, input: Partial<ProviderInput>) => {
    try {
      const res = await providerService.update(id, input)
      setProviders((prev) => prev.map((p) => p.id === id ? res : p))
      toast.success('Provider updated')
      return res
    } catch (err) {
      toast.error('Failed to update provider', (err as Error).message)
      throw err
    }
  }, [])

  const deleteProvider = useCallback(async (id: string) => {
    try {
      await providerService.delete(id)
      setProviders((prev) => prev.filter((p) => p.id !== id))
      toast.success('Provider deleted')
    } catch (err) {
      toast.error('Failed to delete provider', (err as Error).message)
      throw err
    }
  }, [])

  const verifyProvider = useCallback(async (input: { provider_type: string; api_base?: string; api_key?: string }) => {
    return providerService.verify(input)
  }, [])

  return { providers, loading, fetchProviders, createProvider, updateProvider, deleteProvider, verifyProvider }
}
