import { useEffect, useRef, useState } from "react";
import { Image, Save, Upload } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { InfoLabel } from "@/components/shared/info-label";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import {
  BRANDING_ASSET_ACCEPT,
  type BrandingAssetField,
  type BrandingData,
  buildBrandingPatch,
  normalizeBrandingDraft,
  validateBrandingAssetFile,
  withUploadedBrandingAssetURL,
} from "./branding-section-utils";

interface Props {
  data: BrandingData | undefined;
  onSave: (value: BrandingData) => Promise<void>;
  saving: boolean;
}

interface UploadResponse {
  url: string;
}

const ASSET_FIELDS: Array<{ key: BrandingAssetField; labelKey: string; tipKey: string; placeholder: string }> = [
  {
    key: "logo_url",
    labelKey: "branding.logoUrl",
    tipKey: "branding.logoUrlTip",
    placeholder: "/branding-assets/logo.png",
  },
  {
    key: "favicon_url",
    labelKey: "branding.faviconUrl",
    tipKey: "branding.faviconUrlTip",
    placeholder: "/branding-assets/favicon.ico",
  },
  {
    key: "apple_touch_icon_url",
    labelKey: "branding.appleTouchIconUrl",
    tipKey: "branding.appleTouchIconUrlTip",
    placeholder: "/branding-assets/apple-touch-icon.png",
  },
  {
    key: "og_image_url",
    labelKey: "branding.ogImageUrl",
    tipKey: "branding.ogImageUrlTip",
    placeholder: "/branding-assets/og-image.png",
  },
];

export function BrandingSection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const http = useHttp();
  const [draft, setDraft] = useState<BrandingData>(() => normalizeBrandingDraft(data));
  const [dirty, setDirty] = useState(false);
  const [uploadingField, setUploadingField] = useState<BrandingAssetField | null>(null);
  const inputRefs = useRef<Record<string, HTMLInputElement | null>>({});

  useEffect(() => {
    setDraft(normalizeBrandingDraft(data));
    setDirty(false);
  }, [data]);

  const update = (patch: Partial<BrandingData>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  const handleUpload = async (field: BrandingAssetField, file?: File) => {
    if (!file) return;
    const validation = validateBrandingAssetFile(file);
    if (validation) {
      toast.error(t("branding.uploadFailed"), t(`branding.uploadErrors.${validation}`));
      return;
    }

    const form = new FormData();
    form.append("file", file);
    setUploadingField(field);
    try {
      const result = await http.upload<UploadResponse>("/v1/branding/assets", form);
      setDraft((prev) => withUploadedBrandingAssetURL(prev, field, result.url));
      setDirty(true);
      toast.success(t("branding.uploaded"));
    } catch (err) {
      toast.error(t("branding.uploadFailed"), err instanceof Error ? err.message : "");
    } finally {
      setUploadingField(null);
      const input = inputRefs.current[field];
      if (input) input.value = "";
    }
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("branding.title")}</CardTitle>
        <CardDescription>{t("branding.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <TextField
            label={t("branding.appName")}
            tip={t("branding.appNameTip")}
            value={draft.app_name}
            onChange={(value) => update({ app_name: value })}
            placeholder="GoClaw"
          />
          <TextField
            label={t("branding.appShortName")}
            tip={t("branding.appShortNameTip")}
            value={draft.app_short_name}
            onChange={(value) => update({ app_short_name: value })}
            placeholder="GoClaw"
          />
          <TextField
            label={t("branding.themeColor")}
            tip={t("branding.themeColorTip")}
            value={draft.theme_color}
            onChange={(value) => update({ theme_color: value })}
            placeholder="#111827"
          />
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <TextField
            label={t("branding.metaTitle")}
            tip={t("branding.metaTitleTip")}
            value={draft.meta_title}
            onChange={(value) => update({ meta_title: value })}
            placeholder="GoClaw"
          />
          <TextField
            label={t("branding.metaKeywords")}
            tip={t("branding.metaKeywordsTip")}
            value={draft.meta_keywords}
            onChange={(value) => update({ meta_keywords: value })}
            placeholder="ai, agent, gateway"
          />
        </div>
        <TextField
          label={t("branding.metaDescription")}
          tip={t("branding.metaDescriptionTip")}
          value={draft.meta_description}
          onChange={(value) => update({ meta_description: value })}
          placeholder={t("branding.metaDescriptionPlaceholder")}
        />

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <TextField
            label={t("branding.ogTitle")}
            tip={t("branding.ogTitleTip")}
            value={draft.og_title}
            onChange={(value) => update({ og_title: value })}
            placeholder="GoClaw"
          />
          <TextField
            label={t("branding.ogDescription")}
            tip={t("branding.ogDescriptionTip")}
            value={draft.og_description}
            onChange={(value) => update({ og_description: value })}
            placeholder={t("branding.metaDescriptionPlaceholder")}
          />
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          {ASSET_FIELDS.map((field) => {
            const current = draft[field.key] ?? "";
            return (
              <div key={field.key} className="grid gap-1.5">
                <InfoLabel tip={t(field.tipKey)}>{t(field.labelKey)}</InfoLabel>
                <div className="flex gap-2">
                  <Input
                    value={current}
                    onChange={(event) => update({ [field.key]: event.target.value })}
                    placeholder={field.placeholder}
                  />
                  <input
                    ref={(node) => { inputRefs.current[field.key] = node; }}
                    type="file"
                    accept={BRANDING_ASSET_ACCEPT}
                    className="hidden"
                    onChange={(event) => handleUpload(field.key, event.target.files?.[0])}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    title={t("branding.upload")}
                    disabled={uploadingField !== null}
                    onClick={() => inputRefs.current[field.key]?.click()}
                  >
                    <Upload className={"h-4 w-4" + (uploadingField === field.key ? " animate-pulse" : "")} />
                  </Button>
                </div>
                {current && (
                  <div className="flex items-center gap-2 rounded-md border bg-muted/30 px-2 py-1.5 text-xs text-muted-foreground">
                    <Image className="h-3.5 w-3.5 shrink-0" />
                    <span className="truncate">{current}</span>
                  </div>
                )}
              </div>
            );
          })}
        </div>

        {dirty && (
          <div className="flex justify-end pt-2">
            <Button size="sm" onClick={() => onSave(buildBrandingPatch(draft))} disabled={saving} className="gap-1.5">
              <Save className="h-3.5 w-3.5" /> {saving ? t("saving") : t("save")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function TextField({
  label,
  tip,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  tip: string;
  value: string | undefined;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  return (
    <div className="grid gap-1.5">
      <InfoLabel tip={tip}>{label}</InfoLabel>
      <Input value={value ?? ""} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} />
    </div>
  );
}
