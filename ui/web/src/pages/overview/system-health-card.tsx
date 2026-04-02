import {
  Timer,
  Monitor,
  Database,
  Wrench,
  Radio,
  Users,
  CheckCircle2,
  XCircle,
  Minus,
  Tag,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { HealthPayload, ChannelStatusEntry } from "./types";
import type { RuntimeInfo } from "@/pages/skills/hooks/use-runtimes";
import { formatUptime } from "./hooks/use-live-uptime";
import { cleanVersion } from "@/lib/clean-version";
import { formatRelativeTime, getChannelAttentionPriority, getChannelStatusMeta } from "@/pages/channels/channels-status-view";

function StatusDot({ ok }: { ok: boolean | undefined }) {
  if (ok === undefined)
    return <Minus className="h-3.5 w-3.5 text-muted-foreground/40" />;
  return ok ? (
    <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
  ) : (
    <XCircle className="h-3.5 w-3.5 text-red-500" />
  );
}

function HealthCell({
  label,
  icon: Icon,
  value,
  statusOk,
}: {
  label: string;
  icon: React.ElementType;
  value: string;
  statusOk?: boolean;
}) {
  return (
    <div className="flex items-center gap-3 rounded-lg border bg-muted/30 p-3">
      <div className="rounded-md bg-muted p-2">
        <Icon className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0">
        <p className="text-xs text-muted-foreground">{label}</p>
        <div className="flex items-center gap-1.5">
          {statusOk !== undefined && <StatusDot ok={statusOk} />}
          <p className="text-sm font-semibold tabular-nums truncate">{value}</p>
        </div>
      </div>
    </div>
  );
}

export function SystemHealthCard({
  health,
  liveUptime,
  enabledProviderCount,
  sessions,
  clientCount,
  channelEntries,
  runtimeEntries,
}: {
  health: HealthPayload | null;
  liveUptime: number | undefined;
  enabledProviderCount: number;
  sessions: number;
  clientCount: number;
  channelEntries: [string, ChannelStatusEntry][];
  runtimeEntries?: RuntimeInfo[];
}) {
  const { t } = useTranslation("overview");
  const degradedCount = channelEntries.filter(
    ([, ch]) => ch.state === "degraded",
  ).length;
  const failedCount = channelEntries.filter(
    ([, ch]) => ch.state === "failed",
  ).length;
  const attentionEntries = [...channelEntries]
    .filter(([, ch]) => getChannelAttentionPriority(ch, ch.enabled) > 0)
    .sort(
      (a, b) =>
        getChannelAttentionPriority(b[1], b[1].enabled) -
        getChannelAttentionPriority(a[1], a[1].enabled),
    );
  const attentionPreview = attentionEntries.slice(0, 2);
  const hiddenAttentionCount = Math.max(0, attentionEntries.length - attentionPreview.length);

  return (
    <Card className="gap-4">
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="text-base flex items-center gap-2">
            <Monitor className="h-4 w-4" /> {t("systemHealth.title")}
          </CardTitle>
          {health?.version && (
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Tag className="h-3 w-3" />
              <span className="font-medium">
                {cleanVersion(health.version)}
              </span>
              {health.updateAvailable === false && (
                <CheckCircle2 className="h-3 w-3 text-emerald-500" />
              )}
              {health.updateAvailable && health.latestVersion && (
                <span className="ml-1 rounded-full bg-amber-500/15 px-2 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
                  {health.latestVersion} available
                </span>
              )}
            </div>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <HealthCell
            label={t("systemHealth.uptime")}
            icon={Timer}
            value={formatUptime(liveUptime)}
          />
          {health?.database && (
            <HealthCell
              label={t("systemHealth.database")}
              icon={Database}
              value={
                health.database === "ok"
                  ? t("common:connected", "Connected")
                  : health.database
              }
              statusOk={health.database === "ok"}
            />
          )}
          <HealthCell
            label={t("systemHealth.providers")}
            icon={Radio}
            value={
              enabledProviderCount > 0
                ? t("systemHealth.active", { count: enabledProviderCount })
                : t("systemHealth.none")
            }
            statusOk={enabledProviderCount > 0}
          />
          <HealthCell
            label={t("systemHealth.tools")}
            icon={Wrench}
            value={String(health?.tools ?? 0)}
          />
          <HealthCell
            label={t("systemHealth.sessions")}
            icon={Monitor}
            value={String(sessions)}
          />
          <HealthCell
            label={t("systemHealth.clients")}
            icon={Users}
            value={String(clientCount)}
          />
        </div>

        {runtimeEntries && runtimeEntries.length > 0 && (
          <div className="border-t pt-4">
            <p className="mb-2 text-xs font-medium text-muted-foreground uppercase tracking-wider">
              {t("systemHealth.runtimes")}
            </p>
            <div className="flex flex-wrap gap-1.5">
              {runtimeEntries.map((rt) => (
                <span
                  key={rt.name}
                  className="inline-flex items-center gap-1.5 rounded-md bg-muted/50 px-2 py-1 text-xs"
                >
                  <span
                    className={`h-1.5 w-1.5 rounded-full ${rt.available ? "bg-emerald-500" : "bg-red-400"}`}
                  />
                  {rt.name}
                  {rt.version && (
                    <span className="text-muted-foreground">{rt.version}</span>
                  )}
                </span>
              ))}
            </div>
          </div>
        )}

        {channelEntries.length > 0 && (
          <div className="border-t pt-4">
            <div className="mb-2 flex items-center justify-between gap-2">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                {t("systemHealth.channels")}
              </p>
              {(degradedCount > 0 || failedCount > 0) && (
                <span className="text-xs text-muted-foreground">
                  {failedCount > 0
                    ? t("systemHealth.failedCount", {
                        defaultValue: "{{count}} failed",
                        count: failedCount,
                      })
                    : degradedCount === 1
                      ? t("systemHealth.warningCountOne", {
                          defaultValue: "{{count}} warning",
                          count: degradedCount,
                        })
                      : t("systemHealth.warningCountOther", {
                          defaultValue: "{{count}} warnings",
                          count: degradedCount,
                        })}
                </span>
              )}
            </div>
            <div className="flex flex-wrap gap-1.5">
              {channelEntries.map(([name, ch]) => {
                const meta = getChannelStatusMeta(ch, ch.enabled, t);
                return (
                  <span
                    key={name}
                    className="inline-flex items-center gap-1.5 rounded-md bg-muted/50 px-2 py-1 text-xs"
                  >
                    <span
                      className={`h-1.5 w-1.5 rounded-full ${meta.dotClass}`}
                    />
                    {name}
                  </span>
                );
              })}
            </div>
            {attentionPreview.length > 0 && (
              <div className="mt-3 rounded-lg border border-amber-200/70 bg-amber-500/[0.05] p-3 dark:border-amber-500/20 dark:bg-amber-500/10">
                <div className="mb-2 flex items-center justify-between gap-3">
                  <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    {t("systemHealth.needsAttention", {
                      defaultValue: "Needs attention",
                    })}
                  </p>
                  <span className="text-xs text-muted-foreground">
                    {t("systemHealth.channelsNeedingAttention", {
                      defaultValue: "{{count}} channels",
                      count: attentionEntries.length,
                    })}
                  </span>
                </div>
                <div className="space-y-2">
                  {attentionPreview.map(([name, ch]) => {
                    const meta = getChannelStatusMeta(ch, ch.enabled, t);
                    const checked = formatRelativeTime(ch.checked_at);
                    return (
                      <div
                        key={name}
                        className="flex items-start gap-2 text-sm"
                      >
                        <span
                          className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${meta.dotClass}`}
                        />
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="font-medium">{name}</span>
                            <span className="text-xs text-muted-foreground">
                              {meta.label}
                            </span>
                            {checked && (
                              <span className="text-xs text-muted-foreground">
                                {checked}
                              </span>
                            )}
                          </div>
                          <p className="truncate text-xs text-muted-foreground">
                            {ch.summary || meta.label}
                          </p>
                        </div>
                      </div>
                    );
                  })}
                  {hiddenAttentionCount > 0 && (
                    <p className="text-xs text-muted-foreground">
                      {t("systemHealth.moreAttention", {
                        defaultValue: "+{{count}} more",
                        count: hiddenAttentionCount,
                      })}
                    </p>
                  )}
                </div>
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
