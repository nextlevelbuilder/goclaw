// Provider service — wraps HTTP /v1/providers calls
import { getApiClient } from '../lib/api'
import type { ProviderData, ProviderInput } from '../types/provider'

/** POST /v1/providers/{id}/verify response. */
export interface ProviderVerifyResult {
  valid: boolean
  error?: string
}

function parseVerifyResult(raw: unknown): ProviderVerifyResult {
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) {
    throw new Error('provider verify response must be an object')
  }
  const o = raw as Record<string, unknown>
  if (typeof o.valid !== 'boolean') throw new Error('provider verify response.valid must be a boolean')
  return { valid: o.valid, error: typeof o.error === 'string' && o.error ? o.error : undefined }
}

export const providerService = {
  list(): Promise<{ providers: ProviderData[] | null }> {
    return getApiClient().get<{ providers: ProviderData[] | null }>('/v1/providers')
  },

  create(input: ProviderInput): Promise<ProviderData> {
    return getApiClient().post<ProviderData>('/v1/providers', input)
  },

  update(id: string, input: Partial<ProviderInput>): Promise<ProviderData> {
    return getApiClient().put<ProviderData>(`/v1/providers/${encodeURIComponent(id)}`, input)
  },

  delete(id: string): Promise<void> {
    return getApiClient().delete<void>(`/v1/providers/${encodeURIComponent(id)}`)
  },

  /**
   * Tests a saved provider. With a model the gateway sends a minimal chat request;
   * without one it only checks that the provider is registered and reachable.
   */
  async verify(id: string, model?: string): Promise<ProviderVerifyResult> {
    const raw = await getApiClient().post<unknown>(
      `/v1/providers/${encodeURIComponent(id)}/verify`,
      model ? { model } : {},
    )
    return parseVerifyResult(raw)
  },
}
