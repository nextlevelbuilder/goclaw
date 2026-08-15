import { useState, useEffect, useRef } from "react";

/**
 * Returns a boolean that stays `true` for at least `minMs` after `loading` goes from true→false.
 * Useful for showing a brief spin animation on refresh buttons.
 */
export function useMinLoading(loading: boolean, minMs = 600): boolean {
  const [visible, setVisible] = useState(loading);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const visibleRef = useRef(visible);
  visibleRef.current = visible;

  useEffect(() => {
    if (loading) {
      if (timerRef.current) clearTimeout(timerRef.current);
      setVisible(true);
    } else if (visibleRef.current) {
      timerRef.current = setTimeout(() => setVisible(false), minMs);
    }
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [loading, minMs]);

  return visible;
}
