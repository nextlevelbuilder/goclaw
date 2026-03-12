import { useEffect, useRef, useCallback } from "react";

/**
 * Auto-scroll to bottom of a container when content changes.
 * Only auto-scrolls if user is near the bottom (within threshold).
 * Pass a `forceTrigger` counter to force scroll regardless of position (e.g. on send).
 */
export function useAutoScroll<T extends HTMLElement>(
  deps: unknown[],
  threshold = 100,
  forceTrigger = 0,
) {
  const ref = useRef<T>(null);
  const isNearBottom = useRef(true);

  const checkScroll = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    const { scrollTop, scrollHeight, clientHeight } = el;
    isNearBottom.current = scrollHeight - scrollTop - clientHeight < threshold;
  }, [threshold]);

  const scrollToBottom = useCallback((instant = false) => {
    const el = ref.current;
    if (!el) return;
    if (instant) {
      el.scrollTop = el.scrollHeight;
    } else {
      el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    }
  }, []);

  // Auto-scroll when content changes (only if near bottom) — smooth
  useEffect(() => {
    if (isNearBottom.current) {
      scrollToBottom(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  // Force scroll when trigger increments (e.g. user sends a message) — instant
  useEffect(() => {
    if (forceTrigger > 0) {
      isNearBottom.current = true;
      scrollToBottom(true);
    }
  }, [forceTrigger, scrollToBottom]);

  return { ref, onScroll: checkScroll, scrollToBottom };
}
