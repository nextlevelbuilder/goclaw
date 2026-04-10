/** Degree-based node sizing: 4-12px range. Logarithmic scale for dense hubs. */
export function getNodeSize(degree: number): number {
  if (degree === 0) return 4;
  return 4 + Math.min(Math.log2(degree + 1) * 2, 8);
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

/** Sigma settings constants for consistent look across both graph views */
export const SIGMA_SETTINGS = {
  /** Labels hidden for nodes smaller than this on screen */
  labelRenderedSizeThreshold: 8,
  /** Lower = fewer labels shown (avoids overlap) */
  labelDensity: 0.12,
  /** Grid cell size for label collision avoidance */
  labelGridCellSize: 120,
  /** Default edge color (overridden per-theme) */
  defaultEdgeColor: "#334155",
  /** Minimum camera ratio (most zoomed in) */
  minCameraRatio: 0.02,
  /** Maximum camera ratio (most zoomed out) */
  maxCameraRatio: 8,
} as const;
