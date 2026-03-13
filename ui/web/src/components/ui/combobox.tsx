import * as React from "react";
import { createPortal } from "react-dom";
import { ChevronDownIcon, CheckIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export interface ComboboxOption {
  value: string;
  label?: string;
}

interface ComboboxProps {
  value: string;
  onChange: (value: string) => void;
  options: ComboboxOption[];
  placeholder?: string;
  className?: string;
  /** Render dropdown into a portal container (useful inside dialogs with overflow clipping). */
  portalContainer?: React.RefObject<HTMLElement | null>;
}

export function Combobox({
  value,
  onChange,
  options,
  placeholder,
  className,
  portalContainer,
}: ComboboxProps) {
  const [open, setOpen] = React.useState(false);
  const [search, setSearch] = React.useState("");
  const containerRef = React.useRef<HTMLDivElement>(null);
  const dropdownRef = React.useRef<HTMLDivElement>(null);
  const [dropdownStyle, setDropdownStyle] = React.useState<React.CSSProperties>({});

  // Sync search text when value changes externally — show label if available
  React.useEffect(() => {
    const match = options.find((o) => o.value === value);
    setSearch(match?.label || value);
  }, [value, options]);

  // Close on outside click
  React.useEffect(() => {
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

  // Compute dropdown position — always use fixed positioning for portal rendering
  React.useLayoutEffect(() => {
    if (!open || !containerRef.current) return;
    const inputRect = containerRef.current.getBoundingClientRect();
    if (portalContainer?.current) {
      const portalRect = portalContainer.current.getBoundingClientRect();
      const left = inputRect.left - portalRect.left;
      const maxWidth = portalRect.width - left;
      setDropdownStyle({
        position: "absolute",
        top: inputRect.bottom - portalRect.top + 4,
        left,
        width: inputRect.width,
        maxWidth,
        zIndex: 50,
      });
    } else {
      setDropdownStyle({
        position: "fixed",
        top: inputRect.bottom + 4,
        left: inputRect.left,
        width: inputRect.width,
        zIndex: 9999,
      });
    }
  }, [open, search, portalContainer]);

  const filtered = React.useMemo(() => {
    if (!search) return options;
    const q = search.toLowerCase();
    return options.filter(
      (o) =>
        o.value.toLowerCase().includes(q) ||
        (o.label && o.label.toLowerCase().includes(q)),
    );
  }, [options, search]);

  const handleSelect = (val: string) => {
    onChange(val);
    const match = options.find((o) => o.value === val);
    setSearch(match?.label || val);
    setOpen(false);
  };

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value;
    setSearch(val);
    onChange(val);
    if (!open && options.length > 0) setOpen(true);
  };

  const dropdownContent = open && filtered.length > 0 && (
    <div
      ref={dropdownRef}
      style={dropdownStyle}
      className="bg-popover text-popover-foreground pointer-events-auto max-h-60 overflow-y-auto rounded-md border p-1 shadow-md"
    >
      {filtered.map((o) => (
        <button
          key={o.value}
          type="button"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => handleSelect(o.value)}
          className="hover:bg-accent hover:text-accent-foreground relative flex w-full cursor-pointer items-center rounded-sm py-1.5 pr-8 pl-2 text-sm outline-hidden select-none"
        >
          <span className="truncate">{o.label || o.value}</span>
          {o.value === value && (
            <CheckIcon className="absolute right-2 size-4" />
          )}
        </button>
      ))}
    </div>
  );

  return (
    <div ref={containerRef} className={cn("relative", className)}>
      <input
        value={search}
        onChange={handleInputChange}
        onFocus={() => options.length > 0 && setOpen(true)}
        placeholder={placeholder}
        className={cn(
          "border-input placeholder:text-muted-foreground dark:bg-input/30 h-9 w-full rounded-md border bg-transparent px-3 py-1 pr-8 text-sm shadow-xs outline-none transition-[color,box-shadow]",
          "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]",
        )}
      />
      {options.length > 0 && (
        <ChevronDownIcon
          className="text-muted-foreground absolute top-1/2 right-2.5 size-4 -translate-y-1/2 cursor-pointer opacity-50"
          onClick={() => setOpen(!open)}
        />
      )}
      {dropdownContent && createPortal(
        dropdownContent,
        portalContainer?.current ?? document.body,
      )}
    </div>
  );
}
