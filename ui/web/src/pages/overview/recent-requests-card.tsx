import { useMemo } from "react";
import { ArrowRight } from "lucide-react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { StatusBadge } from "@/components/shared/status-badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ROUTES } from "@/lib/constants";
import {
  formatRelativeTime,
  formatTokens,
  formatDuration,
} from "@/lib/format";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { formatUserLabel } from "@/lib/format-user-label";

interface Trace {
  id: string;
  name: string;
  user_id: string;
  channel: string;
  total_input_tokens: number;
  total_output_tokens: number;
  duration_ms: number;
  status: string;
  created_at: string;
}

export function RecentRequestsCard({ traces }: { traces: Trace[] }) {
  const { t } = useTranslation("overview");
  const userIds = useMemo(() => traces.map((tr) => tr.user_id).filter(Boolean) as string[], [traces]);
  const { resolve } = useContactResolver(userIds);
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-3">
        <CardTitle className="text-base">{t("recentRequests.title")}</CardTitle>
        {traces.length > 0 && (
          <Link
            to={ROUTES.TRACES}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            {t("recentRequests.viewAll")} <ArrowRight className="h-3 w-3" />
          </Link>
        )}
      </CardHeader>
      <CardContent>
        {traces.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {t("recentRequests.noRequests")}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-muted-foreground">
                  <th className="pb-2 pr-4 font-medium">{t("recentRequests.columns.time")}</th>
                  <th className="pb-2 px-4 font-medium">{t("recentRequests.columns.name")}</th>
                  <th className="pb-2 px-4 font-medium">{t("recentRequests.columns.user")}</th>
                  <th className="pb-2 px-4 font-medium">{t("recentRequests.columns.channel")}</th>
                  <th className="pb-2 px-4 font-medium text-right">{t("recentRequests.columns.tokens")}</th>
                  <th className="pb-2 px-4 font-medium text-right">
                    {t("recentRequests.columns.duration")}
                  </th>
                  <th className="pb-2 pl-4 font-medium">{t("recentRequests.columns.status")}</th>
                </tr>
              </thead>
              <tbody>
                {traces.map((t) => (
                  <tr key={t.id} className="border-b last:border-0">
                    <td className="py-2.5 pr-4 text-muted-foreground whitespace-nowrap">
                      {formatRelativeTime(t.created_at)}
                    </td>
                    <td className="py-2.5 px-4 max-w-[160px] truncate">
                      {t.name || "--"}
                    </td>
                    <td className="py-2.5 px-4 font-mono text-xs">
                      {t.user_id ? formatUserLabel(t.user_id, resolve) : "--"}
                    </td>
                    <td className="py-2.5 px-4">{t.channel || "--"}</td>
                    <td className="py-2.5 px-4 text-right tabular-nums">
                      {formatTokens(
                        t.total_input_tokens + t.total_output_tokens,
                      )}
                    </td>
                    <td className="py-2.5 px-4 text-right tabular-nums">
                      {formatDuration(t.duration_ms)}
                    </td>
                    <td className="py-2.5 pl-4">
                      <StatusBadge
                        status={
                          t.status === "completed"
                            ? "success"
                            : t.status === "error"
                              ? "error"
                              : t.status === "running"
                                ? "info"
                                : "default"
                        }
                        label={t.status}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
