import { useState, useEffect, useMemo } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { ChevronRight, ChevronDown, Copy, Check } from "lucide-react";
import { useClipboard } from "@/hooks/use-clipboard";
import { formatDate, formatDuration, formatTokens, computeDurationMs } from "@/lib/format";
import type { TraceData, SpanData } from "./hooks/use-traces";

interface SpanNode {
  span: SpanData;
  children: SpanNode[];
}

function buildSpanTree(spans: SpanData[]): SpanNode[] {
  const map = new Map<string, SpanNode>();
  const roots: SpanNode[] = [];

  // Create nodes
  for (const span of spans) {
    map.set(span.id, { span, children: [] });
  }

  // Link parent → children
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

interface TraceDetailDialogProps {
  traceId: string;
  onClose: () => void;
  getTrace: (id: string) => Promise<{ trace: TraceData; spans: SpanData[] } | null>;
  onNavigateTrace?: (traceId: string) => void;
}

export function TraceDetailDialog({ traceId, onClose, getTrace, onNavigateTrace }: TraceDetailDialogProps) {
  const [trace, setTrace] = useState<TraceData | null>(null);
  const [spans, setSpans] = useState<SpanData[]>([]);
  const [loading, setLoading] = useState(true);
  const { copied, copy } = useClipboard();

  useEffect(() => {
    setLoading(true);
    getTrace(traceId)
      .then((result) => {
        if (result) {
          setTrace(result.trace);
          setSpans(result.spans ?? []);
        }
      })
      .finally(() => setLoading(false));
  }, [traceId, getTrace]);

  const spanTree = useMemo(() => buildSpanTree(spans), [spans]);

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] w-[95vw] overflow-y-auto sm:max-w-6xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            Trace Detail
            <button
              type="button"
              onClick={() => copy(traceId)}
              className="rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              title="Copy trace ID"
            >
              {copied ? <Check className="h-3.5 w-3.5 text-green-500" /> : <Copy className="h-3.5 w-3.5" />}
            </button>
          </DialogTitle>
        </DialogHeader>

        {loading && !trace ? (
          <div className="flex items-center justify-center py-12">
            <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
          </div>
        ) : !trace ? (
          <p className="py-8 text-center text-sm text-muted-foreground">Trace not found.</p>
        ) : (
          <div className="space-y-4">
            {/* Trace summary */}
            <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
              <div>
                <span className="text-muted-foreground">Name:</span>{" "}
                <span className="font-medium">{trace.name || "Unnamed"}</span>
              </div>
              <div>
                <span className="text-muted-foreground">Status:</span>{" "}
                <StatusBadge status={trace.status} />
              </div>
              <div>
                <span className="text-muted-foreground">Duration:</span>{" "}
                {formatDuration(trace.duration_ms || computeDurationMs(trace.start_time, trace.end_time))}
              </div>
              <div>
                <span className="text-muted-foreground">Channel:</span>{" "}
                {trace.channel || "—"}
              </div>
              <div>
                <span className="text-muted-foreground">Tokens:</span>{" "}
                {formatTokens(trace.total_input_tokens)} in / {formatTokens(trace.total_output_tokens)} out
                {((trace.metadata?.total_cache_read_tokens ?? 0) > 0 || (trace.metadata?.total_cache_creation_tokens ?? 0) > 0) && (
                  <span className="ml-1 text-xs">
                    {(trace.metadata?.total_cache_read_tokens ?? 0) > 0 && (
                      <span className="text-green-400">{formatTokens(trace.metadata!.total_cache_read_tokens!)} cached</span>
                    )}
                  </span>
                )}
              </div>
              <div>
                <span className="text-muted-foreground">Spans:</span>{" "}
                {trace.span_count} ({trace.llm_call_count} LLM, {trace.tool_call_count} tool)
              </div>
              <div>
                <span className="text-muted-foreground">Started:</span>{" "}
                {formatDate(trace.start_time)}
              </div>
              {trace.parent_trace_id && (
                <div>
                  <span className="text-muted-foreground">Delegated from:</span>{" "}
                  <button
                    type="button"
                    className="cursor-pointer font-mono text-xs text-primary hover:underline"
                    onClick={() => onNavigateTrace?.(trace.parent_trace_id!)}
                  >
                    {trace.parent_trace_id.slice(0, 8)}...
                  </button>
                </div>
              )}
            </div>

            {trace.input_preview && (
              <div className="rounded-md border p-3">
                <p className="mb-1 text-xs font-medium text-muted-foreground">Input</p>
                <pre className="max-h-[20vh] overflow-y-auto whitespace-pre-wrap break-all text-sm">{trace.input_preview}</pre>
              </div>
            )}

            {trace.output_preview && (
              <div className="rounded-md border p-3">
                <p className="mb-1 text-xs font-medium text-muted-foreground">Output</p>
                <pre className="max-h-[20vh] overflow-y-auto whitespace-pre-wrap break-all text-sm">{trace.output_preview}</pre>
              </div>
            )}

            {trace.error && (
              <div className="rounded-md border border-red-400/30 bg-red-500/10 p-3">
                <p className="break-all text-sm text-red-300">{trace.error}</p>
              </div>
            )}

            {/* Span tree */}
            {spans.length > 0 && (
              <div>
                <h4 className="mb-2 text-sm font-medium">Spans ({spans.length})</h4>
                <div className="space-y-1">
                  {spanTree.map((node) => (
                    <SpanTreeNode key={node.span.id} node={node} depth={0} />
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function SpanTreeNode({ node, depth }: { node: SpanNode; depth: number }) {
  const [expanded, setExpanded] = useState(depth === 0);
  const [detailOpen, setDetailOpen] = useState(false);
  const { span, children } = node;
  const hasChildren = children.length > 0;

  return (
    <div>
      <div
        className="mt-1.5 min-w-0 rounded-md border text-sm"
        style={{ marginLeft: depth * 16 }}
      >
        <div className="flex w-full items-center gap-1 px-2 py-2">
          {/* Tree toggle */}
          {hasChildren ? (
            <button
              type="button"
              className="flex h-5 w-5 shrink-0 cursor-pointer items-center justify-center rounded hover:bg-muted"
              onClick={() => setExpanded(!expanded)}
            >
              {expanded ? (
                <ChevronDown className="h-3.5 w-3.5" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5" />
              )}
            </button>
          ) : (
            <span className="w-5 shrink-0" />
          )}

          {/* Span info row - clickable for detail */}
          <button
            type="button"
            className="flex flex-1 cursor-pointer items-center gap-2 text-left hover:opacity-80"
            onClick={() => setDetailOpen(!detailOpen)}
          >
            <Badge variant="outline" className="shrink-0 text-xs">
              {span.span_type}
            </Badge>
            <span className="flex-1 truncate font-medium">
              {span.name || span.tool_name || "span"}
            </span>
            {(span.input_tokens > 0 || span.output_tokens > 0) && (
              <span className="hidden shrink-0 text-xs text-muted-foreground sm:inline">
                {formatTokens(span.input_tokens)}/{formatTokens(span.output_tokens)}
                {(span.metadata?.cache_read_tokens ?? 0) > 0 && (
                  <span className="ml-1 text-green-400" title="Cached tokens read">
                    ({formatTokens(span.metadata!.cache_read_tokens!)} cached)
                  </span>
                )}
                {(span.metadata?.thinking_tokens ?? 0) > 0 && (
                  <span className="ml-1 text-purple-400" title="Thinking tokens">
                    ({formatTokens(span.metadata!.thinking_tokens!)} thinking)
                  </span>
                )}
              </span>
            )}
            <span className="shrink-0 text-xs text-muted-foreground">
              {formatDuration(span.duration_ms || computeDurationMs(span.start_time, span.end_time))}
            </span>
            <StatusBadge status={span.status} />
          </button>
        </div>

        {/* Expanded detail panel */}
        {detailOpen && (
          <div className="max-h-[50vh] space-y-2 overflow-y-auto border-t px-3 py-2">
            {span.model && (
              <div className="text-xs">
                <span className="text-muted-foreground">Model:</span> {span.provider}/{span.model}
              </div>
            )}
            {(span.input_tokens > 0 || span.output_tokens > 0) && (
              <div className="text-xs">
                <span className="text-muted-foreground">Tokens:</span>{" "}
                {formatTokens(span.input_tokens)} in / {formatTokens(span.output_tokens)} out
                {((span.metadata?.cache_creation_tokens ?? 0) > 0 || (span.metadata?.cache_read_tokens ?? 0) > 0) && (
                  <span className="ml-2 text-muted-foreground">
                    (cache:
                    {(span.metadata?.cache_read_tokens ?? 0) > 0 && (
                      <span className="ml-1 text-green-400">{formatTokens(span.metadata!.cache_read_tokens!)} read</span>
                    )}
                    {(span.metadata?.cache_creation_tokens ?? 0) > 0 && (
                      <span className="ml-1 text-yellow-400">{formatTokens(span.metadata!.cache_creation_tokens!)} write</span>
                    )}
                    )
                  </span>
                )}
                {(span.metadata?.thinking_tokens ?? 0) > 0 && (
                  <span className="ml-2 text-muted-foreground">
                    (<span className="text-purple-400">{formatTokens(span.metadata!.thinking_tokens!)} thinking</span>)
                  </span>
                )}
              </div>
            )}
            {span.input_preview && (
              <div>
                <p className="text-xs text-muted-foreground">Input:</p>
                <pre className="mt-1 max-h-[20vh] overflow-y-auto break-all whitespace-pre-wrap rounded bg-muted/50 p-2 text-xs">
                  {span.input_preview}
                </pre>
              </div>
            )}
            {span.output_preview && (
              <div>
                <p className="text-xs text-muted-foreground">Output:</p>
                <pre className="mt-1 max-h-[20vh] overflow-y-auto break-all whitespace-pre-wrap rounded bg-muted/50 p-2 text-xs">
                  {span.output_preview}
                </pre>
              </div>
            )}
            {span.error && (
              <p className="break-all text-xs text-red-300">{span.error}</p>
            )}
          </div>
        )}
      </div>

      {/* Render children when tree is expanded */}
      {expanded && children.map((child) => (
        <SpanTreeNode key={child.span.id} node={child} depth={depth + 1} />
      ))}
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const variant =
    status === "ok" || status === "success" || status === "completed"
      ? "success"
      : status === "error" || status === "failed"
        ? "destructive"
        : status === "running" || status === "pending"
          ? "info"
          : "secondary";

  return <Badge variant={variant} className="text-xs">{status || "unknown"}</Badge>;
}
