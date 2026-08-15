import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { ChevronRight, ChevronDown, CircleCheck, CircleX, Loader, CircleMinus } from "lucide-react";
import { formatDate, formatDuration, formatTokens, computeDurationMs } from "@/lib/format";
import { useUiStore } from "@/stores/use-ui-store";
import { TracePreviewBlock } from "./trace-preview-block";
import type { SpanData } from "./hooks/use-traces";
import { buildSpanTree } from "@/adapters/trace.adapter";
import type { SpanNode } from "@/adapters/trace.adapter";

export { buildSpanTree };
export type { SpanNode };

export function StatusBadge({ status }: { status: string }) {
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

export function SpanTreeNode({ node, depth }: { node: SpanNode; depth: number }) {
  const { t } = useTranslation("traces");
  const tz = useUiStore((s) => s.timezone);
  const [expanded, setExpanded] = useState(depth === 0);
  const [detailOpen, setDetailOpen] = useState(false);
  const { span, children } = node;
  const hasChildren = children.length > 0;

  return (
    <div>
      <div className="mt-1.5 min-w-0 rounded-md border text-sm" style={{ marginLeft: depth * 16 }}>
        <div className="flex w-full items-center gap-1 px-2 py-2">
          {hasChildren ? (
            <button type="button" className="flex h-5 w-5 shrink-0 cursor-pointer items-center justify-center rounded hover:bg-muted" onClick={() => setExpanded(!expanded)}>
              {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
            </button>
          ) : (
            <span className="w-5 shrink-0" />
          )}

          <button type="button" className="flex flex-1 cursor-pointer items-center gap-2 text-left hover:opacity-80" onClick={() => setDetailOpen(!detailOpen)}>
            <Badge variant="outline" className="shrink-0 text-xs">{span.span_type}</Badge>
            <span className="flex-1 truncate font-medium">{span.name || span.tool_name || "span"}</span>
            {(span.input_tokens > 0 || span.output_tokens > 0) && (
              <span className="hidden shrink-0 text-xs text-muted-foreground sm:inline">
                {formatTokens(span.input_tokens)}/{formatTokens(span.output_tokens)}
                {(span.metadata?.cache_read_tokens ?? 0) > 0 && <span className="ml-1 text-green-400">({formatTokens(span.metadata!.cache_read_tokens!)} {t("span.cached")})</span>}
                {(span.metadata?.thinking_tokens ?? 0) > 0 && <span className="ml-1 text-orange-400">({formatTokens(span.metadata!.thinking_tokens!)} {t("span.thinking")})</span>}
              </span>
            )}
            {span.created_at && <Badge variant="outline" className="hidden shrink-0 text-xs text-muted-foreground lg:inline-flex">{formatDate(span.created_at, tz)}</Badge>}
            <Badge variant="outline" className="shrink-0 text-xs text-muted-foreground">{formatDuration(span.duration_ms || computeDurationMs(span.start_time, span.end_time))}</Badge>
            <StatusBadge status={span.status} />
          </button>
        </div>

        {detailOpen && <SpanDetailPanel span={span} />}
      </div>

      {expanded && children.map((child) => (
        <SpanTreeNode key={child.span.id} node={child} depth={depth + 1} />
      ))}
    </div>
  );
}

/** Expanded detail panel for a single span */
function SpanDetailPanel({ span }: { span: SpanData }) {
  const { t } = useTranslation("traces");
  const tz = useUiStore((s) => s.timezone);
  const reasoning = span.metadata?.reasoning;

  return (
    <div className="max-h-[50vh] space-y-2 overflow-y-auto border-t px-3 py-2">
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs">
        {span.start_time && <div><span className="text-muted-foreground">{t("span.startTime")}</span> {formatDate(span.start_time, tz)}</div>}
        {span.end_time && <div><span className="text-muted-foreground">{t("span.endTime")}</span> {formatDate(span.end_time, tz)}</div>}
        {span.model && <div><span className="text-muted-foreground">{t("span.model")}</span> {span.provider}/{span.model}</div>}
      </div>
      {(span.input_tokens > 0 || span.output_tokens > 0) && (
        <div className="text-xs">
          <span className="text-muted-foreground">{t("span.tokens")}</span>{" "}
          {formatTokens(span.input_tokens)} in / {formatTokens(span.output_tokens)} out
          {((span.metadata?.cache_creation_tokens ?? 0) > 0 || (span.metadata?.cache_read_tokens ?? 0) > 0) && (
            <span className="ml-2 text-muted-foreground">
              (cache:
              {(span.metadata?.cache_read_tokens ?? 0) > 0 && <span className="ml-1 text-green-400">{formatTokens(span.metadata!.cache_read_tokens!)} {t("span.cacheRead")}</span>}
              {(span.metadata?.cache_creation_tokens ?? 0) > 0 && <span className="ml-1 text-yellow-400">{formatTokens(span.metadata!.cache_creation_tokens!)} {t("span.cacheWrite")}</span>}
              )
            </span>
          )}
          {(span.metadata?.thinking_tokens ?? 0) > 0 && (
            <span className="ml-2 text-muted-foreground">(<span className="text-orange-400">{formatTokens(span.metadata!.thinking_tokens!)} {t("span.thinking")}</span>)</span>
          )}
        </div>
      )}
      {reasoning && (
        <div className="text-xs text-muted-foreground">
          <span>{t("span.reasoning")}</span>{" "}
          {reasoning.requested_effort ? <span>{t("span.requested")} {reasoning.requested_effort}</span> : null}
          {reasoning.source ? <span className="ml-2">{t("span.source")} {t(`span.sourceValue.${reasoning.source}`)}</span> : null}
          {reasoning.effective_effort ? <span className="ml-2">{t("span.effective")} {reasoning.effective_effort}</span> : null}
          {reasoning.fallback ? <span className="ml-2">{t("span.fallback")} {reasoning.fallback}</span> : null}
          {reasoning.used_provider_default ? <span className="ml-2">{t("span.modelDefault")}</span> : null}
          {reasoning.reason ? <div className="mt-1">{reasoning.reason}</div> : null}
          {reasoning.supported_levels?.length ? <div className="mt-1">{t("span.supportedLevels")} {reasoning.supported_levels.join(", ")}</div> : null}
        </div>
      )}
      {span.input_preview && <TracePreviewBlock label={t("span.input")} content={span.input_preview} />}
      {span.output_preview && <TracePreviewBlock label={t("span.output")} content={span.output_preview} />}
      {span.error && <p className="break-all text-xs text-red-300">{span.error}</p>}
    </div>
  );
}
