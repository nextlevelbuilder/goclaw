import { useEffect, useRef, useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useChat } from '../../hooks/use-chat'
import { useAgents } from '../../hooks/use-agents'
import { useTeamTasks } from '../../hooks/use-team-tasks'
import { useSessionStore } from '../../stores/session-store'
import { ChatTopBar } from './ChatTopBar'
import { MessageBubble } from './MessageBubble'
import { ActivityIndicator } from './ActivityIndicator'
import { InputBar, type AttachedFile } from './InputBar'
import { TaskPanel } from './TaskPanel'
import { TaskDetailModal } from '../teams/TaskDetailModal'
import type { TeamTaskData } from '../../types/team'

export function ChatCanvas() {
  const { t } = useTranslation('common')
  const { messages, isRunning, activity, sendMessage, abort } = useChat()
  const { selectedAgent } = useAgents()
  const { members, fetchTaskDetail } = useTeamTasks()
  const activeSessionKey = useSessionStore((s) => s.activeSessionKey)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const scrollAreaRef = useRef<HTMLDivElement>(null)
  const userScrolledUp = useRef(false)
  const [selectedTask, setSelectedTask] = useState<TeamTaskData | null>(null)

  // Find last assistant message ID for streaming cursor
  const lastAssistantId = useMemo(() => {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].role === 'assistant') return messages[i].id
    }
    return null
  }, [messages])

  useEffect(() => {
    if (!userScrolledUp.current) {
      messagesEndRef.current?.scrollIntoView({ behavior: isRunning ? 'smooth' : 'instant' })
    }
  }, [messages, isRunning])

  const handleScroll = useCallback(() => {
    const el = scrollAreaRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50
    userScrolledUp.current = !atBottom
  }, [])

  const handleSend = useCallback((text: string, files?: AttachedFile[]) => {
    if (!selectedAgent) return
    userScrolledUp.current = false
    sendMessage(text, selectedAgent.id, files)
  }, [selectedAgent, sendMessage])

  const handleStop = useCallback(() => {
    abort()
  }, [abort])

  const hasMessages = messages.length > 0

  return (
    <div className="flex-1 flex flex-col min-h-0">
      <ChatTopBar />

      {/* Chat body — dots background covers messages + input */}
      <div className="flex-1 flex flex-col min-h-0 canvas-dots">
        {/* Messages area */}
        <div
          ref={scrollAreaRef}
          onScroll={handleScroll}
          className="flex-1 overflow-y-auto overscroll-contain px-4 py-2"
        >
          <div className="max-w-3xl mx-auto">
            {!selectedAgent && (
              <div className="flex flex-col items-center justify-center py-20">
                <div className="w-6 h-6 border-2 border-accent border-t-transparent rounded-full animate-spin mb-3" />
                <p className="text-sm text-text-muted">{t('loading')}</p>
              </div>
            )}
            {selectedAgent && !hasMessages && <EmptyState agentName={selectedAgent.name} onSuggestion={handleSend} />}

            {messages.map((msg) => (
              <MessageBubble
                key={msg.id}
                message={msg}
                isStreaming={isRunning && msg.id === lastAssistantId}
              />
            ))}

            {isRunning && activity && (
              <ActivityIndicator phase={activity.phase} tool={activity.tool} iteration={activity.iteration} />
            )}

            <div ref={messagesEndRef} />
          </div>
        </div>

        {/* Team task panel */}
        <TaskPanel sessionKey={activeSessionKey} onTaskClick={setSelectedTask} />

        {/* Input bar */}
        <InputBar
          onSend={handleSend}
          onStop={handleStop}
          disabled={!selectedAgent}
          isRunning={isRunning}
          placeholder={selectedAgent ? t('sendMessage') : t('selectAgent')}
        />
      </div>

      {/* Task detail modal (from TaskPanel click) */}
      {selectedTask && (
        <TaskDetailModal
          task={selectedTask}
          members={members}
          onClose={() => setSelectedTask(null)}
          onAssign={async () => {}}
          onDelete={async () => {}}
          onFetchDetail={fetchTaskDetail}
        />
      )}
    </div>
  )
}

/** Empty state with logo and clickable suggested prompts */
function EmptyState({ agentName, onSuggestion }: { agentName?: string; onSuggestion?: (text: string) => void }) {
  const { t } = useTranslation('desktop')
  const suggestions = t('chat.suggestions', { returnObjects: true }) as string[]
  return (
    <div className="flex flex-col items-center justify-center text-center py-20">
      <img src="/goclaw-icon.svg" alt="GoClaw" className="h-14 w-14 mb-5 opacity-30" />
      <h2 className="text-lg font-medium text-text-primary mb-1">
        {agentName
          ? t('chat.emptyTitle', { name: agentName })
          : t('chat.emptyTitleDefault')}
      </h2>
      <p className="text-sm text-text-muted max-w-sm mb-6">
        {agentName ? t('chat.emptyDescAgent') : t('chat.emptyDescNoAgent')}
      </p>
      {agentName && (
        <div className="flex flex-wrap justify-center gap-2">
          {suggestions.map((prompt) => (
            <button
              key={prompt}
              onClick={() => onSuggestion?.(prompt)}
              className="text-xs text-text-secondary bg-surface-secondary border border-border rounded-full px-3 py-1.5 hover:border-accent/40 hover:text-accent transition-colors cursor-pointer"
            >
              {prompt}
            </button>
          ))}
        </div>
      )}
      <p className="text-[10px] text-text-muted mt-4">
        Press <kbd className="px-1 py-0.5 bg-surface-tertiary rounded text-[10px] font-mono">⌘N</kbd> {t('sidebar.newChat').toLowerCase()}
      </p>
    </div>
  )
}
