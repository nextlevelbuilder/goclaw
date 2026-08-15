import { useEffect, useRef, useState, useMemo } from "react";
import { Terminal, Play, Square, Trash2, Search } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { useLogs, type LogEntry, type LogLevel } from "./hooks/use-logs";

const levelColors: Record<string, string> = {
  error: "text-red-500",
  warn: "text-yellow-500",
  info: "text-blue-500",
  debug: "text-zinc-500",
};

const levels: LogLevel[] = ["debug", "info", "warn", "error"];

export function LogsPage() {
  const { t } = useTranslation("logs");
  const { logs, tailing, level, error, startTail, stopTail, clearLogs } =
    useLogs();
  const scrollRef = useRef<HTMLDivElement>(null);
  const [filterLevel, setFilterLevel] = useState<LogLevel>("debug");
  const [search, setSearch] = useState("");

  // Auto-scroll to bottom on new logs.
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs]);

  const filtered = useMemo(() => {
    const levelIdx = levels.indexOf(filterLevel);
    return logs.filter((entry) => {
      if (levels.indexOf(entry.level as LogLevel) < levelIdx) return false;
      if (search) {
        const q = search.toLowerCase();
        const haystack =
          `${entry.message} ${entry.source ?? ""} ${Object.values(entry.attrs ?? {}).join(" ")}`.toLowerCase();
        if (!haystack.includes(q)) return false;
      }
      return true;
    });
  }, [logs, filterLevel, search]);

  return (
    <div className="flex h-full flex-col p-6">
      <PageHeader
        title={t("title")}
        description={
          tailing
            ? t("descriptionLive", { level })
            : t("description")
        }
        actions={
          <div className="flex items-center gap-2">
            {tailing && <Badge variant="success">{t("live")}</Badge>}
            {tailing ? (
              <Button
                variant="outline"
                size="sm"
                onClick={stopTail}
                className="gap-1"
              >
                <Square className="h-3.5 w-3.5" /> {t("stop")}
              </Button>
            ) : (
              <div className="flex items-center gap-1">
                <select
                  value={level}
                  onChange={(e) => startTail(e.target.value as LogLevel)}
                  className="h-8 cursor-pointer rounded-md border bg-background px-2 text-base md:text-xs"
                  title={t("logLevelTitle")}
                >
                  {levels.map((l) => (
                    <option key={l} value={l}>
                      {l.toUpperCase()}
                    </option>
                  ))}
                </select>
                <Button
                  size="sm"
                  onClick={() => startTail()}
                  className="gap-1"
                >
                  <Play className="h-3.5 w-3.5" /> {t("start")}
                </Button>
              </div>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={clearLogs}
              disabled={logs.length === 0}
              className="gap-1"
            >
              <Trash2 className="h-3.5 w-3.5" /> {t("clear")}
            </Button>
          </div>
        }
      />

      {/* Filters */}
      <div className="mt-3 flex items-center gap-2">
        <div className="relative flex-1">
          <Search className="absolute left-2.5 top-2 h-3.5 w-3.5 text-muted-foreground" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t("filterPlaceholder")}
            className="h-7 w-full rounded-md border bg-background pl-8 pr-3 text-base md:text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </div>
        <div className="flex gap-0.5">
          {levels.map((l) => (
            <button
              key={l}
              onClick={() => setFilterLevel(l)}
              className={`cursor-pointer rounded-md px-2 py-1 text-xs font-medium transition-colors ${
                filterLevel === l
                  ? "bg-accent text-accent-foreground"
                  : "text-muted-foreground hover:bg-accent/50"
              }`}
            >
              {l.toUpperCase()}
            </button>
          ))}
        </div>
        <span className="text-xs text-muted-foreground">
          {t("count", { filtered: filtered.length, total: logs.length })}
        </span>
      </div>

      {/* Log output */}
      <div
        ref={scrollRef}
        className="mt-2 flex-1 overflow-y-auto rounded-md border bg-zinc-950 p-4 font-mono text-xs text-zinc-300"
      >
        {error ? (
          <div className="flex items-center justify-center py-12 text-zinc-500">
            <div className="text-center">
              <Terminal className="mx-auto mb-2 h-8 w-8" />
              <p className="text-yellow-500">{error}</p>
            </div>
          </div>
        ) : filtered.length === 0 ? (
          <div className="flex items-center justify-center py-12 text-zinc-500">
            <div className="text-center">
              <Terminal className="mx-auto mb-2 h-8 w-8" />
              <p>
                {tailing
                  ? search
                    ? t("emptyFilter")
                    : t("emptyWaiting")
                  : t("emptyStart")}
              </p>
            </div>
          </div>
        ) : (
          filtered.map((entry: LogEntry, i: number) => (
            <LogLine key={i} entry={entry} />
          ))
        )}
      </div>
    </div>
  );
}

function LogLine({ entry }: { entry: LogEntry }) {
  const attrs = entry.attrs;
  const hasAttrs = attrs && Object.keys(attrs).length > 0;

  return (
    <div className="leading-relaxed">
      <span className="text-zinc-500">
        {new Date(entry.timestamp).toLocaleTimeString()}
      </span>{" "}
      <span className={levelColors[entry.level] || "text-zinc-400"}>
        [{entry.level?.toUpperCase() || "LOG"}]
      </span>{" "}
      {entry.source && (
        <span className="text-cyan-600">[{entry.source}] </span>
      )}
      <span>{entry.message}</span>
      {hasAttrs && (
        <span className="ml-2 text-zinc-600">
          {Object.entries(attrs!).map(([k, v]) => (
            <span key={k} className="mr-2">
              <span className="text-zinc-500">{k}</span>
              <span className="text-zinc-600">=</span>
              <span className="text-zinc-400">{v}</span>
            </span>
          ))}
        </span>
      )}
    </div>
  );
}
