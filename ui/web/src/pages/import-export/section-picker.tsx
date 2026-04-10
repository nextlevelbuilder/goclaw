import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";

export interface SectionDef {
  id: string;
  labelKey: string;
  count?: number;
  countLabel?: string;
  required?: boolean;
  hint?: string;
  comingSoon?: boolean;
  children?: SectionDef[];
}

interface SectionPickerProps {
  sections: SectionDef[];
  selected: Set<string>;
  onChange: (selected: Set<string>) => void;
  disabled?: boolean;
  ns?: string;
}

export function SectionPicker({ sections, selected, onChange, disabled, ns = "import-export" }: SectionPickerProps) {
  const { t } = useTranslation(ns);

  const toggle = (id: string, checked: boolean) => {
    const next = new Set(selected);
    if (checked) next.add(id);
    else next.delete(id);
    onChange(next);
  };

  return (
    <div className="rounded-lg border divide-y">
      {sections.map((sec) => (
        <div key={sec.id} className="px-3 py-2.5">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              <Label htmlFor={`sec-${sec.id}`} className={`cursor-pointer text-sm ${sec.required ? "font-medium" : ""}`}>
                {t(sec.labelKey)}
              </Label>
              {sec.required && (
                <Badge variant="outline" className="text-2xs px-1.5 py-0 shrink-0">required</Badge>
              )}
              {sec.count != null && sec.count > 0 && (
                <span className="text-xs text-muted-foreground tabular-nums shrink-0">
                  {sec.countLabel ?? sec.count.toLocaleString()}
                </span>
              )}
              {sec.comingSoon && (
                <Badge variant="secondary" className="text-2xs px-1.5 py-0 shrink-0">soon</Badge>
              )}
            </div>
            <Switch
              id={`sec-${sec.id}`}
              checked={!sec.comingSoon && (sec.required || selected.has(sec.id))}
              onCheckedChange={(v) => toggle(sec.id, v)}
              disabled={disabled || sec.required || sec.comingSoon}
            />
          </div>
          {sec.hint && <p className="text-xs text-muted-foreground mt-0.5">{t(sec.hint)}</p>}

          {sec.children && sec.children.length > 0 && selected.has(sec.id) && (
            <div className="mt-2 ml-4 space-y-2">
              {sec.children.map((child) => (
                <div key={child.id} className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <Label htmlFor={`sec-${child.id}`} className="cursor-pointer text-xs">{t(child.labelKey)}</Label>
                    {child.count != null && child.count > 0 && (
                      <span className="text-xs text-muted-foreground tabular-nums">{child.countLabel ?? child.count.toLocaleString()}</span>
                    )}
                  </div>
                  <Switch
                    id={`sec-${child.id}`}
                    checked={selected.has(child.id)}
                    onCheckedChange={(v) => toggle(child.id, v)}
                    disabled={disabled}
                  />
                </div>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}

/** Presets for agent section selection (no skills/mcp/permissions). */
export const PRESETS: Record<string, ReadonlySet<string>> = {
  minimal: new Set(["config", "context_files"]),
  standard: new Set(["config", "context_files", "memory", "knowledge_graph", "cron"]),
  complete: new Set([
    "config", "context_files", "user_data", "user_context_files", "user_profiles", "user_overrides",
    "memory", "memory_global", "memory_per_user", "knowledge_graph", "cron", "workspace",
  ]),
};
