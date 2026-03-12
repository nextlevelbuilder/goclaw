import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { InfoLabel } from "@/components/shared/info-label";

/* eslint-disable @typescript-eslint/no-explicit-any */
type ToolsData = Record<string, any>;

interface Props {
  data: ToolsData | undefined;
  onSave: (value: ToolsData) => Promise<void>;
  saving: boolean;
}

export function ToolsExecSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<ToolsData>(data ?? {});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? {});
    setDirty(false);
  }, [data]);

  const updateNested = (section: string, patch: Record<string, any>) => {
    setDraft((prev) => ({
      ...prev,
      [section]: { ...(prev[section] ?? {}), ...patch },
    }));
    setDirty(true);
  };

  if (!data) return null;

  const exec = draft.execApproval ?? {};

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("tools.execApproval")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("tools.execSecurityTip")}>{t("tools.execSecurity")}</InfoLabel>
            <Select value={exec.security ?? "full"} onValueChange={(v) => updateNested("execApproval", { security: v })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="deny">Deny All</SelectItem>
                <SelectItem value="allowlist">Allowlist</SelectItem>
                <SelectItem value="full">Full (Allow All)</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("tools.execAskModeTip")}>{t("tools.execAskMode")}</InfoLabel>
            <Select value={exec.ask ?? "off"} onValueChange={(v) => updateNested("execApproval", { ask: v })}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="off">Off</SelectItem>
                <SelectItem value="on-miss">On Miss</SelectItem>
                <SelectItem value="always">Always</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        {exec.security === "allowlist" && (
          <div className="grid gap-1.5">
            <Label>{t("tools.execAllowlistLabel")}</Label>
            <Textarea
              value={(exec.allowlist ?? []).join("\n")}
              onChange={(e) =>
                updateNested("execApproval", {
                  allowlist: e.target.value.split("\n").filter(Boolean),
                })
              }
              className="min-h-[80px] font-mono text-xs"
              placeholder="git *&#10;npm *&#10;ls *"
            />
          </div>
        )}

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={() => onSave(draft)} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
