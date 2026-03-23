import { Archive, Clock, Hash, Info } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ProviderModelSelect } from "@/components/shared/provider-model-select";

export interface PendingCompactionValues {
  threshold?: number;
  keep_recent?: number;
  max_tokens?: number;
  provider?: string;
  model?: string;
}

interface Props {
  value: PendingCompactionValues;
  onChange: (v: PendingCompactionValues) => void;
}

/** Global pending message compaction thresholds with visual emphasis. */
export function BehaviorPendingCompactionCard({ value, onChange }: Props) {
  const { t } = useTranslation("config");

  const update = (patch: Partial<PendingCompactionValues>) =>
    onChange({ ...value, ...patch });

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">
          {t("behavior.pendingCompactionTitle")}
        </CardTitle>
        <CardDescription>
          {t("behavior.pendingCompactionDescription")}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-0">
        {/* Provider & Model */}
        <div className="border-b py-4">
          <ProviderModelSelect
            provider={value.provider ?? ""}
            onProviderChange={(v) => update({ provider: v })}
            model={value.model ?? ""}
            onModelChange={(v) => update({ model: v })}
            providerLabel={t("behavior.pendingCompactionProvider")}
            modelLabel={t("behavior.pendingCompactionModel")}
            providerTip={t("behavior.pendingCompactionProviderTip")}
            modelTip={t("behavior.pendingCompactionModelTip")}
            providerPlaceholder={t("behavior.pendingCompactionProviderPlaceholder")}
            modelPlaceholder={t("behavior.pendingCompactionModelPlaceholder")}
            allowEmpty
          />
        </div>

        {/* Threshold */}
        <div className="border-b py-4">
          <div className="flex items-start justify-between gap-4">
            <div className="flex items-start gap-3">
              <Archive className="mt-0.5 h-4 w-4 shrink-0 text-orange-500" />
              <div className="space-y-1">
                <Label className="text-sm font-medium">
                  {t("behavior.pendingCompactionThreshold")}
                </Label>
                <p className="text-xs text-muted-foreground">
                  {t("behavior.pendingCompactionThresholdTip")}
                </p>
              </div>
            </div>
            <Input
              type="number"
              value={value.threshold ?? ""}
              onChange={(e) => update({ threshold: Number(e.target.value) })}
              placeholder="50"
              min={0}
              className="w-24 shrink-0"
            />
          </div>
        </div>

        {/* Keep Recent */}
        <div className="border-b py-4">
          <div className="flex items-start justify-between gap-4">
            <div className="flex items-start gap-3">
              <Clock className="mt-0.5 h-4 w-4 shrink-0 text-blue-500" />
              <div className="space-y-1">
                <Label className="text-sm font-medium">
                  {t("behavior.pendingCompactionKeepRecent")}
                </Label>
                <p className="text-xs text-muted-foreground">
                  {t("behavior.pendingCompactionKeepRecentTip")}
                </p>
              </div>
            </div>
            <Input
              type="number"
              value={value.keep_recent ?? ""}
              onChange={(e) => update({ keep_recent: Number(e.target.value) })}
              placeholder="15"
              min={1}
              className="w-24 shrink-0"
            />
          </div>
        </div>

        {/* Max Tokens */}
        <div className="py-4">
          <div className="flex items-start justify-between gap-4">
            <div className="flex items-start gap-3">
              <Hash className="mt-0.5 h-4 w-4 shrink-0 text-orange-500" />
              <div className="space-y-1">
                <Label className="text-sm font-medium">
                  {t("behavior.pendingCompactionMaxTokens")}
                </Label>
                <p className="text-xs text-muted-foreground">
                  {t("behavior.pendingCompactionMaxTokensTip")}
                </p>
              </div>
            </div>
            <Input
              type="number"
              value={value.max_tokens ?? ""}
              onChange={(e) => update({ max_tokens: Number(e.target.value) })}
              placeholder="4096"
              min={256}
              className="w-24 shrink-0"
            />
          </div>
        </div>

        {/* Info banner */}
        <div className="flex items-start gap-2 rounded-md border border-orange-200 bg-orange-50 px-3 py-2 text-xs text-orange-700 dark:border-orange-800 dark:bg-orange-950/30 dark:text-orange-300">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{t("behavior.pendingCompactionInfo")}</span>
        </div>
      </CardContent>
    </Card>
  );
}
