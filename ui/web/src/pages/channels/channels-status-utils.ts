import type { TFunction } from "i18next";
import i18next from "i18next";
import type {
  ChannelInstanceData,
  ChannelRuntimeStatus,
} from "@/types/channel";

export type ChannelStatus = ChannelRuntimeStatus;

export const channelTypeLabels: Record<string, string> = {
  telegram: "Telegram",
  discord: "Discord",
  slack: "Slack",
  feishu: "Feishu / Lark",
  zalo_oa: "Zalo OA",
  zalo_personal: "Zalo Personal",
  whatsapp: "WhatsApp",
};

export type BadgeVariant =
  | "secondary"
  | "success"
  | "warning"
  | "info"
  | "destructive";

export interface ChannelStatusMeta {
  dotClass: string;
  badgeVariant: BadgeVariant;
  label: string;
  surfaceClass: string;
  priority: number;
  attention: boolean;
}

export interface ChannelRemediationMeta {
  target: "credentials" | "advanced" | "reauth" | "details";
  label: string;
  headline: string;
  hint?: string;
}

export function translateChannelText(key: string, defaultValue: string) {
  const value = i18next.t(`channels:${key}`, { defaultValue });
  return typeof value === "string" && value.length > 0 ? value : defaultValue;
}

export function getRelativeUnit(diffSeconds: number) {
  const abs = Math.abs(diffSeconds);
  if (abs < 60) return { value: Math.round(diffSeconds), unit: "second" as const };
  if (abs < 3600) return { value: Math.round(diffSeconds / 60), unit: "minute" as const };
  if (abs < 86400) return { value: Math.round(diffSeconds / 3600), unit: "hour" as const };
  if (abs < 604800) return { value: Math.round(diffSeconds / 86400), unit: "day" as const };
  return { value: Math.round(diffSeconds / 604800), unit: "week" as const };
}

export function formatRelativeTime(value?: string) {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  if (date.getUTCFullYear() <= 1) return null;
  const diffSeconds = (date.getTime() - Date.now()) / 1000;
  const { value: relativeValue, unit } = getRelativeUnit(diffSeconds);
  const language = i18next.resolvedLanguage || i18next.language || undefined;
  return new Intl.RelativeTimeFormat(language, { numeric: "auto" }).format(
    relativeValue,
    unit,
  );
}

export function getChannelStatusFallback(
  instance: Pick<
    ChannelInstanceData,
    "enabled" | "has_credentials" | "channel_type"
  >,
): ChannelRuntimeStatus | null {
  if (!instance.enabled || instance.has_credentials) {
    return null;
  }

  if (instance.channel_type === "zalo_personal") {
    return {
      enabled: true,
      running: false,
      state: "failed",
      summary: translateChannelText(
        "fallback.authRequiredSummary",
        "Authentication required",
      ),
      detail: translateChannelText(
        "fallback.authRequiredDetail",
        "Channel instance is enabled but requires sign-in before it can connect.",
      ),
      failure_kind: "auth",
      retryable: false,
      remediation: {
        code: "reauth",
        headline: translateChannelText(
          "fallback.authRequiredHeadline",
          "Reconnect the channel session",
        ),
        hint: translateChannelText(
          "fallback.authRequiredHint",
          "Authenticate this channel again to restore the current session.",
        ),
        target: "reauth",
      },
    };
  }

  return {
    enabled: true,
    running: false,
    state: "failed",
    summary: translateChannelText(
      "fallback.missingCredentialsSummary",
      "Missing credentials",
    ),
    detail: translateChannelText(
      "fallback.missingCredentialsDetail",
      "Channel instance is enabled but required credentials are incomplete.",
    ),
    failure_kind: "config",
    retryable: false,
    remediation: {
      code: "open_credentials",
      headline: translateChannelText(
        "fallback.missingCredentialsHeadline",
        "Complete required credentials",
      ),
      hint: translateChannelText(
        "fallback.missingCredentialsHint",
        "Open credentials and fill the missing or invalid values for this channel.",
      ),
      target: "credentials",
    },
  };
}

export function getRenderableChannelStatus(
  status: ChannelRuntimeStatus | null | undefined,
  instance?: Pick<
    ChannelInstanceData,
    "enabled" | "has_credentials" | "channel_type"
  >,
): ChannelRuntimeStatus | null {
  if (status) return status;
  if (!instance) return null;
  return getChannelStatusFallback(instance);
}

export function getChannelCheckedLabel(
  status: ChannelRuntimeStatus | null | undefined,
  t: TFunction,
) {
  const relative = formatRelativeTime(status?.checked_at);
  if (!relative) return null;
  return t("detail.checkedRelative", {
    defaultValue: "Checked {{value}}",
    value: relative,
  });
}

export function getChannelFailureKindLabel(
  kind: ChannelRuntimeStatus["failure_kind"],
  t: TFunction,
) {
  switch (kind) {
    case "auth":
      return t("failureKind.auth", { defaultValue: "Auth" });
    case "config":
      return t("failureKind.config", { defaultValue: "Config" });
    case "network":
      return t("failureKind.network", { defaultValue: "Network" });
    case "unknown":
      return t("failureKind.unknown", { defaultValue: "Attention" });
    default:
      return null;
  }
}

