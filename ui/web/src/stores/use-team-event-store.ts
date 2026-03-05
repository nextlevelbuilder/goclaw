import { create } from "zustand";

const MAX_EVENTS = 500;

/** A single captured WS event entry */
export interface TeamEventEntry {
  id: number;
  event: string;
  payload: unknown;
  timestamp: number;
  teamId: string | null;
}

interface TeamEventState {
  events: TeamEventEntry[];
  paused: boolean;
  addEvent: (event: string, payload: unknown) => void;
  clear: () => void;
  setPaused: (paused: boolean) => void;
}

/**
 * Extract team_id from any known payload shape.
 * Delegation/team events use snake_case `team_id`,
 * enriched agent events use camelCase `teamId`.
 */
function extractTeamId(payload: unknown): string | null {
  if (!payload || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;
  if (typeof p.team_id === "string" && p.team_id) return p.team_id;
  if (typeof p.teamId === "string" && p.teamId) return p.teamId;
  return null;
}

let counter = 0;

export const useTeamEventStore = create<TeamEventState>((set) => ({
  events: [],
  paused: false,

  addEvent: (event, payload) => {
    set((s) => {
      if (s.paused) return s;
      const entry: TeamEventEntry = {
        id: ++counter,
        event,
        payload,
        timestamp: Date.now(),
        teamId: extractTeamId(payload),
      };
      const next = [...s.events, entry];
      if (next.length > MAX_EVENTS) {
        return { events: next.slice(next.length - MAX_EVENTS) };
      }
      return { events: next };
    });
  },

  clear: () => set({ events: [] }),
  setPaused: (paused) => set({ paused }),
}));
