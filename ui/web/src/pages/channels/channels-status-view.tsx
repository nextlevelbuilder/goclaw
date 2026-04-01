import { Radio, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { CardSkeleton } from "@/components/shared/loading-skeleton";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import type { ChannelStatus } from "./hooks/use-channels";

const channelTypeLabels: Record<string, string> = {
  telegram: "Telegram",
  discord: "Discord",
  slack: "Slack",
  feishu: "Feishu / Lark",
  zalo_oa: "Zalo OA",
  zalo_personal: "Zalo Personal",
  whatsapp: "WhatsApp",
};

export { channelTypeLabels };

interface ChannelsStatusViewProps {
  channels: Record<string, ChannelStatus>;
  loading: boolean;
  spinning: boolean;
  refresh: () => void;
}

function getStatusMeta(
  status: ChannelStatus | null,
  enabled: boolean,
  t: ReturnType<typeof useTranslation>["t"],
) {
  if (!enabled) {
    return {
      dotClass: "bg-muted-foreground/40",
      badgeVariant: "secondary" as const,
      label: t("disabled"),
    };
  }

  switch (status?.state) {
    case "healthy":
      return {
        dotClass: "bg-emerald-500",
        badgeVariant: "success" as const,
        label: t("status.running"),
      };
    case "degraded":
      return {
        dotClass: "bg-amber-500",
        badgeVariant: "warning" as const,
        label: t("status.degraded", { defaultValue: "Degraded" }),
      };
    case "starting":
      return {
        dotClass: "bg-sky-500",
        badgeVariant: "info" as const,
        label: t("status.starting", { defaultValue: "Starting" }),
      };
    case "registered":
      return {
        dotClass: "bg-slate-400",
        badgeVariant: "secondary" as const,
        label: t("status.registered", { defaultValue: "Configured" }),
      };
    case "failed":
      return {
        dotClass: "bg-red-500",
        badgeVariant: "destructive" as const,
        label: t("status.failed", { defaultValue: "Failed" }),
      };
    case "stopped":
      return {
        dotClass: "bg-muted-foreground",
        badgeVariant: "secondary" as const,
        label: t("status.stopped"),
      };
    default:
      return status?.running
        ? {
            dotClass: "bg-emerald-500",
            badgeVariant: "success" as const,
            label: t("status.running"),
          }
        : {
            dotClass: "bg-muted-foreground",
            badgeVariant: "secondary" as const,
            label: t("status.stopped"),
          };
  }
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
            {entries.map(([name, status]: [string, ChannelStatus]) => {
              const meta = getStatusMeta(status, status.enabled, t);
              return (
                <div key={name} className="rounded-lg border p-4">
                  <div className="flex items-center justify-between">
                    <h4 className="text-sm font-medium">
                      {channelTypeLabels[name] || name}
                    </h4>
                    {status.enabled ? (
                      <Badge variant="success">{t("enabled")}</Badge>
                    ) : (
                      <Badge variant="secondary">{t("disabled")}</Badge>
                    )}
                  </div>
                  <div className="mt-3 flex items-center gap-2 text-sm">
                    <span className={`h-2 w-2 rounded-full ${meta.dotClass}`} />
                    <span className="text-muted-foreground">{meta.label}</span>
                  </div>
                  {status.summary && (
                    <div className="mt-2">
                      <Badge variant={meta.badgeVariant}>
                        {status.summary}
                      </Badge>
                    </div>
                  )}
                  {status.detail && (
                    <p className="mt-2 text-xs text-muted-foreground break-words">
                      {status.detail}
                    </p>
                  )}
                  {status.checked_at && (
                    <p className="mt-2 text-xs text-muted-foreground">
                      {t("detail.lastChecked", {
                        defaultValue: "Last checked: {{value}}",
                        value: new Date(status.checked_at).toLocaleString(),
                      })}
                    </p>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
