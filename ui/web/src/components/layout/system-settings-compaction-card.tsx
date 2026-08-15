import { useTranslation } from "react-i18next";
import { Archive, Clock, Hash, Info } from "lucide-react";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

interface SystemSettingsCompactionCardProps {
  compProvider: string;
  setCompProvider: (v: string) => void;
  compModel: string;
  setCompModel: (v: string) => void;
  compThreshold: string;
  setCompThreshold: (v: string) => void;
  compKeepRecent: string;
  setCompKeepRecent: (v: string) => void;
  compMaxTokens: string;
  setCompMaxTokens: (v: string) => void;
}

export function SystemSettingsCompactionCard({
  compProvider, setCompProvider,
  compModel, setCompModel,
  compThreshold, setCompThreshold,
  compKeepRecent, setCompKeepRecent,
  compMaxTokens, setCompMaxTokens,
}: SystemSettingsCompactionCardProps) {
  const { t } = useTranslation("system-settings");

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("compaction.title")}</CardTitle>
        <CardDescription>{t("compaction.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-0 pt-0">
        <div className="border-b pb-4">
          <ProviderModelSelect
            provider={compProvider}
            onProviderChange={setCompProvider}
            model={compModel}
            onModelChange={setCompModel}
            allowEmpty
            providerPlaceholder={t("compaction.providerPlaceholder")}
          />
        </div>

        <div className="flex items-start justify-between gap-4 border-b py-4">
          <div className="flex items-start gap-3">
            <Archive className="mt-0.5 h-4 w-4 shrink-0 text-orange-500" />
            <div className="space-y-0.5">
              <Label className="text-sm font-medium">{t("compaction.threshold")}</Label>
              <p className="text-xs text-muted-foreground">{t("compaction.thresholdHint")}</p>
            </div>
          </div>
          <Input type="number" value={compThreshold} onChange={(e) => setCompThreshold(e.target.value)} placeholder="200" min={1} className="w-24 shrink-0 text-base md:text-sm" />
        </div>

        <div className="flex items-start justify-between gap-4 border-b py-4">
          <div className="flex items-start gap-3">
            <Clock className="mt-0.5 h-4 w-4 shrink-0 text-blue-500" />
            <div className="space-y-0.5">
              <Label className="text-sm font-medium">{t("compaction.keepRecent")}</Label>
              <p className="text-xs text-muted-foreground">{t("compaction.keepRecentHint")}</p>
            </div>
          </div>
          <Input type="number" value={compKeepRecent} onChange={(e) => setCompKeepRecent(e.target.value)} placeholder="40" min={1} className="w-24 shrink-0 text-base md:text-sm" />
        </div>

        <div className="flex items-start justify-between gap-4 border-b py-4">
          <div className="flex items-start gap-3">
            <Hash className="mt-0.5 h-4 w-4 shrink-0 text-orange-500" />
            <div className="space-y-0.5">
              <Label className="text-sm font-medium">{t("compaction.maxTokens")}</Label>
              <p className="text-xs text-muted-foreground">{t("compaction.maxTokensHint")}</p>
            </div>
          </div>
          <Input type="number" value={compMaxTokens} onChange={(e) => setCompMaxTokens(e.target.value)} placeholder="4096" min={256} className="w-24 shrink-0 text-base md:text-sm" />
        </div>

        <div className="flex items-start gap-2 rounded-md border border-orange-200 bg-orange-50 px-3 py-2 mt-4 text-xs text-orange-700 dark:border-orange-800 dark:bg-orange-950/30 dark:text-orange-300">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{t("compaction.info")}</span>
        </div>
      </CardContent>
    </Card>
  );
}
