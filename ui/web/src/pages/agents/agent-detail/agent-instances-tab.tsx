import { useState, useEffect, useMemo, useRef, useLayoutEffect } from "react";
import { createPortal } from "react-dom";
import { Save, Check, AlertCircle, Users, FileText, Search, UserPlus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Textarea } from "@/components/ui/textarea";
import { useContactResolver } from "@/hooks/use-contact-resolver";
import { useAgentInstances, type UserInstance } from "../hooks/use-agent-instances";
import { useContactSearch } from "../hooks/use-contact-search";

interface AgentInstancesTabProps {
  agentId: string;
}

export function AgentInstancesTab({ agentId }: AgentInstancesTabProps) {
  const { t } = useTranslation("agents");
  const { instances, loading, saving, getFiles, setFile } = useAgentInstances(agentId);
  const [selected, setSelected] = useState<string | null>(null);
  const [content, setContent] = useState("");
  const [originalContent, setOriginalContent] = useState("");
  const [loadingFiles, setLoadingFiles] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!selected) return;
    let cancelled = false;
    setLoadingFiles(true);
    setError(null);
    getFiles(selected).then((files) => {
      if (cancelled) return;
      const userFile = files.find((f) => f.file_name === "USER.md");
      const c = userFile?.content ?? "";
      setContent(c);
      setOriginalContent(c);
    }).catch((err) => {
      if (!cancelled) setError(err instanceof Error ? err.message : t("instances.loading"));
    }).finally(() => {
      if (!cancelled) setLoadingFiles(false);
    });
    return () => { cancelled = true; };
  }, [selected, getFiles, t]);

  const handleSave = async () => {
    if (!selected) return;
    setError(null);
    setSaved(false);
    try {
      await setFile(selected, "USER.md", content);
      setOriginalContent(content);
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("instances.saving"));
    }
  };

  const isDirty = content !== originalContent;

  // Resolve user_ids to contact names for instances without metadata
  const instanceUserIDs = useMemo(() => instances.map((i) => i.user_id), [instances]);
  const { resolve } = useContactResolver(instanceUserIDs);

  // Existing instance user_ids for deduplication
  const existingIDs = useMemo(() => new Set(instances.map((i) => i.user_id)), [instances]);

  const handleContactSelect = (senderID: string) => {
    setSelected(senderID);
  };

  if (loading) {
    return <div className="py-8 text-center text-sm text-muted-foreground">{t("instances.loadingInstances")}</div>;
  }

  return (
    <div className="flex gap-4" style={{ minHeight: 400 }}>
      {/* Instance list */}
      <div className="w-64 shrink-0 space-y-1 overflow-y-auto rounded-md border p-2">
        <ContactSearchBox
          existingIDs={existingIDs}
          onSelect={handleContactSelect}
        />
        {instances.length > 0 && (
          <div className="px-2 pb-1 pt-1 text-xs font-medium text-muted-foreground">
            {instances.length} instance{instances.length !== 1 ? "s" : ""}
          </div>
        )}
        {instances.length === 0 && (
          <div className="flex flex-col items-center gap-2 py-6 text-center">
            <Users className="h-6 w-6 text-muted-foreground/50" />
            <p className="text-xs text-muted-foreground">{t("instances.noInstances")}</p>
          </div>
        )}
        {instances.map((inst) => (
          <InstanceRow
            key={inst.user_id}
            instance={inst}
            isSelected={selected === inst.user_id}
            onClick={() => setSelected(inst.user_id)}
            resolve={resolve}
          />
        ))}
      </div>

      {/* Editor */}
      <div className="flex flex-1 flex-col gap-3">
        {!selected ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            {t("instances.selectInstance")}
          </div>
        ) : loadingFiles ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            {t("instances.loading")}
          </div>
        ) : (
          <>
            <div className="flex items-center gap-2">
              <FileText className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm font-medium">USER.md</span>
              <span className="text-xs text-muted-foreground">— {selected}</span>
            </div>
            <Textarea
              className="flex-1 font-mono text-sm"
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder="(empty)"
              style={{ minHeight: 300 }}
            />
            {error && (
              <div className="flex items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                <AlertCircle className="h-4 w-4 shrink-0" />
                {error}
              </div>
            )}
            <div className="flex items-center justify-end gap-2">
              {saved && (
                <span className="flex items-center gap-1 text-sm text-success">
                  <Check className="h-3.5 w-3.5" /> {t("instances.saved")}
                </span>
              )}
              <Button onClick={handleSave} disabled={saving || !isDirty} size="sm">
                {!saving && <Save className="h-4 w-4" />}
                {saving ? t("instances.saving") : t("instances.save")}
              </Button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

/** Inline contact search dropdown for adding new instances. */
function ContactSearchBox({ existingIDs, onSelect }: { existingIDs: Set<string>; onSelect: (id: string) => void }) {
  const { t } = useTranslation("agents");
  const [search, setSearch] = useState("");
  const [open, setOpen] = useState(false);
  const { contacts } = useContactSearch(search);
  const containerRef = useRef<HTMLDivElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropdownStyle, setDropdownStyle] = useState<React.CSSProperties>({});

  // Filter out contacts already in instances
  const filtered = contacts.filter((c) => !existingIDs.has(c.sender_id));

  // Compute dropdown position for portal rendering
  useLayoutEffect(() => {
    if (!open || !containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    setDropdownStyle({
      position: "fixed",
      top: rect.bottom + 4,
      left: rect.left,
      width: rect.width,
      zIndex: 9999,
    });
  }, [open, search]);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        containerRef.current && !containerRef.current.contains(target) &&
        (!dropdownRef.current || !dropdownRef.current.contains(target))
      ) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  return (
    <div ref={containerRef} className="relative px-1 pb-2">
      <div className="relative">
        <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <input
          value={search}
          onChange={(e) => { setSearch(e.target.value); setOpen(true); }}
          onFocus={() => search.length >= 2 && setOpen(true)}
          placeholder={t("instances.searchContacts")}
          className="h-8 w-full rounded-md border bg-transparent pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
        />
      </div>
      {open && search.length >= 2 && filtered.length > 0 && createPortal(
        <div ref={dropdownRef} style={dropdownStyle} className="max-h-48 overflow-y-auto rounded-md border bg-popover p-1 shadow-md">
          {filtered.map((c) => (
            <button
              key={c.id}
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => {
                onSelect(c.sender_id);
                setSearch("");
                setOpen(false);
              }}
              className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-accent hover:text-accent-foreground"
            >
              <UserPlus className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium">
                  {c.display_name || c.sender_id}
                </div>
                <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
                  {c.username && <span>@{c.username}</span>}
                  <Badge variant="outline" className="text-[9px] px-1 py-0">{c.channel_type}</Badge>
                </div>
              </div>
            </button>
          ))}
        </div>,
        document.body,
      )}
      {open && search.length >= 2 && filtered.length === 0 && contacts.length === 0 && createPortal(
        <div ref={dropdownRef} style={dropdownStyle} className="rounded-md border bg-popover p-3 text-center text-xs text-muted-foreground shadow-md">
          {t("instances.noContactsFound")}
        </div>,
        document.body,
      )}
    </div>
  );
}

