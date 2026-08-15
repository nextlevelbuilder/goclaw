import { useMemo } from "react";
import hljs from "highlight.js/lib/core";
import jsonLang from "highlight.js/lib/languages/json";
import { useTranslation } from "react-i18next";

hljs.registerLanguage("json", jsonLang);

interface HookDiffViewerProps {
  before: Record<string, unknown>;
  after: Record<string, unknown>;
}

function highlight(code: string): string {
  try {
    return hljs.highlight(code, { language: "json" }).value;
  } catch {
    return code;
  }
}

function computeDeltaLines(
  beforeLines: string[],
  afterLines: string[],
): { before: Array<{ text: string; changed: boolean }>; after: Array<{ text: string; changed: boolean }> } {
  const maxLen = Math.max(beforeLines.length, afterLines.length);
  const beforeOut: Array<{ text: string; changed: boolean }> = [];
  const afterOut: Array<{ text: string; changed: boolean }> = [];

  for (let i = 0; i < maxLen; i++) {
    const b = beforeLines[i] ?? "";
    const a = afterLines[i] ?? "";
    const changed = b !== a;
    beforeOut.push({ text: b, changed });
    afterOut.push({ text: a, changed });
  }
  return { before: beforeOut, after: afterOut };
}

export function HookDiffViewer({ before, after }: HookDiffViewerProps) {
  const { t } = useTranslation("hooks");

  const beforeJson = useMemo(() => JSON.stringify(before, null, 2), [before]);
  const afterJson = useMemo(() => JSON.stringify(after, null, 2), [after]);

  const isIdentical = beforeJson === afterJson;

  const { before: beforeLines, after: afterLines } = useMemo(() => {
    const bl = beforeJson.split("\n");
    const al = afterJson.split("\n");
    return computeDeltaLines(bl, al);
  }, [beforeJson, afterJson]);

  if (isIdentical) {
    return (
      <div className="rounded border bg-muted/40 px-4 py-3 text-xs text-muted-foreground">
        {t("test.updatedInput")} — no changes
      </div>
    );
  }

  return (
    <div className="space-y-1">
      <p className="text-xs font-medium text-muted-foreground">{t("test.updatedInput")}</p>
      <div className="grid grid-cols-2 gap-1 overflow-x-auto rounded border text-xs font-mono">
        {/* Before */}
        <div className="min-w-0 border-r">
          <div className="border-b bg-muted/50 px-3 py-1 text-2xs font-sans text-muted-foreground">Before</div>
          <div className="overflow-x-auto">
            {beforeLines.map((line, i) => (
              <div
                key={i}
                className={`px-3 py-px leading-5 whitespace-pre ${line.changed ? "bg-red-50 dark:bg-red-950/20" : ""}`}
                // highlight.js output is safe escaped HTML
                dangerouslySetInnerHTML={{ __html: highlight(line.text) || "\u00a0" }}
              />
            ))}
          </div>
        </div>
        {/* After */}
        <div className="min-w-0">
          <div className="border-b bg-muted/50 px-3 py-1 text-2xs font-sans text-muted-foreground">After</div>
          <div className="overflow-x-auto">
            {afterLines.map((line, i) => (
              <div
                key={i}
                className={`px-3 py-px leading-5 whitespace-pre ${line.changed ? "bg-green-50 dark:bg-green-950/20" : ""}`}
                dangerouslySetInnerHTML={{ __html: highlight(line.text) || "\u00a0" }}
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
