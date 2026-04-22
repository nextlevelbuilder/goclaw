import { useMemo, useState, useRef, useEffect, useLayoutEffect } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronDownIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { useBuiltinTools } from "@/pages/builtin-tools/hooks/use-builtin-tools";

interface ToolSingleComboboxProps {
  value: string;
  onChange: (value: string) => void;
  /** Fires only when selecting from dropdown (not on typing) */
  onToolSelect?: (toolName: string) => void;
  placeholder?: string;
  className?: string;
}

interface ToolOption {
  name: string;
  displayName: string;
}

export function ToolSingleCombobox({
  value,
  onChange,
  onToolSelect,
  placeholder,
  className,
}: ToolSingleComboboxProps) {
  const { t } = useTranslation("common");
  const { tools: builtinTools } = useBuiltinTools();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropdownStyle, setDropdownStyle] = useState<React.CSSProperties>({});

  // When value changes externally, sync the search field
  const displayValue = open ? search : value;

  const allTools = useMemo<ToolOption[]>(() => {
    return builtinTools.map((t) => ({
      name: t.name,
      displayName: t.display_name || t.name,
    }));
  }, [builtinTools]);

  const filtered = useMemo(() => {
    const q = (open ? search : "").toLowerCase();
    return allTools.filter(
      (t) => !q || t.name.toLowerCase().includes(q) || t.displayName.toLowerCase().includes(q),
    );
  }, [allTools, search, open]);

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
  }, [open]);

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

  const selectTool = (name: string) => {
    onChange(name);
    onToolSelect?.(name);
    setSearch("");
    setOpen(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      const trimmed = search.trim();
      if (trimmed) {
        onChange(trimmed);
        // Check if it matches a known tool for template fill
        const match = allTools.find((t) => t.name === trimmed);
        if (match) onToolSelect?.(trimmed);
      }
      setSearch("");
      setOpen(false);
    }
  };

  const handleFocus = () => {
    setSearch(value);
    setOpen(true);
  };

  const handleBlur = () => {
    // Commit typed value on blur if changed
    if (search.trim() && search.trim() !== value) {
      onChange(search.trim());
    }
  };

  return (
    <div ref={containerRef} className={cn("relative", className)}>
      <div
        className={cn(
          "border-input dark:bg-input/30 flex min-h-9 items-center gap-1 rounded-md border bg-transparent px-2 py-1 text-sm shadow-xs transition-[color,box-shadow]",
          "focus-within:border-ring focus-within:ring-ring/50 focus-within:ring-2",
        )}
        onClick={() => inputRef.current?.focus()}
      >
        <input
          ref={inputRef}
          value={displayValue}
          onChange={(e) => {
            setSearch(e.target.value);
            if (!open) setOpen(true);
          }}
          onFocus={handleFocus}
          onBlur={handleBlur}
          onKeyDown={handleKeyDown}
          placeholder={placeholder ?? t("selectOrTypeTools")}
          className="placeholder:text-muted-foreground min-w-[80px] flex-1 bg-transparent py-0.5 text-base md:text-sm font-mono outline-none"
        />
        <ChevronDownIcon
          className="text-muted-foreground size-4 shrink-0 cursor-pointer opacity-50"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => {
            if (open) {
              setOpen(false);
            } else {
              setSearch(value);
              setOpen(true);
              inputRef.current?.focus();
            }
          }}
        />
      </div>
      {open && filtered.length > 0 && createPortal(
        <div
          ref={dropdownRef}
          style={dropdownStyle}
          className="bg-popover text-popover-foreground pointer-events-auto max-h-60 overflow-y-auto rounded-md border p-1 shadow-md"
        >
          <div className="text-muted-foreground px-2 py-1 text-2xs font-semibold uppercase tracking-wider">
            {t("builtinTools")}
          </div>
          {filtered.map((tool) => (
            <button
              key={tool.name}
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => selectTool(tool.name)}
              className={cn(
                "hover:bg-accent hover:text-accent-foreground flex w-full cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none",
                tool.name === value && "bg-accent/50",
              )}
            >
              <span className="truncate">{tool.displayName}</span>
              <code className="text-muted-foreground text-2xs">{tool.name}</code>
            </button>
          ))}
        </div>,
        document.body,
      )}
    </div>
  );
}
