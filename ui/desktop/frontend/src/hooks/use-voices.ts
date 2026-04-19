import { useState, useEffect, useCallback } from 'react'
import { listVoices, refreshVoices, type Voice } from '../services/voices'

interface UseVoicesResult {
  data: Voice[]
  isLoading: boolean
  error: string | null
  refetch: () => void
}

interface UseRefreshVoicesResult {
  mutate: () => Promise<void>
  isPending: boolean
}

export function useVoices(): UseVoicesResult {
  const [data, setData] = useState<Voice[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetch = useCallback(async () => {
    setIsLoading(true)
    setError(null)
    try {
      const voices = await listVoices()
      setData(voices)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load voices')
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => { fetch() }, [fetch])

  return { data, isLoading, error, refetch: fetch }
}

export function useRefreshVoices(): UseRefreshVoicesResult {
  const [isPending, setIsPending] = useState(false)

  const mutate = useCallback(async () => {
    setIsPending(true)
    try {
      await refreshVoices()
    } finally {
      setIsPending(false)
    }
  }, [])

  return { mutate, isPending }
}
