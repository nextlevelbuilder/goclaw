import type { SpanData } from "@/types/trace";

export interface SpanNode {
  span: SpanData;
  children: SpanNode[];
}

/** Build a parent→children tree from a flat span list. */
export function buildSpanTree(spans: SpanData[]): SpanNode[] {
  const map = new Map<string, SpanNode>();
  const roots: SpanNode[] = [];
  for (const span of spans) map.set(span.id, { span, children: [] });
  for (const span of spans) {
    const node = map.get(span.id)!;
    if (span.parent_span_id && map.has(span.parent_span_id)) {
      map.get(span.parent_span_id)!.children.push(node);
    } else {
      roots.push(node);
    }
  }
  return roots;
}
