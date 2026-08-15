import { create } from "zustand";

export interface LogEntry {
  timestamp: number;
  level: string;
  message: string;
  source?: string;
  attrs?: Record<string, string>;
}

export type LogLevel = "debug" | "info" | "warn" | "error";

interface LogsState {
  logs: LogEntry[];
  tailing: boolean;
  level: LogLevel;
  error: string | null;

  appendLog: (entry: LogEntry) => void;
  setTailing: (v: boolean) => void;
  setLevel: (l: LogLevel) => void;
  setError: (e: string | null) => void;
  clearLogs: () => void;
}

const MAX_LOGS = 500;

export const useLogsStore = create<LogsState>((set) => ({
  logs: [],
  tailing: false,
  level: "info",
  error: null,

  appendLog: (entry) =>
    set((s) => {
      const next = [...s.logs, entry];
      return { logs: next.length > MAX_LOGS ? next.slice(-MAX_LOGS) : next };
    }),
  setTailing: (v) => set({ tailing: v }),
  setLevel: (l) => set({ level: l }),
  setError: (e) => set({ error: e }),
  clearLogs: () => set({ logs: [] }),
}));
