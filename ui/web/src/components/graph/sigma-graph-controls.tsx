import { useCallback, useEffect, useState } from "react";
import type Sigma from "sigma";
import { ZoomIn, ZoomOut, Maximize2 } from "lucide-react";
import { Button } from "@/components/ui/button";

const NODE_LIMIT_OPTIONS = [100, 200, 500, 1000, 5000] as const;

interface SigmaGraphControlsProps {
  sigma: Sigma | null;
  nodeLimit: number;
  isLimited: boolean;
  onNodeLimitChange: (limit: number) => void;
  /** i18n labels — caller provides pre-formatted translated strings */
  labels: {
    nodes: string;
    edges: string;
    limitNote?: string;
  };
}

export function SigmaGraphControls({
  sigma,
  nodeLimit,
  isLimited,
  onNodeLimitChange,
  labels,
}: SigmaGraphControlsProps) {
  const [zoomPercent, setZoomPercent] = useState(100);

  // Track zoom level from camera events
  useEffect(() => {
    if (!sigma) return;
    const camera = sigma.getCamera();
    const update = () => {
      // ratio < 1 = zoomed in, ratio > 1 = zoomed out. Invert for display.
      setZoomPercent(Math.round((1 / camera.ratio) * 100));
    };
    update();
    camera.on("updated", update);
    return () => { camera.off("updated", update); };
  }, [sigma]);

  const handleZoomIn = useCallback(() => {
    sigma?.getCamera().animatedZoom({ duration: 200 });
  }, [sigma]);

  const handleZoomOut = useCallback(() => {
    sigma?.getCamera().animatedUnzoom({ duration: 200 });
  }, [sigma]);

  const handleFitToView = useCallback(() => {
    sigma?.getCamera().animatedReset({ duration: 300 });
  }, [sigma]);

  return (
    <div className="flex items-center gap-3 px-3 py-1.5 border-t text-2xs text-muted-foreground shrink-0">
      <span>{labels.nodes}</span>
      <span>{labels.edges}</span>
      {isLimited && labels.limitNote && (
        <span>· {labels.limitNote}</span>
      )}
      <div className="flex-1" />
      <div className="flex items-center gap-1">
        <Button variant="ghost" size="sm" className="h-6 px-1.5" onClick={handleZoomOut} aria-label="Zoom out">
          <ZoomOut className="h-3 w-3" />
        </Button>
        <span className="w-9 text-center" aria-live="polite">{zoomPercent}%</span>
        <Button variant="ghost" size="sm" className="h-6 px-1.5" onClick={handleZoomIn} aria-label="Zoom in">
          <ZoomIn className="h-3 w-3" />
        </Button>
      </div>
      <select
        value={nodeLimit}
        onChange={(e) => onNodeLimitChange(Number(e.target.value))}
        className="h-5 rounded border bg-background px-1 text-base md:text-2xs"
        aria-label="Node limit"
      >
        {NODE_LIMIT_OPTIONS.map((n) => (
          <option key={n} value={n}>{n} nodes</option>
        ))}
      </select>
      <Button variant="ghost" size="sm" className="h-6 px-1.5" onClick={handleFitToView} aria-label="Fit to view">
        <Maximize2 className="h-3 w-3" />
      </Button>
    </div>
  );
}
