import type { AgentStatus } from '../../stores/agent-store'

interface AgentAvatarProps {
  name: string
  status: AgentStatus
  size?: 'sm' | 'md'
  emoji?: string
}

const STATUS_CLASSES: Record<AgentStatus, string> = {
  online: 'bg-success',
  busy: 'bg-warning animate-pulse',
  offline: 'bg-error',
  idle: 'border-2 border-idle bg-transparent',
}

export function AgentAvatar({ name, status, size = 'sm', emoji }: AgentAvatarProps) {
  const dim = size === 'sm' ? 28 : 36
  const textSize = size === 'sm' ? 'text-[11px]' : 'text-sm'

  return (
    <div className="relative shrink-0" style={{ width: dim, height: dim }}>
      <div
        className={`flex items-center justify-center rounded-full ${emoji ? 'bg-accent/10' : 'bg-accent text-white'} font-semibold select-none ${textSize}`}
        style={{ width: dim, height: dim }}
      >
        {emoji || name.charAt(0).toUpperCase()}
      </div>
      <span
        className={`absolute bottom-0 right-0 w-2 h-2 rounded-full ring-1 ring-surface-secondary ${STATUS_CLASSES[status]}`}
      />
    </div>
  )
}
