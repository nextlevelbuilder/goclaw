import { useEffect, type RefObject } from "react";
import type Sigma from "sigma";
import type Graph from "graphology";

interface UseSigmaKeyboardOptions {
  sigma: Sigma | null;
  graph: Graph;
  /** Container element — shortcuts only fire when this element or its children are focused */
  containerRef: RefObject<HTMLElement | null>;
  selectedNodeId?: string | null;
  onNodeSelect?: (nodeId: string | null) => void;
  /** Ref to search input — "/" focuses it */
  searchInputRef?: RefObject<HTMLInputElement | null>;
}

/**
 * Keyboard shortcuts for Sigma graph views.
 * Only active when the graph container (or a child) is focused.
 *
 * | Key     | Action              |
 * |---------|---------------------|
 * | + / =   | Zoom in             |
 * | -       | Zoom out            |
 * | R       | Reset view (fit all)|
 * | F       | Focus selected node |
 * | Escape  | Deselect all        |
 * | /       | Focus search input  |
 */
export function useSigmaKeyboard({
  sigma,
  graph,
  containerRef,
  selectedNodeId,
  onNodeSelect,
  searchInputRef,
}: UseSigmaKeyboardOptions) {
  useEffect(() => {
    const container = containerRef.current;
    if (!container || !sigma) return;

    const handler = (e: KeyboardEvent) => {
      // Ignore if typing in an input/textarea
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;

      switch (e.key) {
        case "+":
        case "=":
          e.preventDefault();
          sigma.getCamera().animatedZoom({ duration: 200 });
          break;
        case "-":
          e.preventDefault();
          sigma.getCamera().animatedUnzoom({ duration: 200 });
          break;
        case "r":
        case "R":
          e.preventDefault();
          sigma.getCamera().animatedReset({ duration: 300 });
          break;
        case "f":
        case "F":
          if (selectedNodeId && graph.hasNode(selectedNodeId)) {
            e.preventDefault();
            const nodeDisplay = sigma.getNodeDisplayData(selectedNodeId);
            if (nodeDisplay) {
              const currentRatio = sigma.getCamera().ratio;
              sigma.getCamera().animate(
                { x: nodeDisplay.x, y: nodeDisplay.y, ratio: Math.min(currentRatio, 0.5) },
                { duration: 400 },
              );
            }
          }
          break;
        case "Escape":
          onNodeSelect?.(null);
          break;
        case "/":
          e.preventDefault();
          searchInputRef?.current?.focus();
          break;
      }
    };

    container.addEventListener("keydown", handler);
    return () => container.removeEventListener("keydown", handler);
  }, [sigma, graph, containerRef, selectedNodeId, onNodeSelect, searchInputRef]);
}
