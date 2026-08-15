import { ArrowRight, Timer } from "lucide-react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ROUTES } from "@/lib/constants";
import { formatRelativeTime } from "@/lib/format";
import type { CronJob } from "./types";

export function CronJobsCard({ jobs }: { jobs: CronJob[] }) {
  const { t } = useTranslation("overview");
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-3">
        <CardTitle className="text-base">{t("cronJobs.title")}</CardTitle>
        {jobs.length > 0 && (
          <Link
            to={ROUTES.CRON}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            {t("cronJobs.manage")} <ArrowRight className="h-3 w-3" />
          </Link>
        )}
      </CardHeader>
      <CardContent>
        {jobs.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {t("cronJobs.noJobs")}
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
                  <Link
                    to={`/cron/${job.id}`}
                    className={`hover:underline ${
                      job.enabled ? "" : "text-muted-foreground"
                    }`}
                  >
                    {job.name}
                  </Link>
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
                    t("cronJobs.disabled")
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
