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

interface BrandData {
    app_name?: string;
    app_key?: string;
    tagline?: string;
    logo_url?: string;
}

const DEFAULT: BrandData = {};

interface Props {
    data: BrandData | undefined;
    onSave: (value: BrandData) => Promise<void>;
    saving: boolean;
}

export function BrandSection({ data, onSave, saving }: Props) {
    const { t } = useTranslation("config");
    const [draft, setDraft] = useState<BrandData>(data ?? DEFAULT);
    const [dirty, setDirty] = useState(false);

    useEffect(() => {
        setDraft(data ?? DEFAULT);
        setDirty(false);
    }, [data]);

    const update = (patch: Partial<BrandData>) => {
        setDraft((prev) => ({ ...prev, ...patch }));
        setDirty(true);
    };

    if (!data) return null;

    return (
        <Card>
            <CardHeader className="pb-3">
                <CardTitle className="text-base">{t("brand.title")}</CardTitle>
                <CardDescription>{t("brand.description")}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                    <div className="grid gap-1.5">
                        <InfoLabel tip={t("brand.appNameTip")}>{t("brand.appName")}</InfoLabel>
                        <Input
                            value={draft.app_name ?? ""}
                            onChange={(e) => update({ app_name: e.target.value })}
                            placeholder={t("brand.appNamePlaceholder")}
                        />
                    </div>
                    <div className="grid gap-1.5">
                        <InfoLabel tip={t("brand.appKeyTip")}>{t("brand.appKey")}</InfoLabel>
                        <Input
                            value={draft.app_key ?? ""}
                            onChange={(e) => update({ app_key: e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "") })}
                            placeholder={t("brand.appKeyPlaceholder")}
                        />
                    </div>
                </div>

                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                    <div className="grid gap-1.5">
                        <InfoLabel tip={t("brand.taglineTip")}>{t("brand.tagline")}</InfoLabel>
                        <Input
                            value={draft.tagline ?? ""}
                            onChange={(e) => update({ tagline: e.target.value })}
                            placeholder={t("brand.taglinePlaceholder")}
                        />
                    </div>
                    <div className="grid gap-1.5">
                        <InfoLabel tip={t("brand.logoUrlTip")}>{t("brand.logoUrl")}</InfoLabel>
                        <Input
                            value={draft.logo_url ?? ""}
                            onChange={(e) => update({ logo_url: e.target.value })}
                            placeholder={t("brand.logoUrlPlaceholder")}
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
