import { Radio, RefreshCw } from "lucide-react";
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

export function ChannelsStatusView({ channels, loading, spinning, refresh }: ChannelsStatusViewProps) {
  const entries = Object.entries(channels);
  const showSkeleton = useDeferredLoading(loading && entries.length === 0);

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Channels"
        description="Communication channel status"
        actions={
          <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> Refresh
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
            title="No channels"
            description="No communication channels are configured."
          />
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {entries.map(([name, status]: [string, ChannelStatus]) => (
              <div key={name} className="rounded-lg border p-4">
                <div className="flex items-center justify-between">
                  <h4 className="text-sm font-medium">
                    {channelTypeLabels[name] || name}
                  </h4>
                  {status.enabled ? (
                    <Badge variant="success">enabled</Badge>
                  ) : (
                    <Badge variant="secondary">disabled</Badge>
                  )}
                </div>
                <div className="mt-3 flex items-center gap-2 text-sm">
                  <span
                    className={`h-2 w-2 rounded-full ${status.running ? "bg-green-500" : "bg-muted-foreground"}`}
                  />
                  <span className="text-muted-foreground">
                    {status.running ? "Running" : "Stopped"}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
