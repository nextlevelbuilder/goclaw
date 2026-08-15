// HTTP API client for GoClaw REST endpoints

class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public code?: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

class ApiClient {
  private baseUrl: string
  private token: string

  constructor(baseUrl: string, token: string) {
    this.baseUrl = baseUrl.replace(/\/$/, '')
    this.token = token
  }

  private headers(extra?: Record<string, string>): Record<string, string> {
    // Send locale for i18n error messages from backend
    const lang = typeof localStorage !== 'undefined' ? localStorage.getItem('goclaw:language') : null
    return {
      Authorization: `Bearer ${this.token}`,
      'Content-Type': 'application/json',
      'X-GoClaw-User-Id': 'system',
      ...(lang ? { 'Accept-Language': lang } : {}),
      ...extra,
    }
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const url = `${this.baseUrl}${path}`
    const res = await fetch(url, {
      method,
      headers: this.headers(),
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })

    if (!res.ok) {
      let code: string | undefined
      let message = res.statusText
      try {
        const json = await res.json()
        // Backend sends either { error: "string" } or { error: { code, message } }
        if (typeof json.error === 'string') {
          message = json.error
        } else if (json.error && typeof json.error === 'object') {
          code = json.error.code
          message = json.error.message ?? message
        }
      } catch {
        // non-JSON error body
      }
      throw new ApiError(message, res.status, code)
    }

    if (res.status === 204) return undefined as T
    return res.json() as Promise<T>
  }

  async get<T>(path: string): Promise<T> {
    return this.request<T>('GET', path)
  }

  async getWithParams<T>(path: string, params?: Record<string, string>): Promise<T> {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return this.request<T>('GET', `${path}${qs}`)
  }

  async post<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>('POST', path, body)
  }

  async put<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>('PUT', path, body)
  }

  async patch<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>('PATCH', path, body)
  }

  async delete<T>(path: string): Promise<T> {
    return this.request<T>('DELETE', path)
  }

  getBaseUrl(): string {
    return this.baseUrl
  }

  /** Fetch a file with Bearer auth. Use for URLs without ?ft= token (e.g. media_refs). */
  async fetchFile(url: string): Promise<Response> {
    const fullUrl = url.startsWith('http') ? url : `${this.baseUrl}${url}`
    return fetch(fullUrl, {
      headers: { Authorization: `Bearer ${this.token}` },
    })
  }

  /** Sign a file path, returning a URL with ?ft= token for unauthenticated access. */
  async signFileUrl(filePath: string): Promise<string> {
    const res = await this.post<{ url: string }>('/v1/files/sign', { path: filePath })
    return `${this.baseUrl}${res.url}`
  }

  /** Fetch a file as Blob with Bearer auth. Used for image previews and downloads. */
  async fetchBlob(path: string, params?: Record<string, string>): Promise<Blob> {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    const url = `${this.baseUrl}${path}${qs}`
    const res = await fetch(url, {
      headers: { Authorization: `Bearer ${this.token}` },
    })
    if (!res.ok) throw new ApiError(res.statusText, res.status)
    return res.blob()
  }

  /** Fetch an SSE stream with Bearer auth. Used for storage size streaming. */
  async streamFetch(path: string, signal?: AbortSignal): Promise<Response> {
    const url = `${this.baseUrl}${path}`
    return fetch(url, {
      headers: { Authorization: `Bearer ${this.token}` },
      signal,
    })
  }

  /** Raw PUT request without JSON body (for query-param-only endpoints like /v1/storage/move). */
  async putRaw(path: string): Promise<void> {
    const url = `${this.baseUrl}${path}`
    const res = await fetch(url, {
      method: 'PUT',
      headers: {
        Authorization: `Bearer ${this.token}`,
        'X-GoClaw-User-Id': 'system',
      },
    })
    if (!res.ok) {
      let message = res.statusText
      try {
        const json = (await res.json()) as { error?: string | { message?: string } }
        message = typeof json.error === 'string' ? json.error : json.error?.message ?? message
      } catch { /* non-JSON */ }
      throw new ApiError(message, res.status)
    }
  }

  async uploadFile<T = { url: string }>(path: string, file: File): Promise<T> {
    const url = `${this.baseUrl}${path}`
    const form = new FormData()
    form.append('file', file)

    const res = await fetch(url, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${this.token}`,
        'X-GoClaw-User-Id': 'system',
      },
      body: form,
    })

    if (!res.ok) {
      let message = res.statusText
      try {
        const json = (await res.json()) as { error?: string | { message?: string } }
        message = typeof json.error === 'string' ? json.error : json.error?.message ?? message
      } catch { /* non-JSON */ }
      throw new ApiError(message, res.status)
    }
    return res.json() as Promise<T>
  }
}

// Singleton
let apiClient: ApiClient | null = null

export function getApiClient(): ApiClient {
  if (!apiClient) throw new Error('ApiClient not initialized — call initApiClient() first')
  return apiClient
}

/** Safe check — returns true if the API client has been initialized. */
export function isApiClientReady(): boolean {
  return apiClient !== null
}

export function initApiClient(baseUrl: string, token: string): ApiClient {
  apiClient = new ApiClient(baseUrl, token)
  return apiClient
}

export { ApiClient, ApiError }
