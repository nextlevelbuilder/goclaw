import { useMemo, useState, useRef, useEffect, useLayoutEffect } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { X, ChevronDownIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { useSkills } from "@/pages/skills/hooks/use-skills";

interface SkillNameSelectProps {
  value: string[];
  onChange: (value: string[]) => void;
  placeholder?: string;
  className?: string;
}

export function SkillNameSelect({
  value,
  onChange,
  placeholder,
  className,
}: SkillNameSelectProps) {
  const { t } = useTranslation("common");
  const { skills } = useSkills();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropdownStyle, setDropdownStyle] = useState<React.CSSProperties>({});

  const allSkills = useMemo(() => {
    return skills.map((s) => ({ name: s.name, description: s.description }));
  }, [skills]);

  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    return allSkills
      .filter((s) => !value.includes(s.name))
      .filter((s) => !q || s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q));
  }, [allSkills, value, search]);

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

  const addSkill = (name: string) => {
    if (!value.includes(name)) {
      onChange([...value, name]);
    }
    setSearch("");
    inputRef.current?.focus();
  };

  const removeSkill = (name: string) => {
    onChange(value.filter((v) => v !== name));
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      const trimmed = search.trim().replace(/,$/, "");
      if (trimmed) addSkill(trimmed);
    }
    if (e.key === "Backspace" && !search && value.length > 0) {
      removeSkill(value[value.length - 1]!);
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
              onClick={(e) => { e.stopPropagation(); removeSkill(name); }}
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
          placeholder={value.length === 0 ? (placeholder ?? t("selectOrTypeSkills")) : ""}
          className="placeholder:text-muted-foreground min-w-[80px] flex-1 bg-transparent py-0.5 text-base md:text-sm outline-none"
        />
        <ChevronDownIcon
          className="text-muted-foreground size-4 shrink-0 cursor-pointer opacity-50"
          onClick={() => setOpen(!open)}
        />
      </div>
      {open && filtered.length > 0 && createPortal(
        <div
          ref={dropdownRef}
          style={dropdownStyle}
          className="bg-popover text-popover-foreground pointer-events-auto max-h-60 overflow-y-auto rounded-md border p-1 shadow-md"
        >
          {filtered.map((s) => (
            <button
              key={s.name}
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => addSkill(s.name)}
              className="hover:bg-accent hover:text-accent-foreground flex w-full cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none"
            >
              <span className="shrink-0 font-medium">{s.name}</span>
              {s.description && (
                <span className="text-muted-foreground truncate text-xs">{s.description}</span>
              )}
            </button>
          ))}
        </div>,
        document.body,
      )}
    </div>
  );
}
