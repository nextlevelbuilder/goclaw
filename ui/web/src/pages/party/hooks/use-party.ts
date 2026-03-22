import { useState, useCallback, useRef } from "react";
import { useWs } from "@/hooks/use-ws";
import { useWsEvent } from "@/hooks/use-ws-event";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods, Events } from "@/api/protocol";

// --- Types ---

export type PartyMode = "standard" | "deep" | "token_ring";

export interface PersonaInfo {
  key: string;
  emoji: string;
  name: string;
  role: string;
  color: string;
}

export interface PartyMessage {
  id: string;
  type: "intro" | "spoke" | "thinking" | "round_header" | "context" | "summary" | "artifact";
  personaKey?: string;
  personaEmoji?: string;
  personaName?: string;
  content: string;
  round?: number;
  mode?: PartyMode;
  timestamp: number;
}

export interface PartySession {
  id: string;
  topic: string;
  status: "active" | "closed";
  personas: PersonaInfo[];
  round: number;
  mode: PartyMode;
  createdAt: string;
  // Preserved from backend for session restore
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  _history?: any[];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  _summary?: any;
}

export interface PartySummary {
  agreements?: string[];
  disagreements?: string[];
  decisions?: string[];
  actionItems?: string[];
  compliance?: string[];
  markdown?: string;
}

// Persona color palette for left-border styling
const PERSONA_COLORS = [
  "#3b82f6", "#ef4444", "#10b981", "#f59e0b", "#8b5cf6",
  "#ec4899", "#06b6d4", "#f97316", "#6366f1", "#14b8a6",
  "#e11d48", "#84cc16", "#a855f7", "#0ea5e9",
];

// Map backend status to frontend display status
function mapStatus(backendStatus: string): "active" | "closed" {
  return backendStatus === "closed" ? "closed" : "active";
}

// Transform backend session (snake_case) to frontend PartySession
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function transformSession(raw: any): PartySession {
  const personaKeys: string[] = Array.isArray(raw.personas)
    ? raw.personas
    : [];
  return {
    id: raw.id,
    topic: raw.topic ?? "",
    status: mapStatus(raw.status ?? ""),
    personas: personaKeys.map((k: string) => ({
      key: k,
      emoji: "",
      name: k,
      role: "",
      color: "",
    })),
    round: raw.round ?? 0,
    mode: raw.mode ?? "standard",
    createdAt: raw.created_at ?? raw.createdAt ?? "",
    _history: Array.isArray(raw.history) ? raw.history : undefined,
    _summary: raw.summary ?? undefined,
  };
}

// Hydrate PartyMessage[] from backend history (RoundResult[]) and summary
function hydrateMessages(
  history: any[] | undefined, // eslint-disable-line @typescript-eslint/no-explicit-any
  summary: any | undefined, // eslint-disable-line @typescript-eslint/no-explicit-any
  startId: number,
): { msgs: PartyMessage[]; nextId: number } {
  let id = startId;
  const msgs: PartyMessage[] = [];
  if (!history) return { msgs, nextId: id };

  for (const round of history) {
    // Round header
    msgs.push({
      id: `pm-${++id}`,
      type: "round_header",
      content: "",
      round: round.round,
      mode: round.mode as PartyMode,
      timestamp: Date.now(),
    });
    // Persona messages
    for (const m of round.messages ?? []) {
      msgs.push({
        id: `pm-${++id}`,
        type: "spoke",
        personaKey: m.persona_key,
        personaEmoji: m.emoji ?? "",
        personaName: m.display_name ?? m.persona_key,
        content: m.content ?? "",
        round: round.round,
        timestamp: Date.now(),
      });
    }
  }

  // Summary (if exists and has markdown)
  if (summary && typeof summary === "object" && summary.markdown) {
    msgs.push({
      id: `pm-${++id}`,
      type: "summary",
      content: summary.markdown,
      timestamp: Date.now(),
    });
  }

  return { msgs, nextId: id };
}

