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
import { InfoLabel } from "@/components/shared/info-label";
import { TagInput } from "@/components/shared/tag-input";

interface ServerData {
  host?: string;
  port?: number;
  token?: string;
  owner_ids?: string[];
  allowed_origins?: string[];
}

const DEFAULT: ServerData = {};

function isSecret(val: unknown): boolean {
  return typeof val === "string" && val.includes("***");
}

interface Props {
  data: ServerData | undefined;
  onSave: (value: ServerData) => Promise<void>;
  saving: boolean;
}

export function ServerSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const [draft, setDraft] = useState<ServerData>(data ?? DEFAULT);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data ?? DEFAULT);
    setDirty(false);
  }, [data]);

  const update = (patch: Partial<ServerData>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  const handleSave = () => {
    const toSave = { ...draft };
    if (isSecret(toSave.token)) delete toSave.token;
    onSave(toSave).catch(() => {});
  };

  if (!data) return null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("server.title")}</CardTitle>
        <CardDescription>{t("server.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("gateway.hostTip")}>{t("gateway.host")}</InfoLabel>
            <Input
              value={draft.host ?? ""}
              onChange={(e) => update({ host: e.target.value })}
              placeholder="0.0.0.0"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("gateway.portTip")}>{t("gateway.port")}</InfoLabel>
            <Input
              type="number"
              value={draft.port ?? ""}
              onChange={(e) => update({ port: Number(e.target.value) })}
              placeholder="18790"
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("gateway.tokenTip")}>{t("gateway.token")}</InfoLabel>
            <Input
              type="password"
              value={draft.token ?? ""}
              disabled={isSecret(draft.token)}
              readOnly={isSecret(draft.token)}
              onChange={(e) => update({ token: e.target.value })}
            />
            {isSecret(draft.token) && (
              <p className="text-xs text-muted-foreground">{t("gateway.tokenManaged")}</p>
            )}
          </div>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="grid gap-1.5">
            <InfoLabel tip={t("gateway.ownerIdsTip")}>{t("gateway.ownerIds")}</InfoLabel>
            <TagInput
              value={draft.owner_ids ?? []}
              onChange={(v) => update({ owner_ids: v })}
              placeholder={t("gateway.ownerIdsPlaceholder")}
            />
          </div>
          <div className="grid gap-1.5">
            <InfoLabel tip={t("gateway.allowedOriginsTip")}>{t("gateway.allowedOrigins")}</InfoLabel>
            <TagInput
              value={draft.allowed_origins ?? []}
              onChange={(v) => update({ allowed_origins: v })}
              placeholder={t("gateway.allowedOriginsPlaceholder")}
            />
          </div>
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
  );
}
