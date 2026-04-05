import { Radio, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { CardSkeleton } from "@/components/shared/loading-skeleton";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import type { ChannelRuntimeStatus } from "@/types/channel";
import {
  channelTypeLabels,
  getChannelStatusMeta,
  getChannelCheckedLabel,
  getChannelFailureKindLabel,
} from "./channels-status-utils";

export type { ChannelStatus } from "./channels-status-utils";
export type { ChannelStatusMeta, ChannelRemediationMeta } from "./channels-status-utils";
export {
  channelTypeLabels,
  formatRelativeTime,
  getChannelStatusFallback,
  getRenderableChannelStatus,
  getChannelCheckedLabel,
  getChannelFailureKindLabel,
  getChannelAttentionPriority,
  getChannelStatusMeta,
  getChannelRemediationMeta,
} from "./channels-status-utils";

interface ChannelsStatusViewProps {
  channels: Record<string, ChannelRuntimeStatus>;
  loading: boolean;
  spinning: boolean;
  refresh: () => void;
}

export function ChannelsStatusView({
  channels,
  loading,
  spinning,
  refresh,
}: ChannelsStatusViewProps) {
  const { t } = useTranslation("channels");
  const entries = Object.entries(channels);
  const showSkeleton = useDeferredLoading(loading && entries.length === 0);

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("statusDescription")}
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={refresh}
            disabled={spinning}
            className="gap-1"
          >
            <RefreshCw
              className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")}
            />{" "}
            {t("refresh")}
          </Button>
        }
      />

      <div className="mt-4">
        {showSkeleton ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {[1, 2, 3].map((i) => (
              <CardSkeleton key={i} />
            ))}
          </div>
        ) : entries.length === 0 ? (
          <EmptyState
            icon={Radio}
            title={t("emptyTitle")}
            description={t("emptyStatusDescription")}
          />
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {entries.map(([name, status]) => {
              const meta = getChannelStatusMeta(status, status.enabled, t);
              const checked = getChannelCheckedLabel(status, t);
              const failureKind = getChannelFailureKindLabel(
                status.failure_kind,
                t,
              );

              return (
                <div
                  key={name}
                  className={`rounded-lg border p-4 ${meta.surfaceClass}`}
                >
                  <div className="flex items-center justify-between gap-3">
                    <h4 className="text-sm font-medium">
                      {channelTypeLabels[name] || name}
                    </h4>
                    <Badge variant={meta.badgeVariant}>{meta.label}</Badge>
                  </div>
                  {status.summary && (
                    <p className="mt-3 text-sm font-medium">{status.summary}</p>
                  )}
                  <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    {failureKind && <Badge variant="outline">{failureKind}</Badge>}
                    {checked && <span>{checked}</span>}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
