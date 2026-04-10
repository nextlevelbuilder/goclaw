import { useState, useEffect, useRef } from "react";
import { Keyboard, X } from "lucide-react";
import { Button } from "@/components/ui/button";

interface Shortcut {
  keys: string[];
  description: string;
}

const SHORTCUTS: Shortcut[] = [
  { keys: ["+"], description: "Zoom in" },
  { keys: ["-"], description: "Zoom out" },
  { keys: ["R"], description: "Reset view (fit all)" },
  { keys: ["F"], description: "Focus selected node" },
  { keys: ["Esc"], description: "Deselect" },
  { keys: ["/"], description: "Focus search" },
  { keys: ["?"], description: "Show this help" },
];

/**
 * Keyboard shortcuts help popover.
 * Triggered by `?` key or by clicking the keyboard icon.
 */
export function SigmaGraphKeyboardHelp() {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  // Global `?` key handler to open help (when not typing in input)
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      if (e.key === "?") {
        e.preventDefault();
        setOpen((prev) => !prev);
      } else if (e.key === "Escape" && open) {
        setOpen(false);
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open]);

  // Outside click to dismiss
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  return (
    <div ref={containerRef} className="relative">
      <Button
        variant="ghost"
        size="sm"
        className="h-7 w-7 p-0"
        onClick={() => setOpen((p) => !p)}
        aria-label="Keyboard shortcuts"
        title="Keyboard shortcuts (?)"
      >
        <Keyboard className="h-3.5 w-3.5" />
      </Button>

      {open && (
        <div
          role="dialog"
          aria-label="Keyboard shortcuts"
          className="absolute right-0 top-full z-50 mt-1 min-w-[240px] rounded-md border bg-popover p-3 shadow-lg"
        >
          <div className="flex items-center justify-between mb-2">
            <h4 className="text-xs font-medium">Keyboard shortcuts</h4>
            <button
              onClick={() => setOpen(false)}
              className="p-0.5 rounded hover:bg-accent"
              aria-label="Close shortcuts help"
            >
              <X className="h-3 w-3 text-muted-foreground" />
            </button>
          </div>
          <dl className="space-y-1.5">
            {SHORTCUTS.map(({ keys, description }) => (
              <div key={keys.join("+")} className="flex items-center justify-between gap-3 text-xs">
                <dt className="text-muted-foreground">{description}</dt>
                <dd className="flex gap-1">
                  {keys.map((k) => (
                    <kbd
                      key={k}
                      className="inline-flex items-center justify-center min-w-[22px] h-5 px-1.5 rounded border bg-muted text-2xs font-mono"
                    >
                      {k}
                    </kbd>
                  ))}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      )}
    </div>
  );
}
