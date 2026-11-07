import { useState, useEffect, useCallback } from 'react'
import i18n from '../i18n'
import { providerService, type ProviderVerifyResult } from '../services/provider-service'
import { toast } from '../stores/toast-store'
import type { ProviderData, ProviderInput } from '../types/provider'

export function useProviders() {
  const [providers, setProviders] = useState<ProviderData[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  const fetchProviders = useCallback(async () => {
    try {
      const res = await providerService.list()
      setProviders(res.providers ?? [])
      setError(null)
    } catch (err) {
      console.error('Failed to fetch providers:', err)
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchProviders() }, [fetchProviders])

  const createProvider = useCallback(async (input: ProviderInput) => {
    try {
      const res = await providerService.create(input)
      setProviders((prev) => [...prev, res])
      toast.success(i18n.t('providers:toast.created'), i18n.t('providers:toast.createdDesc', { name: res.display_name || res.name }))
      return res
    } catch (err) {
      toast.error(i18n.t('providers:toast.failedCreate'), (err as Error).message)
      throw err
    }
  }, [])

  const updateProvider = useCallback(async (id: string, input: Partial<ProviderInput>, quiet = false) => {
    try {
      const res = await providerService.update(id, input)
      setProviders((prev) => prev.map((p) => p.id === id ? res : p))
      if (!quiet) toast.success(i18n.t('providers:toast.updated'))
      return res
    } catch (err) {
      toast.error(i18n.t('providers:toast.failedUpdate'), (err as Error).message)
      throw err
    }
  }, [])

  const deleteProvider = useCallback(async (id: string) => {
    try {
      await providerService.delete(id)
      setProviders((prev) => prev.filter((p) => p.id !== id))
      toast.success(i18n.t('providers:toast.deleted'))
    } catch (err) {
      toast.error(i18n.t('providers:toast.failedDelete'), (err as Error).message)
      throw err
    }
  }, [])

  /** Tests a saved provider; pass a model to send a minimal chat request instead of a connectivity check. */
  const verifyProvider = useCallback((id: string, model?: string): Promise<ProviderVerifyResult> => {
    return providerService.verify(id, model)
  }, [])

  return { providers, loading, error, fetchProviders, createProvider, updateProvider, deleteProvider, verifyProvider }
}
