import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowUpRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatRelativeTime } from "@/lib/format";
import type { ChatGPTOAuthAvailability } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import type { ChatGPTOAuthProviderQuota } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import { ChatGPTOAuthQuotaStrip } from "./chatgpt-oauth-quota-strip";
import { getQuotaFailureKind, getQuotaSignals, getQuotaBadgeVariant, isQuotaUsable } from "./chatgpt-oauth-quota-utils";

export interface ChatGPTOAuthQuotaReadinessEntry {
  name: string;
  label: string;
  role: "preferred" | "extra";
  availability: ChatGPTOAuthAvailability;
  providerHref?: string;
  quota?: ChatGPTOAuthProviderQuota | null;
}

interface ChatGPTOAuthQuotaReadinessSectionProps {
  entries: ChatGPTOAuthQuotaReadinessEntry[];
  loading?: boolean;
  showProviderLinks?: boolean;
}

function quotaStateVariant(
  entry: ChatGPTOAuthQuotaReadinessEntry,
): "success" | "warning" | "destructive" | "outline" {
  if (entry.availability !== "ready") return "outline";
  if (!entry.quota) return "outline";
  if (isQuotaUsable(entry.quota)) return "success";

  const failureKind = getQuotaFailureKind(entry.quota);
  if (failureKind === "retry_later" || failureKind === "needs_setup" || failureKind === "reauth") {
    return "warning";
  }
  return "destructive";
}

function quotaStateLabel(
  entry: ChatGPTOAuthQuotaReadinessEntry,
  t: (key: string, options?: Record<string, unknown>) => string,
): string {
  if (entry.availability !== "ready") {
    return t(`chatgptOAuthRouting.status.${entry.availability}`);
  }
  if (!entry.quota) {
    return t("chatgptOAuthRouting.quota.checking");
  }
  if (isQuotaUsable(entry.quota)) {
    return t("chatgptOAuthRouting.quota.readyLabel");
  }

  const failureKind = getQuotaFailureKind(entry.quota);
  if (failureKind) {
    return t(`chatgptOAuthRouting.quota.failure.${failureKind}.label`);
  }
  return t("chatgptOAuthRouting.quota.failure.unavailable.label");
}

function quotaStateDescription(
  entry: ChatGPTOAuthQuotaReadinessEntry,
  t: (key: string, options?: Record<string, unknown>) => string,
): string {
  if (entry.availability !== "ready") {
    return t("chatgptOAuthRouting.quota.requiresReadyAlias");
  }
  if (!entry.quota) {
    return t("chatgptOAuthRouting.quota.checkingDescription");
  }
  if (isQuotaUsable(entry.quota)) {
    const signals = getQuotaSignals(entry.quota);
    if (signals.length === 0) {
      return t("chatgptOAuthRouting.quota.readyDescription");
    }
    return signals.map((signal) => `${signal.shortLabel} ${signal.remaining}%`).join(" · ");
  }

  const failureKind = getQuotaFailureKind(entry.quota);
  if (entry.quota.action_hint) {
    return entry.quota.action_hint;
  }
  if (failureKind) {
    return t(`chatgptOAuthRouting.quota.failure.${failureKind}.description`);
  }
  return t("chatgptOAuthRouting.quota.failure.unavailable.description");
}

export function ChatGPTOAuthQuotaReadinessSection({
  entries,
  loading = false,
  showProviderLinks = true,
}: ChatGPTOAuthQuotaReadinessSectionProps) {
  const { t } = useTranslation("agents");
  const readyEntries = entries.filter((entry) => entry.availability === "ready");
  const usableCount = readyEntries.filter((entry) => isQuotaUsable(entry.quota)).length;
  const blockerCount = readyEntries.filter((entry) => !isQuotaUsable(entry.quota)).length;
  const floorFiveHour = readyEntries.flatMap((entry) => getQuotaSignals(entry.quota))
    .filter((signal) => signal.shortLabel === "5h")
    .reduce<number | null>((min, signal) => (min == null ? signal.remaining : Math.min(min, signal.remaining)), null);

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {t("chatgptOAuthRouting.quota.readinessTitle")}
          </p>
          <p className="text-xs text-muted-foreground">
            {t("chatgptOAuthRouting.quota.readinessDescription")}
          </p>
        </div>

        <div className="flex flex-wrap gap-2">
          {loading ? (
            <Badge variant="outline">{t("chatgptOAuthRouting.quota.checking")}</Badge>
          ) : (
            <>
              <Badge variant={usableCount === readyEntries.length && readyEntries.length > 0 ? "success" : "warning"}>
                {t("chatgptOAuthRouting.quota.healthySummary", { usable: usableCount, total: readyEntries.length })}
              </Badge>
              {blockerCount > 0 && (
                <Badge variant="warning">
                  {t("chatgptOAuthRouting.quota.needsAttention", { count: blockerCount })}
                </Badge>
              )}
              {floorFiveHour != null && (
                <Badge variant={getQuotaBadgeVariant(floorFiveHour)}>
                  {t("chatgptOAuthRouting.quota.floorFiveHour", { value: floorFiveHour })}
                </Badge>
              )}
            </>
          )}
        </div>
      </div>

      <div className="space-y-2.5">
        {entries.map((entry) => (
          <div key={entry.name} className="rounded-lg border bg-muted/10 p-3">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium">{entry.label}</span>
                  <Badge variant={entry.role === "preferred" ? "secondary" : "outline"}>
                    {t(`chatgptOAuthRouting.role.${entry.role}`)}
                  </Badge>
                  <Badge variant={quotaStateVariant(entry)}>
                    {quotaStateLabel(entry, t)}
                  </Badge>
                </div>
                <p className="font-mono text-xs text-muted-foreground">{entry.name}</p>
              </div>

              {showProviderLinks && entry.providerHref && (
                <Button asChild variant="ghost" size="sm" className="h-7 px-0 text-xs shrink-0">
                  <Link to={entry.providerHref}>
                    {t("chatgptOAuthRouting.openProvider")}
                    <ArrowUpRight className="ml-1 h-3.5 w-3.5" />
                  </Link>
                </Button>
              )}
            </div>

            <div className="mt-3">
              <ChatGPTOAuthQuotaStrip quota={entry.quota} />
            </div>

            <div className="mt-3 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
              <span>{quotaStateDescription(entry, t)}</span>
              {entry.quota?.last_updated && (
                <span>{t("chatgptOAuthRouting.quota.lastChecked", { value: formatRelativeTime(entry.quota.last_updated) })}</span>
              )}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
