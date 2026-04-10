import { useRef, useEffect, useCallback } from "react";
import type Sigma from "sigma";
import type Graph from "graphology";

interface SigmaGraphMinimapProps {
  sigma: Sigma | null;
  graph: Graph;
  /** Minimap canvas size in pixels */
  size?: number;
}

/**
 * Lightweight canvas-overlay minimap.
 * Draws all nodes as small dots + a viewport rectangle showing the current camera view.
 * Click to jump, no drag (KISS).
 */
export function SigmaGraphMinimap({ sigma, graph, size = 140 }: SigmaGraphMinimapProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const rafRef = useRef<number>(0);

  // Compute graph bounding box from node positions
  const getBounds = useCallback(() => {
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    graph.forEachNode((_node, attrs) => {
      const x = attrs.x as number;
      const y = attrs.y as number;
      if (x < minX) minX = x;
      if (x > maxX) maxX = x;
      if (y < minY) minY = y;
      if (y > maxY) maxY = y;
    });
    // Add padding
    const padX = (maxX - minX) * 0.1 || 50;
    const padY = (maxY - minY) * 0.1 || 50;
    return { minX: minX - padX, minY: minY - padY, maxX: maxX + padX, maxY: maxY + padY };
  }, [graph]);

  // Draw minimap
  const draw = useCallback(() => {
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    if (!canvas || !ctx || !sigma || graph.order === 0) return;

    const dpr = window.devicePixelRatio || 1;
    canvas.width = size * dpr;
    canvas.height = size * dpr;
    ctx.scale(dpr, dpr);

    const bounds = getBounds();
    const bw = bounds.maxX - bounds.minX;
    const bh = bounds.maxY - bounds.minY;
    const scale = Math.min(size / bw, size / bh);
    const offsetX = (size - bw * scale) / 2;
    const offsetY = (size - bh * scale) / 2;

    // Clear
    ctx.clearRect(0, 0, size, size);

    // Background
    ctx.fillStyle = "rgba(0,0,0,0.03)";
    ctx.fillRect(0, 0, size, size);

    // Draw nodes as 1.5px dots
    graph.forEachNode((_node, attrs) => {
      const x = ((attrs.x as number) - bounds.minX) * scale + offsetX;
      const y = ((attrs.y as number) - bounds.minY) * scale + offsetY;
      ctx.beginPath();
      ctx.arc(x, y, 1.5, 0, Math.PI * 2);
      ctx.fillStyle = (attrs.color as string) || "#9ca3af";
      ctx.fill();
    });

    // Draw viewport rectangle — use viewportToGraph to derive the 4 corners
    // in graph space, then map to minimap space
    const { width, height } = sigma.getDimensions();
    const topLeft = sigma.viewportToGraph({ x: 0, y: 0 });
    const bottomRight = sigma.viewportToGraph({ x: width, y: height });

    const vx1 = (topLeft.x - bounds.minX) * scale + offsetX;
    const vy1 = (topLeft.y - bounds.minY) * scale + offsetY;
    const vx2 = (bottomRight.x - bounds.minX) * scale + offsetX;
    const vy2 = (bottomRight.y - bounds.minY) * scale + offsetY;
    const vw = vx2 - vx1;
    const vh = vy2 - vy1;

    ctx.strokeStyle = "rgba(59, 130, 246, 0.7)";
    ctx.lineWidth = 1.5;
    ctx.strokeRect(vx1, vy1, vw, vh);
    ctx.fillStyle = "rgba(59, 130, 246, 0.08)";
    ctx.fillRect(vx1, vy1, vw, vh);
  }, [sigma, graph, size, getBounds]);

  // Redraw on camera changes
  useEffect(() => {
    if (!sigma) return;
    const camera = sigma.getCamera();

    // Throttled redraw — skip if already scheduled
    let scheduled = false;
    const scheduleRedraw = () => {
      if (scheduled) return;
      scheduled = true;
      rafRef.current = requestAnimationFrame(() => {
        scheduled = false;
        draw();
      });
    };

    // Initial draw
    scheduleRedraw();

    // Redraw on camera updates (pan/zoom) AND after every sigma render
    // (so FA2 layout position updates are reflected)
    camera.on("updated", scheduleRedraw);
    sigma.on("afterRender", scheduleRedraw);

    return () => {
      camera.off("updated", scheduleRedraw);
      sigma.off("afterRender", scheduleRedraw);
      cancelAnimationFrame(rafRef.current);
    };
  }, [sigma, draw]);

  // Click to jump: find nearest node to click position and center camera on it
  const handleClick = useCallback(
    (e: React.MouseEvent<HTMLCanvasElement>) => {
      if (!sigma || graph.order === 0) return;
      const rect = canvasRef.current?.getBoundingClientRect();
      if (!rect) return;

      const bounds = getBounds();
      const bw = bounds.maxX - bounds.minX;
      const bh = bounds.maxY - bounds.minY;
      const scale = Math.min(size / bw, size / bh);
      const offsetX = (size - bw * scale) / 2;
      const offsetY = (size - bh * scale) / 2;

      // Convert click position to graph coordinates
      const clickX = e.clientX - rect.left;
      const clickY = e.clientY - rect.top;
      const targetGraphX = (clickX - offsetX) / scale + bounds.minX;
      const targetGraphY = (clickY - offsetY) / scale + bounds.minY;

      // Find nearest node (avoids coordinate-system issues with camera.animate)
      let nearestId: string | null = null;
      let minDist = Infinity;
      graph.forEachNode((id, attrs) => {
        const dx = (attrs.x as number) - targetGraphX;
        const dy = (attrs.y as number) - targetGraphY;
        const d = dx * dx + dy * dy;
        if (d < minDist) { minDist = d; nearestId = id; }
      });

      if (nearestId) {
        const display = sigma.getNodeDisplayData(nearestId);
        if (display) {
          sigma.getCamera().animate(
            { x: display.x, y: display.y },
            { duration: 300 },
          );
        }
      }
    },
    [sigma, graph, size, getBounds],
  );

  if (graph.order === 0) return null;

  return (
    <canvas
      ref={canvasRef}
      onClick={handleClick}
      className="rounded border bg-background/80 backdrop-blur-sm cursor-crosshair"
      style={{ width: size, height: size }}
    />
  );
}
