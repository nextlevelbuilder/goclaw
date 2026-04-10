import { useState, useEffect, type ChangeEvent } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { InfoLabel } from "@/components/shared/info-label";
import { WebSearchProviderSequence, type ProviderId } from "./web-search-provider-sequence";

/* eslint-disable @typescript-eslint/no-explicit-any */
type ToolsData = Record<string, any>;

interface Props {
  data: ToolsData | undefined;
  onSave: (value: ToolsData) => Promise<void>;
  saving: boolean;
}

const SORTABLE_PROVIDER_ORDER: ProviderId[] = ["exa", "tavily", "brave"];

function normalizeProviderOrder(value: unknown): ProviderId[] {
  const result: ProviderId[] = [];
  const seen = new Set<ProviderId>();

  if (Array.isArray(value)) {
    for (const entry of value) {
      if (
        (entry === "exa" || entry === "tavily" || entry === "brave") &&
        !seen.has(entry)
      ) {
        seen.add(entry);
        result.push(entry);
      }
    }
  }

  for (const provider of SORTABLE_PROVIDER_ORDER) {
    if (!seen.has(provider)) {
      result.push(provider);
    }
  }

  return result;
}

export function ToolsWebSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<ToolsData>(data ?? {});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? {});
    setDirty(false);
  }, [data]);

  const updateNested = (section: string, patch: Record<string, any>) => {
    setDraft((prev) => ({
      ...prev,
      [section]: { ...(prev[section] ?? {}), ...patch },
    }));
    setDirty(true);
  };

  const handleSave = () => {
    const toSave: ToolsData = { ...draft };
    const web = { ...(toSave.web ?? {}) };
    web.duckduckgo = { ...(web.duckduckgo ?? {}), enabled: true };
    web.provider_order = normalizeProviderOrder(web.provider_order);
    toSave.web = web;
    onSave(toSave);
  };

  if (!data) return null;

  const web = draft.web ?? {};
  const providerOrder = normalizeProviderOrder(web.provider_order);
  const ddg = web.duckduckgo ?? {};
  const exa = web.exa ?? {};
  const tavily = web.tavily ?? {};
  const brave = web.brave ?? {};
  const webFetch = draft.web_fetch ?? {};
  const browser = draft.browser ?? {};

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("tools.webSearch")}</CardTitle>
        <CardDescription>{t("tools.webFetch")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Web Search */}
        <div className="grid gap-3">
          <WebSearchProviderSequence
            providerOrder={providerOrder}
            onProviderOrderChange={(nextOrder) =>
              updateNested("web", { provider_order: nextOrder })
            }
            exa={exa}
            tavily={tavily}
            brave={brave}
            duckduckgo={ddg}
            onProviderPatch={(provider, patch) =>
              updateNested("web", {
                [provider]: { ...(web[provider] ?? {}), ...patch },
              })
            }
            onDuckDuckGoPatch={(patch) =>
              updateNested("web", {
                duckduckgo: { ...ddg, ...patch, enabled: true },
              })
            }
          />
        </div>

        <Separator />

        {/* Web Fetch */}
        <div className="grid gap-3">
          <div className="grid gap-1.5 max-w-xs">
            <InfoLabel tip={t("tools.webFetchPolicyTip")}>{t("tools.webFetchPolicy")}</InfoLabel>
            <Select
              value={webFetch.policy ?? "allow_all"}
              onValueChange={(v) => updateNested("web_fetch", { ...webFetch, policy: v })}
            >
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="allow_all">Allow All</SelectItem>
                <SelectItem value="allowlist">Allowlist</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {webFetch.policy === "allowlist" && (
            <div className="grid gap-1.5">
              <Label>{t("tools.allowedDomains")}</Label>
              <Textarea
                value={(webFetch.allowed_domains ?? []).join("\n")}
                onChange={(e: ChangeEvent<HTMLTextAreaElement>) =>
                  updateNested("web_fetch", {
                    ...webFetch,
                    allowed_domains: e.target.value.split("\n").filter(Boolean),
                  })
                }
                className="min-h-[80px] font-mono text-xs"
                placeholder={"github.com\n*.wikipedia.org\ndocs.google.com"}
              />
            </div>
          )}
          <div className="grid gap-1.5">
            <InfoLabel tip={t("tools.blockedDomainsTip")}>{t("tools.blockedDomains")}</InfoLabel>
            <Textarea
              value={(webFetch.blocked_domains ?? []).join("\n")}
              onChange={(e: ChangeEvent<HTMLTextAreaElement>) =>
                updateNested("web_fetch", {
                  ...webFetch,
                  blocked_domains: e.target.value.split("\n").filter(Boolean),
                })
              }
              className="min-h-[80px] font-mono text-xs"
              placeholder={"ifconfig.co\nipinfo.io\n*.whatismyip.com"}
            />
          </div>
        </div>

        <Separator />

        {/* Browser */}
        <div>
          <h4 className="mb-3 text-sm font-medium">{t("tools.browser")}</h4>
          <div className="flex gap-6">
            <div className="flex items-center gap-2">
              <Label>{t("tools.browserEnabled")}</Label>
              <Switch
                checked={browser.enabled !== false}
                onCheckedChange={(v: boolean) => updateNested("browser", { enabled: v })}
              />
            </div>
            <div className="flex items-center gap-2">
              <Label>{t("tools.browserHeadless")}</Label>
              <Switch
                checked={browser.headless !== false}
                onCheckedChange={(v: boolean) => updateNested("browser", { headless: v })}
              />
            </div>
          </div>
        </div>

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={handleSave} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
