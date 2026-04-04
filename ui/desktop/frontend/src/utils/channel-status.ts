import type { ChannelStatus } from "../types/channel";

interface ChannelStatusDisplay {
  dotColor: string;
  statusText: string;
}

export function getChannelStatusDisplay(
  enabled: boolean,
  status: ChannelStatus | null,
  t: (key: string, options?: Record<string, unknown>) => string,
): ChannelStatusDisplay {
  // Default: disabled
  let dotColor = "bg-gray-400";
  let statusText = t("status.disabled");

  if (enabled) {
    switch (status?.state) {
      case "healthy":
        dotColor = "bg-emerald-500";
        statusText = t("status.running");
        break;
      case "degraded":
        dotColor = "bg-amber-500";
        statusText = t("status.degraded", { defaultValue: "Degraded" });
        break;
      case "starting":
        dotColor = "bg-sky-500";
        statusText = t("status.starting", { defaultValue: "Starting" });
        break;
      case "registered":
        dotColor = "bg-slate-400";
        statusText = t("status.registered", { defaultValue: "Configured" });
        break;
      case "failed":
        dotColor = "bg-red-500";
        statusText = t("status.failed", { defaultValue: "Failed" });
        break;
      case "stopped":
        dotColor = "bg-gray-400";
        statusText = t("status.stopped");
        break;
      default:
        dotColor = status?.running ? "bg-emerald-500" : "bg-gray-400";
        statusText = status?.running ? t("status.running") : t("status.stopped");
    }
  }

  return { dotColor, statusText };
}
