import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { DatabaseZap, RefreshCw, Save, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { useAuthStore } from "@/stores/use-auth-store";
import type { ProviderData } from "@/types/provider";
import type { UsagePricingFields } from "@/types/usage-caps";
import { useModelPricing } from "../hooks/use-model-pricing";

const PRICE_FIELDS: Array<keyof UsagePricingFields> = [
  "input", "output", "cache_read", "cache_write", "reasoning", "request", "image", "web_search",
];
const SUBSCRIPTION_TYPES = new Set(["chatgpt_oauth", "claude_cli", "bailian", "acp", "ollama"]);

export function ProviderPricingSection({ provider }: { provider: ProviderData }) {
  const { t } = useTranslation("providers");
  const [modelId, setModelId] = useState("");
  const [prices, setPrices] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const isMasterScope = useAuthStore((s) => s.isMasterScope);
  const { catalog, overrides, refreshing, syncOpenRouter, saveOverride, deleteOverride } = useModelPricing(provider.id, modelId.trim());
  const isSkipped = SUBSCRIPTION_TYPES.has(provider.provider_type);

  const pricing = useMemo(() => {
    const out: UsagePricingFields = {};
    for (const field of PRICE_FIELDS) {
      const value = prices[field]?.trim();
      if (value) out[field] = value;
    }
    return out;
  }, [prices]);

  const onSave = async () => {
    if (!modelId.trim() || Object.keys(pricing).length === 0) return;
    setSaving(true);
    try {
      await saveOverride({
        provider_id: provider.id,
        provider_type: provider.provider_type,
        model_id: modelId.trim(),
        pricing,
        enabled: true,
      });
    } finally {
      setSaving(false);
    }
  };

  const onSync = async () => {
    setSyncing(true);
    try {
      await syncOpenRouter();
    } finally {
      setSyncing(false);
    }
  };

  return (
    <section className="space-y-4 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1">
          <h3 className="text-sm font-medium">{t("pricing.title")}</h3>
          <p className="text-xs text-muted-foreground">{isSkipped ? t("pricing.skippedDescription") : t("pricing.description")}</p>
        </div>
        {isMasterScope ? (
          <Button type="button" variant="outline" size="sm" onClick={() => void onSync()} disabled={syncing} className="gap-1 self-start">
            <DatabaseZap className={`h-3.5 w-3.5${syncing ? " animate-pulse" : ""}`} />
            {t("pricing.sync")}
          </Button>
        ) : null}
      </div>

      <div className="space-y-3">
        <div className="space-y-1.5">
          <Label htmlFor="pricingModel">{t("pricing.model")}</Label>
          <Input id="pricingModel" value={modelId} onChange={(e) => setModelId(e.target.value)} placeholder="openai/gpt-4o-mini" className="text-base md:text-sm" />
        </div>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {PRICE_FIELDS.map((field) => (
            <div key={field} className="space-y-1.5">
              <Label htmlFor={`pricing-${field}`} className="text-xs">{t(`pricing.fields.${field}`)}</Label>
              <Input
                id={`pricing-${field}`}
                value={prices[field] ?? ""}
                onChange={(e) => setPrices((current) => ({ ...current, [field]: e.target.value }))}
                inputMode="decimal"
                placeholder="0.00000015"
                className="text-base md:text-sm"
              />
            </div>
          ))}
        </div>
        <Button type="button" onClick={() => void onSave()} disabled={saving || !modelId.trim() || Object.keys(pricing).length === 0} className="gap-1">
          <Save className="h-4 w-4" />
          {t("pricing.saveOverride")}
        </Button>
      </div>

      {catalog.length > 0 ? (
        <div className="space-y-2">
          <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
            <RefreshCw className={`h-3.5 w-3.5${refreshing ? " animate-spin" : ""}`} />
            {t("pricing.catalogMatches")}
          </div>
          <div className="flex flex-wrap gap-2">
            {catalog.map((entry) => (
              <button key={entry.id} type="button" onClick={() => { setModelId(entry.model_id); setPrices(cleanPricing(entry.pricing)); }} className="rounded-md border px-2 py-1 text-left text-xs hover:bg-muted">
                {entry.model_id}
              </button>
            ))}
          </div>
        </div>
      ) : null}

      <div className="overflow-x-auto">
        <table className="w-full min-w-[680px] text-sm">
          <thead>
            <tr className="border-b bg-muted/40">
              <th className="px-3 py-2 text-left font-medium">{t("pricing.model")}</th>
              <th className="px-3 py-2 text-left font-medium">{t("pricing.source")}</th>
              <th className="px-3 py-2 text-left font-medium">{t("pricing.configuredFields")}</th>
              <th className="px-3 py-2 text-right font-medium">{t("columns.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {overrides.length === 0 ? (
              <tr><td colSpan={4} className="px-3 py-5 text-center text-muted-foreground">{t("pricing.emptyOverrides")}</td></tr>
            ) : overrides.map((override) => (
              <tr key={override.id} className="border-b last:border-0">
                <td className="px-3 py-2 font-medium">{override.model_id}</td>
                <td className="px-3 py-2"><Badge variant="outline">{t("pricing.override")}</Badge></td>
                <td className="px-3 py-2 text-muted-foreground">{Object.keys(override.pricing ?? {}).join(", ") || "—"}</td>
                <td className="px-3 py-2 text-right">
                  <Button type="button" variant="ghost" size="icon" onClick={() => void deleteOverride(override.id)} aria-label={t("pricing.deleteOverride")}>
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function cleanPricing(fields: UsagePricingFields): Record<string, string> {
  const out: Record<string, string> = {};
  for (const field of PRICE_FIELDS) {
    const value = fields[field];
    if (value) out[field] = value;
  }
  return out;
}
