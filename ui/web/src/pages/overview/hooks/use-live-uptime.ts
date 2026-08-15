import { useState, useEffect, useRef } from "react";

export function useLiveUptime(serverUptimeMs: number | undefined) {
  const [tick, setTick] = useState(0);
  const baseRef = useRef<{ serverMs: number; localTs: number } | null>(null);

  useEffect(() => {
    if (serverUptimeMs != null) {
      baseRef.current = { serverMs: serverUptimeMs, localTs: Date.now() };
    }
  }, [serverUptimeMs]);

  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, []);

  if (!baseRef.current) return undefined;
  void tick; // used for re-render
  return baseRef.current.serverMs + (Date.now() - baseRef.current.localTs);
}

export function formatUptime(ms: number | undefined): string {
  if (!ms) return "--";
  const sec = Math.floor(ms / 1000);
  const s = sec % 60;
  const min = Math.floor(sec / 60) % 60;
  const hr = Math.floor(sec / 3600) % 24;
  const day = Math.floor(sec / 86400);
  if (day > 0) return `${day}d ${hr}h ${min}m ${s}s`;
  if (hr > 0) return `${hr}h ${min}m ${s}s`;
  if (min > 0) return `${min}m ${s}s`;
  return `${s}s`;
}

export function formatClientTime(iso: string): string {
  try {
    const d = new Date(iso);
    const now = Date.now();
    const diffMs = now - d.getTime();
    if (diffMs < 0) return "just now";
    const sec = Math.floor(diffMs / 1000);
    if (sec < 60) return `${sec}s ago`;
    const min = Math.floor(sec / 60);
    if (min < 60) return `${min}m ago`;
    const hr = Math.floor(min / 60);
    return `${hr}h ${min % 60}m ago`;
  } catch {
    return "--";
  }
}
