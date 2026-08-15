import { Upload } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";

interface SystemSettingsSkillsCardProps {
  uploadMaxSize: string;
  setUploadMaxSize: (value: string) => void;
  slashEnabled: boolean;
  setSlashEnabled: (value: boolean) => void;
  slashSuggest: boolean;
  setSlashSuggest: (value: boolean) => void;
  slashPartial: boolean;
  setSlashPartial: (value: boolean) => void;
  slashPrefix: string;
  setSlashPrefix: (value: string) => void;
}

export function SystemSettingsSkillsCard({
  uploadMaxSize,
  setUploadMaxSize,
  slashEnabled,
  setSlashEnabled,
  slashSuggest,
  setSlashSuggest,
  slashPartial,
  setSlashPartial,
  slashPrefix,
  setSlashPrefix,
}: SystemSettingsSkillsCardProps) {
  const { t } = useTranslation("system-settings");

  return (
    <Card className="border-sky-200 dark:border-sky-800">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Upload className="h-4 w-4 text-sky-500" />
          {t("skills.title")}
        </CardTitle>
        <CardDescription>{t("skills.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 pt-0">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-0.5">
            <Label htmlFor="skillUploadMaxSize" className="text-sm font-medium">
              {t("skills.maxUploadSize")}
            </Label>
            <p className="text-xs text-muted-foreground">{t("skills.maxUploadSizeHint")}</p>
          </div>
          <Input
            id="skillUploadMaxSize"
            type="number"
            min={1}
            max={500}
            value={uploadMaxSize}
            onChange={(e) => setUploadMaxSize(e.target.value)}
            className="w-24 shrink-0 text-base md:text-sm"
          />
        </div>
        <div className="space-y-3 border-t pt-4">
          <SkillSwitchRow
            id="skillSlashEnabled"
            label={t("skills.slashEnabled")}
            hint={t("skills.slashEnabledHint")}
            checked={slashEnabled}
            onCheckedChange={setSlashEnabled}
          />
          <SkillSwitchRow
            id="skillSlashSuggest"
            label={t("skills.slashSuggest")}
            hint={t("skills.slashSuggestHint")}
            checked={slashSuggest}
            onCheckedChange={setSlashSuggest}
          />
          <SkillSwitchRow
            id="skillSlashPartial"
            label={t("skills.slashPartial")}
            hint={t("skills.slashPartialHint")}
            checked={slashPartial}
            onCheckedChange={setSlashPartial}
          />
          <div className="flex items-start justify-between gap-4">
            <div className="space-y-0.5">
              <Label htmlFor="skillSlashPrefix" className="text-sm font-medium">
                {t("skills.slashPrefix")}
              </Label>
              <p className="text-xs text-muted-foreground">{t("skills.slashPrefixHint")}</p>
            </div>
            <Input
              id="skillSlashPrefix"
              maxLength={1}
              value={slashPrefix}
              onChange={(e) => setSlashPrefix(e.target.value.slice(0, 1))}
              className="w-16 shrink-0 text-center text-base md:text-sm"
            />
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function SkillSwitchRow({
  id,
  label,
  hint,
  checked,
  onCheckedChange,
}: {
  id: string;
  label: string;
  hint: string;
  checked: boolean;
  onCheckedChange: (value: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="space-y-0.5">
        <Label htmlFor={id} className="text-sm font-medium">
          {label}
        </Label>
        <p className="text-xs text-muted-foreground">{hint}</p>
      </div>
      <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}
