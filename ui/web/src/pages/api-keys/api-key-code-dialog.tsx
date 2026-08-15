import React, { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Copy, Check, Code2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";

type Tab = "curl" | "typescript" | "go";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** Simple syntax highlight: comments, strings, keywords */
function highlightCode(code: string, lang: Tab): React.ReactNode[] {
  const lines = code.split("\n");
  return lines.map((line, i) => {
    const highlighted = highlightLine(line, lang);
    return <div key={i}>{highlighted || "\u00A0"}</div>;
  });
}

function highlightLine(line: string, lang: Tab): React.ReactNode | string {
  // Comments
  if (lang === "curl" && line.trimStart().startsWith("#")) {
    return <span className="text-emerald-600 dark:text-emerald-400">{line}</span>;
  }
  if ((lang === "typescript" || lang === "go") && line.trimStart().startsWith("//")) {
    return <span className="text-emerald-600 dark:text-emerald-400">{line}</span>;
  }

  // Simple token-based highlighting
  const parts: React.ReactNode[] = [];
  const remaining = line;
  let key = 0;

  const kwPatterns = lang === "go"
    ? /\b(package|import|func|var|const|defer|if|err|nil|map|string|any)\b/g
    : lang === "typescript"
      ? /\b(const|let|await|async|new|return|import|from|export)\b/g
      : null;

  // Match strings first
  const stringRe = /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`/g;
  let lastIndex = 0;
  let match;

  while ((match = stringRe.exec(remaining)) !== null) {
    // Text before the string
    if (match.index > lastIndex) {
      const before = remaining.slice(lastIndex, match.index);
      parts.push(<span key={key++}>{highlightKeywords(before, kwPatterns)}</span>);
    }
    // The string itself
    parts.push(
      <span key={key++} className="text-amber-600 dark:text-amber-400">{match[0]}</span>,
    );
    lastIndex = match.index + match[0].length;
  }

  // Remaining text after last string
  if (lastIndex < remaining.length) {
    const after = remaining.slice(lastIndex);
    parts.push(<span key={key}>{highlightKeywords(after, kwPatterns)}</span>);
  }

  if (parts.length === 0) return line;
  return <>{parts}</>;
}

function highlightKeywords(text: string, pattern: RegExp | null): React.ReactNode | string {
  if (!pattern || !text) return text;
  const parts: (string | React.ReactNode)[] = [];
  let lastIdx = 0;
  let key = 0;
  let m;
  const re = new RegExp(pattern.source, "g");
  while ((m = re.exec(text)) !== null) {
    if (m.index > lastIdx) parts.push(text.slice(lastIdx, m.index));
    parts.push(
      <span key={key++} className="text-violet-600 dark:text-violet-400 font-semibold">{m[0]}</span>,
    );
    lastIdx = m.index + m[0].length;
  }
  if (lastIdx < text.length) parts.push(text.slice(lastIdx));
  if (parts.length === 0) return text;
  return <>{parts}</>;
}

export function ApiKeyCodeDialog({ open, onOpenChange }: Props) {
  const { t } = useTranslation("api-keys");
  const [tab, setTab] = useState<Tab>("curl");
  const [copied, setCopied] = useState(false);

  const baseUrl = "https://YOUR-GOCLAW-BACKEND";
  const placeholder = "YOUR-GOCLAW-API-KEY";

  const snippets: Record<Tab, string> = useMemo(() => ({
    curl: buildCurl(baseUrl, placeholder, t),
    typescript: buildTypescript(baseUrl, placeholder, t),
    go: buildGo(baseUrl, placeholder, t),
  }), [baseUrl, placeholder, t]);

  const highlighted = useMemo(() => highlightCode(snippets[tab], tab), [snippets, tab]);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(snippets[tab]);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const tabs: { key: Tab; label: string }[] = [
    { key: "curl", label: t("codeDialog.tabs.curl") },
    { key: "typescript", label: t("codeDialog.tabs.typescript") },
    { key: "go", label: t("codeDialog.tabs.go") },
  ];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-2xl flex flex-col max-h-[85vh]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Code2 className="h-5 w-5" />
            {t("codeDialog.title")}
          </DialogTitle>
          <DialogDescription>{t("codeDialog.description")}</DialogDescription>
        </DialogHeader>

        {/* Tab bar + copy */}
        <div className="flex items-center justify-between">
          <div className="flex rounded-lg bg-muted p-1 gap-0.5">
            {tabs.map((t) => (
              <button
                key={t.key}
                onClick={() => { setTab(t.key); setCopied(false); }}
                className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                  tab === t.key
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                {t.label}
              </button>
            ))}
          </div>
          <Button variant="outline" size="sm" onClick={handleCopy} className="gap-1.5 h-8">
            {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            <span className="text-xs">{copied ? t("codeDialog.copied") : t("codeDialog.copy")}</span>
          </Button>
        </div>

        {/* Code block — scrollable */}
        <div className="relative rounded-lg bg-muted/60 border overflow-auto min-h-0 flex-1">
          <pre className="p-4 text-xs leading-relaxed font-mono whitespace-pre">
            {highlighted}
          </pre>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// --- snippet builders ---

function buildCurl(baseUrl: string, key: string, t: (k: string) => string): string {
  return `# ${t("codeDialog.comments.chat")}
curl -X POST ${baseUrl}/v1/chat/completions \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "agent": "your-agent-key",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# ${t("codeDialog.comments.listAgents")}
curl ${baseUrl}/v1/agents \\
  -H "Authorization: Bearer ${key}"

# ${t("codeDialog.comments.listSessions")}
curl ${baseUrl}/v1/sessions \\
  -H "Authorization: Bearer ${key}"`;
}

function buildTypescript(baseUrl: string, key: string, t: (k: string) => string): string {
  return `const BASE_URL = "${baseUrl}";
const API_KEY = "${key}";

// ${t("codeDialog.comments.chat")}
const res = await fetch(\`\${BASE_URL}/v1/chat/completions\`, {
  method: "POST",
  headers: {
    "Authorization": \`Bearer \${API_KEY}\`,
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    agent: "your-agent-key",
    messages: [{ role: "user", content: "Hello!" }],
  }),
});

const data = await res.json();
console.log(data.choices[0].message.content);

// ${t("codeDialog.comments.listAgents")}
const agents = await fetch(\`\${BASE_URL}/v1/agents\`, {
  headers: { "Authorization": \`Bearer \${API_KEY}\` },
}).then(r => r.json());`;
}

function buildGo(baseUrl: string, key: string, t: (k: string) => string): string {
  return `package main

import (
\t"bytes"
\t"encoding/json"
\t"fmt"
\t"io"
\t"net/http"
)

func main() {
\tbaseURL := "${baseUrl}"
\tapiKey := "${key}"

\t// ${t("codeDialog.comments.chat")}
\tbody, _ := json.Marshal(map[string]any{
\t\t"agent":    "your-agent-key",
\t\t"messages": []map[string]string{
\t\t\t{"role": "user", "content": "Hello!"},
\t\t},
\t})

\treq, _ := http.NewRequest("POST",
\t\tbaseURL+"/v1/chat/completions",
\t\tbytes.NewReader(body))
\treq.Header.Set("Authorization", "Bearer "+apiKey)
\treq.Header.Set("Content-Type", "application/json")

\tresp, err := http.DefaultClient.Do(req)
\tif err != nil {
\t\tpanic(err)
\t}
\tdefer resp.Body.Close()

\tdata, _ := io.ReadAll(resp.Body)
\tfmt.Println(string(data))
}`;
}
