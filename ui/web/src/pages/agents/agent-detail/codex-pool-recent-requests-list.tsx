import { useTranslation } from "react-i18next";
import { Route } from "lucide-react";
import { EmptyState } from "@/components/shared/empty-state";
import { cn } from "@/lib/utils";
import type { CodexPoolRecentRequestsListProps } from "./codex-pool-entry-types";
import {
  CompactRequestCard,
  FullRequestRow,
} from "./codex-pool-request-row-cards";

export function CodexPoolRecentRequestsList({
  recentRequests,
  loading,
  compact = false,
  className,
}: CodexPoolRecentRequestsListProps) {
  const { t } = useTranslation("agents");

  if (loading) {
    return (
      <div
        className={cn(
          "rounded-lg border border-dashed text-muted-foreground",
          compact ? "px-3 py-3 text-xs" : "px-4 py-4 text-sm",
          className,
        )}
      >
        {t("chatgptOAuthRouting.loadingEvidence")}
      </div>
    );
  }

  if (recentRequests.length === 0) {
    return compact ? (
      <div
        className={cn(
          "rounded-lg border border-dashed bg-muted/5 px-3 py-3 text-xs text-muted-foreground",
          className,
        )}
      >
        {t("chatgptOAuthRouting.noEvidence")}
      </div>
    ) : (
      <div className={cn("rounded-lg border border-dashed bg-muted/5", className)}>
        <EmptyState
          icon={Route}
          title={t("chatgptOAuthRouting.sequenceEmptyTitle")}
          description={t("chatgptOAuthRouting.noEvidence")}
          className="py-6"
        />
      </div>
    );
  }

  if (compact) {
    return (
      <div
        className={cn(
          "overflow-x-auto overflow-y-hidden overscroll-contain pb-1",
          className,
        )}
      >
        <div className="flex min-w-max gap-2">
          {recentRequests.map((request, index) => (
            <CompactRequestCard key={request.span_id} request={request} index={index} />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div
      className={cn(
        "min-h-0 flex-1 overflow-y-auto overscroll-contain pr-1",
        className,
      )}
    >
      <div className="space-y-2">
        {recentRequests.map((request) => (
          <FullRequestRow key={request.span_id} request={request} />
        ))}
      </div>
    </div>
  );
}
