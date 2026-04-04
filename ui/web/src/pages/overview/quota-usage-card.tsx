import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { StatusBadge } from "@/components/shared/status-badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { QuotaUsage, QuotaUsageResult } from "./types";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { formatUserLabel } from "@/lib/format-user-label";

function QuotaBar({ used, limit }: { used: number; limit: number }) {
  const { t } = useTranslation("overview");
  if (limit === 0) {
    return <span className="text-xs text-muted-foreground">{t("quotaUsage.noLimit")}</span>;
  }
  const pct = Math.min((used / limit) * 100, 100);
  const color =
    pct > 85
      ? "bg-red-500"
      : pct > 60
        ? "bg-amber-500"
        : "bg-emerald-500";
  return (
    <div className="h-1.5 w-full rounded-full bg-muted">
      <div
        className={`h-full rounded-full transition-all ${color}`}
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}

function QuotaCell({ usage }: { usage: QuotaUsage }) {
  const label =
    usage.limit === 0
      ? String(usage.used)
      : `${usage.used}/${usage.limit}`;
  return (
    <div className="space-y-1">
      <span className="text-sm tabular-nums">{label}</span>
      <QuotaBar used={usage.used} limit={usage.limit} />
    </div>
  );
}

export function QuotaUsageCard({ quota }: { quota: QuotaUsageResult }) {
  const { t } = useTranslation("overview");
  const userIds = useMemo(() => quota.entries.map((e) => e.userId).filter(Boolean) as string[], [quota.entries]);
  const { resolve } = useContactResolver(userIds);
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-3">
        <CardTitle className="text-base">{t("quotaUsage.title")}</CardTitle>
        <StatusBadge status="success" label={t("common:enabled", "Enabled")} />
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b text-left text-muted-foreground">
                <th className="pb-2 pr-4 font-medium">{t("quotaUsage.columns.userGroup")}</th>
                <th className="pb-2 px-4 font-medium w-36">{t("quotaUsage.columns.hour")}</th>
                <th className="pb-2 px-4 font-medium w-36">{t("quotaUsage.columns.day")}</th>
                <th className="pb-2 pl-4 font-medium w-36">{t("quotaUsage.columns.week")}</th>
              </tr>
            </thead>
            <tbody>
              {quota.entries.map((entry) => (
                <tr
                  key={entry.userId}
                  className="border-b last:border-0"
                >
                  <td className="py-3 pr-4">
                    <span className="font-mono text-xs">{formatUserLabel(entry.userId, resolve)}</span>
                  </td>
                  <td className="py-3 px-4">
                    <QuotaCell usage={entry.hour} />
                  </td>
                  <td className="py-3 px-4">
                    <QuotaCell usage={entry.day} />
                  </td>
                  <td className="py-3 pl-4">
                    <QuotaCell usage={entry.week} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
}
