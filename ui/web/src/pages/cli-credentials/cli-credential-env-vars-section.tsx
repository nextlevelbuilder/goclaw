import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import type { CLIPreset } from "./hooks/use-cli-credentials";
import type { CLIEnvEntryKind } from "@/types/cli-credential";

export interface ManualEnvEntry {
  key: string;
  value: string;
  kind: CLIEnvEntryKind;
}

interface CliCredentialEnvVarsSectionProps {
  isManualMode: boolean;
  activePreset: CLIPreset | null;
  envValues: Record<string, string>;
  setEnvValues: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
  manualEnvEntries: ManualEnvEntry[];
  setManualEnvEntries: (updater: (prev: ManualEnvEntry[]) => ManualEnvEntry[]) => void;
}

const SUSPICIOUS_VALUE_RE = /(api[_-]?key|token|secret|password|credential|bearer\s+[a-z0-9._-]+|sk-[a-z0-9_-]{12,}|gh[pousr]_[a-z0-9_]{20,})/i;
export const isSuspiciousPlaintextEnv = (key: string, value: string) =>
  SUSPICIOUS_VALUE_RE.test(`${key}=${value}`);

/** Env var inputs: preset-driven fields or free-form key/value pairs in manual mode. */
export function CliCredentialEnvVarsSection({
  isManualMode,
  activePreset,
  envValues,
  setEnvValues,
  manualEnvEntries,
  setManualEnvEntries,
}: CliCredentialEnvVarsSectionProps) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");

  const addEntry = useCallback(() => {
    setManualEnvEntries((prev) => [...prev, { key: "", value: "", kind: "sensitive" }]);
  }, [setManualEnvEntries]);

  const removeEntry = useCallback((index: number) => {
    setManualEnvEntries((prev) => prev.filter((_, i) => i !== index));
  }, [setManualEnvEntries]);

  const updateEntry = useCallback((index: number, field: "key" | "value" | "kind", val: string) => {
    setManualEnvEntries((prev) =>
      prev.map((entry, i) => (i === index ? { ...entry, [field]: val } : entry)),
    );
  }, [setManualEnvEntries]);

  // Preset-driven env var inputs
  if (!isManualMode && activePreset && activePreset.env_vars.length > 0) {
    return (
      <div className="grid gap-3 rounded-md border p-3">
        <p className="text-sm font-medium">{t("form.envVars")}</p>
        {activePreset.env_vars.map((ev) => (
          <div key={ev.name} className="grid gap-1.5">
            <Label htmlFor={`env-${ev.name}`}>
              {ev.name}
              {ev.optional && <span className="ml-1 text-xs text-muted-foreground">({tc("optional")})</span>}
            </Label>
            <Input
              id={`env-${ev.name}`}
              type="password"
              autoComplete="off"
              placeholder={ev.desc}
              value={envValues[ev.name] ?? ""}
              onChange={(e) => setEnvValues((prev) => ({ ...prev, [ev.name]: e.target.value }))}
              className="text-base md:text-sm"
            />
            {ev.desc && <p className="text-xs text-muted-foreground">{ev.desc}</p>}
          </div>
        ))}
      </div>
    );
  }

  // Manual free-form key/value entries
  if (isManualMode) {
    return (
      <div className="grid gap-3 rounded-md border p-3">
        <div className="flex items-center justify-between">
          <p className="text-sm font-medium">{t("form.envVars")}</p>
          <Button type="button" variant="outline" size="sm" onClick={addEntry}>
            <Plus className="mr-1 h-3.5 w-3.5" />
            {t("form.addEnvVar")}
          </Button>
        </div>
        {manualEnvEntries.length === 0 && (
          <p className="text-xs text-muted-foreground">{t("form.noEnvVarsHint")}</p>
        )}
        {manualEnvEntries.map((entry, idx) => (
          <div key={idx} className="grid gap-2 rounded-md border p-2">
            <div className="flex items-start gap-2">
              <div className="grid flex-1 gap-1.5">
                <Input
                  placeholder={t("form.envKeyPlaceholder")}
                  value={entry.key}
                  onChange={(e) => updateEntry(idx, "key", e.target.value)}
                  className="text-base md:text-sm font-mono"
                />
              </div>
              <div className="grid flex-1 gap-1.5">
                <Input
                  type={entry.kind === "sensitive" ? "password" : "text"}
                  autoComplete="off"
                  placeholder={t("form.envValuePlaceholder")}
                  value={entry.value}
                  onChange={(e) => updateEntry(idx, "value", e.target.value)}
                  className="text-base md:text-sm"
                />
              </div>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="mt-0.5 h-8 w-8 shrink-0"
                onClick={() => removeEntry(idx)}
              >
                <X className="h-4 w-4" />
              </Button>
            </div>
            <RadioGroup
              value={entry.kind}
              onValueChange={(v) => updateEntry(idx, "kind", v)}
              className="flex items-center gap-4"
            >
              <label className="flex items-center gap-1.5 text-xs">
                <RadioGroupItem value="sensitive" />
                {t("form.envKindSensitive")}
              </label>
              <label className="flex items-center gap-1.5 text-xs">
                <RadioGroupItem value="value" />
                {t("form.envKindValue")}
              </label>
            </RadioGroup>
            {entry.kind === "value" && (
              <p className="text-xs text-amber-700 dark:text-amber-300">
                {isSuspiciousPlaintextEnv(entry.key, entry.value)
                  ? t("form.envValueSuspicious")
                  : t("form.envValueWarning")}
              </p>
            )}
          </div>
        ))}
      </div>
    );
  }

  return null;
}
