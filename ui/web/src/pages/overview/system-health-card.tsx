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
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { HealthPayload, ChannelStatusEntry } from "./types";
import type { RuntimeInfo } from "@/pages/skills/hooks/use-runtimes";
import { formatUptime } from "./hooks/use-live-uptime";

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
  return (
    <Card className="gap-4">
      <CardHeader>
        <CardTitle className="text-base flex items-center gap-2">
          <Monitor className="h-4 w-4" /> {t("systemHealth.title")}
        </CardTitle>
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
            <p className="mb-2 text-xs font-medium text-muted-foreground uppercase tracking-wider">
              {t("systemHealth.channels")}
            </p>
            <div className="flex flex-wrap gap-1.5">
              {channelEntries.map(([name, ch]) => (
                <span
                  key={name}
                  className="inline-flex items-center gap-1.5 rounded-md bg-muted/50 px-2 py-1 text-xs"
                >
                  <span
                    className={`h-1.5 w-1.5 rounded-full ${ch.running ? "bg-emerald-500" : "bg-red-400"}`}
                  />
                  {name}
                </span>
              ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
