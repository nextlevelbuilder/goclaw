import { create } from "zustand";
import type { ChatMessage } from "@/types/chat";

interface SessionMessages {
  messages: ChatMessage[];
  streamText: string | null;
  thinkingText: string | null;
  isRunning: boolean;
}

interface ChatMessagesState {
  // Map of sessionKey -> session message state
  sessions: Record<string, SessionMessages>;
  
  // Set messages for a specific session
  setSessionMessages: (sessionKey: string, messages: ChatMessage[]) => void;
  
  // Update session messages incrementally
  updateSessionMessages: (sessionKey: string, updater: (prev: ChatMessage[]) => ChatMessage[]) => void;
  
  // Set streaming text for a session
  setSessionStream: (sessionKey: string, streamText: string | null) => void;
  
  // Set thinking text for a session
  setSessionThinking: (sessionKey: string, thinkingText: string | null) => void;
  
  // Set running state for a session
  setSessionRunning: (sessionKey: string, isRunning: boolean) => void;
}

export const useChatMessagesStore = create<ChatMessagesState>((set) => ({
  sessions: {},
  
  setSessionMessages: (sessionKey: string, messages: ChatMessage[]) => {
    set((state) => {
      const existing = state.sessions[sessionKey];
      return {
        sessions: {
          ...state.sessions,
          [sessionKey]: {
            messages,
            streamText: existing?.streamText ?? null,
            thinkingText: existing?.thinkingText ?? null,
            isRunning: existing?.isRunning ?? false,
          },
        },
      };
    });
  },
  
  updateSessionMessages: (sessionKey: string, updater: (prev: ChatMessage[]) => ChatMessage[]) => {
    set((state) => {
      const existing = state.sessions[sessionKey];
      const current = existing?.messages ?? [];
      return {
        sessions: {
          ...state.sessions,
          [sessionKey]: {
            messages: updater(current),
            streamText: existing?.streamText ?? null,
            thinkingText: existing?.thinkingText ?? null,
            isRunning: existing?.isRunning ?? false,
          },
        },
      };
    });
  },
  
  setSessionStream: (sessionKey: string, streamText: string | null) => {
    set((state) => {
      const existing = state.sessions[sessionKey];
      return {
        sessions: {
          ...state.sessions,
          [sessionKey]: {
            messages: existing?.messages ?? [],
            streamText,
            thinkingText: existing?.thinkingText ?? null,
            isRunning: existing?.isRunning ?? false,
          },
        },
      };
    });
  },
  
  setSessionThinking: (sessionKey: string, thinkingText: string | null) => {
    set((state) => {
      const existing = state.sessions[sessionKey];
      return {
        sessions: {
          ...state.sessions,
          [sessionKey]: {
            messages: existing?.messages ?? [],
            streamText: existing?.streamText ?? null,
            thinkingText,
            isRunning: existing?.isRunning ?? false,
          },
        },
      };
    });
  },
  
  setSessionRunning: (sessionKey: string, isRunning: boolean) => {
    set((state) => {
      const existing = state.sessions[sessionKey];
      return {
        sessions: {
          ...state.sessions,
          [sessionKey]: {
            messages: existing?.messages ?? [],
            streamText: existing?.streamText ?? null,
            thinkingText: existing?.thinkingText ?? null,
            isRunning,
          },
        },
      };
    });
  },
}));
