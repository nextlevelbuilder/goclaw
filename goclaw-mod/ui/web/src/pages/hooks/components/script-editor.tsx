import { useId, useMemo, useRef, type ChangeEvent, type UIEvent } from "react";
import Prism from "prismjs";
import "prismjs/components/prism-javascript";
// Tomorrow theme matches the dark editor surface defined below; tokens get
// proper color without requiring app-wide CSS coordination.
import "prismjs/themes/prism-tomorrow.css";
import { useTranslation } from "react-i18next";

interface ScriptEditorProps {
  value: string;
  onChange: (v: string) => void;
  // Server-returned compile error. goja prints "(anonymous): Line N:M message"
  // (see goja parser/error.go — "%s: Line %d:%d %s"). We extract the location
  // with the regex below; when the shape doesn't match we surface the raw
  // string unchanged.
  error?: string;
  readOnly?: boolean;
  minLines?: number;
}

// parseLineCol extracts {line, col} from a goja compile-error string. Format
// is "... Line N:M ...". Any other shape returns null so the caller can fall
// back to rendering the raw message.
export function parseLineCol(err?: string): { line: number; col: number } | null {
  if (!err) return null;
  const m = err.match(/Line (\d+):(\d+)/);
  if (!m || !m[1] || !m[2]) return null;
  return { line: parseInt(m[1], 10), col: parseInt(m[2], 10) };
}

// Minimal native overlay editor: a textarea sits on top of a <pre> showing
// the highlighted source. Keeping this in-house instead of pulling in
// react-simple-code-editor avoids a React 19 compat surface and lets the
// component lazy-create Prism grammar without crashing when the language
// hasn't loaded yet.
export function ScriptEditor({
  value,
  onChange,
  error,
  readOnly,
  minLines = 12,
}: ScriptEditorProps) {
  const { t } = useTranslation("hooks");
  const id = useId();
  const preRef = useRef<HTMLPreElement>(null);
  const taRef = useRef<HTMLTextAreaElement>(null);

  const loc = useMemo(() => parseLineCol(error), [error]);

  const safeValue = value ?? "";
  // Keep a trailing newline so the highlighted layer matches textarea height
  // when the file ends without one (avoids the last line getting clipped).
  const displayValue =
    safeValue.length === 0 || safeValue.endsWith("\n") ? safeValue : safeValue + "\n";

  const highlighted = useMemo(() => {
    const lang = Prism.languages.javascript;
    if (!lang) return escapeHtml(displayValue);
    try {
      return Prism.highlight(displayValue, lang, "javascript");
    } catch {
      return escapeHtml(displayValue);
    }
  }, [displayValue]);

  const handleScroll = (e: UIEvent<HTMLTextAreaElement>) => {
    if (!preRef.current) return;
    preRef.current.scrollTop = e.currentTarget.scrollTop;
    preRef.current.scrollLeft = e.currentTarget.scrollLeft;
  };

  const handleChange = (e: ChangeEvent<HTMLTextAreaElement>) => {
    if (!readOnly) onChange(e.target.value);
  };

  return (
    <div className="space-y-1.5">
      <label htmlFor={id} className="text-sm font-medium">
        {t("form.scriptSource")}
      </label>
      <div
        className="relative rounded-lg border bg-[#2d2d2d] overflow-hidden"
        style={{ minHeight: `${minLines * 1.5}em` }}
      >
        <pre
          ref={preRef}
          aria-hidden="true"
          className="m-0 overflow-auto p-3 text-xs leading-6 font-mono pointer-events-none"
          style={{
            color: "#ccc",
            background: "transparent",
            whiteSpace: "pre",
            fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
          }}
        >
          <code
            className="language-javascript"
            dangerouslySetInnerHTML={{ __html: highlighted }}
          />
        </pre>
        <textarea
          ref={taRef}
          id={id}
          value={safeValue}
          onChange={handleChange}
          onScroll={handleScroll}
          readOnly={readOnly}
          spellCheck={false}
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          className="absolute inset-0 w-full h-full resize-none border-0 bg-transparent p-3 text-xs leading-6 font-mono outline-none"
          style={{
            color: "transparent",
            caretColor: "#ccc",
            fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
            whiteSpace: "pre",
            overflow: "auto",
          }}
          placeholder="function handle(event) {\n  return { decision: 'allow' };\n}"
        />
      </div>
      {error ? (
        <p className="text-xs text-destructive">
          {loc
            ? t("form.compileError", { line: loc.line, col: loc.col })
            : error}
        </p>
      ) : (
        <p className="text-xs text-muted-foreground">{t("form.scriptSchemaHelp")}</p>
      )}
    </div>
  );
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}
