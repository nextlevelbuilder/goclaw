import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2 } from "lucide-react";
import {
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const DEFAULT_TIMEOUT_SECONDS = 60;
const MIN_TIMEOUT_SECONDS = 1;
const MAX_TIMEOUT_SECONDS = 3600;

interface Props {
  initialSettings: Record<string, unknown>;
  onSave: (settings: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}

function resolveTimeoutSeconds(settings: Record<string, unknown>): number {
  const value = settings.timeout_seconds;
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return DEFAULT_TIMEOUT_SECONDS;
  }
  if (value < MIN_TIMEOUT_SECONDS) {
    return DEFAULT_TIMEOUT_SECONDS;
  }
  return Math.min(Math.trunc(value), MAX_TIMEOUT_SECONDS);
}

export function ExecSettingsForm({ initialSettings, onSave, onCancel }: Props) {
  const { t } = useTranslation("tools");
  const [timeoutSeconds, setTimeoutSeconds] = useState(() => resolveTimeoutSeconds(initialSettings));
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setTimeoutSeconds(resolveTimeoutSeconds(initialSettings));
  }, [initialSettings]);

  const handleSave = async () => {
    const nextTimeout = Math.min(
      Math.max(Math.trunc(timeoutSeconds), MIN_TIMEOUT_SECONDS),
      MAX_TIMEOUT_SECONDS,
    );
    setSaving(true);
    try {
      await onSave({ timeout_seconds: nextTimeout });
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("builtin.execSettings.title")}</DialogTitle>
        <DialogDescription>{t("builtin.execSettings.description")}</DialogDescription>
      </DialogHeader>

      <div className="space-y-4 py-2">
        <div className="grid gap-1.5">
          <Label htmlFor="exec-timeout-seconds" className="text-sm">
            {t("builtin.execSettings.timeoutSeconds")}
          </Label>
          <Input
            id="exec-timeout-seconds"
            type="number"
            min={MIN_TIMEOUT_SECONDS}
            max={MAX_TIMEOUT_SECONDS}
            step={1}
            value={timeoutSeconds}
            onChange={(e) => setTimeoutSeconds(Number(e.target.value) || DEFAULT_TIMEOUT_SECONDS)}
            className="max-w-[140px] text-base md:text-sm"
          />
          <p className="text-xs text-muted-foreground">
            {t("builtin.execSettings.timeoutHint", {
              min: MIN_TIMEOUT_SECONDS,
              max: MAX_TIMEOUT_SECONDS,
            })}
          </p>
        </div>

        <p className="text-xs text-muted-foreground">
          {t("builtin.execSettings.sandboxHint")}
        </p>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>
          {t("builtin.execSettings.cancel")}
        </Button>
        <Button onClick={handleSave} disabled={saving}>
          {saving && <Loader2 className="h-4 w-4 animate-spin" />}
          {saving ? t("builtin.execSettings.saving") : t("builtin.execSettings.save")}
        </Button>
      </DialogFooter>
    </>
  );
}
