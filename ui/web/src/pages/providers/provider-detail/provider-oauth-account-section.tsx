import { useTranslation } from "react-i18next";
import { Copy, Info } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { ChatGPTOAuthQuotaStrip } from "@/pages/agents/agent-detail/chatgpt-oauth-quota-strip";
import type { ChatGPTOAuthProviderQuota } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-quotas";
import type { ChatGPTOAuthAvailability } from "@/pages/providers/hooks/use-chatgpt-oauth-provider-statuses";
import { toast } from "@/stores/use-toast-store";
import type { ProviderData } from "@/types/provider";

interface ProviderOAuthAccountSectionProps {
  provider: ProviderData;
  managedByProvider?: ProviderData;
  managedMemberCount?: number;
  availability: ChatGPTOAuthAvailability;
  quota?: ChatGPTOAuthProviderQuota | null;
  quotaLoading?: boolean;
}

export function ProviderOAuthAccountSection({
  provider,
  managedByProvider,
  managedMemberCount = 0,
  availability,
  quota,
  quotaLoading = false,
}: ProviderOAuthAccountSectionProps) {
  const { t } = useTranslation("providers");
  const modelPrefix = `${provider.name}/`;
  const role: "member" | "owner" | "standalone" = managedByProvider
    ? "member"
    : managedMemberCount > 0
      ? "owner"
      : "standalone";

  const handleCopyPrefix = () => {
    navigator.clipboard.writeText(modelPrefix).catch(() => {});
    toast.success(t("detail.oauthModelPrefixCopied"));
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="space-y-0.5">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="text-sm font-medium">{t("detail.oauthAccountUsage")}</h3>
          <Badge variant="outline" className="h-6 px-2 text-[11px]">
            {t(`detail.oauthPoolRole.${role}`)}
          </Badge>
        </div>
        <p className="text-xs text-muted-foreground">
          {managedByProvider
            ? t("detail.oauthAccountUsageManagedDesc", {
                provider: managedByProvider.display_name || managedByProvider.name,
              })
            : managedMemberCount > 0
              ? t("detail.oauthAccountUsageOwnerDesc", {
                  count: managedMemberCount,
                })
              : t("detail.oauthAccountUsageDesc")}
        </p>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="space-y-2">
          <Label>{t("detail.oauthAliasLabel")}</Label>
          <code className="block rounded-md border bg-muted px-3 py-2 font-mono text-sm text-muted-foreground">
            {provider.name}
          </code>
        </div>

        <div className="space-y-2">
          <Label>{t("detail.oauthModelPrefix")}</Label>
          <div className="flex items-center gap-2">
            <code className="flex-1 rounded-md border bg-muted px-3 py-2 font-mono text-sm text-muted-foreground">
              {modelPrefix}
            </code>
            <Button type="button" variant="outline" size="icon" className="size-9 shrink-0" onClick={handleCopyPrefix}>
              <Copy className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </div>

      <div className="rounded-lg border bg-muted/10 p-3">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div className="space-y-1 lg:max-w-xs">
            <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {t("detail.oauthQuotaTitle")}
            </p>
            <p className="text-xs text-muted-foreground">
              {t("detail.oauthQuotaDescription")}
            </p>
          </div>

          <div className="min-w-0 flex-1 rounded-md border bg-background/70 px-3 py-2">
            {availability === "ready" || quotaLoading || quota ? (
              <ChatGPTOAuthQuotaStrip
                quota={quota}
                loading={quotaLoading}
                embedded
                showSignalBadges={false}
                translationNamespace="providers"
                translationKeyPrefix="quota"
                className="w-full"
              />
            ) : (
              <Badge variant="outline" className="h-5 w-fit px-1.5 text-[10px]">
                {t(
                  availability === "disabled"
                    ? "list.status.disabled"
                    : "list.status.needsSignIn",
                )}
              </Badge>
            )}
          </div>
        </div>
      </div>

      <Alert>
        <Info className="h-4 w-4" />
        <AlertTitle>{t("detail.oauthAccountBadge")}</AlertTitle>
        <AlertDescription>
          {managedByProvider ? (
            <p>
              {t("detail.oauthManagedByHint", {
                provider: managedByProvider.display_name || managedByProvider.name,
              })}
            </p>
          ) : (
            <>
              <p>{t("detail.oauthPreferredHint")}</p>
              <p>{t("detail.oauthProviderDefaultHint")}</p>
              <p>{t("detail.oauthRoutingHint")}</p>
              {!provider.display_name && (
                <p>{t("detail.oauthDisplayNameRecommendation")}</p>
              )}
            </>
          )}
        </AlertDescription>
      </Alert>
    </section>
  );
}
