import { useState, useRef, useCallback, useEffect } from "react";
import type { ProgressStep } from "@/components/shared/operation-progress";

/** Server-sent progress event from export/import endpoints. */
interface SseProgressEvent {
  phase: string;
  status: "running" | "done" | "error";
  detail?: string;
  current?: number;
  total?: number;
}

export interface SseCompleteEvent {
  download_url?: string;
  file_size?: number;
  file_name?: string;
  agent_id?: string;
  agent_key?: string;
  summary?: Record<string, number>;
  [key: string]: unknown;
}

interface SseErrorEvent {
  phase: string;
  detail: string;
  rolled_back: boolean;
  cleanup?: { db?: string; files?: string };
}

export type SseStatus = "idle" | "running" | "complete" | "error";

export interface UseSseProgressReturn {
  steps: ProgressStep[];
  status: SseStatus;
  error: SseErrorEvent | null;
  elapsed: number;
  result: SseCompleteEvent | null;
  startGet: (url: string) => void;
  startPost: (url: string, body: FormData) => void;
  cancel: () => void;
  reset: () => void;
}

/**
 * Generic hook for SSE progress tracking.
 * Parses `event: progress`, `event: complete`, `event: error` from streaming response.
 * Used by both export and import flows.
 */
export function useSseProgress(authHeaders: () => Record<string, string>): UseSseProgressReturn {
  const [steps, setSteps] = useState<ProgressStep[]>([]);
  const [status, setStatus] = useState<SseStatus>("idle");
  const [error, setError] = useState<SseErrorEvent | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const [result, setResult] = useState<SseCompleteEvent | null>(null);

  const abortRef = useRef<AbortController | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const cleanup = useCallback(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  useEffect(() => cleanup, [cleanup]);

  const startTimer = useCallback(() => {
    const t0 = Date.now();
    timerRef.current = setInterval(() => {
      setElapsed(Math.floor((Date.now() - t0) / 1000));
    }, 1000);
  }, []);

  const handleProgress = useCallback((evt: SseProgressEvent) => {
    setSteps((prev) => {
      const idx = prev.findIndex((s) => s.id === evt.phase);
      const step: ProgressStep = {
        id: evt.phase,
        label: evt.phase.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase()),
        status: evt.status === "error" ? "error" : evt.status === "done" ? "done" : "running",
        detail: evt.detail,
        current: evt.current,
        total: evt.total,
      };
      if (idx >= 0) {
        const next = [...prev];
        next[idx] = step;
        return next;
      }
      return [...prev, step];
    });
  }, []);

  const processStream = useCallback(
    async (res: Response) => {
      const reader = res.body?.getReader();
      if (!reader) return;

      const decoder = new TextDecoder();
      let buffer = "";

      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() ?? "";

          let eventType = "";
          let dataStr = "";

          for (const line of lines) {
            if (line.startsWith("event: ")) {
              eventType = line.slice(7).trim();
            } else if (line.startsWith("data: ")) {
              dataStr = line.slice(6);
            } else if (line === "" && eventType && dataStr) {
              try {
                const data = JSON.parse(dataStr);
                if (eventType === "progress") {
                  handleProgress(data as SseProgressEvent);
                } else if (eventType === "complete") {
                  setResult(data as SseCompleteEvent);
                  setStatus("complete");
                  cleanup();
                } else if (eventType === "error") {
                  setError(data as SseErrorEvent);
                  setStatus("error");
                  cleanup();
                }
              } catch { /* skip malformed data */ }
              eventType = "";
              dataStr = "";
            }
          }
        }
      } catch (e) {
        if ((e as Error).name !== "AbortError") {
          setError({ phase: "connection", detail: (e as Error).message, rolled_back: false });
          setStatus("error");
        }
      } finally {
        cleanup();
      }
    },
    [handleProgress, cleanup],
  );

  const doFetch = useCallback(
    async (url: string, init: RequestInit) => {
      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;

      setSteps([]);
      setStatus("running");
      setError(null);
      setResult(null);
      setElapsed(0);
      startTimer();

      try {
        const res = await fetch(url, { ...init, signal: controller.signal });
        if (!res.ok) {
          const err = await res.json().catch(() => ({ error: res.statusText }));
          const detail = typeof err.error === "string" ? err.error : err.error?.message ?? res.statusText;
          setError({ phase: "request", detail, rolled_back: false });
          setStatus("error");
          cleanup();
          return;
        }
        await processStream(res);
      } catch (e) {
        if ((e as Error).name !== "AbortError") {
          setError({ phase: "network", detail: (e as Error).message, rolled_back: false });
          setStatus("error");
          cleanup();
        }
      }
    },
    [startTimer, processStream, cleanup],
  );

  const startGet = useCallback(
    (url: string) => {
      doFetch(url, { method: "GET", headers: authHeaders() });
    },
    [doFetch, authHeaders],
  );

  const startPost = useCallback(
    (url: string, body: FormData) => {
      doFetch(url, { method: "POST", headers: authHeaders(), body });
    },
    [doFetch, authHeaders],
  );

  const cancel = useCallback(() => {
    abortRef.current?.abort();
    cleanup();
    if (status === "running") {
      setStatus("error");
      setError({ phase: "cancelled", detail: "Operation cancelled by user", rolled_back: false });
    }
  }, [status, cleanup]);

  const reset = useCallback(() => {
    abortRef.current?.abort();
    cleanup();
    setSteps([]);
    setStatus("idle");
    setError(null);
    setResult(null);
    setElapsed(0);
  }, [cleanup]);

  return { steps, status, error, elapsed, result, startGet, startPost, cancel, reset };
}
