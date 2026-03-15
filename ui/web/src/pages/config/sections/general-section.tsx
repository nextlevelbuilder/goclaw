import { useState, useEffect } from "react";
import { Save, FolderOpen } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { InfoLabel } from "@/components/shared/info-label";

interface GeneralData {
  app_name?: string;
  app_description?: string;
  data_dir?: string;
}

const DEFAULT: GeneralData = {};

interface Props {
  data: GeneralData | undefined;
  configPath: string;
  onSave: (value: GeneralData) => Promise<void>;
  saving: boolean;
}

export function GeneralSection({ data, configPath, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<GeneralData>(data ?? DEFAULT);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? DEFAULT);
    setDirty(false);
  }, [data]);

  const update = (patch: Partial<GeneralData>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  const handleSave = () => {
    onSave({ ...draft });
  };

  if (!data) return null;

  return (
    <>
      {/* Branding card */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">{t("general.brandingTitle")}</CardTitle>
          <CardDescription>{t("general.brandingDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("general.appNameTip")}>{t("general.appName")}</InfoLabel>
            <Input
              className="text-base md:text-sm"
              value={draft.app_name ?? ""}
              onChange={(e) => update({ app_name: e.target.value })}
              placeholder="GoClaw"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("general.appDescriptionTip")}>{t("general.appDescription")}</InfoLabel>
            <Textarea
              className="text-base md:text-sm"
              value={draft.app_description ?? ""}
              onChange={(e) => update({ app_description: e.target.value })}
              placeholder={t("general.appDescriptionPlaceholder")}
              rows={3}
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("general.dataDirTip")}>{t("general.dataDir")}</InfoLabel>
            <Input
              className="text-base md:text-sm font-mono"
              value={draft.data_dir ?? ""}
              onChange={(e) => update({ data_dir: e.target.value })}
              placeholder="~/.goclaw"
            />
          </div>
          {dirty && (
            <div className="flex justify-end pt-2">
              <Button size="sm" onClick={handleSave} disabled={saving} className="gap-1.5">
                <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      {/* System Info card */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">{t("general.systemInfoTitle")}</CardTitle>
          <CardDescription>{t("general.systemInfoDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-1.5">
            <span className="text-sm font-medium">{t("general.configPath")}</span>
            <div className="flex items-center gap-2 rounded-md border bg-muted/50 px-3 py-2 text-sm font-mono text-muted-foreground">
              <FolderOpen className="h-3.5 w-3.5 shrink-0" />
              <span className="break-all">{configPath || "—"}</span>
            </div>
          </div>
        </CardContent>
      </Card>
    </>
  );
}
