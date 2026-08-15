import type { ChatGPTOAuthProviderQuota } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import type { ChatGPTOAuthAvailability } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";

export type ChatGPTOAuthQuotaFailureKind =
  | "billing"
  | "exhausted"
  | "reauth"
  | "forbidden"
  | "needs_setup"
  | "retry_later"
  | "unavailable";

export type ChatGPTOAuthRouteReadiness =
  | "healthy"
  | "fallback"
  | "checking"
  | "blocked";

export interface ChatGPTOAuthQuotaSignal {
  shortLabel: "5h" | "Wk";
  remaining: number;
  resetAt?: string | null;
}

export function getQuotaSignals(
  quota?: ChatGPTOAuthProviderQuota | null,
): ChatGPTOAuthQuotaSignal[] {
  if (!quota?.success) return [];

  const explicit: ChatGPTOAuthQuotaSignal[] = [];
  if (quota.core_usage?.five_hour) {
    explicit.push({
      shortLabel: "5h",
      remaining: quota.core_usage.five_hour.remaining_percent,
      resetAt: quota.core_usage.five_hour.reset_at,
    });
  }
  if (quota.core_usage?.weekly) {
    explicit.push({
      shortLabel: "Wk",
      remaining: quota.core_usage.weekly.remaining_percent,
      resetAt: quota.core_usage.weekly.reset_at,
    });
  }

  if (explicit.length > 0) return explicit;

  const usageWindows = quota.windows
    .filter((window) => !window.label.toLowerCase().includes("code review"))
    .sort(
      (a, b) =>
        (a.reset_after_seconds ?? Number.MAX_SAFE_INTEGER) -
        (b.reset_after_seconds ?? Number.MAX_SAFE_INTEGER),
    );

  if (usageWindows.length === 0) return [];

  const first = usageWindows[0];
  const last = usageWindows[usageWindows.length - 1];
  if (!first || !last) return [];
  const signals: ChatGPTOAuthQuotaSignal[] = [
    {
      shortLabel: "5h",
      remaining: first.remaining_percent,
      resetAt: first.reset_at,
    },
  ];

  if (last !== first) {
    signals.push({
      shortLabel: "Wk",
      remaining: last.remaining_percent,
      resetAt: last.reset_at,
    });
  }

  return signals;
}

export function getQuotaFailureKind(
  quota?: ChatGPTOAuthProviderQuota | null,
): ChatGPTOAuthQuotaFailureKind | null {
  if (!quota) return null;
  if (quota.success) {
    const signals = getQuotaSignals(quota);
    if (signals.length > 0 && signals.some((signal) => signal.remaining <= 0)) {
      return "exhausted";
    }
    return null;
  }
  if (quota.error_code === "payment_required") return "billing";
  if (quota.needs_reauth) return "reauth";
  if (quota.is_forbidden) return "forbidden";
  if (quota.error_code === "missing_account_id") return "needs_setup";
  if (quota.retryable || quota.error_code === "rate_limited")
    return "retry_later";
  return "unavailable";
}

export function getQuotaPlanLabel(planType?: string | null): string | null {
  if (!planType) return null;
  return planType
    .split(/[\s_-]+/g)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export function getQuotaBadgeVariant(
  remaining: number,
): "success" | "warning" | "destructive" {
  if (remaining <= 20) return "destructive";
  if (remaining <= 50) return "warning";
  return "success";
}

export function isQuotaUsable(
  quota?: ChatGPTOAuthProviderQuota | null,
): boolean {
  const signals = getQuotaSignals(quota);
  return (
    Boolean(quota?.success) &&
    signals.length > 0 &&
    signals.every((signal) => signal.remaining > 0)
  );
}

export function getRouteReadiness(
  availability: ChatGPTOAuthAvailability,
  quota?: ChatGPTOAuthProviderQuota | null,
): ChatGPTOAuthRouteReadiness {
  if (availability !== "ready") return "blocked";
  if (!quota) return "checking";
  if (isQuotaUsable(quota)) return "healthy";

  const failureKind = getQuotaFailureKind(quota);
  if (
    failureKind === "billing" ||
    failureKind === "exhausted" ||
    failureKind === "reauth" ||
    failureKind === "forbidden" ||
    failureKind === "needs_setup"
  ) {
    return "blocked";
  }
  if (failureKind === "retry_later" || failureKind === "unavailable") {
    return "fallback";
  }
  return "fallback";
}

export function summarizeQuotaHealth<
  T extends { quota?: ChatGPTOAuthProviderQuota | null },
>(entries: T[]) {
  let usable = 0;
  let attention = 0;
  let floorFiveHour: number | null = null;
  let floorWeekly: number | null = null;

  for (const entry of entries) {
    const quota = entry.quota;
    if (!quota) continue;

    if (isQuotaUsable(quota)) {
      usable += 1;
    } else {
      attention += 1;
    }

    for (const signal of getQuotaSignals(quota)) {
      if (signal.shortLabel === "5h") {
        floorFiveHour =
          floorFiveHour == null
            ? signal.remaining
            : Math.min(floorFiveHour, signal.remaining);
      }
      if (signal.shortLabel === "Wk") {
        floorWeekly =
          floorWeekly == null
            ? signal.remaining
            : Math.min(floorWeekly, signal.remaining);
      }
    }
  }

  return {
    usable,
    attention,
    floorFiveHour,
    floorWeekly,
  };
}