export function getChannelAttentionPriority(
  status: ChannelRuntimeStatus | null | undefined,
  enabled = true,
) {
  if (!enabled) return 0;
  if (!status) return 0;
  switch (status?.state) {
    case "failed":
      return 5;
    case "degraded":
      return 4;
    case "starting":
      return 3;
    case "registered":
      return 2;
    case "stopped":
      return 1;
    case "healthy":
      return 0;
    default:
      return status?.running ? 0 : 1;
  }
}

export function getChannelStatusMeta(
  status: ChannelRuntimeStatus | null | undefined,
  enabled: boolean,
  t: TFunction,
): ChannelStatusMeta {
  if (!enabled) {
    return {
      dotClass: "bg-muted-foreground/40",
      badgeVariant: "secondary",
      label: t("disabled", { defaultValue: "Disabled" }),
      surfaceClass: "border-border bg-card",
      priority: 0,
      attention: false,
    };
  }

  if (!status) {
    return {
      dotClass: "bg-slate-300 dark:bg-slate-600",
      badgeVariant: "secondary",
      label: t("status.checking", { defaultValue: "Checking" }),
      surfaceClass: "border-border bg-card",
      priority: 0,
      attention: false,
    };
  }

  switch (status?.state) {
    case "healthy":
      return {
        dotClass: "bg-emerald-500",
        badgeVariant: "success",
        label: t("status.running", { defaultValue: "Running" }),
        surfaceClass:
          "border-emerald-200/70 bg-emerald-500/[0.04] dark:border-emerald-500/20 dark:bg-emerald-500/10",
        priority: 0,
        attention: false,
      };
    case "degraded":
      return {
        dotClass: "bg-amber-500",
        badgeVariant: "warning",
        label: t("status.degraded", { defaultValue: "Degraded" }),
        surfaceClass:
          "border-amber-200/80 bg-amber-500/[0.06] dark:border-amber-500/25 dark:bg-amber-500/10",
        priority: 4,
        attention: true,
      };
    case "starting":
      return {
        dotClass: "bg-sky-500",
        badgeVariant: "info",
        label: t("status.starting", { defaultValue: "Starting" }),
        surfaceClass:
          "border-sky-200/80 bg-sky-500/[0.05] dark:border-sky-500/25 dark:bg-sky-500/10",
        priority: 3,
        attention: true,
      };
    case "registered":
      return {
        dotClass: "bg-slate-400",
        badgeVariant: "secondary",
        label: t("status.registered", { defaultValue: "Configured" }),
        surfaceClass:
          "border-slate-200/80 bg-slate-500/[0.04] dark:border-slate-500/25 dark:bg-slate-500/10",
        priority: 2,
        attention: true,
      };
    case "failed":
      return {
        dotClass: "bg-red-500",
        badgeVariant: "destructive",
        label: t("status.failed", { defaultValue: "Failed" }),
        surfaceClass:
          "border-red-200/80 bg-red-500/[0.05] dark:border-red-500/25 dark:bg-red-500/10",
        priority: 5,
        attention: true,
      };
    case "stopped":
      return {
        dotClass: "bg-muted-foreground",
        badgeVariant: "secondary",
        label: t("status.stopped", { defaultValue: "Stopped" }),
        surfaceClass: "border-border bg-muted/20",
        priority: 1,
        attention: true,
      };
    default:
      return status?.running
        ? {
            dotClass: "bg-emerald-500",
            badgeVariant: "success",
            label: t("status.running", { defaultValue: "Running" }),
            surfaceClass:
              "border-emerald-200/70 bg-emerald-500/[0.04] dark:border-emerald-500/20 dark:bg-emerald-500/10",
            priority: 0,
            attention: false,
          }
        : {
            dotClass: "bg-muted-foreground",
            badgeVariant: "secondary",
            label: t("status.stopped", { defaultValue: "Stopped" }),
            surfaceClass: "border-border bg-muted/20",
            priority: 1,
            attention: true,
          };
  }
}

export function getChannelRemediationMeta(
  status: ChannelRuntimeStatus | null | undefined,
  supportsReauth: boolean,
  t: TFunction,
): ChannelRemediationMeta | null {
  const remediation = status?.remediation;
  if (!status || !remediation) return null;

  const rawTarget = remediation.target ?? "details";
  const target =
    rawTarget === "reauth" && !supportsReauth ? "credentials" : rawTarget;

  const label =
    target === "reauth"
      ? t("actions.reauthShort", { defaultValue: "Re-auth" })
      : target === "credentials"
        ? t("actions.openCredentials", { defaultValue: "Open credentials" })
        : target === "advanced"
          ? t("actions.openAdvanced", { defaultValue: "Open advanced" })
          : t("actions.inspect", { defaultValue: "Inspect issue" });

  return {
    target,
    label,
    headline: remediation.headline,
    hint: remediation.hint,
  };
}
