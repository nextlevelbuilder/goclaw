import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { formatRelativeTime } from "@/lib/format";
import type { AgentData } from "@/types/agent";
import type { VaultDocument } from "@/types/vault";

const DOC_TYPE_COLORS: Record<string, string> = {
  context: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
  memory: "bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300",
  note: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300",
  skill: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
  episodic: "bg-orange-100 text-orange-700 dark:bg-orange-900 dark:text-orange-300",
  media: "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
};

const SCOPE_COLORS: Record<string, string> = {
  personal: "bg-sky-100 text-sky-700 dark:bg-sky-900 dark:text-sky-300",
  team: "bg-violet-100 text-violet-700 dark:bg-violet-900 dark:text-violet-300",
  shared: "bg-emerald-100 text-emerald-700 dark:bg-emerald-900 dark:text-emerald-300",
};

/** Truncate path from the start, keeping the tail visible (e.g. ".../subdir/file.md") */
function truncatePath(path: string, maxLen = 40): string {
  if (path.length <= maxLen) return path;
  return "\u2026" + path.slice(-(maxLen - 1));
}

interface Props {
  documents: VaultDocument[];
  agents?: AgentData[];
  loading: boolean;
  onSelect: (doc: VaultDocument) => void;
}

export function VaultDocumentsTable({ documents, agents, loading, onSelect }: Props) {
  const { t } = useTranslation("vault");

  // Build agent lookup map: id → display_name or agent_key
  const agentMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents ?? []) map.set(a.id, a.display_name || a.agent_key);
    return map;
  }, [agents]);

  if (loading && documents.length === 0) {
    return <div className="h-[200px] animate-pulse rounded-md bg-muted" />;
  }

  if (documents.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center space-y-2">
        <p className="text-sm font-medium">{t("noDocuments")}</p>
      </div>
    );
  }

  return (
    <div className="rounded-md border">
      <div className="overflow-x-auto">
        <table className="w-full text-sm min-w-[600px]">
          <thead>
            <tr className="border-b bg-muted/50 text-left">
              <th className="px-3 py-2 font-medium">{t("columns.title")}</th>
              <th className="px-3 py-2 font-medium">{t("columns.agent")}</th>
              <th className="px-3 py-2 font-medium">{t("columns.path")}</th>
              <th className="px-3 py-2 font-medium">{t("columns.type")}</th>
              <th className="px-3 py-2 font-medium">{t("columns.scope")}</th>
              <th className="px-3 py-2 font-medium">{t("columns.updated")}</th>
            </tr>
          </thead>
          <tbody>
            {documents.map((doc) => (
              <tr
                key={doc.id}
                className="border-b hover:bg-muted/30 cursor-pointer"
                onClick={() => onSelect(doc)}
              >
                <td className="px-3 py-2 font-medium max-w-[200px] truncate" title={doc.title}>
                  {doc.title || doc.path.split("/").pop()}
                </td>
                <td className="px-3 py-2 text-muted-foreground text-xs whitespace-nowrap">
                  {doc.agent_id ? (agentMap.get(doc.agent_id) ?? doc.agent_id.slice(0, 8)) : t("scope.shared")}
                </td>
                <td className="px-3 py-2 text-muted-foreground max-w-[200px]" title={doc.path}>
                  <span className="font-mono text-xs">{truncatePath(doc.path)}</span>
                </td>
                <td className="px-3 py-2">
                  <Badge variant="outline" className={DOC_TYPE_COLORS[doc.doc_type] ?? ""}>
                    {t(`type.${doc.doc_type}`)}
                  </Badge>
                </td>
                <td className="px-3 py-2">
                  <Badge variant="outline" className={SCOPE_COLORS[doc.scope] ?? ""}>
                    {t(`scope.${doc.scope}`)}
                  </Badge>
                </td>
                <td className="px-3 py-2 text-xs text-muted-foreground whitespace-nowrap">
                  {formatRelativeTime(doc.updated_at)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
