/**
 * Sub-components for session detail view:
 * - SystemMessageBlock: collapsible system/internal message display
 * - SummaryBlock: collapsible session summary with truncation
 */
import { useState, useRef, useLayoutEffect } from "react";
import { useTranslation } from "react-i18next";
import { Info } from "lucide-react";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";

export function SystemMessageBlock({ content }: { content: string }) {
  const { t } = useTranslation("sessions");
  const [expanded, setExpanded] = useState(false);
  // Extract the first line as title, rest as body
  const lines = content.split("\n");
  const title = (lines[0] ?? "").replace(/^\[System Message\]\s*/, "").trim();
  const body = lines.slice(1).join("\n").trim();

  return (
    <div className="mx-auto flex max-w-3xl items-start gap-2 rounded-md border border-dashed border-muted-foreground/30 bg-muted/30 px-4 py-2">
      <Info className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0 text-xs text-muted-foreground">
        <span className="font-medium">{title || t("detail.systemMessage")}</span>
        {body && (
          <>
            <button
              type="button"
              onClick={() => setExpanded((v) => !v)}
              className="ml-1 cursor-pointer text-primary hover:underline"
            >
              {expanded ? t("detail.hide") : t("detail.showDetails")}
            </button>
            {expanded && (
              <div className="mt-2">
                <MarkdownRenderer content={body} className="text-xs" />
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

const SUMMARY_MAX_HEIGHT = 72; // ~3 lines of text

export function SummaryBlock({ text }: { text: string }) {
  const { t } = useTranslation("sessions");
  const [expanded, setExpanded] = useState(false);
  const [needsTruncation, setNeedsTruncation] = useState(false);
  const contentRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    if (contentRef.current) {
      // Temporarily remove max-height to measure true content height
      const el = contentRef.current;
      const prev = el.style.maxHeight;
      el.style.maxHeight = "none";
      setNeedsTruncation(el.scrollHeight > SUMMARY_MAX_HEIGHT);
      el.style.maxHeight = prev;
    }
  }, [text]);

  return (
    <div className="border-b bg-muted/50 px-6 py-3 text-sm">
      <span className="font-medium">{t("detail.summary")}: </span>
      <div
        ref={contentRef}
        className="mt-1 overflow-hidden transition-[max-height] duration-200"
        style={{ maxHeight: expanded ? (contentRef.current?.scrollHeight ?? "none") : SUMMARY_MAX_HEIGHT }}
      >
        {text}
      </div>
      {needsTruncation && (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="mt-1 cursor-pointer text-xs font-medium text-primary hover:underline"
        >
          {expanded ? t("detail.showLess") : t("detail.showMore")}
        </button>
      )}
    </div>
  );
}
