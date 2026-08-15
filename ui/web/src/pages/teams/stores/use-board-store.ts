import { create } from "zustand";

export type ViewMode = "kanban" | "list";
export type GroupBy = "status" | "owner";

interface BoardState {
  viewMode: ViewMode;
  groupBy: GroupBy;
  setViewMode: (mode: ViewMode) => void;
  setGroupBy: (by: GroupBy) => void;
}

const KEY = "goclaw:teamBoard";

function load(): Record<string, unknown> {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

function save(partial: Record<string, unknown>) {
  try {
    localStorage.setItem(KEY, JSON.stringify({ ...load(), ...partial }));
  } catch {
    /* ignore */
  }
}

export const useBoardStore = create<BoardState>((set) => {
  const s = load();
  return {
    viewMode: (s.viewMode as ViewMode) ?? "kanban",
    groupBy: (s.groupBy as GroupBy) ?? "status",
    setViewMode: (mode) => {
      save({ viewMode: mode });
      set({ viewMode: mode });
    },
    setGroupBy: (by) => {
      save({ groupBy: by });
      set({ groupBy: by });
    },
  };
});
