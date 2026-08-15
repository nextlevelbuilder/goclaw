import type { CodexPoolEntry } from "@/pages/agents/agent-detail/codex-pool-entry-types";
import type { CodexPoolProviderCount } from "@/pages/agents/agent-detail/hooks/use-codex-pool-activity";
import type { ChatGPTOAuthAvailability } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import type { ChatGPTOAuthProviderQuota } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";

/** Minimal provider shape needed for pool entry construction. */
export interface PoolProviderInfo {
  id?: string;
  display_name?: string;
  enabled?: boolean;
}

/** Resolve OAuth availability from status map with enabled fallback. */
export function resolveProviderAvailability(
  providerName: string,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
  enabled?: boolean,
): ChatGPTOAuthAvailability {
  return (
    statusByName.get(providerName)?.availability ??
    (enabled === false ? "disabled" : "needs_sign_in")
  );
}

/**
 * Build a CodexPoolEntry for a provider name with live activity counts.
 * Used when CodexPoolProviderCount data is available (activity views).
 */
export function toPoolEntryWithCounts(
  providerName: string,
  count: CodexPoolProviderCount,
  preferredProviderName: string,
  providerByName: Map<string, PoolProviderInfo>,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
  quotaByName: Map<string, ChatGPTOAuthProviderQuota | null>,
): CodexPoolEntry {
  const provider = providerByName.get(providerName);
  return {
    name: providerName,
    label: provider?.display_name || providerName,
    availability: resolveProviderAvailability(providerName, statusByName, provider?.enabled),
    role: providerName === preferredProviderName ? "preferred" : "extra",
    requestCount: count.request_count,
    directSelectionCount: count.direct_selection_count,
    failoverServeCount: count.failover_serve_count,
    successCount: count.success_count,
    failureCount: count.failure_count,
    consecutiveFailures: count.consecutive_failures,
    successRate: count.success_rate,
    healthScore: count.health_score,
    healthState: count.health_state ?? "idle",
    lastSelectedAt: count.last_selected_at,
    lastFailoverAt: count.last_failover_at,
    lastUsedAt: count.last_used_at,
    lastSuccessAt: count.last_success_at,
    lastFailureAt: count.last_failure_at,
    providerHref: provider?.id ? `/providers/${provider.id}` : undefined,
    quota: quotaByName.get(providerName),
  };
}

/**
 * Build a CodexPoolEntry for a provider name with zeroed counts.
 * Used when no activity data is available (pool configuration views).
 */
export function toPoolEntry(
  providerName: string,
  preferredProviderName: string,
  providerByName: Map<string, PoolProviderInfo>,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
  quotaByName: Map<string, ChatGPTOAuthProviderQuota | null>,
): CodexPoolEntry {
  const provider = providerByName.get(providerName);
  return {
    name: providerName,
    label: provider?.display_name || providerName,
    availability: resolveProviderAvailability(providerName, statusByName, provider?.enabled),
    role: providerName === preferredProviderName ? "preferred" : "extra",
    requestCount: 0,
    directSelectionCount: 0,
    failoverServeCount: 0,
    successCount: 0,
    failureCount: 0,
    consecutiveFailures: 0,
    successRate: 0,
    healthScore: 0,
    healthState: "idle",
    providerHref: provider?.id ? `/providers/${provider.id}` : undefined,
    quota: quotaByName.get(providerName),
  };
}

/**
 * Batch-transform a list of provider names into CodexPoolEntry[] with zeroed counts.
 * Used in pool configuration views (no activity data).
 */
export function toPoolEntries(
  poolNames: string[],
  preferredProviderName: string,
  providerByName: Map<string, PoolProviderInfo>,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
  quotaByName: Map<string, ChatGPTOAuthProviderQuota | null>,
): CodexPoolEntry[] {
  return poolNames.map((name) =>
    toPoolEntry(name, preferredProviderName, providerByName, statusByName, quotaByName),
  );
}

/**
 * Batch-transform CodexPoolProviderCount[] into CodexPoolEntry[] with live counts.
 * Used in pool activity views where counts drive the entry list (provider-pool-activity-section).
 */
export function toPoolEntriesWithCounts(
  counts: CodexPoolProviderCount[],
  preferredProviderName: string,
  providerByName: Map<string, PoolProviderInfo>,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
  quotaByName: Map<string, ChatGPTOAuthProviderQuota | null>,
): CodexPoolEntry[] {
  return counts.map((c) =>
    toPoolEntryWithCounts(
      c.provider_name,
      c,
      preferredProviderName,
      providerByName,
      statusByName,
      quotaByName,
    ),
  );
}

/**
 * Build CodexPoolEntry[] for a list of pool provider names, merging in any available
 * activity counts by name. Names with no count data get zeroed fields.
 * directSelectionCount falls back to request_count when direct_selection_count is absent.
 * Used in agent codex pool page where pool names drive the list, not counts.
 */
export function toPoolEntriesMerged(
  poolNames: string[],
  counts: CodexPoolProviderCount[],
  preferredProviderName: string,
  providerByName: Map<string, PoolProviderInfo>,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
  quotaByName: Map<string, ChatGPTOAuthProviderQuota | null>,
): CodexPoolEntry[] {
  const countsByName = new Map(counts.map((c) => [c.provider_name, c]));
  return poolNames.map((providerName) => {
    const count = countsByName.get(providerName);
    if (!count) return toPoolEntry(providerName, preferredProviderName, providerByName, statusByName, quotaByName);
    // directSelectionCount falls back to request_count when field is absent (older API responses)
    return {
      ...toPoolEntryWithCounts(providerName, count, preferredProviderName, providerByName, statusByName, quotaByName),
      directSelectionCount: count.direct_selection_count ?? count.request_count ?? 0,
    };
  });
}
