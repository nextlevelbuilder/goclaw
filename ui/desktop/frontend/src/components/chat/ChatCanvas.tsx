import { useEffect, useRef, useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useChat } from '../../hooks/use-chat'
import { useAgents } from '../../hooks/use-agents'
import { useTeamTasks } from '../../hooks/use-team-tasks'
import { useSessionStore } from '../../stores/session-store'
import { useUiStore } from '../../stores/ui-store'
import { isToolDenied } from '../../utils/chat-error'
import { cn } from '../../lib/cn'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { Button, PageHeader, StatusChip } from '../ui'
import { TaskDetailModal } from '../teams/TaskDetailModal'
import type { TeamTaskData } from '../../types/team'
import { ChatHome } from './ChatHome'
import { ChatNotice } from './ChatNotice'
import { MessageBubble } from './MessageBubble'
import { ActivityIndicator } from './ActivityIndicator'
import { InputBar, type AttachedFile } from './InputBar'
import { ModelPicker } from './ModelPicker'
import { TaskPanel } from './TaskPanel'

export function ChatCanvas() {
  const { t } = useTranslation('chat')
  const { messages, isRunning, activity, error, sendMessage, abort, retry, continueReply, dismissError } = useChat()
  const { selectedAgent } = useAgents()
  const { members, fetchTaskDetail } = useTeamTasks()
  const { disconnected } = useGatewayStatus()
  const setActiveView = useUiStore((s) => s.setActiveView)
  const activeSessionKey = useSessionStore((s) => s.activeSessionKey)
  const sessionTitle = useSessionStore((s) => s.sessions.find((x) => x.key === s.activeSessionKey)?.title)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const scrollAreaRef = useRef<HTMLDivElement>(null)
  const userScrolledUp = useRef(false)
  const [selectedTask, setSelectedTask] = useState<TeamTaskData | null>(null)

  // Last assistant message gets the streaming cursor.
  const lastAssistant = useMemo(() => [...messages].reverse().find((m) => m.role === 'assistant') ?? null, [messages])
  const lastAssistantId = lastAssistant?.id ?? null

  // A tool the settings refused, from the latest reply only.
  const deniedTool = useMemo(
    () => [...(lastAssistant?.toolCalls ?? [])].reverse().find((tc) => tc.state === 'error' && isToolDenied(tc.error)),
    [lastAssistant],
  )

  useEffect(() => {
    if (!userScrolledUp.current) {
      messagesEndRef.current?.scrollIntoView({ behavior: isRunning ? 'smooth' : 'instant' })
    }
  }, [messages, isRunning, error])

  const handleScroll = useCallback(() => {
    const el = scrollAreaRef.current
    if (!el) return
    userScrolledUp.current = el.scrollHeight - el.scrollTop - el.clientHeight >= 50
  }, [])

  const handleSend = useCallback((text: string, files?: AttachedFile[]) => {
    if (!selectedAgent) return
    userScrolledUp.current = false
    sendMessage(text, selectedAgent.id, files)
  }, [selectedAgent, sendMessage])

  const showHome = !activeSessionKey && messages.length === 0 && !isRunning

  const agentId = selectedAgent?.id
  const noticeActions = useMemo(() => {
    if (!agentId || !error) return null
    switch (error.kind) {
      case 'provider':
        return (
          <>
            <Button size="sm" onClick={() => retry(agentId)}>{t('notice.retry')}</Button>
            <Button size="sm" variant="ghost" onClick={() => setActiveView('providers')}>{t('notice.provider.action')}</Button>
          </>
        )
      case 'interrupted':
        return (
          <>
            <Button size="sm" onClick={() => continueReply(agentId)}>{t('notice.interrupted.continue')}</Button>
            <Button size="sm" onClick={() => retry(agentId)}>{t('notice.retry')}</Button>
          </>
        )
      default:
        return <Button size="sm" onClick={() => retry(agentId)}>{t('notice.retry')}</Button>
    }
  }, [agentId, error, retry, continueReply, setActiveView, t])

  if (showHome) {
    return (
      <div className={cn('flex min-h-0 flex-1 flex-col', disconnected && 'opacity-60')}>
        <ChatHome onSend={handleSend} disabled={disconnected} />
      </div>
    )
  }

  const title = sessionTitle || messages.find((m) => m.role === 'user')?.content.slice(0, 60) || t('header.newChat')
  const description = selectedAgent
    ? t('header.agentModel', { agent: selectedAgent.name, model: selectedAgent.model })
    : t('header.selectAgent')

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title={title}
        description={description}
        actions={isRunning ? <StatusChip tone="success" label={t('header.running')} /> : undefined}
      />

      <div
        ref={scrollAreaRef}
        onScroll={handleScroll}
        className={cn('min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-6', disconnected && 'opacity-60')}
      >
        <div className="mx-auto flex w-full max-w-reading flex-col gap-4">
          {messages.map((msg) => (
            <MessageBubble key={msg.id} message={msg} isStreaming={isRunning && msg.id === lastAssistantId} />
          ))}

          {isRunning && activity && (
            <ActivityIndicator phase={activity.phase} tool={activity.tool} iteration={activity.iteration} />
          )}

          {deniedTool && (
            <ChatNotice
              tone="warning"
              title={t('notice.toolDenied.title')}
              body={t('notice.toolDenied.body', { tool: deniedTool.toolName })}
              actions={<Button size="sm" onClick={() => setActiveView('tools')}>{t('notice.toolDenied.action')}</Button>}
            />
          )}

          {error && (
            <ChatNotice
              tone={error.kind === 'provider' ? 'warning' : 'danger'}
              title={t(`notice.${error.kind}.title`)}
              body={error.kind === 'failed' ? error.message : t(`notice.${error.kind}.body`)}
              detail={error.kind !== 'failed' && error.message ? error.message : undefined}
              actions={noticeActions}
            />
          )}

          <div ref={messagesEndRef} />
        </div>
      </div>

      <TaskPanel sessionKey={activeSessionKey} onTaskClick={setSelectedTask} />

      <div className="shrink-0 px-6 pb-5">
        <div className="mx-auto w-full max-w-reading">
          <InputBar
            onSend={(text, files) => { dismissError(); handleSend(text, files) }}
            onStop={abort}
            disabled={!selectedAgent || disconnected}
            isRunning={isRunning}
            placeholder={selectedAgent ? t('composer.placeholder', { agent: selectedAgent.name }) : t('composer.placeholderNoAgent')}
            pills={<ModelPicker agentModel={selectedAgent?.model} disabled={!selectedAgent || disconnected} />}
          />
        </div>
      </div>

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
