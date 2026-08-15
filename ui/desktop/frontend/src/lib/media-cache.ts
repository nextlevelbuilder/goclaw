import { getApiClient, isApiClientReady } from './api'

/** Client-side blob cache for file URLs.
 *  Supports both ?ft= signed token URLs and Bearer-auth URLs (media_refs).
 *  Cache key = clean path (without ?ft=), value = blob ObjectURL with 5-min TTL. */

const CACHE_TTL_MS = 5 * 60 * 1000 // 5 minutes, matches backend FileTokenTTL

interface CacheEntry {
  objectUrl: string
  expiresAt: number
}

const cache = new Map<string, CacheEntry>()
const inflight = new Map<string, Promise<string>>()

/** Strip ?ft=... or &ft=... from URL, return clean path for cache key. */
export function stripFt(url: string): string {
  return url.replace(/[?&]ft=[^&\s)"'<>]*/g, '').replace(/[?&]$/, '')
}

/** Evict expired entries and revoke their ObjectURLs. */
function evictExpired() {
  const now = Date.now()
  for (const [key, entry] of cache) {
    if (entry.expiresAt <= now) {
      URL.revokeObjectURL(entry.objectUrl)
      cache.delete(key)
    }
  }
}

/** Get cached ObjectURL or fetch blob and cache it.
 *  Returns the blob ObjectURL on success, or the original signed URL on failure. */
export async function getOrFetchUrl(signedUrl: string): Promise<string> {
  evictExpired()

  const key = stripFt(signedUrl)
  const cached = cache.get(key)
  if (cached && cached.expiresAt > Date.now()) {
    return cached.objectUrl
  }

  // Deduplicate concurrent fetches for the same key
  const existing = inflight.get(key)
  if (existing) return existing

  const promise = (async () => {
    try {
      // For URLs without ?ft= token, sign first via API then fetch
      let fetchUrl = signedUrl
      if (!signedUrl.includes('ft=') && isApiClientReady()) {
        // Extract absolute path from /v1/files/{path} URL
        const match = signedUrl.match(/\/v1\/files\/(.+?)(?:\?|$)/)
        if (match) {
          fetchUrl = await getApiClient().signFileUrl('/' + decodeURIComponent(match[1]))
        }
      }
      const res = await fetch(fetchUrl)
      if (!res.ok) return signedUrl
      const blob = await res.blob()
      const objectUrl = URL.createObjectURL(blob)
      cache.set(key, { objectUrl, expiresAt: Date.now() + CACHE_TTL_MS })
      return objectUrl
    } catch {
      return signedUrl // graceful degradation
    } finally {
      inflight.delete(key)
    }
  })()

  inflight.set(key, promise)
  return promise
}

/** Append ?download=true to a URL for Content-Disposition: attachment. */
export function toDownloadUrl(url: string): string {
  return url + (url.includes('?') ? '&' : '?') + 'download=true'
}

/** Revoke all cached ObjectURLs (call on disconnect). */
export function revokeAll() {
  for (const entry of cache.values()) {
    URL.revokeObjectURL(entry.objectUrl)
  }
  cache.clear()
  inflight.clear()
}
