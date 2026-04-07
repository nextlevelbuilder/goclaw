import { useRef, useMemo, useState, useEffect, useLayoutEffect, useCallback } from "react";
import ForceGraph2D, { type ForceGraphMethods } from "react-force-graph-2d";
import { useUiStore } from "@/stores/use-ui-store";
import { buildVaultGraphData, VAULT_TYPE_COLORS } from "@/adapters/vault-graph-adapter";
import type { VaultGraphNode, VaultGraphLink } from "@/adapters/vault-graph-adapter";
import type { VaultDocument, VaultLink } from "@/types/vault";

const NODE_R = 5;

interface Props {
  documents: VaultDocument[];
  links: VaultLink[];
  onNodeClick?: (doc: VaultDocument) => void;
}

export function VaultGraphView({ documents, links, onNodeClick }: Props) {
  const theme = useUiStore((s) => s.theme);
  const isDark = theme === "dark" || (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  const graphRef = useRef<ForceGraphMethods<VaultGraphNode, VaultGraphLink>>(undefined);
  const containerRef = useRef<HTMLDivElement>(null);
  const [dimensions, setDimensions] = useState({ width: 600, height: 400 });
  const [ready, setReady] = useState(false);

  const docMap = useMemo(() => new Map(documents.map((d) => [d.id, d])), [documents]);
  const graphData = useMemo(() => buildVaultGraphData(documents, links), [documents, links]);

  // Container sizing
  useLayoutEffect(() => {
    const el = containerRef.current;
    if (el && el.clientWidth > 0 && el.clientHeight > 0) {
      setDimensions({ width: el.clientWidth, height: el.clientHeight });
      setReady(true);
    }
  }, []);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => {
      if (!entry) return;
      const { width, height } = entry.contentRect;
      if (width > 0 && height > 0) {
        setDimensions({ width, height });
        setReady(true);
      }
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const handleNodeClick = useCallback(
    (node: VaultGraphNode) => {
      const doc = docMap.get(node.id);
      if (doc && onNodeClick) onNodeClick(doc);
    },
    [docMap, onNodeClick],
  );

  const nodeCanvasObject = useCallback(
    (node: VaultGraphNode, ctx: CanvasRenderingContext2D) => {
      const x = node.x ?? 0;
      const y = node.y ?? 0;
      const r = NODE_R + Math.min(node.degree, 10);

      // Circle
      ctx.beginPath();
      ctx.arc(x, y, r, 0, 2 * Math.PI);
      ctx.fillStyle = node.color;
      ctx.fill();

      // Label
      ctx.font = "3px sans-serif";
      ctx.textAlign = "center";
      ctx.textBaseline = "top";
      ctx.fillStyle = isDark ? "#e4e4e7" : "#27272a";
      ctx.fillText(node.title.slice(0, 20), x, y + r + 2);
    },
    [isDark],
  );

  return (
    <div className="space-y-2">
      {/* Legend */}
      <div className="flex flex-wrap gap-3 text-xs text-muted-foreground px-1">
        {Object.entries(VAULT_TYPE_COLORS).map(([type, color]) => (
          <span key={type} className="flex items-center gap-1">
            <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ backgroundColor: color }} />
            {type}
          </span>
        ))}
      </div>

      {/* Graph container */}
      <div ref={containerRef} className="h-[400px] w-full rounded-md border bg-muted/20">
        {ready && (
          <ForceGraph2D
            ref={graphRef}
            graphData={graphData}
            width={dimensions.width}
            height={dimensions.height}
            nodeCanvasObject={nodeCanvasObject}
            nodePointerAreaPaint={(node, color, ctx) => {
              const r = NODE_R + Math.min((node as VaultGraphNode).degree, 10);
              ctx.beginPath();
              ctx.arc(node.x ?? 0, node.y ?? 0, r, 0, 2 * Math.PI);
              ctx.fillStyle = color;
              ctx.fill();
            }}
            onNodeClick={handleNodeClick}
            linkColor={() => isDark ? "#52525b" : "#d4d4d8"}
            linkDirectionalArrowLength={4}
            linkDirectionalArrowRelPos={1}
            enableNodeDrag
            cooldownTicks={80}
          />
        )}
      </div>
    </div>
  );
}
