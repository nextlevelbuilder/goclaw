import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight, Wrench } from "lucide-react";

/** Mirrors providers.ToolFunctionSchema on the Go side. */
interface ToolFunctionSchema {
  name: string;
  description: string;
  parameters?: Record<string, unknown>;
}

/** Mirrors providers.ToolDefinition on the Go side. */
export interface ToolDefinition {
  type: string;
  function?: ToolFunctionSchema;
}

interface ToolsPreviewSectionProps {
  tools: ToolDefinition[] | undefined;
}

/**
 * Lists the tool schemas (name, description, parameters) sent to the LLM
 * provider as the `tools` API parameter, alongside the system prompt text.
 */
export function ToolsPreviewSection({ tools }: ToolsPreviewSectionProps) {
  const { t } = useTranslation("agents");
  const list = tools ?? [];

  return (
    <div className="mt-4 border-t pt-3">
      <div className="mb-2 flex items-center gap-2">
        <Wrench className="h-4 w-4 text-muted-foreground" />
        <span className="text-sm font-medium">{t("files.toolsSection")}</span>
        <span className="rounded bg-muted px-2 py-0.5 text-xs tabular-nums text-muted-foreground">
          {t("files.toolsAvailable", { count: list.length })}
        </span>
      </div>
      {list.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("files.noTools")}</p>
      ) : (
        <div className="space-y-1.5">
          {list.map((tool, idx) => (
            <ToolEntry key={`${tool.function?.name ?? "tool"}-${idx}`} tool={tool} />
          ))}
        </div>
      )}
    </div>
  );
}

function ToolEntry({ tool }: { tool: ToolDefinition }) {
  const { t } = useTranslation("agents");
  const [expanded, setExpanded] = useState(false);
  const fn = tool.function;
  const hasParams = !!fn?.parameters && Object.keys(fn.parameters).length > 0;

  return (
    <div className="rounded-md border bg-muted">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs cursor-pointer"
        onClick={() => setExpanded((v) => !v)}
      >
        <ChevronRight
          className={`h-3 w-3 shrink-0 text-muted-foreground transition-transform ${expanded ? "rotate-90" : ""}`}
        />
        <span className="font-medium shrink-0 font-mono">{fn?.name ?? tool.type}</span>
        {fn?.description && (
          <span className="truncate text-muted-foreground ml-1">{fn.description}</span>
        )}
      </button>
      {expanded && (
        <div className="border-t border-muted px-2 py-1.5 space-y-1.5">
          <div>
            <div className="text-2xs font-semibold uppercase text-muted-foreground mb-0.5">
              {t("files.toolParameters")}
            </div>
            {hasParams ? (
              <div className="overflow-x-auto">
                <pre className="whitespace-pre-wrap break-words text-xs-plus font-mono bg-background rounded p-1.5 max-h-60 overflow-y-auto">
                  {JSON.stringify(fn?.parameters, null, 2)}
                </pre>
              </div>
            ) : (
              <p className="text-xs-plus text-muted-foreground">{t("files.noParameters")}</p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
