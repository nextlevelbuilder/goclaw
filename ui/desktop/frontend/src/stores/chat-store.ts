// Barrel re-export — consumers can import from the focused stores directly
// or continue using this file for backward compatibility.
export { useChatMessageStore } from './chat-message-store'
export { useChatActivityStore } from './chat-activity-store'
export type { ChatMessage, ToolCall } from './chat-message-store'
export type { Activity } from './chat-activity-store'
