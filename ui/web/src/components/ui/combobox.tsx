import * as React from "react";
import { createPortal } from "react-dom";
import { ChevronDownIcon, CheckIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { usePortalDropdownClose } from "@/hooks/use-portal-dropdown-close";

export interface ComboboxOption {
  value: string;
  label?: string;
}

interface ComboboxProps {
  value: string;
  onChange: (value: string) => void;
  /** Fires only on dropdown item click or custom value commit (not on keystrokes).
   *  Use this for multi-select wrappers that need to distinguish typing from selection. */
  onSelect?: (value: string) => void;
  options: ComboboxOption[];
  placeholder?: string;
  className?: string;
  /** Render dropdown into a portal container (useful inside dialogs with overflow clipping). */
  portalContainer?: React.RefObject<HTMLElement | null>;
  /** Allow typing custom values not in the options list. Shows a hint in the dropdown. */
  allowCustom?: boolean;
  /** Label for the custom value hint (default: "Use custom:"). */
  customLabel?: string;
}

export function Combobox({
  value,
  onChange,
  onSelect,
  options,
  placeholder,
  className,
  portalContainer,
  allowCustom,
  customLabel = "Use custom:",
}: ComboboxProps) {
  const [open, setOpen] = React.useState(false);
  const [search, setSearch] = React.useState("");
  // Track whether user actively typed since last focus — when false, show all options
  const inputDirtyRef = React.useRef(false);
  const [inputDirty, setInputDirty] = React.useState(false);
  // After selection, hold the selected value to suppress sync effects until user types again.
  // Prevents label→UUID→label flash caused by options reloading after selection.
  const selectedValueRef = React.useRef<string | null>(null);
  const inputRef = React.useRef<HTMLInputElement>(null);
  const containerRef = React.useRef<HTMLDivElement>(null);
  const dropdownRef = React.useRef<HTMLDivElement>(null);
  const [dropdownStyle, setDropdownStyle] = React.useState<React.CSSProperties>({});

  // Sync search text when value changes externally — show label if available
  React.useEffect(() => {
    // After handleSelect, skip sync while value is still the selected value.
    // handleSelect already set the display text to the label.
    if (selectedValueRef.current !== null && selectedValueRef.current === value) return;
    selectedValueRef.current = null;
    const match = options.find((o) => o.value === value);
    setSearch(match?.label || value);
  }, [value, options]);

  // Close on outside interaction (pointer/touch-aware, ignores in-list scroll)
  usePortalDropdownClose({
    open,
    onClose: () => {
      setOpen(false);
      setInputDirty(false);
      inputDirtyRef.current = false;
    },
    ignore: [containerRef, dropdownRef],
  });

  // Resolve the actual portal target: explicit prop > closest dialog content > document.body
  const resolvedPortal = React.useMemo(() => {
    if (portalContainer?.current) return portalContainer.current;
    // Auto-detect if inside a Radix Dialog (which sets pointer-events:none on body)
    const el = containerRef.current?.closest<HTMLElement>('[data-slot="dialog-content"]');
    return el ?? null;
   
  }, [portalContainer, open]);

  // Compute dropdown position — flip above input when near viewport bottom.
  // When flipped up, use `bottom` anchoring so the dropdown grows upward
  // from the input edge (filtering reduces items without creating a gap).
  React.useLayoutEffect(() => {
    if (!open || !containerRef.current) return;
    const inputRect = containerRef.current.getBoundingClientRect();
    const DROP_H = 256; // max-h-60 ≈ 240px + border/padding
    const GAP = 4;
    const spaceBelow = window.innerHeight - inputRect.bottom;
    const flipUp = spaceBelow < DROP_H && inputRect.top > DROP_H;

    if (resolvedPortal) {
      const portalRect = resolvedPortal.getBoundingClientRect();
      const scrollTop = resolvedPortal.scrollTop || 0;
      const scrollLeft = resolvedPortal.scrollLeft || 0;
      const left = inputRect.left - portalRect.left + scrollLeft;
      const maxWidth = portalRect.width - (inputRect.left - portalRect.left);
      const portalRelativeTop = inputRect.top - portalRect.top + scrollTop;
      const portalFlipUp = spaceBelow < DROP_H && portalRelativeTop > DROP_H;
      if (portalFlipUp) {
        // Anchor bottom edge to input top — dropdown grows upward.
        const portalH = resolvedPortal.scrollHeight || portalRect.height;
        setDropdownStyle({
          position: "absolute",
          bottom: portalH - portalRelativeTop + GAP,
          left,
          width: inputRect.width,
          maxWidth,
          maxHeight: DROP_H,
          zIndex: 50,
        });
      } else {
        setDropdownStyle({
          position: "absolute",
          top: inputRect.bottom - portalRect.top + scrollTop + GAP,
          left,
          width: inputRect.width,
          maxWidth,
          zIndex: 50,
        });
      }
    } else if (flipUp) {
      // Fixed: anchor bottom edge to input top — dropdown grows upward.
      const bottomFromViewport = window.innerHeight - inputRect.top;
      setDropdownStyle({
        position: "fixed",
        bottom: bottomFromViewport + GAP,
        left: inputRect.left,
        width: inputRect.width,
        maxHeight: DROP_H,
        zIndex: 9999,
      });
    } else {
      setDropdownStyle({
        position: "fixed",
        top: inputRect.bottom + GAP,
        left: inputRect.left,
        width: inputRect.width,
        zIndex: 9999,
      });
    }
  }, [open, search, resolvedPortal]);

  // When not dirty (just focused), show all options. When dirty, filter by search.
  const filtered = React.useMemo(() => {
    if (!inputDirty || !search) return options;
    const q = search.toLowerCase();
    return options.filter(
      (o) =>
        o.value.toLowerCase().includes(q) ||
        (o.label && o.label.toLowerCase().includes(q)),
    );
  }, [options, search, inputDirty]);

  // Check if typed value is a custom value (not matching any option exactly)
  const isCustomValue = React.useMemo(() => {
    if (!search.trim()) return false;
    return !options.some(
      (o) => o.value === search || o.label === search,
    );
  }, [options, search]);

  const handleSelect = (val: string) => {
    selectedValueRef.current = val; // suppress value-sync until user types again
    onChange(val);
    onSelect?.(val);
    const match = options.find((o) => o.value === val);
    setSearch(match?.label || val);
    setOpen(false);
    setInputDirty(false);
    inputDirtyRef.current = false;
  };

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value;
    selectedValueRef.current = null; // user is typing — resume normal value sync
    setSearch(val);
    onChange(val);
    if (!inputDirtyRef.current) {
      inputDirtyRef.current = true;
      setInputDirty(true);
    }
    if (!open && options.length > 0) setOpen(true);
  };

  const handleFocus = (e: React.FocusEvent) => {
    // Only open dropdown on user-initiated focus (click/tab), not programmatic.
    // relatedTarget is null for programmatic focus or first tab into page.
    if (!e.relatedTarget && document.hasFocus()) return;
    inputDirtyRef.current = false;
    setInputDirty(false);
    if (options.length > 0) setOpen(true);
    requestAnimationFrame(() => inputRef.current?.select());
  };

  const showCustomHint = allowCustom && inputDirty && isCustomValue && search.trim();

  const dropdownContent = open && (filtered.length > 0 || showCustomHint) && (
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
      {showCustomHint && (
        <button
          type="button"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => handleSelect(search.trim())}
          className="hover:bg-accent hover:text-accent-foreground text-muted-foreground flex w-full cursor-pointer items-center rounded-sm py-1.5 pl-2 text-sm italic outline-hidden select-none"
        >
          {customLabel} <span className="text-foreground ml-1 font-medium not-italic">{search.trim()}</span>
        </button>
      )}
    </div>
  );

  return (
    <div ref={containerRef} className={cn("relative", className)}>
      <input
        ref={inputRef}
        value={search}
        onChange={handleInputChange}
        onFocus={handleFocus}
        placeholder={placeholder}
        className={cn(
          "border-input placeholder:text-muted-foreground dark:bg-input/30 h-9 w-full rounded-md border bg-transparent px-3 py-1 pr-8 text-base md:text-sm shadow-xs outline-none transition-[color,box-shadow]",
          "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-1",
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
        resolvedPortal ?? document.body,
      )}
    </div>
  );
}
