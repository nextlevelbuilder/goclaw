import { useState, useEffect } from "react";

/** Format seconds into mm:ss or hh:mm:ss countdown. */
export function formatCountdown(seconds: number): string {
  if (seconds <= 0) return "00:00";
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  const mm = String(m).padStart(2, "0");
  const ss = String(s).padStart(2, "0");
  if (h > 0) return `${String(h).padStart(2, "0")}:${mm}:${ss}`;
  return `${mm}:${ss}`;
}

/**
 * Hook that ticks a countdown from a target ISO date string.
 * Returns formatted string like "12m", "1h 5m", "now", or null if no target.
 * Ticks every `tickMs` (default 1s for mm:ss display).
 */
export function useCountdown(targetISO: string | undefined | null, tickMs = 1_000): string | null {
  const [now, setNow] = useState(Date.now());

  useEffect(() => {
    if (!targetISO) return;
    setNow(Date.now()); // immediate sync on target change
    const id = setInterval(() => setNow(Date.now()), tickMs);
    return () => clearInterval(id);
  }, [targetISO, tickMs]);

  if (!targetISO) return null;
  const diff = Math.max(0, Math.floor((new Date(targetISO).getTime() - now) / 1000));
  return formatCountdown(diff);
}
