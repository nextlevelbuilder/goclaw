import louvain from "graphology-communities-louvain";
import type Graph from "graphology";

/** 16-color palette for community detection — high contrast, distinct hues. */
const COMMUNITY_PALETTE = [
  "#e6194B", "#3cb44b", "#4363d8", "#f58231",
  "#42d4f4", "#f032e6", "#bfef45", "#fabed4",
  "#469990", "#dcbeff", "#9A6324", "#ffe119",
  "#800000", "#aaffc3", "#808000", "#000075",
] as const;

/** Run Louvain community detection and assign `community` + `color` attrs.
 *  Call AFTER graph is built (nodes + edges added). */
export function assignCommunityColors(graph: Graph): void {
  if (graph.order === 0) return;
  const communities = louvain(graph, { resolution: 1.0 });
  graph.forEachNode((node) => {
    const c = communities[node] ?? 0;
    graph.setNodeAttribute(node, "community", c);
    graph.setNodeAttribute(node, "color", COMMUNITY_PALETTE[c % COMMUNITY_PALETTE.length]);
  });
}

/** Get community palette color by index (for legend). */
export function getCommunityColor(idx: number): string {
  return COMMUNITY_PALETTE[idx % COMMUNITY_PALETTE.length]!;
}

/** Degree-based node sizing, scaled by graph density.
 *  Larger graphs → smaller nodes to avoid overlap. */
export function getNodeSize(degree: number, nodeCount = 200): number {
  const s = nodeCount < 100 ? 1.0 : nodeCount < 500 ? 0.6 : nodeCount < 2000 ? 0.4 : 0.3;
  const base = 3 * s;
  if (degree === 0) return base;
  return base + Math.min(Math.log2(degree + 1) * 1.2 * s, 5 * s);
}

/**
 * Truncate a long string by removing the middle and inserting an ellipsis.
 * Example: truncateMiddle("Model Steering System Benchmark", 20) → "Model Ster…hmark"
 */
export function truncateMiddle(str: string, maxLength = 28): string {
  if (!str || str.length <= maxLength) return str;
  const keepStart = Math.ceil((maxLength - 1) * 0.6);
  const keepEnd = Math.floor((maxLength - 1) * 0.4);
  return `${str.slice(0, keepStart)}…${str.slice(-keepEnd)}`;
}

/** Adaptive FA2 worker config based on graph size and orphan ratio.
 *  Key fix: NO strongGravityMode, NO linLogMode — eliminates ring artifacts. */
export function getFA2WorkerSettings(nodeCount: number, orphanRatio: number) {
  // Scale up repulsion for larger graphs to prevent dense blobs
  const baseScaling = nodeCount < 200 ? 5 : nodeCount < 1000 ? 10 : 20;
  return {
    settings: {
      linLogMode: false,
      outboundAttractionDistribution: true,
      gravity: 0.5 + orphanRatio * 2.0,
      scalingRatio: baseScaling + (1 - orphanRatio) * 5,
      strongGravityMode: false,
      slowDown: 5,
      barnesHutOptimize: nodeCount > 50,
      barnesHutTheta: 0.5,
      edgeWeightInfluence: 0,
      adjustSizes: true,
    },
    durationMs: nodeCount < 200 ? 2000 :
                nodeCount < 1000 ? 3500 :
                nodeCount < 5000 ? 5000 : 8000,
  };
}

/** Semantic zoom tiers — cameraRatio thresholds (lower = more zoomed in).
 *  Zoom % ≈ 100/ratio. ratio 1.0=100%, 0.5=200%, 0.25=400%. */
export const ZOOM_TIERS = {
  /** Fully zoomed out: nodes only, no edges, no labels */
  FAR: 0.6,
  /** Intermediate: hub edges only */
  MID: 0.3,
  /** Close: most edges visible */
  NEAR: 0.12,
} as const;

/** Minimum degree for nodes to remain visible per tier */
export const TIER_MIN_DEGREE = {
  FAR: 2,
  MID: 1,
  NEAR: 0,
} as const;

/** Minimum endpoint degree for edges per tier.
 *  Edges hidden when EITHER endpoint below threshold. */
export const TIER_EDGE_DEGREE = {
  FAR: Infinity,    // no edges at default view
  MID: 6,           // only hub-to-hub
  NEAR: 2,          // most edges
} as const;

/** Sigma settings constants for consistent look across both graph views */
export const SIGMA_SETTINGS = {
  /** Labels hidden for nodes smaller than this on screen */
  labelRenderedSizeThreshold: 14,
  /** Lower = fewer labels shown (avoids overlap) */
  labelDensity: 0.04,
  /** Grid cell size for label collision avoidance */
  labelGridCellSize: 200,
  /** Default edge color (overridden per-theme) */
  defaultEdgeColor: "#334155",
  /** Minimum camera ratio (most zoomed in) */
  minCameraRatio: 0.02,
  /** Maximum camera ratio (most zoomed out) */
  maxCameraRatio: 8,
} as const;
