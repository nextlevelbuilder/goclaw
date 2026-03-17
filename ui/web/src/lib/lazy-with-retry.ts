import { lazy, type ComponentType } from "react";

/**
 * Wraps React.lazy() with retry logic for failed chunk loads.
 * - Retries the import once on failure (handles transient network errors)
 * - If retry fails, forces a page reload to clear stale chunk URLs (e.g. after a deploy)
 * - sessionStorage guard prevents infinite reload loops
 */
export function lazyWithRetry<T extends ComponentType<unknown>>(
  importFn: () => Promise<{ default: T }>,
) {
  return lazy(async () => {
    try {
      return await importFn();
    } catch {
      // Retry once after transient failure
      try {
        return await importFn();
      } catch (retryError) {
        // Force reload to clear stale chunks — but only once per session
        const alreadyReloaded = sessionStorage.getItem("chunk-failed-reload");
        if (!alreadyReloaded) {
          sessionStorage.setItem("chunk-failed-reload", "1");
          window.location.reload();
        }
        // Already reloaded and still failing — let ErrorBoundary handle it
        throw retryError;
      }
    }
  });
}
