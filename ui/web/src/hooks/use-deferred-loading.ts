import { useState, useEffect, useRef } from "react";

/**
 * Anti-flicker loading state for skeleton displays.
 *
 * Phase 1 (delay): waits `delayMs` before returning true.
 * If loading finishes within this window, skeleton is never shown.
 *
 * Phase 2 (min display): once shown, stays visible for at least `minDisplayMs`.
 */
export function useDeferredLoading(
  loading: boolean,
  delayMs = 200,
  minDisplayMs = 400,
): boolean {
  const [show, setShow] = useState(false);
  const delayTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const minTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const shownAt = useRef<number | null>(null);

  useEffect(() => {
    if (loading) {
      if (!show && !delayTimer.current) {
        delayTimer.current = setTimeout(() => {
          delayTimer.current = null;
          shownAt.current = Date.now();
          setShow(true);
        }, delayMs);
      }
    } else {
      if (delayTimer.current) {
        clearTimeout(delayTimer.current);
        delayTimer.current = null;
      }

      if (show && shownAt.current !== null) {
        const remaining = minDisplayMs - (Date.now() - shownAt.current);
        if (remaining > 0) {
          minTimer.current = setTimeout(() => {
            minTimer.current = null;
            shownAt.current = null;
            setShow(false);
          }, remaining);
        } else {
          shownAt.current = null;
          setShow(false);
        }
      }
    }

    return () => {
      if (delayTimer.current) clearTimeout(delayTimer.current);
      if (minTimer.current) clearTimeout(minTimer.current);
    };
  }, [loading, delayMs, minDisplayMs, show]);

  return show;
}
