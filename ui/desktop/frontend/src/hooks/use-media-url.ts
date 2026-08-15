import { useState, useEffect } from 'react'
import { getOrFetchUrl } from '../lib/media-cache'

/** Returns a cached blob ObjectURL for the given signed media URL.
 *  On first render returns the signed URL as-is (no blank frame),
 *  then swaps to blob URL once fetched. Prevents flicker on session switch. */
export function useMediaUrl(signedUrl: string | undefined): string | undefined {
  // Only show signed (?ft=) URLs immediately — others need Bearer auth fetch first
  const [url, setUrl] = useState(() =>
    signedUrl?.includes('ft=') ? signedUrl : undefined,
  )

  useEffect(() => {
    if (!signedUrl) {
      setUrl(undefined)
      return
    }
    // Show signed URL immediately (browser can load it); others wait for blob
    if (signedUrl.includes('ft=')) setUrl(signedUrl)
    else setUrl(undefined)

    let cancelled = false
    getOrFetchUrl(signedUrl).then((resolved) => {
      if (!cancelled) setUrl(resolved)
    })
    return () => { cancelled = true }
  }, [signedUrl])

  return url
}
