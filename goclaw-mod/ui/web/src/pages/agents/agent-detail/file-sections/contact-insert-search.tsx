import { useState } from "react";
import { useTranslation } from "react-i18next";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";
import { useUserPicker } from "@/hooks/use-user-picker";
import type { UserPickerItem } from "@/hooks/use-user-picker";

/** Format a UserPickerItem into a human-readable snippet for insertion into context files. */
function formatSnippet(item: UserPickerItem): string {
  const parts: string[] = [];
  if (item.display_name) parts.push(item.display_name);
  if (item.username) parts.push(`@${item.username}`);
  if (item.channel_type) parts.push(`${item.channel_type}:${item.id}`);
  else parts.push(item.id);
  return `- ${parts.join(" — ")}`;
}

interface ContactInsertSearchProps {
  onInsert: (text: string) => void;
}

export function ContactInsertSearch({ onInsert }: ContactInsertSearchProps) {
  const { t } = useTranslation("agents");
  const [search, setSearch] = useState("");
  const { results } = useUserPicker(search);

  const handleChange = (val: string) => {
    const item = results.find((r) => r.id === val);
    if (item) {
      onInsert(formatSnippet(item));
      setSearch("");
    } else {
      setSearch(val);
    }
  };

  return (
    <UserPickerCombobox
      value={search}
      onChange={handleChange}
      placeholder={t("files.insertContact")}
      className="h-8 w-full max-w-sm"
    />
  );
}
