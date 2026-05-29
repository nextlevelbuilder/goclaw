import { useEffect, useState } from "react";
import { Cookie, Globe2, Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { InfoLabel } from "@/components/shared/info-label";

type ToolsData = Record<string, any>;
type BrowserConfig = {
  enabled?: boolean;
  headless?: boolean;
  remote_url?: string;
  action_timeout_ms?: number;
  idle_timeout_ms?: number;
  max_pages?: number;
  cookie_sync_enabled?: boolean;
};

interface Props {
  data: ToolsData | undefined;
  onSave: (value: ToolsData) => Promise<void>;
  saving: boolean;
}

const toNumber = (value: string) => {
  if (value.trim() === "") return undefined;
  const n = Number(value);
  return Number.isFinite(n) ? n : undefined;
};

export function ToolsBrowserSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<ToolsData>(data ?? {});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? {});
    setDirty(false);
  }, [data]);

  const browser = (draft.browser ?? {}) as BrowserConfig;
  const updateBrowser = (patch: Partial<BrowserConfig>) => {
    setDraft((prev) => ({
      ...prev,
      browser: { ...(prev.browser ?? {}), ...patch },
    }));
    setDirty(true);
  };

  if (!data) return null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          <Globe2 className="h-4 w-4" />
          {t("browser.title")}
        </CardTitle>
        <CardDescription>{t("browser.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
            <div className="space-y-1">
              <Label className="text-sm font-medium">{t("browser.enabled")}</Label>
              <p className="text-xs text-muted-foreground">{t("browser.enabledHint")}</p>
            </div>
            <Switch checked={browser.enabled !== false} onCheckedChange={(v) => updateBrowser({ enabled: v })} />
          </div>

          <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
            <div className="space-y-1">
              <Label className="text-sm font-medium">{t("browser.headless")}</Label>
              <p className="text-xs text-muted-foreground">{t("browser.headlessHint")}</p>
            </div>
            <Switch checked={browser.headless !== false} onCheckedChange={(v) => updateBrowser({ headless: v })} />
          </div>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("browser.remoteUrlTip")}>{t("browser.remoteUrl")}</InfoLabel>
            <Input
              value={browser.remote_url ?? ""}
              onChange={(e) => updateBrowser({ remote_url: e.target.value })}
              placeholder="ws://chrome:9222"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("browser.maxPagesTip")}>{t("browser.maxPages")}</InfoLabel>
            <Input
              type="number"
              min={1}
              value={browser.max_pages ?? ""}
              onChange={(e) => updateBrowser({ max_pages: toNumber(e.target.value) })}
              placeholder="5"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("browser.actionTimeoutTip")}>{t("browser.actionTimeout")}</InfoLabel>
            <Input
              type="number"
              min={1000}
              value={browser.action_timeout_ms ?? ""}
              onChange={(e) => updateBrowser({ action_timeout_ms: toNumber(e.target.value) })}
              placeholder="30000"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("browser.idleTimeoutTip")}>{t("browser.idleTimeout")}</InfoLabel>
            <Input
              type="number"
              min={0}
              value={browser.idle_timeout_ms ?? ""}
              onChange={(e) => updateBrowser({ idle_timeout_ms: toNumber(e.target.value) })}
              placeholder="600000"
            />
          </div>
        </div>

        <div className="flex items-center justify-between gap-4 rounded-md border px-3 py-2.5">
          <div className="flex items-start gap-3">
            <Cookie className="mt-0.5 h-4 w-4 shrink-0 text-amber-500" />
            <div className="space-y-1">
              <Label className="text-sm font-medium">{t("browser.cookieSync")}</Label>
              <p className="text-xs text-muted-foreground">{t("browser.cookieSyncHint")}</p>
            </div>
          </div>
          <Switch
            checked={browser.cookie_sync_enabled !== false}
            onCheckedChange={(v) => updateBrowser({ cookie_sync_enabled: v })}
            className="shrink-0"
          />
        </div>

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={() => onSave(draft)} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
