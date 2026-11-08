import { useChatActivityStore } from '../../stores/chat-activity-store'
import { useChatMessageStore } from '../../stores/chat-message-store'
import { useSessionStore } from '../../stores/session-store'
import { useUiStore } from '../../stores/ui-store'
import { useAgentStore } from '../../stores/agent-store'

/**
 * Start a new chat: clear the active session and the transcript, then show the
 * chat view. The session itself is created by the first message. Works on the
 * stores directly so callers don't mount another useSessions() instance
 * (each instance re-runs its fetch effect).
 */
export function startNewChat(): void {
  useSessionStore.getState().setActiveSession(null)
  useChatMessageStore.getState().clear()
  useChatActivityStore.getState().clear()
  useUiStore.getState().setActiveView('chat')
}

/** Open an existing conversation of the selected agent in the chat view. */
export function openSession(key: string): void {
  useSessionStore.getState().setActiveSession(key)
  useUiStore.getState().setActiveView('chat')
}

/** Make an agent the chat agent and show the chat view. */
export function chatWithAgent(agentId: string): void {
  useAgentStore.getState().selectAgent(agentId)
  useUiStore.getState().setActiveView('chat')
}
