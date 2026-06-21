import type { LucideIcon } from "lucide-react";
import { Info } from "lucide-react";
import { cn } from "@/lib/utils";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { InfoLabel } from "@/components/shared/info-label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export interface FeatureSwitchItem {
  icon?: LucideIcon;
  /** Accent class for icon color, e.g. "text-blue-500" */
  iconClass?: string;
  label: string;
  /** Short description below the label explaining the impact */
  hint: string;
  /** Tooltip explaining the purpose of the feature */
  tooltip?: string;
  checked: boolean;
  onCheckedChange: (v: boolean) => void;
  /** Contextual info message shown when the toggle is ON */
  infoWhenOn?: string;
  /** Accent classes for the info box (border + bg + text + dark variants) */
  infoClass?: string;
}

interface FeatureSwitchGroupProps {
  title: string;
  description?: string;
  items: FeatureSwitchItem[];
  /** Subtle highlight border to draw attention */
  highlight?: boolean;
}

export function FeatureSwitchGroup({
  title,
  description,
  items,
  highlight,
}: FeatureSwitchGroupProps) {
  return (
    <Card
      className={cn(highlight && "border-primary/20 bg-primary/[0.02]")}
    >
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{title}</CardTitle>
        {description && <CardDescription>{description}</CardDescription>}
      </CardHeader>
      <CardContent className="space-y-0">
        {items.map((item, i) => (
          <div
            key={i}
            className={cn(
              "py-4",
              i < items.length - 1 && "border-b",
            )}
          >
            {/* Header row: icon + label ... switch */}
            <div className="flex items-center justify-between gap-4">
              <div className="flex items-center gap-3">
                {item.icon && (
                  <item.icon className={cn("h-4 w-4 shrink-0", item.iconClass ?? "text-muted-foreground")} />
                )}
                <div className="space-y-1">
                  {item.tooltip ? (
                    <InfoLabel tip={item.tooltip} labelClassName="text-sm font-medium">
                      {item.label}
                    </InfoLabel>
                  ) : (
                    <Label className="text-sm font-medium">{item.label}</Label>
                  )}
                  <p className="text-xs text-muted-foreground">{item.hint}</p>
                </div>
              </div>
              <Switch
                checked={item.checked}
                onCheckedChange={item.onCheckedChange}
                className="shrink-0"
              />
            </div>

            {/* Conditional info box when enabled */}
            {item.checked && item.infoWhenOn && (
              <div className={cn(
                "mt-3 flex items-start gap-2 rounded-md border px-3 py-2 text-xs",
                item.infoClass ?? "border-primary/20 bg-primary/5 text-primary dark:bg-primary/10",
              )}>
                <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                <span>{item.infoWhenOn}</span>
              </div>
            )}
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
