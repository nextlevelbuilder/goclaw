import { ArrowRight } from "lucide-react";
import { Link } from "react-router";
import { StatusBadge } from "@/components/shared/status-badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ROUTES } from "@/lib/constants";
import {
  formatRelativeTime,
  formatTokens,
  formatDuration,
} from "@/lib/format";

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
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-3">
        <CardTitle className="text-base">Recent Requests</CardTitle>
        {traces.length > 0 && (
          <Link
            to={ROUTES.TRACES}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            View All <ArrowRight className="h-3 w-3" />
          </Link>
        )}
      </CardHeader>
      <CardContent>
        {traces.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            No recent requests
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-muted-foreground">
                  <th className="pb-2 pr-4 font-medium">Time</th>
                  <th className="pb-2 px-4 font-medium">Name</th>
                  <th className="pb-2 px-4 font-medium">User</th>
                  <th className="pb-2 px-4 font-medium">Channel</th>
                  <th className="pb-2 px-4 font-medium text-right">Tokens</th>
                  <th className="pb-2 px-4 font-medium text-right">
                    Duration
                  </th>
                  <th className="pb-2 pl-4 font-medium">Status</th>
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
                      {t.user_id || "--"}
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
