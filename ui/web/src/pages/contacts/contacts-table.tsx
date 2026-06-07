import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, Copy, Link2, Loader2, ShieldPlus } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
  onAllowContact?: (contact: ChannelContact) => void;
  isContactAllowed?: (contact: ChannelContact) => boolean;
  allowingIds?: Set<string>;
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
  onAllowContact,
  isContactAllowed,
  allowingIds = new Set(),
}: ContactsTableProps) {
  const { t } = useTranslation("contacts");
  const [copiedId, setCopiedId] = useState<string | null>(null);

  const copySenderId = async (contact: ChannelContact) => {
    try {
      await navigator.clipboard.writeText(contact.sender_id);
    } catch {
      const textarea = document.createElement("textarea");
      textarea.value = contact.sender_id;
      textarea.style.position = "fixed";
      textarea.style.opacity = "0";
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand("copy");
      document.body.removeChild(textarea);
    }
    setCopiedId(contact.id);
    window.setTimeout(() => setCopiedId(null), 1400);
  };

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
            <th className="px-3 py-2.5 text-right font-medium text-xs uppercase tracking-wide text-muted-foreground">{t("columns.actions")}</th>
          </tr>
        </thead>
        <tbody>
          {contacts.map((c) => {
            const isZalo = c.channel_type === "zalo_personal" || c.channel_type === "zalo_oa";
            const allowed = isContactAllowed?.(c) ?? false;
            const allowing = allowingIds.has(c.id);
            return (
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
                <td className="px-3 py-2.5" onClick={(e) => e.stopPropagation()}>
                  <div className="flex items-center justify-end gap-1.5">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8"
                      title={t("actions.copySenderId")}
                      onClick={() => copySenderId(c)}
                    >
                      {copiedId === c.id ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                    </Button>
                    {isZalo && onAllowContact && (
                      <Button
                        type="button"
                        variant={allowed ? "secondary" : "outline"}
                        size="sm"
                        className="h-8 gap-1.5"
                        disabled={allowed || allowing}
                        title={allowed ? t("actions.allowed") : t("actions.allowZalo")}
                        onClick={() => onAllowContact(c)}
                      >
                        {allowing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ShieldPlus className="h-3.5 w-3.5" />}
                        {allowed ? t("actions.allowed") : t("actions.allow")}
                      </Button>
                    )}
                  </div>
                </td>
              </tr>
            );
          })}
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
