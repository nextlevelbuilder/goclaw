import { useState, useEffect } from "react";
import { Save, ChevronDown, ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { isSecret } from "@/lib/secret";

type ProviderEntry = {
  api_key?: string;
  api_base?: string;
};

type ProvidersData = Record<string, ProviderEntry>;

const KNOWN_PROVIDERS = [
  { key: "anthropic", label: "Anthropic", envKey: "GOCLAW_ANTHROPIC_API_KEY" },
  { key: "openai", label: "OpenAI", envKey: "GOCLAW_OPENAI_API_KEY" },
  { key: "openrouter", label: "OpenRouter", envKey: "GOCLAW_OPENROUTER_API_KEY" },
  { key: "groq", label: "Groq", envKey: "GOCLAW_GROQ_API_KEY" },
  { key: "gemini", label: "Gemini", envKey: "GOCLAW_GEMINI_API_KEY" },
  { key: "deepseek", label: "DeepSeek", envKey: "GOCLAW_DEEPSEEK_API_KEY" },
  { key: "mistral", label: "Mistral", envKey: "GOCLAW_MISTRAL_API_KEY" },
  { key: "xai", label: "xAI", envKey: "GOCLAW_XAI_API_KEY" },
  { key: "minimax", label: "MiniMax", envKey: "GOCLAW_MINIMAX_API_KEY" },
  { key: "cohere", label: "Cohere", envKey: "GOCLAW_COHERE_API_KEY" },
  { key: "perplexity", label: "Perplexity", envKey: "GOCLAW_PERPLEXITY_API_KEY" },
  { key: "ollama_cloud", label: "Ollama Cloud", envKey: "GOCLAW_OLLAMA_CLOUD_API_KEY" },
];

interface Props {
  data: ProvidersData | undefined;
  onSave: (value: ProvidersData) => Promise<void>;
  saving: boolean;
}

export function ProvidersSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<ProvidersData>(data ?? {});
  const [dirty, setDirty] = useState(false);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  useEffect(() => {
    setDraft(data ?? {});
    setDirty(false);
  }, [data]);

  const updateProvider = (key: string, patch: Partial<ProviderEntry>) => {
    setDraft((prev) => ({
      ...prev,
      [key]: { ...prev[key], ...patch },
    }));
    setDirty(true);
  };

  const toggle = (key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const handleSave = () => {
    // Strip masked secrets from the payload
    const toSave: ProvidersData = {};
    for (const [key, entry] of Object.entries(draft)) {
      const clean: ProviderEntry = {};
      if (entry.api_base !== undefined) clean.api_base = entry.api_base;
      if (entry.api_key !== undefined && !isSecret(entry.api_key)) {
        clean.api_key = entry.api_key;
      }
      toSave[key] = clean;
    }
    onSave(toSave);
  };

  if (!data) return null;

  // Only show providers that exist in config
  const activeProviders = KNOWN_PROVIDERS.filter((p) => data[p.key] != null);

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("providers.title")}</CardTitle>
        <CardDescription>{t("providers.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        {activeProviders.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("providers.noProviders")}</p>
        ) : (
          activeProviders.map((p) => {
            const entry = draft[p.key] ?? {};
            const isOpen = expanded.has(p.key);
            return (
              <div key={p.key} className="rounded-md border">
                <button
                  type="button"
                  className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-sm hover:bg-muted/50"
                  onClick={() => toggle(p.key)}
                >
                  {isOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                  <span className="font-medium">{p.label}</span>
                  {entry.api_base && (
                    <span className="ml-auto text-xs text-muted-foreground">{entry.api_base}</span>
                  )}
                </button>
                {isOpen && (
                  <div className="space-y-3 border-t px-3 py-3">
                    <div className="grid gap-1.5">
                      <Label>{t("providers.apiKey")}</Label>
                      <Input
                        type="password"
                        value={entry.api_key ?? ""}
                        disabled={isSecret(entry.api_key)}
                        readOnly={isSecret(entry.api_key)}
                        onChange={(e) => updateProvider(p.key, { api_key: e.target.value })}
                      />
                      {isSecret(entry.api_key) && (
                        <p className="text-xs text-muted-foreground">{t("providers.managedVia", { envKey: p.envKey })}</p>
                      )}
                    </div>
                    <div className="grid gap-1.5">
                      <Label>{t("providers.apiBaseUrl")}</Label>
                      <Input
                        value={entry.api_base ?? ""}
                        onChange={(e) => updateProvider(p.key, { api_base: e.target.value })}
                        placeholder={t("providers.apiBaseUrlPlaceholder")}
                      />
                    </div>
                  </div>
                )}
              </div>
            );
          })
        )}

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
