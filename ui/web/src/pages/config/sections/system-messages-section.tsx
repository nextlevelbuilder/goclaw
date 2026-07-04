import { useEffect, useMemo, useState } from "react";
import { MessageSquareText, RotateCcw, Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { InfoLabel } from "@/components/shared/info-label";
import {
  SYSTEM_MESSAGE_LOCALES,
  type SystemMessagesData,
  type SystemMessagesDraft,
  buildSystemMessagesPatch,
  localizedSystemMessageDescription,
  localizedSystemMessageLabel,
  normalizeSystemMessagesDraft,
  systemMessageDefinitionsFromSchema,
} from "./system-messages-section-utils";

interface Props {
  data: SystemMessagesData | undefined;
  schema: Record<string, any> | null;
  onSave: (value: SystemMessagesData) => Promise<void>;
  saving: boolean;
}

export function SystemMessagesSection({ data, schema, onSave, saving }: Props) {
  const { t, i18n } = useTranslation("config");
  const definitions = useMemo(() => systemMessageDefinitionsFromSchema(schema), [schema]);
  const [draft, setDraft] = useState<SystemMessagesDraft>(() => normalizeSystemMessagesDraft(data, definitions));
  const [selectedKey, setSelectedKey] = useState("");
  const [selectedLocale, setSelectedLocale] = useState<(typeof SYSTEM_MESSAGE_LOCALES)[number]["code"]>("vi");
  const [defaultLocale, setDefaultLocale] = useState<(typeof SYSTEM_MESSAGE_LOCALES)[number]["code"]>(
    (data?.default_locale as (typeof SYSTEM_MESSAGE_LOCALES)[number]["code"]) || "en",
  );
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(normalizeSystemMessagesDraft(data, definitions));
    setDefaultLocale((data?.default_locale as typeof defaultLocale) || "en");
    setSelectedKey((prev) => (prev && definitions.some((definition) => definition.key === prev) ? prev : definitions[0]?.key ?? ""));
    setDirty(false);
  }, [data, definitions]);

  const activeKey = selectedKey || definitions[0]?.key || "";
  const selectedDefinition = definitions.find((definition) => definition.key === activeKey);
  const currentValue = activeKey ? draft[activeKey]?.[selectedLocale] ?? "" : "";
  const uiLocale = i18n.resolvedLanguage || i18n.language || selectedLocale;

  const updateTemplate = (value: string) => {
    if (!activeKey) return;
    setDraft((prev) => ({
      ...prev,
      [activeKey]: {
        ...(prev[activeKey] ?? {}),
        [selectedLocale]: value,
      },
    }));
    setDirty(true);
  };

  const restoreDefault = () => updateTemplate("");

  if (definitions.length === 0) {
    return null;
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          <MessageSquareText className="h-4 w-4" />
          {t("systemMessages.title")}
        </CardTitle>
        <CardDescription>{t("systemMessages.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-[minmax(0,1fr)_12rem_12rem]">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("systemMessages.messageTip")}>{t("systemMessages.message")}</InfoLabel>
            <Select value={activeKey} onValueChange={setSelectedKey}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {definitions.map((definition) => (
                  <SelectItem
                    key={definition.key}
                    value={definition.key}
                    textValue={localizedSystemMessageLabel(definition, uiLocale)}
                  >
                    <span className="flex flex-col gap-0.5">
                      <span>{localizedSystemMessageLabel(definition, uiLocale)}</span>
                      <span className="font-mono text-xs text-muted-foreground">{definition.key}</span>
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("systemMessages.localeTip")}>{t("systemMessages.locale")}</InfoLabel>
            <Select value={selectedLocale} onValueChange={(value) => setSelectedLocale(value as typeof selectedLocale)}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SYSTEM_MESSAGE_LOCALES.map((locale) => (
                  <SelectItem key={locale.code} value={locale.code}>
                    {t(locale.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("systemMessages.defaultLocaleTip")}>{t("systemMessages.defaultLocale")}</InfoLabel>
            <Select
              value={defaultLocale}
              onValueChange={(value) => {
                setDefaultLocale(value as typeof defaultLocale);
                setDirty(true);
              }}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SYSTEM_MESSAGE_LOCALES.map((locale) => (
                  <SelectItem key={locale.code} value={locale.code}>
                    {t(locale.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        {selectedDefinition && (
          <div className="space-y-4">
            {localizedSystemMessageDescription(selectedDefinition, uiLocale) && (
              <p className="text-sm text-muted-foreground">{localizedSystemMessageDescription(selectedDefinition, uiLocale)}</p>
            )}
            {selectedDefinition.variables && selectedDefinition.variables.length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {selectedDefinition.variables.map((variable) => (
                  <Badge key={variable} variant="outline" className="font-mono text-xs">
                    {"{{" + variable + "}}"}
                  </Badge>
                ))}
              </div>
            )}

            <div className="grid gap-1.5">
              <InfoLabel tip={t("systemMessages.overrideTip")}>{t("systemMessages.override")}</InfoLabel>
              <Textarea
                value={currentValue}
                onChange={(event) => updateTemplate(event.target.value)}
                className="min-h-[150px] font-mono text-base md:text-sm"
                spellCheck={false}
                placeholder={selectedDefinition.template}
              />
            </div>

            <div className="grid gap-1.5">
              <InfoLabel tip={t("systemMessages.defaultTemplateTip")}>{t("systemMessages.defaultTemplate")}</InfoLabel>
              <pre className="max-h-48 overflow-auto rounded-md border bg-muted/30 p-3 text-xs whitespace-pre-wrap text-muted-foreground">
                {selectedDefinition.template}
              </pre>
            </div>
          </div>
        )}

        {dirty && (
          <div className="flex flex-wrap justify-end gap-2 pt-2">
            <Button type="button" variant="outline" size="sm" onClick={restoreDefault} className="gap-1.5">
              <RotateCcw className="h-3.5 w-3.5" /> {t("systemMessages.useDefault")}
            </Button>
            <Button size="sm" onClick={() => onSave(buildSystemMessagesPatch(draft, data, defaultLocale))} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
