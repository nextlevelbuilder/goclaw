import type { CodexPoolRecentRequest } from "./hooks/use-codex-pool-activity";

export interface RequestAccentClasses {
  card: string;
  glow: string;
  stripe: string;
  marker: string;
  index: string;
  directBadge: string;
  pill: string;
  trace: string;
}

export const REQUEST_ACCENTS: RequestAccentClasses[] = [
  {
    card: "border-emerald-500/20",
    glow:
      "from-emerald-500/[0.12] via-emerald-500/[0.04] to-transparent dark:from-emerald-500/[0.16] dark:via-emerald-500/[0.06]",
    stripe: "from-emerald-400/90 via-teal-400/75 to-cyan-400/35",
    marker: "bg-emerald-500",
    index:
      "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-200",
    directBadge:
      "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/15 dark:text-emerald-200",
    pill:
      "border-emerald-500/15 bg-emerald-500/[0.07] text-emerald-950 dark:border-emerald-500/20 dark:bg-emerald-500/[0.12] dark:text-emerald-100",
    trace:
      "text-emerald-700 hover:border-emerald-500/25 hover:bg-emerald-500/10 hover:text-emerald-800 dark:text-emerald-200 dark:hover:border-emerald-500/30 dark:hover:bg-emerald-500/15 dark:hover:text-emerald-100",
  },
  {
    card: "border-sky-500/20",
    glow:
      "from-sky-500/[0.12] via-sky-500/[0.04] to-transparent dark:from-sky-500/[0.16] dark:via-sky-500/[0.06]",
    stripe: "from-sky-400/90 via-cyan-400/75 to-blue-400/35",
    marker: "bg-sky-500",
    index:
      "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-200",
    directBadge:
      "border-sky-500/20 bg-sky-500/10 text-sky-700 dark:border-sky-500/25 dark:bg-sky-500/15 dark:text-sky-200",
    pill:
      "border-sky-500/15 bg-sky-500/[0.07] text-sky-950 dark:border-sky-500/20 dark:bg-sky-500/[0.12] dark:text-sky-100",
    trace:
      "text-sky-700 hover:border-sky-500/25 hover:bg-sky-500/10 hover:text-sky-800 dark:text-sky-200 dark:hover:border-sky-500/30 dark:hover:bg-sky-500/15 dark:hover:text-sky-100",
  },
  {
    card: "border-amber-500/20",
    glow:
      "from-amber-500/[0.12] via-amber-500/[0.04] to-transparent dark:from-amber-500/[0.16] dark:via-amber-500/[0.06]",
    stripe: "from-amber-400/90 via-orange-400/75 to-yellow-400/35",
    marker: "bg-amber-500",
    index:
      "border-amber-500/30 bg-amber-500/10 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/15 dark:text-amber-100",
    directBadge:
      "border-amber-500/20 bg-amber-500/10 text-amber-800 dark:border-amber-500/25 dark:bg-amber-500/15 dark:text-amber-100",
    pill:
      "border-amber-500/15 bg-amber-500/[0.07] text-amber-950 dark:border-amber-500/20 dark:bg-amber-500/[0.12] dark:text-amber-50",
    trace:
      "text-amber-800 hover:border-amber-500/25 hover:bg-amber-500/10 hover:text-amber-900 dark:text-amber-100 dark:hover:border-amber-500/30 dark:hover:bg-amber-500/15 dark:hover:text-amber-50",
  },
  {
    card: "border-rose-500/20",
    glow:
      "from-rose-500/[0.12] via-rose-500/[0.04] to-transparent dark:from-rose-500/[0.16] dark:via-rose-500/[0.06]",
    stripe: "from-rose-400/90 via-pink-400/75 to-orange-300/35",
    marker: "bg-rose-500",
    index:
      "border-rose-500/30 bg-rose-500/10 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/15 dark:text-rose-200",
    directBadge:
      "border-rose-500/20 bg-rose-500/10 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/15 dark:text-rose-200",
    pill:
      "border-rose-500/15 bg-rose-500/[0.07] text-rose-950 dark:border-rose-500/20 dark:bg-rose-500/[0.12] dark:text-rose-100",
    trace:
      "text-rose-700 hover:border-rose-500/25 hover:bg-rose-500/10 hover:text-rose-800 dark:text-rose-200 dark:hover:border-rose-500/30 dark:hover:bg-rose-500/15 dark:hover:text-rose-100",
  },
];

export function requestProviderSummary(request: CodexPoolRecentRequest): string {
  const selected = request.selected_provider?.trim();
  const served = request.provider_name?.trim();
  if (request.used_failover && selected && served && selected !== served) {
    return `${selected} -> ${served}`;
  }
  return served || selected || "";
}

export function requestAccentSeed(request: CodexPoolRecentRequest): string {
  return (
    request.provider_name?.trim() ||
    request.selected_provider?.trim() ||
    requestProviderSummary(request) ||
    request.model ||
    request.span_id
  );
}

export function requestAccentClasses(seed: string): RequestAccentClasses {
  let hash = 0;
  for (const char of seed) {
    hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  }
  return REQUEST_ACCENTS[hash % REQUEST_ACCENTS.length] ?? REQUEST_ACCENTS[0]!;
}
