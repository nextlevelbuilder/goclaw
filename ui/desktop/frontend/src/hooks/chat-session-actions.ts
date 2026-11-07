import { sessionService } from '../services/session-service'
import { useSessionStore } from '../stores/session-store'
import { useChatMessageStore } from '../stores/chat-message-store'
import { useChatActivityStore } from '../stores/chat-activity-store'

/**
 * Session actions that work on the stores directly, so views can delete or
 * rename a conversation without mounting a second `useSessions()` (the shell
 * holds the single instance that loads the list).
 */

/** Deletes a conversation on the gateway, then drops it locally. Throws if the gateway refuses. */
export async function deleteSessionByKey(key: string): Promise<void> {
  await sessionService.delete(key)
  const wasActive = useSessionStore.getState().activeSessionKey === key
  useSessionStore.getState().removeSession(key)
  if (wasActive) {
    useChatMessageStore.getState().clear()
    useChatActivityStore.getState().clear()
  }
}

/** Renames a conversation on the gateway, then updates the local title. Throws if the gateway refuses. */
export async function renameSessionByKey(key: string, label: string): Promise<void> {
  await sessionService.rename(key, label)
  useSessionStore.getState().updateSession(key, { title: label })
}
