import { create } from "zustand";
import type { ChatMessage } from "@/types/chat";

const MAX_CACHED_SESSIONS = 25;

interface SessionMessages {
  messages: ChatMessage[];
  streamText: string | null;
  thinkingText: string | null;
  isRunning: boolean;
  lastAccessedAt: number;
}

interface ChatMessagesState {
  sessions: Record<string, SessionMessages>;
  setSessionMessages: (sessionKey: string, messages: ChatMessage[]) => void;
  updateSessionMessages: (sessionKey: string, updater: (prev: ChatMessage[]) => ChatMessage[]) => void;
  setSessionStream: (sessionKey: string, streamText: string | null) => void;
  setSessionThinking: (sessionKey: string, thinkingText: string | null) => void;
  setSessionRunning: (sessionKey: string, isRunning: boolean) => void;
}

// Evict oldest idle sessions when cache exceeds limit.
// Never evict sessions with an active run (isRunning).
function evictStale(sessions: Record<string, SessionMessages>): Record<string, SessionMessages> {
  const keys = Object.keys(sessions);
  if (keys.length <= MAX_CACHED_SESSIONS) return sessions;

  const evictable = keys
    .filter((k) => !sessions[k]?.isRunning)
    .sort((a, b) => (sessions[a]?.lastAccessedAt ?? 0) - (sessions[b]?.lastAccessedAt ?? 0));

  const toRemove = evictable.slice(0, keys.length - MAX_CACHED_SESSIONS);
  if (toRemove.length === 0) return sessions;

  const next = { ...sessions };
  for (const k of toRemove) delete next[k];
  return next;
}

function touchSession(existing: SessionMessages | undefined, patch: Partial<SessionMessages>): SessionMessages {
  return {
    messages: patch.messages ?? existing?.messages ?? [],
    streamText: patch.streamText !== undefined ? patch.streamText : (existing?.streamText ?? null),
    thinkingText: patch.thinkingText !== undefined ? patch.thinkingText : (existing?.thinkingText ?? null),
    isRunning: patch.isRunning !== undefined ? patch.isRunning : (existing?.isRunning ?? false),
    lastAccessedAt: Date.now(),
  };
}

export const useChatMessagesStore = create<ChatMessagesState>((set) => ({
  sessions: {},

  setSessionMessages: (sessionKey, messages) => {
    set((state) => ({
      sessions: evictStale({
        ...state.sessions,
        [sessionKey]: touchSession(state.sessions[sessionKey], { messages }),
      }),
    }));
  },

  updateSessionMessages: (sessionKey, updater) => {
    set((state) => {
      const current = state.sessions[sessionKey]?.messages ?? [];
      return {
        sessions: evictStale({
          ...state.sessions,
          [sessionKey]: touchSession(state.sessions[sessionKey], { messages: updater(current) }),
        }),
      };
    });
  },

  setSessionStream: (sessionKey, streamText) => {
    set((state) => ({
      sessions: {
        ...state.sessions,
        [sessionKey]: touchSession(state.sessions[sessionKey], { streamText }),
      },
    }));
  },

  setSessionThinking: (sessionKey, thinkingText) => {
    set((state) => ({
      sessions: {
        ...state.sessions,
        [sessionKey]: touchSession(state.sessions[sessionKey], { thinkingText }),
      },
    }));
  },

  setSessionRunning: (sessionKey, isRunning) => {
    set((state) => ({
      sessions: {
        ...state.sessions,
        [sessionKey]: touchSession(state.sessions[sessionKey], { isRunning }),
      },
    }));
  },
}));