function InstanceRow({ instance, isSelected, onClick, resolve }: { instance: UserInstance; isSelected: boolean; onClick: () => void; resolve: (id: string) => import("@/types/contact").ChannelContact | null }) {
  const lastSeen = instance.last_seen_at ? formatRelative(instance.last_seen_at) : null;
  const contact = resolve(instance.user_id);
  const displayName = instance.metadata?.display_name || instance.metadata?.chat_title || contact?.display_name || null;

  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors ${
        isSelected ? "bg-accent text-accent-foreground" : "hover:bg-muted/50"
      }`}
    >
      <span className="truncate text-xs font-medium">
        {displayName || instance.user_id}
      </span>
      {displayName && (
        <span className="truncate font-mono text-[10px] text-muted-foreground">{instance.user_id}</span>
      )}
      <div className="flex items-center gap-2">
        {instance.file_count > 0 && (
          <Badge variant="outline" className="text-[10px]">
            {instance.file_count} file{instance.file_count !== 1 ? "s" : ""}
          </Badge>
        )}
        {lastSeen && (
          <span className="text-[10px] text-muted-foreground">{lastSeen}</span>
        )}
      </div>
    </button>
  );
}

function formatRelative(iso: string): string {
  const d = new Date(iso);
  const now = Date.now();
  const diff = now - d.getTime();
  if (diff < 60_000) return "just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  if (diff < 604_800_000) return `${Math.floor(diff / 86_400_000)}d ago`;
  return d.toLocaleDateString();
}
