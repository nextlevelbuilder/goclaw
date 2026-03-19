import { useMemo, useState, useRef, useEffect, useLayoutEffect } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { X, ChevronDownIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { useBuiltinTools } from "@/pages/builtin-tools/hooks/use-builtin-tools";
import { useCustomTools } from "@/pages/custom-tools/hooks/use-custom-tools";

interface ToolNameSelectProps {
  value: string[];
  onChange: (value: string[]) => void;
  placeholder?: string;
  className?: string;
}

interface ToolOption {
  name: string;
  displayName: string;
  group: "built-in" | "custom";
}

export function ToolNameSelect({
  value,
  onChange,
  placeholder,
  className,
}: ToolNameSelectProps) {
  const { t } = useTranslation("common");
  const { tools: builtinTools } = useBuiltinTools();
  const { tools: customTools } = useCustomTools();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropdownStyle, setDropdownStyle] = useState<React.CSSProperties>({});

  const allTools = useMemo<ToolOption[]>(() => {
    const builtin: ToolOption[] = builtinTools.map((t) => ({
      name: t.name,
      displayName: t.display_name || t.name,
      group: "built-in",
    }));
    const custom: ToolOption[] = customTools.map((t) => ({
      name: t.name,
      displayName: t.name,
      group: "custom",
    }));
    return [...builtin, ...custom];
  }, [builtinTools, customTools]);

  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    return allTools
      .filter((t) => !value.includes(t.name))
      .filter((t) => !q || t.name.toLowerCase().includes(q) || t.displayName.toLowerCase().includes(q));
  }, [allTools, value, search]);

  const grouped = useMemo(() => {
    const builtinGroup = filtered.filter((t) => t.group === "built-in");
    const customGroup = filtered.filter((t) => t.group === "custom");
    return { builtin: builtinGroup, custom: customGroup };
  }, [filtered]);

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

  const addTool = (name: string) => {
    if (!value.includes(name)) {
      onChange([...value, name]);
    }
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
      if (trimmed) {
        addTool(trimmed);
      }
    }
    if (e.key === "Backspace" && !search && value.length > 0) {
      removeTool(value[value.length - 1]!);
    }
  };

  return (
    <div ref={containerRef} className={cn("relative", className)}>
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
          onChange={(e) => {
            setSearch(e.target.value);
            if (!open) setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={handleKeyDown}
          placeholder={value.length === 0 ? (placeholder ?? t("selectOrTypeTools")) : ""}
          className="placeholder:text-muted-foreground min-w-[80px] flex-1 bg-transparent py-0.5 text-base md:text-sm outline-none"
        />
        <ChevronDownIcon
          className="text-muted-foreground size-4 shrink-0 cursor-pointer opacity-50"
          onClick={() => setOpen(!open)}
        />
      </div>
      {open && (grouped.builtin.length > 0 || grouped.custom.length > 0) && createPortal(
        <div
          ref={dropdownRef}
          style={dropdownStyle}
          className="bg-popover text-popover-foreground pointer-events-auto max-h-60 overflow-y-auto rounded-md border p-1 shadow-md"
        >
          {grouped.builtin.length > 0 && (
            <>
              <div className="text-muted-foreground px-2 py-1 text-[10px] font-semibold uppercase tracking-wider">
                {t("builtinTools")}
              </div>
              {grouped.builtin.map((t) => (
                <button
                  key={t.name}
                  type="button"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => addTool(t.name)}
                  className="hover:bg-accent hover:text-accent-foreground flex w-full cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none"
                >
                  <span className="truncate">{t.displayName}</span>
                  <code className="text-muted-foreground text-[10px]">{t.name}</code>
                </button>
              ))}
            </>
          )}
          {grouped.custom.length > 0 && (
            <>
              <div className="text-muted-foreground mt-1 px-2 py-1 text-[10px] font-semibold uppercase tracking-wider">
                {t("customTools")}
              </div>
              {grouped.custom.map((t) => (
                <button
                  key={t.name}
                  type="button"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => addTool(t.name)}
                  className="hover:bg-accent hover:text-accent-foreground flex w-full cursor-pointer items-center rounded-sm px-2 py-1.5 text-sm outline-hidden select-none"
                >
                  <span className="truncate">{t.name}</span>
                </button>
              ))}
            </>
          )}
        </div>,
        document.body,
      )}
    </div>
  );
}
