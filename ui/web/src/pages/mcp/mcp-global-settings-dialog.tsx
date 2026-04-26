import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Settings2, Loader2, Save, Info } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  useMCPGlobalSettings,
  type MCPGlobalHealthSettings,
} from "./hooks/use-mcp-global-settings";

const FIELDS: { key: keyof MCPGlobalHealthSettings; labelKey: string; hintKey: string; placeholder: string; min: number }[] = [
  { key: "healthFailThreshold", labelKey: "mcp.healthFailThreshold", hintKey: "mcp.healthFailThresholdHint", placeholder: "3", min: 1 },
  { key: "healthCheckInterval", labelKey: "mcp.healthCheckInterval", hintKey: "mcp.healthCheckIntervalHint", placeholder: "30", min: 5 },
  { key: "maxReconnectAttempts", labelKey: "mcp.maxReconnectAttempts", hintKey: "mcp.maxReconnectAttemptsHint", placeholder: "10", min: 1 },
  { key: "reconnectCooldown", labelKey: "mcp.reconnectCooldown", hintKey: "mcp.reconnectCooldownHint", placeholder: "300", min: 10 },
  { key: "idleTimeout", labelKey: "mcp.idleTimeout", hintKey: "mcp.idleTimeoutHint", placeholder: "0", min: 0 },
];

interface MCPGlobalSettingsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function MCPGlobalSettingsDialog({ open, onOpenChange }: MCPGlobalSettingsDialogProps) {
  const { t } = useTranslation("system-settings");
  const { settings, isLoading, save } = useMCPGlobalSettings();
  const [saving, setSaving] = useState(false);
  const [values, setValues] = useState<MCPGlobalHealthSettings>({
    healthFailThreshold: "3",
    healthCheckInterval: "30",
    maxReconnectAttempts: "10",
    reconnectCooldown: "300",
    idleTimeout: "0",
  });
  const [init, setInit] = useState<MCPGlobalHealthSettings>(values);

  useEffect(() => {
    if (open && settings) {
      setValues(settings);
      setInit(settings);
    }
  }, [open, settings]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await save(values, init);
      onOpenChange(false);
    } catch {
      // toast handled by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !saving && onOpenChange(v)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings2 className="h-4 w-4" />
            {t("mcp.title")}
          </DialogTitle>
        </DialogHeader>

        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-3">
            {FIELDS.map((f) => (
              <div key={f.key} className="space-y-1">
                <Label className="text-xs">{t(f.labelKey)}</Label>
                <Input
                  type="number"
                  min={f.min}
                  placeholder={f.placeholder}
                  value={values[f.key]}
                  onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))}
                  className="text-base md:text-sm"
                />
                <p className="text-[10px] text-muted-foreground">{t(f.hintKey)}</p>
              </div>
            ))}
          </div>
        )}

        <div className="flex items-start gap-2 rounded-md border border-teal-200 bg-teal-50 px-3 py-2 text-xs text-teal-700 dark:border-teal-800 dark:bg-teal-950/30 dark:text-teal-300">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{t("mcp.info")}</span>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("cancel")}
          </Button>
          <Button onClick={handleSave} disabled={saving || isLoading}>
            {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
            {saving ? t("saving") : t("save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
