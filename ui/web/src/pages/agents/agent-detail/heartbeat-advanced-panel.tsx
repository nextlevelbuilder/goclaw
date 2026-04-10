import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";

interface HeartbeatAdvancedPanelProps {
  ackMaxChars: number;
  setAckMaxChars: (v: number) => void;
  maxRetries: number;
  setMaxRetries: (v: number) => void;
  isolatedSession: boolean;
  setIsolatedSession: (v: boolean) => void;
  lightContext: boolean;
  setLightContext: (v: boolean) => void;
}

/** Collapsible advanced settings panel for the heartbeat config dialog. */
export function HeartbeatAdvancedPanel({
  ackMaxChars, setAckMaxChars,
  maxRetries, setMaxRetries,
  isolatedSession, setIsolatedSession,
  lightContext, setLightContext,
}: HeartbeatAdvancedPanelProps) {
  const { t } = useTranslation("agents");
  const [showAdvanced, setShowAdvanced] = useState(false);

  return (
    <div className="rounded-lg border">
      <button
        type="button"
        onClick={() => setShowAdvanced(!showAdvanced)}
        className="flex w-full items-center justify-between px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
      >
        <span>{t("heartbeat.advancedSettings")}</span>
        <ChevronDown className={`h-3.5 w-3.5 transition-transform ${showAdvanced ? "rotate-180" : ""}`} />
      </button>
      {showAdvanced && (
        <div className="border-t px-3 py-3 space-y-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="space-y-1">
              <Label htmlFor="hb-ack" className="text-xs">{t("heartbeat.ackMaxChars")}</Label>
              <Input
                id="hb-ack"
                type="number"
                min={0}
                value={ackMaxChars}
                onChange={(e) => setAckMaxChars(Number(e.target.value))}
                className="text-base md:text-sm"
              />
              <p className="text-xs-plus text-muted-foreground">{t("heartbeat.ackMaxCharsHint")}</p>
            </div>
            <div className="space-y-1">
              <Label htmlFor="hb-retries" className="text-xs">{t("heartbeat.maxRetries")}</Label>
              <Input
                id="hb-retries"
                type="number"
                min={0}
                max={10}
                value={maxRetries}
                onChange={(e) => setMaxRetries(Number(e.target.value))}
                className="text-base md:text-sm"
              />
              <p className="text-xs-plus text-muted-foreground">{t("heartbeat.maxRetriesHint")}</p>
            </div>
          </div>
          <div className="flex items-center justify-between gap-4">
            <div>
              <span className="text-xs font-medium">{t("heartbeat.isolatedSession")}</span>
              <p className="text-xs-plus text-muted-foreground">{t("heartbeat.isolatedSessionHint")}</p>
            </div>
            <Switch checked={isolatedSession} onCheckedChange={setIsolatedSession} />
          </div>
          <div className="flex items-center justify-between gap-4">
            <div>
              <span className="text-xs font-medium">{t("heartbeat.lightContext")}</span>
              <p className="text-xs-plus text-muted-foreground">{t("heartbeat.lightContextHint")}</p>
            </div>
            <Switch checked={lightContext} onCheckedChange={setLightContext} />
          </div>
        </div>
      )}
    </div>
  );
}
