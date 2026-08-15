import { useRef, useCallback, useEffect } from "react";

/**
 * Returns a debounced version of the given callback.
 * Multiple calls within `delayMs` are collapsed into one trailing call.
 * The timer is cleared on unmount.
 */
export function useDebouncedCallback(callback: () => void, delayMs = 2000): () => void {
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const callbackRef = useRef(callback);
  callbackRef.current = callback;

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  return useCallback(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      callbackRef.current();
    }, delayMs);
  }, [delayMs]);
}
