import { useTranslation } from "react-i18next";
import { Link2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Pagination } from "@/components/shared/pagination";
import { formatDate } from "@/lib/format";
import type { ChannelContact } from "@/types/contact";

interface ContactsTableProps {
  contacts: ChannelContact[];
  selectedIds: Set<string>;
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
  onToggleSelect: (id: string) => void;
  onToggleSelectAll: () => void;
  onPageChange: (page: number) => void;
  onPageSizeChange: (size: number) => void;
}

export function ContactsTable({
  contacts,
  selectedIds,
  total,
  page,
  pageSize,
  totalPages,
  onToggleSelect,
  onToggleSelectAll,
  onPageChange,
  onPageSizeChange,
}: ContactsTableProps) {
  const { t } = useTranslation("contacts");

  return (
    <div className="rounded-md border overflow-x-auto">
      <table className="w-full min-w-[750px] text-sm">
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="w-10 px-3 py-2.5">
              <input
                type="checkbox"
                checked={contacts.length > 0 && selectedIds.size === contacts.length}
                onChange={onToggleSelectAll}
                className="accent-primary h-4 w-4 cursor-pointer"
              />
            </th>
            <th className="px-3 py-2.5 text-left font-medium text-xs uppercase tracking-wide text-muted-foreground">{t("columns.name")}</th>
            <th className="px-3 py-2.5 text-left font-medium text-xs uppercase tracking-wide text-muted-foreground">{t("columns.username")}</th>
            <th className="px-3 py-2.5 text-left font-medium text-xs uppercase tracking-wide text-muted-foreground">{t("columns.senderId")}</th>
            <th className="px-3 py-2.5 text-left font-medium text-xs uppercase tracking-wide text-muted-foreground">{t("columns.channelType")}</th>
            <th className="px-3 py-2.5 text-left font-medium text-xs uppercase tracking-wide text-muted-foreground">{t("columns.peerKind")}</th>
            <th className="px-3 py-2.5 text-left font-medium text-xs uppercase tracking-wide text-muted-foreground">{t("columns.lastSeen")}</th>
          </tr>
        </thead>
        <tbody>
          {contacts.map((c) => (
            <tr
              key={c.id}
              className={`border-b last:border-0 transition-colors cursor-pointer ${
                selectedIds.has(c.id) ? "bg-primary/5" : "hover:bg-muted/20"
              }`}
              onClick={() => onToggleSelect(c.id)}
            >
              <td className="px-3 py-2.5" onClick={(e) => e.stopPropagation()}>
                <input
                  type="checkbox"
                  checked={selectedIds.has(c.id)}
                  onChange={() => onToggleSelect(c.id)}
                  className="accent-primary h-4 w-4 cursor-pointer"
                />
              </td>
              <td className="px-3 py-2.5">
                <span className="flex items-center gap-1.5">
                  {c.display_name || <span className="text-muted-foreground">—</span>}
                  {c.merged_id && (
                    <span title={t("columns.merged")}>
                      <Link2 className="h-3 w-3 text-blue-500 shrink-0" />
                    </span>
                  )}
                </span>
              </td>
              <td className="px-3 py-2.5">
                {c.username
                  ? <span className="text-muted-foreground">@{c.username}</span>
                  : <span className="text-muted-foreground">—</span>
                }
              </td>
              <td className="px-3 py-2.5 font-mono text-xs">
                {c.sender_id}
                {c.thread_id && <span className="text-muted-foreground">:topic:{c.thread_id}</span>}
              </td>
              <td className="px-3 py-2.5">
                <Badge variant="outline" className="text-xs-plus">{c.channel_type}</Badge>
              </td>
              <td className="px-3 py-2.5">
                <Badge
                  variant={c.contact_type === "user" ? "outline" : c.contact_type === "topic" ? "outline" : "secondary"}
                  className={
                    c.contact_type === "user"
                      ? "text-xs-plus bg-primary/10 text-primary border-primary/30 dark:bg-primary/20 dark:text-primary-foreground dark:border-primary/40"
                      : "text-xs-plus"
                  }
                >
                  {c.contact_type === "user" ? t("types.user") : c.contact_type === "topic" ? t("types.topic", "Topic") : t("types.group")}
                </Badge>
              </td>
              <td className="px-3 py-2.5 text-muted-foreground text-xs">
                {formatDate(c.last_seen_at)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <Pagination
        page={page}
        pageSize={pageSize}
        total={total}
        totalPages={totalPages}
        onPageChange={onPageChange}
        onPageSizeChange={onPageSizeChange}
      />
    </div>
  );
}
