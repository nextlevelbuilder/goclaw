import { useEffect, useRef, useCallback } from "react";

/**
 * Auto-scroll to bottom of a container when content changes.
 * Only auto-scrolls if user is near the bottom (within threshold).
 * Pass a `forceTrigger` counter to force scroll regardless of position (e.g. on send).
 *
 * Uses instant scroll (not smooth) to avoid jitter during rapid streaming updates.
 * Ignores scroll events fired by programmatic scrolls so `isNearBottom` isn't
 * incorrectly flipped to false mid-animation.
 */
export function useAutoScroll<T extends HTMLElement>(
  deps: unknown[],
  threshold = 100,
  forceTrigger = 0,
) {
  const ref = useRef<T>(null);
  const isNearBottom = useRef(true);
  // Guard: ignore scroll events triggered by our own programmatic scrolls.
  const programmaticScroll = useRef(false);

  const checkScroll = useCallback(() => {
    // Skip scroll events fired by our own scrollToBottom calls.
    if (programmaticScroll.current) return;
    const el = ref.current;
    if (!el) return;
    const { scrollTop, scrollHeight, clientHeight } = el;
    isNearBottom.current = scrollHeight - scrollTop - clientHeight < threshold;
  }, [threshold]);

  const scrollToBottom = useCallback((instant = false) => {
    const el = ref.current;
    if (!el) return;
    programmaticScroll.current = true;
    if (instant) {
      el.scrollTop = el.scrollHeight;
    } else {
      el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    }
    // Re-enable user scroll detection after the browser paints.
    requestAnimationFrame(() => {
      programmaticScroll.current = false;
    });
  }, []);

  // Auto-scroll when content changes (only if near bottom).
  // Uses instant scroll to prevent jitter from rapid streaming chunk updates.
  useEffect(() => {
    if (isNearBottom.current) {
      scrollToBottom(true);
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
