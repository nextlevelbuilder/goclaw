import { useState, useEffect, useRef, useLayoutEffect } from "react";
import { createPortal } from "react-dom";
import { Search, UserPlus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { useContactSearch } from "../../hooks/use-contact-search";
import type { ChannelContact } from "@/types/contact";

/** Format a contact into a human-readable snippet for insertion into context files. */
function formatContactSnippet(c: ChannelContact): string {
  const parts: string[] = [];
  if (c.display_name) parts.push(c.display_name);
  if (c.username) parts.push(`@${c.username}`);
  parts.push(`${c.channel_type}:${c.sender_id}`);
  return `- ${parts.join(" — ")}`;
}

interface ContactInsertSearchProps {
  onInsert: (text: string) => void;
}

export function ContactInsertSearch({ onInsert }: ContactInsertSearchProps) {
  const { t } = useTranslation("agents");
  const [search, setSearch] = useState("");
  const [open, setOpen] = useState(false);
  const { contacts } = useContactSearch(search);
  const containerRef = useRef<HTMLDivElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropdownStyle, setDropdownStyle] = useState<React.CSSProperties>({});

  // Compute dropdown position for portal rendering
  useLayoutEffect(() => {
    if (!open || !containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    setDropdownStyle({
      position: "fixed",
      top: rect.bottom + 4,
      left: rect.left,
      width: Math.min(rect.width, 384), // max-w-sm = 24rem = 384px
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

  const handleSelect = (c: ChannelContact) => {
    onInsert(formatContactSnippet(c));
    setSearch("");
    setOpen(false);
  };

  return (
    <div ref={containerRef} className="relative">
      <div className="relative">
        <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <input
          value={search}
          onChange={(e) => { setSearch(e.target.value); setOpen(true); }}
          onFocus={() => search.length >= 2 && setOpen(true)}
          placeholder={t("files.insertContact")}
          className="h-8 w-full max-w-sm rounded-md border bg-transparent pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
        />
      </div>
      {open && search.length >= 2 && contacts.length > 0 && createPortal(
        <div ref={dropdownRef} style={dropdownStyle} className="max-h-48 overflow-y-auto rounded-md border bg-popover p-1 shadow-md">
          {contacts.map((c) => (
            <button
              key={c.id}
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => handleSelect(c)}
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
      {open && search.length >= 2 && contacts.length === 0 && createPortal(
        <div ref={dropdownRef} style={dropdownStyle} className="rounded-md border bg-popover p-3 text-center text-xs text-muted-foreground shadow-md">
          {t("instances.noContactsFound")}
        </div>,
        document.body,
      )}
    </div>
  );
}
