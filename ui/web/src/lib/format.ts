export function formatDate(date: string | Date, tz?: string): string {
  const d = typeof date === "string" ? new Date(date) : date;
  const opts: Intl.DateTimeFormatOptions = {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  };
  if (tz) opts.timeZone = resolveTimezone(tz);
  return d.toLocaleDateString("en-US", opts);
}

export function formatRelativeTime(date: string | Date): string {
  const d = typeof date === "string" ? new Date(date) : date;
  const now = Date.now();
  const diffMs = now - d.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHr = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHr / 24);

  if (diffSec < 60) return "just now";
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHr < 24) return `${diffHr}h ago`;
  if (diffDay < 30) return `${diffDay}d ago`;
  return formatDate(d);
}

export function formatTokens(count: number | null | undefined): string {
  if (count == null) return "0";
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`;
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`;
  return count.toString();
}

export function formatCost(cost: number | null | undefined): string {
  if (cost == null || cost === 0) return "$0.00";
  if (cost < 0.01) return `$${cost.toFixed(4)}`;
  return `$${cost.toFixed(2)}`;
}

export function formatDuration(ms: number | undefined | null): string {
  if (ms == null || isNaN(ms)) return "—";
  if (ms < 1000) return `${ms}ms`;
  const sec = ms / 1000;
  if (sec < 60) return `${sec.toFixed(1)}s`;
  const min = Math.floor(sec / 60);
  const remainSec = Math.floor(sec % 60);
  return `${min}m ${remainSec}s`;
}

/**
 * Resolve the effective IANA timezone string.
 * "auto" → browser's local timezone.
 */
export function resolveTimezone(tz: string): string {
  if (tz === "auto") return Intl.DateTimeFormat().resolvedOptions().timeZone;
  return tz;
}

/**
 * Format a UTC timestamp for chart labels, respecting the user's chosen timezone.
 * Uses Intl.DateTimeFormat for native timezone support (no extra deps).
 */
export function formatBucketTz(
  bucket: string,
  tz: string,
  granularity: "hour" | "day",
): string {
  try {
    const d = new Date(bucket);
    const resolved = resolveTimezone(tz);
    const opts: Intl.DateTimeFormatOptions = {
      timeZone: resolved,
      month: "short",
      day: "numeric",
      ...(granularity === "hour" ? { hour: "2-digit", minute: "2-digit", hour12: false } : {}),
    };
    return new Intl.DateTimeFormat("en-US", opts).format(d);
  } catch {
    return bucket;
  }
}

/**
 * Format a file size in bytes to a human-readable string.
 */
export function formatFileSize(bytes: number): string {
  if (bytes <= 0) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

/**
 * Compute duration in ms from start/end time strings.
 * Falls back to 0 if either is missing.
 */
export function computeDurationMs(startTime?: string, endTime?: string): number | null {
  if (!startTime || !endTime) return null;
  const start = new Date(startTime).getTime();
  const end = new Date(endTime).getTime();
  if (isNaN(start) || isNaN(end)) return null;
  return end - start;
}