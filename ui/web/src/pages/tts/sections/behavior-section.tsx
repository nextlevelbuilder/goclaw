/**
 * Behavior section — collapsible "Advanced settings" panel.
 * Contains: auto mode, reply mode, max_length, timeout_ms.
 * Expanded by default for visibility; users can collapse if needed.
 * Uses local useState + chevron (no Radix Collapsible dependency needed).
 */
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import type { TtsConfig } from "../hooks/use-tts-config";

const AUTO_MODE_VALUES = ["off", "always", "inbound", "tagged"] as const;
const REPLY_MODE_VALUES = ["final", "all"] as const;

interface Props {
  draft: TtsConfig;
  onUpdate: (patch: Partial<TtsConfig>) => void;
}

export function BehaviorSection({ draft, onUpdate }: Props) {
  const { t } = useTranslation("tts");
  const [open, setOpen] = useState(true);

  return (
    <Card className="gap-3">
      <CardHeader>
        <button
          type="button"
          className="flex w-full items-center justify-between py-1 text-left"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
        >
          <span className="text-sm font-medium">
            {t("advanced.title", "Advanced settings")}
          </span>
          <ChevronDown
            className={cn(
              "h-4 w-4 text-muted-foreground transition-transform duration-200",
              open && "rotate-180",
            )}
          />
        </button>
      </CardHeader>

      {open && (
        <CardContent className="space-y-4">
          {/* Auto mode */}
          <div className="grid gap-1.5">
            <Label>{t("general.autoApplyMode")}</Label>
            <div className="flex flex-wrap gap-2">
              {AUTO_MODE_VALUES.map((v) => (
                <Button
                  key={v}
                  type="button"
                  variant={draft.auto === v ? "default" : "outline"}
                  size="sm"
                  className="h-9 min-w-[44px]"
                  onClick={() => onUpdate({ auto: v })}
                  title={t(`autoModes.${v}Desc`)}
                >
                  {t(`autoModes.${v}`)}
                </Button>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">
              {t(`autoModes.${draft.auto}Desc`)}
            </p>
          </div>

          {/* Reply mode */}
          <div className="grid gap-1.5">
            <Label>{t("general.replyMode")}</Label>
            <div className="flex flex-wrap gap-2">
              {REPLY_MODE_VALUES.map((v) => (
                <Button
                  key={v}
                  type="button"
                  variant={draft.mode === v ? "default" : "outline"}
                  size="sm"
                  className="h-9 min-w-[44px]"
                  onClick={() => onUpdate({ mode: v })}
                  title={t(`replyModes.${v}Desc`)}
                >
                  {t(`replyModes.${v}`)}
                </Button>
              ))}
            </div>
          </div>

          {/* Max length & timeout */}
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label htmlFor="tts-maxlen">{t("general.maxTextLength")}</Label>
              <Input
                id="tts-maxlen"
                type="number"
                className="text-base md:text-sm"
                value={draft.max_length}
                onChange={(e) => onUpdate({ max_length: Number(e.target.value) })}
                min={10}
              />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="tts-timeout">{t("general.timeout")}</Label>
              <Input
                id="tts-timeout"
                type="number"
                className="text-base md:text-sm"
                value={draft.timeout_ms}
                onChange={(e) => onUpdate({ timeout_ms: Number(e.target.value) })}
                min={1000}
              />
            </div>
          </div>
        </CardContent>
      )}
    </Card>
  );
}
