import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";

export { InfoLabel } from "@/components/shared/info-label";

interface ConfigSectionProps {
  title: string;
  description: string;
  enabled: boolean;
  onToggle: (v: boolean) => void;
  children: React.ReactNode;
}

export function ConfigSection({
  title,
  description,
  enabled,
  onToggle,
  children,
}: ConfigSectionProps) {
  const { t } = useTranslation("agents");
  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium">{title}</h3>
          <p className="text-xs text-muted-foreground">{description}</p>
        </div>
        <Switch checked={enabled} onCheckedChange={onToggle} />
      </div>
      {enabled ? (
        <div className="rounded-lg border p-3 space-y-4 sm:p-4">{children}</div>
      ) : (
        <p className="text-xs text-muted-foreground italic">
          {t("config.usingGlobalDefaults")}
        </p>
      )}
    </section>
  );
}

/** Parse a string input value to number or undefined. */
export function numOrUndef(v: string): number | undefined {
  const n = Number(v);
  return isNaN(n) || v === "" ? undefined : n;
}

/** Convert comma-separated string to string array, or undefined if empty. */
export function tagsToArray(s: string): string[] | undefined {
  const trimmed = s.trim();
  if (!trimmed) return undefined;
  return trimmed.split(",").map((t) => t.trim()).filter(Boolean);
}

/** Convert string array to comma-separated display string. */
export function arrayToTags(arr?: string[]): string {
  return arr?.join(", ") ?? "";
}
