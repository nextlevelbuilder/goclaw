import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, Contact, Info, Merge, RefreshCw, Search, Unlink } from "lucide-react";
import { useUiStore } from "@/stores/use-ui-store";
import { Button } from "@/components/ui/button";
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
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { toast } from "@/stores/use-toast-store";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useContacts } from "./hooks/use-contacts";
import { useContactMerge } from "./hooks/use-contact-merge";
import { MergeContactsDialog } from "./merge-contacts-dialog";
import { ContactsTable } from "./contacts-table";

const CHANNEL_TYPES = ["telegram", "discord", "slack", "whatsapp", "zalo_oa", "zalo_personal", "feishu"];
const PERM_CHANNELS = ["telegram", "discord", "zalo", "slack", "feishu"] as const;

export function ContactsPage() {
  const { t } = useTranslation("contacts");
  const { t: tc } = useTranslation("common");

  const globalPageSize = useUiStore((s) => s.pageSize);
  const setGlobalPageSize = useUiStore((s) => s.setPageSize);
  const [search, setSearch] = useState("");
  const [appliedSearch, setAppliedSearch] = useState("");
  const [channelType, setChannelType] = useState("");
  const [contactType, setContactType] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSizeRaw] = useState(globalPageSize);
  const setPageSize = (size: number) => { setPageSizeRaw(size); setPage(1); setGlobalPageSize(size); };

  // Selection state
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [mergeDialogOpen, setMergeDialogOpen] = useState(false);

  const { contacts, total, loading, fetching, refresh } = useContacts({
    search: appliedSearch || undefined,
    channelType: channelType || undefined,
    contactType: contactType || undefined,
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
  }, [page, pageSize, appliedSearch, channelType, contactType]);

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setAppliedSearch(search);
    setPage(1);
  };

  const handleChannelChange = (val: string) => {
    setChannelType(val === "all" ? "" : val);
    setPage(1);
  };

  const handleContactTypeChange = (val: string) => {
    setContactType(val === "all" ? "" : val);
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

        <Select value={contactType || "all"} onValueChange={handleContactTypeChange}>
          <SelectTrigger className="w-[140px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("filters.allTypes")}</SelectItem>
            <SelectItem value="user">{t("types.user")}</SelectItem>
            <SelectItem value="group">{t("types.group")}</SelectItem>
            <SelectItem value="topic">{t("types.topic", "Topic")}</SelectItem>
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
            title={appliedSearch || channelType || contactType ? t("noMatchTitle") : t("emptyTitle")}
            description={appliedSearch || channelType || contactType ? t("noMatchDescription") : t("emptyDescription")}
          />
        ) : (
          <ContactsTable
            contacts={contacts}
            selectedIds={selectedIds}
            total={total}
            page={page}
            pageSize={pageSize}
            totalPages={totalPages}
            onToggleSelect={toggleSelect}
            onToggleSelectAll={toggleSelectAll}
            onPageChange={setPage}
            onPageSizeChange={(s) => { setPageSize(s); setPage(1); }}
          />
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
