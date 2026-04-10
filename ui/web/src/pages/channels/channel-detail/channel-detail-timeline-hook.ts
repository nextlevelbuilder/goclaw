import { useMemo } from "react";
import type { TFunction } from "i18next";
import { formatRelativeTime } from "../channels-status-view";
import type { ChannelRuntimeStatus } from "@/types/channel";

export interface TimelineItem {
  label: string;
  value: string;
}

export function useChannelTimeline(
  status: ChannelRuntimeStatus | null,
  t: TFunction,
): TimelineItem[] {
  return useMemo(() => {
    const items: TimelineItem[] = [];
    const firstFailed = formatRelativeTime(status?.first_failed_at);
    const lastChecked = formatRelativeTime(status?.checked_at);
    const lastHealthy = formatRelativeTime(status?.last_healthy_at);

    if (firstFailed) {
      items.push({
        label: t("detail.timeline.firstFailed", { defaultValue: "First failed" }),
        value: firstFailed,
      });
    }
    if (lastChecked) {
      items.push({
        label: t("detail.timeline.lastChecked", { defaultValue: "Last checked" }),
        value: lastChecked,
      });
    }
    if (status?.consecutive_failures) {
      items.push({
        label: t("detail.timeline.failures", { defaultValue: "Failures" }),
        value: t("detail.timeline.failureStreak", {
          defaultValue: "{{count}} in a row",
          count: status.consecutive_failures,
        }),
      });
    } else if (status?.failure_count) {
      items.push({
        label: t("detail.timeline.failures", { defaultValue: "Failures" }),
        value: t("detail.timeline.failureTotal", {
          defaultValue: "{{count}} total",
          count: status.failure_count,
        }),
      });
    }
    if (lastHealthy) {
      items.push({
        label: t("detail.timeline.lastHealthy", { defaultValue: "Last healthy" }),
        value: lastHealthy,
      });
    }

    return items;
  }, [
    status?.checked_at,
    status?.consecutive_failures,
    status?.failure_count,
    status?.first_failed_at,
    status?.last_healthy_at,
    t,
  ]);
}
