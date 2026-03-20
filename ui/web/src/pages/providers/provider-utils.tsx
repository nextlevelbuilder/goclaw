import { Key, Link2, ShieldCheck } from "lucide-react";
import { useTranslation } from "react-i18next";
import { PROVIDER_TYPES } from "@/constants/providers";
import type { ProviderData } from "./hooks/use-providers";

type BadgeVariant = "default" | "secondary" | "outline";

const SPECIAL_VARIANTS: Record<string, BadgeVariant> = {
  anthropic_native: "default",
  chatgpt_oauth: "default",
  claude_cli: "outline",
  acp: "outline",
};

/** Derive badge labels from PROVIDER_TYPES constant (single source of truth). */
export const PROVIDER_TYPE_BADGE: Record<string, { label: string; variant: BadgeVariant }> = Object.fromEntries(
  PROVIDER_TYPES.map((pt) => [
    pt.value,
    { label: pt.label.replace(/ \(.*\)$/, ""), variant: SPECIAL_VARIANTS[pt.value] ?? "secondary" },
  ]),
);

/** Shared API key status indicator. */
export function ProviderApiKeyBadge({ provider }: { provider: ProviderData }) {
  const { t } = useTranslation("providers");
  if (provider.provider_type === "chatgpt_oauth") {
    return (
      <span className="flex items-center gap-1 text-[11px] text-emerald-600 dark:text-emerald-400">
        <Link2 className="h-3 w-3" />{t("card.oauthLinked")}
      </span>
    );
  }
  if (provider.provider_type === "claude_cli") {
    return (
      <span className="flex items-center gap-1 text-[11px] text-emerald-600 dark:text-emerald-400">
        <ShieldCheck className="h-3 w-3" />{t("card.authenticated")}
      </span>
    );
  }
  if (provider.api_key === "***") {
    return (
      <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
        <Key className="h-3 w-3" />{t("card.apiKeySet")}
      </span>
    );
  }
  return (
    <span className="text-[11px] text-muted-foreground/60">{t("apiKey.notSet")}</span>
  );
}
