import { X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { isSuspiciousPlaintextEnv } from "./cli-credential-env-vars-section";
import type { GrantEnvEntry } from "./cli-credential-grant-env-section";

interface Props {
  entry: GrantEnvEntry;
  hasError: boolean;
  onRemove: () => void;
  onUpdate: (field: "key" | "value" | "kind", value: string) => void;
}

export function CliCredentialGrantEnvRow({ entry, hasError, onRemove, onUpdate }: Props) {
  const { t } = useTranslation("cli-credentials");

  return (
    <div className="flex flex-wrap items-start gap-2">
      <div className="min-w-[11rem] flex-1">
        <Input
          placeholder={t("grants.envVars.keyPlaceholder")}
          value={entry.key}
          onChange={(e) => onUpdate("key", e.target.value)}
          className={`text-base md:text-sm font-mono${hasError ? " border-destructive" : ""}`}
        />
        {hasError && (
          <p className="mt-0.5 text-xs text-destructive">
            {t("grants.envVars.deniedKey", { key: entry.key })}
          </p>
        )}
      </div>
      <div className="min-w-[11rem] flex-1">
        {entry.masked ? (
          <Input
            disabled
            value={t("grants.envVars.revealHidden")}
            className="text-base md:text-sm text-muted-foreground italic"
          />
        ) : (
          <Input
            type="password"
            autoComplete="off"
            placeholder={t("grants.envVars.valuePlaceholder")}
            value={entry.value}
            onChange={(e) => onUpdate("value", e.target.value)}
            className="text-base md:text-sm"
          />
        )}
      </div>
      {!entry.masked && (
        <select
          value={entry.kind}
          onChange={(e) => onUpdate("kind", e.target.value)}
          className="h-9 rounded-md border bg-background px-2 text-base md:text-sm"
        >
          <option value="sensitive">{t("form.envKindSensitive")}</option>
          <option value="value">{t("form.envKindValue")}</option>
        </select>
      )}
      <Button type="button" variant="ghost" size="icon" className="mt-0.5 h-8 w-8 shrink-0" onClick={onRemove}>
        <X className="h-4 w-4" />
      </Button>
      {!entry.masked && entry.kind === "value" && (
        <p className="basis-full text-xs text-amber-700 dark:text-amber-300">
          {isSuspiciousPlaintextEnv(entry.key, entry.value)
            ? t("form.envValueSuspicious")
            : t("form.envValueWarning")}
        </p>
      )}
    </div>
  );
}
