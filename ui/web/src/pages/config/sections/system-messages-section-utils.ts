export interface SystemMessageDefinition {
  key: string;
  template: string;
  description?: string;
  labels?: Record<string, string>;
  descriptions?: Record<string, string>;
  variables?: string[];
}

export interface SystemMessagesData {
  default_locale?: string;
  messages?: Record<string, Record<string, string>>;
}

export type SystemMessagesDraft = Record<string, Record<string, string>>;

export const SYSTEM_MESSAGE_LOCALES = [
  { code: "en", labelKey: "systemMessages.locale.en" },
  { code: "vi", labelKey: "systemMessages.locale.vi" },
  { code: "zh", labelKey: "systemMessages.locale.zh" },
  { code: "ko", labelKey: "systemMessages.locale.ko" },
] as const;

export function systemMessageDefinitionsFromSchema(schema: Record<string, any> | null | undefined): SystemMessageDefinition[] {
  const definitions = schema?.properties?.system_messages?.definitions;
  if (!Array.isArray(definitions)) return [];
  return definitions
    .filter((definition) => typeof definition?.key === "string" && typeof definition?.template === "string")
    .map((definition) => ({
      key: definition.key,
      template: definition.template,
      description: typeof definition.description === "string" ? definition.description : undefined,
      labels: sanitizeLocalizedTextMap(definition.labels),
      descriptions: sanitizeLocalizedTextMap(definition.descriptions),
      variables: Array.isArray(definition.variables)
        ? definition.variables.filter((variable: unknown): variable is string => typeof variable === "string")
        : [],
    }));
}

function sanitizeLocalizedTextMap(value: unknown): Record<string, string> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const out: Record<string, string> = {};
  for (const [key, text] of Object.entries(value as Record<string, unknown>)) {
    if (typeof text === "string" && text.trim() !== "") {
      out[key.toLowerCase()] = text;
    }
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function normalizeLocale(locale: string | undefined): string {
  return (locale || "en").toLowerCase().split("-")[0] || "en";
}

function localizedText(values: Record<string, string> | undefined, locale: string | undefined): string {
  if (!values) return "";
  const normalized = normalizeLocale(locale);
  return values[normalized] || values.en || "";
}

export function localizedSystemMessageLabel(definition: SystemMessageDefinition, locale: string | undefined): string {
  return localizedText(definition.labels, locale) || definition.key;
}

export function localizedSystemMessageDescription(definition: SystemMessageDefinition, locale: string | undefined): string {
  return localizedText(definition.descriptions, locale) || definition.description || "";
}

export function normalizeSystemMessagesDraft(
  data: SystemMessagesData | undefined,
  definitions: SystemMessageDefinition[],
): SystemMessagesDraft {
  const messages = data?.messages ?? {};
  const out: SystemMessagesDraft = {};
  for (const definition of definitions) {
    const byLocale = messages[definition.key] ?? {};
    const entry: Record<string, string> = {};
    for (const locale of SYSTEM_MESSAGE_LOCALES) {
      entry[locale.code] = byLocale[locale.code] ?? "";
    }
    out[definition.key] = entry;
  }
  return out;
}

export function buildSystemMessagesPatch(
  draft: SystemMessagesDraft,
  current?: SystemMessagesData,
  defaultLocale?: string,
): SystemMessagesData {
  const messages: Record<string, Record<string, string>> = {};
  for (const [key, byLocale] of Object.entries(draft)) {
    const next: Record<string, string> = {};
    let hasValue = false;
    for (const locale of SYSTEM_MESSAGE_LOCALES) {
      const value = byLocale[locale.code] ?? "";
      if (value.trim() !== "") {
        hasValue = true;
      }
      next[locale.code] = value;
    }
    if (hasValue || Object.prototype.hasOwnProperty.call(current?.messages ?? {}, key)) {
      messages[key] = next;
    }
  }
  const patch: SystemMessagesData = { messages };
  if (defaultLocale !== undefined) {
    patch.default_locale = defaultLocale;
  } else if (current?.default_locale) {
    patch.default_locale = current.default_locale;
  }
  return patch;
}
