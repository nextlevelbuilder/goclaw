import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { ParamField } from "./media-provider-params-schema";

/** Renders a single provider param field based on its type declaration. */
export function ParamFieldControl({
  field,
  value,
  onChange,
}: {
  field: ParamField;
  value: unknown;
  onChange: (v: unknown) => void;
}) {
  return (
    <div className="space-y-1">
      <Label className="text-xs">{field.label}</Label>
      {field.type === "select" && field.options && (
        <Select value={String(value ?? "")} onValueChange={onChange}>
          <SelectTrigger className="h-8 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {field.options.map((opt) => (
              <SelectItem key={opt.value} value={opt.value}>
                {opt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {field.type === "toggle" && (
        <div className="flex items-center h-8">
          <Switch
            size="sm"
            checked={Boolean(value)}
            onCheckedChange={onChange}
          />
        </div>
      )}
      {field.type === "number" && (
        <Input
          type="number"
          min={field.min}
          max={field.max}
          step={field.step}
          value={Number(value ?? 0)}
          onChange={(e) => onChange(Number(e.target.value))}
          className="h-8 text-sm"
        />
      )}
      {field.type === "text" && (
        <div className="space-y-1">
          <Input
            value={String(value ?? "")}
            onChange={(e) => onChange(e.target.value)}
            placeholder={field.description}
            className="h-8 text-sm"
          />
          {field.description && (
            <p className="text-xs text-muted-foreground">{field.description}</p>
          )}
        </div>
      )}
    </div>
  );
}
