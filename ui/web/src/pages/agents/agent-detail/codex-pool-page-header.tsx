import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, ArrowLeft } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ROUTES } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { strategyLabelKey } from "./agent-display-utils";
import type { EffectiveChatGPTOAuthRoutingStrategy } from "@/types/agent";

type SummaryTone = "healthy" | "warning" | "manual";

interface CodexPoolPageHeaderProps {
  title: string;
  savedStrategy: EffectiveChatGPTOAuthRoutingStrategy;
  summaryTone: SummaryTone;
  overrideMode: "inherit" | "custom";
  recentRequestCount: number;
  runtimeHealthyCount: number;
  runtimeDegradedCount: number;
  runtimeCriticalCount: number;
  isDirty: boolean;
  canManageProviders: boolean;
  isEligible: boolean;
  onBack: () => void;
}

export function CodexPoolPageHeader({
  title,
  savedStrategy,
  summaryTone,
  overrideMode,
  recentRequestCount,
  runtimeHealthyCount,
  runtimeDegradedCount,
  runtimeCriticalCount,
  isDirty,
  canManageProviders,
  isEligible,
  onBack,
}: CodexPoolPageHeaderProps) {
  const { t } = useTranslation("agents");

  return (
    <>
      <section className="shrink-0 rounded-xl border bg-card/70 px-3 py-2.5 shadow-sm sm:px-4 sm:py-3 [@media(max-height:760px)]:py-2">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between [@media(max-height:760px)]:gap-2">
          <div className="min-w-0 flex-1">
            <div className="flex flex-col items-start gap-1.5 [@media(max-height:760px)]:flex-row [@media(max-height:760px)]:items-center [@media(max-height:760px)]:gap-2">
              <Button
                variant="ghost"
                size="sm"
                className="mb-1.5 h-7 gap-1.5 px-2 text-xs sm:h-8 sm:px-2.5 sm:text-sm [@media(max-height:760px)]:mb-0 [@media(max-height:760px)]:h-7 [@media(max-height:760px)]:px-1.5"
                onClick={onBack}
              >
                <ArrowLeft className="h-3.5 w-3.5" />
                {t("chatgptOAuthRouting.backToAgent")}
              </Button>

              <h1 className="text-xl font-semibold tracking-tight sm:text-2xl [@media(max-height:760px)]:text-lg">
                {t("chatgptOAuthRouting.pageTitle")}
              </h1>
            </div>
            <p className="mt-1 max-w-3xl text-xs text-muted-foreground sm:text-sm [@media(max-height:760px)]:hidden">
              {t("chatgptOAuthRouting.pageDescription", { name: title })}
            </p>

            <div className="mt-2 flex flex-wrap items-center gap-1.5 [@media(max-height:760px)]:mt-1.5">
              <Badge
                variant="outline"
                className={cn(
                  "px-2.5 py-1 font-semibold",
                  summaryTone === "healthy" &&
                    "border-emerald-500/30 bg-emerald-500/[0.07] text-emerald-700 dark:text-emerald-200",
                  summaryTone === "warning" &&
                    "border-amber-500/30 bg-amber-500/[0.08] text-amber-800 dark:text-amber-200",
                  summaryTone === "manual" && "border-border/70 bg-muted/20",
                )}
              >
                {t(`chatgptOAuthRouting.verdict.${summaryTone}.title`)}
              </Badge>
              <Badge variant="outline">
                {t(strategyLabelKey(savedStrategy))}
              </Badge>
              <Badge variant="outline">
                {overrideMode === "inherit"
                  ? t("chatgptOAuthRouting.mode.inherit")
                  : t("chatgptOAuthRouting.mode.custom")}
              </Badge>
              <Badge variant="outline" className="[@media(max-height:760px)]:hidden">
                {recentRequestCount > 0
                  ? t("chatgptOAuthRouting.sampleBadge", { count: recentRequestCount })
                  : t("chatgptOAuthRouting.noSampleBadge")}
              </Badge>
              <Badge variant="success">
                {t("chatgptOAuthRouting.healthState.healthy")} {runtimeHealthyCount}
              </Badge>
              {runtimeDegradedCount > 0 ? (
                <Badge variant="warning">
                  {t("chatgptOAuthRouting.healthState.degraded")} {runtimeDegradedCount}
                </Badge>
              ) : null}
              {runtimeCriticalCount > 0 ? (
                <Badge variant="destructive">
                  {t("chatgptOAuthRouting.healthState.critical")} {runtimeCriticalCount}
                </Badge>
              ) : null}
              {isDirty ? (
                <Badge variant="warning">
                  {t("chatgptOAuthRouting.draftBadge")}
                </Badge>
              ) : null}
            </div>
          </div>

          {canManageProviders ? (
            <Button
              asChild
              variant="outline"
              size="sm"
              className="h-8 shrink-0 self-start px-3 [@media(max-height:760px)]:h-7"
            >
              <Link to={ROUTES.PROVIDERS}>
                {t("chatgptOAuthRouting.openProviders")}
              </Link>
            </Button>
          ) : null}
        </div>
      </section>

      {!isEligible ? (
        <Alert className="mt-3 shrink-0">
          <AlertTriangle className="h-4 w-4" />
          <AlertTitle>
            {t("chatgptOAuthRouting.pageUnsupportedTitle")}
          </AlertTitle>
          <AlertDescription>
            {t("chatgptOAuthRouting.pageUnsupportedDescription")}
          </AlertDescription>
        </Alert>
      ) : null}
    </>
  );
}

