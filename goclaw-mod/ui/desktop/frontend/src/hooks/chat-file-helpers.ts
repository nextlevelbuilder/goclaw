import { getApiClient, isApiClientReady } from '../lib/api'

/** Resolve relative path to absolute URL using API base */
function resolveBase(path: string): string {
  if (path.startsWith('http')) return path
  if (isApiClientReady()) return getApiClient().getBaseUrl() + path
  return path
}

/**
 * Convert any file path to /v1/files/ URL for serving.
 * Handles absolute Unix paths, Windows paths, and relative filenames.
 */
export function toFileUrl(path: string): string {
  if (!path) return ''
  if (path.includes('/v1/files/')) return resolveBase(path)
  // Normalize backslashes (Windows paths: C:\Users\... → C:/Users/...)
  const normalized = path.replace(/\\/g, '/')
  // Absolute: Unix /... or Windows C:/...
  if (normalized.startsWith('/')) return resolveBase(`/v1/files${normalized}`)
  if (/^[a-zA-Z]:\//.test(normalized)) return resolveBase(`/v1/files/${normalized}`)
  return resolveBase(`/v1/files/${normalized.split('/').pop() ?? normalized}`)
}
