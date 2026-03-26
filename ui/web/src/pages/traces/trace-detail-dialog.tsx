import { useState, useEffect, useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { ChevronRight, ChevronDown, Copy, Check, Download, CircleCheck, CircleX, Loader, CircleMinus, Square } from "lucide-react";
import { useClipboard } from "@/hooks/use-clipboard";
import { useHttp } from "@/hooks/use-ws";
import { useWsEvent } from "@/hooks/use-ws-event";
import { Events } from "@/api/protocol";
import hljs from "highlight.js/lib/core";
import json from "highlight.js/lib/languages/json";
import { formatDate, formatDuration, formatTokens, computeDurationMs } from "@/lib/format";
import { useUiStore } from "@/stores/use-ui-store";
import type { TraceData, SpanData } from "./hooks/use-traces";
import type { AgentEventPayload } from "@/types/chat";

hljs.registerLanguage("json", json);

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
  onAbortRun?: (trace: TraceData, e: React.MouseEvent) => void;
}

export function TraceDetailDialog({ traceId, onClose, getTrace, onNavigateTrace, onAbortRun }: TraceDetailDialogProps) {
  const { t } = useTranslation("traces");
  const tz = useUiStore((s) => s.timezone);
  const http = useHttp();
  const [trace, setTrace] = useState<TraceData | null>(null);
  const [spans, setSpans] = useState<SpanData[]>([]);
  const [loading, setLoading] = useState(true);
  const [exporting, setExporting] = useState(false);
  const { copied, copy } = useClipboard();

  const handleExport = useCallback(async () => {
    setExporting(true);
    try {
      const blob = await http.downloadBlob(`/v1/traces/${traceId}/export`);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `trace-${traceId.slice(0, 8)}.json.gz`;
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      // silently fail — user sees no download
    } finally {
      setExporting(false);
    }
  }, [http, traceId]);

  const fetchTrace = useCallback(() => {
    getTrace(traceId).then((result) => {
      if (result) {
        setTrace(result.trace);
        setSpans(result.spans ?? []);
      }
    });
  }, [traceId, getTrace]);

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

  // Auto-refetch when trace aggregates update (spans flushed every ~5s)
  useWsEvent(
    Events.TRACE_UPDATED,
    useCallback(
      (payload: unknown) => {
        const data = payload as { trace_ids?: string[] };
        if (data?.trace_ids?.includes(traceId)) {
          fetchTrace();
        }
      },
      [traceId, fetchTrace],
    ),
  );

  // Also refetch on run completion (final status + output)
  useWsEvent(
    Events.AGENT,
    useCallback(
      (payload: unknown) => {
        const event = payload as AgentEventPayload;
        if (event?.type === "run.completed" || event?.type === "run.failed") {
          fetchTrace();
        }
      },
      [fetchTrace],
    ),
  );

  const spanTree = useMemo(() => buildSpanTree(spans), [spans]);

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] w-[95vw] flex flex-col sm:max-w-6xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 pr-8">
            {t("detail.title")}
            <button
              type="button"
              onClick={() => copy(traceId)}
              className="ml-auto flex items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
              {copied ? <Check className="h-3.5 w-3.5 text-green-500" /> : <Copy className="h-3.5 w-3.5" />}
              {t("detail.copyTraceId")}
            </button>
            <button
              type="button"
              onClick={handleExport}
              disabled={exporting || !trace}
              className="flex items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
            >
              {exporting ? (
                <div className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
              ) : (
                <Download className="h-3.5 w-3.5" />
              )}
              {t("detail.export")}
            </button>
            {trace && (trace.status === "running") && onAbortRun && (
              <button
                type="button"
                onClick={(e) => onAbortRun(trace, e)}
                className="flex cursor-pointer items-center gap-1 rounded-md bg-destructive px-2 py-1 text-xs text-destructive-foreground transition-colors hover:bg-destructive/90"
              >
                <Square className="h-3.5 w-3.5" />
                {t("detail.stopRun")}
              </button>
            )}
          </DialogTitle>
        </DialogHeader>

        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6">
        {loading && !trace ? (
          <div className="flex items-center justify-center py-12">
            <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
          </div>
        ) : !trace ? (
          <p className="py-8 text-center text-sm text-muted-foreground">{t("detail.notFound")}</p>
        ) : (
          <div className="space-y-4">
            {/* Trace summary */}
            <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
              <div>
                <span className="text-muted-foreground">{t("detail.name")}</span>{" "}
                <span className="font-medium">{trace.name || t("unnamed")}</span>
              </div>
              <div>
                <span className="text-muted-foreground">{t("detail.status")}</span>{" "}
                <StatusBadge status={trace.status} />
              </div>
              <div>
                <span className="text-muted-foreground">{t("detail.duration")}</span>{" "}
                {formatDuration(trace.duration_ms || computeDurationMs(trace.start_time, trace.end_time))}
              </div>
              <div>
                <span className="text-muted-foreground">{t("detail.channel")}</span>{" "}
                {trace.channel || "—"}
              </div>
              <div>
                <span className="text-muted-foreground">{t("detail.tokens")}</span>{" "}
                {formatTokens(trace.total_input_tokens)} in / {formatTokens(trace.total_output_tokens)} out
                {((trace.metadata?.total_cache_read_tokens ?? 0) > 0 || (trace.metadata?.total_cache_creation_tokens ?? 0) > 0) && (
                  <span className="ml-1 text-xs">
                    {(trace.metadata?.total_cache_read_tokens ?? 0) > 0 && (
                      <span className="text-green-400">{formatTokens(trace.metadata!.total_cache_read_tokens!)} {t("span.cached")}</span>
                    )}
                  </span>
                )}
              </div>
              <div>
                <span className="text-muted-foreground">{t("detail.spans")}</span>{" "}
                {trace.span_count} ({trace.llm_call_count} {t("detail.llmCalls")}, {trace.tool_call_count} {t("detail.toolCalls")})
              </div>
              <div>
                <span className="text-muted-foreground">{t("detail.started")}</span>{" "}
                {formatDate(trace.start_time, tz)}
              </div>
              <div>
                <span className="text-muted-foreground">{t("detail.createdAt")}</span>{" "}
                {formatDate(trace.created_at, tz)}
              </div>
              {trace.parent_trace_id && (
                <div>
                  <span className="text-muted-foreground">{t("detail.delegatedFrom")}</span>{" "}
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
              <PreviewBlock label={t("detail.input")} content={trace.input_preview} />
            )}

            {trace.output_preview && (
              <PreviewBlock label={t("detail.output")} content={trace.output_preview} />
            )}

            {trace.error && (
              <div className="rounded-md border border-red-400/30 bg-red-500/10 p-3">
                <p className="break-all text-sm text-red-300">{trace.error}</p>
              </div>
            )}

            {/* Span tree */}
            {spans.length > 0 && (
              <div>
                <h4 className="mb-2 text-sm font-medium">{t("detail.spansCount", { count: spans.length })}</h4>
                <div className="space-y-1">
                  {spanTree.map((node) => (
                    <SpanTreeNode key={node.span.id} node={node} depth={0} />
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function SpanTreeNode({ node, depth }: { node: SpanNode; depth: number }) {
  const { t } = useTranslation("traces");
  const tz = useUiStore((s) => s.timezone);
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
                  <span className="ml-1 text-green-400">
                    ({formatTokens(span.metadata!.cache_read_tokens!)} {t("span.cached")})
                  </span>
                )}
                {(span.metadata?.thinking_tokens ?? 0) > 0 && (
                  <span className="ml-1 text-orange-400">
                    ({formatTokens(span.metadata!.thinking_tokens!)} {t("span.thinking")})
                  </span>
                )}
              </span>
            )}
            {span.created_at && (
              <Badge variant="outline" className="hidden shrink-0 text-xs text-muted-foreground lg:inline-flex">
                {formatDate(span.created_at, tz)}
              </Badge>
            )}
            <Badge variant="outline" className="shrink-0 text-xs text-muted-foreground">
              {formatDuration(span.duration_ms || computeDurationMs(span.start_time, span.end_time))}
            </Badge>
            <StatusBadge status={span.status} />
          </button>
        </div>

        {/* Expanded detail panel */}
        {detailOpen && (
          <div className="max-h-[50vh] space-y-2 overflow-y-auto border-t px-3 py-2">
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs">
              {span.start_time && (
                <div>
                  <span className="text-muted-foreground">{t("span.startTime")}</span> {formatDate(span.start_time, tz)}
                </div>
              )}
              {span.end_time && (
                <div>
                  <span className="text-muted-foreground">{t("span.endTime")}</span> {formatDate(span.end_time, tz)}
                </div>
              )}
              {span.model && (
                <div>
                  <span className="text-muted-foreground">{t("span.model")}</span> {span.provider}/{span.model}
                </div>
              )}
            </div>
            {(span.input_tokens > 0 || span.output_tokens > 0) && (
              <div className="text-xs">
                <span className="text-muted-foreground">{t("span.tokens")}</span>{" "}
                {formatTokens(span.input_tokens)} in / {formatTokens(span.output_tokens)} out
                {((span.metadata?.cache_creation_tokens ?? 0) > 0 || (span.metadata?.cache_read_tokens ?? 0) > 0) && (
                  <span className="ml-2 text-muted-foreground">
                    (cache:
                    {(span.metadata?.cache_read_tokens ?? 0) > 0 && (
                      <span className="ml-1 text-green-400">{formatTokens(span.metadata!.cache_read_tokens!)} {t("span.cacheRead")}</span>
                    )}
                    {(span.metadata?.cache_creation_tokens ?? 0) > 0 && (
                      <span className="ml-1 text-yellow-400">{formatTokens(span.metadata!.cache_creation_tokens!)} {t("span.cacheWrite")}</span>
                    )}
                    )
                  </span>
                )}
                {(span.metadata?.thinking_tokens ?? 0) > 0 && (
                  <span className="ml-2 text-muted-foreground">
                    (<span className="text-orange-400">{formatTokens(span.metadata!.thinking_tokens!)} {t("span.thinking")}</span>)
                  </span>
                )}
              </div>
            )}
            {span.input_preview && (
              <PreviewBlock label={t("span.input")} content={span.input_preview} />
            )}
            {span.output_preview && (
              <PreviewBlock label={t("span.output")} content={span.output_preview} />
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

/** Try to pretty-print JSON; returns { text, isJson }. */
function formatPreview(text: string): { text: string; isJson: boolean } {
  const trimmed = text.trim();
  if ((trimmed.startsWith("{") && trimmed.endsWith("}")) || (trimmed.startsWith("[") && trimmed.endsWith("]"))) {
    try {
      const pretty = JSON.stringify(JSON.parse(trimmed), null, 2);
      return { text: pretty, isJson: true };
    } catch {
      // not valid JSON
    }
  }
  return { text, isJson: false };
}

function PreviewBlock({ label, content }: { label: string; content: string }) {
  const { t } = useTranslation("traces");
  const { copied, copy } = useClipboard();
  const { text: formatted, isJson } = useMemo(() => formatPreview(content), [content]);
  const highlightedHtml = useMemo(() => {
    if (!isJson) return null;
    return hljs.highlight(formatted, { language: "json" }).value;
  }, [formatted, isJson]);

  return (
    <div className="relative rounded-md border p-3">
      <p className="mb-1 text-xs font-medium text-muted-foreground">{label}</p>
      <button
        type="button"
        onClick={() => copy(content)}
        className="absolute right-2 top-2 flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
      >
        {copied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
        {t("detail.copy")}
      </button>
      {highlightedHtml ? (
        <pre
          className="hljs mt-1 max-h-[20vh] overflow-y-auto whitespace-pre-wrap break-all text-xs sm:max-h-[40vh]"
          dangerouslySetInnerHTML={{ __html: highlightedHtml }}
        />
      ) : (
        <pre className="mt-1 max-h-[20vh] overflow-y-auto whitespace-pre-wrap break-all text-xs sm:max-h-[40vh]">{formatted}</pre>
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const isOk = status === "ok" || status === "success" || status === "completed";
  const isError = status === "error" || status === "failed";
  const isRunning = status === "running" || status === "pending";

  const variant = isOk ? "success" : isError ? "destructive" : isRunning ? "info" : "secondary";
  const Icon = isOk ? CircleCheck : isError ? CircleX : isRunning ? Loader : CircleMinus;

  return (
    <Badge variant={variant} className="text-xs">
      <Icon className={"h-3 w-3 sm:hidden" + (isRunning ? " animate-spin" : "")} />
      <span className="hidden sm:inline">{status || "unknown"}</span>
    </Badge>
  );
}
