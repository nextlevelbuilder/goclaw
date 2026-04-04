import { create } from "zustand";
import { persist } from "zustand/middleware";

const MAX_EVENTS = 500;
const PERSIST_MAX = 20;

/** A single captured WS event entry */
export interface TeamEventEntry {
  id: number;
  event: string;
  payload: unknown;
  timestamp: number;
  teamId: string | null;
  userId: string | null;
  chatId: string | null;
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

function extractUserId(payload: unknown): string | null {
  if (!payload || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;
  if (typeof p.user_id === "string" && p.user_id) return p.user_id;
  if (typeof p.userId === "string" && p.userId) return p.userId;
  return null;
}

function extractChatId(payload: unknown): string | null {
  if (!payload || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;
  if (typeof p.chat_id === "string" && p.chat_id) return p.chat_id;
  if (typeof p.chatId === "string" && p.chatId) return p.chatId;
  return null;
}

// Counter seeded from persisted state after hydration
let counter = 0;

export const useTeamEventStore = create<TeamEventState>()(
  persist(
    (set) => ({
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
            userId: extractUserId(payload),
            chatId: extractChatId(payload),
          };
          const next = [...s.events, entry];
          const trimmed = next.length > MAX_EVENTS ? next.slice(next.length - MAX_EVENTS) : next;
          return { events: trimmed };
        });
      },

      clear: () => {
        set({ events: [] });
      },

      setPaused: (paused) => set({ paused }),
    }),
    {
      name: "goclaw:recentEvents", // keep existing localStorage key for backward compat
      partialize: (state) => ({
        // Only persist the most recent events — not transient paused flag
        events: state.events.slice(-PERSIST_MAX),
      }),
      onRehydrateStorage: () => (state) => {
        // Seed the counter from the highest persisted event id
        if (state?.events && state.events.length > 0) {
          const lastEvent = state.events[state.events.length - 1];
          if (lastEvent) counter = lastEvent.id;
        }
      },
    }
  )
);
