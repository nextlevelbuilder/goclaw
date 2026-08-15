import { useState, useCallback, useRef, useEffect } from "react";
import type { ComboboxOption } from "@/components/ui/combobox";
import type { ChannelContact } from "@/types/contact";

/** Formats a ChannelContact as a combobox option. */
function contactToOption(c: ChannelContact): ComboboxOption {
  const parts: string[] = [];
  if (c.display_name) parts.push(c.display_name);
  if (c.username) parts.push(`@${c.username}`);
  parts.push(`(${c.sender_id})`);
  return { value: c.sender_id, label: parts.join(" — ") };
}

/**
 * Reusable hook for contact search combobox.
 * Encapsulates debounced search, contact cache, and ComboboxOption formatting.
 */
export function useContactPicker(
  listContacts: (search: string) => Promise<ChannelContact[]>,
  debounceMs = 300,
) {
  const [options, setOptions] = useState<ComboboxOption[]>([]);
  const cacheRef = useRef<Record<string, ChannelContact>>({});
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  // Cleanup debounce on unmount
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  const searchContacts = useCallback(
    (query: string) => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (!query || query.length < 2) {
        setOptions([]);
        return;
      }
      debounceRef.current = setTimeout(async () => {
        try {
          const contacts = await listContacts(query);
          const cache: Record<string, ChannelContact> = {};
          const opts = contacts.map((c) => {
            cache[c.sender_id] = c;
            return contactToOption(c);
          });
          cacheRef.current = { ...cacheRef.current, ...cache };
          setOptions(opts);
        } catch {
          setOptions([]);
        }
      }, debounceMs);
    },
    [listContacts, debounceMs],
  );

  /** Get a cached contact by sender_id. */
  const getContact = useCallback((senderID: string): ChannelContact | undefined => {
    return cacheRef.current[senderID];
  }, []);

  /** Clear options (e.g. after form submission). */
  const clearOptions = useCallback(() => setOptions([]), []);

  return { options, searchContacts, getContact, clearOptions };
}
