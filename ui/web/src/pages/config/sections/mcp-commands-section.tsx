import { useState, useEffect } from "react";
import { Save } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { InfoLabel } from "@/components/shared/info-label";
import { TagInput } from "@/components/shared/tag-input";

type ToolsData = Record<string, any>;

interface Props {
  data: ToolsData | undefined;
  onSave: (value: ToolsData) => Promise<void>;
  saving: boolean;
}

export function McpCommandsSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<ToolsData>(data ?? {});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? {});
    setDirty(false);
  }, [data]);

  if (!data) return null;

  const commands: string[] = draft.allowed_commands ?? [];

  const update = (patch: Record<string, any>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("tools.mcpCommands")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-1.5">
          <InfoLabel tip={t("tools.mcpCommandsTip")}>
            {t("tools.mcpCommandsLabel")}
          </InfoLabel>
          <TagInput
            value={commands}
            onChange={(v) => update({ allowed_commands: v })}
            placeholder={t("tools.mcpCommandsPlaceholder")}
          />
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
