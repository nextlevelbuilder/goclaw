import { useTranslation } from "react-i18next";

// SystemBadge marks a hook row as shipped by the binary (source='builtin').
// User cannot edit content — only toggle enabled. Placed next to the event
// chip in hook-list-row so it's visible without opening the detail panel.
export function SystemBadge() {
  const { t } = useTranslation("hooks");
  return (
    <span
      className="ml-1 inline-flex items-center rounded px-1.5 py-0.5 text-2xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300"
      title={t("form.builtinReadonly")}
    >
      {t("table.systemBadge")}
    </span>
  );
}
