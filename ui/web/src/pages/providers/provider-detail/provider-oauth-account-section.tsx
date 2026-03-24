import { useTranslation } from "react-i18next";
import { Copy, Info } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { toast } from "@/stores/use-toast-store";
import type { ProviderData } from "@/types/provider";

interface ProviderOAuthAccountSectionProps {
  provider: ProviderData;
}

export function ProviderOAuthAccountSection({ provider }: ProviderOAuthAccountSectionProps) {
  const { t } = useTranslation("providers");
  const modelPrefix = `${provider.name}/`;

  const handleCopyPrefix = () => {
    navigator.clipboard.writeText(modelPrefix).catch(() => {});
    toast.success(t("detail.oauthModelPrefixCopied"));
  };

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="space-y-0.5">
        <h3 className="text-sm font-medium">{t("detail.oauthAccountUsage")}</h3>
        <p className="text-xs text-muted-foreground">{t("detail.oauthAccountUsageDesc")}</p>
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

      <Alert>
        <Info className="h-4 w-4" />
        <AlertTitle>{t("detail.oauthAccountBadge")}</AlertTitle>
        <AlertDescription>
          <p>{t("detail.oauthPreferredHint")}</p>
          <p>{t("detail.oauthRoutingHint")}</p>
          {!provider.display_name && (
            <p>{t("detail.oauthDisplayNameRecommendation")}</p>
          )}
        </AlertDescription>
      </Alert>
    </section>
  );
}