export function useParty() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);

  const [sessions, setSessions] = useState<PartySession[]>([]);
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [messages, setMessages] = useState<PartyMessage[]>([]);
  const [personas, setPersonas] = useState<PersonaInfo[]>([]);
  const [thinkingPersonas, setThinkingPersonas] = useState<Set<string>>(new Set());
  const [round, setRound] = useState(0);
  const [mode, setMode] = useState<PartyMode>("standard");
  const [status, setStatus] = useState<"idle" | "active" | "closed">("idle");
  const [summary, setSummary] = useState<PartySummary | null>(null);
  const [loading, setLoading] = useState(false);

  const msgIdCounter = useRef(0);
  const personaColorMap = useRef<Map<string, string>>(new Map());

  const getPersonaColor = useCallback((key: string): string => {
    if (personaColorMap.current.has(key)) {
      return personaColorMap.current.get(key)!;
    }
    const idx = personaColorMap.current.size % PERSONA_COLORS.length;
    const color = PERSONA_COLORS[idx] ?? "#3b82f6";
    personaColorMap.current.set(key, color);
    return color;
  }, []);

  const addMessage = useCallback((msg: Omit<PartyMessage, "id" | "timestamp">) => {
    const id = `pm-${++msgIdCounter.current}`;
    setMessages((prev) => [...prev, { ...msg, id, timestamp: Date.now() }]);
  }, []);

  // --- Event handlers ---
  // Backend sends snake_case field names (Go json tags)

  const handlePartyStarted = useCallback((payload: unknown) => {
    // Backend: { session_id, topic, personas: [{ agent_key, display_name, emoji, movie_ref }] }
    const p = payload as {
      session_id: string;
      topic: string;
      personas: Array<{ agent_key: string; display_name: string; emoji: string; movie_ref: string }>;
    };
    const sessionId = p.session_id;
    setActiveSessionId(sessionId);
    const enriched: PersonaInfo[] = (p.personas ?? []).map((pe) => ({
      key: pe.agent_key,
      emoji: pe.emoji ?? "",
      name: pe.display_name ?? pe.agent_key,
      role: pe.movie_ref ?? "",
      color: getPersonaColor(pe.agent_key),
    }));
    setPersonas(enriched);
    setRound(0);
    setMode("standard");
    setStatus("active");
    setSummary(null);
    setMessages([]);
    personaColorMap.current.clear();
    enriched.forEach((pe) => personaColorMap.current.set(pe.key, pe.color));

    // Add new session to sessions list for sidebar
    setSessions((prev) => [
      {
        id: sessionId,
        topic: p.topic ?? "",
        status: "active",
        personas: enriched,
        round: 0,
        mode: "standard",
        createdAt: new Date().toISOString(),
      },
      ...prev,
    ]);
  }, [getPersonaColor]);

  const handlePersonaIntro = useCallback((payload: unknown) => {
    // Backend: { session_id, persona, emoji, content }
    const p = payload as { persona: string; emoji: string; content: string };
    addMessage({
      type: "intro",
      personaKey: p.persona,
      personaEmoji: p.emoji ?? "",
      personaName: p.persona,
      content: p.content ?? "",
    });
  }, [addMessage]);

  const handleRoundStarted = useCallback((payload: unknown) => {
    const p = payload as { round: number; mode: string };
    setRound(p.round);
    setMode(p.mode as PartyMode);
    addMessage({
      type: "round_header",
      content: "",
      round: p.round,
      mode: p.mode as PartyMode,
    });
  }, [addMessage]);

  const handlePersonaThinking = useCallback((payload: unknown) => {
    // Backend: { session_id, persona, emoji }
    const p = payload as { persona: string };
    setThinkingPersonas((prev) => new Set(prev).add(p.persona));
  }, []);

  const handlePersonaSpoke = useCallback((payload: unknown) => {
    // Backend: { session_id, persona, emoji, content }
    const p = payload as { persona: string; emoji: string; content: string };
    setThinkingPersonas((prev) => {
      const next = new Set(prev);
      next.delete(p.persona);
      return next;
    });
    addMessage({
      type: "spoke",
      personaKey: p.persona,
      personaEmoji: p.emoji ?? "",
      personaName: p.persona,
      content: p.content ?? "",
    });
  }, [addMessage]);

  const handleRoundComplete = useCallback((payload: unknown) => {
    const p = payload as { round: number };
    setThinkingPersonas(new Set());
    void p;
  }, []);

  const handleContextAdded = useCallback((payload: unknown) => {
    const p = payload as { type: string; name?: string };
    addMessage({
      type: "context",
      content: `Context added: ${p.type}${p.name ? ` (${p.name})` : ""}`,
    });
  }, [addMessage]);

  const handleSummaryReady = useCallback((payload: unknown) => {
    // Backend: { session_id, summary: { markdown, ... } }
    const raw = payload as { summary?: PartySummary; markdown?: string };
    const s: PartySummary = raw.summary ?? raw as PartySummary;
    setSummary(s);
    addMessage({
      type: "summary",
      content: s.markdown ?? "",
    });
  }, [addMessage]);

  const handleArtifact = useCallback((payload: unknown) => {
    const p = payload as { name: string; content: string };
    addMessage({
      type: "artifact",
      content: `**${p.name}**\n\n${p.content}`,
    });
  }, [addMessage]);

  const handlePartyClosed = useCallback((payload: unknown) => {
    const p = payload as { session_id?: string };
    setStatus("closed");
    setThinkingPersonas(new Set());
    // Update session status in the list
    if (p.session_id) {
      setSessions((prev) =>
        prev.map((s) => (s.id === p.session_id ? { ...s, status: "closed" as const } : s)),
      );
    }
  }, []);

  // --- Subscribe to events ---
  useWsEvent(Events.PARTY_STARTED, handlePartyStarted);
  useWsEvent(Events.PARTY_PERSONA_INTRO, handlePersonaIntro);
  useWsEvent(Events.PARTY_ROUND_STARTED, handleRoundStarted);
  useWsEvent(Events.PARTY_PERSONA_THINKING, handlePersonaThinking);
  useWsEvent(Events.PARTY_PERSONA_SPOKE, handlePersonaSpoke);
  useWsEvent(Events.PARTY_ROUND_COMPLETE, handleRoundComplete);
  useWsEvent(Events.PARTY_CONTEXT_ADDED, handleContextAdded);
  useWsEvent(Events.PARTY_SUMMARY_READY, handleSummaryReady);
  useWsEvent(Events.PARTY_ARTIFACT, handleArtifact);
  useWsEvent(Events.PARTY_CLOSED, handlePartyClosed);

  // --- RPC calls ---
  // Backend expects snake_case field names (Go json tags)

  const listSessions = useCallback(async () => {
    if (!connected) return;
    setLoading(true);
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const res = await ws.call<{ sessions: any[] }>(Methods.PARTY_LIST, {});
      setSessions((res.sessions ?? []).map(transformSession));
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, [ws, connected]);

  const startParty = useCallback(
    async (topic: string, teamPreset?: string, personaKeys?: string[]) => {
      if (!connected) return;
      setLoading(true);
      try {
        await ws.call(Methods.PARTY_START, {
          topic,
          team_preset: teamPreset ?? undefined,
          personas: personaKeys ?? undefined,
        });
      } catch {
        // ignore
      } finally {
        setLoading(false);
      }
    },
    [ws, connected],
  );

  const runRound = useCallback(
    async (sessionId: string, roundMode?: PartyMode) => {
      await ws.call(Methods.PARTY_ROUND, {
        session_id: sessionId,
        mode: roundMode ?? undefined,
      });
    },
    [ws],
  );

  const askQuestion = useCallback(
    async (sessionId: string, text: string) => {
      await ws.call(Methods.PARTY_QUESTION, { session_id: sessionId, text });
    },
    [ws],
  );

  const addContext = useCallback(
    async (sessionId: string, type: string, name?: string, content?: string) => {
      await ws.call(Methods.PARTY_ADD_CONTEXT, { session_id: sessionId, type, name, content });
    },
    [ws],
  );

  const getSummary = useCallback(
    async (sessionId: string) => {
      await ws.call(Methods.PARTY_SUMMARY, { session_id: sessionId });
    },
    [ws],
  );

  const exitParty = useCallback(
    async (sessionId: string) => {
      await ws.call(Methods.PARTY_EXIT, { session_id: sessionId });
    },
    [ws],
  );

  // Activate an existing session from the list (hydrates all state including history)
  const selectSession = useCallback(
    (session: PartySession) => {
      setActiveSessionId(session.id);
      const enriched = session.personas.map((pe) => ({
        ...pe,
        color: getPersonaColor(pe.key),
      }));
      setPersonas(enriched);
      setRound(session.round);
      setMode(session.mode);
      setStatus(session.status === "closed" ? "closed" : "active");
      personaColorMap.current.clear();
      enriched.forEach((pe) => personaColorMap.current.set(pe.key, pe.color));

      // Hydrate messages from stored history
      const { msgs: restored, nextId } = hydrateMessages(session._history, session._summary, msgIdCounter.current);
      if (restored.length > 0) {
        msgIdCounter.current = nextId;
        setMessages(restored);
        // Restore summary state
        if (session._summary && typeof session._summary === "object" && session._summary.markdown) {
          setSummary(session._summary as PartySummary);
        } else {
          setSummary(null);
        }
      } else {
        setMessages([]);
        setSummary(null);
      }
    },
    [getPersonaColor],
  );

  return {
    // state
    sessions,
    activeSessionId,
    messages,
    personas,
    thinkingPersonas,
    round,
    mode,
    status,
    summary,
    loading,

    // actions
    listSessions,
    startParty,
    runRound,
    askQuestion,
    addContext,
    getSummary,
    exitParty,
    selectSession,
    setActiveSessionId,
    getPersonaColor,
  };
}
