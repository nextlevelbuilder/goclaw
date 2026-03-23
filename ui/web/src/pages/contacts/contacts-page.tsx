import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, Contact, Info, Link2, Merge, RefreshCw, Search, Unlink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { Pagination } from "@/components/shared/pagination";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { toast } from "@/stores/use-toast-store";
import { formatDate } from "@/lib/format";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useContacts } from "./hooks/use-contacts";
import { useContactMerge } from "./hooks/use-contact-merge";
import { MergeContactsDialog } from "./merge-contacts-dialog";

const CHANNEL_TYPES = ["telegram", "discord", "slack", "whatsapp", "zalo_oa", "zalo_personal", "feishu"];
const PERM_CHANNELS = ["telegram", "discord", "zalo", "slack", "feishu"] as const;

export function ContactsPage() {
  const { t } = useTranslation("contacts");
  const { t: tc } = useTranslation("common");

  const [search, setSearch] = useState("");
  const [appliedSearch, setAppliedSearch] = useState("");
  const [channelType, setChannelType] = useState("");
  const [peerKind, setPeerKind] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  // Selection state
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [mergeDialogOpen, setMergeDialogOpen] = useState(false);

  const { contacts, total, loading, fetching, refresh } = useContacts({
    search: appliedSearch || undefined,
    channelType: channelType || undefined,
    peerKind: peerKind || undefined,
    limit: pageSize,
    offset: (page - 1) * pageSize,
  });
  const { unmerge } = useContactMerge();

  const spinning = useMinLoading(fetching);
  const showSkeleton = useDeferredLoading(loading && contacts.length === 0);
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  // Clear selection on page/filter change
  useEffect(() => {
    setSelectedIds(new Set());
  }, [page, pageSize, appliedSearch, channelType, peerKind]);

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setAppliedSearch(search);
    setPage(1);
  };

  const handleChannelChange = (val: string) => {
    setChannelType(val === "all" ? "" : val);
    setPage(1);
  };

  const handlePeerKindChange = (val: string) => {
    setPeerKind(val === "all" ? "" : val);
    setPage(1);
  };

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleSelectAll = () => {
    if (selectedIds.size === contacts.length) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(contacts.map((c) => c.id)));
    }
  };

  const selectedContacts = contacts.filter((c) => selectedIds.has(c.id));
  const allSelectedMerged = selectedContacts.length > 0 && selectedContacts.every((c) => c.merged_id);

  const handleUnmerge = async () => {
    try {
      await unmerge(selectedContacts.map((c) => c.id));
      toast.success(t("merge.dialogTitle"), t("merge.unmergeSuccess"));
      setSelectedIds(new Set());
    } catch (err) {
      toast.error(t("merge.dialogTitle"), err instanceof Error ? err.message : t("merge.unmergeError"));
    }
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
            <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
          </Button>
        }
      />

      {/* Permissions note */}
      <PermissionsNote />

      {/* Filters */}
      <div className="mt-4 flex flex-wrap items-end gap-2">
        <form onSubmit={handleSearchSubmit} className="flex gap-2 flex-1 min-w-[200px] max-w-md">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t("searchPlaceholder")}
              className="pl-9"
            />
          </div>
          <Button type="submit" variant="outline">
            {t("filter")}
          </Button>
        </form>

        <Select value={channelType || "all"} onValueChange={handleChannelChange}>
          <SelectTrigger className="w-[160px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("filters.allChannels")}</SelectItem>
            {CHANNEL_TYPES.map((ct) => (
              <SelectItem key={ct} value={ct}>{ct}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={peerKind || "all"} onValueChange={handlePeerKindChange}>
          <SelectTrigger className="w-[140px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("filters.allTypes")}</SelectItem>
            <SelectItem value="direct">{t("filters.direct")}</SelectItem>
            <SelectItem value="group">{t("filters.group")}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {/* Selection toolbar — always rendered to avoid layout shift */}
      <div className="mt-3 flex items-center gap-2 rounded-md border px-3 py-2 transition-colors"
        style={{ visibility: selectedIds.size > 0 ? "visible" : "hidden" }}
      >
        <span className="text-sm font-medium">
          {t("selectedCount", { count: selectedIds.size })}
        </span>
        <div className="ml-auto flex gap-2">
          <Button size="sm" variant="default" className="gap-1" onClick={() => setMergeDialogOpen(true)}>
            <Merge className="h-3.5 w-3.5" /> {t("merge.button")}
          </Button>
          {allSelectedMerged && (
            <Button size="sm" variant="outline" className="gap-1" onClick={handleUnmerge}>
              <Unlink className="h-3.5 w-3.5" /> {t("merge.unmergeButton")}
            </Button>
          )}
        </div>
      </div>

      {/* Table */}
      <div className="mt-2">
        {showSkeleton ? (
          <TableSkeleton rows={8} />
        ) : contacts.length === 0 ? (
          <EmptyState
            icon={Contact}
            title={appliedSearch || channelType || peerKind ? t("noMatchTitle") : t("emptyTitle")}
            description={appliedSearch || channelType || peerKind ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[750px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="w-10 px-3 py-2.5">
                    <input
                      type="checkbox"
                      checked={contacts.length > 0 && selectedIds.size === contacts.length}
                      onChange={toggleSelectAll}
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
                    onClick={() => toggleSelect(c.id)}
                  >
                    <td className="px-3 py-2.5" onClick={(e) => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        checked={selectedIds.has(c.id)}
                        onChange={() => toggleSelect(c.id)}
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
                    <td className="px-3 py-2.5 font-mono text-xs">{c.sender_id}</td>
                    <td className="px-3 py-2.5">
                      <Badge variant="outline" className="text-[11px]">{c.channel_type}</Badge>
                    </td>
                    <td className="px-3 py-2.5">
                      {c.peer_kind && (
                        <Badge variant={c.peer_kind === "direct" ? "default" : "secondary"} className="text-[11px]">
                          {c.peer_kind === "direct" ? t("filters.direct") : t("filters.group")}
                        </Badge>
                      )}
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
              onPageChange={setPage}
              onPageSizeChange={(s) => { setPageSize(s); setPage(1); }}
            />
          </div>
        )}
      </div>

      {/* Merge dialog */}
      <MergeContactsDialog
        open={mergeDialogOpen}
        onOpenChange={setMergeDialogOpen}
        selectedContacts={selectedContacts}
        onSuccess={() => {
          setSelectedIds(new Set());
          refresh();
        }}
      />
    </div>
  );
}

function PermissionsNote() {
  const { t } = useTranslation("contacts");
  const [open, setOpen] = useState(true);
  const p = "permissionsNote";

  return (
    <div className="mt-4 rounded-md border border-blue-200 bg-blue-50/50 dark:border-blue-900 dark:bg-blue-950/30">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm"
      >
        <Info className="h-4 w-4 text-blue-500 shrink-0" />
        <span className="font-medium text-blue-700 dark:text-blue-400">{t(`${p}.title`)}</span>
        <ChevronDown className={`ml-auto h-4 w-4 text-blue-400 transition-transform ${open ? "rotate-180" : ""}`} />
      </button>
      {open && (
        <ul className="px-3 pb-3 space-y-1 text-xs text-muted-foreground">
          {PERM_CHANNELS.map((ch) => (
            <li key={ch} className={ch === "feishu" ? "text-amber-600 dark:text-amber-400 font-medium" : ""}>
              {t(`${p}.${ch}`)}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
