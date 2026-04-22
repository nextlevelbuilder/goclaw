import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Play, FlaskConical, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ToolSingleCombobox } from "@/components/shared/tool-single-combobox";
import { TOOL_INPUT_TEMPLATES } from "./tool-input-templates";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { useTestHook, type HookConfig, type HookTestResult } from "@/hooks/use-hooks";
import { HookDiffViewer } from "./hook-diff-viewer";

interface HookTestPanelProps {
  hook: Partial<HookConfig>;
}

const DECISION_STYLES: Record<string, string> = {
  allow: "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300",
  block: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300",
  error: "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300",
  timeout: "bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300",
};

const LS_KEY = (id: string) => `goclaw:hook-test:${id}`;

function loadSavedSample(id: string | undefined) {
  if (!id) return null;
  try {
    const raw = localStorage.getItem(LS_KEY(id));
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

function saveSample(id: string | undefined, data: unknown) {
  if (!id) return;
  try {
    localStorage.setItem(LS_KEY(id), JSON.stringify(data));
  } catch {
    // ignore quota errors
  }
}

export function HookTestPanel({ hook }: HookTestPanelProps) {
  const { t } = useTranslation("hooks");
  const testMutation = useTestHook();

  const saved = loadSavedSample(hook.id);
  const [toolName, setToolName] = useState<string>(saved?.toolName ?? "bash");
  const [toolInputRaw, setToolInputRaw] = useState<string>(
    saved?.toolInputRaw ?? JSON.stringify({ command: "ls -la" }, null, 2),
  );
  const [rawInput, setRawInput] = useState<string>(saved?.rawInput ?? "");
  const [result, setResult] = useState<HookTestResult | null>(null);
  const [parseError, setParseError] = useState<string | null>(null);

  const handleToolSelect = (name: string) => {
    const template = TOOL_INPUT_TEMPLATES[name];
    if (!template) return;
    const json = JSON.stringify(template, null, 2);
    if (toolInputRaw.trim() && toolInputRaw.trim() !== "{}") {
      if (!window.confirm(t("test.overwriteConfirm"))) return;
    }
    setToolInputRaw(json);
  };

  const handleFire = async () => {
    setParseError(null);
    let toolInput: Record<string, unknown>;
    try {
      toolInput = JSON.parse(toolInputRaw);
    } catch {
      setParseError(t("test.parseError"));
      return;
    }

    saveSample(hook.id, { toolName, toolInputRaw, rawInput });

    const res = await testMutation.mutateAsync({
      config: hook,
      sampleEvent: { toolName, toolInput, rawInput: rawInput || undefined },
    });
    setResult(res.result);
  };

  return (
    <div className="grid gap-5 lg:grid-cols-2">
      {/* ── Left: sample event input ─────────────────────────────── */}
      <section className="space-y-3 rounded-lg border bg-card p-4">
        <header className="flex items-center justify-between">
          <div>
            <p className="text-sm font-medium">{t("test.sampleEvent")}</p>
            <p className="text-xs text-muted-foreground">{t("test.sampleEventHint")}</p>
          </div>
        </header>

        <div className="space-y-1.5">
          <Label className="text-xs">{t("test.toolName")}</Label>
          <ToolSingleCombobox
            value={toolName}
            onChange={setToolName}
            onToolSelect={handleToolSelect}
            placeholder={t("test.toolNamePickerPlaceholder")}
          />
          <p className="text-2xs text-muted-foreground">{t("test.toolNameHint")}</p>
        </div>

        <div className="space-y-1.5">
          <Label className="text-xs">{t("test.toolInput")}</Label>
          <Textarea
            value={toolInputRaw}
            onChange={(e) => setToolInputRaw(e.target.value)}
            rows={10}
            placeholder='{"command": "ls -la"}'
            className="text-base md:text-sm font-mono"
          />
          {parseError ? (
            <p className="text-xs text-destructive">{parseError}</p>
          ) : (
            <p className="text-2xs text-muted-foreground">{t("test.toolInputHint")}</p>
          )}
        </div>

        <div className="space-y-1.5">
          <Label className="text-xs">{t("test.rawInput")}</Label>
          <Input
            value={rawInput}
            onChange={(e) => setRawInput(e.target.value)}
            placeholder={t("test.rawInputPlaceholder")}
            className="text-base md:text-sm"
          />
          <p className="text-2xs text-muted-foreground">{t("test.rawInputHint")}</p>
        </div>

        <Button
          onClick={handleFire}
          disabled={testMutation.isPending}
          className="w-full gap-1.5"
        >
          <Play className="h-3.5 w-3.5" />
          {testMutation.isPending ? t("test.firing") : t("test.fire")}
        </Button>
      </section>

      {/* ── Right: result ────────────────────────────────────────── */}
      <section className="space-y-3 rounded-lg border bg-card p-4">
        <header>
          <p className="text-sm font-medium">{t("test.resultTitle")}</p>
          <p className="text-xs text-muted-foreground">{t("test.resultHint")}</p>
        </header>

        {!result && !testMutation.isPending && (
          <div className="flex flex-col items-center justify-center gap-2 py-12 text-center">
            <FlaskConical className="h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm text-muted-foreground">{t("test.emptyState")}</p>
          </div>
        )}

        {testMutation.isPending && (
          <div className="flex items-center justify-center py-12">
            <p className="text-sm text-muted-foreground animate-pulse">{t("test.firing")}</p>
          </div>
        )}

        {result && (
          <div className="space-y-4">
            {/* Decision pill row */}
            <div className="flex flex-wrap items-center gap-3">
              <span
                className={`inline-flex items-center rounded-full px-3 py-1 text-sm font-semibold ${DECISION_STYLES[result.decision] ?? "bg-muted text-muted-foreground"}`}
              >
                {t(`decision.${result.decision}`)}
              </span>
              <div className="text-xs text-muted-foreground">
                <span className="font-medium">{t("test.duration")}:</span>{" "}
                <span className="font-mono">{result.durationMs}ms</span>
              </div>
              {result.statusCode != null && (
                <div className="text-xs text-muted-foreground">
                  <span className="font-medium">{t("test.statusCode")}:</span>{" "}
                  <Badge variant="outline">{result.statusCode}</Badge>
                </div>
              )}
            </div>

            {result.reason && (
              <div className="rounded border bg-muted/40 p-3">
                <p className="text-2xs uppercase tracking-wide text-muted-foreground mb-1">{t("test.reason")}</p>
                <p className="text-sm">{result.reason}</p>
              </div>
            )}

            {result.stdout && (
              <div className="space-y-1">
                <p className="text-2xs uppercase tracking-wide text-muted-foreground">{t("test.stdout")}</p>
                <pre className="overflow-x-auto rounded bg-muted p-2 text-xs font-mono">{result.stdout}</pre>
              </div>
            )}

            {result.stderr && (
              <div className="space-y-1">
                <p className="text-2xs uppercase tracking-wide text-destructive">{t("test.stderr")}</p>
                <pre className="overflow-x-auto rounded bg-destructive/10 p-2 text-xs font-mono text-destructive">{result.stderr}</pre>
              </div>
            )}

            {result.error && (
              <div className="flex items-start gap-2 rounded border border-destructive/30 bg-destructive/5 p-3">
                <AlertCircle className="h-4 w-4 shrink-0 text-destructive mt-0.5" />
                <p className="text-xs text-destructive">{result.error}</p>
              </div>
            )}

            {result.updatedInput && (
              <div className="space-y-1">
                <p className="text-2xs uppercase tracking-wide text-muted-foreground">{t("test.updatedInput")}</p>
                <HookDiffViewer
                  before={(() => {
                    try { return JSON.parse(toolInputRaw); } catch { return {}; }
                  })()}
                  after={result.updatedInput}
                />
              </div>
            )}
          </div>
        )}
      </section>
    </div>
  );
}
