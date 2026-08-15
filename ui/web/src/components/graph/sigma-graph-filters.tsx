import { useCallback, useMemo, useEffect, useRef } from "react";
import type Graph from "graphology";
import { Filter, ChevronDown, ChevronUp } from "lucide-react";
import { Button } from "@/components/ui/button";

interface TypeCount {
  type: string;
  color: string;
  count: number;
}

interface SigmaGraphFiltersProps {
  graph: Graph;
  /** Map of type → color for the legend dots */
  typeColors: Record<string, string>;
  defaultColor?: string;
  /** Currently hidden types (controlled state) */
  hiddenTypes: Set<string>;
  onHiddenTypesChange: (types: Set<string>) => void;
  collapsed: boolean;
  onCollapsedChange: (collapsed: boolean) => void;
}

export function SigmaGraphFilters({
  graph, typeColors, defaultColor = "#9ca3af",
  hiddenTypes, onHiddenTypesChange,
  collapsed, onCollapsedChange,
}: SigmaGraphFiltersProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  // Memoize type counts to avoid iterating all nodes on every render
  const typeCounts = useMemo<TypeCount[]>(() => {
    if (graph.order === 0) return [];
    const counts = new Map<string, number>();
    graph.forEachNode((_node, attrs) => {
      const t = (attrs.docType || attrs.entityType || "other") as string;
      counts.set(t, (counts.get(t) ?? 0) + 1);
    });
    const result: TypeCount[] = [];
    for (const [type, count] of counts) {
      result.push({ type, color: typeColors[type] ?? defaultColor, count });
    }
    return result.sort((a, b) => b.count - a.count);
  }, [graph, typeColors, defaultColor]);

  const toggleType = useCallback((type: string) => {
    const next = new Set(hiddenTypes);
    if (next.has(type)) next.delete(type);
    else next.add(type);
    onHiddenTypesChange(next);
  }, [hiddenTypes, onHiddenTypesChange]);

  // Close dropdown on outside click OR Escape key
  useEffect(() => {
    if (collapsed) return;
    const handleClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        onCollapsedChange(true);
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onCollapsedChange(true);
      }
    };
    document.addEventListener("mousedown", handleClick);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handleClick);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [collapsed, onCollapsedChange]);

  if (typeCounts.length === 0) return null;

  return (
    <div ref={containerRef} className="relative flex flex-col">
      <Button
        variant="ghost"
        size="sm"
        className="h-7 gap-1 text-xs text-muted-foreground"
        onClick={() => onCollapsedChange(!collapsed)}
        aria-label={`Filter by type${hiddenTypes.size > 0 ? ` (${hiddenTypes.size} hidden)` : ""}`}
        aria-expanded={!collapsed}
        aria-haspopup="menu"
      >
        <Filter className="h-3 w-3" />
        Filter
        {hiddenTypes.size > 0 && (
          <span className="ml-1 text-2xs rounded-full bg-primary/10 text-primary px-1.5">
            {hiddenTypes.size} hidden
          </span>
        )}
        {collapsed ? <ChevronDown className="h-3 w-3" /> : <ChevronUp className="h-3 w-3" />}
      </Button>

      {!collapsed && (
        <div className="absolute right-0 top-full z-50 mt-1 flex flex-col gap-0.5 rounded-md border bg-popover p-1 shadow-md min-w-[160px]">
          {typeCounts.map(({ type, color, count }) => {
            const hidden = hiddenTypes.has(type);
            return (
              <button
                key={type}
                onClick={() => toggleType(type)}
                className={`flex items-center gap-2 px-2 py-1 rounded text-xs hover:bg-accent transition-opacity ${
                  hidden ? "opacity-40" : ""
                }`}
              >
                <span
                  className="inline-block h-2.5 w-2.5 shrink-0 rounded-full transition-opacity"
                  style={{ backgroundColor: color, opacity: hidden ? 0.3 : 1 }}
                />
                <span className="flex-1 text-left truncate">{type}</span>
                <span className="text-2xs text-muted-foreground">{count}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
