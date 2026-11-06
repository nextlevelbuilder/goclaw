import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";

/** Shape of GET /v1/providers/{id}/health (see __fixtures__/provider-health.json). */
export interface ProviderHealth {
  provider_id: string;
  provider: string;
  enabled: boolean;
  consecutive_failures: number;
  cooling_down: boolean;
  cooldown_until: string | null;
  last_error_class: string;
  error_counts: Record<string, number>;
  last_probe_at: string | null;
  updated_at: string | null;
  max_cooldown_seconds: number;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function asString(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function asNumber(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

/** Valid RFC3339 timestamp or null; anything unparsable is treated as absent. */
function asTimestamp(v: unknown): string | null {
  if (typeof v !== "string" || v === "") return null;
  return Number.isNaN(Date.parse(v)) ? null : v;
}

/**
 * Validates the health payload at the network boundary. Missing or malformed
 * fields degrade to safe defaults so the UI never renders `undefined`.
 */
export function parseProviderHealth(raw: unknown): ProviderHealth {
  if (!isRecord(raw)) throw new Error("invalid provider health response");
  const counts: Record<string, number> = {};
  if (isRecord(raw.error_counts)) {
    for (const [cls, n] of Object.entries(raw.error_counts)) {
      if (typeof n === "number" && Number.isFinite(n)) counts[cls] = n;
    }
  }
  return {
    provider_id: asString(raw.provider_id),
    provider: asString(raw.provider),
    enabled: raw.enabled === true,
    consecutive_failures: asNumber(raw.consecutive_failures),
    cooling_down: raw.cooling_down === true,
    cooldown_until: asTimestamp(raw.cooldown_until),
    last_error_class: asString(raw.last_error_class),
    error_counts: counts,
    last_probe_at: asTimestamp(raw.last_probe_at),
    updated_at: asTimestamp(raw.updated_at),
    max_cooldown_seconds: asNumber(raw.max_cooldown_seconds),
  };
}

/** Milliseconds until the cooldown ends, or null when not cooling down / unknown. */
export function cooldownRemainingMs(health: ProviderHealth | undefined, now: number): number | null {
  if (!health?.cooling_down || !health.cooldown_until) return null;
  return Math.max(0, Date.parse(health.cooldown_until) - now);
}

/** Whole minutes left (rounded up) for the banner copy; 0 means "any moment now". */
export function cooldownRemainingMinutes(health: ProviderHealth | undefined, now: number): number | null {
  const ms = cooldownRemainingMs(health, now);
  return ms == null ? null : Math.ceil(ms / 60_000);
}

const HEALTH_POLL_MS = 30_000;
// Small grace after cooldown_until so the gateway has cleared the state when we re-read it.
const COOLDOWN_GRACE_MS = 1_500;

/**
 * Next refetch delay: when cooling down, re-read shortly after cooldown_until
 * passes (capped by the normal poll interval), otherwise poll at the normal rate.
 */
export function healthRefetchDelay(health: ProviderHealth | undefined, now: number): number {
  const remaining = cooldownRemainingMs(health, now);
  if (remaining == null) return HEALTH_POLL_MS;
  return Math.min(HEALTH_POLL_MS, remaining + COOLDOWN_GRACE_MS);
}

export const providerHealthKey = (providerId: string) => ["providers", providerId, "health"] as const;

export function useProviderHealth(providerId: string, enabled = true) {
  const http = useHttp();
  return useQuery({
    queryKey: providerHealthKey(providerId),
    enabled: enabled && Boolean(providerId),
    staleTime: 10_000,
    queryFn: async () => parseProviderHealth(await http.get<unknown>(`/v1/providers/${providerId}/health`)),
    refetchInterval: (query) => healthRefetchDelay(query.state.data, Date.now()),
    refetchIntervalInBackground: false,
  });
}
