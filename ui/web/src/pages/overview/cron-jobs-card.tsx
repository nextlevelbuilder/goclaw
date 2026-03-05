import { ArrowRight, Timer } from "lucide-react";
import { Link } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ROUTES } from "@/lib/constants";
import { formatRelativeTime } from "@/lib/format";
import type { CronJob } from "./types";

export function CronJobsCard({ jobs }: { jobs: CronJob[] }) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-3">
        <CardTitle className="text-base">Cron Jobs</CardTitle>
        {jobs.length > 0 && (
          <Link
            to={ROUTES.CRON}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            Manage <ArrowRight className="h-3 w-3" />
          </Link>
        )}
      </CardHeader>
      <CardContent>
        {jobs.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            No cron jobs configured
          </p>
        ) : (
          <div className="space-y-2.5">
            {jobs.slice(0, 5).map((job) => (
              <div
                key={job.id}
                className="flex items-center justify-between text-sm"
              >
                <div className="flex items-center gap-2">
                  <span
                    className={`h-1.5 w-1.5 rounded-full ${
                      job.enabled
                        ? "bg-emerald-500"
                        : "bg-muted-foreground/40"
                    }`}
                  />
                  <span
                    className={
                      job.enabled ? "" : "text-muted-foreground"
                    }
                  >
                    {job.name}
                  </span>
                </div>
                <span className="flex items-center gap-1 text-xs text-muted-foreground">
                  {job.enabled && job.state.nextRunAtMs ? (
                    <>
                      <Timer className="h-3 w-3" />
                      {formatRelativeTime(
                        new Date(job.state.nextRunAtMs),
                      ).replace(" ago", "")}
                    </>
                  ) : !job.enabled ? (
                    "disabled"
                  ) : (
                    "--"
                  )}
                </span>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
