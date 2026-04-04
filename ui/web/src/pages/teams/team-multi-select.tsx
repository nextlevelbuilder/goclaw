import { X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Combobox } from "@/components/ui/combobox";

/** Generic multi-select with static options (used for channel type selection). */
export function MultiSelect({
  options,
  selected,
  onChange,
  placeholder,
}: {
  options: { value: string; label?: string }[];
  selected: string[];
  onChange: (values: string[]) => void;
  placeholder: string;
}) {
  return (
    <div className="space-y-2">
      <Combobox
        value=""
        onChange={(val) => {
          if (val && !selected.includes(val)) {
            onChange([...selected, val]);
          }
        }}
        options={options.filter((o) => !selected.includes(o.value))}
        placeholder={placeholder}
      />
      {selected.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {selected.map((id) => (
            <Badge key={id} variant="secondary" className="gap-1 pr-1">
              {options.find((o) => o.value === id)?.label ?? id}
              <button
                type="button"
                onClick={() => onChange(selected.filter((s) => s !== id))}
                className="ml-0.5 cursor-pointer rounded-full p-0.5 hover:bg-muted"
              >
                <X className="h-3 w-3" />
              </button>
            </Badge>
          ))}
        </div>
      )}
    </div>
  );
}
