import { useState, useEffect } from "react";
import { getOrFetchUrl } from "@/lib/media-cache";

/** Returns a cached blob ObjectURL for the given signed media URL.
 *  On first render returns the signed URL as-is (no blank frame),
 *  then swaps to blob URL once fetched. On subsequent renders with
 *  the same clean path, returns the cached blob instantly. */
export function useMediaUrl(signedUrl: string | undefined): string | undefined {
  const [url, setUrl] = useState(signedUrl);

  useEffect(() => {
    if (!signedUrl) {
      setUrl(undefined);
      return;
    }
    // Show signed URL immediately (browser may already have it cached)
    setUrl(signedUrl);

    let cancelled = false;
    getOrFetchUrl(signedUrl).then((resolved) => {
      if (!cancelled) setUrl(resolved);
    });
    return () => { cancelled = true; };
  }, [signedUrl]);

  return url;
}
