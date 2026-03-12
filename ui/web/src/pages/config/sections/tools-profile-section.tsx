import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Card,
  CardContent,
  CardDescription,
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
import { ToolNameSelect } from "@/components/shared/tool-name-select";

/* eslint-disable @typescript-eslint/no-explicit-any */
type ToolsData = Record<string, any>;

interface Props {
  data: ToolsData | undefined;
  onSave: (value: ToolsData) => Promise<void>;
  saving: boolean;
}

export function ToolsProfileSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<ToolsData>(data ?? {});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? {});
    setDirty(false);
  }, [data]);

  const update = (patch: Partial<ToolsData>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  if (!data) return null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("tools.title")}</CardTitle>
        <CardDescription>{t("tools.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("tools.profileTip")}>{t("tools.profile")}</InfoLabel>
            <Select value={draft.profile ?? ""} onValueChange={(v) => update({ profile: v })}>
              <SelectTrigger>
                <SelectValue placeholder={t("tools.profilePlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="minimal">Minimal</SelectItem>
                <SelectItem value="coding">Coding</SelectItem>
                <SelectItem value="messaging">Messaging</SelectItem>
                <SelectItem value="full">Full</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("tools.rateLimitPerHourTip")}>{t("tools.rateLimitPerHour")}</InfoLabel>
            <Input
              type="number"
              value={draft.rate_limit_per_hour ?? ""}
              onChange={(e) => update({ rate_limit_per_hour: Number(e.target.value) })}
              placeholder="0 = disabled"
              min={0}
            />
          </div>
        </div>

        <div className="space-y-4">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("tools.allowTip")}>{t("tools.allow")}</InfoLabel>
            <ToolNameSelect
              value={draft.allow ?? []}
              onChange={(v) => update({ allow: v })}
              placeholder={t("tools.allowPlaceholder")}
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("tools.denyTip")}>{t("tools.deny")}</InfoLabel>
            <ToolNameSelect
              value={draft.deny ?? []}
              onChange={(v) => update({ deny: v })}
              placeholder={t("tools.denyPlaceholder")}
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("tools.alsoAllowTip")}>{t("tools.alsoAllow")}</InfoLabel>
            <ToolNameSelect
              value={draft.alsoAllow ?? []}
              onChange={(v) => update({ alsoAllow: v })}
              placeholder={t("tools.alsoAllowPlaceholder")}
            />
          </div>
        </div>

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
