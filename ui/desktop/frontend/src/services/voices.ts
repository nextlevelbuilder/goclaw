import { getApiClient } from '../lib/api'

export interface Voice {
  voice_id: string
  name: string
  preview_url?: string
  labels?: Record<string, string>
  category?: string
}

interface VoicesResponse {
  voices: Voice[]
}

export async function listVoices(): Promise<Voice[]> {
  const res = await getApiClient().get<VoicesResponse>('/v1/voices')
  return res.voices ?? []
}

export async function refreshVoices(): Promise<void> {
  await getApiClient().post<{ status: string }>('/v1/voices/refresh')
}
