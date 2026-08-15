import { useState, useMemo, useRef, useLayoutEffect } from "react";
import { createPortal } from "react-dom";
import { X, ChevronDownIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { usePortalDropdownClose } from "@/hooks/use-portal-dropdown-close";
import type { MCPToolInfo } from "./hooks/use-mcp";

interface ToolMultiSelectProps {
  value: string[];
  onChange: (value: string[]) => void;
  options: MCPToolInfo[];
  placeholder?: string;
  portalContainer?: React.RefObject<HTMLDivElement | null>;
}

/** Multi-select input for MCP tool names with dropdown + free-text/keyboard support. */
export function ToolMultiSelect({
  value,
  onChange,
  options,
  placeholder = "Select or type tool names...",
  portalContainer,
}: ToolMultiSelectProps) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    return options
      .filter((t) => !value.includes(t.name))
      .filter((t) => !q || t.name.toLowerCase().includes(q) || (t.description ?? "").toLowerCase().includes(q));
  }, [options, value, search]);

  usePortalDropdownClose({
    open,
    onClose: () => setOpen(false),
    // portalContainer is a whole modal container wrapping the dropdown; treat as inside.
    ignore: [containerRef, ...(portalContainer ? [portalContainer] : [])],
  });

  const addTool = (name: string) => {
    if (!value.includes(name)) onChange([...value, name]);
    setSearch("");
    inputRef.current?.focus();
  };

  const removeTool = (name: string) => {
    onChange(value.filter((v) => v !== name));
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      const trimmed = search.trim().replace(/,$/, "");
      if (trimmed) addTool(trimmed);
    }
    if (e.key === "Backspace" && !search && value.length > 0) {
      removeTool(value[value.length - 1]!);
    }
  };

  // Portal dropdown: position relative to portal container (fixed avoids Radix Dialog transform).
  const [dropdownStyle, setDropdownStyle] = useState<React.CSSProperties>({});
  useLayoutEffect(() => {
    if (!open || filtered.length === 0 || !containerRef.current || !portalContainer?.current) return;
    const inputRect = containerRef.current.getBoundingClientRect();
    const portalRect = portalContainer.current.getBoundingClientRect();
    setDropdownStyle({
      position: "absolute",
      top: inputRect.bottom - portalRect.top + 4,
      left: inputRect.left - portalRect.left,
      width: inputRect.width,
      zIndex: 50,
    });
  }, [open, filtered.length, search, value, portalContainer]);

  return (
    <div ref={containerRef} className="relative">
      <div
        className={cn(
          "border-input dark:bg-input/30 flex min-h-9 flex-wrap items-center gap-1 rounded-md border bg-transparent px-2 py-1 text-sm shadow-xs transition-[color,box-shadow]",
          "focus-within:border-ring focus-within:ring-ring/50 focus-within:ring-2",
        )}
        onClick={() => inputRef.current?.focus()}
      >
        {value.map((name) => (
          <span
            key={name}
            className="bg-secondary text-secondary-foreground inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-xs"
          >
            {name}
            <button
              type="button"
              className="hover:text-destructive ml-0.5"
              onClick={(e) => { e.stopPropagation(); removeTool(name); }}
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
        <input
          ref={inputRef}
          value={search}
          onChange={(e) => { setSearch(e.target.value); if (!open) setOpen(true); }}
          onFocus={() => setOpen(true)}
          onKeyDown={handleKeyDown}
          placeholder={value.length === 0 ? placeholder : ""}
          className="placeholder:text-muted-foreground min-w-[80px] flex-1 bg-transparent py-0.5 text-base md:text-sm outline-none"
        />
        <ChevronDownIcon
          className="text-muted-foreground size-4 shrink-0 cursor-pointer opacity-50"
          onClick={() => setOpen(!open)}
        />
      </div>
      {open && filtered.length > 0 && portalContainer?.current && createPortal(
        <div
          style={dropdownStyle}
          className="bg-popover text-popover-foreground max-h-56 overflow-y-auto rounded-md border p-1 shadow-md pointer-events-auto"
        >
          {filtered.map((t) => (
            <button
              key={t.name}
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => addTool(t.name)}
              className="hover:bg-accent hover:text-accent-foreground flex w-full cursor-pointer flex-col items-start rounded-sm px-2 py-1.5 outline-hidden select-none"
            >
              <span className="truncate font-mono text-xs">{t.name}</span>
              {t.description && (
                <span className="text-muted-foreground truncate text-xs-plus w-full text-left">
                  {t.description}
                </span>
              )}
            </button>
          ))}
        </div>,
        portalContainer.current,
      )}
    </div>
  );
}
