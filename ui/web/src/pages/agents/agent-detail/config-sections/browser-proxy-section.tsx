import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";

interface BrowserProxySectionProps {
  value: boolean;
  onChange: (v: boolean) => void;
}

export function BrowserProxySection({ value, onChange }: BrowserProxySectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.browserProxy";

  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">{t(`${s}.title`)}</h3>
        <p className="text-xs text-muted-foreground">
          {t(`${s}.description`)}
        </p>
      </div>
      <div className="flex items-center gap-3">
        <Switch
          id="browser-use-proxy"
          checked={value}
          onCheckedChange={onChange}
        />
        <Label htmlFor="browser-use-proxy" className="text-sm">
          {t(`${s}.label`)}
        </Label>
      </div>
    </section>
  );
}
