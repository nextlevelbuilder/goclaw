import { Combobox } from "@/components/ui/combobox";
import { useUserPicker } from "@/hooks/use-user-picker";

interface UserPickerComboboxProps {
  value: string;
  onChange: (value: string) => void;
  /** Fires only on dropdown item click or custom value commit (not keystrokes). */
  onSelect?: (value: string) => void;
  placeholder?: string;
  className?: string;
  /** Filter contacts by peer_kind: "direct" | "group" | undefined (all). */
  peerKind?: "direct" | "group";
  /** Filter by source: "contact" | "tenant_user" | undefined (both).
   *  Use "tenant_user" for merge dialogs and tenant user pickers. */
  source?: "contact" | "tenant_user";
  /** Allow typing custom values not in the list. Default true. */
  allowCustom?: boolean;
  /** Render dropdown into a portal container (useful inside dialogs). */
  portalContainer?: React.RefObject<HTMLElement | null>;
}

/**
 * Unified user picker that searches both channel_contacts and tenant_users.
 * Drop-in replacement for Combobox + useContactSearch/useContactPicker.
 *
 * - Shows 30 most recent results when opened (no typing needed)
 * - Debounced server-side search as user types
 * - Source badges: [telegram], [discord], [tenant], merged status
 * - Deduplicates merged contacts
 *
 * Uses `value` prop as search term (same pattern as useContactSearch(userId)).
 */
export function UserPickerCombobox({
  value,
  onChange,
  onSelect,
  placeholder,
  className,
  peerKind,
  source,
  allowCustom = true,
  portalContainer,
}: UserPickerComboboxProps) {
  const { options } = useUserPicker(value, peerKind, source);

  return (
    <Combobox
      value={value}
      onChange={onChange}
      onSelect={onSelect}
      options={options}
      placeholder={placeholder}
      className={className}
      allowCustom={allowCustom}
      portalContainer={portalContainer}
    />
  );
}
