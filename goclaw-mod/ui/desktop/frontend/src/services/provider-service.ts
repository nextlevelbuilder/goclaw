// Provider service — wraps HTTP /v1/providers calls
import { getApiClient } from '../lib/api'
import type { ProviderData, ProviderInput } from '../types/provider'

export interface ProviderVerifyInput {
  provider_type: string
  api_base?: string
  api_key?: string
}

export interface ProviderVerifyResult {
  success: boolean
  error?: string
  models?: string[]
}

export const providerService = {
  list(): Promise<{ providers: ProviderData[] | null }> {
    return getApiClient().get<{ providers: ProviderData[] | null }>('/v1/providers')
  },

  create(input: ProviderInput): Promise<ProviderData> {
    return getApiClient().post<ProviderData>('/v1/providers', input)
  },

  update(id: string, input: Partial<ProviderInput>): Promise<ProviderData> {
    return getApiClient().put<ProviderData>(`/v1/providers/${id}`, input)
  },

  delete(id: string): Promise<void> {
    return getApiClient().delete<void>(`/v1/providers/${id}`)
  },

  verify(input: ProviderVerifyInput): Promise<ProviderVerifyResult> {
    return getApiClient().post<ProviderVerifyResult>('/v1/providers/verify', input)
  },
}
