import { useState } from "react";
import { X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { UserPickerCombobox } from "./user-picker-combobox";

interface MultiUserPickerProps {
  value: string[];
  onChange: (values: string[]) => void;
  placeholder?: string;
  /** Filter by source: "contact" | "tenant_user" | undefined (both). */
  source?: "contact" | "tenant_user";
  /** Filter contacts by peer_kind. */
  peerKind?: "direct" | "group";
  /** Allow typing custom values not in the list. Default true. */
  allowCustom?: boolean;
  /** Render dropdown into a portal container (useful inside dialogs). */
  portalContainer?: React.RefObject<HTMLElement | null>;
}

/**
 * Multi-select user picker: search + select → badge list.
 * Wraps UserPickerCombobox for multi-value fields (allow_from, deny lists).
 *
 * Uses `onSelect` (fires on dropdown click / custom commit) instead of
 * `onChange` (fires on every keystroke) to avoid adding partial text.
 */
export function MultiUserPicker({
  value,
  onChange,
  placeholder,
  source,
  peerKind,
  allowCustom = true,
  portalContainer,
}: MultiUserPickerProps) {
  const [inputValue, setInputValue] = useState("");

  const handleCommit = (val: string) => {
    const trimmed = val.trim();
    if (trimmed && !value.includes(trimmed)) {
      onChange([...value, trimmed]);
    }
    setInputValue("");
  };

  return (
    <div className="space-y-2">
      <UserPickerCombobox
        value={inputValue}
        onChange={setInputValue}
        onSelect={handleCommit}
        placeholder={placeholder}
        source={source}
        peerKind={peerKind}
        allowCustom={allowCustom}
        portalContainer={portalContainer}
      />
      {value.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {value.map((id) => (
            <Badge key={id} variant="secondary" className="gap-1 pr-1">
              {id}
              <button
                type="button"
                onClick={() => onChange(value.filter((v) => v !== id))}
                className="relative ml-0.5 cursor-pointer rounded-full p-0.5 hover:bg-muted after:absolute after:-inset-2 after:content-[''] md:after:hidden"
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
