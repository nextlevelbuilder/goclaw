/**
 * Provider Setup section — Step 1 of the TTS configuration flow.
 * Renders a button group for selecting the active TTS provider.
 * Selecting a provider reveals the Credentials + Voice/Model sections.
 */
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { getProviderDefinition } from "@/data/tts-providers";

const PROVIDER_VALUES = ["", "openai", "elevenlabs", "edge", "minimax"] as const;

interface Props {
  provider: string;
  onChange: (provider: string) => void;
}

export function ProviderSetup({ provider, onChange }: Props) {
  const { t } = useTranslation("tts");

  const hint = (() => {
    if (!provider) return null;
    if (provider === "edge") return t("edge.hint");
    const def = getProviderDefinition(provider);
    if (def && !def.requiresApiKey) return t("edge.hint");
    return null;
  })();

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">
          1. {t("general.primaryProvider")}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid gap-1.5">
          <Label className="sr-only">{t("general.primaryProvider")}</Label>
          <div className="flex flex-wrap gap-2">
            {PROVIDER_VALUES.map((v) => (
              <Button
                key={v}
                type="button"
                variant={provider === v ? "default" : "outline"}
                size="sm"
                className="h-9 min-w-[44px]"
                onClick={() => onChange(v)}
              >
                {t(`providers.${v || "none"}`)}
              </Button>
            ))}
          </div>
        </div>
        {hint && (
          <p className="text-xs text-muted-foreground">{hint}</p>
        )}
      </CardContent>
    </Card>
  );
}
